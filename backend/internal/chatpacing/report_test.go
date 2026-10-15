package chatpacing

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestReportUsesAdmissionPolicyAndPreservesState(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	full := make([]int64, accountLimit)
	for i := range full {
		full[i] = now.Add(-time.Second).UnixMicro()
	}
	for _, tc := range []struct {
		name               string
		state              accountState
		channel, deadline  time.Time
		ready              bool
		remaining          int
		blocking, warnings []string
	}{
		{name: "empty", ready: true, remaining: 20, blocking: []string{}, warnings: []string{}},
		{name: "spacing", channel: now.Add(time.Second), remaining: 20, blocking: []string{"channel_spacing"}, warnings: []string{}},
		{name: "permit", state: accountState{Attempts: []int64{now.UnixMicro()}, LeaseUntil: now.Add(30 * time.Second)}, remaining: 19, blocking: []string{"in_flight"}, warnings: []string{}},
		{name: "cooldown", state: accountState{BlockedUntil: now.Add(time.Minute)}, remaining: 20, blocking: []string{"provider_cooldown"}, warnings: []string{"provider_cooldown"}},
		{name: "full", state: accountState{Attempts: full}, remaining: 0, blocking: []string{"account_budget"}, warnings: []string{"low_headroom"}},
		{name: "warning-before-limit", state: accountState{Attempts: full[:16]}, ready: true, remaining: 4, blocking: []string{}, warnings: []string{"low_headroom"}},
		{name: "window-edge", state: accountState{Attempts: []int64{now.Add(-accountWindow).UnixMicro()}}, ready: true, remaining: 20, blocking: []string{}, warnings: []string{}},
		{name: "clock-backwards", state: accountState{Attempts: []int64{now.Add(time.Second).UnixMicro()}}, remaining: 19, blocking: []string{"clock_anomaly"}, warnings: []string{"clock_anomaly"}},
		{name: "deadline-equals-retry", channel: now.Add(time.Second), deadline: now.Add(time.Second), remaining: 20, blocking: []string{"channel_spacing"}, warnings: []string{"deadline_risk"}},
		{name: "expired", deadline: now.Add(-time.Second), remaining: 20, blocking: []string{}, warnings: []string{"deadline_risk"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := append([]int64(nil), tc.state.Attempts...)
			report := reportAt(now, tc.state, tc.channel, tc.deadline)
			copyState := tc.state
			copyState.Attempts = append([]int64(nil), tc.state.Attempts...)
			retry := copyState.readyAt(now, tc.channel)
			if report.Ready != tc.ready || report.Remaining != tc.remaining || !report.RetryAt.Equal(retry) || !reflect.DeepEqual(report.Blocking, tc.blocking) || !reflect.DeepEqual(report.Warnings, tc.warnings) {
				t.Fatalf("unexpected report %#v", report)
			}
			if !reflect.DeepEqual(tc.state.Attempts, original) {
				t.Fatal("inspection modified source attempts")
			}
			if !tc.deadline.IsZero() && report.DeadlineAtRisk != !retry.Before(tc.deadline) {
				t.Fatal("deadline comparison")
			}
		})
	}
	if waitMS(now, now.Add(time.Nanosecond)) != 1 || waitMS(now, now.Add(-time.Second)) != 0 {
		t.Fatal("submillisecond wait rounded incorrectly")
	}
}
func TestReportRejectsMalformedAttemptsAndHasFixedFields(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `[0]`, `[-1]`, `[2,1]`, `[1.5]`, `[9223372036854775808]`} {
		if _, err := decodeAttempts([]byte(raw)); err == nil {
			t.Fatal("invalid attempts accepted", raw)
		}
	}
	raw, _ := json.Marshal(reportAt(time.Now(), accountState{}, time.Time{}, time.Time{}))
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	expected := []string{"observed_at", "ready", "retry_at", "account_limit", "account_window_seconds", "used", "remaining", "channel_wait_ms", "permit_wait_ms", "cooldown_wait_ms", "budget_wait_ms", "clock_wait_ms", "deadline_at_risk", "blocking", "warnings"}
	if len(fields) != len(expected) {
		t.Fatal("report field count", string(raw))
	}
	for _, key := range expected {
		if _, ok := fields[key]; !ok {
			t.Fatal("missing fixed field", key)
		}
	}
}
