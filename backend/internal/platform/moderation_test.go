package platform

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func moderationSource(t *testing.T) ChatEvent {
	t.Helper()
	now := time.Date(2026, 10, 15, 12, 0, 0, 123456000, time.UTC)
	event, err := NewChatEvent(Connection{TenantID: "tenant-a", ID: "twitch:123", Platform: "twitch"}, "native-1", now, now,
		ChatMessage{MessageID: "message-1", Text: "fixture", Actor: Identity{ID: "456"}, Channel: Identity{ID: "123"}})
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func TestModerationIdentityAndImmutablePayload(t *testing.T) {
	source := moderationSource(t)
	input := ModerationAction{ChannelID: "123", UserID: "456", Capability: "user.timeout", Duration: time.Minute, Reason: "fixture"}
	first, err := NewTwitchModerationIntent(source, "filter:1", input)
	if err != nil {
		t.Fatal(err)
	}
	if !moderationHash(first.Action().ActionID) || !moderationHash(first.Fingerprint()) || first.TenantID() != source.TenantID ||
		first.EventID() != source.ID || first.ConnectionID() != source.ConnectionID || first.Key() != "filter:1" ||
		!first.Deadline().Equal(source.OccurredAt.Add(2*time.Minute)) {
		t.Fatalf("incomplete intent: %#v", first)
	}
	// Pin the v1 serialization: changing these identities needs an explicit
	// version/migration decision rather than silently creating new actions.
	if first.Action().ActionID != "163f5b38d604d2c6e7464cfbc3f84012997a69634f6f41db20809a4942b35d0e" ||
		first.Fingerprint() != "981d76f9f991adba776854b24387fdf91d903994b646816f9325a58089e50b1d" {
		t.Fatal("v1 identity or fingerprint encoding changed")
	}
	// Receipt time, timezone representation and display metadata cannot renew
	// the deadline or select a new action on redelivery.
	source.ReceivedAt = source.ReceivedAt.Add(time.Hour)
	source.OccurredAt = source.OccurredAt.In(time.FixedZone("fixture", 3600))
	source.Chat.Actor.DisplayName = "changed"
	source.Chat.Text = "different display text"
	again, err := NewTwitchModerationIntent(source, "filter:1", input)
	if err != nil || !reflect.DeepEqual(first, again) {
		t.Fatalf("redelivery changed intent: %#v %v", again, err)
	}
	for _, change := range []func(*ModerationAction){
		func(a *ModerationAction) { a.UserID = "789" },
		func(a *ModerationAction) { a.Reason = "different" },
		func(a *ModerationAction) { a.Duration = 2 * time.Minute },
		func(a *ModerationAction) { a.Capability = "user.ban"; a.Duration = 0 },
	} {
		changed := input
		change(&changed)
		other, err := NewTwitchModerationIntent(source, "filter:1", changed)
		if err != nil || other.Action().ActionID != first.Action().ActionID || other.Fingerprint() == first.Fingerprint() {
			t.Fatalf("payload conflict not detectable: %#v %v", other, err)
		}
	}
	source.OccurredAt = source.OccurredAt.Add(time.Nanosecond)
	changedTime, err := NewTwitchModerationIntent(source, "filter:1", input)
	if err != nil || changedTime.Action().ActionID != first.Action().ActionID || changedTime.Fingerprint() == first.Fingerprint() {
		t.Fatal("changed source deadline did not conflict", err)
	}
	copy := first.Action()
	copy.UserID = "789"
	if first.Action().UserID != "456" || input.ActionID != "" {
		t.Fatal("caller changed snapshot")
	}
	if (ModerationIntent{}).Action().ValidateTwitch() == nil {
		t.Fatal("zero intent authorized")
	}
}

func TestModerationIdentitySeparatesScopesAndChat(t *testing.T) {
	base := moderationSource(t)
	input := ModerationAction{ChannelID: "123", MessageID: "message-1", Capability: "message.delete"}
	first, err := NewTwitchModerationIntent(base, "key", input)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"tenant", "channel", "source", "key"} {
		t.Run(field, func(t *testing.T) {
			c := Connection{TenantID: base.TenantID, ID: base.ConnectionID, Platform: base.Platform}
			chat, native, key := base.Chat, base.NativeEventID, "key"
			switch field {
			case "tenant":
				c.TenantID = "tenant-b"
			case "channel":
				chat.Channel.ID = "789"
				c.ID = "twitch:789"
			case "source":
				native = "native-2"
			case "key":
				key = "key-2"
			}
			source, err := NewChatEvent(c, native, base.OccurredAt, base.ReceivedAt, chat)
			if err != nil {
				t.Fatal(err)
			}
			action := input
			action.ChannelID = chat.Channel.ID
			other, err := NewTwitchModerationIntent(source, key, action)
			if err != nil || other.Action().ActionID == first.Action().ActionID {
				t.Fatal("scope collision", err)
			}
		})
	}
	// Existing chat IDs use the plain four-element scope. Moderation is
	// explicitly namespaced and versioned, without changing old chat identity.
	if first.Action().ActionID == moderationDigest([]any{base.TenantID, base.ConnectionID, base.ID, "key"}) {
		t.Fatal("moderation reused chat namespace")
	}
	// JSON array boundaries prevent separator collisions in otherwise valid keys.
	for _, key := range []string{"a:b", "a|b", "a\"b", "é"} {
		x, err := NewTwitchModerationIntent(base, key, input)
		if err != nil || x.Action().ActionID == first.Action().ActionID {
			t.Fatal("key encoding collision", err)
		}
	}
}

