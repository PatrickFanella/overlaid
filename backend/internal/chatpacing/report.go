package chatpacing

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

var ErrReportUnavailable = errors.New("pacing report unavailable")

// Report is a read-only point-in-time policy view, not an authorization or
// permit. It deliberately excludes sender/channel keys and private lease data.
type Report struct {
	ObservedAt           time.Time  `json:"observed_at"`
	Ready                bool       `json:"ready"`
	RetryAt              time.Time  `json:"retry_at"`
	AccountLimit         int        `json:"account_limit"`
	AccountWindowSeconds int        `json:"account_window_seconds"`
	Used                 int        `json:"used"`
	Remaining            int        `json:"remaining"`
	ChannelWaitMS        int64      `json:"channel_wait_ms"`
	PermitWaitMS         int64      `json:"permit_wait_ms"`
	CooldownWaitMS       int64      `json:"cooldown_wait_ms"`
	BudgetWaitMS         int64      `json:"budget_wait_ms"`
	ClockWaitMS          int64      `json:"clock_wait_ms"`
	Deadline             *time.Time `json:"deadline,omitempty"`
	DeadlineAtRisk       bool       `json:"deadline_at_risk"`
	Blocking             []string   `json:"blocking"`
	Warnings             []string   `json:"warnings"`
}

// Inspect starts an explicitly read-only transaction. It creates no state,
// prunes no history, holds no admission lock, and never reserves/refunds a slot.
// Deadline is the original action deadline; zero omits that optional check.
func (s *Store) Inspect(ctx context.Context, sender, channel string, deadline time.Time) (Report, error) {
	if !validID(sender) || !validID(channel) {
		return Report{}, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	fail := func() (Report, error) {
		if ctx.Err() != nil {
			return Report{}, errors.Join(ErrReportUnavailable, ctx.Err())
		}
		return Report{}, ErrReportUnavailable
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fail()
	}
	defer func() { _ = tx.Rollback() }()
	account, key := keys(sender, channel)
	var now, next time.Time
	var state accountState
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT clock_timestamp(),coalesce(a.attempts,'[]'::jsonb),
 greatest(coalesce(a.blocked_until,'-infinity'::timestamptz),'0001-01-01 UTC'::timestamptz),
 greatest(coalesce(a.lease_until,'-infinity'::timestamptz),'0001-01-01 UTC'::timestamptz),
 greatest(coalesce(c.next_at,'-infinity'::timestamptz),'0001-01-01 UTC'::timestamptz)
 FROM (SELECT $1::text account_key,$2::text channel_key) k
 LEFT JOIN chat_pacing_accounts a ON a.account_key=k.account_key
 LEFT JOIN chat_pacing_channels c ON c.account_key=k.account_key AND c.channel_key=k.channel_key`, account, key).Scan(&now, &raw, &state.BlockedUntil, &state.LeaseUntil, &next)
	if err != nil {
		return fail()
	}
	state.Attempts, err = decodeAttempts(raw)
	if err != nil {
		return fail()
	}
	result := reportAt(now, state, next, deadline)
	if err = tx.Commit(); err != nil {
		return fail()
	}
	return result, nil
}

func decodeAttempts(raw []byte) ([]int64, error) {
	var attempts []int64
	if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '[' || json.Unmarshal(trimmed, &attempts) != nil || len(attempts) > accountLimit {
		return nil, ErrInvalid
	}
	for i, at := range attempts {
		if at <= 0 || i > 0 && at < attempts[i-1] {
			return nil, ErrInvalid
		}
	}
	return attempts, nil
}
func waitMS(now, at time.Time) int64 {
	d := at.Sub(now)
	if d <= 0 {
		return 0
	}
	n := int64(d / time.Millisecond)
	if d%time.Millisecond != 0 {
		n++
	}
	return n
}
func reportAt(now time.Time, state accountState, channelNext, deadline time.Time) Report {
	// readyAt prunes in memory; do not alter a caller's attempt slice.
	state.Attempts = append([]int64(nil), state.Attempts...)
	retry := state.readyAt(now, channelNext)
	result := Report{ObservedAt: now.UTC(), Ready: !retry.After(now), RetryAt: retry.UTC(), AccountLimit: accountLimit, AccountWindowSeconds: int(accountWindow / time.Second), Used: len(state.Attempts), Remaining: accountLimit - len(state.Attempts), Blocking: []string{}, Warnings: []string{}}
	result.ChannelWaitMS = waitMS(now, channelNext)
	result.PermitWaitMS = waitMS(now, state.LeaseUntil)
	result.CooldownWaitMS = waitMS(now, state.BlockedUntil)
	if len(state.Attempts) >= accountLimit {
		result.BudgetWaitMS = waitMS(now, time.UnixMicro(state.Attempts[0]).Add(accountWindow))
	}
	if n := len(state.Attempts); n > 0 && time.UnixMicro(state.Attempts[n-1]).After(now) {
		result.ClockWaitMS = waitMS(now, time.UnixMicro(state.Attempts[n-1]).Add(accountWindow))
	}
	for _, v := range []struct {
		name  string
		delay int64
	}{{"account_budget", result.BudgetWaitMS}, {"channel_spacing", result.ChannelWaitMS}, {"in_flight", result.PermitWaitMS}, {"provider_cooldown", result.CooldownWaitMS}, {"clock_anomaly", result.ClockWaitMS}} {
		if v.delay > 0 {
			result.Blocking = append(result.Blocking, v.name)
		}
	}
	if result.Remaining <= accountLimit/5 {
		result.Warnings = append(result.Warnings, "low_headroom")
	}
	if result.CooldownWaitMS > 0 {
		result.Warnings = append(result.Warnings, "provider_cooldown")
	}
	if result.ClockWaitMS > 0 {
		result.Warnings = append(result.Warnings, "clock_anomaly")
	}
	if !deadline.IsZero() {
		d := deadline.UTC()
		result.Deadline = &d
		result.DeadlineAtRisk = !retry.Before(deadline)
		if result.DeadlineAtRisk {
			result.Ready = false
			result.Warnings = append(result.Warnings, "deadline_risk")
		}
	}
	return result
}
