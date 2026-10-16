package platformoutbound

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformdispatch"
	"github.com/riverqueue/river"
)

func workerPool(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func startOutbound(t *testing.T, service *sql.DB, executor *Executor) *river.Client[*sql.Tx] {
	t.Helper()
	c, err := NewClient(t.Context(), service, executor)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopOutbound(t, c) })
	return c
}

func stopOutbound(t *testing.T, c *river.Client[*sql.Tx]) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Stop(ctx); err != nil {
		t.Error(err)
	}
}

func waitOutbound(t *testing.T, db *sql.DB, state string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if awaitOutbound(ctx, db, state) {
		return
	}
	logOutboundDiagnostics(t, db)
	t.Fatalf("outbound job did not reach %s", state)
}

func TestOutboundRiverLeasingIsolationAndRestart(t *testing.T) {
	f, product, pacer, args := executorFixture(t)
	service := workerPool(t, f.OutboundWorkerURL)
	chat := workerPool(t, f.WorkerURL)
	if _, err := f.Admin.Exec(`INSERT INTO river_job(kind,args,max_attempts,tags,queue) VALUES('another_kind','{"secret":"PRIVATE"}',3,'{}','chat_outbound')`); err != nil {
		t.Fatal(err)
	}
	for _, pool := range []*sql.DB{service, chat} {
		conn, err := pool.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = conn.ExecContext(t.Context(), `SELECT set_config('app.tenant_id',$1,false)`, product.TenantID()); err != nil {
			t.Fatal(err)
		}
		var visible int
		if err = conn.QueryRowContext(t.Context(), `SELECT count(*) FROM river_job`).Scan(&visible); err != nil || visible != 1 {
			t.Fatal("kind isolation", visible, err)
		}
		result, err := conn.ExecContext(t.Context(), `UPDATE river_job SET priority=priority WHERE kind='another_kind'`)
		if err != nil {
			t.Fatal(err)
		}
		n, _ := result.RowsAffected()
		if n != 0 {
			t.Fatal("foreign job changed")
		}
		if err = conn.Close(); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"platform_outbound_actions", "normalized_chat_events", "platform_connections", "bot_accounts", "chat_pacing_accounts", "service_bot_accounts"} {
		var count int
		if err := service.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err == nil {
			t.Fatal("worker read private table", table)
		}
	}
	var sends atomic.Int32
	e, err := NewExecutor(t.Context(), product, pacer, func(context.Context, Target) (Authorization, error) {
		return Authorization{SenderID: "123", Capabilities: fullCapabilities(), Send: func(ctx context.Context, m platform.OutboundMessage) (platform.Receipt, error) {
			sends.Add(1)
			if m.ActionID != args.ActionID || m.Text != "response text" {
				t.Error("message changed")
			}
			return platform.Receipt{ActionID: m.ActionID, Status: platform.Delivered, ProviderMessageID: "worker-sent"}, nil
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	client := startOutbound(t, service, e)
	waitOutbound(t, service, "completed")
	stopOutbound(t, client)
	if sends.Load() != 1 || receiptFor(t, product).State != "delivered" {
		t.Fatal("delivery missing", sends.Load())
	}
	// Re-deliver the same retained job to a fresh client. The durable ledger,
	// rather than process memory or a River success acknowledgement, prevents send.
	if _, err = f.Admin.Exec(`UPDATE river_job SET state='available',finalized_at=NULL,scheduled_at=now() WHERE kind='overlaid_chat_send'`); err != nil {
		t.Fatal(err)
	}
	restarted := startOutbound(t, service, e)
	waitOutbound(t, service, "completed")
	stopOutbound(t, restarted)
	if sends.Load() != 1 {
		t.Fatal("restart replayed send", sends.Load())
	}
	var untouched int
	if err = f.Admin.QueryRow(`SELECT count(*) FROM river_job WHERE kind<>'overlaid_chat_send' AND state='available'`).Scan(&untouched); err != nil || untouched != 2 {
		t.Fatal("other jobs changed", untouched, err)
	}
}

func TestOutboundRiverPreparationRetryAndRedaction(t *testing.T) {
	f, product, pacer, _ := executorFixture(t)
	service := workerPool(t, f.OutboundWorkerURL)
	var auths, sends atomic.Int32
	e, err := NewExecutor(t.Context(), product, pacer, func(context.Context, Target) (Authorization, error) {
		if auths.Add(1) == 1 {
			return Authorization{}, errors.New("PRIVATE provider detail")
		}
		return Authorization{SenderID: "123", Capabilities: fullCapabilities(), Send: func(ctx context.Context, m platform.OutboundMessage) (platform.Receipt, error) {
			sends.Add(1)
			return platform.Receipt{ActionID: m.ActionID, Status: platform.Delivered, ProviderMessageID: "retry-sent"}, nil
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	c := startOutbound(t, service, e)
	waitOutbound(t, service, "completed")
	stopOutbound(t, c)
	var attempts int
	var diagnostics string
	if err = service.QueryRow(`SELECT attempt,errors::text FROM river_job`).Scan(&attempts, &diagnostics); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 || sends.Load() != 1 || strings.Contains(diagnostics, "PRIVATE") || !strings.Contains(diagnostics, "outbound_prepare_unavailable") {
		t.Fatal("unsafe retry", attempts, sends.Load(), diagnostics)
	}
}

func TestOutboundRiverPreparationExhaustionAndExpiry(t *testing.T) {
	for _, mode := range []string{"unavailable", "expired"} {
		t.Run(mode, func(t *testing.T) {
			f, product, pacer, args := executorFixture(t)
			service := workerPool(t, f.OutboundWorkerURL)
			if mode == "expired" {
				if _, err := f.Admin.Exec(`UPDATE platform_outbound_actions SET deadline=clock_timestamp()-interval '1 second'`); err != nil {
					t.Fatal(err)
				}
				if _, err := f.Admin.Exec(`UPDATE river_job SET args=jsonb_set(args,'{deadline}',to_jsonb((SELECT floor(extract(epoch FROM deadline))::bigint FROM platform_outbound_actions))) WHERE kind='overlaid_chat_send'`); err != nil {
					t.Fatal(err)
				}
			}
			var auths atomic.Int32
			e, err := NewExecutor(t.Context(), product, pacer, func(context.Context, Target) (Authorization, error) {
				auths.Add(1)
				return Authorization{}, errors.New("PRIVATE unavailable")
			})
			if err != nil {
				t.Fatal(err)
			}
			c := startOutbound(t, service, e)
			state := "discarded"
			if mode == "expired" {
				state = "cancelled"
			}
			waitOutbound(t, service, state)
			stopOutbound(t, c)
			var attempt int
			var scheduled time.Time
			if err := service.QueryRow(`SELECT attempt,scheduled_at FROM river_job`).Scan(&attempt, &scheduled); err != nil {
				t.Fatal(err)
			}
			if mode == "unavailable" {
				if attempt != 3 || auths.Load() != 3 || scheduled.After(time.Unix(args.Deadline, 0)) || receiptFor(t, product).State != "pending" {
					t.Fatal("preparation retry bound", attempt, auths.Load())
				}
			} else if auths.Load() != 0 || receiptFor(t, product).State != "rejected" {
				t.Fatal("expired authorization attempted", auths.Load())
			}
		})
	}
}

func TestOutboundRiverPacingSnoozeAndRestart(t *testing.T) {
	f, product, pacer, args := executorFixture(t)
	service := workerPool(t, f.OutboundWorkerURL)
	permit, err := pacer.Acquire(t.Context(), "123", "456")
	if err != nil || !permit.Granted {
		t.Fatal(err)
	}
	var sends atomic.Int32
	e, err := NewExecutor(t.Context(), product, pacer, func(context.Context, Target) (Authorization, error) {
		return Authorization{SenderID: "123", Capabilities: fullCapabilities(), Send: func(ctx context.Context, m platform.OutboundMessage) (platform.Receipt, error) {
			sends.Add(1)
			return platform.Receipt{ActionID: m.ActionID, Status: platform.Delivered, ProviderMessageID: "snooze-sent"}, nil
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewClient(t.Context(), service, e)
	if err != nil {
		t.Fatal(err)
	}
	events, cancel := c.Subscribe(river.EventKindJobSnoozed)
	defer cancel()
	if err = c.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopOutbound(t, c) })
	select {
	case <-events:
	case <-time.After(10 * time.Second):
		logOutboundDiagnostics(t, service)
		t.Fatal("job did not snooze")
	}
	stopOutbound(t, c)
	var attempt, maxAttempts int
	var scheduled time.Time
	if err = service.QueryRow(`SELECT attempt,max_attempts,scheduled_at FROM river_job`).Scan(&attempt, &maxAttempts, &scheduled); err != nil {
		t.Fatal(err)
	}
	if attempt != 0 || maxAttempts != 3 || scheduled.After(time.Unix(args.Deadline, 0).Add(100*time.Millisecond)) || receiptFor(t, product).State != "pending" || sends.Load() != 0 {
		t.Fatal("snooze consumed attempt or claim", attempt, maxAttempts, scheduled)
	}
	diagnostic := snapshotOutbound(service)
	if !diagnostic.JobRead || diagnostic.ErrorCount != 0 || diagnostic.DeadlineInMS <= 0 {
		t.Fatalf("unreadable snoozed job metadata: %+v", diagnostic)
	}
	if err = pacer.Complete(t.Context(), permit.Permit, 0); err != nil {
		t.Fatal(err)
	}
	// Advance only River's test schedule after releasing the competing permit.
	// Production pacing/deadlines stay intact and are rechecked by the new client.
	if _, err = f.Admin.Exec(`UPDATE river_job SET state='available',scheduled_at=now()+interval '1100 milliseconds' WHERE kind='overlaid_chat_send'`); err != nil {
		t.Fatal(err)
	}
	restarted := startOutbound(t, service, e)
	waitOutbound(t, service, "completed")
	stopOutbound(t, restarted)
	if sends.Load() != 1 {
		t.Fatal("snoozed job delivery", sends.Load())
	}
}

func TestOutboundWorkerTerminalAndAbandonedClaims(t *testing.T) {
	for _, mode := range []string{"uncertain", "bookkeeping", "abandoned", "denied"} {
		t.Run(mode, func(t *testing.T) {
			f, product, pacer, args := executorFixture(t)
			var sends atomic.Int32
			runtimeURL, _ := url.Parse(f.RuntimeURL)
			e, err := NewExecutor(t.Context(), product, pacer, func(context.Context, Target) (Authorization, error) {
				if mode == "denied" {
					return Authorization{}, ErrDenied
				}
				return Authorization{SenderID: "123", Capabilities: fullCapabilities(), Send: func(ctx context.Context, m platform.OutboundMessage) (platform.Receipt, error) {
					sends.Add(1)
					if mode == "bookkeeping" {
						if _, err := f.Admin.Exec(`REVOKE UPDATE ON platform_outbound_actions FROM ` + runtimeURL.User.Username()); err != nil {
							t.Error(err)
						}
					}
					return platform.Receipt{ActionID: m.ActionID, Status: platform.Uncertain}, errors.New("PRIVATE failure")
				}}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "abandoned" {
				s, _ := New(product)
				if _, ok, err := s.Start(t.Context(), args.ActionID); err != nil || !ok {
					t.Fatal(ok, err)
				}
			}
			w, _ := NewWorker(e)
			job := &river.Job[platform.ChatSendArgs]{Args: args}
			err = w.Work(t.Context(), job)
			var cancelled *river.JobCancelError
			if !errors.As(err, &cancelled) || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("terminal retried or leaked", err)
			}
			if mode == "bookkeeping" {
				if _, err = f.Admin.Exec(`GRANT UPDATE ON platform_outbound_actions TO ` + runtimeURL.User.Username()); err != nil {
					t.Fatal(err)
				}
			}
			restarted, _ := NewWorker(e)
			_ = restarted.Work(t.Context(), job)
			expected := int32(1)
			if mode == "abandoned" || mode == "denied" {
				expected = 0
			}
			if sends.Load() != expected {
				t.Fatal("terminal replay", sends.Load())
			}
		})
	}
}

func TestQueueClientsRejectCombinedReachableAndChangedPolicies(t *testing.T) {
	f, product, pacer, _ := executorFixture(t)
	outbound := workerPool(t, f.OutboundWorkerURL)
	chat := workerPool(t, f.WorkerURL)
	e, err := NewExecutor(t.Context(), product, pacer, func(context.Context, Target) (Authorization, error) { return Authorization{}, ErrDenied })
	if err != nil {
		t.Fatal(err)
	}
	handler := func(context.Context, platform.ChatEvent) error { return nil }
	checkOutbound := func(pool *sql.DB) {
		t.Helper()
		if _, err := NewClient(t.Context(), pool, e); err == nil {
			t.Fatal("unsafe outbound role accepted")
		}
	}
	checkChat := func() {
		t.Helper()
		if _, err := platformdispatch.NewClient(t.Context(), chat, product, handler); err == nil {
			t.Fatal("unsafe chat role accepted")
		}
	}
	checkOutbound(f.Admin)
	checkOutbound(workerPool(t, f.RuntimeURL))
	checkOutbound(chat)
	_, foreign, _, _ := executorFixture(t)
	foreignExecutor := *e
	foreignExecutor.product = foreign
	if _, err = NewClient(t.Context(), outbound, &foreignExecutor); err == nil {
		t.Fatal("foreign schema accepted")
	}
	outboundURL, _ := url.Parse(f.OutboundWorkerURL)
	chatURL, _ := url.Parse(f.WorkerURL)
	runtimeURL, _ := url.Parse(f.RuntimeURL)
	outRole, chatRole, runtimeRole := outboundURL.User.Username(), chatURL.User.Username(), runtimeURL.User.Username()
	for _, inherit := range []string{"TRUE", "FALSE"} {
		if _, err = f.Admin.Exec(`GRANT ` + chatRole + ` TO ` + outRole + ` WITH INHERIT ` + inherit); err != nil {
			t.Fatal(err)
		}
		checkOutbound(outbound)
		if _, err = f.Admin.Exec(`REVOKE ` + chatRole + ` FROM ` + outRole); err != nil {
			t.Fatal(err)
		}
		if _, err = f.Admin.Exec(`GRANT ` + outRole + ` TO ` + chatRole + ` WITH INHERIT ` + inherit); err != nil {
			t.Fatal(err)
		}
		checkChat()
		if _, err = f.Admin.Exec(`REVOKE ` + outRole + ` FROM ` + chatRole); err != nil {
			t.Fatal(err)
		}
	}
	for _, role := range []string{outRole, chatRole} {
		if _, err = f.Admin.Exec(`GRANT ` + runtimeRole + ` TO ` + role + ` WITH INHERIT FALSE`); err != nil {
			t.Fatal(err)
		}
		if role == outRole {
			checkOutbound(outbound)
		} else {
			checkChat()
		}
		if _, err = f.Admin.Exec(`REVOKE ` + runtimeRole + ` FROM ` + role); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = f.Admin.Exec(`GRANT TRUNCATE ON river_job TO ` + outRole); err != nil {
		t.Fatal(err)
	}
	checkOutbound(outbound)
	if _, err = f.Admin.Exec(`REVOKE TRUNCATE ON river_job FROM ` + outRole + `; ALTER TABLE river_job NO FORCE ROW LEVEL SECURITY`); err != nil {
		t.Fatal(err)
	}
	checkOutbound(outbound)
	checkChat()
	if _, err = f.Admin.Exec(`ALTER TABLE river_job FORCE ROW LEVEL SECURITY`); err != nil {
		t.Fatal(err)
	}

	if _, err = f.Admin.Exec(`ALTER POLICY outbound_worker_dispatch ON river_job USING(true) WITH CHECK(true)`); err != nil {
		t.Fatal(err)
	}
	checkOutbound(outbound)
	if _, err = f.Admin.Exec(`ALTER POLICY outbound_worker_dispatch ON river_job USING(kind='overlaid_chat_send') WITH CHECK(kind='overlaid_chat_send'); CREATE POLICY accidental_public ON river_job USING(true)`); err != nil {
		t.Fatal(err)
	}
	checkOutbound(outbound)
	checkChat()
}
