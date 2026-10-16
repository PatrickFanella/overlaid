package platformoutbound

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// awaitOutbound keeps the existing fifteen-second test budget meaningful even
// when the state query is blocked. It never changes a job or product deadline.
func awaitOutbound(ctx context.Context, db *sql.DB, state string) bool {
	for {
		var got string
		if err := db.QueryRowContext(ctx, `SELECT state FROM river_job WHERE kind='overlaid_chat_send'`).Scan(&got); err != nil {
			return false
		}
		if got == state {
			return true
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}

// Only allowlisted metadata can enter failure output. Never include job args,
// error bodies, query text, credentials, role names, claims or provider payloads.
type outboundDiagnostics struct {
	PoolOpen, PoolInUse, PoolIdle                       int
	PoolWaitCount, PoolWaitMS                           int64
	ActivityRead                                        bool
	ActiveSessions, LockWaits, IOWaits, BlockedSessions int
	JobRead                                             bool
	State                                               string
	Attempt, MaxAttempts, ErrorCount                    int
	ScheduledInMS, DeadlineInMS                         int64
}

func snapshotOutbound(db *sql.DB) outboundDiagnostics {
	stats := db.Stats()
	d := outboundDiagnostics{PoolOpen: stats.OpenConnections, PoolInUse: stats.InUse, PoolIdle: stats.Idle,
		PoolWaitCount: stats.WaitCount, PoolWaitMS: stats.WaitDuration.Milliseconds()}
	// Independent of the expired test/worker context, but bounded to a single
	// second in total. Activity goes first so a locked job table cannot hide it.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	d.ActivityRead = db.QueryRowContext(ctx, `SELECT
 count(*) FILTER(WHERE state='active'),
 count(*) FILTER(WHERE wait_event_type='Lock'),
 count(*) FILTER(WHERE wait_event_type='IO'),
 count(*) FILTER(WHERE cardinality(pg_blocking_pids(pid))>0)
 FROM pg_stat_activity WHERE datname=current_database() AND usename=current_user`).Scan(
		&d.ActiveSessions, &d.LockWaits, &d.IOWaits, &d.BlockedSessions) == nil
	// River stores the absence of attempts as a NULL error array. Count that
	// as zero so scanning an otherwise valid job does not stop at this field.
	d.JobRead = db.QueryRowContext(ctx, `SELECT state,attempt,max_attempts,coalesce(cardinality(errors),0),
 floor(extract(epoch FROM scheduled_at-clock_timestamp())*1000)::bigint,
 floor(((args->>'deadline')::bigint-extract(epoch FROM clock_timestamp()))*1000)::bigint
 FROM river_job WHERE kind='overlaid_chat_send'`).Scan(
		&d.State, &d.Attempt, &d.MaxAttempts, &d.ErrorCount, &d.ScheduledInMS, &d.DeadlineInMS) == nil
	return d
}

func logOutboundDiagnostics(t *testing.T, db *sql.DB) {
	t.Helper()
	raw, _ := json.Marshal(snapshotOutbound(db))
	t.Logf("outbound failure metadata: %s", raw)
}

func TestOutboundDiagnosticsUnavailablePool(t *testing.T) {
	// Closing before querying proves no network or credential-bearing errors
	// are needed to report an unavailable pool.
	db, err := sql.Open("pgx", "postgres://fixture:PRIVATE_PASSWORD@127.0.0.1:1/fixture")
	if err != nil {
		t.Fatal("fixture pool could not be constructed")
	}
	if err = db.Close(); err != nil {
		t.Fatal("fixture pool could not be closed")
	}
	d := snapshotOutbound(db)
	raw, _ := json.Marshal(d)
	if d.ActivityRead || d.JobRead || strings.Contains(string(raw), "PRIVATE") || awaitOutbound(t.Context(), db, "completed") {
		t.Fatal("unavailable pool produced unsafe diagnostic success")
	}
}

func TestOutboundDiagnosticsMetadataAndBlockedQuery(t *testing.T) {
	f, _, _, _ := executorFixture(t)
	service := workerPool(t, f.OutboundWorkerURL)
	var role string
	if err := service.QueryRowContext(t.Context(), `SELECT current_user`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	// Newly inserted and snoozed jobs have no error entries. Their metadata
	// must remain readable before any provider error has been recorded.
	fresh := snapshotOutbound(service)
	if !fresh.JobRead || fresh.ErrorCount != 0 || fresh.DeadlineInMS <= 0 {
		t.Fatalf("missing fresh job metadata: %+v", fresh)
	}
	// Provider text deliberately lives in a field the diagnostic query must
	// count without projecting. The source text already contains private input.
	if _, err := f.Admin.Exec(`UPDATE river_job SET errors=ARRAY['{"error":"PRIVATE_PROVIDER_TOKEN"}'::jsonb] WHERE kind='overlaid_chat_send'`); err != nil {
		t.Fatal(err)
	}
	d := snapshotOutbound(service)
	if !d.ActivityRead || !d.JobRead || d.State != "available" || d.Attempt != 0 || d.MaxAttempts != 3 || d.ErrorCount != 1 || d.DeadlineInMS <= 0 {
		t.Fatalf("missing metadata: %+v", d)
	}
	raw, _ := json.Marshal(d)
	if strings.Contains(string(raw), "PRIVATE") || strings.Contains(string(raw), "response text") {
		t.Fatal("diagnostics projected private input")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 17 {
		t.Fatalf("diagnostic projection changed: %s", raw)
	}
	lock, err := f.Admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Rollback() }()
	if _, err = lock.ExecContext(t.Context(), `LOCK TABLE river_job IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	// A broken waiter must fail this regression instead of hanging the suite.
	unlock := time.AfterFunc(3*time.Second, func() { _ = lock.Rollback() })
	defer unlock.Stop()
	// Reproduce a blocked state observation, with no provider callback. The
	// previous QueryRow ignored this context and could wait until test timeout.
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	if awaitOutbound(ctx, service, "completed") || ctx.Err() != context.DeadlineExceeded {
		t.Fatal("blocked state observation did not honor deadline")
	}
	if time.Since(started) >= time.Second {
		t.Fatal("state query exceeded its context budget")
	}
	unlock.Stop()
	// Keep another own-role reader waiting while taking the independent
	// snapshot, proving lock metadata survives a blocked job-table read.
	blockedCtx, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()
	done := make(chan struct{})
	go func() {
		defer close(done)
		var n int
		_ = service.QueryRowContext(blockedCtx, `SELECT count(*) FROM river_job`).Scan(&n)
	}()
	defer func() { stop(); <-done }()
	readyCtx, end := context.WithTimeout(t.Context(), 2*time.Second)
	defer end()
	for {
		var count int
		if err := f.Admin.QueryRowContext(readyCtx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND usename=$1 AND wait_event_type='Lock'`, role).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count > 0 {
			break
		}
		if readyCtx.Err() != nil {
			t.Fatal("reader did not block")
		}
		time.Sleep(10 * time.Millisecond)
	}
	d = snapshotOutbound(service)
	if !d.ActivityRead || d.LockWaits == 0 || d.BlockedSessions == 0 || d.JobRead {
		t.Fatalf("blocked job-table read hid diagnostics: %+v", d)
	}
}