func TestModerationRejectsInvalidSources(t *testing.T) {
	base := moderationSource(t)
	input := ModerationAction{ChannelID: "123", MessageID: "message-1", Capability: "message.delete"}
	for name, change := range map[string]func(*ChatEvent){
		"version":          func(e *ChatEvent) { e.Version++ },
		"platform":         func(e *ChatEvent) { e.Platform = "kick" },
		"kind":             func(e *ChatEvent) { e.Kind = "raid" },
		"id":               func(e *ChatEvent) { e.ID = strings.Repeat("0", 64) },
		"idempotency":      func(e *ChatEvent) { e.IdempotencyKey = "wrong" },
		"trace":            func(e *ChatEvent) { e.TraceID = "wrong" },
		"channel":          func(e *ChatEvent) { e.Chat.Channel.ID = "789" },
		"connection":       func(e *ChatEvent) { e.ConnectionID = "twitch:789" },
		"tenant":           func(e *ChatEvent) { e.TenantID = "" },
		"native":           func(e *ChatEvent) { e.NativeEventID = "" },
		"native control":   func(e *ChatEvent) { e.NativeEventID = "native\n" },
		"native oversized": func(e *ChatEvent) { e.NativeEventID = strings.Repeat("a", 257) },
		"occurred":         func(e *ChatEvent) { e.OccurredAt = time.Time{} },
		"received":         func(e *ChatEvent) { e.ReceivedAt = time.Time{} },
		"time overflow":    func(e *ChatEvent) { e.OccurredAt = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC) },
	} {
		t.Run(name, func(t *testing.T) {
			source := base
			change(&source)
			if _, err := NewTwitchModerationIntent(source, "key", input); !errors.Is(err, ErrModerationAction) {
				t.Fatal("invalid source accepted", err)
			}
		})
	}
	for _, key := range []string{"", " ", "line\nbreak", string([]byte{255}), strings.Repeat("a", 129)} {
		if _, err := NewTwitchModerationIntent(base, key, input); !errors.Is(err, ErrModerationAction) {
			t.Fatal("invalid key accepted", err)
		}
	}
	input.ActionID = strings.Repeat("a", 64)
	if _, err := NewTwitchModerationIntent(base, "key", input); err == nil {
		t.Fatal("caller selected action ID")
	}
}

