package chatpacing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
)

func TestFairPermitsConcurrentPoolsRestartAndInspection(t *testing.T) {
	f := testpostgres.NewTenantFixture(t, "")
	s, err := Open(t.Context(), f.PacerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	other, err := Open(t.Context(), f.PacerURL)
	if err != nil {
		t.Fatal(err)
	}
	busy, err := s.Acquire(t.Context(), "123", "456")
	if err != nil || !busy.Granted {
		t.Fatal("initial fence", err)
	}
	var now time.Time
	if err = f.Admin.QueryRow(`SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	requests := []FairRequest{}
	for _, tenant := range []int{1, 2} {
		for i := range 3 {
			r := fairRequest(tenant, tenant*10+i, now.Add(time.Minute))
			requests = append(requests, r)
			d, err := s.AcquireFair(t.Context(), "123", fmt.Sprint(tenant*10+i), r)
			if err != nil || d.Granted || !d.RetryAt.After(now) || d.RetryAt.After(now.Add(2*time.Second)) {
				t.Fatal("blocked registration", d, err)
			}
		}
	}
	if err = other.Close(); err != nil {
		t.Fatal(err)
	}
	other, err = Open(t.Context(), f.PacerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Close() }()
	if _, err = other.Acquire(t.Context(), "123", "789"); !errors.Is(err, ErrFairRequired) {
		t.Fatal("legacy bypass", err)
	}
	snapshot := func() string {
		var raw string
		if err := f.Admin.QueryRow(`SELECT jsonb_agg(to_jsonb(a))::text FROM chat_pacing_accounts a`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	before := snapshot()
	report, err := other.Inspect(t.Context(), "123", "789", now.Add(time.Minute))
	if err != nil || report.Ready || !strings.Contains(fmt.Sprint(report.Blocking), "fair_admission_required") {
		t.Fatal("fair report", report, err)
	}
	if snapshot() != before {
		t.Fatal("inspection wrote fair state")
	}
	// Denials only registered hashes; the original held lease is the sole charge.
	var attempts int
	if err = f.Admin.QueryRow(`SELECT jsonb_array_length(attempts) FROM chat_pacing_accounts`).Scan(&attempts); err != nil || attempts != 1 {
		t.Fatal("denial charged a slot", attempts, err)
	}
	if err = s.Complete(t.Context(), busy.Permit, 0); err != nil {
		t.Fatal(err)
	}
	var got []int
	for range 6 {
		type acquired struct {
			index  int
			permit Permit
		}
		grants := make(chan acquired, len(requests)*2)
		var wg sync.WaitGroup
		for i, r := range requests {
			for _, pool := range []*Store{s, other} {
				wg.Add(1)
				go func(i int, r FairRequest, pool *Store) {
					defer wg.Done()
					d, err := pool.AcquireFair(t.Context(), "123", fmt.Sprint(i/3*10+10+i%3), r)
					if err != nil {
						t.Error(err)
						return
					}
					if d.Granted {
						grants <- acquired{i, d.Permit}
					}
				}(i, r, pool)
			}
		}
		wg.Wait()
		close(grants)
		if len(grants) != 1 {
			t.Fatalf("expected one fenced grant, got %d", len(grants))
		}
		g := <-grants
		got = append(got, g.index/3+1)
		if err = other.Complete(t.Context(), g.permit, 0); err != nil {
			t.Fatal(err)
		}
	}
	if fmt.Sprint(got) != "[1 2 1 2 1 2]" {
		t.Fatal("tenant turns lost", got)
	}
	d, err := s.AcquireFair(t.Context(), "123", "10", requests[0])
	if err != nil || d.Granted {
		t.Fatal("duplicate permit granted", err)
	}
	raw := snapshot()
	for _, r := range requests {
		if strings.Contains(raw, r.TenantID) || strings.Contains(raw, r.ActionID) {
			t.Fatal("raw retained identity leaked")
		}
	}
}
func TestFairPermitsDeadlineConflictCancellationAndCapacity(t *testing.T) {
	f := testpostgres.NewTenantFixture(t, "")
	s, err := Open(t.Context(), f.PacerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	busy, err := s.Acquire(t.Context(), "123", "456")
	if err != nil || !busy.Granted {
		t.Fatal(err)
	}
	var now time.Time
	if err = f.Admin.QueryRow(`SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	r := fairRequest(1, 1, now.Add(time.Minute))
	if _, err = s.AcquireFair(t.Context(), "123", "111", r); err != nil {
		t.Fatal(err)
	}
	changed := r
	changed.Deadline = changed.Deadline.Add(time.Second)
	if _, err = s.AcquireFair(t.Context(), "123", "111", changed); !errors.Is(err, ErrInvalid) {
		t.Fatal("deadline conflict", err)
	}
	if _, err = s.AcquireFair(t.Context(), "123", "222", r); !errors.Is(err, ErrInvalid) {
		t.Fatal("channel conflict", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = s.AcquireFair(ctx, "123", "111", fairRequest(2, 2, r.Deadline)); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation identity", err)
	}
	// Expire a missing caller in synthetic state; the next action must progress
	// without refunding or clearing its separate held pacing permit.
	var raw []byte
	if err = f.Admin.QueryRow(`SELECT fair_state FROM chat_pacing_accounts`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	state, err := decodeFair(raw)
	if err != nil {
		t.Fatal(err)
	}
	state.Waiters[0].Deadline = now.Add(-time.Second)
	raw, _ = json.Marshal(state)
	if _, err = f.Admin.Exec(`UPDATE chat_pacing_accounts SET fair_state=$1`, raw); err != nil {
		t.Fatal(err)
	}
	if err = s.Complete(t.Context(), busy.Permit, 0); err != nil {
		t.Fatal(err)
	}
	d, err := s.AcquireFair(t.Context(), "123", "222", fairRequest(2, 2, r.Deadline))
	if err != nil || !d.Granted {
		t.Fatal("expired waiter stalled admission", err)
	}
	// Capacity is enforced in the durable path, and rejection rolls back even the
	// channel row created before registration.
	for i := 3; i <= fairTenantLimit; i++ {
		if _, err = s.AcquireFair(t.Context(), "123", "333", fairRequest(i, i, r.Deadline)); err != nil {
			t.Fatal(err)
		}
	}
	var before, after int
	if err = f.Admin.QueryRow(`SELECT count(*) FROM chat_pacing_channels`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AcquireFair(t.Context(), "123", "999", fairRequest(999, 999, r.Deadline)); !errors.Is(err, ErrCapacity) {
		t.Fatal("durable tenant capacity", err)
	}
	if err = f.Admin.QueryRow(`SELECT count(*) FROM chat_pacing_channels`).Scan(&after); err != nil || before != after {
		t.Fatal("capacity rejection wrote channel", before, after, err)
	}
}
func TestFairPermitsRequireMigrationAndRejectMalformedState(t *testing.T) {
	f := testpostgres.NewTenantFixture(t, "")
	s, err := Open(t.Context(), f.PacerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if _, err = f.Admin.Exec(`INSERT INTO chat_pacing_accounts(account_key,fair_state) VALUES($1,'{"version":2}')`, func() string { a, _ := keys("123", "456"); return a }()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AcquireFair(t.Context(), "123", "456", fairRequest(1, 1, time.Now().Add(time.Minute))); !errors.Is(err, ErrFairState) {
		t.Fatal("malformed state admitted", err)
	}
	if _, err = s.Inspect(t.Context(), "123", "456", time.Time{}); !errors.Is(err, ErrReportUnavailable) {
		t.Fatal("malformed report", err)
	}
	if _, err = f.Admin.Exec(`ALTER TABLE chat_pacing_accounts DROP COLUMN fair_state`); err != nil {
		t.Fatal(err)
	}
	if invalid, err := Open(t.Context(), f.PacerURL); err == nil {
		_ = invalid.Close()
		t.Fatal("missing migration accepted")
	}
}

func TestFairGrantRollbackAndUnclaimedRetry(t *testing.T) {
	f := testpostgres.NewTenantFixture(t, "")
	s, err := Open(t.Context(), f.PacerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	r := fairRequest(1, 1, time.Now().UTC().Add(time.Minute))
	if _, err = f.Admin.Exec(`CREATE FUNCTION reject_fair_permit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.lease_hash IS NOT NULL THEN RAISE EXCEPTION 'synthetic private fixture failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_fair_permit BEFORE UPDATE ON chat_pacing_accounts FOR EACH ROW EXECUTE FUNCTION reject_fair_permit()`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AcquireFair(t.Context(), "123", "456", r); err == nil {
		t.Fatal("injected grant failure ignored")
	}
	var count int
	if err = f.Admin.QueryRow(`SELECT count(*) FROM chat_pacing_accounts`).Scan(&count); err != nil || count != 0 {
		t.Fatal("grant failure committed partial registration", count, err)
	}
	if _, err = f.Admin.Exec(`DROP TRIGGER reject_fair_permit ON chat_pacing_accounts`); err != nil {
		t.Fatal(err)
	}
	d, err := s.AcquireFair(t.Context(), "123", "456", r)
	if err != nil || !d.Granted {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err = s.CompleteUnclaimed(canceled, d.Permit); err != nil {
		t.Fatal("detached unclaimed completion", err)
	}
	if err = s.CompleteUnclaimed(t.Context(), d.Permit); !errors.Is(err, ErrFence) {
		t.Fatal("stale unclaimed completion", err)
	}
	// Unclaimed cleanup retains all quota charges and channel spacing. Advance
	// only synthetic channel time to qualify retry without a real provider call.
	if _, err = f.Admin.Exec(`UPDATE chat_pacing_channels SET next_at=clock_timestamp()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	next, err := s.AcquireFair(t.Context(), "123", "456", r)
	if err != nil || !next.Granted {
		t.Fatal("preclaim retry lost", err)
	}
	var attempts int
	if err = f.Admin.QueryRow(`SELECT jsonb_array_length(attempts) FROM chat_pacing_accounts`).Scan(&attempts); err != nil || attempts != 2 {
		t.Fatal("unclaimed attempt refunded", attempts, err)
	}
	if err = s.Complete(t.Context(), next.Permit, 0); err != nil {
		t.Fatal(err)
	}
	again, err := s.AcquireFair(t.Context(), "123", "456", r)
	if err != nil || again.Granted {
		t.Fatal("completed action replayed", err)
	}
}

func fairRequest(tenant, action int, deadline time.Time) FairRequest {
	return FairRequest{TenantID: fmt.Sprintf("%08x-0000-0000-0000-000000000001", tenant), ActionID: fmt.Sprintf("%064x", action), Deadline: deadline}
}
