// Package platformingest retains internal provider events and schedules their
// follow-up atomically. It exposes no public projection or provider effects.
package platformingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

var ErrInvalid = errors.New("invalid or unbound platform event")

type Store struct{ db *tenantdb.DB }

func New(db *tenantdb.DB) (*Store, error) {
	if db == nil || !db.IsScoped() {
		return nil, ErrInvalid
	}
	return &Store{db: db}, nil
}

// AcceptChat implements platform.EventSink when no native payload is retained.
func (s *Store) AcceptChat(ctx context.Context, event platform.ChatEvent) error {
	_, err := s.IngestChat(ctx, event, nil)
	return err
}

// IngestChat returns true only for the first committed delivery. Duplicate
// deliveries retain the original envelope and payload, never replacing them.
func (s *Store) IngestChat(ctx context.Context, event platform.ChatEvent, native json.RawMessage) (bool, error) {
	canonical, err := platform.NewChatEvent(platform.Connection{TenantID: event.TenantID, ID: event.ConnectionID, Platform: event.Platform}, event.NativeEventID, event.OccurredAt, event.ReceivedAt, event.Chat)
	if err != nil || event.TenantID != s.db.TenantID() || event.Version != platform.EventVersion || event.Kind != "chat_message" || event.ID != canonical.ID || event.IdempotencyKey != canonical.IdempotencyKey || event.TraceID != canonical.TraceID {
		return false, ErrInvalid
	}
	for _, v := range []string{event.ConnectionID, event.NativeEventID, event.Chat.Actor.ID, event.Chat.Actor.Login, event.Chat.Channel.ID, event.Platform} {
		if len(v) > 256 {
			return false, ErrInvalid
		}
	}
	raw, err := json.Marshal(canonical)
	if err != nil || len(raw) > 65536 {
		return false, ErrInvalid
	}
	if len(native) > 65536 {
		return false, ErrInvalid
	}
	if len(native) > 0 {
		var object map[string]json.RawMessage
		if json.Unmarshal(native, &object) != nil || object == nil {
			return false, ErrInvalid
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	// Serialize admission with plan changes and concurrent ingest for this tenant.
	var days int
	var limitsRaw []byte
	if err = tx.QueryRowContext(ctx, `SELECT t.retention_days,p.limits FROM tenants t JOIN plans p ON p.id=t.plan_id WHERE t.id=$1 FOR UPDATE OF t`, s.db.TenantID()).Scan(&days, &limitsRaw); err != nil {
		return false, err
	}
	var duplicate bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM normalized_chat_events WHERE /*tenant:normalized_chat_events*/ AND connection_id=$1 AND idempotency_key=$2)`, event.ConnectionID, event.IdempotencyKey).Scan(&duplicate); err != nil {
		return false, err
	}
	if duplicate {
		return false, tx.Commit()
	}
	now := time.Now().UTC()
	if event.OccurredAt.Before(now.Add(-time.Duration(days)*24*time.Hour)) || event.ReceivedAt.After(now.Add(time.Minute)) || event.ReceivedAt.Before(event.OccurredAt.Add(-5*time.Minute)) {
		return false, ErrInvalid
	}
	var channel string
	err = tx.QueryRowContext(ctx, `SELECT provider_user_id FROM platform_connections WHERE /*tenant:platform_connections*/ AND provider=$1 AND revoked_at IS NULL FOR SHARE`, event.Platform).Scan(&channel)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (channel != event.Chat.Channel.ID || event.ConnectionID != event.Platform+":"+channel) {
		return false, ErrInvalid
	}
	if err != nil {
		return false, err
	}
	limits := map[string]int64{}
	if json.Unmarshal(limitsRaw, &limits) != nil {
		return false, ErrInvalid
	}
	limit, ok := limits["storedEvents"]
	if !ok || limit < -1 {
		return false, ErrInvalid
	}
	if limit == -1 || limit > 250000 {
		limit = 250000
	}
	var count int64
	if err = tx.QueryRowContext(ctx, retainedEventCount).Scan(&count); err != nil {
		return false, err
	}
	if count >= limit {
		return false, tenantdb.ErrLimit
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO normalized_chat_events(id,connection_id,idempotency_key,platform,actor_id,actor_login,channel_id,occurred_at,received_at,envelope) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, event.ID, event.ConnectionID, event.IdempotencyKey, event.Platform, event.Chat.Actor.ID, strings.ToLower(event.Chat.Actor.Login), event.Chat.Channel.ID, event.OccurredAt, event.ReceivedAt, raw)
	if err != nil {
		return false, err
	}
	if len(native) > 0 {
		if _, err = tx.ExecContext(ctx, `INSERT INTO platform_event_payloads(event_id,payload) VALUES($1,$2)`, event.ID, native); err != nil {
			return false, err
		}
	}
	if _, err = tx.EnqueueChatDispatch(ctx, event.ConnectionID, event.ID); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
