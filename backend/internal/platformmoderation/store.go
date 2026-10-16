// Package platformmoderation retains typed moderation actions and one-use
// pre-effect fences. It atomically schedules jobs but grants no effect permission.
package platformmoderation

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

var (
	ErrInvalid  = errors.New("invalid or unbound moderation action")
	ErrConflict = errors.New("moderation action payload conflict")
	ErrFence    = errors.New("moderation completion fence rejected")
)

type Store struct{ db *tenantdb.DB }

func New(db *tenantdb.DB) (*Store, error) {
	if db == nil || !db.IsScoped() {
		return nil, ErrInvalid
	}
	return &Store{db: db}, nil
}

// Admit rebuilds the intent from its retained source, not caller-supplied chat
// data. Duplicate input is a no-op even after completion. Admission and chat
// follow-ups share the tenant lock, plan bound and sixteen-per-source bound.
func (s *Store) Admit(ctx context.Context, intent platform.ModerationIntent) (string, bool, error) {
	a := intent.Action()
	if a.ValidateTwitch() != nil || intent.TenantID() != s.db.TenantID() {
		return "", false, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = tx.Rollback() }()
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT p.limits FROM tenants t JOIN plans p ON p.id=t.plan_id WHERE t.id=$1 FOR UPDATE OF t`, s.db.TenantID()).Scan(&raw); err != nil {
		return "", false, err
	}
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT payload_hash FROM platform_moderation_actions WHERE /*tenant:platform_moderation_actions*/ AND id=$1`, a.ActionID).Scan(&existing)
	if err == nil {
		if existing != intent.Fingerprint() {
			return "", false, ErrConflict
		}
		return a.ActionID, false, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", false, err
	}
	var envelope []byte
	var channel string
	err = tx.QueryRowContext(ctx, `SELECT e.envelope,e.channel_id FROM normalized_chat_events e
 JOIN platform_connections c ON c.tenant_id=e.tenant_id AND c.provider=e.platform AND c.provider_user_id=e.channel_id
 WHERE e.tenant_id=$1 AND e.id=$2 AND e.connection_id=$3 AND e.platform='twitch' AND c.revoked_at IS NULL AND $4::timestamptz>clock_timestamp()
 FOR SHARE OF c`, s.db.TenantID(), intent.EventID(), intent.ConnectionID(), intent.Deadline()).Scan(&envelope, &channel)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, ErrInvalid
	}
	if err != nil {
		return "", false, err
	}
	var source platform.ChatEvent
	if json.Unmarshal(envelope, &source) != nil || source.TenantID != s.db.TenantID() || source.ID != intent.EventID() || source.ConnectionID != intent.ConnectionID() || source.Chat.Channel.ID != channel {
		return "", false, ErrInvalid
	}
	input := a
	input.ActionID = ""
	canonical, err := platform.NewTwitchModerationIntent(source, intent.Key(), input)
	if err != nil || canonical.Action().ActionID != a.ActionID || canonical.Fingerprint() != intent.Fingerprint() || !canonical.Deadline().Equal(intent.Deadline()) {
		return "", false, ErrInvalid
	}
	var targetEvent *string
	targetActor := a.UserID
	if a.Capability == "message.delete" {
		var target string
		err = tx.QueryRowContext(ctx, `SELECT id,actor_id FROM normalized_chat_events WHERE /*tenant:normalized_chat_events*/ AND connection_id=$1 AND platform='twitch' AND channel_id=$2 AND envelope->'chat'->>'message_id'=$3 ORDER BY occurred_at,id LIMIT 1`, intent.ConnectionID(), a.ChannelID, a.MessageID).Scan(&target, &targetActor)
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, ErrInvalid
		}
		if err != nil {
			return "", false, err
		}
		targetEvent = &target
	}
	limits := map[string]int64{}
	if json.Unmarshal(raw, &limits) != nil {
		return "", false, ErrInvalid
	}
	limit, ok := limits["storedEvents"]
	if !ok || limit < -1 {
		return "", false, ErrInvalid
	}
	if limit == -1 || limit > 10000 {
		limit = 10000
	}
	var total, perEvent int64
	err = tx.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER(WHERE event_id=$1) FROM (
 SELECT event_id FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/ UNION ALL
 SELECT event_id FROM platform_moderation_actions WHERE /*tenant:platform_moderation_actions*/) actions`, intent.EventID()).Scan(&total, &perEvent)
	if err != nil {
		return "", false, err
	}
	if total >= limit || perEvent >= 16 {
		return "", false, tenantdb.ErrLimit
	}
	insert, err := tx.ExecContext(ctx, `INSERT INTO platform_moderation_actions(id,event_id,connection_id,action_key,channel_id,capability,user_id,message_id,reason,duration_ns,target_event_id,target_actor_id,payload_hash,deadline)
 SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14 WHERE $14::timestamptz>clock_timestamp()`, a.ActionID, intent.EventID(), intent.ConnectionID(), intent.Key(), a.ChannelID, a.Capability, a.UserID, a.MessageID, a.Reason, int64(a.Duration), targetEvent, targetActor, intent.Fingerprint(), intent.Deadline())
	if err != nil {
		return "", false, err
	}
	count, err := insert.RowsAffected()
	if err != nil {
		return "", false, err
	}
	if count != 1 {
		return "", false, ErrInvalid
	}
	if _, err = tx.EnqueueModeration(ctx, a.ActionID); err != nil {
		return "", false, err
	}
	return a.ActionID, true, tx.Commit()
}

// Claim contains private effect input and a private completion token. Neither
// belongs in jobs, exports or logs. Only a successful Start creates a claim.
type Claim struct {
	Action            platform.ModerationAction
	Deadline          time.Time
	id, tenant, token string
}

// Start commits the running fence before exposing the action. Concurrent or
// repeated starts, including after restart, cannot obtain another claim. The
// future executor must freshly authorize the effect after this durable fence.
func (s *Store) Start(ctx context.Context, id string) (Claim, bool, error) {
	if !validHash(id) {
		return Claim{}, false, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Claim{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	var tenant string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, s.db.TenantID()).Scan(&tenant); err != nil {
		return Claim{}, false, err
	}
	c := Claim{id: id, tenant: tenant}
	var state, connection string
	var expired bool
	err = tx.QueryRowContext(ctx, `SELECT state,connection_id,channel_id,capability,user_id,message_id,reason,duration_ns,deadline,deadline<=clock_timestamp()
 FROM platform_moderation_actions WHERE /*tenant:platform_moderation_actions*/ AND id=$1 FOR UPDATE`, id).Scan(&state, &connection, &c.Action.ChannelID, &c.Action.Capability, &c.Action.UserID, &c.Action.MessageID, &c.Action.Reason, &c.Action.Duration, &c.Deadline, &expired)
	if errors.Is(err, sql.ErrNoRows) {
		return Claim{}, false, ErrInvalid
	}
	if err != nil {
		return Claim{}, false, err
	}
	if state != "pending" {
		return Claim{}, false, tx.Commit()
	}
	c.Action.ActionID = id
	if c.Action.ValidateTwitch() != nil {
		return Claim{}, false, ErrInvalid
	}
	// Lock the current connection through commit so admission/start and
	// revocation have a definite ordering. This remains no effect permission.
	var channel string
	err = tx.QueryRowContext(ctx, `SELECT provider_user_id FROM platform_connections WHERE /*tenant:platform_connections*/ AND provider='twitch' AND provider_user_id=$1 AND revoked_at IS NULL FOR SHARE`, c.Action.ChannelID).Scan(&channel)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Claim{}, false, err
	}
	code := ""
	if expired {
		code = "expired"
	} else if errors.Is(err, sql.ErrNoRows) || connection != "twitch:"+channel {
		code = "connection_unavailable"
	}
	if code != "" {
		_, err = tx.ExecContext(ctx, `UPDATE platform_moderation_actions SET state='rejected',code=$1,failure='permanent',finished_at=clock_timestamp() WHERE /*tenant:platform_moderation_actions*/ AND id=$2`, code, id)
		if err != nil {
			return Claim{}, false, err
		}
		return Claim{}, false, tx.Commit()
	}
	var nonce [32]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return Claim{}, false, err
	}
	c.token = hex.EncodeToString(nonce[:])
	err = tx.QueryRowContext(ctx, `UPDATE platform_moderation_actions SET state='running',code='started',claim_hash=$1,started_at=clock_timestamp() WHERE /*tenant:platform_moderation_actions*/ AND id=$2 AND deadline>clock_timestamp() RETURNING deadline`, hash(c.token), id).Scan(&c.Deadline)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, `UPDATE platform_moderation_actions SET state='rejected',code='expired',failure='permanent',finished_at=clock_timestamp() WHERE /*tenant:platform_moderation_actions*/ AND id=$1`, id)
		if err != nil {
			return Claim{}, false, err
		}
		return Claim{}, false, tx.Commit()
	}
	if err != nil {
		return Claim{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return Claim{}, false, err
	}
	return c, true, nil
}

// Finish is a detached, five-second completion write. Failure never permits a
// second provider attempt: the running fence survives for terminal recovery.
func (s *Store) Finish(ctx context.Context, c Claim, r platform.ModerationReceipt) error {
	if c.tenant != s.db.TenantID() || !validHash(c.id) || !validHash(c.token) || c.Action.ActionID != c.id {
		return ErrFence
	}
	if r.Validate(c.id) != nil {
		return ErrInvalid
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	result, err := s.db.ExecContext(ctx, `UPDATE platform_moderation_actions SET state=$1,code=$1,failure=$2,retry_after_ns=$3,claim_hash=NULL,finished_at=clock_timestamp()
 WHERE /*tenant:platform_moderation_actions*/ AND id=$4 AND state='running' AND claim_hash=$5`, string(r.Status), string(r.Failure), int64(r.RetryAfter), c.id, hash(c.token))
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrFence
	}
	return nil
}

func (s *Store) RecoverExpired(ctx context.Context, limit int) (int64, error) {
	if limit < 1 || limit > 100 {
		return 0, ErrInvalid
	}
	r, err := s.db.ExecContext(ctx, `WITH expired AS MATERIALIZED (
 SELECT id FROM platform_moderation_actions WHERE /*tenant:platform_moderation_actions*/ AND state IN ('pending','running') AND deadline<=clock_timestamp() ORDER BY deadline,id LIMIT $1 FOR UPDATE SKIP LOCKED)
 UPDATE platform_moderation_actions SET state=CASE WHEN state='running' THEN 'uncertain' ELSE 'rejected' END,
 code=CASE WHEN state='running' THEN 'deadline_elapsed' ELSE 'expired' END,
 failure=CASE WHEN state='running' THEN '' ELSE 'permanent' END,claim_hash=NULL,finished_at=clock_timestamp()
 WHERE /*tenant:platform_moderation_actions*/ AND state IN ('pending','running') AND deadline<=clock_timestamp() AND id IN(SELECT id FROM expired)`, limit)
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}

type Receipt struct {
	ActionID     string               `json:"actionId"`
	EventID      string               `json:"eventId"`
	ConnectionID string               `json:"connectionId"`
	Capability   platform.Capability  `json:"capability"`
	State        string               `json:"state"`
	Code         string               `json:"code"`
	Failure      platform.FailureKind `json:"failure,omitempty"`
	RetryAfter   time.Duration        `json:"retryAfter,omitempty"`
	CreatedAt    time.Time            `json:"createdAt"`
	StartedAt    *time.Time           `json:"startedAt,omitempty"`
	FinishedAt   *time.Time           `json:"finishedAt,omitempty"`
}

func (s *Store) Receipts(ctx context.Context, limit int) ([]Receipt, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,event_id,connection_id,capability,state,code,failure,retry_after_ns,created_at,started_at,finished_at FROM platform_moderation_actions WHERE /*tenant:platform_moderation_actions*/ ORDER BY created_at DESC,id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []Receipt{}
	for rows.Next() {
		var r Receipt
		if err = rows.Scan(&r.ActionID, &r.EventID, &r.ConnectionID, &r.Capability, &r.State, &r.Code, &r.Failure, &r.RetryAfter, &r.CreatedAt, &r.StartedAt, &r.FinishedAt); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func validHash(value string) bool {
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
