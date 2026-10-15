package chatpacing

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
)

func TestInspectIsReadOnlyAndPreservesPermitCompletion(t *testing.T) {
	f := testpostgres.NewTenantFixture(t, "")
	s, err := Open(t.Context(), f.PacerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	snapshot := func() string {
		t.Helper()
		var raw string
		if err := f.Admin.QueryRow(`SELECT jsonb_build_array((SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY account_key),'[]') FROM chat_pacing_accounts a),(SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY account_key,channel_key),'[]') FROM chat_pacing_channels c))::text`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	before := snapshot()
	report, err := s.Inspect(t.Context(), "123", "456", time.Time{})
	if err != nil || !report.Ready || report.Remaining != 20 || snapshot() != before {
		t.Fatal("empty inspection created state", report, err)
	}
	permit, err := s.Acquire(t.Context(), "123", "456")
	if err != nil || !permit.Granted {
		t.Fatal(err)
	}
	before = snapshot()
	for range 3 {
		report, err = s.Inspect(t.Context(), "123", "456", time.Now().Add(5*time.Second))
		if err != nil || report.Ready || report.Used != 1 || report.PermitWaitMS <= 0 || report.ChannelWaitMS <= 0 || !report.DeadlineAtRisk || snapshot() != before {
			t.Fatal("inspection changed active pacing", report, err)
		}
	}
	if err = s.Complete(t.Context(), permit.Permit, 42*time.Second); err != nil {
		t.Fatal("inspection invalidated permit", err)
	}
	restarted, err := Open(t.Context(), f.PacerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restarted.Close() }()
	report, err = restarted.Inspect(t.Context(), "123", "789", time.Now().Add(10*time.Second))
	if err != nil || report.Ready || report.CooldownWaitMS < 40000 || report.PermitWaitMS != 0 || !report.DeadlineAtRisk {
		t.Fatal("restart/cross-channel cooldown report", report, err)
	}
	independent, err := restarted.Inspect(t.Context(), "999", "456", time.Time{})
	if err != nil || !independent.Ready || independent.Remaining != 20 {
		t.Fatal("independent sender", independent, err)
	}
	if _, err = s.Inspect(t.Context(), "invalid", "456", time.Time{}); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid identity accepted", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = s.Inspect(ctx, "123", "456", time.Time{}); !errors.Is(err, context.Canceled) || !errors.Is(err, ErrReportUnavailable) {
		t.Fatal("cancellation identity lost", err)
	}
}

func TestInspectLowHeadroomExpiredWindowAndMalformedState(t *testing.T) {
	f := testpostgres.NewTenantFixture(t, "")
	s, err := Open(t.Context(), f.PacerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	permit, err := s.Acquire(t.Context(), "123", "456")
	if err != nil || !permit.Granted {
		t.Fatal(err)
	}
	if err = s.Complete(t.Context(), permit.Permit, 0); err != nil {
		t.Fatal(err)
	}
	setAttempts := func(count int, age time.Duration) {
		t.Helper()
		var now time.Time
		if err := f.Admin.QueryRow(`SELECT clock_timestamp()`).Scan(&now); err != nil {
			t.Fatal(err)
		}
		values := make([]int64, count)
		for i := range values {
			values[i] = now.Add(-age).UnixMicro()
		}
		raw, _ := json.Marshal(values)
		if _, err := f.Admin.Exec(`UPDATE chat_pacing_accounts SET attempts=$1,blocked_until='-infinity',lease_hash=NULL,lease_until=NULL`, raw); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Admin.Exec(`UPDATE chat_pacing_channels SET next_at='-infinity'`); err != nil {
			t.Fatal(err)
		}
	}
	setAttempts(16, time.Second)
	report, err := s.Inspect(t.Context(), "123", "456", time.Time{})
	if err != nil || !report.Ready || report.Remaining != 4 || len(report.Warnings) != 1 || report.Warnings[0] != "low_headroom" {
		t.Fatal("early headroom warning", report, err)
	}
	setAttempts(20, time.Second)
	report, err = s.Inspect(t.Context(), "123", "456", time.Now().Add(time.Second))
	if err != nil || report.Ready || report.BudgetWaitMS < 28000 || !report.DeadlineAtRisk {
		t.Fatal("full rolling budget", report, err)
	}
	setAttempts(20, time.Minute)
	report, err = s.Inspect(t.Context(), "123", "456", time.Time{})
	if err != nil || !report.Ready || report.Used != 0 || report.Remaining != 20 {
		t.Fatal("expired window not derived", report, err)
	}
	var stored int
	if err = f.Admin.QueryRow(`SELECT jsonb_array_length(attempts) FROM chat_pacing_accounts`).Scan(&stored); err != nil || stored != 20 {
		t.Fatal("inspection pruned retained attempts", stored, err)
	}
	if _, err = f.Admin.Exec(`UPDATE chat_pacing_accounts SET attempts='[2,1]'`); err != nil {
		t.Fatal(err)
	}
	if report, err = s.Inspect(t.Context(), "123", "456", time.Time{}); !errors.Is(err, ErrReportUnavailable) || report.Ready {
		t.Fatal("malformed state became clear", report, err)
	}
}
