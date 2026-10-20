// Package platformstreamdispatch provides an explicit fenced stream consumer.
// It has no application startup or provider subscription registration.
package platformstreamdispatch

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformqueue"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
)

type Handler func(context.Context, platform.StreamEvent) error

// Prepared binds resources to their owner lifecycle. Capabilities must obtain a
// fresh report each time; the handler must independently authorize its effects.
// Any returned Release is called once, including partial preparation failure.
type Prepared struct {
	Context      context.Context
	Handle       Handler
	Capabilities func(context.Context, platform.StreamEvent) (platform.CapabilityReport, error)
	Release      func()
}

// Preparation initializes resources only; it must not publish or execute effects.
// It must respect the bounded job context and return the owner's lifetime context.
type Preparation func(context.Context, platform.StreamEvent) (Prepared, error)

type Worker struct {
	river.WorkerDefaults[platform.StreamDispatchArgs]
	product *tenantdb.DB
	prepare Preparation
}

var eventID = regexp.MustCompile(`^[0-9a-f]{64}$`)

func NewWorker(product *tenantdb.DB, prepare Preparation) (*Worker, error) {
	if product == nil || !product.IsScoped() || prepare == nil {
		return nil, errors.New("stream consumer requires scoped product storage and preparation")
	}
	return &Worker{product: product, prepare: prepare}, nil
}

// NewControlledClient borrows both pools. The caller must stop the client before
// closing either pool. active gates leases only, not effect authorization or
// already leased work; Prepared.Context supplies owner cancellation for those.
func NewControlledClient(ctx context.Context, service *sql.DB, product *tenantdb.DB, prepare Preparation, active func() bool) (*river.Client[*sql.Tx], error) {
	w, err := NewWorker(product, prepare)
	if err != nil || service == nil || active == nil {
		return nil, errors.New("stream consumer configuration invalid")
	}
	schema, err := platformqueue.Validate(ctx, service, product, "overlaid_stream_dispatch")
	if err != nil {
		return nil, err
	}
	workers := river.NewWorkers()
	river.AddWorker(workers, w)
	return river.NewClient(platformqueue.Gate(riverdatabasesql.New(service), active), &river.Config{Schema: schema, PollOnly: true, Workers: workers, Queues: map[string]river.QueueConfig{"stream_followup": {MaxWorkers: 4}}, JobTimeout: 40 * time.Second})
}

func terminal(code string) error { return river.JobCancel(errors.New(code)) }

