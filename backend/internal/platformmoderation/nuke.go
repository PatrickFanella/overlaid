package platformmoderation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

// NukeOptions is the normalized private input to one retained preview. Neither
// a preview nor its fingerprint authorizes an effect or activates a worker.
type NukeOptions struct {
	Pattern                           string
	Regex, CaseSensitive              bool
	LookbackMinutes, HistoryLimit     int
	Action                            string
	DurationSeconds, RadiationMinutes int
}

type NukeTarget struct {
	EventID, MessageID, UserID, Login, Text string
	OccurredAt                              time.Time
}

type nukeBinding struct {
	options    NukeOptions
	observedAt time.Time
}

// NukePlan is an immutable in-process snapshot. Getters return copies; callers
// cannot replace its tenant, actor, options or private retained fingerprints.
// HTTP serialization/rehydration is a separate contract, not implemented here.
type NukePlan struct {
	tenant  string
	request ManualRequest
	targets []NukeTarget
}

func (p NukePlan) Options() NukeOptions {
	if p.request.nuke == nil {
		return NukeOptions{}
	}
	return p.request.nuke.options
}
func (p NukePlan) Targets() []NukeTarget { return append([]NukeTarget(nil), p.targets...) }
func (p NukePlan) ObservedAt() time.Time {
	if p.request.nuke == nil {
		return time.Time{}
	}
	return p.request.nuke.observedAt
}
func (p NukePlan) Fingerprint() string {
	_, fingerprint, _, _ := manualIdentity(p.tenant, p.request)
	return fingerprint
}

type NukeAdmission struct {
	ManualAdmission
	RadiationRuleID string `json:"radiationRuleId,omitempty"`
}

type NukeStatus struct {
	ManualStatus
	RadiationRuleID string `json:"radiationRuleId,omitempty"`
}

func normalizeNukeOptions(o NukeOptions) (NukeOptions, error) {
	o.Pattern = strings.TrimSpace(o.Pattern)
	o.Action = strings.ToLower(strings.TrimSpace(o.Action))
	if o.Action == "" {
		o.Action = "delete"
	}
	if !utf8.ValidString(o.Pattern) || len(o.Pattern) == 0 || len(o.Pattern) > 200 || o.Action != "delete" && o.Action != "timeout" || o.RadiationMinutes < 0 || o.RadiationMinutes > 120 {
		return NukeOptions{}, ErrInvalid
	}
	if o.LookbackMinutes == 0 {
		o.LookbackMinutes = 30
	}
	if o.HistoryLimit == 0 {
		o.HistoryLimit = 100
	}
	if o.LookbackMinutes < 1 || o.LookbackMinutes > 360 || o.HistoryLimit < 1 || o.HistoryLimit > 250 {
		return NukeOptions{}, ErrInvalid
	}
	if o.Action == "timeout" {
		if o.DurationSeconds == 0 {
			o.DurationSeconds = 60
		}
		if o.DurationSeconds < 1 || o.DurationSeconds > 1209600 {
			return NukeOptions{}, ErrInvalid
		}
	} else {
		o.DurationSeconds = 0
	}
	if _, err := bot.CompileModerationPattern(o.Pattern, o.Regex, o.CaseSensitive); err != nil {
		return NukeOptions{}, ErrInvalid
	}
	if o.RadiationMinutes > 0 {
		if _, err := nukeRadiationRule(o, "preview", time.Unix(1, 0)); err != nil {
			return NukeOptions{}, ErrInvalid
		}
	}
	return o, nil
}

func nukeRadiationID(tenant, source string) string {
	raw, _ := json.Marshal([]string{"nuke-radiation-v1", tenant, source})
	return hash(string(raw))
}
func nukeRadiationRule(o NukeOptions, id string, created time.Time) (bot.ModerationRule, error) {
	expiry := created.Add(time.Duration(o.RadiationMinutes) * time.Minute)
	return bot.NormalizeModerationRule(bot.ModerationRule{ID: id, Name: "nuke_radiation_" + id, Enabled: true, Enforce: true, Pattern: o.Pattern, MatchTarget: "message", CaseSensitive: o.CaseSensitive, Regex: o.Regex, Action: o.Action, Reason: "radiation:nuke:" + o.Pattern, Source: "nuke_radiation", ExpiresAt: &expiry, ExemptRole: "moderator", TimeoutSeconds: o.DurationSeconds})
}

