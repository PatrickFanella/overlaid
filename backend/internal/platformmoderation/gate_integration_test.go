package platformmoderation

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
)

func TestAttemptGateFailurePreservesPreclaimFence(t *testing.T) {
	for _, mode := range []string{"error", "error-with-release", "panic", "nil-release", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			f, db, pacer, args := executionFixture(t)
			var attempts, releases atomic.Int32
			e, err := NewExecutorWithGate(t.Context(), db, pacer, func(context.Context, Target) (Authorization, error) {
				return executionAuthorization(func(ctx context.Context, a platform.ModerationAction) (platform.ModerationReceipt, error) {
					attempts.Add(1)
					return delivered(ctx, a)
				}), nil
			}, func(_ context.Context, _ Target, cancel context.CancelFunc) (func(), error) {
				switch mode {
				case "error":
					return nil, errors.New("PRIVATE_GATE_ERROR")
				case "error-with-release":
					return func() { releases.Add(1) }, errors.New("PRIVATE_GATE_ERROR")
				case "panic":
					panic("PRIVATE_GATE_PANIC")
				case "nil-release":
					return nil, nil
				default:
					cancel()
					return func() {}, nil
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := e.Execute(t.Context(), args)
			if err == nil || strings.Contains(err.Error(), "PRIVATE") || result.Done || attempts.Load() != 0 {
				t.Fatal("gate crossed claim or leaked failure", result, attempts.Load(), err)
			}
			if mode == "error-with-release" && releases.Load() != 1 {
				t.Fatal("failed gate leaked its returned release", releases.Load())
			}
			var pending bool
			if err := db.QueryRowContext(t.Context(), `SELECT state='pending' AND started_at IS NULL FROM platform_moderation_actions WHERE /*tenant:platform_moderation_actions*/ AND id=$1`, args.ActionID).Scan(&pending); err != nil || !pending {
				t.Fatal("failed gate claimed action", pending, err)
			}
			var charged, leases int
			if err := f.Admin.QueryRow(`SELECT count(*) FILTER(WHERE next_at>'-infinity'::timestamptz),count(*) FILTER(WHERE lease_until IS NOT NULL) FROM moderation_pacing_buckets`).Scan(&charged, &leases); err != nil || charged != 2 || leases != 0 {
				t.Fatal("failed gate refunded or leaked permit", charged, leases, err)
			}
		})
	}
}

func TestAttemptGateCancellationAndReleasePanicNeverReplay(t *testing.T) {
	for _, mode := range []string{"cancel-in-attempt", "release-panic"} {
		t.Run(mode, func(t *testing.T) {
			_, db, pacer, args := executionFixture(t)
			var attempts atomic.Int32
			var cancelAttempt context.CancelFunc
			e, err := NewExecutorWithGate(t.Context(), db, pacer, func(context.Context, Target) (Authorization, error) {
				return executionAuthorization(func(ctx context.Context, a platform.ModerationAction) (platform.ModerationReceipt, error) {
					attempts.Add(1)
					if mode == "cancel-in-attempt" {
						cancelAttempt()
						<-ctx.Done()
						return platform.ModerationReceipt{ActionID: a.ActionID, Status: platform.Uncertain}, ctx.Err()
					}
					return delivered(ctx, a)
				}), nil
			}, func(_ context.Context, _ Target, cancel context.CancelFunc) (func(), error) {
				cancelAttempt = cancel
				return func() {
					if mode == "release-panic" {
						panic("PRIVATE_RELEASE_PANIC")
					}
				}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := e.Execute(t.Context(), args)
			if !result.Done || attempts.Load() != 1 || err != nil && strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("gate completion lost terminal fence", result, attempts.Load(), err)
			}
			want := "uncertain"
			if mode == "release-panic" {
				want = "delivered"
				if err == nil {
					t.Fatal("release panic not reported")
				}
			}
			if receipt := executionReceipt(t, db); receipt.State != want {
				t.Fatal(receipt.State, want)
			}
			again, err := e.Execute(t.Context(), args)
			if err != nil || !again.Done || attempts.Load() != 1 {
				t.Fatal("terminal gate outcome replayed", again, attempts.Load(), err)
			}
		})
	}
}
