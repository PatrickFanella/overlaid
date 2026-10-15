package chatpacing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
)

func TestConcurrentPermitsRestartCooldownAndFences(t *testing.T) {
	f := testpostgres.NewTenantFixture(t, "")
	s, err := Open(t.Context(), f.PacerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	permits := make(chan Permit, 24)
	var wg sync.WaitGroup
	for range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := s.Acquire(t.Context(), "123", "456")
			if err != nil {
				t.Error(err)
			}
			if d.Granted {
				permits <- d.Permit
			}
		}()
	}
	wg.Wait()
	close(permits)
	if len(permits) != 1 {
		t.Fatalf("concurrent permits %d", len(permits))
	}
	p := <-permits
	restarted, err := Open(t.Context(), f.PacerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restarted.Close() }()
	if d, err := restarted.Acquire(t.Context(), "123", "789"); err != nil || d.Granted || !d.RetryAt.After(time.Now()) {
		t.Fatal("cross-channel lease lost", d.Granted, err)
	}
	forged := p
	forged.token = strings.Repeat("0", 64)
	if err = s.Complete(t.Context(), forged, 0); !errors.Is(err, ErrFence) {
		t.Fatal("forged permit completed", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err = restarted.Complete(ctx, p, 42*time.Second); err != nil {
		t.Fatal(err)
	}
	if err = s.Complete(t.Context(), p, 0); !errors.Is(err, ErrFence) {
		t.Fatal("duplicate completion", err)
	}
	if d, err := s.Acquire(t.Context(), "123", "789"); err != nil || d.Granted || time.Until(d.RetryAt) < 40*time.Second {
		t.Fatal("durable cooldown lost", d.Granted, d.RetryAt, err)
	}
	if d, err := s.Acquire(t.Context(), "999", "456"); err != nil || !d.Granted {
		t.Fatal("independent sender blocked", d.Granted, err)
	}
	var raw string
	if err = f.Admin.QueryRow(`SELECT jsonb_agg(to_jsonb(t))::text FROM chat_pacing_accounts t`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, `"123"`) || strings.Contains(raw, p.token) {
		t.Fatal("raw identity/permit retained")
	}
}

func TestLeaseExpiryBudgetCapacityAndRetention(t *testing.T) {
	f := testpostgres.NewTenantFixture(t, "")
	s, err := Open(t.Context(), f.PacerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	d, err := s.Acquire(t.Context(), "123", "456")
	if err != nil || !d.Granted {
		t.Fatal(err)
	}
	old := d.Permit
	if _, err = f.Admin.Exec(`UPDATE chat_pacing_accounts SET lease_until=clock_timestamp()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	d, err = s.Acquire(t.Context(), "123", "789")
	if err != nil || !d.Granted {
		t.Fatal("expired lease blocked new work", d.Granted, err)
	}
	if err = s.Complete(t.Context(), old, 0); !errors.Is(err, ErrFence) {
		t.Fatal("stale lease completion", err)
	}
	if err = s.Complete(t.Context(), d.Permit, 0); err != nil {
		t.Fatal(err)
	}
	if d, err = s.Acquire(t.Context(), "123", "789"); err != nil || d.Granted || !d.RetryAt.After(time.Now()) {
		t.Fatal("channel spacing lost", d.Granted, err)
	}
	var now time.Time
	if err = f.Admin.QueryRow(`SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	times := make([]int64, 20)
	for i := range times {
		times[i] = now.UnixMicro()
	}
	raw, _ := json.Marshal(times)
	if _, err = f.Admin.Exec(`UPDATE chat_pacing_accounts SET attempts=$1`, raw); err != nil {
		t.Fatal(err)
	}
	if d, err = s.Acquire(t.Context(), "123", "900"); err != nil || d.Granted || time.Until(d.RetryAt) < 28*time.Second {
		t.Fatal("rolling account budget bypass", d.Granted, err)
	}
	if _, err = f.Admin.Exec(`INSERT INTO chat_pacing_accounts(account_key) SELECT lpad(to_hex(i),64,'0') FROM generate_series(1,1023) i`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Acquire(t.Context(), "999", "100"); !errors.Is(err, ErrCapacity) {
		t.Fatal("capacity bypass", err)
	}
	if _, err = f.Admin.Exec(`DELETE FROM chat_pacing_channels; INSERT INTO chat_pacing_channels(account_key,channel_key) SELECT a.account_key,lpad(to_hex(i),64,'0') FROM (SELECT account_key FROM chat_pacing_accounts WHERE jsonb_array_length(attempts)>0) a,generate_series(1,10000) i`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Acquire(t.Context(), "123", "100"); !errors.Is(err, ErrCapacity) {
		t.Fatal("channel capacity bypass", err)
	}
	if _, err = f.Admin.Exec(`UPDATE chat_pacing_accounts SET updated_at=clock_timestamp()-interval '2 days',attempts='[]'; UPDATE chat_pacing_channels SET updated_at=clock_timestamp()-interval '2 days',next_at=clock_timestamp()-interval '1 day'`); err != nil {
		t.Fatal(err)
	}
	if d, err = s.Acquire(t.Context(), "999", "100"); err != nil || !d.Granted {
		t.Fatal("stale state not pruned", d.Granted, err)
	}
	var count int
	if err = f.Admin.QueryRow(`SELECT count(*) FROM chat_pacing_accounts`).Scan(&count); err != nil || count != 1 {
		t.Fatal("prune count", count, err)
	}
}

func TestRestrictedPoolAndInvalidInputs(t *testing.T) {
	f := testpostgres.NewTenantFixture(t, "")
	for _, dsn := range []string{"", f.RuntimeURL, f.WorkerURL, f.BotAdminURL} {
		if s, err := Open(t.Context(), dsn); err == nil {
			_ = s.Close()
			t.Fatal("unsafe pacing pool accepted")
		}
	}
	for _, dsn := range []string{f.RuntimeURL, f.WorkerURL, f.BotAdminURL} {
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Exec(`SELECT * FROM chat_pacing_accounts`)
		_ = db.Close()
		if err == nil {
			t.Fatal("product/worker/admin can read pacing state")
		}
	}
	s, err := Open(t.Context(), f.PacerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	for _, id := range []string{"", "sender", "12\n", strings.Repeat("1", 129)} {
		if _, err = s.Acquire(t.Context(), id, "123"); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid identity accepted", err)
		}
	}
	if err = s.Complete(t.Context(), Permit{}, 0); !errors.Is(err, ErrFence) {
		t.Fatal(err)
	}
	var role string
	if err = s.db.QueryRow(`SELECT current_user`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`GRANT SELECT ON service_bot_accounts TO ` + role); err != nil {
		t.Fatal(err)
	}
	if other, err := Open(t.Context(), f.PacerURL); err == nil {
		_ = other.Close()
		t.Fatal("pacer accepted vault access")
	}
	alias := role + "_reachable"
	if _, err = f.Admin.Exec(`REVOKE SELECT ON service_bot_accounts FROM ` + role + `; CREATE ROLE ` + alias + ` NOLOGIN; GRANT SELECT ON service_bot_accounts TO ` + alias + `; GRANT ` + alias + ` TO ` + role + `; ALTER ROLE ` + role + ` NOINHERIT`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.Admin.Exec(`DROP OWNED BY ` + alias + `; DROP ROLE ` + alias); err != nil {
			t.Error(err)
		}
	})
	if other, err := Open(t.Context(), f.PacerURL); err == nil {
		_ = other.Close()
		t.Fatal("pacer accepted reachable vault role")
	}
}
