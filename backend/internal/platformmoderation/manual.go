package platformmoderation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

// ManualRequest comes from an authenticated operator, never normalized chat.
// ActorAccountID is trusted request identity; RequestKey is one stable UUID.
// No supplied timestamp, deadline, action ID or credential is accepted.
type ManualRequest struct {
	ActorAccountID, RequestKey, ConnectionID, Kind string
	Actions                                        []ManualAction
	nuke                                           *nukeBinding
}

type ManualAction struct {
	Key                              string
	Action                           platform.ModerationAction
	targetEventID, targetFingerprint string
}

// ManualAdmission acknowledges durable queueing, not provider delivery. IDs
// are ordered by exact action key, independently of request array order.
type ManualAdmission struct {
	SourceID  string    `json:"sourceId"`
	ActionIDs []string  `json:"actionIds"`
	Created   bool      `json:"created"`
	CreatedAt time.Time `json:"createdAt"`
	Deadline  time.Time `json:"deadline"`
}

func manualIdentity(tenant string, r ManualRequest) (string, string, []ManualAction, error) {
	if !tenantdb.ValidID(r.ActorAccountID) || r.ActorAccountID != strings.ToLower(r.ActorAccountID) || !tenantdb.ValidID(r.RequestKey) || r.RequestKey != strings.ToLower(r.RequestKey) ||
		(r.Kind != "single" && r.Kind != "batch") || len(r.Actions) < 1 || len(r.Actions) > 50 || r.Kind == "single" && len(r.Actions) != 1 {
		return "", "", nil, ErrInvalid
	}
	actions := append([]ManualAction(nil), r.Actions...)
	sort.Slice(actions, func(i, j int) bool { return actions[i].Key < actions[j].Key })
	fields := []any{"manual-moderation-payload-v1", r.Kind, r.ConnectionID}
	for i, entry := range actions {
		if !utf8.ValidString(entry.Key) || utf8.RuneCountInString(entry.Key) > 128 || strings.TrimSpace(entry.Key) == "" || strings.IndexFunc(entry.Key, unicode.IsControl) >= 0 || i > 0 && entry.Key == actions[i-1].Key || entry.Action.ActionID != "" || r.ConnectionID != "twitch:"+entry.Action.ChannelID {
			return "", "", nil, ErrInvalid
		}
		checked := entry.Action
		checked.ActionID = strings.Repeat("0", 64)
		if checked.ValidateTwitch() != nil {
			return "", "", nil, ErrInvalid
		}
		fields = append(fields, []any{entry.Key, checked.ChannelID, checked.Capability, checked.UserID, checked.MessageID, checked.Reason, int64(checked.Duration)})
	}
	if r.nuke != nil {
		normalized, err := normalizeNukeOptions(r.nuke.options)
		if err != nil || normalized != r.nuke.options || r.Kind != "batch" || r.nuke.observedAt.Year() < 1970 || r.nuke.observedAt.Year() > 9998 {
			return "", "", nil, ErrInvalid
		}
		fields = append(fields, []any{"nuke-preview-v1", normalized, r.nuke.observedAt.UTC().Format(time.RFC3339Nano)})
		for _, entry := range actions {
			if !validHash(entry.targetEventID) || !validHash(entry.targetFingerprint) || entry.Key != "target:"+entry.targetEventID {
				return "", "", nil, ErrInvalid
			}
			fields = append(fields, []string{entry.targetEventID, entry.targetFingerprint})
		}
	} else {
		for _, entry := range actions {
			if entry.targetEventID != "" || entry.targetFingerprint != "" {
				return "", "", nil, ErrInvalid
			}
		}
	}
	raw, _ := json.Marshal([]string{"manual-moderation-source-v1", tenant, r.ActorAccountID, r.RequestKey})
	source := hash(string(raw))
	raw, _ = json.Marshal(fields)
	return source, hash(string(raw)), actions, nil
}

func manualActionID(tenant, source, key string) string {
	raw, _ := json.Marshal([]string{"manual-moderation-action-v1", tenant, source, key})
	return hash(string(raw))
}

