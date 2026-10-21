package tenantdb

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
)

var (
	ErrStreamInvalid  = errors.New("invalid or unbound stream admission")
	ErrStreamConflict = errors.New("retained stream conflicts with original input")
)

// MaxStreamFanout bounds one native delivery's configured workspace targets.
const MaxStreamFanout = 16

// StreamAdmission binds private input to one immutable validated product scope.
// It grants no permission; callers must hold every local owner lease until return.
type StreamAdmission struct {
	Scope  *DB
	Event  platform.StreamEvent
	Native json.RawMessage
}

type streamInput struct {
	scope       *DB
	event       platform.StreamEvent
	raw, native []byte
	days        int
	limits      []byte
	index       int
}

// SharesPool reports identity of a borrowed pool, without exposing it or allowing
// a caller to mutate a scope. Separate pools are not transaction-compatible.
func (d *DB) SharesPool(other *DB) bool {
	return d != nil && other != nil && d.raw != nil && d.raw == other.raw && d.IsScoped() && other.IsScoped()
}

// AdmitStreamFanout commits one native delivery for all configured scopes or
// none. Only this typed bridge switches transaction-local tenant context; no
// mutable scope or generic cross-tenant transaction is exposed. Results follow
// input order; errors return no partial results, including uncertain commits.
func AdmitStreamFanout(ctx context.Context, targets []StreamAdmission) ([]bool, error) {
	if ctx == nil || len(targets) == 0 || len(targets) > MaxStreamFanout {
		return nil, ErrStreamInvalid
	}
	inputs := make([]streamInput, len(targets))
	seen := make(map[string]bool, len(targets))
	var reference []byte
	for i, target := range targets {
		if !target.Scope.SharesPool(targets[0].Scope) || seen[target.Scope.TenantID()] {
			return nil, ErrStreamInvalid
		}
		seen[target.Scope.TenantID()] = true
		input, err := validateStreamInput(target)
		if err != nil {
			return nil, err
		}
		// Reconstruct under the first trusted scope to compare the complete delivery,
		// excluding only the per-tenant canonical identity derived by the constructor.
		event := input.event
		shared, err := platform.NewStreamEvent(platform.Connection{TenantID: targets[0].Scope.TenantID(), ID: event.ConnectionID, Platform: event.Platform}, event.NativeEventID, event.Kind, event.OccurredAt, event.ReceivedAt, event.Channel, event.Online, event.Offline, event.Update)
		if err != nil {
			return nil, ErrStreamInvalid
		}
		raw, err := json.Marshal(shared)
		if err != nil {
			return nil, ErrStreamInvalid
		}
		if i == 0 {
			reference = raw
		} else if !bytes.Equal(reference, raw) || !bytes.Equal(inputs[0].native, input.native) {
			return nil, ErrStreamInvalid
		}
		input.index = i
		inputs[i] = input
	}
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].scope.TenantID() < inputs[j].scope.TenantID() })
	tx, err := inputs[0].scope.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	// Lock every tenant in the same order used by all fanouts, before any write or
	// connection lock. Chat and single-stream admission use the same tenant fence.
	for i := range inputs {
		input := &inputs[i]
		view, err := streamScope(ctx, tx, input.scope)
		if err != nil {
			return nil, err
		}
		if err = view.QueryRowContext(ctx, `SELECT t.retention_days,p.limits FROM tenants t JOIN plans p ON p.id=t.plan_id WHERE t.id=$1 FOR UPDATE OF t`, input.scope.TenantID()).Scan(&input.days, &input.limits); err != nil {
			return nil, err
		}
	}
	for _, input := range inputs {
		view, err := streamScope(ctx, tx, input.scope)
		if err != nil {
			return nil, err
		}
		if err = lockStreamConnection(ctx, view, input.event); err != nil {
			return nil, err
		}
	}
	// Eligibility follows every admission lock, using the database's actual clock.
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}
	results := make([]bool, len(inputs))
	for _, input := range inputs {
		view, err := streamScope(ctx, tx, input.scope)
		if err != nil {
			return nil, err
		}
		added, err := retainStreamInput(ctx, view, input, now)
		if err != nil {
			return nil, err
		}
		results[input.index] = added
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return results, nil
}

