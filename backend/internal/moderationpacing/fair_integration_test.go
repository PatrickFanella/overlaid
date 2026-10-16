package moderationpacing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/permitturns"
)

func fairRequest(tenant, action int, deadline time.Time) FairRequest {
	return FairRequest{TenantID: fmt.Sprintf("%08x-0000-0000-0000-000000000001", tenant), ActionID: fmt.Sprintf("%064x", action), Deadline: deadline}
}
func TestFairTurnsAcrossModeratorsSharingChannel(t *testing.T) {
	f, s := fixture(t)
	second, err := Open(t.Context(), f.ModerationPacerURL)
	if err != nil {
		t.Fatal(err)
	}
	busy := acquire(t, s, "fixture-client", "100", "500")
	if !busy.Granted {
		t.Fatal("initial fence")
	}
	var now time.Time
	if err = f.Admin.QueryRow(`SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	requests := []FairRequest{}
	for _, tenant := range []int{1, 2} {
		for i := range 3 {
			r := fairRequest(tenant, tenant*100+i, now.Add(time.Minute))
			requests = append(requests, r)
			d, err := s.AcquireFair(t.Context(), "fixture-client", fmt.Sprint(tenant*100+i), "500", r)
			if err != nil || d.Granted || !d.RetryAt.After(now) || d.RetryAt.After(now.Add(2*time.Second)) {
				t.Fatal("blocked registration", d, err)
			}
		}
	}
	if _, err = s.Acquire(t.Context(), "fixture-client", "999", "999"); !errors.Is(err, ErrFairRequired) {
		t.Fatal("legacy bypass", err)
	}
	var untouched bool
	if err = f.Admin.QueryRow(`SELECT bool_and(next_at='-infinity'::timestamptz AND lease_hash IS NULL) FROM moderation_pacing_buckets WHERE kind='sender' AND key<>$1`, busy.Permit.sender).Scan(&untouched); err != nil || !untouched {
		t.Fatal("denial booked a future slot", err)
	}
	if err = second.Close(); err != nil {
		t.Fatal(err)
	}
	second, err = Open(t.Context(), f.ModerationPacerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	if err = s.Complete(t.Context(), busy.Permit, 0); err != nil {
		t.Fatal(err)
	}
	advance := func() {
		if _, err := f.Admin.Exec(`UPDATE moderation_pacing_buckets SET next_at=clock_timestamp()-interval '1 second' WHERE kind<>'client'`); err != nil {
			t.Fatal(err)
		}
	}
	advance()
	var turns []int
	for range 6 {
		type granted struct {
			index  int
			permit Permit
		}
		grants := make(chan granted, 12)
		var wg sync.WaitGroup
		for i, r := range requests {
			for _, pool := range []*Store{s, second} {
				wg.Add(1)
				go func(i int, r FairRequest, pool *Store) {
					defer wg.Done()
					d, err := pool.AcquireFair(t.Context(), "fixture-client", fmt.Sprint(i/3*100+100+i%3), "500", r)
					if err != nil {
						t.Error(err)
						return
					}
					if d.Granted {
						grants <- granted{i, d.Permit}
					}
				}(i, r, pool)
			}
		}
		wg.Wait()
		close(grants)
		if len(grants) != 1 {
			t.Fatalf("overlapping or missing channel grant: %d", len(grants))
		}
		g := <-grants
		turns = append(turns, g.index/3+1)
		if err = second.Complete(t.Context(), g.permit, 0); err != nil {
			t.Fatal(err)
		}
		advance()
	}
	if fmt.Sprint(turns) != "[1 2 1 2 1 2]" {
		t.Fatal("cross-moderator tenant turns lost", turns)
	}
	d, err := s.AcquireFair(t.Context(), "fixture-client", "100", "500", requests[0])
	if err != nil || d.Granted {
		t.Fatal("duplicate grant", err)
	}
	var raw string
	if err = f.Admin.QueryRow(`SELECT jsonb_agg(to_jsonb(b))::text FROM moderation_pacing_buckets b`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	for _, r := range requests {
		if strings.Contains(raw, r.TenantID) || strings.Contains(raw, r.ActionID) {
			t.Fatal("raw retained identity leaked")
		}
	}
	if strings.Contains(raw, "fixture-client") {
		t.Fatal("raw client retained")
	}
}
func TestFairIndependentResourcesAndClients(t *testing.T) {
	_, s := fixture(t)
	deadline := time.Now().Add(time.Minute)
	for _, v := range []struct {
		client, moderator, channel string
		tenant, action             int
		grant                      bool
	}{
		{"client", "1", "1", 1, 1, true},
		{"client", "2", "2", 2, 2, true},
		{"client", "1", "2", 1, 3, false},
		{"client", "3", "1", 2, 4, false},
		{"client", "4", "4", 3, 5, true},
		{"another-client", "1", "1", 1, 6, true},
	} {
		d, err := s.AcquireFair(t.Context(), v.client, v.moderator, v.channel, fairRequest(v.tenant, v.action, deadline))
		if err != nil || d.Granted != v.grant {
			t.Fatal("resource eligibility", v, d.Granted, err)
		}
	}
}
func TestFairBindingsDeadlineCancellationAndExpiredCaller(t *testing.T) {
	f, s := fixture(t)
	busy := acquire(t, s, "client", "1", "1")
	deadline := time.Now().UTC().Add(time.Minute)
	r := fairRequest(1, 1, deadline)
	if _, err := s.AcquireFair(t.Context(), "client", "2", "1", r); err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][2]string{{"3", "1"}, {"2", "2"}} {
		if _, err := s.AcquireFair(t.Context(), "client", ids[0], ids[1], r); !errors.Is(err, ErrInvalid) {
			t.Fatal("resource binding changed", err)
		}
	}
	changed := r
	changed.Deadline = deadline.Add(time.Second)
	if _, err := s.AcquireFair(t.Context(), "client", "2", "1", changed); !errors.Is(err, ErrInvalid) {
		t.Fatal("deadline changed", err)
	}
	tx, err := f.Admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = lock(t.Context(), tx); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if _, err = s.AcquireFair(ctx, "client", "9", "9", fairRequest(9, 9, deadline)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("blocked cancellation", err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err = f.Admin.QueryRow(`SELECT fair_state FROM moderation_pacing_buckets WHERE kind='client'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	state, err := permitturns.Decode(raw)
	if err != nil || len(state.Waiters) != 1 {
		t.Fatal("canceled registration persisted", err)
	}
	state.Waiters[0].Deadline = time.Now().Add(-time.Second)
	state.Waiters[0].ActiveUntil = state.Waiters[0].Deadline
	raw, _ = json.Marshal(state)
	if _, err = f.Admin.Exec(`UPDATE moderation_pacing_buckets SET fair_state=$1 WHERE kind='client'`, raw); err != nil {
		t.Fatal(err)
	}
	if err = s.Complete(t.Context(), busy.Permit, 0); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`UPDATE moderation_pacing_buckets SET next_at='-infinity' WHERE kind<>'client'`); err != nil {
		t.Fatal(err)
	}
	d, err := s.AcquireFair(t.Context(), "client", "3", "1", fairRequest(2, 2, deadline))
	if err != nil || !d.Granted {
		t.Fatal("expired selected caller stalled channel", err)
	}
}
func TestFairGrantRollbackPreclaimCleanupAndMalformedState(t *testing.T) {
	f, s := fixture(t)
	r := fairRequest(1, 1, time.Now().UTC().Add(time.Minute))
	if _, err := f.Admin.Exec(`CREATE FUNCTION reject_moderation_permit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.lease_hash IS NOT NULL THEN RAISE EXCEPTION 'synthetic private fixture failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_moderation_permit BEFORE UPDATE ON moderation_pacing_buckets FOR EACH ROW EXECUTE FUNCTION reject_moderation_permit()`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcquireFair(t.Context(), "client", "1", "1", r); err == nil {
		t.Fatal("grant failure ignored")
	}
	var count int
	if err := f.Admin.QueryRow(`SELECT count(*) FROM moderation_pacing_buckets`).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial grant persisted", count, err)
	}
	if _, err := f.Admin.Exec(`DROP TRIGGER reject_moderation_permit ON moderation_pacing_buckets`); err != nil {
		t.Fatal(err)
	}
	d, err := s.AcquireFair(t.Context(), "client", "1", "1", r)
	if err != nil || !d.Granted {
		t.Fatal(err)
	}
	forged := d.Permit
	forged.token = strings.Repeat("f", 64)
	if err = s.CompleteUnclaimed(t.Context(), forged); !errors.Is(err, ErrFence) {
		t.Fatal("forged marker cleanup", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err = s.CompleteUnclaimed(ctx, d.Permit); err != nil {
		t.Fatal("detached cleanup", err)
	}
	if err = s.CompleteUnclaimed(t.Context(), d.Permit); !errors.Is(err, ErrFence) {
		t.Fatal("cleanup replay", err)
	}
	next, err := s.AcquireFair(t.Context(), "client", "1", "1", r)
	if err != nil || next.Granted {
		t.Fatal("preclaim cleanup refunded spacing", err)
	}
	if _, err = f.Admin.Exec(`UPDATE moderation_pacing_buckets SET next_at='-infinity' WHERE kind<>'client'`); err != nil {
		t.Fatal(err)
	}
	next, err = s.AcquireFair(t.Context(), "client", "1", "1", r)
	if err != nil || !next.Granted {
		t.Fatal("preclaim retry lost", err)
	}
	if err = s.Complete(t.Context(), next.Permit, 0); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`UPDATE moderation_pacing_buckets SET fair_state='{"version":3}' WHERE kind='client'`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AcquireFair(t.Context(), "client", "1", "1", r); !errors.Is(err, ErrFairState) {
		t.Fatal("malformed state accepted", err)
	}
	if _, err = f.Admin.Exec(`ALTER TABLE moderation_pacing_buckets DROP COLUMN fair_state`); err != nil {
		t.Fatal(err)
	}
	if bad, err := Open(t.Context(), f.ModerationPacerURL); err == nil {
		_ = bad.Close()
		t.Fatal("missing migration accepted")
	}
}
func TestFairClientAndTenantCapacity(t *testing.T) {
	f, s := fixture(t)
	deadline := time.Now().UTC().Add(time.Minute)
	for i := 1; i <= permitturns.TenantLimit; i++ {
		if _, err := s.AcquireFair(t.Context(), "client", "1", "1", fairRequest(i, i, deadline)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AcquireFair(t.Context(), "client", "1", "9", fairRequest(999, 999, deadline)); !errors.Is(err, ErrCapacity) {
		t.Fatal("tenant capacity", err)
	}
	if _, err := f.Admin.Exec(`INSERT INTO moderation_pacing_buckets(kind,key) SELECT 'client',repeat(md5(i::text),2) FROM generate_series(1,1023) i`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcquireFair(t.Context(), "new-client", "9", "9", fairRequest(999, 999, deadline)); !errors.Is(err, ErrCapacity) {
		t.Fatal("client capacity", err)
	}
}

func TestMissingFairCallerDoesNotHoldModerationChannelUntilActionExpiry(t *testing.T) {
	f, s := fixture(t)
	busy := acquire(t, s, "client", "100", "500")
	deadline := time.Now().UTC().Add(time.Minute)
	a := fairRequest(1, 1, deadline)
	b := fairRequest(2, 2, deadline)
	for i, r := range []FairRequest{a, b} {
		if _, err := s.AcquireFair(t.Context(), "client", fmt.Sprint(101+i), "500", r); err != nil {
			t.Fatal(err)
		}
	}
	var raw []byte
	if err := f.Admin.QueryRow(`SELECT fair_state FROM moderation_pacing_buckets WHERE kind='client'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	state, err := permitturns.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	ticket := state.Waiters[0].Ticket
	state.Waiters[0].ActiveUntil = time.Now().Add(-time.Second)
	raw, _ = json.Marshal(state)
	if _, err = f.Admin.Exec(`UPDATE moderation_pacing_buckets SET fair_state=$1 WHERE kind='client'`, raw); err != nil {
		t.Fatal(err)
	}
	if err = s.Complete(t.Context(), busy.Permit, 0); err != nil {
		t.Fatal(err)
	}
	advance := func() {
		if _, err := f.Admin.Exec(`UPDATE moderation_pacing_buckets SET next_at='-infinity' WHERE kind<>'client'`); err != nil {
			t.Fatal(err)
		}
	}
	advance()
	d, err := s.AcquireFair(t.Context(), "client", "102", "500", b)
	if err != nil || !d.Granted {
		t.Fatal("missing caller stalled active moderator", err)
	}
	if err = s.Complete(t.Context(), d.Permit, 0); err != nil {
		t.Fatal(err)
	}
	advance()
	d, err = s.AcquireFair(t.Context(), "client", "101", "500", a)
	if err != nil || !d.Granted {
		t.Fatal("rejoining caller lost turn", err)
	}
	if err = f.Admin.QueryRow(`SELECT fair_state FROM moderation_pacing_buckets WHERE kind='client'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	state, err = permitturns.Decode(raw)
	if err != nil || state.Waiters[0].Ticket != ticket || !state.Waiters[0].Deadline.Equal(deadline) {
		t.Fatal("rejoin changed original identity", err)
	}
}