func (w *Worker) Work(ctx context.Context, job *river.Job[platform.StreamDispatchArgs]) (result error) {
	if job == nil || job.JobRow == nil || job.Kind != "overlaid_stream_dispatch" || job.Queue != "stream_followup" || job.Priority != 4 || job.MaxAttempts != 3 || job.Attempt < 1 || job.Attempt > 3 {
		return terminal("stream_invalid_job")
	}
	a := job.Args
	if !tenantdb.ValidID(a.TenantID) || a.TenantID != strings.ToLower(a.TenantID) || !eventID.MatchString(a.EventID) || a.ConnectionID == "" || len(a.ConnectionID) > 256 || a.Deadline <= 0 || a.Deadline > 999999999999 {
		return terminal("stream_invalid_job")
	}
	db, err := w.product.Scoped(a.TenantID)
	if err != nil {
		return terminal("stream_invalid_job")
	}
	// Local deadlines bound trusted callbacks. Database time remains authoritative
	// for source eligibility after admission locks.
	deadline := time.Now().Add(30 * time.Second)
	if original := time.Unix(a.Deadline, 0); original.Before(deadline) {
		deadline = original
	}
	runCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	inspectCtx, stopInspect := context.WithTimeout(ctx, 5*time.Second)
	event, prior, code, err := inspect(inspectCtx, db, a)
	stopInspect()
	if err != nil {
		return errors.New("stream_prepare_unavailable")
	}
	if code == "event_missing" || code == "invalid_job" {
		return terminal("stream_" + code)
	}
	prepared := Prepared{}
	released := false
	fenced := false
	release := func() bool {
		if released || prepared.Release == nil {
			return true
		}
		released = true
		return safeRelease(prepared.Release)
	}
	defer func() {
		if !release() {
			if fenced {
				result = terminal("stream_owner_release_failed")
			} else {
				result = errors.New("stream_prepare_unavailable")
			}
		}
	}()
	ready, denied := false, false
	if !prior && code == "" {
		if runCtx.Err() != nil {
			return errors.New("stream_owner_unavailable")
		}
		prepared, err = safePrepare(w.prepare, runCtx, snapshot(event))
		if err != nil || prepared.Context == nil || prepared.Handle == nil || prepared.Capabilities == nil || prepared.Release == nil {
			return errors.New("stream_prepare_unavailable")
		}
		stopOwner := context.AfterFunc(prepared.Context, cancel)
		defer stopOwner()
		if prepared.Context.Err() != nil || runCtx.Err() != nil {
			return errors.New("stream_owner_unavailable")
		}
		allowed, authErr := authorize(runCtx, prepared, event)
		if authErr != nil {
			return errors.New("stream_capability_unavailable")
		}
		ready, denied = true, !allowed
	}
	if ready && prepared.Context.Err() != nil {
		return errors.New("stream_owner_unavailable")
	}
	// Use the job context for terminal source/receipt bookkeeping even when the
	// original callback deadline is already over. No handler can start unprepared.
	claimCtx, stopClaim := context.WithTimeout(ctx, 5*time.Second)
	event, claim, code, err := start(claimCtx, db, a, ready, denied, runCtx)
	stopClaim()
	if err != nil {
		return errors.New("stream_prepare_unavailable")
	}
	fenced = true
	if code != "" {
		if code == "completed" {
			return nil
		}
		return terminal("stream_" + code)
	}
	state, finishCode := "completed", "completed"
	if runCtx.Err() != nil || prepared.Context.Err() != nil {
		state, finishCode = "uncertain", "handler_cancelled"
	} else if allowed, err := authorize(runCtx, prepared, event); err != nil || !allowed {
		state, finishCode = "failed", "capability_unavailable"
	} else if ok, err := currentSource(runCtx, db, a); err != nil || !ok {
		state, finishCode = "failed", "connection_unavailable"
	} else if runCtx.Err() != nil || prepared.Context.Err() != nil {
		state, finishCode = "uncertain", "handler_cancelled"
	} else if err := invoke(runCtx, prepared.Handle, event); err != nil {
		state, finishCode = "failed", "handler_failed"
	}
	if runCtx.Err() != nil || prepared.Context.Err() != nil {
		state, finishCode = "uncertain", "handler_cancelled"
	}
	if !release() {
		state, finishCode = "uncertain", "owner_release_failed"
	}
	finishCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	r, err := db.ExecContext(finishCtx, `UPDATE platform_stream_dispatch_receipts SET state=$1,code=$2,finished_at=clock_timestamp() WHERE /*tenant:platform_stream_dispatch_receipts*/ AND event_id=$3 AND claim_hash=$4 AND state='running'`, state, finishCode, a.EventID, claim)
	if err != nil {
		return terminal("stream_completion_unavailable")
	}
	n, err := r.RowsAffected()
	if err != nil || n != 1 {
		return terminal("stream_completion_fenced")
	}
	if state != "completed" {
		return terminal("stream_" + finishCode)
	}
	return nil
}

func (w *Worker) NextRetry(job *river.Job[platform.StreamDispatchArgs]) time.Time {
	retry := time.Now().Add(time.Second)
	if job != nil && time.Unix(job.Args.Deadline, 0).Before(retry) {
		return time.Unix(job.Args.Deadline, 0)
	}
	return retry
}

func safePrepare(fn Preparation, ctx context.Context, event platform.StreamEvent) (p Prepared, err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("stream preparation failed")
		}
	}()
	return fn(ctx, event)
}

func safeRelease(fn func()) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	fn()
	return true
}

