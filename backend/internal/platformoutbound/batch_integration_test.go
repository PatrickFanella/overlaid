package platformoutbound

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

func replyBatch(r Request, n int) []Request {
	batch := make([]Request, n)
	for i := range batch {
		batch[i] = r
		batch[i].Key = fmt.Sprintf("batch-%d", i)
	}
	return batch
}
func TestAtomicReplyBatchConcurrentDuplicatesAndMixedExisting(t *testing.T) {
	_, db, otherDB := fixture(t)
	s, _ := New(db)
	r := source(t, db, "reply-batch")
	batch := replyBatch(r, 3)
	var created atomic.Int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			admitted, err := s.AdmitBatch(t.Context(), batch)
			if err != nil {
				t.Error(err)
				return
			}
			if len(admitted) != 3 {
				t.Error("incomplete batch result")
				return
			}
			for i, a := range admitted {
				want, _ := identity(db.TenantID(), batch[i])
				if a.ActionID != want {
					t.Error("unstable action identity")
				}
				if a.Created {
					created.Add(1)
				}
			}
		})
	}
	wg.Wait()
	if created.Load() != 3 {
		t.Fatal("duplicate batch admission", created.Load())
	}
	sendJobs(t, db, 3)
	if id, fresh, err := s.Admit(t.Context(), batch[0]); err != nil || fresh || id == "" {
		t.Fatal("single/batch identity drift", err)
	}
	mixed := replyBatch(r, 4)
	admitted, err := s.AdmitBatch(t.Context(), mixed)
	if err != nil || len(admitted) != 4 || admitted[0].Created || !admitted[3].Created {
		t.Fatal("mixed prior/new admission", admitted, err)
	}
	sendJobs(t, db, 4)
	changed := mixed[1]
	changed.Text = "changed immutable payload"
	fresh := r
	fresh.Key = "fresh-before-conflict"
	if out, err := s.AdmitBatch(t.Context(), []Request{fresh, changed}); !errors.Is(err, ErrConflict) || out != nil {
		t.Fatal("conflict returned partial results", out, err)
	}
	sendJobs(t, db, 4)
	other, _ := New(otherDB)
	if _, err = other.AdmitBatch(t.Context(), batch); !errors.Is(err, ErrInvalid) {
		t.Fatal("cross-tenant source admitted", err)
	}
	sendJobs(t, otherDB, 0)
}
func TestAtomicReplyBatchLimitsAndEnqueueRollback(t *testing.T) {
	f, db, _ := fixture(t)
	s, _ := New(db)
	r := source(t, db, "batch-limits")
	batch := replyBatch(r, 16)
	original := batch
	if _, err := s.AdmitBatch(t.Context(), batch[:15]); err != nil {
		t.Fatal(err)
	}
	a, b := r, r
	a.Key = "overflow-a"
	b.Key = "overflow-b"
	if _, err := s.AdmitBatch(t.Context(), []Request{a, b}); !errors.Is(err, tenantdb.ErrLimit) {
		t.Fatal("partial source capacity accepted", err)
	}
	sendJobs(t, db, 15)
	if _, err := s.AdmitBatch(t.Context(), []Request{batch[0], batch[15]}); err != nil {
		t.Fatal("existing action counted as new", err)
	}
	sendJobs(t, db, 16)
	r = source(t, db, "batch-job-failure")
	batch = replyBatch(r, 3)
	failID, _ := identity(db.TenantID(), batch[1])
	if _, err := f.Admin.Exec(`CREATE FUNCTION reject_batch_job() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.kind='overlaid_chat_send' AND NEW.args->>'action_id'='` + failID + `' THEN RAISE EXCEPTION 'synthetic private enqueue failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_batch_job BEFORE INSERT ON river_job FOR EACH ROW EXECUTE FUNCTION reject_batch_job()`); err != nil {
		t.Fatal(err)
	}
	if out, err := s.AdmitBatch(t.Context(), batch); err == nil || out != nil {
		t.Fatal("enqueue failure returned partial admission", out, err)
	}
	var count int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/ AND event_id=$1`, r.EventID).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial actions survived enqueue failure", count, err)
	}
	sendJobs(t, db, 16)
	if _, err := f.Admin.Exec(`DROP TRIGGER reject_batch_job ON river_job; UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','17') WHERE id='free'`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(`UPDATE tenants SET plan_id='free' WHERE id=$1`, db.TenantID()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AdmitBatch(t.Context(), batch[:2]); !errors.Is(err, tenantdb.ErrLimit) {
		t.Fatal("partial plan capacity accepted", err)
	}
	sendJobs(t, db, 16)
	// No-op replay stays available after a plan reduction or source revocation.
	if _, err := f.Admin.Exec(`UPDATE platform_connections SET revoked_at=clock_timestamp() WHERE tenant_id=$1`, db.TenantID()); err != nil {
		t.Fatal(err)
	}

	if admitted, err := s.AdmitBatch(t.Context(), original); err != nil || len(admitted) != 16 {
		t.Fatal("no-op batch rejected after revoke", err)
	}
}

func TestAtomicReplyBatchRejectsInvalidShapesWithoutWrites(t *testing.T) {
	_, db, _ := fixture(t)
	s, _ := New(db)
	r := source(t, db, "batch-shapes")
	other := source(t, db, "other-source")
	bad := r
	bad.Text = strings.Repeat("x", 501)
	for _, batch := range [][]Request{nil, replyBatch(r, 17), {r, r}, {r, other}, {r, bad}} {
		if out, err := s.AdmitBatch(t.Context(), batch); !errors.Is(err, ErrInvalid) || out != nil {
			t.Fatal("invalid batch accepted", err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.AdmitBatch(ctx, replyBatch(r, 2)); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation identity lost", err)
	}
	sendJobs(t, db, 0)
}
func TestReplyBatchUsesDatabaseClockAfterAdmissionLock(t *testing.T) {
	f, db, _ := fixture(t)
	s, _ := New(db)
	r := source(t, db, "batch-lock-expiry")
	tx, err := f.Admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(t.Context(), `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, db.TenantID()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Go(func() { _, err := s.AdmitBatch(ctx, replyBatch(r, 2)); done <- err })
	defer func() { cancel(); _ = tx.Rollback(); wg.Wait() }()
	u, _ := url.Parse(f.RuntimeURL)
	var started time.Time
	for {
		err = f.Admin.QueryRowContext(ctx, `SELECT xact_start FROM pg_stat_activity WHERE datname=current_database() AND usename=$1 AND wait_event_type='Lock' LIMIT 1`, u.User.Username()).Scan(&started)
		if err == nil {
			break
		}
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("batch did not wait on admission lock")
		case <-time.After(10 * time.Millisecond):
		}
	}
	// Synthetic source aging places its fixed deadline after transaction start
	// but before lock release. No wall-clock sleep or provider call is required.
	var deadline, observed time.Time
	if err = tx.QueryRowContext(ctx, `UPDATE normalized_chat_events SET occurred_at=$1::timestamptz+(clock_timestamp()-$1::timestamptz)/2-interval '2 minutes' WHERE tenant_id=$2 AND id=$3 RETURNING occurred_at+interval '2 minutes',clock_timestamp()`, started, db.TenantID(), r.EventID).Scan(&deadline, &observed); err != nil || !deadline.After(started) || !deadline.Before(observed) {
		t.Fatal("expiry fixture did not straddle transaction clock", err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-done; !errors.Is(err, ErrInvalid) {
		t.Fatal("lock wait admitted expired source", err)
	}
	sendJobs(t, db, 0)
}