// AdmitManual commits an authenticated occurrence, all actions and their
// payload-free jobs together. Duplicates never renew deadlines, recreate a
// lifecycle-deleted action or enqueue a terminal action again. No transport,
// pacing, claim or startup registration is performed here.
func (s *Store) AdmitManual(ctx context.Context, request ManualRequest) (ManualAdmission, error) {
	source, fingerprint, actions, err := manualIdentity(s.db.TenantID(), request)
	if err != nil {
		return ManualAdmission{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ManualAdmission{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT p.limits FROM tenants t JOIN plans p ON p.id=t.plan_id WHERE t.id=$1 FOR UPDATE OF t`, s.db.TenantID()).Scan(&raw); err != nil {
		return ManualAdmission{}, err
	}
	// Membership changes share the tenant lock. A request UUID grants no access
	// to an earlier result after the actor loses moderate permission.
	var role string
	err = tx.QueryRowContext(ctx, `SELECT role FROM tenant_memberships WHERE tenant_id=$1 AND account_id=$2`, s.db.TenantID(), request.ActorAccountID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) || err == nil && role != "owner" && role != "manager" && role != "moderator" {
		return ManualAdmission{}, ErrDenied
	}
	if err != nil {
		return ManualAdmission{}, err
	}
	result := ManualAdmission{SourceID: source, ActionIDs: make([]string, len(actions))}
	for i, entry := range actions {
		result.ActionIDs[i] = manualActionID(s.db.TenantID(), source, entry.Key)
	}
	var previous string
	err = tx.QueryRowContext(ctx, `SELECT payload_hash,created_at,deadline FROM platform_moderation_sources WHERE /*tenant:platform_moderation_sources*/ AND id=$1`, source).Scan(&previous, &result.CreatedAt, &result.Deadline)
	if err == nil {
		if previous != fingerprint {
			return ManualAdmission{}, ErrConflict
		}
		for _, id := range result.ActionIDs {
			var exists bool
			if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM platform_moderation_actions WHERE /*tenant:platform_moderation_actions*/ AND id=$1 AND manual_source_id=$2)`, id, source).Scan(&exists); err != nil {
				return ManualAdmission{}, err
			}
			if !exists {
				return ManualAdmission{}, ErrInvalid
			}
		}
		return result, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ManualAdmission{}, err
	}
	limits := map[string]int64{}
	if json.Unmarshal(raw, &limits) != nil {
		return ManualAdmission{}, ErrInvalid
	}
	limit, ok := limits["storedEvents"]
	if !ok || limit < -1 {
		return ManualAdmission{}, ErrInvalid
	}
	if limit == -1 || limit > 10000 {
		limit = 10000
	}
	var retained, sources int64
	if err = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/)+(SELECT count(*) FROM platform_moderation_actions WHERE /*tenant:platform_moderation_actions*/), (SELECT count(*) FROM platform_moderation_sources WHERE /*tenant:platform_moderation_sources*/ )`).Scan(&retained, &sources); err != nil {
		return ManualAdmission{}, err
	}
	if retained+int64(len(actions)) > limit || sources >= limit {
		return ManualAdmission{}, tenantdb.ErrLimit
	}
	var channel string
	err = tx.QueryRowContext(ctx, `SELECT provider_user_id FROM platform_connections WHERE /*tenant:platform_connections*/ AND provider='twitch' AND revoked_at IS NULL FOR SHARE`).Scan(&channel)
	if errors.Is(err, sql.ErrNoRows) || err == nil && request.ConnectionID != "twitch:"+channel {
		return ManualAdmission{}, ErrInvalid
	}
	if err != nil {
		return ManualAdmission{}, err
	}
	targets, nukeTargets, actors := make([]*string, len(actions)), make([]*string, len(actions)), make([]string, len(actions))
	for i, entry := range actions {
		if request.nuke != nil {
			actor, err := validateNukeTarget(ctx, tx, s.db.TenantID(), request.ConnectionID, request.nuke, entry)
			if err != nil {
				return ManualAdmission{}, err
			}
			target := entry.targetEventID
			actors[i] = actor
			if entry.Action.Capability == "user.timeout" {
				nukeTargets[i] = &target
			} else {
				targets[i] = &target
			}
			continue
		}
		actors[i] = entry.Action.UserID
		if entry.Action.Capability == "message.delete" {
			var target string
			err = tx.QueryRowContext(ctx, `SELECT id,actor_id FROM normalized_chat_events WHERE /*tenant:normalized_chat_events*/ AND connection_id=$1 AND platform='twitch' AND channel_id=$2 AND envelope->'chat'->>'message_id'=$3 ORDER BY occurred_at,id LIMIT 1`, request.ConnectionID, channel, entry.Action.MessageID).Scan(&target, &actors[i])
			if errors.Is(err, sql.ErrNoRows) {
				return ManualAdmission{}, ErrInvalid
			}
			if err != nil {
				return ManualAdmission{}, err
			}
			targets[i] = &target
		}
	}
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&result.CreatedAt); err != nil {
		return ManualAdmission{}, err
	}
	if request.nuke != nil && (result.CreatedAt.Before(request.nuke.observedAt) || !result.CreatedAt.Before(request.nuke.observedAt.Add(2*time.Minute))) {
		return ManualAdmission{}, ErrInvalid
	}
	result.Deadline, result.Created = result.CreatedAt.Add(2*time.Minute), true
	_, err = tx.ExecContext(ctx, `INSERT INTO platform_moderation_sources(id,actor_account_id,connection_id,channel_id,kind,payload_hash,action_count,created_at,deadline) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, source, request.ActorAccountID, request.ConnectionID, channel, request.Kind, fingerprint, len(actions), result.CreatedAt, result.Deadline)
	if err != nil {
		return ManualAdmission{}, err
	}
	for i, entry := range actions {
		a := entry.Action
		_, err = tx.ExecContext(ctx, `INSERT INTO platform_moderation_actions(id,manual_source_id,connection_id,action_key,channel_id,capability,user_id,message_id,reason,duration_ns,target_event_id,target_actor_id,payload_hash,deadline,nuke_target_event_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, result.ActionIDs[i], source, request.ConnectionID, entry.Key, channel, a.Capability, a.UserID, a.MessageID, a.Reason, int64(a.Duration), targets[i], actors[i], fingerprint, result.Deadline, nukeTargets[i])
		if err != nil {
			return ManualAdmission{}, err
		}
		if _, err = tx.EnqueueModeration(ctx, result.ActionIDs[i]); err != nil {
			return ManualAdmission{}, err
		}
	}
	if err = insertNukeRadiation(ctx, tx, s.db.TenantID(), source, request.nuke, result.CreatedAt); err != nil {
		return ManualAdmission{}, err
	}
	if err = tx.Commit(); err != nil {
		return ManualAdmission{}, err
	}
	return result, nil
}