func authorize(ctx context.Context, p Prepared, event platform.StreamEvent) (ok bool, err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("stream capability check failed")
		}
	}()
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if p.Context.Err() != nil {
		return false, p.Context.Err()
	}
	capability, known := platform.StreamReadCapability(event.Kind)
	if !known {
		return false, nil
	}
	report, err := p.Capabilities(ctx, snapshot(event))
	return err == nil && report.Check(capability).Available, err
}

func invoke(ctx context.Context, handler Handler, event platform.StreamEvent) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("stream handler failed")
		}
	}()
	return handler(ctx, snapshot(event))
}

type source struct {
	raw                            []byte
	provider, kind, channel, login string
	occurred, received             time.Time
	deadline                       int64
}

const sourceQuery = `SELECT envelope,platform,kind,channel_id,channel_login,occurred_at,received_at,dispatch_deadline FROM normalized_stream_events WHERE /*tenant:normalized_stream_events*/ AND id=$1 AND connection_id=$2`

func read(row tenantdb.Row) (source, error) {
	var s source
	err := row.Scan(&s.raw, &s.provider, &s.kind, &s.channel, &s.login, &s.occurred, &s.received, &s.deadline)
	return s, err
}

func canonical(s source, a platform.StreamDispatchArgs) (platform.StreamEvent, string) {
	if s.deadline != a.Deadline {
		return platform.StreamEvent{}, "invalid_job"
	}
	var e platform.StreamEvent
	d := json.NewDecoder(bytes.NewReader(s.raw))
	d.DisallowUnknownFields()
	if len(s.raw) > 65536 || d.Decode(&e) != nil || d.Decode(new(any)) != io.EOF {
		return e, "invalid_event"
	}
	c, err := platform.NewStreamEvent(platform.Connection{TenantID: e.TenantID, ID: e.ConnectionID, Platform: e.Platform}, e.NativeEventID, e.Kind, e.OccurredAt, e.ReceivedAt, e.Channel, e.Online, e.Offline, e.Update)
	raw, rawErr := json.Marshal(e)
	expected, expectedErr := json.Marshal(c)
	if err != nil || rawErr != nil || expectedErr != nil || !bytes.Equal(raw, expected) || e.TenantID != a.TenantID || e.ID != a.EventID || e.ConnectionID != a.ConnectionID || e.Platform != "twitch" || s.provider != e.Platform || s.kind != string(e.Kind) || s.channel != e.Channel.ID || s.login != strings.ToLower(e.Channel.Login) || !s.occurred.Equal(e.OccurredAt.Truncate(time.Microsecond)) || !s.received.Equal(e.ReceivedAt.Truncate(time.Microsecond)) || a.Deadline != e.OccurredAt.Unix()+120 {
		return e, "invalid_event"
	}
	return e, ""
}

