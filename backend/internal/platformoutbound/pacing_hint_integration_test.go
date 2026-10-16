package platformoutbound

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/chatpacing"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
)

func TestPacingHintFailureAndCooldownAreUnclaimed(t *testing.T) {
	for _, mode := range []string{"error", "panic", "invalid-sender", "cooldown"} {
		t.Run(mode, func(t *testing.T) {
			f, db, pacer, args := executorFixture(t)
			if mode == "cooldown" {
				decision, err := pacer.Acquire(t.Context(), "123", "789")
				if err != nil || !decision.Granted {
					t.Fatal(err)
				}
				if err := pacer.Complete(t.Context(), decision.Permit, time.Minute); err != nil {
					t.Fatal(err)
				}
			}
			var authorized atomic.Int32
			e, err := NewExecutorWithPacingHint(t.Context(), db, pacer, func(context.Context, Target) (Authorization, error) {
				authorized.Add(1)
				return Authorization{}, ErrDenied
			}, func(context.Context, Target, context.CancelFunc) (func(), error) {
				t.Error("hint acquired send gate")
				return func() {}, nil
			}, func(context.Context, Target) (string, error) {
				switch mode {
				case "error":
					return "", errors.New("private fixture error")
				case "panic":
					panic("private fixture panic")
				case "invalid-sender":
					return "invalid", nil
				}
				return "123", nil
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := e.Execute(t.Context(), args)
			if mode == "cooldown" {
				if err != nil || result.RetryAt.IsZero() || time.Until(result.RetryAt) > time.Second {
					t.Fatal("cooldown hint did not retain short retry", result, err)
				}
			} else if err == nil {
				t.Fatal("hint failure was accepted", result)
			}
			if result.Done || authorized.Load() != 0 {
				t.Fatal("hint crossed authorization or claim", result, authorized.Load())
			}
			var pending bool
			if err := db.QueryRowContext(t.Context(), `SELECT state='pending' AND started_at IS NULL FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/ AND id=$1`, args.ActionID).Scan(&pending); err != nil || !pending {
				t.Fatal("hint consumed action claim", pending, err)
			}
			var accounts, channels, attempts int
			if err := f.Admin.QueryRow(`SELECT (SELECT count(*) FROM chat_pacing_accounts),(SELECT count(*) FROM chat_pacing_channels),(SELECT coalesce(sum(jsonb_array_length(attempts)),0) FROM chat_pacing_accounts)`).Scan(&accounts, &channels, &attempts); err != nil {
				t.Fatal(err)
			}
			want := 0
			if mode == "cooldown" {
				want = 1
			}
			if accounts != want || channels != want || attempts != want {
				t.Fatal("inspection created or charged pacing state", accounts, channels, attempts)
			}
		})
	}
}

func TestPacingHintCannotAuthorizeOrSelectTheActualSender(t *testing.T) {
	for _, deny := range []bool{true, false} {
		t.Run(map[bool]string{true: "denied", false: "changed-sender"}[deny], func(t *testing.T) {
			_, db, pacer, args := executorFixture(t)
			var calls, sends atomic.Int32
			e, err := NewExecutorWithPacingHint(t.Context(), db, pacer, func(context.Context, Target) (Authorization, error) {
				calls.Add(1)
				if deny {
					return Authorization{}, ErrDenied
				}
				return Authorization{SenderID: "999", Binding: "current-binding", Capabilities: fullCapabilities(), Send: func(_ context.Context, m platform.OutboundMessage) (platform.Receipt, error) {
					sends.Add(1)
					return platform.Receipt{ActionID: m.ActionID, Status: platform.Delivered, ProviderMessageID: "hint-current-sender"}, nil
				}}, nil
			}, func(context.Context, Target, context.CancelFunc) (func(), error) { return func() {}, nil }, func(context.Context, Target) (string, error) { return "123", nil })
			if err != nil {
				t.Fatal(err)
			}
			result, err := e.Execute(t.Context(), args)
			if err != nil || !result.Done {
				t.Fatal(result, err)
			}
			if deny {
				if result.Status != platform.Rejected || calls.Load() != 1 || sends.Load() != 0 {
					t.Fatal("clear hint granted authorization", result, calls.Load(), sends.Load())
				}
			} else {
				if result.Status != platform.Delivered || calls.Load() != 2 || sends.Load() != 1 {
					t.Fatal("hint bypassed fresh authorization", result, calls.Load(), sends.Load())
				}
			}
			old, err := pacer.Inspect(t.Context(), "123", "456", time.Time{})
			if err != nil || old.Used != 0 {
				t.Fatal("hint sender received actual pacing charge", old, err)
			}
			current, err := pacer.Inspect(t.Context(), "999", "456", time.Time{})
			want := 0
			if !deny {
				want = 1
			}
			if err != nil || current.Used != want {
				t.Fatal("actual sender pacing mismatch", current, err)
			}
		})
	}
}

func TestPacingHintRetryPolicy(t *testing.T) {
	now := time.Now().UTC()
	for _, test := range []struct {
		name   string
		report chatpacing.Report
		wait   time.Duration
	}{
		{"clear", chatpacing.Report{Ready: true}, 0},
		{"fair-only", chatpacing.Report{Ready: false, Blocking: []string{"fair_admission_required"}}, 0},
		{"cooldown", chatpacing.Report{CooldownWaitMS: 60000}, time.Second},
		{"account-window", chatpacing.Report{BudgetWaitMS: 30000}, time.Second},
		{"in-flight", chatpacing.Report{PermitWaitMS: 30000}, time.Second},
		{"spacing", chatpacing.Report{ChannelWaitMS: 125}, 125 * time.Millisecond},
		{"clock", chatpacing.Report{ClockWaitMS: 1 << 62}, time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.report.ObservedAt = now
			got := pacingRetry(test.report)
			if test.wait == 0 {
				if !got.IsZero() {
					t.Fatal(got)
				}
			} else if !got.Equal(now.Add(test.wait)) {
				t.Fatal(got)
			}
		})
	}
}
