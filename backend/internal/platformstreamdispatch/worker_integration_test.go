package platformstreamdispatch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformingest"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformqueue"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const owner = "00000000-0000-4000-8000-000000000002"

func fixture(t *testing.T) (testpostgres.TenantFixture, *tenantdb.DB) {
	t.Helper()
	f := testpostgres.NewTenantFixture(t, "")
	if _, err := f.Admin.Exec(`INSERT INTO platform_connections(tenant_id,provider,provider_user_id,login) VALUES($1,'twitch','channel','Channel')`, tenantdb.BootstrapID); err != nil {
		t.Fatal(err)
	}
	db, err := tenantdb.Open(f.RuntimeURL, tenantdb.BootstrapID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return f, db
}

func enqueue(t *testing.T, db *tenantdb.DB, native string, age time.Duration) *river.Job[platform.StreamDispatchArgs] {
	t.Helper()
	now := time.Now().UTC()
	e, err := platform.NewStreamEvent(platform.Connection{TenantID: db.TenantID(), ID: "twitch:channel", Platform: "twitch"}, native, platform.ChannelUpdateKind, now.Add(-age), now, platform.Identity{ID: "channel", Login: "Channel"}, nil, nil, &platform.ChannelUpdate{Title: "original private title", ContentClassificationLabels: []string{"MatureGame"}})
	if err != nil {
		t.Fatal(err)
	}
	s, _ := platformingest.New(db)
	if added, err := s.IngestStream(t.Context(), e, json.RawMessage(`{"privateNative":"secret"}`)); err != nil || !added {
		t.Fatal(added, err)
	}
	var id int64
	if err := db.QueryRowContext(t.Context(), `SELECT id FROM river_job WHERE overlaid_stream_event_id=$1`, e.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return &river.Job[platform.StreamDispatchArgs]{JobRow: &rivertype.JobRow{ID: id, Kind: "overlaid_stream_dispatch", Queue: "stream_followup", Priority: 4, MaxAttempts: 3, Attempt: 1}, Args: platform.StreamDispatchArgs{TenantID: e.TenantID, ConnectionID: e.ConnectionID, EventID: e.ID, Deadline: e.OccurredAt.Unix() + 120}}
}

func granted() platform.CapabilityReport {
	return platform.EvaluateCapabilities(map[platform.Capability]platform.CapabilityGates{"stream.read": {Supported: true, Granted: true, RoleAllowed: true, StateAllowed: true, Entitled: true}})
}

func preparation(ctx context.Context, handler Handler) Preparation {
	return func(context.Context, platform.StreamEvent) (Prepared, error) {
		return Prepared{Context: ctx, Handle: handler, Capabilities: func(context.Context, platform.StreamEvent) (platform.CapabilityReport, error) { return granted(), nil }, Release: func() {}}, nil
	}
}

func requireTerminal(t *testing.T, err error) {
	t.Helper()
	var canceled *river.JobCancelError
	if !errors.As(err, &canceled) || strings.Contains(err.Error(), "private-secret") {
		t.Fatal("expected fixed terminal error", err)
	}
}

func receipt(t *testing.T, db *tenantdb.DB, id, state, code string) {
	t.Helper()
	var actualState, actualCode string
	if err := db.QueryRowContext(t.Context(), `SELECT state,code FROM platform_stream_dispatch_receipts WHERE /*tenant:platform_stream_dispatch_receipts*/ AND event_id=$1`, id).Scan(&actualState, &actualCode); err != nil || actualState != state || actualCode != code {
		t.Fatalf("receipt %s/%s want %s/%s: %v", actualState, actualCode, state, code, err)
	}
}

func TestStreamCompletionSnapshotsRestartAndExport(t *testing.T) {
	f, db := fixture(t)
	job := enqueue(t, db, "delivery", 0)
	var prepares, auths, handles, releases atomic.Int32
	prep := func(ctx context.Context, e platform.StreamEvent) (Prepared, error) {
		prepares.Add(1)
		e.Update.Title = "changed by prepare"
		return Prepared{Context: t.Context(), Handle: func(_ context.Context, event platform.StreamEvent) error {
			handles.Add(1)
			if event.Update.Title != "original private title" || event.Update.ContentClassificationLabels[0] != "MatureGame" {
				t.Error("callback mutated input snapshot")
			}
			return nil
		}, Capabilities: func(_ context.Context, event platform.StreamEvent) (platform.CapabilityReport, error) {
			auths.Add(1)
			event.Update.Title = "changed by capability"
			event.Update.ContentClassificationLabels[0] = "changed"
			return granted(), nil
		}, Release: func() { releases.Add(1) }}, nil
	}
	w, err := NewWorker(db, prep)
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Work(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	receipt(t, db, job.Args.EventID, "completed", "completed")
	reopened, err := tenantdb.Open(f.RuntimeURL, db.TenantID())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	restarted, _ := NewWorker(reopened, prep)
	if err = restarted.Work(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	if prepares.Load() != 1 || handles.Load() != 1 || auths.Load() != 2 || releases.Load() != 1 {
		t.Fatal("replayed or missing fresh checks", prepares.Load(), handles.Load(), auths.Load(), releases.Load())
	}
	control, err := tenancy.Open(f.RuntimeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = control.Close() }()
	exported, err := control.Export(t.Context(), owner, db.TenantID())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(exported)
	if !strings.Contains(string(raw), "platform_stream_dispatch_receipts") || strings.Contains(string(raw), "claim_hash") || strings.Contains(string(raw), "privateNative") {
		t.Fatal("unsafe receipt export", string(raw))
	}
	if err = control.DeleteViewer(t.Context(), owner, db.TenantID(), "channel", "Channel"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRowContext(t.Context(), `SELECT count(*) FROM platform_stream_dispatch_receipts`).Scan(&count); err != nil || count != 0 {
		t.Fatal("receipt did not cascade", count, err)
	}
}

func TestStreamSourceLockGrantPreservesImmutableInput(t *testing.T) {
	f, db := fixture(t)
	job := enqueue(t, db, "immutable", 0)
	if _, err := db.ExecContext(t.Context(), `UPDATE normalized_stream_events SET id=id WHERE /*tenant:normalized_stream_events*/`); err != nil {
		t.Fatal("same-value lock grant unavailable", err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE normalized_stream_events SET envelope='{}' WHERE /*tenant:normalized_stream_events*/`); err == nil {
		t.Fatal("source payload became mutable")
	}
	// Remove dependents so their foreign keys cannot mask the ID binding check.
	if _, err := db.ExecContext(t.Context(), `DELETE FROM river_job WHERE id=$1`, job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `DELETE FROM platform_stream_payloads WHERE /*tenant:platform_stream_payloads*/`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE normalized_stream_events SET id=repeat('0',64) WHERE /*tenant:normalized_stream_events*/`); err == nil {
		t.Fatal("source identity became mutable")
	}
	u, _ := url.Parse(f.RuntimeURL)
	if _, err := f.Admin.Exec(`GRANT UPDATE(envelope) ON normalized_stream_events TO ` + u.User.Username()); err != nil {
		t.Fatal(err)
	}
	if opened, err := tenantdb.Open(f.RuntimeURL, db.TenantID()); err == nil {
		_ = opened.Close()
		t.Fatal("broadened source write grant accepted")
	}
}

func TestStreamConsumerMigrationPreservesPendingInput(t *testing.T) {
	f, db := fixture(t)
	job := enqueue(t, db, "pre-consumer", 0)
	var before, after string
	const snapshot = `SELECT jsonb_build_object('source',(SELECT to_jsonb(e) FROM normalized_stream_events e),'payload',(SELECT to_jsonb(p) FROM platform_stream_payloads p),'job',(SELECT to_jsonb(j) FROM river_job j))::text`
	if err := f.Admin.QueryRow(snapshot).Scan(&before); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(f.RuntimeURL)
	if _, err := f.Admin.Exec(`DROP POLICY stream_worker_dispatch ON river_job; DROP TABLE platform_stream_dispatch_receipts; ALTER TABLE normalized_stream_events DROP CONSTRAINT stream_dispatch_locked_identity; REVOKE UPDATE(id) ON normalized_stream_events FROM ` + u.User.Username() + `; DELETE FROM overlaid_migrations WHERE name='0032_stream_dispatch.sql'`); err != nil {
		t.Fatal(err)
	}
	if opened, err := tenantdb.Open(f.RuntimeURL, db.TenantID()); err == nil {
		_ = opened.Close()
		t.Fatal("missing consumer migration accepted")
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "0032_stream_dispatch.sql"))
	if err != nil {
		t.Fatal(err)
	}
	worker, _ := url.Parse(f.StreamWorkerURL)
	if _, err = f.Admin.Exec(strings.NewReplacer("overlaid_runtime", u.User.Username(), "overlaid_stream_worker", worker.User.Username()).Replace(string(raw))); err != nil {
		t.Fatal(err)
	}
	if err = f.Admin.QueryRow(snapshot).Scan(&after); err != nil || before != after {
		t.Fatal("pending stream input changed", err)
	}
	reopened, err := tenantdb.Open(f.RuntimeURL, db.TenantID())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	w, _ := NewWorker(reopened, preparation(t.Context(), func(context.Context, platform.StreamEvent) error { return nil }))
	if err = w.Work(t.Context(), job); err != nil {
		t.Fatal(err)
	}
}

func TestStreamConcurrentStartsFenceAndDoNotReplay(t *testing.T) {
	_, db := fixture(t)
	job := enqueue(t, db, "concurrent", 0)
	var handles atomic.Int32
	entered, finish := make(chan struct{}), make(chan struct{})
	w, _ := NewWorker(db, preparation(t.Context(), func(context.Context, platform.StreamEvent) error {
		handles.Add(1)
		close(entered)
		<-finish
		return nil
	}))
	result := make(chan error, 1)
	go func() { result <- w.Work(t.Context(), job) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not start")
	}
	var group sync.WaitGroup
	for range 10 {
		group.Add(1)
		go func() { defer group.Done(); requireTerminal(t, w.Work(t.Context(), job)) }()
	}
	group.Wait()
	close(finish)
	requireTerminal(t, <-result)
	receipt(t, db, job.Args.EventID, "uncertain", "prior_start")
	if handles.Load() != 1 {
		t.Fatal("concurrent replay", handles.Load())
	}
}

func TestStreamTerminalSourcesDoNotPrepare(t *testing.T) {
	for _, mode := range []string{"expired", "revoked", "invalid", "missing", "wrong_deadline"} {
		t.Run(mode, func(t *testing.T) {
			f, db := fixture(t)
			age := time.Duration(0)
			if mode == "expired" {
				age = 3 * time.Minute
			}
			job := enqueue(t, db, mode, age)
			switch mode {
			case "revoked":
				if _, err := f.Admin.Exec(`UPDATE platform_connections SET revoked_at=now()`); err != nil {
					t.Fatal(err)
				}
			case "invalid":
				if _, err := f.Admin.Exec(`UPDATE normalized_stream_events SET envelope=jsonb_set(envelope,'{version}','999')`); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if _, err := db.ExecContext(t.Context(), `DELETE FROM normalized_stream_events WHERE /*tenant:normalized_stream_events*/`); err != nil {
					t.Fatal(err)
				}
			case "wrong_deadline":
				job.Args.Deadline++
			}
			var prepares atomic.Int32
			w, _ := NewWorker(db, func(context.Context, platform.StreamEvent) (Prepared, error) {
				prepares.Add(1)
				return Prepared{}, errors.New("private-secret")
			})
			requireTerminal(t, w.Work(t.Context(), job))
			if prepares.Load() != 0 {
				t.Fatal("terminal source prepared runtime")
			}
			if mode == "expired" {
				receipt(t, db, job.Args.EventID, "skipped", "expired")
			}
			if mode == "revoked" {
				receipt(t, db, job.Args.EventID, "skipped", "connection_unavailable")
			}
			if mode == "invalid" {
				receipt(t, db, job.Args.EventID, "skipped", "invalid_event")
			}
		})
	}
}

func TestStreamPreparationAndFreshCapabilityFences(t *testing.T) {
	for _, mode := range []string{"prepare_error", "prepare_panic", "nil_owner", "pre_deny", "cap_error", "cap_panic", "post_deny", "post_revocation", "owner_pre_cancel", "owner_post_cancel"} {
		t.Run(mode, func(t *testing.T) {
			f, db := fixture(t)
			job := enqueue(t, db, mode, 0)
			ownerCtx, cancelOwner := context.WithCancel(t.Context())
			defer cancelOwner()
			var calls, handles, releases atomic.Int32
			prep := func(context.Context, platform.StreamEvent) (Prepared, error) {
				p := Prepared{Context: ownerCtx, Handle: func(context.Context, platform.StreamEvent) error { handles.Add(1); return nil }, Release: func() { releases.Add(1) }}
				p.Capabilities = func(context.Context, platform.StreamEvent) (platform.CapabilityReport, error) {
					n := calls.Add(1)
					if mode == "cap_error" {
						return platform.CapabilityReport{}, errors.New("private-secret")
					}
					if mode == "cap_panic" {
						panic("private-secret")
					}
					if mode == "pre_deny" || mode == "post_deny" && n == 2 {
						return platform.CapabilityReport{}, nil
					}
					if mode == "owner_pre_cancel" || mode == "owner_post_cancel" && n == 2 {
						cancelOwner()
					}
					if mode == "post_revocation" && n == 2 {
						if _, err := f.Admin.Exec(`UPDATE platform_connections SET revoked_at=now()`); err != nil {
							t.Error(err)
						}
					}
					return granted(), nil
				}
				if mode == "prepare_error" {
					return p, errors.New("private-secret")
				}
				if mode == "prepare_panic" {
					panic("private-secret")
				}
				if mode == "nil_owner" {
					p.Context = nil
				}
				return p, nil
			}
			w, _ := NewWorker(db, prep)
			err := w.Work(t.Context(), job)
			if err == nil || strings.Contains(err.Error(), "private-secret") || handles.Load() != 0 {
				t.Fatal("unsafe preparation/capability result", err, handles.Load())
			}
			if mode != "prepare_panic" && releases.Load() != 1 {
				t.Fatal("resource release lost", releases.Load())
			}
			var count int
			if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM platform_stream_dispatch_receipts`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if mode == "pre_deny" {
				requireTerminal(t, err)
				receipt(t, db, job.Args.EventID, "skipped", "capability_unavailable")
			} else if strings.HasPrefix(mode, "post_") {
				requireTerminal(t, err)
				code := "capability_unavailable"
				if mode == "post_revocation" {
					code = "connection_unavailable"
				}
				receipt(t, db, job.Args.EventID, "failed", code)
			} else if mode == "owner_post_cancel" {
				requireTerminal(t, err)
				receipt(t, db, job.Args.EventID, "uncertain", "handler_cancelled")
			} else if count != 0 {
				t.Fatal("pre-handler failure created a receipt")
			}
			if count > 0 {
				before := calls.Load()
				requireTerminal(t, w.Work(t.Context(), job))
				if calls.Load() != before {
					t.Fatal("terminal receipt reauthorized")
				}
			}
		})
	}
}

func TestStreamClaimedFailuresRemainTerminal(t *testing.T) {
	for _, mode := range []string{"handler_error", "handler_panic", "owner_cancel", "release_panic", "completion_failure", "claim_changed", "source_deleted"} {
		t.Run(mode, func(t *testing.T) {
			f, db := fixture(t)
			job := enqueue(t, db, mode, 0)
			ownerCtx, cancelOwner := context.WithCancel(t.Context())
			defer cancelOwner()
			var handles atomic.Int32
			w, _ := NewWorker(db, func(context.Context, platform.StreamEvent) (Prepared, error) {
				return Prepared{Context: ownerCtx, Capabilities: func(context.Context, platform.StreamEvent) (platform.CapabilityReport, error) { return granted(), nil }, Release: func() {
					if mode == "release_panic" {
						panic("private-secret")
					}
				}, Handle: func(ctx context.Context, _ platform.StreamEvent) error {
					handles.Add(1)
					switch mode {
					case "handler_error":
						return errors.New("private-secret")
					case "handler_panic":
						panic("private-secret")
					case "owner_cancel":
						cancelOwner()
						select {
						case <-ctx.Done():
						case <-time.After(time.Second):
							t.Error("owner cancellation not forwarded")
						}
					case "release_panic":
					case "completion_failure":
						u, _ := url.Parse(f.RuntimeURL)
						if _, err := f.Admin.Exec(`REVOKE UPDATE ON platform_stream_dispatch_receipts FROM ` + u.User.Username()); err != nil {
							t.Error(err)
						}
					case "claim_changed":
						if _, err := f.Admin.Exec(`UPDATE platform_stream_dispatch_receipts SET claim_hash=repeat('0',64)`); err != nil {
							t.Error(err)
						}
					case "source_deleted":
						if _, err := db.ExecContext(t.Context(), `DELETE FROM normalized_stream_events WHERE /*tenant:normalized_stream_events*/`); err != nil {
							t.Error(err)
						}
					}
					return nil
				}}, nil
			})
			requireTerminal(t, w.Work(t.Context(), job))
			if mode == "completion_failure" {
				u, _ := url.Parse(f.RuntimeURL)
				if _, err := f.Admin.Exec(`GRANT UPDATE ON platform_stream_dispatch_receipts TO ` + u.User.Username()); err != nil {
					t.Fatal(err)
				}
			}
			requireTerminal(t, w.Work(t.Context(), job))
			if handles.Load() != 1 {
				t.Fatal("claimed handler replayed", handles.Load())
			}
			if mode == "handler_error" || mode == "handler_panic" {
				receipt(t, db, job.Args.EventID, "failed", "handler_failed")
			}
			if mode == "owner_cancel" {
				receipt(t, db, job.Args.EventID, "uncertain", "handler_cancelled")
			}
			if mode == "release_panic" {
				receipt(t, db, job.Args.EventID, "uncertain", "owner_release_failed")
			}
			if mode == "completion_failure" || mode == "claim_changed" {
				receipt(t, db, job.Args.EventID, "uncertain", "prior_start")
			}
		})
	}
}

func TestStreamControlledLeasingAndRoleIsolation(t *testing.T) {
	f, db := fixture(t)
	job := enqueue(t, db, "leased", 0)
	s, _ := platformingest.New(db)
	now := time.Now().UTC()
	chat, _ := platform.NewChatEvent(platform.Connection{TenantID: db.TenantID(), ID: "twitch:channel", Platform: "twitch"}, "chat", now, now, platform.ChatMessage{MessageID: "chat", Text: "hello", Actor: platform.Identity{ID: "viewer"}, Channel: platform.Identity{ID: "channel"}})
	if _, err := s.IngestChat(t.Context(), chat, nil); err != nil {
		t.Fatal(err)
	}
	pool, err := sql.Open("pgx", f.StreamWorkerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Close() }()
	var active atomic.Bool
	var checks, handles, unavailable atomic.Int32
	ready := preparation(t.Context(), func(context.Context, platform.StreamEvent) error { handles.Add(1); return nil })
	prep := func(ctx context.Context, e platform.StreamEvent) (Prepared, error) {
		if e.NativeEventID == "unavailable" {
			unavailable.Add(1)
			return Prepared{}, errors.New("private-secret")
		}
		return ready(ctx, e)
	}
	client, err := NewControlledClient(t.Context(), pool, db, prep, func() bool { checks.Add(1); return active.Load() })
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := client.Stop(ctx); err != nil {
			t.Error(err)
		}
	}()
	until := time.Now().Add(5 * time.Second)
	for checks.Load() == 0 {
		if time.Now().After(until) {
			t.Fatal("lease gate not consulted")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var attempts int
	if err = db.QueryRowContext(t.Context(), `SELECT attempt FROM river_job WHERE id=$1`, job.ID).Scan(&attempts); err != nil || attempts != 0 || handles.Load() != 0 {
		t.Fatal("closed gate leased work", attempts, err)
	}
	active.Store(true)
	until = time.Now().Add(8 * time.Second)
	for {
		var state string
		if err = db.QueryRowContext(t.Context(), `SELECT state FROM river_job WHERE id=$1`, job.ID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "completed" {
			break
		}
		if time.Now().After(until) {
			t.Fatal("stream job did not complete", state)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if handles.Load() != 1 {
		t.Fatal("native worker did not run once")
	}
	failed := enqueue(t, db, "unavailable", 0)
	until = time.Now().Add(8 * time.Second)
	for {
		var state string
		var raw string
		if err = db.QueryRowContext(t.Context(), `SELECT state,attempt,coalesce(errors::text,'') FROM river_job WHERE id=$1`, failed.ID).Scan(&state, &attempts, &raw); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(raw, "private-secret") {
			t.Fatal("native job leaked preparation error")
		}
		if state == "discarded" {
			break
		}
		if time.Now().After(until) {
			t.Fatal("preparation attempts did not exhaust", state, attempts)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if attempts != 3 || unavailable.Load() != 3 {
		t.Fatal("preparation attempt bound lost", attempts, unavailable.Load())
	}
	var receiptCount int
	if err = db.QueryRowContext(t.Context(), `SELECT count(*) FROM platform_stream_dispatch_receipts WHERE event_id=$1`, failed.Args.EventID).Scan(&receiptCount); err != nil || receiptCount != 0 {
		t.Fatal("preparation failure created a receipt", receiptCount, err)
	}
	if err = db.QueryRowContext(t.Context(), `SELECT attempt FROM river_job WHERE kind='overlaid_chat_dispatch'`).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatal("stream worker leased chat", attempts, err)
	}
	if _, err = NewControlledClient(t.Context(), pool, db, preparation(t.Context(), func(context.Context, platform.StreamEvent) error { return nil }), nil); err == nil {
		t.Fatal("missing readiness gate accepted")
	}
}

func TestStreamMixedRolesAndSchemaDriftFailClosed(t *testing.T) {
	for _, mode := range []string{"mixed_roles", "private_access", "schema_state", "schema_cascade", "schema_claim", "schema_policy"} {
		t.Run(mode, func(t *testing.T) {
			f, db := fixture(t)
			streamURL, _ := url.Parse(f.StreamWorkerURL)
			chatURL, _ := url.Parse(f.WorkerURL)
			switch mode {
			case "mixed_roles":
				if _, err := f.Admin.Exec(`GRANT ` + chatURL.User.Username() + ` TO ` + streamURL.User.Username()); err != nil {
					t.Fatal(err)
				}
			case "private_access":
				if _, err := f.Admin.Exec(`GRANT SELECT ON normalized_stream_events TO ` + streamURL.User.Username()); err != nil {
					t.Fatal(err)
				}
			case "schema_state":
				if _, err := f.Admin.Exec(`ALTER TABLE platform_stream_dispatch_receipts DROP CONSTRAINT stream_dispatch_state; ALTER TABLE platform_stream_dispatch_receipts ADD CONSTRAINT stream_dispatch_state CHECK(true)`); err != nil {
					t.Fatal(err)
				}
			case "schema_cascade":
				if _, err := f.Admin.Exec(`ALTER TABLE platform_stream_dispatch_receipts DROP CONSTRAINT stream_dispatch_source; ALTER TABLE platform_stream_dispatch_receipts ADD CONSTRAINT stream_dispatch_source FOREIGN KEY(tenant_id,event_id,connection_id,deadline) REFERENCES normalized_stream_events(tenant_id,id,connection_id,dispatch_deadline)`); err != nil {
					t.Fatal(err)
				}
			case "schema_claim":
				if _, err := f.Admin.Exec(`ALTER TABLE platform_stream_dispatch_receipts DROP CONSTRAINT stream_dispatch_claim`); err != nil {
					t.Fatal(err)
				}
			case "schema_policy":
				if _, err := f.Admin.Exec(`ALTER POLICY tenant_isolation ON platform_stream_dispatch_receipts USING(true) WITH CHECK(true)`); err != nil {
					t.Fatal(err)
				}
			}
			if strings.HasPrefix(mode, "schema_") {
				if opened, err := tenantdb.Open(f.RuntimeURL, db.TenantID()); err == nil {
					_ = opened.Close()
					t.Fatal("unsafe receipt schema accepted")
				}
				return
			}
			pool, err := sql.Open("pgx", f.StreamWorkerURL)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = pool.Close() }()
			if _, err = platformqueue.Validate(t.Context(), pool, db, "overlaid_stream_dispatch"); err == nil {
				t.Fatal("unsafe stream role accepted")
			}
		})
	}
}
