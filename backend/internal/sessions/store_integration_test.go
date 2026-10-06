package sessions

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/history"
)

func isolatedSessionStore(t *testing.T) *Store {
	t.Helper()
	databaseURL := os.Getenv("OVERLAID_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set OVERLAID_TEST_DATABASE_URL to disposable Postgres")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatal("test database must be a Postgres URL")
	}
	admin, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	schema := "sessions_test_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		_ = admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(ctx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
		_ = admin.Close()
	})
	query := parsed.Query()
	query.Set("options", strings.TrimSpace(query.Get("options")+" -csearch_path="+schema+",public"))
	parsed.RawQuery = query.Encode()
	events, err := history.NewPostgres(parsed.String(), 250)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = events.Close() })
	store, err := NewPostgres(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	var actual string
	if err := store.db.QueryRowContext(ctx, "SELECT current_schema()").Scan(&actual); err != nil || actual != schema {
		t.Fatalf("schema=%q err=%v", actual, err)
	}
	return store
}

func TestConcurrentSessionLifecyclePostgres(t *testing.T) {
	store := isolatedSessionStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Widen the empty-table overlap in this isolated fixture only.
	if _, err := store.db.ExecContext(ctx, `CREATE FUNCTION delay_session_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(0.04); RETURN NEW; END $$;
	CREATE TRIGGER delay_insert BEFORE INSERT ON stream_sessions FOR EACH ROW EXECUTE FUNCTION delay_session_insert()`); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errorsFound := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := store.Start(ctx, "Concurrent "+strconv.Itoa(i), "rehearsal", "fixture")
			errorsFound <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count, active int
	if err := store.db.QueryRowContext(ctx, `SELECT count(*), count(*) FILTER (WHERE ended_at IS NULL) FROM stream_sessions`).Scan(&count, &active); err != nil {
		t.Fatal(err)
	}
	if count != 8 || active != 1 {
		t.Fatalf("concurrent starts: total=%d active=%d", count, active)
	}
	current, ok, err := store.Current(ctx)
	if err != nil || !ok {
		t.Fatalf("current=%v err=%v", ok, err)
	}
	recent, err := store.Recent(ctx, 8)
	if err != nil || len(recent) != 8 || recent[0].ID != current.ID {
		t.Fatalf("recent order disagrees with active session: %#v err=%v", recent, err)
	}
	for _, session := range recent {
		if session.EndedAt != nil && session.EndedAt.Before(session.StartedAt) {
			t.Fatalf("negative session duration: %#v", session)
		}
	}
	ended := make(chan bool, 8)
	errorsFound = make(chan error, 8)
	start = make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; _, ok, err := store.End(ctx); ended <- ok; errorsFound <- err }()
	}
	close(start)
	wg.Wait()
	close(ended)
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
	successes := 0
	for ok := range ended {
		if ok {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent ends accepted=%d", successes)
	}
	final, err := store.ByID(ctx, current.ID)
	if err != nil || final.EndedAt == nil {
		t.Fatalf("session not ended: %#v err=%v", final, err)
	}
	if _, ok, err := store.Current(ctx); err != nil || ok {
		t.Fatalf("active after end: %v err=%v", ok, err)
	}
}

func TestSessionLifecycleLockCancellationPostgres(t *testing.T) {
	store := isolatedSessionStore(t)
	ctx := context.Background()
	original, err := store.Start(ctx, "Original", "live", "")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock('stream_sessions'::regclass::oid::bigint)`); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"start", "end"} {
		limited, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		if operation == "start" {
			_, err = store.Start(limited, "Blocked", "live", "")
		} else {
			_, _, err = store.End(limited)
		}
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s ignored locked deadline: %v", operation, err)
		}
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	current, ok, err := store.Current(ctx)
	if err != nil || !ok || current.ID != original.ID || current.EndedAt != nil {
		t.Fatalf("blocked lifecycle mutated session: %#v ok=%v err=%v", current, ok, err)
	}
}

func TestSessionReportConsistentSnapshotPostgres(t *testing.T) {
	store := isolatedSessionStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := store.Start(ctx, "Snapshot report", "rehearsal", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO stream_events (id, type, source, title, message, severity, data, created_at, session_id) VALUES ('before', 'alert', 'test', 'Before', '', 'info', '{}', now(), $1)`, session.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.AddReadinessSnapshot(ctx, session.ID, "start", "ready", []string{}); err != nil {
		t.Fatal(err)
	}
	// Gate only the event projection. COUNT(id) in the first report query does
	// not evaluate this view's type expression, so the writer can commit after
	// the summary has been read and before the events/snapshots are returned.
	if _, err := store.db.ExecContext(ctx, `
 ALTER TABLE stream_events RENAME TO report_events;
 CREATE FUNCTION gate_report_type(value text) RETURNS text LANGUAGE plpgsql AS $$
 BEGIN PERFORM pg_advisory_xact_lock('stream_events'::regclass::oid::bigint); RETURN value; END $$;
 CREATE VIEW stream_events AS SELECT id, gate_report_type(type) AS type, source, title, message, severity, data, created_at, inserted_at, session_id FROM report_events;
 `); err != nil {
		t.Fatal(err)
	}
	var gate int64
	if err := store.db.QueryRowContext(ctx, `SELECT 'stream_events'::regclass::oid::bigint`).Scan(&gate); err != nil {
		t.Fatal(err)
	}
	lock, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback()
	if _, err := lock.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, gate); err != nil {
		t.Fatal(err)
	}
	type result struct {
		report Report
		err    error
	}
	done := make(chan result, 1)
	go func() { report, err := store.Report(ctx, session.ID, 100); done <- result{report, err} }()
	// Observe the actual blocked query rather than relying on a timing delay.
	deadline := time.Now().Add(3 * time.Second)
	for {
		var waiting bool
		if err := store.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_locks WHERE locktype = 'advisory' AND objid = $1 AND NOT granted)`, gate).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("report did not reach the event projection gate")
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	limited, stop := context.WithTimeout(ctx, 50*time.Millisecond)
	_, canceledErr := store.Report(limited, session.ID, 100)
	stop()
	if !errors.Is(canceledErr, context.DeadlineExceeded) {
		t.Fatalf("blocked report ignored cancellation: %v", canceledErr)
	}
	writer, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback()
	for _, statement := range []string{
		`INSERT INTO report_events (id, type, source, title, message, severity, data, created_at, session_id) VALUES ('after', 'alert', 'test', 'After', '', 'info', '{}', now(), $1)`,
		`INSERT INTO stream_session_readiness_snapshots (session_id, phase, status, checks) VALUES ($1, 'end', 'ready', '[]')`,
		`UPDATE stream_sessions SET ended_at = clock_timestamp() WHERE id = $1`,
	} {
		if _, err := writer.ExecContext(ctx, statement, session.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := lock.Rollback(); err != nil {
		t.Fatal(err)
	}
	var got result
	select {
	case got = <-done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.report.Session.EventCount != 1 || len(got.report.Events) != 1 || got.report.Events[0].ID != "before" || len(got.report.Snapshots) != 1 || got.report.Session.EndedAt != nil {
		t.Fatalf("report mixed committed states: %#v", got.report)
	}
	latest, err := store.Report(ctx, session.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Session.EventCount != 2 || len(latest.Events) != 1 || latest.Events[0].ID != "after" || len(latest.Snapshots) != 2 || latest.Session.EndedAt == nil {
		t.Fatalf("later bounded report missed committed evidence: %#v", latest)
	}
}