func nukeAction(e platform.ChatEvent, o NukeOptions) platform.ModerationAction {
	a := platform.ModerationAction{ChannelID: e.Chat.Channel.ID, Capability: "message.delete", MessageID: e.Chat.MessageID}
	if o.Action == "timeout" {
		a.Capability, a.MessageID, a.UserID, a.Duration, a.Reason = "user.timeout", "", e.Chat.Actor.ID, time.Duration(o.DurationSeconds)*time.Second, "nuke:"+o.Pattern
	}
	return a
}
func nukeEvent(raw []byte, tenant, connection, id string) (platform.ChatEvent, error) {
	var e platform.ChatEvent
	if json.Unmarshal(raw, &e) != nil || e.Platform != "twitch" || e.TenantID != tenant || e.ConnectionID != connection || e.ID != id || connection != "twitch:"+e.Chat.Channel.ID {
		return e, ErrInvalid
	}
	rebuilt, err := platform.NewChatEvent(platform.Connection{TenantID: tenant, ID: connection, Platform: "twitch"}, e.NativeEventID, e.OccurredAt, e.ReceivedAt, e.Chat)
	if err != nil || rebuilt.ID != e.ID || e.IdempotencyKey != e.ID || e.TraceID != e.ID || e.Version != platform.EventVersion || e.Kind != "chat_message" {
		return e, ErrInvalid
	}
	return e, nil
}
func nukeEventHash(e platform.ChatEvent) string { raw, _ := json.Marshal(e); return hash(string(raw)) }

