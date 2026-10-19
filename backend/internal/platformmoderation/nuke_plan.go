package platformmoderation

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

const (
	MaxNukePlanTokenBytes = 512 * 1024
	maxNukePlanPlainBytes = 256 * 1024
	nukePlanRequestKey    = "00000000-0000-4000-8000-000000000000"
)

var ErrNukePlanUnavailable = errors.New("nuke preview unavailable")

// NukePlanScope comes from trusted request/connection metadata, never the
// browser. Binding is the current opaque ChatSenderSelection binding. It is
// a generation comparison, not authorization or a provider credential.
type NukePlanScope struct {
	TenantID, ActorID, ConnectionID, Binding string
}

type sealedNukePlan struct {
	Version    int    `json:"version"`
	KeyVersion string `json:"keyVersion"`
	Ciphertext []byte `json:"ciphertext"`
}

type savedNukeAction struct {
	Key               string                    `json:"key"`
	Action            platform.ModerationAction `json:"action"`
	TargetEventID     string                    `json:"targetEventId"`
	TargetFingerprint string                    `json:"targetFingerprint"`
}

type savedNukePlan struct {
	Options     NukeOptions       `json:"options"`
	ObservedAt  time.Time         `json:"observedAt"`
	Kind        string            `json:"kind"`
	Fingerprint string            `json:"fingerprint"`
	Targets     []NukeTarget      `json:"targets"`
	Actions     []savedNukeAction `json:"actions"`
}

func (s NukePlanScope) purpose() (string, bool) {
	channel, ok := strings.CutPrefix(s.ConnectionID, "twitch:")
	if !ok || len(channel) == 0 || len(channel) > 128 || !tenantdb.ValidID(s.TenantID) || s.TenantID != strings.ToLower(s.TenantID) || !tenantdb.ValidID(s.ActorID) || s.ActorID != strings.ToLower(s.ActorID) || !validHash(s.Binding) {
		return "", false
	}
	for _, c := range channel {
		if c < '0' || c > '9' {
			return "", false
		}
	}
	raw, _ := json.Marshal(s)
	return "retained-nuke-preview-v1:" + hash(string(raw)), true
}

// SealNukePlan encodes input only. It creates no row or effect permission and
// uses no provider credentials. Missing vault custody has no fallback key.
func SealNukePlan(keys tenancy.Keyring, scope NukePlanScope, plan NukePlan) (string, error) {
	purpose, ok := scope.purpose()
	if !ok || len(keys.Active) == 0 || len(keys.Active) > 40 || len(keys.Keys[keys.Active]) != 32 || !validSavedNukePlan(scope, plan) {
		return "", ErrNukePlanUnavailable
	}
	saved := savedNukePlan{Options: plan.Options(), ObservedAt: plan.ObservedAt(), Kind: plan.request.Kind, Fingerprint: plan.Fingerprint(), Targets: plan.Targets(), Actions: make([]savedNukeAction, len(plan.request.Actions))}
	for i, a := range plan.request.Actions {
		saved.Actions[i] = savedNukeAction{Key: a.Key, Action: a.Action, TargetEventID: a.targetEventID, TargetFingerprint: a.targetFingerprint}
	}
	raw, err := json.Marshal(saved)
	if err != nil || len(raw) > maxNukePlanPlainBytes {
		return "", ErrNukePlanUnavailable
	}
	ciphertext, err := keys.Seal(scope.TenantID, purpose, raw)
	if err != nil {
		return "", ErrNukePlanUnavailable
	}
	wrapped, err := json.Marshal(sealedNukePlan{Version: 1, KeyVersion: keys.Active, Ciphertext: ciphertext})
	if err != nil || base64.RawURLEncoding.EncodedLen(len(wrapped)) > MaxNukePlanTokenBytes {
		return "", ErrNukePlanUnavailable
	}
	return base64.RawURLEncoding.EncodeToString(wrapped), nil
}

