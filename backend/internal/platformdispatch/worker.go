// Package platformdispatch supplies the fenced consumer boundary. Application
// startup selects this worker explicitly for retained Twitch ingestion.
package platformdispatch

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformqueue"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
)

type Handler func(context.Context, platform.ChatEvent) error

// Preparation initializes trusted runtime resources before the handler fence.
// It must not execute commands or grant provider permission.
type Preparation func(context.Context, string) (Handler, error)

type Worker struct {
	river.WorkerDefaults[platform.ChatDispatchArgs]
	product *tenantdb.DB
	handle  Handler
	prepare Preparation
}

var eventID = regexp.MustCompile(`^[0-9a-f]{64}$`)

func NewWorker(product *tenantdb.DB, handler Handler) (*Worker, error) {
	if product == nil || !product.IsScoped() || handler == nil {
		return nil, errors.New("scoped product database and handler required")
	}
	return &Worker{product: product, handle: handler}, nil
}

// NewClient uses a separate service pool for global River leasing. It verifies
// that this role cannot directly read product rows and targets the same database
// and schema as the immutable product pool. The caller owns pool lifetimes.
func NewClient(ctx context.Context, service *sql.DB, product *tenantdb.DB, handler Handler) (*river.Client[*sql.Tx], error) {
	return newClient(ctx, service, product, handler, nil, nil)
}

// NewControlledClient delays leasing until the owning controller activates it.
func NewControlledClient(ctx context.Context, service *sql.DB, product *tenantdb.DB, handler Handler, active func() bool) (*river.Client[*sql.Tx], error) {
	if active == nil {
		return nil, errors.New("chat worker activation gate required")
	}
	return newClient(ctx, service, product, handler, active, nil)
}

// NewPreparedClient adds pre-handler runtime preparation to controlled leasing.
// Existing receipts and missing/expired sources retain their terminal path.
func NewPreparedClient(ctx context.Context, service *sql.DB, product *tenantdb.DB, handler Handler, active func() bool, preparation Preparation) (*river.Client[*sql.Tx], error) {
	if active == nil || preparation == nil {
		return nil, errors.New("chat worker preparation and activation gate required")
	}
	return newClient(ctx, service, product, handler, active, preparation)
}

func newClient(ctx context.Context, service *sql.DB, product *tenantdb.DB, handler Handler, active func() bool, preparation Preparation) (*river.Client[*sql.Tx], error) {
	w, err := NewWorker(product, handler)
	if err != nil || service == nil {
		return nil, errors.New("chat worker configuration invalid")
	}
	w.prepare = preparation
	serviceSchema, err := platformqueue.Validate(ctx, service, product, "overlaid_chat_dispatch")
	if err != nil {
		return nil, err
	}
	workers := river.NewWorkers()
	river.AddWorker(workers, w)
	var driver riverdriver.Driver[*sql.Tx] = riverdatabasesql.New(service)
	if active != nil {
		driver = platformqueue.Gate(driver, active)
	}
	return river.NewClient(driver, &river.Config{Schema: serviceSchema, PollOnly: true, Queues: map[string]river.QueueConfig{"ingest_followup": {MaxWorkers: 4}}, Workers: workers, JobTimeout: 30 * time.Second})
}

func cancel(code string) error { return river.JobCancel(errors.New(code)) }