// PreviewNuke reads a bounded canonical chat snapshot using database time. It
// creates no source, action, job, pacing row or radiation rule. Empty previews
// can admit only an explicitly requested radiation-only occurrence.
func (s *Store) PreviewNuke(ctx context.Context, actor, connection string, options NukeOptions) (NukePlan, error) {
	o, err := normalizeNukeOptions(options)
	if err != nil || !tenantdb.ValidID(actor) || actor != strings.ToLower(actor) {
		return NukePlan{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return NukePlan{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var role, channel string
	err = tx.QueryRowContext(ctx, `SELECT role FROM tenant_memberships WHERE tenant_id=$1 AND account_id=$2`, s.db.TenantID(), actor).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) || err == nil && role != "owner" && role != "manager" && role != "moderator" {
		return NukePlan{}, ErrDenied
	}
	if err != nil {
		return NukePlan{}, err
	}
	err = tx.QueryRowContext(ctx, `SELECT provider_user_id FROM platform_connections WHERE /*tenant:platform_connections*/ AND provider='twitch' AND revoked_at IS NULL`).Scan(&channel)
	if errors.Is(err, sql.ErrNoRows) || err == nil && connection != "twitch:"+channel {
		return NukePlan{}, ErrInvalid
	}
	if err != nil {
		return NukePlan{}, err
	}
	var observed time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&observed); err != nil {
		return NukePlan{}, err
	}
	p := NukePlan{tenant: s.db.TenantID(), request: ManualRequest{ActorAccountID: actor, RequestKey: nukePlanRequestKey, ConnectionID: connection, Kind: "batch", nuke: &nukeBinding{options: o, observedAt: observed}}, targets: []NukeTarget{}}
	rows, err := tx.QueryContext(ctx, `SELECT id,envelope,occurred_at,actor_id FROM normalized_chat_events WHERE /*tenant:normalized_chat_events*/ AND connection_id=$1 AND platform='twitch' AND channel_id=$2 AND occurred_at BETWEEN $3 AND $4 ORDER BY occurred_at DESC,id DESC LIMIT $5`, connection, channel, observed.Add(-time.Duration(o.LookbackMinutes)*time.Minute), observed, o.HistoryLimit)
	if err != nil {
		return NukePlan{}, err
	}
	defer func() { _ = rows.Close() }()
	matcher, _ := bot.CompileModerationPattern(o.Pattern, o.Regex, o.CaseSensitive)
	seen := map[string]bool{}
	for rows.Next() {
		var id, actor string
		var occurred time.Time
		var raw []byte
		if err = rows.Scan(&id, &raw, &occurred, &actor); err != nil {
			return NukePlan{}, err
		}
		e, err := nukeEvent(raw, s.db.TenantID(), connection, id)
		if err != nil {
			return NukePlan{}, err
		}
		// PostgreSQL stores microseconds; the envelope keeps original precision.
		if e.OccurredAt.UnixMicro() != occurred.UnixMicro() || e.Chat.Actor.ID != actor || e.OccurredAt.After(observed) || e.OccurredAt.Before(observed.Add(-time.Duration(o.LookbackMinutes)*time.Minute)) {
			return NukePlan{}, ErrInvalid
		}
		if !matcher.Matches(e.Chat.Text) {
			continue
		}
		key := e.Chat.MessageID
		if o.Action == "timeout" {
			key = e.Chat.Actor.ID
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		action := nukeAction(e, o)
		checked := action
		checked.ActionID = strings.Repeat("0", 64)
		if checked.ValidateTwitch() != nil {
			return NukePlan{}, ErrInvalid
		}
		p.targets = append(p.targets, NukeTarget{EventID: e.ID, MessageID: e.Chat.MessageID, UserID: e.Chat.Actor.ID, Login: e.Chat.Actor.Login, Text: e.Chat.Text, OccurredAt: e.OccurredAt})
		p.request.Actions = append(p.request.Actions, ManualAction{Key: "target:" + e.ID, Action: action, targetEventID: e.ID, targetFingerprint: nukeEventHash(e)})
		if len(p.targets) == 50 {
			break
		}
	}
	if err = rows.Err(); err != nil {
		return NukePlan{}, err
	}
	if err = rows.Close(); err != nil {
		return NukePlan{}, err
	}
	if err = tx.Commit(); err != nil {
		return NukePlan{}, err
	}
	if len(p.targets) == 0 && o.RadiationMinutes > 0 {
		p.request.Kind = "radiation"
	}
	return p, nil
}

func (s *Store) nukeRequest(actor, key string, p NukePlan) (ManualRequest, error) {
	if p.tenant != s.db.TenantID() || p.request.ActorAccountID != actor || p.request.nuke == nil {
		return ManualRequest{}, ErrInvalid
	}
	r := p.request
	r.RequestKey = key
	r.Actions = append([]ManualAction(nil), r.Actions...)
	return r, nil
}
func (s *Store) AdmitNuke(ctx context.Context, actor, key string, p NukePlan) (NukeAdmission, error) {
	r, err := s.nukeRequest(actor, key, p)
	if err != nil {
		return NukeAdmission{}, err
	}
	admitted, err := s.AdmitManual(ctx, r)
	if err != nil {
		return NukeAdmission{}, err
	}
	result := NukeAdmission{ManualAdmission: admitted}
	if r.nuke.options.RadiationMinutes > 0 {
		result.RadiationRuleID = nukeRadiationID(s.db.TenantID(), admitted.SourceID)
	}
	return result, nil
}
func (s *Store) LookupNuke(ctx context.Context, actor, key string, p NukePlan) (NukeStatus, error) {
	r, err := s.nukeRequest(actor, key, p)
	if err != nil {
		return NukeStatus{}, err
	}
	status, err := s.LookupManual(ctx, r)
	if err != nil {
		return NukeStatus{}, err
	}
	result := NukeStatus{ManualStatus: status}
	if r.nuke.options.RadiationMinutes > 0 {
		result.RadiationRuleID = nukeRadiationID(s.db.TenantID(), status.SourceID)
	}
	return result, nil
}

func validateNukeTarget(ctx context.Context, tx *tenantdb.Tx, tenant, connection string, b *nukeBinding, entry ManualAction) (string, error) {
	var raw []byte
	var occurred time.Time
	var actor string
	err := tx.QueryRowContext(ctx, `SELECT envelope,occurred_at,actor_id FROM normalized_chat_events WHERE /*tenant:normalized_chat_events*/ AND id=$1 AND connection_id=$2 AND platform='twitch'`, entry.targetEventID, connection).Scan(&raw, &occurred, &actor)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrInvalid
	}
	if err != nil {
		return "", err
	}
	e, err := nukeEvent(raw, tenant, connection, entry.targetEventID)
	if err != nil || e.OccurredAt.UnixMicro() != occurred.UnixMicro() || e.Chat.Actor.ID != actor || nukeEventHash(e) != entry.targetFingerprint || nukeAction(e, b.options) != entry.Action || e.OccurredAt.After(b.observedAt) || e.OccurredAt.Before(b.observedAt.Add(-time.Duration(b.options.LookbackMinutes)*time.Minute)) {
		return "", ErrInvalid
	}
	matcher, _ := bot.CompileModerationPattern(b.options.Pattern, b.options.Regex, b.options.CaseSensitive)
	if !matcher.Matches(e.Chat.Text) {
		return "", ErrInvalid
	}
	return e.Chat.Actor.ID, nil
}

func insertNukeRadiation(ctx context.Context, tx *tenantdb.Tx, tenant, source string, b *nukeBinding, created time.Time) error {
	if b == nil || b.options.RadiationMinutes == 0 {
		return nil
	}
	rule, err := nukeRadiationRule(b.options, nukeRadiationID(tenant, source), created)
	if err != nil {
		return ErrInvalid
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO bot_moderation_rules(id,name,enabled,enforce,pattern,match_target,case_sensitive,regex,action,reason,source,expires_at,exempt_role,timeout_seconds) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, rule.ID, rule.Name, rule.Enabled, rule.Enforce, rule.Pattern, rule.MatchTarget, rule.CaseSensitive, rule.Regex, rule.Action, rule.Reason, rule.Source, rule.ExpiresAt, rule.ExemptRole, rule.TimeoutSeconds)
	return err
}
