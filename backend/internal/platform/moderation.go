package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const ModerationVersion = 1

var (
	ErrModerationAction  = errors.New("invalid moderation action")
	ErrModerationReceipt = errors.New("invalid moderation receipt")
	ErrModerationAttempt = errors.New("moderation attempt failed")
)

// ModerationAction is private effect input, not a public projection or job
// argument. An empty MessageID must never turn a delete into clear-chat.
type ModerationAction struct {
	ActionID, ChannelID, UserID, MessageID, Reason string
	Capability                                     Capability
	Duration                                       time.Duration
}

// ValidateTwitch validates only the four existing moderation operations. It
// grants no permission; the adapter must freshly check identity, grants, role,
// consent and entitlement before making a bounded attempt.
func (a ModerationAction) ValidateTwitch() error {
	if !moderationHash(a.ActionID) || !moderationNumericID(a.ChannelID) ||
		!moderationText(a.Reason, 500, true) {
		return ErrModerationAction
	}
	switch a.Capability {
	case "message.delete":
		if !moderationToken(a.MessageID, 256) || a.UserID != "" || a.Reason != "" || a.Duration != 0 {
			return ErrModerationAction
		}
	case "user.timeout", "user.ban", "user.unban":
		if !moderationNumericID(a.UserID) || a.MessageID != "" {
			return ErrModerationAction
		}
		if a.Capability == "user.timeout" {
			if a.Duration < time.Second || a.Duration > 14*24*time.Hour || a.Duration%time.Second != 0 {
				return ErrModerationAction
			}
		} else if a.Duration != 0 {
			return ErrModerationAction
		}
		if a.Capability == "user.unban" && a.Reason != "" {
			return ErrModerationAction
		}
	default:
		return ErrModerationAction
	}
	return nil
}

// ModerationIntent is a value snapshot. Its private fields cannot be replaced
// after construction; getters return values. Persistence must still verify the
// retained source and active connection and atomically enforce ID/fingerprint
// uniqueness, plan limits and a pre-effect claim. This type is not a ledger.
type ModerationIntent struct {
	action                                      ModerationAction
	tenant, connection, event, key, fingerprint string
	deadline                                    time.Time
}

func (i ModerationIntent) Action() ModerationAction { return i.action }
func (i ModerationIntent) TenantID() string         { return i.tenant }
func (i ModerationIntent) ConnectionID() string     { return i.connection }
func (i ModerationIntent) EventID() string          { return i.event }
func (i ModerationIntent) Key() string              { return i.key }
func (i ModerationIntent) Fingerprint() string      { return i.fingerprint }
func (i ModerationIntent) Deadline() time.Time      { return i.deadline }

// NewTwitchModerationIntent scopes a follow-up to a canonical chat source. The
// caller supplies no action ID or deadline. Receipt time, sender credentials and
// display metadata are excluded. Reusing a key with changed effect input keeps
// the ID and changes the fingerprint, so admission can reject the conflict.
func NewTwitchModerationIntent(source ChatEvent, key string, input ModerationAction) (ModerationIntent, error) {
	if source.Platform != "twitch" || source.Version != EventVersion || source.Kind != "chat_message" ||
		!moderationToken(source.TenantID, 256) || !moderationToken(source.NativeEventID, 256) ||
		!moderationNumericID(source.Chat.Channel.ID) || source.ConnectionID != "twitch:"+source.Chat.Channel.ID ||
		!moderationText(key, 128, false) || input.ActionID != "" || input.ChannelID != source.Chat.Channel.ID {
		return ModerationIntent{}, ErrModerationAction
	}
	canonical, err := NewChatEvent(Connection{TenantID: source.TenantID, ID: source.ConnectionID, Platform: source.Platform},
		source.NativeEventID, source.OccurredAt, source.ReceivedAt, source.Chat)
	if err != nil || source.ID != canonical.ID || source.IdempotencyKey != canonical.ID || source.TraceID != canonical.ID ||
		source.OccurredAt.Year() < 1 || source.OccurredAt.Year() > 9998 {
		return ModerationIntent{}, ErrModerationAction
	}
	id := moderationDigest([]any{"moderation", ModerationVersion, source.TenantID, source.ConnectionID, source.ID, key})
	input.ActionID = id
	if input.ValidateTwitch() != nil {
		return ModerationIntent{}, ErrModerationAction
	}
	// Match retained chat follow-ups: two minutes from occurrence, never from
	// receipt or a retry. Keep subsecond precision in the conflict fingerprint.
	deadline := source.OccurredAt.UTC().Add(2 * time.Minute)
	fingerprint := moderationDigest([]any{ModerationVersion, input.ChannelID, input.Capability,
		input.UserID, input.MessageID, input.Reason, int64(input.Duration), deadline.Format(time.RFC3339Nano)})
	return ModerationIntent{action: input, tenant: source.TenantID, connection: source.ConnectionID,
		event: source.ID, key: key, fingerprint: fingerprint, deadline: deadline}, nil
}

// ModerationReceipt describes one provider attempt. Delivered means confirmed
// provider acceptance, not observation of the resulting channel state. Unlike a
// chat receipt, success has no invented provider message ID. This is metadata
// only: target IDs, reasons, credentials and raw provider errors are excluded.
type ModerationReceipt struct {
	ActionID   string         `json:"action_id"`
	Status     DeliveryStatus `json:"status"`
	Failure    FailureKind    `json:"failure,omitempty"`
	RetryAfter time.Duration  `json:"retry_after,omitempty"`
}

func (r ModerationReceipt) Validate(expected string) error {
	if !moderationHash(expected) || r.ActionID != expected || r.RetryAfter < 0 || r.RetryAfter > 24*time.Hour {
		return ErrModerationReceipt
	}
	switch r.Status {
	case Delivered, Uncertain:
		if r.Failure != "" || r.RetryAfter != 0 {
			return ErrModerationReceipt
		}
	case Rejected:
		switch r.Failure {
		case Retryable, QuotaLimited:
		case Unauthorized, Unsupported, Permanent:
			if r.RetryAfter != 0 {
				return ErrModerationReceipt
			}
		default:
			return ErrModerationReceipt
		}
	default:
		return ErrModerationReceipt
	}
	return nil
}

// ResolveModerationAttempt checks an adapter result before retaining it. A
// malformed result or success accompanied by an error becomes uncertain. A
// confirmed rejection remains meaningful alongside an error. Returned errors
// are fixed and contain no provider data. No outcome here authorizes replay of
// a claimed action; RetryAfter is only a pacing hint for future actions.
func ResolveModerationAttempt(expected string, got ModerationReceipt, attemptErr error) (ModerationReceipt, error) {
	if !moderationHash(expected) {
		return ModerationReceipt{}, ErrModerationAction
	}
	if got.Validate(expected) != nil || attemptErr != nil && got.Status == Delivered {
		return ModerationReceipt{ActionID: expected, Status: Uncertain}, ErrModerationReceipt
	}
	if attemptErr != nil {
		return got, ErrModerationAttempt
	}
	return got, nil
}

func moderationDigest(value []any) string {
	raw, _ := json.Marshal(value) // Fixed primitive fields, validated before use.
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func moderationHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func moderationNumericID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func moderationToken(value string, max int) bool {
	if len(value) == 0 || len(value) > max {
		return false
	}
	for _, c := range value {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}

func moderationText(value string, max int, empty bool) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > max || (!empty && strings.TrimSpace(value) == "") {
		return false
	}
	for _, c := range value {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}