func streamScope(ctx context.Context, tx *Tx, scope *DB) (*Tx, error) {
	if _, err := tx.raw.ExecContext(ctx, `SELECT set_config('app.tenant_id', $1, true)`, scope.tenant); err != nil {
		return nil, err
	}
	return &Tx{raw: tx.raw, db: scope}, nil
}

func validateStreamInput(input StreamAdmission) (streamInput, error) {
	event, native := input.Event, input.Native
	canonical, err := platform.NewStreamEvent(platform.Connection{TenantID: event.TenantID, ID: event.ConnectionID, Platform: event.Platform}, event.NativeEventID, event.Kind, event.OccurredAt, event.ReceivedAt, event.Channel, event.Online, event.Offline, event.Update)
	if err != nil || event.TenantID != input.Scope.TenantID() || event.Platform != "twitch" {
		return streamInput{}, ErrStreamInvalid
	}
	raw, err := json.Marshal(canonical)
	original, originalErr := json.Marshal(event)
	if err != nil || originalErr != nil || string(raw) != string(original) || len(raw) > 65536 {
		return streamInput{}, ErrStreamInvalid
	}
	for _, v := range []string{event.ConnectionID, event.NativeEventID, event.Channel.ID, event.Channel.Login} {
		if len(v) > 256 {
			return streamInput{}, ErrStreamInvalid
		}
	}
	if len(native) > 65536 {
		return streamInput{}, ErrStreamInvalid
	}
	if len(native) > 0 {
		var object map[string]json.RawMessage
		if json.Unmarshal(native, &object) != nil || object == nil {
			return streamInput{}, ErrStreamInvalid
		}
	}

	// Own the validated bytes, including nested event data through its envelope.
	var owned platform.StreamEvent
	if json.Unmarshal(raw, &owned) != nil {
		return streamInput{}, ErrStreamInvalid
	}
	return streamInput{scope: input.Scope, event: owned, raw: raw, native: append([]byte(nil), native...)}, nil
}

func lockStreamConnection(ctx context.Context, tx *Tx, event platform.StreamEvent) error {
	var channel string
	err := tx.QueryRowContext(ctx, `SELECT provider_user_id FROM platform_connections WHERE /*tenant:platform_connections*/ AND provider=$1 AND revoked_at IS NULL FOR SHARE`, event.Platform).Scan(&channel)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (channel != event.Channel.ID || event.ConnectionID != event.Platform+":"+channel) {
		return ErrStreamInvalid
	}
	if err != nil {
		return err
	}

	return nil
}

func retainStreamInput(ctx context.Context, tx *Tx, input streamInput, now time.Time) (bool, error) {
	event, raw, native, days, limitsRaw := input.event, input.raw, input.native, input.days, input.limits
	var err error
	if event.OccurredAt.Before(now.Add(-time.Duration(days)*24*time.Hour)) || event.OccurredAt.After(now.Add(time.Minute)) || event.ReceivedAt.After(now.Add(time.Minute)) || event.ReceivedAt.Before(event.OccurredAt.Add(-5*time.Minute)) {
		return false, ErrStreamInvalid
	}
	var equal bool
	err = tx.QueryRowContext(ctx, `SELECT envelope-'received_at'=$2::jsonb-'received_at' FROM normalized_stream_events WHERE /*tenant:normalized_stream_events*/ AND id=$1 AND connection_id=$3`, event.ID, raw, event.ConnectionID).Scan(&equal)
	if err == nil {
		if !equal {
			return false, ErrStreamConflict
		}
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	limits := map[string]int64{}
	if json.Unmarshal(limitsRaw, &limits) != nil {
		return false, ErrStreamInvalid
	}
	limit, ok := limits["storedEvents"]
	if !ok || limit < -1 {
		return false, ErrStreamInvalid
	}
	if limit == -1 || limit > 250000 {
		limit = 250000
	}
	var count int64
	if err = tx.QueryRowContext(ctx, streamRetainedCount).Scan(&count); err != nil {
		return false, err
	}
	if count >= limit {
		return false, ErrLimit
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

	return true, nil
}

// Keep this combined bound aligned with chat admission's tenant-row fence.
const streamRetainedCount = `SELECT (SELECT count(*) FROM normalized_chat_events WHERE /*tenant:normalized_chat_events*/)+(SELECT count(*) FROM normalized_stream_events WHERE /*tenant:normalized_stream_events*/)`