func TestModerationOperationValidation(t *testing.T) {
	for _, action := range []ModerationAction{
		{Capability: "message.delete", MessageID: "message-1"},
		{Capability: "user.timeout", UserID: "456", Duration: time.Second},
		{Capability: "user.timeout", UserID: "456", Duration: 14 * 24 * time.Hour, Reason: strings.Repeat("é", 500)},
		{Capability: "user.ban", UserID: "456", Reason: "fixture"},
		{Capability: "user.unban", UserID: "456"},
	} {
		action.ActionID, action.ChannelID = strings.Repeat("a", 64), "123"
		if err := action.ValidateTwitch(); err != nil {
			t.Fatalf("valid action rejected: %#v %v", action, err)
		}
	}
	base := ModerationAction{ActionID: strings.Repeat("a", 64), ChannelID: "123", Capability: "user.timeout", UserID: "456", Duration: time.Minute}
	for name, change := range map[string]func(*ModerationAction){
		"zero duration":       func(a *ModerationAction) { a.Duration = 0 },
		"fractional duration": func(a *ModerationAction) { a.Duration = time.Second + 1 },
		"negative duration":   func(a *ModerationAction) { a.Duration = -time.Second },
		"long duration":       func(a *ModerationAction) { a.Duration = 14*24*time.Hour + time.Second },
		"invalid user":        func(a *ModerationAction) { a.UserID = "456x" },
		"user whitespace":     func(a *ModerationAction) { a.UserID = " 456" },
		"missing user":        func(a *ModerationAction) { a.UserID = "" },
		"invalid channel":     func(a *ModerationAction) { a.ChannelID = "channel" },
		"uppercase ID":        func(a *ModerationAction) { a.ActionID = strings.Repeat("A", 64) },
		"unsupported":         func(a *ModerationAction) { a.Capability = "user.warn" },
		"implicit clear":      func(a *ModerationAction) { a.Capability = "message.delete"; a.UserID = ""; a.Duration = 0 },
		"explicit clear":      func(a *ModerationAction) { a.Capability = "chat.clear" },
		"ignored message":     func(a *ModerationAction) { a.MessageID = "message-1" },
		"reason length":       func(a *ModerationAction) { a.Reason = strings.Repeat("é", 501) },
		"reason control":      func(a *ModerationAction) { a.Reason = "reason\x00secret" },
		"reason utf8":         func(a *ModerationAction) { a.Reason = string([]byte{255}) },
		"ban duration":        func(a *ModerationAction) { a.Capability = "user.ban" },
		"unban reason":        func(a *ModerationAction) { a.Capability = "user.unban"; a.Duration = 0; a.Reason = "ignored" },
		"delete user":         func(a *ModerationAction) { a.Capability = "message.delete"; a.Duration = 0; a.MessageID = "m" },
		"delete reason": func(a *ModerationAction) {
			a.Capability = "message.delete"
			a.UserID = ""
			a.Duration = 0
			a.MessageID = "m"
			a.Reason = "ignored"
		},
	} {
		t.Run(name, func(t *testing.T) {
			a := base
			change(&a)
			if !errors.Is(a.ValidateTwitch(), ErrModerationAction) {
				t.Fatal("invalid action accepted")
			}
		})
	}
}

