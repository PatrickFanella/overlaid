package platformmoderation

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
)

func TestModerationControlledLeasingAndRestart(t *testing.T) {
	f, product, pacer, args := executionFixture(t)
	service := pool(t, f.ModerationWorkerURL)
	var ready atomic.Bool
	var auths, attempts atomic.Int32
	closedPolls := make(chan struct{}, 32)
	active := func() bool {
		if ready.Load() {
			return true
		}
		select {
		case closedPolls <- struct{}{}:
		default:
		}
		return false
	}
	waitClosedPolls := func() {
		t.Helper()
		// Observe actual gated fetches rather than assuming a wall-clock pause
		// implies that River had an opportunity to lease the pending job.
		for range 3 {
			select {
			case <-closedPolls:
			case <-time.After(10 * time.Second):
				t.Fatal("no closed-gate fetch observed")
			}
		}
	}
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	e, err := NewExecutor(t.Context(), product, pacer, func(context.Context, Target) (Authorization, error) {
		auths.Add(1)
		return executionAuthorization(func(ctx context.Context, action platform.ModerationAction) (platform.ModerationReceipt, error) {
			attempts.Add(1)
			if action.ActionID == args.ActionID {
				ready.Store(false)
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
					return platform.ModerationReceipt{}, ctx.Err()
				}
			}
			return delivered(ctx, action)
		}), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	assertPending := func(id string, deadline int64) {
		t.Helper()
		var state string
		var attempt int
		var savedDeadline int64
		if err := service.QueryRow(`SELECT state,attempt,(args->>'deadline')::bigint FROM river_job WHERE kind='overlaid_moderation' AND args->>'action_id'=$1`, id).Scan(&state, &attempt, &savedDeadline); err != nil {
			t.Fatal(err)
		}
		if state != "available" || attempt != 0 || savedDeadline != deadline {
			t.Fatal("closed gate changed pending job", state, attempt, savedDeadline, deadline)
		}
		var ledgerState string
		if err := f.Admin.QueryRow(`SELECT state FROM platform_moderation_actions WHERE id=$1`, id).Scan(&ledgerState); err != nil || ledgerState != "pending" {
			t.Fatal("closed gate claimed action", ledgerState, err)
		}
	}
	c, err := NewControlledClient(t.Context(), service, e, active)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopModerationClient(t, c) })
	waitClosedPolls()
	assertPending(args.ActionID, args.Deadline)
	var buckets int
	if err = f.Admin.QueryRow(`SELECT count(*) FROM moderation_pacing_buckets`).Scan(&buckets); err != nil || buckets != 0 || auths.Load() != 0 || attempts.Load() != 0 {
		t.Fatal("closed gate reached preparation or pacing", buckets, auths.Load(), attempts.Load(), err)
	}
	ready.Store(true)
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("open gate did not deliver")
	}
	// Closing readiness cannot retract the already claimed first action. Admit
	// the next job only after closing it, so no earlier batch can contain it.
	ledger, _ := New(product)
	second := intent(t, source(t, product, "controlled-second", "111"), "effect")
	secondID := admit(t, ledger, second)
	var secondDeadline int64
	if err = service.QueryRow(`SELECT (args->>'deadline')::bigint FROM river_job WHERE kind='overlaid_moderation' AND args->>'action_id'=$1`, secondID).Scan(&secondDeadline); err != nil {
		t.Fatal(err)
	}
	waitClosedPolls()
	assertPending(secondID, secondDeadline)
	if attempts.Load() != 1 || auths.Load() != 2 {
		t.Fatal("closed gate leased more work", attempts.Load(), auths.Load())
	}
	close(release)
	waitCompleted := func(want int) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		for {
			var count int
			if err := service.QueryRowContext(ctx, `SELECT count(*) FROM river_job WHERE kind='overlaid_moderation' AND state='completed'`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count == want {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatal("moderation did not settle", count, want)
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	waitCompleted(1)
	stopModerationClient(t, c)
	// A new client retains the closed startup barrier and original work.
	restarted, err := NewControlledClient(t.Context(), service, e, active)
	if err != nil {
		t.Fatal(err)
	}
	if err = restarted.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopModerationClient(t, restarted) })
	// Drain observations from the stopped client before checking this client.
	for len(closedPolls) > 0 {
		<-closedPolls
	}
	waitClosedPolls()
	assertPending(secondID, secondDeadline)
	ready.Store(true)
	waitCompleted(2)
	stopModerationClient(t, restarted)
	if attempts.Load() != 2 || auths.Load() != 4 {
		t.Fatal("restart replayed or skipped work", attempts.Load(), auths.Load())
	}
	var deliveredCount, leasedBuckets int
	if err = f.Admin.QueryRow(`SELECT count(*) FROM platform_moderation_actions WHERE state='delivered'`).Scan(&deliveredCount); err != nil || deliveredCount != 2 {
		t.Fatal("missing durable receipts", deliveredCount, err)
	}
	if err = f.Admin.QueryRow(`SELECT count(*) FROM moderation_pacing_buckets WHERE lease_hash IS NOT NULL`).Scan(&leasedBuckets); err != nil || leasedBuckets != 0 {
		t.Fatal("permits not released", leasedBuckets, err)
	}
}

func TestModerationControlledClientRequiresReadiness(t *testing.T) {
	if _, err := NewControlledClient(t.Context(), (*sql.DB)(nil), nil, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("nil readiness accepted", err)
	}
}