func inspect(ctx context.Context, db *tenantdb.DB, a platform.StreamDispatchArgs) (platform.StreamEvent, bool, string, error) {
	s, err := read(db.QueryRowContext(ctx, sourceQuery, a.EventID, a.ConnectionID))
	if errors.Is(err, sql.ErrNoRows) {
		return platform.StreamEvent{}, false, "event_missing", nil
	}
	if err != nil {
		return platform.StreamEvent{}, false, "", err
	}
	e, code := canonical(s, a)
	if code != "" {
		return e, false, code, nil
	}
	var prior, active bool
	var now time.Time
	err = db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM platform_stream_dispatch_receipts WHERE /*tenant:platform_stream_dispatch_receipts*/ AND event_id=$1),EXISTS(SELECT 1 FROM platform_connections WHERE /*tenant:platform_connections*/ AND provider='twitch' AND provider_user_id=$2 AND revoked_at IS NULL),clock_timestamp()`, a.EventID, e.Channel.ID).Scan(&prior, &active, &now)
	if err != nil {
		return e, false, "", err
	}
	if now.Unix() >= a.Deadline {
		code = "expired"
	} else if !active {
		code = "connection_unavailable"
	}
	return e, prior, code, nil
}

func start(ctx context.Context, db *tenantdb.DB, a platform.StreamDispatchArgs, ready, denied bool, owner context.Context) (platform.StreamEvent, string, string, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return platform.StreamEvent{}, "", "", err
	}
	defer func() { _ = tx.Rollback() }()
	// Connection before source matches deletion/reauthorization lock order.
	var channel string
	var revoked bool
	err = tx.QueryRowContext(ctx, `SELECT provider_user_id,revoked_at IS NOT NULL FROM platform_connections WHERE /*tenant:platform_connections*/ AND provider='twitch' FOR SHARE`).Scan(&channel, &revoked)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return platform.StreamEvent{}, "", "", err
	}
	active := err == nil && !revoked
	s, err := read(tx.QueryRowContext(ctx, sourceQuery+` FOR SHARE`, a.EventID, a.ConnectionID))
	if errors.Is(err, sql.ErrNoRows) {
		return platform.StreamEvent{}, "", "event_missing", nil
	}
	if err != nil {
		return platform.StreamEvent{}, "", "", err
	}
	e, code := canonical(s, a)
	if code == "invalid_job" {
		return e, "", code, nil
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "stream:"+a.TenantID+":"+a.EventID); err != nil {
		return e, "", "", err
	}
	var prior string
	err = tx.QueryRowContext(ctx, `SELECT state FROM platform_stream_dispatch_receipts WHERE /*tenant:platform_stream_dispatch_receipts*/ AND event_id=$1 FOR UPDATE`, a.EventID).Scan(&prior)
	if err == nil {
		switch prior {
		case "running", "completed", "failed", "uncertain", "skipped":
		default:
			return e, "", "invalid_receipt", nil
		}
		if prior == "running" {
			if _, err = tx.ExecContext(ctx, `UPDATE platform_stream_dispatch_receipts SET state='uncertain',code='prior_start',finished_at=clock_timestamp() WHERE /*tenant:platform_stream_dispatch_receipts*/ AND event_id=$1 AND state='running'`, a.EventID); err != nil {
				return e, "", "", err
			}
			prior = "prior_start"
		}
		return e, "", prior, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return e, "", "", err
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return e, "", "", err
	}
	if code == "" {
		if now.Unix() >= a.Deadline {
			code = "expired"
		} else if !active || channel != e.Channel.ID || a.ConnectionID != "twitch:"+channel {
			code = "connection_unavailable"
		} else if denied {
			code = "capability_unavailable"
		}
	}
	if code == "" && (!ready || owner.Err() != nil) {
		return e, "", "", errors.New("stream owner not prepared")
	}
	var nonce [32]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return e, "", "", err
	}
	hash := sha256.Sum256(nonce[:])
	claim := hex.EncodeToString(hash[:])
	state, receiptCode := "running", "started"
	if code != "" {
		state, receiptCode = "skipped", code
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO platform_stream_dispatch_receipts(event_id,connection_id,deadline,state,code,claim_hash,finished_at) VALUES($1,$2,$3,$4,$5,$6,CASE WHEN $4='skipped' THEN clock_timestamp() END)`, a.EventID, a.ConnectionID, a.Deadline, state, receiptCode, claim)
	if err != nil {
		return e, "", "", err
	}
	if err = tx.Commit(); err != nil {
		return e, "", "", err
	}
	return e, claim, code, nil
}

func currentSource(ctx context.Context, db *tenantdb.DB, a platform.StreamDispatchArgs) (bool, error) {
	var active bool
	err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM normalized_stream_events e JOIN platform_connections c ON c.tenant_id=e.tenant_id AND c.provider=e.platform AND c.provider_user_id=e.channel_id WHERE e.tenant_id=$1 AND e.id=$2 AND e.connection_id=$3 AND e.dispatch_deadline=$4 AND c.revoked_at IS NULL AND clock_timestamp()<to_timestamp($4))`, a.TenantID, a.EventID, a.ConnectionID, a.Deadline).Scan(&active)
	return active, err
}

func snapshot(e platform.StreamEvent) platform.StreamEvent {
	c, _ := platform.NewStreamEvent(platform.Connection{TenantID: e.TenantID, ID: e.ConnectionID, Platform: e.Platform}, e.NativeEventID, e.Kind, e.OccurredAt, e.ReceivedAt, e.Channel, e.Online, e.Offline, e.Update)
	return c
}