func TestModerationReceiptResolution(t *testing.T) {
	id := strings.Repeat("a", 64)
	providerErr := errors.New("private provider body with credentials")
	for _, tc := range []struct {
		name    string
		got     ModerationReceipt
		err     error
		status  DeliveryStatus
		wantErr error
	}{
		{"success", ModerationReceipt{ActionID: id, Status: Delivered}, nil, Delivered, nil},
		{"uncertain", ModerationReceipt{ActionID: id, Status: Uncertain}, nil, Uncertain, nil},
		{"rejected", ModerationReceipt{ActionID: id, Status: Rejected, Failure: Unauthorized}, nil, Rejected, nil},
		{"quota", ModerationReceipt{ActionID: id, Status: Rejected, Failure: QuotaLimited, RetryAfter: 24 * time.Hour}, providerErr, Rejected, ErrModerationAttempt},
		{"retryable", ModerationReceipt{ActionID: id, Status: Rejected, Failure: Retryable, RetryAfter: time.Second}, nil, Rejected, nil},
		{"success with error", ModerationReceipt{ActionID: id, Status: Delivered}, providerErr, Uncertain, ErrModerationReceipt},
		{"uncertain with error", ModerationReceipt{ActionID: id, Status: Uncertain}, providerErr, Uncertain, ErrModerationAttempt},
		{"zero result", ModerationReceipt{}, providerErr, Uncertain, ErrModerationReceipt},
		{"wrong action", ModerationReceipt{ActionID: strings.Repeat("b", 64), Status: Delivered}, nil, Uncertain, ErrModerationReceipt},
		{"unknown status", ModerationReceipt{ActionID: id, Status: "accepted"}, nil, Uncertain, ErrModerationReceipt},
		{"unknown failure", ModerationReceipt{ActionID: id, Status: Rejected, Failure: "raw provider body"}, nil, Uncertain, ErrModerationReceipt},
		{"success failure", ModerationReceipt{ActionID: id, Status: Delivered, Failure: Permanent}, nil, Uncertain, ErrModerationReceipt},
		{"uncertain retry", ModerationReceipt{ActionID: id, Status: Uncertain, RetryAfter: time.Second}, nil, Uncertain, ErrModerationReceipt},
		{"permanent retry", ModerationReceipt{ActionID: id, Status: Rejected, Failure: Permanent, RetryAfter: time.Second}, nil, Uncertain, ErrModerationReceipt},
		{"negative retry", ModerationReceipt{ActionID: id, Status: Rejected, Failure: QuotaLimited, RetryAfter: -1}, nil, Uncertain, ErrModerationReceipt},
		{"unbounded retry", ModerationReceipt{ActionID: id, Status: Rejected, Failure: QuotaLimited, RetryAfter: 24*time.Hour + 1}, nil, Uncertain, ErrModerationReceipt},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveModerationAttempt(id, tc.got, tc.err)
			if got.Status != tc.status || got.ActionID != id || !errors.Is(err, tc.wantErr) || got.Validate(id) != nil {
				t.Fatalf("unsafe result %#v %v", got, err)
			}
			if err != nil && strings.Contains(err.Error(), "private") {
				t.Fatal("raw provider error retained")
			}
			if tc.wantErr == ErrModerationReceipt && (got.Failure != "" || got.RetryAfter != 0) {
				t.Fatal("malformed provider metadata retained")
			}
		})
	}
	if got, err := ResolveModerationAttempt("invalid", ModerationReceipt{}, nil); got != (ModerationReceipt{}) || !errors.Is(err, ErrModerationAction) {
		t.Fatal("invalid expected identity accepted")
	}
	for _, failure := range []FailureKind{Unauthorized, Unsupported, Permanent, Retryable, QuotaLimited} {
		if (ModerationReceipt{ActionID: id, Status: Rejected, Failure: failure}).Validate(id) != nil {
			t.Fatal("known failure rejected", failure)
		}
	}
}

func TestModerationReceiptMetadataProjection(t *testing.T) {
	raw, err := json.Marshal(ModerationReceipt{ActionID: strings.Repeat("a", 64), Status: Rejected, Failure: QuotaLimited, RetryAfter: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 4 || fields["action_id"] == nil || fields["status"] == nil || fields["failure"] == nil || fields["retry_after"] == nil {
		t.Fatalf("receipt projection changed: %s", raw)
	}
}

func FuzzModerationReceiptResolution(f *testing.F) {
	f.Add("delivered", "", int64(0), false)
	f.Add("rejected", "quota_limited", int64(time.Second), true)
	f.Add("uncertain", "secret", int64(-1), true)
	f.Fuzz(func(t *testing.T, status, failure string, retry int64, failed bool) {
		id := strings.Repeat("a", 64)
		var err error
		if failed {
			err = errors.New("private provider payload")
		}
		r, _ := ResolveModerationAttempt(id, ModerationReceipt{ActionID: id, Status: DeliveryStatus(status), Failure: FailureKind(failure), RetryAfter: time.Duration(retry)}, err)
		if r.Validate(id) != nil || r.ActionID != id {
			t.Fatalf("resolver produced unsafe result: %#v", r)
		}
		if failed && r.Status == Delivered {
			t.Fatal("contradictory success accepted")
		}
	})
}
