package platformmoderation

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
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformingest"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"github.com/riverqueue/river"
)

func stopModerationClient(t *testing.T, c *river.Client[*sql.Tx]) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Stop(ctx); err != nil {
		t.Error(err)
	}
}
func startModerationClient(t *testing.T, service *sql.DB, e *Executor) *river.Client[*sql.Tx] {
	t.Helper()
	c, err := NewClient(t.Context(), service, e)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopModerationClient(t, c) })
	return c
}
func waitModerationJob(t *testing.T, service *sql.DB, want string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	var state string
	var attempt, errorCount int
	for {
		err := service.QueryRowContext(ctx, `SELECT state,attempt,coalesce(cardinality(errors),0) FROM river_job WHERE kind='overlaid_moderation'`).Scan(&state, &attempt, &errorCount)
		if err == nil && state == want {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("moderation job timeout: want=%s state=%s attempt=%d error_count=%d", want, state, attempt, errorCount)
		case <-ticker.C:
		}
	}
}

func TestModerationRiverIsolationDeliveryAndRestart(t *testing.T) {
	f, product, pacer, args := executionFixture(t)
	service := pool(t, f.ModerationWorkerURL)
	if _, err := f.Admin.Exec(`INSERT INTO river_job(kind,args,max_attempts,tags,queue) VALUES('another_kind','{"private":"PRIVATE"}',3,'{}','moderation_outbound')`); err != nil {
		t.Fatal(err)
	}
	conn, err := service.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(t.Context(), `SELECT set_config('app.tenant_id',$1,false)`, product.TenantID()); err != nil {
		t.Fatal(err)
	}
	var visible int
	if err = conn.QueryRowContext(t.Context(), `SELECT count(*) FROM river_job`).Scan(&visible); err != nil || visible != 1 {
		t.Fatal("tenant context crossed job kind", visible, err)
	}
	if err = conn.Close(); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"platform_moderation_actions", "platform_outbound_actions", "normalized_chat_events", "platform_connections", "bot_accounts", "service_bot_accounts", "chat_pacing_accounts", "moderation_pacing_buckets"} {
		if _, err := service.Exec(`SELECT * FROM ` + table); err == nil {
			t.Fatal("worker read private table", table)
		}
	}
	var attempts atomic.Int32
	e, err := NewExecutor(t.Context(), product, pacer, func(context.Context, Target) (Authorization, error) {
		return executionAuthorization(func(ctx context.Context, a platform.ModerationAction) (platform.ModerationReceipt, error) {
			attempts.Add(1)
			if a.ActionID != args.ActionID || a.Reason != "PRIVATE_REASON" {
				t.Error("private action changed")
			}
			return delivered(ctx, a)
		}), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	c := startModerationClient(t, service, e)
	waitModerationJob(t, service, "completed")
	stopModerationClient(t, c)
	if attempts.Load() != 1 || executionReceipt(t, product).State != "delivered" {
		t.Fatal("missing delivery", attempts.Load())
	}
	if _, err = f.Admin.Exec(`UPDATE river_job SET state='available',finalized_at=NULL,scheduled_at=now() WHERE kind='overlaid_moderation'`); err != nil {
		t.Fatal(err)
	}
	// A new client and worker have no previous process-local success state.
	restarted := startModerationClient(t, service, e)
	waitModerationJob(t, service, "completed")
	stopModerationClient(t, restarted)
	if attempts.Load() != 1 {
		t.Fatal("restart repeated effect", attempts.Load())
	}
	var untouched int
	if err = f.Admin.QueryRow(`SELECT count(*) FROM river_job WHERE kind<>'overlaid_moderation' AND state='available'`).Scan(&untouched); err != nil || untouched != 2 {
		t.Fatal("foreign kind consumed", untouched, err)
	}
}

func TestModerationRiverRetriesOnlyPreparation(t *testing.T) {
	for _, mode := range []string{"recovers", "exhausted", "expired", "invalid-deadline"} {
		t.Run(mode, func(t *testing.T) {
			f, product, pacer, args := executionFixture(t)
			service := pool(t, f.ModerationWorkerURL)
			if mode == "expired" {
				if _, err := f.Admin.Exec(`UPDATE platform_moderation_actions SET deadline=clock_timestamp()-interval '1 second'; UPDATE river_job SET args=jsonb_set(args,'{deadline}',to_jsonb((SELECT floor(extract(epoch FROM deadline))::bigint FROM platform_moderation_actions))) WHERE kind='overlaid_moderation'`); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "invalid-deadline" {
				if _, err := f.Admin.Exec(`UPDATE river_job SET args=jsonb_set(args,'{deadline}',to_jsonb((args->>'deadline')::bigint+1)) WHERE kind='overlaid_moderation'`); err != nil {
					t.Fatal(err)
				}
			}
			var auths, attempts atomic.Int32
			e, err := NewExecutor(t.Context(), product, pacer, func(context.Context, Target) (Authorization, error) {
				n := auths.Add(1)
				if mode != "recovers" || n == 1 {
					return Authorization{}, errors.New("PRIVATE_PROVIDER_ERROR")
				}
				return executionAuthorization(func(ctx context.Context, a platform.ModerationAction) (platform.ModerationReceipt, error) {
					attempts.Add(1)
					return delivered(ctx, a)
				}), nil
			})
			if err != nil {
				t.Fatal(err)
			}
			c := startModerationClient(t, service, e)
			want := "cancelled"
			if mode == "recovers" {
				want = "completed"
			}
			if mode == "exhausted" {
				want = "discarded"
			}
			waitModerationJob(t, service, want)
			stopModerationClient(t, c)
			var attempt int
			var raw string
			var scheduled time.Time
			if err = service.QueryRow(`SELECT attempt,coalesce(errors::text,''),scheduled_at FROM river_job WHERE kind='overlaid_moderation'`).Scan(&attempt, &raw, &scheduled); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(raw, "PRIVATE") {
				t.Fatal("provider error retained")
			}
			switch mode {
			case "recovers":
				if attempt != 2 || attempts.Load() != 1 || auths.Load() != 3 || !strings.Contains(raw, "moderation_prepare_unavailable") {
					t.Fatal("preparation retry", attempt, attempts.Load(), auths.Load())
				}
			case "exhausted":
				if attempt != 3 || attempts.Load() != 0 || auths.Load() != 3 || scheduled.After(time.Unix(args.Deadline, 0)) || executionReceipt(t, product).State != "pending" {
					t.Fatal("exhaustion renewed or claimed action", attempt, attempts.Load())
				}
			default:
				if attempt != 1 || attempts.Load() != 0 || auths.Load() != 0 {
					t.Fatal("invalid/expired job authorized", attempt, auths.Load())
				}
			}
		})
	}
}

func TestModerationRiverSnoozeDoesNotConsumeClaimOrAttempts(t *testing.T) {
	f, product, pacer, args := executionFixture(t)
	service := pool(t, f.ModerationWorkerURL)
	competing, err := pacer.Acquire(t.Context(), "client", "123", "123")
	if err != nil || !competing.Granted {
		t.Fatal(err)
	}
	var attempts atomic.Int32
	e, err := NewExecutor(t.Context(), product, pacer, func(context.Context, Target) (Authorization, error) {
		return executionAuthorization(func(ctx context.Context, a platform.ModerationAction) (platform.ModerationReceipt, error) {
			attempts.Add(1)
			return delivered(ctx, a)
		}), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewClient(t.Context(), service, e)
	if err != nil {
		t.Fatal(err)
	}
	events, unsubscribe := c.Subscribe(river.EventKindJobSnoozed)
	defer unsubscribe()
	if err = c.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopModerationClient(t, c) })
	select {
	case <-events:
	case <-time.After(10 * time.Second):
		t.Fatal("no pacing snooze")
	}
	stopModerationClient(t, c)
	var attempt, maxAttempts int
	var scheduled time.Time
	if err = service.QueryRow(`SELECT attempt,max_attempts,scheduled_at FROM river_job WHERE kind='overlaid_moderation'`).Scan(&attempt, &maxAttempts, &scheduled); err != nil {
		t.Fatal(err)
	}
	if attempt != 0 || maxAttempts != 3 || attempts.Load() != 0 || scheduled.After(time.Unix(args.Deadline, 0)) || executionReceipt(t, product).State != "pending" {
		t.Fatal("snooze consumed claim/attempt or renewed deadline", attempt, maxAttempts)
	}
	if err = pacer.Complete(t.Context(), competing.Permit, 0); err != nil {
		t.Fatal(err)
	}
	// Change only the fixture's River wake-up after releasing the competing lease.
	// The restarted executor still rechecks the real cooldown and original deadline.
	if _, err = f.Admin.Exec(`UPDATE river_job SET state='available',scheduled_at=now()+interval '1100 milliseconds' WHERE kind='overlaid_moderation'`); err != nil {
		t.Fatal(err)
	}
	restarted := startModerationClient(t, service, e)
	waitModerationJob(t, service, "completed")
	stopModerationClient(t, restarted)
	if attempts.Load() != 1 {
		t.Fatal("snoozed restart delivery", attempts.Load())
	}
}

func TestModerationRiverTerminalOutcomesNeverRetry(t *testing.T) {
	for _, mode := range []string{"denied", "quota", "retryable", "uncertain", "panic", "completion-failed", "abandoned"} {
		t.Run(mode, func(t *testing.T) {
			f, product, pacer, args := executionFixture(t)
			service := pool(t, f.ModerationWorkerURL)
			var attempts atomic.Int32
			e, err := NewExecutor(t.Context(), product, pacer, func(context.Context, Target) (Authorization, error) {
				if mode == "denied" {
					return Authorization{}, ErrDenied
				}
				return executionAuthorization(func(ctx context.Context, a platform.ModerationAction) (platform.ModerationReceipt, error) {
					attempts.Add(1)
					if mode == "panic" {
						panic("PRIVATE_PANIC")
					}
					r := platform.ModerationReceipt{ActionID: a.ActionID, Status: platform.Uncertain}
					switch mode {
					case "quota":
						r.Status = platform.Rejected
						r.Failure = platform.QuotaLimited
						r.RetryAfter = time.Minute
					case "retryable":
						r.Status = platform.Rejected
						r.Failure = platform.Retryable
					case "completion-failed":
						if _, err := f.Admin.Exec(`CREATE FUNCTION fail_worker_finish() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.state<>'running' THEN RAISE EXCEPTION 'PRIVATE_ERROR'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_worker_finish BEFORE UPDATE ON platform_moderation_actions FOR EACH ROW EXECUTE FUNCTION fail_worker_finish()`); err != nil {
							t.Error(err)
						}
					}
					return r, errors.New("PRIVATE_PROVIDER_ERROR")
				}), nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "abandoned" {
				s, _ := New(product)
				if _, ok, err := s.Start(t.Context(), args.ActionID); err != nil || !ok {
					t.Fatal("abandon claim", ok, err)
				}
			}
			c := startModerationClient(t, service, e)
			waitModerationJob(t, service, "cancelled")
			stopModerationClient(t, c)
			var count int
			var raw string
			if err = service.QueryRow(`SELECT attempt,errors::text FROM river_job WHERE kind='overlaid_moderation'`).Scan(&count, &raw); err != nil {
				t.Fatal(err)
			}
			if count != 1 || strings.Contains(raw, "PRIVATE") {
				t.Fatal("terminal outcome retried/leaked", count)
			}
			expected := int32(1)
			if mode == "denied" || mode == "abandoned" {
				expected = 0
			}
			if attempts.Load() != expected {
				t.Fatal("effect count", attempts.Load(), expected)
			}
			if _, err = f.Admin.Exec(`UPDATE river_job SET state='available',finalized_at=NULL,scheduled_at=now() WHERE kind='overlaid_moderation'`); err != nil {
				t.Fatal(err)
			}
			restarted := startModerationClient(t, service, e)
			waitModerationJob(t, service, "cancelled")
			stopModerationClient(t, restarted)
			if attempts.Load() != expected {
				t.Fatal("terminal effect replayed", attempts.Load())
			}
		})
	}
}

func TestModerationClientRejectsWrongRolesSchemasAndPolicy(t *testing.T) {
	f, product, pacer, _ := executionFixture(t)
	e, err := NewExecutor(t.Context(), product, pacer, func(context.Context, Target) (Authorization, error) { return Authorization{}, ErrDenied })
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewWorker(nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("nil executor worker", err)
	}
	if _, err = NewClient(t.Context(), nil, e); err == nil {
		t.Fatal("nil queue pool accepted")
	}
	if _, err = NewClient(t.Context(), pool(t, f.ModerationWorkerURL), nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("nil executor client", err)
	}
	for _, dsn := range []string{f.RuntimeURL, f.WorkerURL, f.OutboundWorkerURL, f.BotAdminURL, f.BotExecutorURL, f.PacerURL, f.ModerationPacerURL} {
		if _, err = NewClient(t.Context(), pool(t, dsn), e); err == nil {
			t.Fatal("wrong queue role accepted")
		}
	}
	_, foreign, _, _ := executionFixture(t)
	wrong := *e
	wrong.product = foreign
	if _, err = NewClient(t.Context(), pool(t, f.ModerationWorkerURL), &wrong); err == nil {
		t.Fatal("foreign schema accepted")
	}
	u, _ := url.Parse(f.ModerationWorkerURL)
	v, _ := url.Parse(f.OutboundWorkerURL)
	if _, err = f.Admin.Exec(`GRANT ` + v.User.Username() + ` TO ` + u.User.Username() + ` WITH INHERIT FALSE`); err != nil {
		t.Fatal(err)
	}
	if _, err = NewClient(t.Context(), pool(t, f.ModerationWorkerURL), e); err == nil {
		t.Fatal("reachable combined membership accepted")
	}
	if _, err = f.Admin.Exec(`REVOKE ` + v.User.Username() + ` FROM ` + u.User.Username() + `; ALTER POLICY moderation_worker_dispatch ON river_job USING(true) WITH CHECK(true)`); err != nil {
		t.Fatal(err)
	}
	if _, err = NewClient(t.Context(), pool(t, f.ModerationWorkerURL), e); err == nil {
		t.Fatal("changed policy accepted")
	}
}

func TestModerationWorkerInvalidJobsAndRetryDeadline(t *testing.T) {
	// Invalid envelope shape is terminal before the executor is consulted.
	w := &Worker{}
	for _, job := range []*river.Job[platform.ModerationArgs]{nil, {Args: platform.ModerationArgs{Deadline: 0}}, {Args: platform.ModerationArgs{Deadline: 1000000000000}}} {
		var cancelled *river.JobCancelError
		if err := w.Work(t.Context(), job); !errors.As(err, &cancelled) {
			t.Fatal("invalid envelope retried", err)
		}
	}
	for _, deadline := range []time.Time{time.Now().Add(-time.Second), time.Now().Add(500 * time.Millisecond), time.Now().Add(time.Minute)} {
		job := &river.Job[platform.ModerationArgs]{Args: platform.ModerationArgs{Deadline: deadline.Unix()}}
		end := w.NextRetry(job)
		if end.After(time.Unix(job.Args.Deadline, 0)) || end.After(time.Now().Add(time.Second)) {
			t.Fatal("retry renewed original deadline")
		}
	}
}

func TestModerationRiverScopesEachTenant(t *testing.T) {
	f, product, pacer, args := executionFixture(t)
	service := pool(t, f.ModerationWorkerURL)
	if _, err := f.Admin.Exec(`UPDATE platform_connections SET provider_user_id='456' WHERE tenant_id=$1`, otherTenant); err != nil {
		t.Fatal(err)
	}
	other, err := product.Scoped(otherTenant)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	event, err := platform.NewChatEvent(platform.Connection{TenantID: otherTenant, ID: "twitch:456", Platform: "twitch"}, "other-worker-source", now, now, platform.ChatMessage{MessageID: "other-native", Text: "SECOND_PRIVATE_SOURCE", Actor: platform.Identity{ID: "888"}, Channel: platform.Identity{ID: "456"}})
	if err != nil {
		t.Fatal(err)
	}
	ingest, _ := platformingest.New(other)
	if _, err = ingest.IngestChat(t.Context(), event, nil); err != nil {
		t.Fatal(err)
	}
	second, err := platform.NewTwitchModerationIntent(event, "effect", platform.ModerationAction{ChannelID: "456", UserID: "888", Capability: "user.ban", Reason: "SECOND_PRIVATE_REASON"})
	if err != nil {
		t.Fatal(err)
	}
	ledger, _ := New(other)
	secondID := admit(t, ledger, second)
	var attempts atomic.Int32
	e, err := NewExecutor(t.Context(), product, pacer, func(_ context.Context, target Target) (Authorization, error) {
		switch target.TenantID {
		case tenantdb.BootstrapID:
			if target.Action.ActionID != args.ActionID || target.Action.ChannelID != "123" || target.Action.Reason != "PRIVATE_REASON" {
				t.Error("bootstrap scope changed")
			}
		case otherTenant:
			if target.Action.ActionID != secondID || target.Action != second.Action() {
				t.Error("second tenant scope changed")
			}
		default:
			t.Error("unknown tenant")
		}
		a := executionAuthorization(func(ctx context.Context, action platform.ModerationAction) (platform.ModerationReceipt, error) {
			attempts.Add(1)
			if action != target.Action {
				t.Error("cross-tenant callback action")
			}
			return delivered(ctx, action)
		})
		a.ModeratorID = target.Action.ChannelID
		return a, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	c := startModerationClient(t, service, e)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	for {
		var completed int
		if err = service.QueryRowContext(ctx, `SELECT count(*) FROM river_job WHERE kind='overlaid_moderation' AND state='completed'`).Scan(&completed); err != nil {
			t.Fatal(err)
		}
		if completed == 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("two-tenant jobs did not complete")
		case <-time.After(20 * time.Millisecond):
		}
	}
	stopModerationClient(t, c)
	if attempts.Load() != 2 || executionReceipt(t, product).State != "delivered" || executionReceipt(t, other).State != "delivered" {
		t.Fatal("tenant delivery missing", attempts.Load())
	}
}
