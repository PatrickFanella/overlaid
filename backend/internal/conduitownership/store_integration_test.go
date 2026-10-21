package conduitownership

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchconduit"
)

type ownershipFixture struct {
	testpostgres.TenantFixture
	dsn, role string
	migration string
}

func fixture(t *testing.T) ownershipFixture {
	t.Helper()
	f := testpostgres.NewTenantFixture(t, "")
	role := "conduit_admin_" + strings.TrimPrefix(f.Schema, "tenant_test_")
	_, source, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(source), "..", "..", "..", "migrations", "0037_conduit_creation.sql"))
	if err != nil {
		t.Fatal(err)
	}
	migration := strings.ReplaceAll(string(raw), "overlaid_conduit_admin", role)
	if _, err = f.Admin.Exec(migration); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(f.RuntimeURL)
	if err != nil {
		t.Fatal(err)
	}
	password, _ := u.User.Password()
	u.User = url.UserPassword(role, password)
	if _, err = f.Admin.Exec(`ALTER ROLE ` + role + ` LOGIN PASSWORD '` + password + `'`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.Admin.Exec(`DROP TABLE IF EXISTS service_conduit_creations; DROP OWNED BY ` + role + `; DROP ROLE ` + role); err != nil {
			t.Error(err)
		}
	})
	return ownershipFixture{f, u.String(), role, migration}
}
func openFixture(t *testing.T) (ownershipFixture, *Store) {
	t.Helper()
	f := fixture(t)
	s, err := Open(t.Context(), f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return f, s
}
func exec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), q, args...); err != nil {
		t.Fatal(err)
	}
}
func binding(client string) Binding {
	return Binding{AppID: "11111111-1111-4111-8111-111111111111", ClientID: client, Revision: 1}
}
func prepared(t *testing.T, s *Store, client string) Intent {
	t.Helper()
	r, err := s.Prepare(t.Context(), binding(client), 2)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func started(t *testing.T, s *Store, r Intent) Attempt {
	t.Helper()
	_, a, err := s.Start(t.Context(), r.ID, r.Binding, r.ShardCount)
	if err != nil || a == nil {
		t.Fatal(err)
	}
	return *a
}
func created(r Intent) twitchconduit.Creation {
	now := time.Now()
	return twitchconduit.Creation{Outcome: twitchconduit.CreationCreated, Conduit: &twitchconduit.CreatedConduit{ClientID: r.Binding.ClientID, ID: "owned-conduit", ShardCount: r.ShardCount, StartedAt: now, CompletedAt: now}}
}

func TestOwnershipPrepareClaimCompleteAndRestart(t *testing.T) {
	f, s := openFixture(t)
	r := prepared(t, s, "client")
	if r.State != "prepared" || r.Principal != f.role || r.DeadlineAt.Sub(r.CreatedAt) != 5*time.Minute || !validID(r.ID) {
		t.Fatal(r)
	}
	duplicate, err := s.Prepare(t.Context(), r.Binding, 2)
	if err != nil || duplicate != r {
		t.Fatal(duplicate, err)
	}
	changed := r.Binding
	changed.Revision++
	if _, err = s.Prepare(t.Context(), changed, 2); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err = s.Prepare(t.Context(), r.Binding, 3); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, a, err := s.Start(t.Context(), r.ID, changed, 2); !errors.Is(err, ErrConflict) || a != nil {
		t.Fatal(err)
	}
	a := started(t, s, r)
	var digest string
	if err = f.Admin.QueryRow(`SELECT claim_hash FROM service_conduit_creations WHERE id=$1`, r.ID).Scan(&digest); err != nil || len(digest) != 64 {
		t.Fatal(err)
	}
	raw, err := json.Marshal(a)
	if err != nil || strings.Contains(string(raw), digest) || strings.Contains(fmt.Sprintf("%+v %#v", a, a), digest) {
		t.Fatal("claim exposed")
	}
	var restored Attempt
	if err = json.Unmarshal(raw, &restored); !errors.Is(err, ErrInvalid) || restored.claim != nil {
		t.Fatal("claim reconstructed")
	}
	reopened, err := Open(t.Context(), f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	if _, next, err := reopened.Start(t.Context(), r.ID, r.Binding, 2); !errors.Is(err, ErrUncertain) || next != nil {
		t.Fatal(err)
	}
	done, err := s.Complete(t.Context(), a, created(r), nil)
	if err != nil || done.State != "created" || done.ConduitID != "owned-conduit" || done.ID != r.ID || done.DeadlineAt != r.DeadlineAt {
		t.Fatal(done, err)
	}
	if _, next, err := s.Start(t.Context(), r.ID, r.Binding, 2); !errors.Is(err, ErrOwned) || next != nil {
		t.Fatal(err)
	}
	if _, err = s.Complete(t.Context(), a, created(r), nil); !errors.Is(err, ErrUncertain) {
		t.Fatal("completion replay", err)
	}
	lookup, err := s.Lookup(t.Context(), r.ID)
	if err != nil || lookup != done {
		t.Fatal(lookup, err)
	}
	if existing, err := s.Prepare(t.Context(), r.Binding, 2); err != nil || existing != done {
		t.Fatal(existing, err)
	}
}

func TestOwnershipConcurrentOriginalIdentity(t *testing.T) {
	_, s := openFixture(t)
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	for range 8 {
		wg.Go(func() {
			r, err := s.Prepare(t.Context(), binding("client"), 2)
			if err != nil {
				t.Error(err)
				return
			}
			ids <- r.ID
		})
	}
	wg.Wait()
	close(ids)
	id := ""
	for got := range ids {
		if id == "" {
			id = got
		}
		if got != id {
			t.Fatal("duplicate original intent")
		}
	}
	var claims atomic.Int32
	for range 8 {
		wg.Go(func() {
			_, a, err := s.Start(t.Context(), id, binding("client"), 2)
			if err == nil && a != nil {
				claims.Add(1)
			} else if !errors.Is(err, ErrUncertain) || a != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if claims.Load() != 1 {
		t.Fatalf("claims=%d", claims.Load())
	}
}

func TestOwnershipUncertaintyRetentionCapacityAndPurgedIdentity(t *testing.T) {
	f, s := openFixture(t)
	r := prepared(t, s, "unresolved")
	a := started(t, s, r)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	u, err := s.Complete(ctx, a, created(r), context.Canceled)
	if !errors.Is(err, ErrUncertain) || !errors.Is(err, context.Canceled) || u.State != "uncertain" {
		t.Fatal(u, err)
	}
	exec(t, f.Admin, `UPDATE service_conduit_creations SET created_at=statement_timestamp()-interval '25 hours',deadline_at=statement_timestamp()-interval '25 hours'+interval '5 minutes' WHERE id=$1`, r.ID)
	changed := r.Binding
	changed.AppID = "22222222-2222-4222-8222-222222222222"
	changed.Revision = 2
	if prior, err := s.Prepare(t.Context(), changed, 3); !errors.Is(err, ErrUncertain) || prior.ID != r.ID {
		t.Fatal(prior, err)
	}
	for i := range 7 {
		prepared(t, s, fmt.Sprintf("client%d", i))
	}
	if _, err = s.Prepare(t.Context(), binding("ninth"), 2); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if prior, err := s.Lookup(t.Context(), r.ID); err != nil || prior.State != "uncertain" {
		t.Fatal(prior, err)
	}
	// A separately controlled destructive fixture deletion must never cause upsert.
	exec(t, f.Admin, `DELETE FROM service_conduit_creations WHERE id=$1`, r.ID)
	if _, next, err := s.Start(t.Context(), r.ID, r.Binding, 2); !errors.Is(err, ErrConflict) || next != nil {
		t.Fatal(err)
	}
	if _, err = s.Complete(t.Context(), a, created(r), nil); !errors.Is(err, ErrUncertain) {
		t.Fatal(err)
	}
	if _, err = s.Lookup(t.Context(), r.ID); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}

func TestOwnershipCompletionNormalizationAndClaimFences(t *testing.T) {
	f, s := openFixture(t)
	for _, kind := range []string{"not_started", "refused", "refused_without_error", "unknown", "missing", "wrong_client", "wrong_count", "bad_id", "bad_time", "provider_error", "expired", "lost_claim"} {
		t.Run(kind, func(t *testing.T) {
			r := prepared(t, s, kind)
			a := started(t, s, r)
			result := created(r)
			var providerErr error
			switch kind {
			case "not_started":
				result = twitchconduit.Creation{Outcome: twitchconduit.CreationNotStarted}
				providerErr = twitchconduit.ErrUnauthorized
			case "refused":
				result = twitchconduit.Creation{Outcome: twitchconduit.CreationRefused}
				providerErr = twitchconduit.ErrCreationLimited
			case "refused_without_error":
				result = twitchconduit.Creation{Outcome: twitchconduit.CreationRefused}
			case "unknown":
				result.Outcome = "PRIVATE-status"
			case "missing":
				result.Conduit = nil
			case "wrong_client":
				result.Conduit.ClientID = "PRIVATE-client"
			case "wrong_count":
				result.Conduit.ShardCount++
			case "bad_id":
				result.Conduit.ID = "PRIVATE/url"
			case "bad_time":
				result.Conduit.CompletedAt = time.Time{}
			case "provider_error":
				providerErr = errors.New("PRIVATE-transport")
			case "expired":
				exec(t, f.Admin, `UPDATE service_conduit_creations SET created_at=statement_timestamp()-interval '6 minutes',deadline_at=statement_timestamp()-interval '1 minute' WHERE id=$1`, r.ID)
			case "lost_claim":
				exec(t, f.Admin, `UPDATE service_conduit_creations SET claim_hash=repeat('0',64) WHERE id=$1`, r.ID)
			}
			done, err := s.Complete(t.Context(), a, result, providerErr)
			if kind == "not_started" || kind == "refused" {
				if err != nil || done.State != "refused" || done.ConduitID != "" {
					t.Fatal(done, err)
				}
			} else if !errors.Is(err, ErrUncertain) || done.ConduitID != "" {
				t.Fatal(done, err)
			}
			raw, _ := json.Marshal(done)
			if strings.Contains(string(raw), "PRIVATE") || strings.Contains(fmt.Sprint(err), "PRIVATE") {
				t.Fatal("private completion data escaped")
			}
			if _, next, err := s.Start(t.Context(), r.ID, r.Binding, 2); err == nil || next != nil {
				t.Fatal("terminal action restarted")
			}
			// Release fixture-only rows so this table-driven suite tests completion,
			// rather than the separately tested eight-client admission bound.
			exec(t, f.Admin, `DELETE FROM service_conduit_creations WHERE id=$1`, r.ID)
		})
	}
}

func TestOwnershipExpiryAndRetention(t *testing.T) {
	f, s := openFixture(t)
	r := prepared(t, s, "expired")
	exec(t, f.Admin, `UPDATE service_conduit_creations SET created_at=statement_timestamp()-interval '6 minutes',deadline_at=statement_timestamp()-interval '1 minute' WHERE id=$1`, r.ID)
	prior, a, err := s.Start(t.Context(), r.ID, r.Binding, 2)
	if !errors.Is(err, ErrUnavailable) || a != nil || prior.State != "refused" || prior.Failure != "expired" {
		t.Fatal(prior, err)
	}
	exec(t, f.Admin, `UPDATE service_conduit_creations SET created_at=statement_timestamp()-interval '25 hours',deadline_at=statement_timestamp()-interval '25 hours'+interval '5 minutes' WHERE id=$1`, r.ID)
	next := prepared(t, s, "expired")
	if next.ID == r.ID {
		t.Fatal("expired original refreshed")
	}
	if _, err = s.Lookup(t.Context(), r.ID); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	exec(t, f.Admin, `INSERT INTO service_conduit_creations(app_id,client_id,app_revision,shard_count,state,completed_at,failure_code) SELECT $1,'filler',1,1,'refused',clock_timestamp(),'expired' FROM generate_series(1,999)`, binding("filler").AppID)
	if _, err = s.Prepare(t.Context(), binding("full"), 2); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if duplicate, err := s.Prepare(t.Context(), next.Binding, 2); err != nil || duplicate.ID != next.ID {
		t.Fatal(duplicate, err)
	}
}

func TestOwnershipRoleSchemaAndColumnDrift(t *testing.T) {
	f, s := openFixture(t)
	r := prepared(t, s, "client")
	for _, dsn := range []string{f.RuntimeURL, f.AppAdminURL, f.AppExecutionURL, f.WorkerURL} {
		if other, err := Open(t.Context(), dsn); !errors.Is(err, ErrBoundary) || other != nil {
			if other != nil {
				_ = other.Close()
			}
			t.Fatal("foreign role accepted", err)
		}
	}
	for _, test := range []struct{ change, restore string }{
		{`GRANT UPDATE(deadline_at) ON service_conduit_creations TO ` + f.role, `REVOKE UPDATE(deadline_at) ON service_conduit_creations FROM ` + f.role},
		{`GRANT INSERT(principal) ON service_conduit_creations TO ` + f.role, `REVOKE INSERT(principal) ON service_conduit_creations FROM ` + f.role},
		{`GRANT TRUNCATE ON service_conduit_creations TO ` + f.role, `REVOKE TRUNCATE ON service_conduit_creations FROM ` + f.role},
		{`GRANT SELECT ON service_app_credentials TO ` + f.role, `REVOKE SELECT ON service_app_credentials FROM ` + f.role},
		{`GRANT SELECT ON service_conduit_creations TO ` + f.role + ` WITH GRANT OPTION`, `REVOKE GRANT OPTION FOR SELECT ON service_conduit_creations FROM ` + f.role},
		{`ALTER TABLE service_conduit_creations NO FORCE ROW LEVEL SECURITY`, `ALTER TABLE service_conduit_creations FORCE ROW LEVEL SECURITY`},
		{`ALTER POLICY service_conduit_admin ON service_conduit_creations USING(false)`, `ALTER POLICY service_conduit_admin ON service_conduit_creations USING(true)`},
		{`DROP INDEX service_conduit_creation_active`, `CREATE UNIQUE INDEX service_conduit_creation_active ON service_conduit_creations(client_id) WHERE state IN('prepared','started','uncertain','created')`},
		{`ALTER ROLE ` + f.role + ` CREATEROLE`, `ALTER ROLE ` + f.role + ` NOCREATEROLE`},
	} {
		exec(t, f.Admin, test.change)
		if err := s.Validate(t.Context()); !errors.Is(err, ErrBoundary) {
			t.Fatal("drift accepted", test.change, err)
		}
		if _, a, err := s.Start(t.Context(), r.ID, r.Binding, 2); !errors.Is(err, ErrBoundary) || a != nil {
			t.Fatal("drift allowed claim", err)
		}
		exec(t, f.Admin, test.restore)
		if err := s.Validate(t.Context()); err != nil {
			t.Fatal("restored boundary refused", err)
		}
	}
	for _, q := range []string{`UPDATE service_conduit_creations SET deadline_at=clock_timestamp()`, `INSERT INTO service_conduit_creations(app_id,client_id,app_revision,shard_count,principal) VALUES('11111111-1111-4111-8111-111111111111','spoof',1,1,'spoof')`, `SELECT token_ciphertext FROM service_app_credentials`} {
		if _, err := s.db.Exec(q); err == nil {
			t.Fatal("forbidden SQL accepted")
		}
	}
	for _, state := range []string{"created", "uncertain", "refused"} {
		if _, err := f.Admin.Exec(`INSERT INTO service_conduit_creations(app_id,client_id,app_revision,shard_count,state) VALUES($1,'null-fence',1,1,$2)`, r.Binding.AppID, state); err == nil {
			t.Fatal("NULL state fence bypassed")
		}
	}
}

func TestOwnershipPostLockExpiryUnderInheritedIsolation(t *testing.T) {
	f, s := openFixture(t)
	r := prepared(t, s, "client")
	u, _ := url.Parse(f.dsn)
	q := u.Query()
	q.Set("default_transaction_isolation", "repeatable read")
	u.RawQuery = q.Encode()
	repeatable, err := Open(t.Context(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repeatable.Close() }()
	owner, err := f.Admin.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Rollback() }()
	if _, err = owner.Exec(`SELECT pg_advisory_xact_lock(hashtextextended('service_conduit_creation:'||current_database()||':'||current_schema(),0))`); err != nil {
		t.Fatal(err)
	}
	type answer struct {
		r   Intent
		a   *Attempt
		err error
	}
	done := make(chan answer, 1)
	go func() { r, a, e := repeatable.Start(t.Context(), r.ID, r.Binding, 2); done <- answer{r, a, e} }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		var waiting bool
		if err = f.Admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE usename=$1 AND wait_event_type='Lock')`, f.role).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("claim did not reach serialization lock")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err = owner.Exec(`UPDATE service_conduit_creations SET created_at=statement_timestamp()-interval '6 minutes',deadline_at=statement_timestamp()-interval '1 minute' WHERE id=$1`, r.ID); err != nil {
		t.Fatal(err)
	}
	if err = owner.Commit(); err != nil {
		t.Fatal(err)
	}
	a := <-done
	if !errors.Is(a.err, ErrUnavailable) || a.a != nil || a.r.Failure != "expired" {
		t.Fatal("stale deadline granted claim", a.r, a.err)
	}
}

func TestOwnershipCancellationAndBoundaryFailureRetainOriginal(t *testing.T) {
	f, s := openFixture(t)
	r := prepared(t, s, "client")
	owner, err := f.Admin.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Rollback() }()
	if _, err = owner.Exec(`SELECT pg_advisory_xact_lock(hashtextextended('service_conduit_creation:'||current_database()||':'||current_schema(),0))`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	_, a, err := s.Start(ctx, r.ID, r.Binding, 2)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) || a != nil {
		t.Fatal("canceled claim", err)
	}
	if err = owner.Rollback(); err != nil {
		t.Fatal(err)
	}
	lookup, err := s.Lookup(t.Context(), r.ID)
	if err != nil || lookup != r {
		t.Fatal("cancellation changed original", lookup, err)
	}
	attempt := started(t, s, r)
	exec(t, f.Admin, `GRANT UPDATE(deadline_at) ON service_conduit_creations TO `+f.role)
	if got, err := s.Complete(t.Context(), attempt, created(r), nil); !errors.Is(err, ErrUncertain) || got.State != "started" {
		t.Fatal(got, err)
	}
	exec(t, f.Admin, `REVOKE UPDATE(deadline_at) ON service_conduit_creations FROM `+f.role)
	lookup, err = s.Lookup(t.Context(), r.ID)
	if err != nil || lookup.State != "started" {
		t.Fatal(lookup, err)
	}
	if _, next, err := s.Start(t.Context(), r.ID, r.Binding, 2); !errors.Is(err, ErrUncertain) || next != nil {
		t.Fatal("failed completion permitted replay", err)
	}
}

func TestOwnershipRejectsOriginalSessionAndReachableRoles(t *testing.T) {
	f, s := openFixture(t)
	conn, err := f.Admin.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err = conn.ExecContext(t.Context(), `SET ROLE `+f.role); err != nil {
		t.Fatal(err)
	}
	if err = validate(t.Context(), conn); !errors.Is(err, ErrBoundary) {
		t.Fatal("privileged original session accepted", err)
	}
	if _, err = conn.ExecContext(t.Context(), `RESET ROLE`); err != nil {
		t.Fatal(err)
	}
	extra := f.role + "_extra"
	exec(t, f.Admin, `CREATE ROLE `+extra+` NOLOGIN; GRANT `+extra+` TO `+f.role+` WITH INHERIT FALSE`)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := f.Admin.ExecContext(ctx, `REVOKE `+extra+` FROM `+f.role+`; DROP ROLE `+extra); err != nil {
			t.Error(err)
		}
	})
	if err = s.Validate(t.Context()); !errors.Is(err, ErrBoundary) {
		t.Fatal("reachable noinherit role accepted", err)
	}
}

func TestOwnershipAdditiveMigrationPreservesCustodyAndIssuance(t *testing.T) {
	f := fixture(t)
	exec(t, f.Admin, `DROP TABLE service_conduit_creations; DELETE FROM overlaid_migrations WHERE name='0037_conduit_creation.sql'`)
	b := binding("existing")
	exec(t, f.Admin, `INSERT INTO service_app_credentials(id,client_id,revision,revoked_at) VALUES($1,$2,2,clock_timestamp())`, b.AppID, b.ClientID)
	exec(t, f.Admin, `INSERT INTO service_app_issuance(client_id,expected_revision,secret_fingerprint) VALUES($1,2,repeat('a',64))`, b.ClientID)
	var before, after string
	query := `SELECT json_build_object('credentials',(SELECT json_agg(t) FROM service_app_credentials t),'issuance',(SELECT json_agg(t) FROM service_app_issuance t))::text`
	if err := f.Admin.QueryRow(query).Scan(&before); err != nil {
		t.Fatal(err)
	}
	exec(t, f.Admin, f.migration)
	if err := f.Admin.QueryRow(query).Scan(&after); err != nil || after != before {
		t.Fatal("migration changed custody/issuance", err)
	}
	s, err := Open(t.Context(), f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	var count int
	if err = s.db.QueryRow(`SELECT count(*) FROM service_conduit_creations`).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	for _, dsn := range []string{f.RuntimeURL, f.AppAdminURL, f.AppExecutionURL, f.WorkerURL} {
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Fatal(err)
		}
		err = db.QueryRow(`SELECT count(*) FROM service_conduit_creations`).Scan(&count)
		_ = db.Close()
		if err == nil {
			t.Fatal("foreign role read private ledger")
		}
	}
}
