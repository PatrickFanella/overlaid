package platformmoderation

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/moderationpacing"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
)

func executionFixture(t *testing.T) (testpostgres.TenantFixture, *tenantdb.DB, *moderationpacing.Store, platform.ModerationArgs) {
	t.Helper()
	f, db, _ := fixture(t)
	p, err := moderationpacing.Open(t.Context(), f.ModerationPacerURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	s, _ := New(db)
	id := admit(t, s, intent(t, source(t, db, "execute", "111"), "effect"))
	target, _, err := s.target(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return f, db, p, platform.ModerationArgs{TenantID: db.TenantID(), ActionID: id, Deadline: target.Deadline.Unix()}
}

func executionAuthorization(attempt func(context.Context, platform.ModerationAction) (platform.ModerationReceipt, error)) Authorization {
	g := platform.CapabilityGates{Supported: true, Granted: true, RoleAllowed: true, StateAllowed: true, Entitled: true}
	return Authorization{ClientID: "client", ModeratorID: "123", Binding: "selection-revision", Capabilities: platform.EvaluateCapabilities(map[platform.Capability]platform.CapabilityGates{"message.delete": g, "user.timeout": g, "user.ban": g, "user.unban": g}), Attempt: attempt}
}
func executionReceipt(t *testing.T, db *tenantdb.DB) Receipt {
	t.Helper()
	s, _ := New(db)
	rows, err := s.Receipts(t.Context(), 100)
	if err != nil || len(rows) != 1 {
		t.Fatalf("receipt count=%d error=%v", len(rows), err)
	}
	return rows[0]
}
func delivered(_ context.Context, a platform.ModerationAction) (platform.ModerationReceipt, error) {
	return platform.ModerationReceipt{ActionID: a.ActionID, Status: platform.Delivered}, nil
}

func TestExecutorConcurrentClaimAndRestart(t *testing.T) {
	f, db, p, args := executionFixture(t)
	var calls, attempts atomic.Int32
	authorize := func(ctx context.Context, target Target) (Authorization, error) {
		calls.Add(1)
		if target.TenantID != db.TenantID() || target.ConnectionID != "twitch:123" || target.Action.ActionID != args.ActionID || target.Action.Reason != "PRIVATE_REASON" || target.Action.Duration != time.Minute {
			t.Error("private target changed")
		}
		return executionAuthorization(func(ctx context.Context, a platform.ModerationAction) (platform.ModerationReceipt, error) {
			attempts.Add(1)
			var state string
			if err := f.Admin.QueryRow(`SELECT state FROM platform_moderation_actions WHERE id=$1`, a.ActionID).Scan(&state); err != nil || state != "running" {
				t.Error("effect preceded committed claim", err)
			}
			end, ok := ctx.Deadline()
			if !ok || time.Until(end) > 10*time.Second || end.After(target.Deadline) {
				t.Error("unbounded attempt")
			}
			if a != target.Action {
				t.Error("action changed")
			}
			return delivered(ctx, a)
		}), nil
	}
	e, err := NewExecutor(t.Context(), db, p, authorize)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 24 {
		wg.Go(func() {
			if _, err := e.Execute(t.Context(), args); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if attempts.Load() != 1 || calls.Load() < 2 {
		t.Fatal("effect count", attempts.Load(), calls.Load())
	}
	if r := executionReceipt(t, db); r.State != "delivered" {
		t.Fatal(r.State)
	}
	restarted, err := NewExecutor(t.Context(), db, p, authorize)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := restarted.Execute(t.Context(), args); err != nil || !r.Done || attempts.Load() != 1 {
		t.Fatal("restart replay", r, err)
	}
	var echoes int
	if err := f.Admin.QueryRow(`SELECT count(*) FROM twitch_sent_messages`).Scan(&echoes); err != nil || echoes != 0 {
		t.Fatal("moderation invented chat echo", echoes, err)
	}
}

func TestExecutorAuthorizationFence(t *testing.T) {
	for _, mode := range []string{"denied", "capability", "initial-unavailable", "initial-panic", "client-change", "moderator-change", "binding-change", "revoked", "postclaim-unavailable", "postclaim-panic", "postclaim-canceled", "invalid-client", "invalid-moderator", "empty-binding", "nil-attempt", "payload-change", "deadline-change"} {
		t.Run(mode, func(t *testing.T) {
			f, db, p, args := executionFixture(t)
			calls, attempts := 0, 0
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			authorize := func(context.Context, Target) (Authorization, error) {
				calls++
				if calls == 1 && (mode == "payload-change" || mode == "deadline-change") {
					query := `UPDATE platform_moderation_actions SET reason='CHANGED_PRIVATE_REASON' WHERE id=$1`
					if mode == "deadline-change" {
						query = `UPDATE platform_moderation_actions SET deadline=deadline+interval '1 hour' WHERE id=$1`
					}
					if _, err := f.Admin.Exec(query, args.ActionID); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "denied" || mode == "revoked" && calls == 2 {
					return Authorization{}, ErrDenied
				}
				if mode == "initial-unavailable" || mode == "postclaim-unavailable" && calls == 2 {
					return Authorization{}, errors.New("PRIVATE_PROVIDER_ERROR")
				}
				if mode == "initial-panic" || mode == "postclaim-panic" && calls == 2 {
					panic("PRIVATE_PROVIDER_ERROR")
				}
				a := executionAuthorization(func(ctx context.Context, a platform.ModerationAction) (platform.ModerationReceipt, error) {
					attempts++
					return delivered(ctx, a)
				})
				if mode == "capability" {
					a.Capabilities = platform.CapabilityReport{}
				}
				if mode == "invalid-client" {
					a.ClientID = "bad client"
				}
				if mode == "invalid-moderator" {
					a.ModeratorID = "not-numeric"
				}
				if mode == "empty-binding" {
					a.Binding = ""
				}
				if mode == "nil-attempt" {
					a.Attempt = nil
				}
				if calls == 2 {
					switch mode {
					case "client-change":
						a.ClientID = "other-client"
					case "moderator-change":
						a.ModeratorID = "999"
					case "binding-change":
						a.Binding = "other-selection"
					case "postclaim-canceled":
						cancel()
					}
				}
				return a, nil
			}
			e, err := NewExecutor(ctx, db, p, authorize)
			if err != nil {
				t.Fatal(err)
			}
			r, err := e.Execute(ctx, args)
			pending := strings.HasPrefix(mode, "initial-") || strings.HasPrefix(mode, "invalid-") || mode == "empty-binding" || mode == "nil-attempt"
			if r.Done == pending || attempts != 0 || err != nil && strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("unsafe authorization result", r, err, attempts)
			}
			want := "rejected"
			if pending {
				want = "pending"
				if err == nil {
					t.Fatal("unavailable preparation silently accepted")
				}
			}
			if got := executionReceipt(t, db); got.State != want {
				t.Fatal("wrong fence state", got.State, want)
			}
			if !pending && mode != "deadline-change" {
				if next, err := e.Execute(t.Context(), args); err != nil || !next.Done || attempts != 0 {
					t.Fatal("terminal denial replay", next, err)
				}
			}
		})
	}
}

func TestExecutorTypedAttemptOutcomes(t *testing.T) {
	for _, mode := range []string{"success", "quota", "retryable", "permanent", "uncertain", "error", "contradictory", "malformed", "panic", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			_, db, p, args := executionFixture(t)
			attempts := 0
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			e, err := NewExecutor(ctx, db, p, func(context.Context, Target) (Authorization, error) {
				return executionAuthorization(func(ctx context.Context, a platform.ModerationAction) (platform.ModerationReceipt, error) {
					attempts++
					r := platform.ModerationReceipt{ActionID: a.ActionID, Status: platform.Delivered}
					switch mode {
					case "quota":
						r.Status = platform.Rejected
						r.Failure = platform.QuotaLimited
						r.RetryAfter = time.Minute
						return r, errors.New("PRIVATE_ERROR")
					case "retryable":
						r.Status = platform.Rejected
						r.Failure = platform.Retryable
						r.RetryAfter = 2 * time.Second
						return r, nil
					case "permanent":
						r.Status = platform.Rejected
						r.Failure = platform.Permanent
						return r, nil
					case "uncertain":
						r.Status = platform.Uncertain
						return r, nil
					case "error":
						return platform.ModerationReceipt{}, errors.New("PRIVATE_ERROR")
					case "contradictory":
						return r, errors.New("PRIVATE_ERROR")
					case "malformed":
						r.Failure = platform.Permanent
						return r, nil
					case "panic":
						panic("PRIVATE_ERROR")
					case "canceled":
						cancel()
						return platform.ModerationReceipt{}, ctx.Err()
					default:
						return r, nil
					}
				}), nil
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := e.Execute(ctx, args)
			if !result.Done || attempts != 1 || err != nil && strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal(result, attempts, err)
			}
			want := "uncertain"
			switch mode {
			case "success":
				want = "delivered"
			case "quota", "retryable", "permanent":
				want = "rejected"
			}
			if r := executionReceipt(t, db); r.State != want {
				t.Fatal("wrong normalized receipt", r.State, want)
			}
			if next, err := e.Execute(t.Context(), args); err != nil || !next.Done || attempts != 1 {
				t.Fatal("terminal result replayed", next, err)
			}
			if mode == "quota" {
				d, err := p.Acquire(t.Context(), "client", "123", "999")
				if err != nil || d.Granted || time.Until(d.RetryAt) < 50*time.Second {
					t.Fatal("sender cooldown missing", err)
				}
				d, err = p.Acquire(t.Context(), "client", "999", "123")
				if err != nil || d.Granted || time.Until(d.RetryAt) < 50*time.Second {
					t.Fatal("channel cooldown missing", err)
				}
			}
		})
	}
}

func TestExecutorPreparationDeferralAndOriginalDeadline(t *testing.T) {
	f, db, p, args := executionFixture(t)
	attempts := 0
	unavailable := true
	e, err := NewExecutor(t.Context(), db, p, func(context.Context, Target) (Authorization, error) {
		if unavailable {
			return Authorization{}, errors.New("PRIVATE_ERROR")
		}
		return executionAuthorization(func(ctx context.Context, a platform.ModerationAction) (platform.ModerationReceipt, error) {
			attempts++
			return delivered(ctx, a)
		}), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if r, err := e.Execute(t.Context(), args); err == nil || r.Done {
		t.Fatal("preparation consumed claim", r, err)
	}
	unavailable = false
	busy, err := p.Acquire(t.Context(), "client", "123", "123")
	if err != nil || !busy.Granted {
		t.Fatal(err)
	}
	if err = p.Complete(t.Context(), busy.Permit, time.Hour); err != nil {
		t.Fatal(err)
	}
	r, err := e.Execute(t.Context(), args)
	if err != nil || r.Done || r.RetryAt.IsZero() || r.RetryAt.Unix() != args.Deadline || attempts != 0 {
		t.Fatal("unsafe pacing deferral", r, err)
	}
	if row := executionReceipt(t, db); row.State != "pending" {
		t.Fatal(row.State)
	}
	var deadline time.Time
	if err = f.Admin.QueryRow(`UPDATE platform_moderation_actions SET deadline=clock_timestamp()-interval '1 second' WHERE id=$1 RETURNING deadline`, args.ActionID).Scan(&deadline); err != nil {
		t.Fatal(err)
	}
	args.Deadline = deadline.Unix()
	if r, err = e.Execute(t.Context(), args); err != nil || !r.Done || attempts != 0 {
		t.Fatal("expired action attempted", r, err)
	}
	if row := executionReceipt(t, db); row.Code != "expired" {
		t.Fatal(row.Code)
	}
}

func TestExecutorCompletionFailureCannotReplay(t *testing.T) {
	for _, mode := range []string{"ledger", "pacing", "start", "revoked", "deleted"} {
		t.Run(mode, func(t *testing.T) {
			f, db, p, args := executionFixture(t)
			attempts := 0
			calls := 0
			if mode == "start" {
				if _, err := f.Admin.Exec(`CREATE FUNCTION fail_start() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.state='running' THEN RAISE EXCEPTION 'PRIVATE_ERROR'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_start BEFORE UPDATE ON platform_moderation_actions FOR EACH ROW EXECUTE FUNCTION fail_start()`); err != nil {
					t.Fatal(err)
				}
			}
			e, err := NewExecutor(t.Context(), db, p, func(context.Context, Target) (Authorization, error) {
				calls++
				if calls == 1 {
					switch mode {
					case "revoked":
						if _, err := f.Admin.Exec(`UPDATE platform_connections SET revoked_at=clock_timestamp()`); err != nil {
							t.Fatal(err)
						}
					case "deleted":
						if _, err := f.Admin.Exec(`DELETE FROM normalized_chat_events`); err != nil {
							t.Fatal(err)
						}
					}
				}
				return executionAuthorization(func(ctx context.Context, a platform.ModerationAction) (platform.ModerationReceipt, error) {
					attempts++
					switch mode {
					case "ledger":
						if _, err := f.Admin.Exec(`CREATE FUNCTION fail_finish() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.state='delivered' THEN RAISE EXCEPTION 'PRIVATE_ERROR'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_finish BEFORE UPDATE ON platform_moderation_actions FOR EACH ROW EXECUTE FUNCTION fail_finish()`); err != nil {
							t.Fatal(err)
						}
					case "pacing":
						if err := p.Close(); err != nil {
							t.Fatal(err)
						}
					}
					return delivered(ctx, a)
				}), nil
			})
			if err != nil {
				t.Fatal(err)
			}
			r, err := e.Execute(t.Context(), args)
			if mode == "start" || mode == "deleted" {
				if err == nil || r.Done || attempts != 0 {
					t.Fatal("failed claim exposed action", r, err)
				}
				return
			}
			if !r.Done || mode != "revoked" && (err == nil || attempts != 1) {
				t.Fatal("completion lost terminal fence", r, err)
			}
			if _, err = e.Execute(t.Context(), args); err != nil || attempts > 1 {
				t.Fatal("failed completion replayed", err)
			}
			if mode == "ledger" {
				if row := executionReceipt(t, db); row.State != "running" {
					t.Fatal(row.State)
				}
				if _, err = f.Admin.Exec(`DROP TRIGGER fail_finish ON platform_moderation_actions; UPDATE platform_moderation_actions SET deadline=clock_timestamp()-interval '1 second'`); err != nil {
					t.Fatal(err)
				}
				var end time.Time
				if err = f.Admin.QueryRow(`SELECT deadline FROM platform_moderation_actions`).Scan(&end); err != nil {
					t.Fatal(err)
				}
				args.Deadline = end.Unix()
				if r, err = e.Execute(t.Context(), args); err != nil || !r.Done || attempts != 1 {
					t.Fatal("recovery replay", r, err)
				}
				if row := executionReceipt(t, db); row.State != "uncertain" {
					t.Fatal("recovery not terminal", row.State)
				}
			}
		})
	}
}

func TestExecutorInvalidJobsAndPairing(t *testing.T) {
	_, db, p, args := executionFixture(t)
	calls := 0
	auth := func(context.Context, Target) (Authorization, error) {
		calls++
		return executionAuthorization(delivered), nil
	}
	e, err := NewExecutor(t.Context(), db, p, auth)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*platform.ModerationArgs){func(a *platform.ModerationArgs) { a.Deadline++ }, func(a *platform.ModerationArgs) { a.TenantID = "bad" }, func(a *platform.ModerationArgs) { a.ActionID = "bad" }} {
		bad := args
		change(&bad)
		if _, err = e.Execute(t.Context(), bad); !errors.Is(err, ErrInvalid) {
			t.Fatal("bad job accepted", err)
		}
	}
	missing := args
	missing.ActionID = strings.Repeat("0", 64)
	if r, err := e.Execute(t.Context(), missing); err != nil || !r.Done {
		t.Fatal("deleted action not terminal", r, err)
	}
	foreign := args
	foreign.TenantID = otherTenant
	if r, err := e.Execute(t.Context(), foreign); err != nil || !r.Done {
		t.Fatal("foreign action visible", r, err)
	}
	if calls != 0 {
		t.Fatal("invalid/deleted/foreign job authorized")
	}
	for _, build := range []func() (*Executor, error){func() (*Executor, error) { return NewExecutor(t.Context(), nil, p, auth) }, func() (*Executor, error) { return NewExecutor(t.Context(), tenantdb.Legacy(nil), p, auth) }, func() (*Executor, error) { return NewExecutor(t.Context(), db, nil, auth) }, func() (*Executor, error) { return NewExecutor(t.Context(), db, p, nil) }} {
		if _, err = build(); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid executor constructed", err)
		}
	}
	other := testpostgres.NewTenantFixture(t, "")
	wrong, err := moderationpacing.Open(t.Context(), other.ModerationPacerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = wrong.Close() }()
	if _, err = NewExecutor(t.Context(), db, wrong, auth); err == nil {
		t.Fatal("cross-schema pools accepted")
	}
}
