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
