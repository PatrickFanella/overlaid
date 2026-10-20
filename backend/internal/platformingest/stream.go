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

var ErrConflict = errors.New("retained platform event conflicts with original input")

// IngestStream atomically retains one typed stream event, its optional private
// native payload and one separate follow-up job. No current worker leases this
// kind. A duplicate never replaces content, creates another job or renews time.
func (s *Store) IngestStream(ctx context.Context, event platform.StreamEvent, native json.RawMessage) (bool, error) {
	canonical, err := platform.NewStreamEvent(platform.Connection{TenantID: event.TenantID, ID: event.ConnectionID, Platform: event.Platform}, event.NativeEventID, event.Kind, event.OccurredAt, event.ReceivedAt, event.Channel, event.Online, event.Offline, event.Update)
	if err != nil || event.TenantID != s.db.TenantID() || event.Platform != "twitch" {
		return false, ErrInvalid
	}
	raw, err := json.Marshal(canonical)
	original, originalErr := json.Marshal(event)
	if err != nil || originalErr != nil || string(raw) != string(original) || len(raw) > 65536 {
		return false, ErrInvalid
	}
	for _, v := range []string{event.ConnectionID, event.NativeEventID, event.Channel.ID, event.Channel.Login} {
		if len(v) > 256 {
			return false, ErrInvalid
		}
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
	defer func() { _ = tx.Rollback() }()
	var days int
	var limitsRaw []byte
	if err = tx.QueryRowContext(ctx, `SELECT t.retention_days,p.limits FROM tenants t JOIN plans p ON p.id=t.plan_id WHERE t.id=$1 FOR UPDATE OF t`, s.db.TenantID()).Scan(&days, &limitsRaw); err != nil {
		return false, err
	}
	var channel string
	err = tx.QueryRowContext(ctx, `SELECT provider_user_id FROM platform_connections WHERE /*tenant:platform_connections*/ AND provider=$1 AND revoked_at IS NULL FOR SHARE`, event.Platform).Scan(&channel)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (channel != event.Channel.ID || event.ConnectionID != event.Platform+":"+channel) {
		return false, ErrInvalid
	}
	if err != nil {
		return false, err
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return false, err
	}
	if event.OccurredAt.Before(now.Add(-time.Duration(days)*24*time.Hour)) || event.OccurredAt.After(now.Add(time.Minute)) || event.ReceivedAt.After(now.Add(time.Minute)) || event.ReceivedAt.Before(event.OccurredAt.Add(-5*time.Minute)) {
		return false, ErrInvalid
	}
	var equal bool
	err = tx.QueryRowContext(ctx, `SELECT envelope-'received_at'=$2::jsonb-'received_at' FROM normalized_stream_events WHERE /*tenant:normalized_stream_events*/ AND id=$1 AND connection_id=$3`, event.ID, raw, event.ConnectionID).Scan(&equal)
	if err == nil {
		if !equal {
			return false, ErrConflict
		}
		return false, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
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
	_, err = tx.ExecContext(ctx, `INSERT INTO normalized_stream_events(id,connection_id,idempotency_key,platform,kind,channel_id,channel_login,occurred_at,received_at,dispatch_deadline,envelope) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, event.ID, event.ConnectionID, event.IdempotencyKey, event.Platform, event.Kind, event.Channel.ID, strings.ToLower(event.Channel.Login), event.OccurredAt, event.ReceivedAt, event.OccurredAt.Unix()+120, raw)
	if err != nil {
		return false, err
	}
	if len(native) > 0 {
		if _, err = tx.ExecContext(ctx, `INSERT INTO platform_stream_payloads(event_id,payload) VALUES($1,$2)`, event.ID, native); err != nil {
			return false, err
		}
	}
	if _, err = tx.EnqueueStreamDispatch(ctx, event.ConnectionID, event.ID); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// Both kinds serialize on the tenant row and charge this combined admission
// count. Existing display history retains its independent trimming convention.
const retainedEventCount = `SELECT (SELECT count(*) FROM normalized_chat_events WHERE /*tenant:normalized_chat_events*/)+(SELECT count(*) FROM normalized_stream_events WHERE /*tenant:normalized_stream_events*/)`