// OpenNukePlan restores the original input, including its original timestamp.
// It intentionally does not expire a plan: read-only receipt lookup may need
// old input. AdmitNuke remains responsible for fresh DB-clock admission checks.
func OpenNukePlan(keys tenancy.Keyring, scope NukePlanScope, token string) (NukePlan, error) {
	purpose, ok := scope.purpose()
	if !ok || len(token) == 0 || len(token) > MaxNukePlanTokenBytes {
		return NukePlan{}, ErrNukePlanUnavailable
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || base64.RawURLEncoding.EncodeToString(raw) != token {
		return NukePlan{}, ErrNukePlanUnavailable
	}
	var wrapped sealedNukePlan
	if !decodeNukePlanJSON(raw, &wrapped) || wrapped.Version != 1 || len(wrapped.KeyVersion) == 0 || len(wrapped.KeyVersion) > 40 || len(keys.Keys[wrapped.KeyVersion]) != 32 || len(wrapped.Ciphertext) > maxNukePlanPlainBytes+64 {
		return NukePlan{}, ErrNukePlanUnavailable
	}
	raw, err = keys.Unseal(scope.TenantID, purpose, wrapped.KeyVersion, wrapped.Ciphertext)
	if err != nil || len(raw) > maxNukePlanPlainBytes {
		return NukePlan{}, ErrNukePlanUnavailable
	}
	var saved savedNukePlan
	if !decodeNukePlanJSON(raw, &saved) || len(saved.Actions) > 50 || len(saved.Targets) > 50 {
		return NukePlan{}, ErrNukePlanUnavailable
	}
	plan := NukePlan{tenant: scope.TenantID, targets: saved.Targets, request: ManualRequest{ActorAccountID: scope.ActorID, RequestKey: nukePlanRequestKey, ConnectionID: scope.ConnectionID, Kind: saved.Kind, nuke: &nukeBinding{options: saved.Options, observedAt: saved.ObservedAt}, Actions: make([]ManualAction, len(saved.Actions))}}
	for i, a := range saved.Actions {
		plan.request.Actions[i] = ManualAction{Key: a.Key, Action: a.Action, targetEventID: a.TargetEventID, targetFingerprint: a.TargetFingerprint}
	}
	if !validSavedNukePlan(scope, plan) || plan.Fingerprint() != saved.Fingerprint {
		return NukePlan{}, ErrNukePlanUnavailable
	}
	return plan, nil
}

func decodeNukePlanJSON(raw []byte, value any) bool {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(value) != nil {
		return false
	}
	return errors.Is(d.Decode(new(any)), io.EOF)
}

func validSavedNukePlan(scope NukePlanScope, p NukePlan) bool {
	if p.tenant != scope.TenantID || p.request.ActorAccountID != scope.ActorID || p.request.ConnectionID != scope.ConnectionID || p.request.RequestKey != nukePlanRequestKey || p.request.nuke == nil || len(p.targets) != len(p.request.Actions) || p.Fingerprint() == "" {
		return false
	}
	o := p.Options()
	matcher, err := bot.CompileModerationPattern(o.Pattern, o.Regex, o.CaseSensitive)
	if err != nil {
		return false
	}
	seen := map[string]bool{}
	for i, t := range p.targets {
		a := p.request.Actions[i]
		e := platform.ChatEvent{Chat: platform.ChatMessage{MessageID: t.MessageID, Text: t.Text, Actor: platform.Identity{ID: t.UserID}, Channel: platform.Identity{ID: strings.TrimPrefix(scope.ConnectionID, "twitch:")}}}
		key := t.MessageID
		if o.Action == "timeout" {
			key = t.UserID
		}
		if seen[key] || t.EventID != a.targetEventID || a.Action != nukeAction(e, o) || !matcher.Matches(t.Text) || t.OccurredAt.After(p.ObservedAt()) || t.OccurredAt.Before(p.ObservedAt().Add(-time.Duration(o.LookbackMinutes)*time.Minute)) {
			return false
		}
		seen[key] = true
	}
	return true
}
