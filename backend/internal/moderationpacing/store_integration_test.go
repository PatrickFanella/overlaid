package moderationpacing

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
)

func fixture(t *testing.T) (testpostgres.TenantFixture, *Store) {
	t.Helper()
	f := testpostgres.NewTenantFixture(t, "")
	s, err := Open(t.Context(), f.ModerationPacerURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return f, s
}
func acquire(t *testing.T, s *Store, client, moderator, channel string) Decision {
	t.Helper()
	d, err := s.Acquire(t.Context(), client, moderator, channel)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestGlobalConcurrencyAndIndependentKeys(t *testing.T) {
	f, s := fixture(t)
	second, err := Open(t.Context(), f.ModerationPacerURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	var granted atomic.Int32
	var wg sync.WaitGroup
	for i := range 20 {
		pool := s
		if i%2 == 0 {
			pool = second
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := pool.Acquire(t.Context(), "client", "123", "456")
			if err != nil {
				t.Error(err)
			}
			if d.Granted {
				granted.Add(1)
			}
		}()
	}
	wg.Wait()
	if granted.Load() != 1 {
		t.Fatal("overlapping permit grants", granted.Load())
	}
	for _, ids := range [][3]string{{"client", "123", "999"}, {"client", "999", "456"}} {
		d := acquire(t, s, ids[0], ids[1], ids[2])
		if d.Granted || d.RetryAt.IsZero() {
			t.Fatal("shared sender/channel not fenced")
		}
	}
	for _, ids := range [][3]string{{"other-client", "123", "456"}, {"client", "999", "999"}} {
		if !acquire(t, s, ids[0], ids[1], ids[2]).Granted {
			t.Fatal("independent keys blocked")
		}
	}
	var raw string
	if err := s.db.QueryRow(`SELECT string_agg(key||coalesce(lease_hash,''),'') FROM moderation_pacing_buckets`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "client") || strings.Contains(raw, "moderator") {
		t.Fatal("raw identity persisted")
	}
}

func TestRestartCooldownAndDetachedCompletion(t *testing.T) {
	f, s := fixture(t)
	d := acquire(t, s, "client", "123", "456")
	if !d.Granted || d.Permit.Deadline().After(time.Now().Add(leaseDuration)) {
		t.Fatal("invalid local use-by time")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.Complete(ctx, d.Permit, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := s.Complete(t.Context(), d.Permit, 0); !errors.Is(err, ErrFence) {
		t.Fatal("completion replay accepted", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := Open(t.Context(), f.ModerationPacerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restarted.Close() }()
	for _, ids := range [][3]string{{"client", "123", "999"}, {"client", "999", "456"}} {
		got := acquire(t, restarted, ids[0], ids[1], ids[2])
		if got.Granted || time.Until(got.RetryAt) < 50*time.Second {
			t.Fatal("cooldown lost after restart")
		}
	}
	var now time.Time
	if err := restarted.db.QueryRow(`SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	if now.IsZero() {
		t.Fatal("missing database clock")
	}
}

func TestCompletionFencesExpiredAndPartiallyReplacedPermits(t *testing.T) {
	f, s := fixture(t)
	old := acquire(t, s, "client", "123", "456")
	forged := old.Permit
	forged.token = strings.Repeat("f", 64)
	if err := s.Complete(t.Context(), forged, 0); !errors.Is(err, ErrFence) {
		t.Fatal("forged completion accepted")
	}
	if err := s.Complete(t.Context(), old.Permit, -time.Second); !errors.Is(err, ErrFence) {
		t.Fatal("negative cooldown accepted")
	}
	if err := s.Complete(t.Context(), old.Permit, 25*time.Hour); !errors.Is(err, ErrFence) {
		t.Fatal("unbounded cooldown accepted")
	}
	// An abandoned permit expires. Another moderator can then claim the same
	// channel while the old sender row still retains its expired claim hash.
	if _, err := f.Admin.Exec(`UPDATE moderation_pacing_buckets SET next_at=clock_timestamp()-interval '1 second',lease_until=clock_timestamp()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	next := acquire(t, s, "client", "999", "456")
	if !next.Granted {
		t.Fatal("expired lease did not recover")
	}
	if err := s.Complete(t.Context(), old.Permit, time.Hour); !errors.Is(err, ErrFence) {
		t.Fatal("old permit cleared new channel lease")
	}
	if err := s.Complete(t.Context(), next.Permit, 0); err != nil {
		t.Fatal(err)
	}
	// SQL bool_and ignores NULL. Both stored hashes must match, even after the
	// newer completion cleared the channel's hash but not the old sender hash.
	if err := s.Complete(t.Context(), old.Permit, time.Hour); !errors.Is(err, ErrFence) {
		t.Fatal("partly cleared permit accepted")
	}
	if acquire(t, s, "client", "888", "456").Granted {
		t.Fatal("zero completion refunded channel spacing")
	}
}

func TestBoundsPruningAndCanceledAdmission(t *testing.T) {
	f, s := fixture(t)
	for _, ids := range [][3]string{{"", "123", "456"}, {"bad client", "123", "456"}, {"client", "not-id", "456"}, {"client", "123", ""}} {
		if _, err := s.Acquire(t.Context(), ids[0], ids[1], ids[2]); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid identity accepted")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Acquire(ctx, "client", "123", "456"); err == nil {
		t.Fatal("canceled acquisition accepted")
	}
	for _, kind := range []string{"sender", "channel"} {
		limit := 1024
		if kind == "channel" {
			limit = 10000
		}
		if _, err := f.Admin.Exec(`TRUNCATE moderation_pacing_buckets`); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Admin.Exec(`INSERT INTO moderation_pacing_buckets(kind,key) SELECT $1,repeat(md5(i::text),2) FROM generate_series(1,$2) i`, kind, limit); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Acquire(t.Context(), "client", "123", "456"); !errors.Is(err, ErrCapacity) {
			t.Fatal("capacity not bounded", kind, err)
		}
		if _, err := f.Admin.Exec(`UPDATE moderation_pacing_buckets SET updated_at=clock_timestamp()-interval '2 days'`); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Admin.Exec(`UPDATE moderation_pacing_buckets SET next_at=clock_timestamp()+interval '1 hour'`); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Acquire(t.Context(), "client", "123", "456"); !errors.Is(err, ErrCapacity) {
			t.Fatal("pruned live cooldown", err)
		}
		if _, err := f.Admin.Exec(`UPDATE moderation_pacing_buckets SET next_at='-infinity',lease_hash=repeat('a',64),lease_until=clock_timestamp()+interval '1 hour'`); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Acquire(t.Context(), "client", "123", "456"); !errors.Is(err, ErrCapacity) {
			t.Fatal("pruned active lease", err)
		}
		if _, err := f.Admin.Exec(`UPDATE moderation_pacing_buckets SET lease_hash=NULL,lease_until=NULL`); err != nil {
			t.Fatal(err)
		}
		if !acquire(t, s, "client", "123", "456").Granted {
			t.Fatal("expired state not pruned")
		}
	}
}

func TestRestrictedPoolsAndPolicyDrift(t *testing.T) {
	f, s := fixture(t)
	for _, table := range []string{"platform_connections", "river_job", "chat_pacing_accounts", "service_bot_accounts"} {
		var n int
		if err := s.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err == nil {
			t.Fatal("pacer can read private table", table)
		}
	}
	for _, dsn := range []string{f.RuntimeURL, f.WorkerURL, f.OutboundWorkerURL, f.ModerationWorkerURL, f.BotAdminURL, f.BotExecutorURL, f.PacerURL} {
		if bad, err := Open(t.Context(), dsn); err == nil {
			_ = bad.Close()
			t.Fatal("wrong pool accepted")
		}
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Fatal(err)
		}
		var count int
		err = db.QueryRow(`SELECT count(*) FROM moderation_pacing_buckets`).Scan(&count)
		_ = db.Close()
		if err == nil {
			t.Fatal("private global state exposed to another pool")
		}
	}
	var id [4]string
	var err error
	id, err = s.DatabaseIdentity(t.Context())
	if err != nil || id[1] != f.Schema {
		t.Fatal("database identity mismatch", err)
	}
	if _, err = f.Admin.Exec(`CREATE POLICY unexpected ON moderation_pacing_buckets TO PUBLIC USING(true)`); err != nil {
		t.Fatal(err)
	}
	if bad, err := Open(t.Context(), f.ModerationPacerURL); err == nil {
		_ = bad.Close()
		t.Fatal("extra policy accepted")
	}
	if _, err = f.Admin.Exec(`DROP POLICY unexpected ON moderation_pacing_buckets; ALTER POLICY pacer_only ON moderation_pacing_buckets USING(false)`); err != nil {
		t.Fatal(err)
	}
	if bad, err := Open(t.Context(), f.ModerationPacerURL); err == nil {
		_ = bad.Close()
		t.Fatal("changed policy expression accepted")
	}
	if _, err = f.Admin.Exec(`ALTER POLICY pacer_only ON moderation_pacing_buckets USING(true); ALTER TABLE moderation_pacing_buckets NO FORCE ROW LEVEL SECURITY`); err != nil {
		t.Fatal(err)
	}
	if bad, err := Open(t.Context(), f.ModerationPacerURL); err == nil {
		_ = bad.Close()
		t.Fatal("missing forced RLS accepted")
	}
	if _, err = f.Admin.Exec(`ALTER TABLE moderation_pacing_buckets FORCE ROW LEVEL SECURITY`); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(f.ModerationPacerURL)
	runtimeURL, _ := url.Parse(f.RuntimeURL)
	// Fixture-owned generated role names only; no production grant is changed.
	if _, err = f.Admin.Exec(`ALTER ROLE ` + u.User.Username() + ` NOINHERIT; GRANT ` + runtimeURL.User.Username() + ` TO ` + u.User.Username()); err != nil {
		t.Fatal(err)
	}
	if bad, err := Open(t.Context(), f.ModerationPacerURL); err == nil {
		_ = bad.Close()
		t.Fatal("reachable private role accepted")
	}
	// A startup role parameter must not hide a privileged membership reachable
	// by the original session login after RESET ROLE.
	if _, err = f.Admin.Exec(`REVOKE ` + runtimeURL.User.Username() + ` FROM ` + u.User.Username() + `; ALTER ROLE ` + u.User.Username() + ` INHERIT`); err != nil {
		t.Fatal(err)
	}
	sessionRole := f.Schema + "_session"
	password, _ := u.User.Password()
	if _, err = f.Admin.Exec(`CREATE ROLE ` + sessionRole + ` LOGIN NOINHERIT PASSWORD '` + password + `'; GRANT ` + u.User.Username() + `,` + runtimeURL.User.Username() + ` TO ` + sessionRole); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.Admin.Exec(`DROP ROLE ` + sessionRole); err != nil {
			t.Error(err)
		}
	})
	u.User = url.UserPassword(sessionRole, password)
	q := u.Query()
	safeURL, _ := url.Parse(f.ModerationPacerURL)
	q.Set("role", safeURL.User.Username())
	u.RawQuery = q.Encode()
	session, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	var current, original string
	err = session.QueryRowContext(t.Context(), `SELECT current_user,session_user`).Scan(&current, &original)
	_ = session.Close()
	if err != nil || current != safeURL.User.Username() || original != sessionRole {
		t.Fatal("startup role fixture did not switch identity", err)
	}
	if bad, err := Open(t.Context(), u.String()); err == nil {
		_ = bad.Close()
		t.Fatal("session role escape accepted")
	}
}

func TestBlockedAdmissionHonorsContext(t *testing.T) {
	f, s := fixture(t)
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
	if _, err = s.Acquire(ctx, "client", "123", "456"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("blocked admission ignored context", err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.db.QueryRow(`SELECT count(*) FROM moderation_pacing_buckets`).Scan(&count); err != nil || count != 0 {
		t.Fatal("canceled admission retained state", err)
	}
}

func TestAdmissionClockAfterContentionAndConservativeLease(t *testing.T) {
	f, s := fixture(t)
	first := acquire(t, s, "client", "123", "456")
	if err := s.Complete(t.Context(), first.Permit, 0); err != nil {
		t.Fatal(err)
	}
	tx, err := f.Admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = lock(t.Context(), tx); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	type result struct {
		decision Decision
		err      error
	}
	done := make(chan result, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); d, e := s.Acquire(ctx, "client", "123", "456"); done <- result{d, e} }()
	defer func() { cancel(); _ = tx.Rollback(); wg.Wait() }()
	u, _ := url.Parse(f.ModerationPacerURL)
	for {
		var waits int
		if err = f.Admin.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND usename=$1 AND wait_event_type='Lock'`, u.User.Username()).Scan(&waits); err != nil {
			t.Fatal(err)
		}
		if waits > 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("admission did not wait on lock")
		case <-time.After(10 * time.Millisecond):
		}
	}
	// Transaction-start now() predates this deadline. After lock release it is
	// already eligible, so only a fresh database clock should grant admission.
	if _, err = tx.ExecContext(ctx, `UPDATE moderation_pacing_buckets SET next_at=clock_timestamp()+interval '200 milliseconds'`); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	released := time.Now()
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.err != nil || !got.decision.Granted {
		t.Fatal("used stale admission clock", got.err)
	}
	if got.decision.Permit.Deadline().After(released.Add(leaseDuration)) {
		t.Fatal("contention extended local permit lifetime")
	}
}