func (w *Worker) Work(ctx context.Context, job *river.Job[platform.ChatDispatchArgs]) error {
	if job == nil {
		return cancel("invalid_job")
	}
	a := job.Args
	if !tenantdb.ValidID(a.TenantID) || a.TenantID != strings.ToLower(a.TenantID) || !eventID.MatchString(a.EventID) || a.ConnectionID == "" || len(a.ConnectionID) > 256 {
		return cancel("invalid_job")
	}
	db, err := w.product.Scoped(a.TenantID)
	if err != nil {
		return cancel("invalid_job")
	}
	handler, prepared := w.handle, w.prepare == nil
	if w.prepare != nil && time.Now().Unix() < a.Deadline {
		var needsRuntime bool
		err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM normalized_chat_events WHERE /*tenant:normalized_chat_events*/ AND id=$1 AND connection_id=$2)
 AND NOT EXISTS(SELECT 1 FROM platform_dispatch_receipts WHERE /*tenant:platform_dispatch_receipts*/ AND event_id=$1)`, a.EventID, a.ConnectionID).Scan(&needsRuntime)
		if err != nil {
			return errors.New("dispatch_prepare_unavailable")
		}
		if needsRuntime {
			handler, err = w.prepare(ctx, a.TenantID)
			if err != nil || handler == nil {
				return errors.New("dispatch_runtime_unavailable")
			}
			prepared = true
		}
	}
	event, token, terminal, err := prepare(ctx, db, a, prepared)
	if err != nil {
		return errors.New("dispatch_prepare_unavailable")
	}
	if terminal != "" {
		if terminal == "completed" {
			return nil
		}
		return cancel(terminal)
	}
	handlerErr := w.run(ctx, event, handler)
	state, code := "completed", "completed"
	if ctx.Err() != nil {
		state, code = "uncertain", "handler_cancelled"
	} else if handlerErr != nil {
		state, code = "failed", "handler_failed"
	}
	// Cancellation must not prevent the bounded final receipt attempt. Any
	// infrastructure failure after start leaves the durable fence in place.
	finishCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	result, err := db.ExecContext(finishCtx, `UPDATE platform_dispatch_receipts SET state=$1,code=$2,finished_at=now() WHERE /*tenant:platform_dispatch_receipts*/ AND event_id=$3 AND claim_token=$4 AND state='running'`, state, code, a.EventID, token)
	if err != nil {
		return errors.New("dispatch_completion_unavailable")
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		return cancel("completion_fenced")
	}
	if state != "completed" {
		return cancel(code)
	}
	return nil
}

func (w *Worker) run(ctx context.Context, event platform.ChatEvent, handler Handler) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("handler_failed")
		}
	}()
	return handler(ctx, event)
}

func prepare(ctx context.Context, db *tenantdb.DB, a platform.ChatDispatchArgs, mayStart bool) (platform.ChatEvent, string, string, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return platform.ChatEvent{}, "", "", err
	}
	defer tx.Rollback()
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT envelope FROM normalized_chat_events WHERE /*tenant:normalized_chat_events*/ AND id=$1 AND connection_id=$2`, a.EventID, a.ConnectionID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return platform.ChatEvent{}, "", "event_missing", nil
	}
	if err != nil {
		return platform.ChatEvent{}, "", "", err
	}
	// Concurrent attempts serialize on an opaque scoped event key. The durable
	// receipt, rather than this transaction lock, fences crash recovery.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, db.TenantID()+":"+a.EventID); err != nil {
		return platform.ChatEvent{}, "", "", err
	}
	var prior string
	err = tx.QueryRowContext(ctx, `SELECT state FROM platform_dispatch_receipts WHERE /*tenant:platform_dispatch_receipts*/ AND event_id=$1`, a.EventID).Scan(&prior)
	if err == nil {
		if prior == "running" {
			if _, err = tx.ExecContext(ctx, `UPDATE platform_dispatch_receipts SET state='uncertain',code='prior_start',finished_at=now() WHERE /*tenant:platform_dispatch_receipts*/ AND event_id=$1 AND state='running'`, a.EventID); err != nil {
				return platform.ChatEvent{}, "", "", err
			}
			prior = "uncertain"
		}
		return platform.ChatEvent{}, "", prior, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return platform.ChatEvent{}, "", "", err
	}
	var event platform.ChatEvent
	code := ""
	if json.Unmarshal(raw, &event) != nil {
		code = "invalid_event"
	} else {
		canonical, err := platform.NewChatEvent(platform.Connection{TenantID: event.TenantID, ID: event.ConnectionID, Platform: event.Platform}, event.NativeEventID, event.OccurredAt, event.ReceivedAt, event.Chat)
		if err != nil || event.TenantID != a.TenantID || event.ConnectionID != a.ConnectionID || event.ID != a.EventID || event.ID != canonical.ID || event.IdempotencyKey != canonical.IdempotencyKey || event.TraceID != canonical.TraceID || event.Kind != "chat_message" || event.Version != platform.EventVersion || a.Deadline != event.OccurredAt.Add(2*time.Minute).Unix() {
			code = "invalid_event"
		} else if time.Now().Unix() >= a.Deadline {
			code = "expired"
		}
	}
	if code == "" {
		var channel string
		var scopesRaw []byte
		err = tx.QueryRowContext(ctx, `SELECT provider_user_id,scopes FROM platform_connections WHERE /*tenant:platform_connections*/ AND provider=$1 AND revoked_at IS NULL FOR SHARE`, event.Platform).Scan(&channel, &scopesRaw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return platform.ChatEvent{}, "", "", err
		}
		var scopes []string
		granted := false
		if json.Unmarshal(scopesRaw, &scopes) == nil {
			for _, scope := range scopes {
				if scope == "user:read:chat" {
					granted = true
				}
			}
		}
		if err != nil || event.Platform != "twitch" || channel != event.Chat.Channel.ID || event.ConnectionID != "twitch:"+channel || !granted {
			code = "connection_unavailable"
		}
	}
	// A source or receipt may change between the metadata read and this locked
	// reconstruction. Never start a new handler without its prepared runtime.
	if code == "" && !mayStart {
		return platform.ChatEvent{}, "", "", errors.New("runtime not prepared")
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return platform.ChatEvent{}, "", "", err
	}
	token := hex.EncodeToString(nonce[:])
	state, receiptCode := "running", "started"
	if code != "" {
		state, receiptCode = "skipped", code
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO platform_dispatch_receipts(event_id,connection_id,state,code,claim_token,finished_at) VALUES($1,$2,$3,$4,$5,CASE WHEN $3='skipped' THEN now() END)`, a.EventID, a.ConnectionID, state, receiptCode, token)
	if err != nil {
		return platform.ChatEvent{}, "", "", err
	}
	if err = tx.Commit(); err != nil {
		return platform.ChatEvent{}, "", "", err
	}
	return event, token, code, nil
}
