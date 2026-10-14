// Package platformdispatch supplies the fenced consumer boundary. Application
// startup does not register a live effect handler or start this worker yet.
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
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
)

type Handler func(context.Context, platform.ChatEvent) error

type Worker struct {
	river.WorkerDefaults[platform.ChatDispatchArgs]
	product *tenantdb.DB
	handle  Handler
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
	w, err := NewWorker(product, handler)
	if err != nil || service == nil {
		return nil, errors.New("chat worker configuration invalid")
	}
	var unsafe, permitted, private bool
	err = service.QueryRowContext(ctx, `SELECT
 EXISTS(SELECT 1 FROM pg_roles r WHERE r.rolname IN (current_user,session_user) AND (r.rolsuper OR r.rolbypassrls OR EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relkind IN ('r','p') AND c.relowner=r.oid))),
 has_table_privilege(current_user,'river_job','SELECT') AND has_table_privilege(current_user,'river_job','UPDATE') AND EXISTS(SELECT 1 FROM pg_policy p,unnest(p.polroles) role_id WHERE p.polrelid='river_job'::regclass AND p.polname='worker_dispatch' AND pg_has_role(current_user,role_id,'MEMBER')),
 EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relkind IN ('r','p') AND c.relname NOT IN ('river_job','river_queue','river_notification','river_migration','river_leader') AND has_table_privilege(current_user,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER'))`).Scan(&unsafe, &permitted, &private)
	if err != nil || unsafe || !permitted || private {
		return nil, errors.New("separate non-owner chat worker role required")
	}
	const identity = `SELECT current_database(),current_schema(),coalesce(inet_server_addr()::text,'local'),coalesce(inet_server_port(),0)`
	var serviceDB, serviceSchema, serviceAddr, productDB, productSchema, productAddr string
	var servicePort, productPort int
	if err = service.QueryRowContext(ctx, identity).Scan(&serviceDB, &serviceSchema, &serviceAddr, &servicePort); err != nil {
		return nil, errors.New("chat worker database unavailable")
	}
	if err = product.QueryRowContext(ctx, identity).Scan(&productDB, &productSchema, &productAddr, &productPort); err != nil {
		return nil, errors.New("chat product database unavailable")
	}
	if serviceDB != productDB || serviceSchema != productSchema || serviceAddr != productAddr || servicePort != productPort {
		return nil, errors.New("chat worker and product database must match")
	}
	workers := river.NewWorkers()
	river.AddWorker(workers, w)
	return river.NewClient(riverdatabasesql.New(service), &river.Config{Schema: serviceSchema, PollOnly: true, Queues: map[string]river.QueueConfig{"ingest_followup": {MaxWorkers: 4}}, Workers: workers, JobTimeout: 30 * time.Second})
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
	event, token, terminal, err := prepare(ctx, db, a)
	if err != nil {
		return errors.New("dispatch_prepare_unavailable")
	}
	if terminal != "" {
		if terminal == "completed" {
			return nil
		}
		return cancel(terminal)
	}
	handlerErr := w.run(ctx, event)
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

func (w *Worker) run(ctx context.Context, event platform.ChatEvent) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("handler_failed")
		}
	}()
	return w.handle(ctx, event)
}

func prepare(ctx context.Context, db *tenantdb.DB, a platform.ChatDispatchArgs) (platform.ChatEvent, string, string, error) {
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
