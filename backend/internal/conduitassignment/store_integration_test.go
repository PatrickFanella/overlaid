package conduitassignment

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

type assignmentFixture struct {
	testpostgres.TenantFixture
	dsn, role string
	migration string
}

func fixture(t *testing.T) assignmentFixture {
	t.Helper()
	f := testpostgres.NewTenantFixture(t, "")
	role := "assignment_admin_" + strings.TrimPrefix(f.Schema, "tenant_test_")
	_, source, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(source), "..", "..", "..", "migrations", "0038_conduit_assignment.sql"))
	if err != nil {
		t.Fatal(err)
	}
	migration := strings.ReplaceAll(string(raw), "overlaid_conduit_assignment_admin", role)
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
		if _, err := f.Admin.Exec(`DROP TABLE IF EXISTS service_conduit_assignments; DROP OWNED BY ` + role + `; DROP ROLE ` + role); err != nil {
			t.Error(err)
		}
	})
	return assignmentFixture{f, u.String(), role, migration}
}
func openFixture(t *testing.T) (assignmentFixture, *Store) {
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
	return Binding{AppID: "11111111-1111-4111-8111-111111111111", ClientID: client, Revision: 1, CreationID: "22222222-2222-4222-8222-222222222222"}
}
func prepared(t *testing.T, s *Store, client string) Intent {
	t.Helper()
	r, err := s.Prepare(t.Context(), binding(client), private())
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func started(t *testing.T, s *Store, r Intent) Attempt {
	t.Helper()
	_, a, err := s.Start(t.Context(), r.ID, r.Binding, private())
	if err != nil || a == nil {
		t.Fatal(err)
	}
	return *a
}
func private() twitchconduit.ShardBinding {
	return twitchconduit.ShardBinding{ConduitID: "owned-conduit", ShardCount: 2, ShardID: "0", SessionID: "PRIVATE-SESSION"}
}
func assigned(r Intent) twitchconduit.Assignment {
	now := time.Now()
	return twitchconduit.Assignment{Outcome: twitchconduit.AssignmentAssigned, Shard: &twitchconduit.AssignedShard{ClientID: r.Binding.ClientID, ConduitID: r.ConduitID, ShardID: fmt.Sprint(r.ShardID), StartedAt: now, CompletedAt: now}}
}

func expire(t *testing.T, f assignmentFixture, id string) {
	t.Helper()
	exec(t, f.Admin, `WITH stamp AS(SELECT clock_timestamp()-interval '11 seconds' AS t) UPDATE service_conduit_assignments SET created_at=stamp.t,deadline_at=stamp.t+interval '10 seconds' FROM stamp WHERE id=$1`, id)
}
func TestAssignmentLedgerClaimCompleteRestart(t *testing.T) {
	f, s := openFixture(t)
	r := prepared(t, s, "client")
	if r.Principal != f.role || r.State != "prepared" || r.DeadlineAt.Sub(r.CreatedAt) != 10*time.Second || !validID(r.ID) || len(r.SessionHash) != 64 {
		t.Fatal(r)
	}
	same, err := s.Prepare(t.Context(), r.Binding, private())
	if err != nil || same != r {
		t.Fatal("duplicate changed original", err)
	}
	for _, kind := range []string{"session", "revision", "creation", "count"} {
		b, p := r.Binding, private()
		switch kind {
		case "session":
			p.SessionID = "other"
		case "revision":
			b.Revision++
		case "creation":
			b.CreationID = "33333333-3333-4333-8333-333333333333"
		case "count":
			p.ShardCount++
		}
		if _, err = s.Prepare(t.Context(), b, p); !errors.Is(err, ErrConflict) {
			t.Fatal(kind, err)
		}
		if _, a, err := s.Start(t.Context(), r.ID, b, p); !errors.Is(err, ErrConflict) || a != nil {
			t.Fatal(kind, err)
		}
	}
	a := started(t, s, r)
	var claim string
	if err = f.Admin.QueryRow(`SELECT claim_hash FROM service_conduit_assignments WHERE id=$1`, r.ID).Scan(&claim); err != nil || len(claim) != 64 {
		t.Fatal(err)
	}
	raw, err := json.Marshal(a)
	if err != nil || strings.Contains(string(raw), claim) || strings.Contains(string(raw), "PRIVATE-SESSION") || strings.Contains(fmt.Sprintf("%+v %#v", a, a), claim) {
		t.Fatal("private claim/session escaped")
	}
	var decoded Attempt
	if err = json.Unmarshal(raw, &decoded); !errors.Is(err, ErrInvalid) || decoded.claim != nil {
		t.Fatal("reconstructed claim")
	}
	reopened, err := Open(t.Context(), f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	if _, again, err := reopened.Start(t.Context(), r.ID, r.Binding, private()); !errors.Is(err, ErrUncertain) || again != nil {
		t.Fatal("restart replay", err)
	}
	forged := a
	forged.original.ConduitID = "other"
	if _, err = reopened.Complete(t.Context(), forged, assigned(r), nil); !errors.Is(err, ErrUncertain) {
		t.Fatal("forged binding accepted", err)
	}
	done, err := reopened.Complete(t.Context(), a, assigned(r), nil)
	if err != nil || done.State != "assigned" {
		t.Fatal(done, err)
	}
	if _, again, err := s.Start(t.Context(), r.ID, r.Binding, private()); !errors.Is(err, ErrOwned) || again != nil {
		t.Fatal("assigned replay", err)
	}
	if _, err = s.Complete(t.Context(), a, assigned(r), nil); !errors.Is(err, ErrUncertain) {
		t.Fatal("claim reused", err)
	}
	if same, err = s.Lookup(t.Context(), r.ID); err != nil || same != done {
		t.Fatal("lookup drift", err)
	}
	p := private()
	p.SessionID = "replacement"
	if _, err = s.Prepare(t.Context(), r.Binding, p); !errors.Is(err, ErrConflict) {
		t.Fatal("implicit rebind", err)
	}
	var persisted string
	if err = f.Admin.QueryRow(`SELECT row_to_json(x)::text FROM service_conduit_assignments x WHERE id=$1`, r.ID).Scan(&persisted); err != nil || strings.Contains(persisted, "PRIVATE-SESSION") {
		t.Fatal("private persisted", err)
	}
}
func TestAssignmentLedgerConcurrentClaimAndIndependentSlots(t *testing.T) {
	_, s := openFixture(t)
	r := prepared(t, s, "client")
	var claims atomic.Int32
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			_, a, _ := s.Start(t.Context(), r.ID, r.Binding, private())
			if a != nil {
				claims.Add(1)
			}
		})
	}
	wg.Wait()
	if claims.Load() != 1 {
		t.Fatal("claims", claims.Load())
	}
	p := private()
	p.ShardID = "1"
	other, err := s.Prepare(t.Context(), r.Binding, p)
	if err != nil || other.ID == r.ID {
		t.Fatal("independent slot refused", err)
	}
	if _, a, err := s.Start(t.Context(), other.ID, r.Binding, p); err != nil || a == nil {
		t.Fatal(err)
	}
}
func TestAssignmentLedgerCompletionNormalization(t *testing.T) {
	for _, kind := range []string{"uncertain", "canceled", "mismatch", "missing", "contradiction", "interval", "late", "refused", "not_started"} {
		t.Run(kind, func(t *testing.T) {
			f, s := openFixture(t)
			r := prepared(t, s, "client")
			a := started(t, s, r)
			ctx := t.Context()
			result := assigned(r)
			var providerErr error
			switch kind {
			case "uncertain":
				result = twitchconduit.Assignment{Outcome: twitchconduit.AssignmentUncertain}
				providerErr = errors.New("PRIVATE-PROVIDER")
			case "canceled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			case "mismatch":
				result.Shard.ConduitID = "other"
			case "missing":
				result.Shard = nil
			case "contradiction":
				providerErr = errors.New("PRIVATE-PROVIDER")
			case "interval":
				result.Shard.CompletedAt = result.Shard.StartedAt.Add(11 * time.Second)
			case "late":
				expire(t, f, r.ID)
			case "refused":
				result = twitchconduit.Assignment{Outcome: twitchconduit.AssignmentRefused}
				providerErr = errors.New("PRIVATE-PROVIDER")
			case "not_started":
				result = twitchconduit.Assignment{Outcome: twitchconduit.AssignmentNotStarted}
				providerErr = errors.New("PRIVATE-PROVIDER")
			}
			done, err := s.Complete(ctx, a, result, providerErr)
			want := "uncertain"
			if kind == "refused" || kind == "not_started" {
				want = "refused"
			}
			if done.State != want || (want == "uncertain" && !errors.Is(err, ErrUncertain)) || (want == "refused" && err != nil) {
				t.Fatal(done, err)
			}
			if kind == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost")
			}
			if strings.Contains(fmt.Sprint(err), "PRIVATE") {
				t.Fatal("private error escaped")
			}
			if _, again, _ := s.Start(t.Context(), r.ID, r.Binding, private()); again != nil {
				t.Fatal("terminal replay")
			}
		})
	}
}
func TestAssignmentLedgerExpiryOriginalIdentityAndLockSnapshot(t *testing.T) {
	for _, drift := range []bool{false, true} {
		t.Run(fmt.Sprint(drift), func(t *testing.T) {
			f, s := openFixture(t)
			r := prepared(t, s, "client")
			s.db.SetMaxOpenConns(1)
			exec(t, s.db, `SET default_transaction_isolation='repeatable read'`)
			tx, err := f.Admin.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback() }()
			if _, err = tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended('service_conduit_assignment:'||current_database()||':'||current_schema(),0))`); err != nil {
				t.Fatal(err)
			}
			type answer struct {
				r   Intent
				a   *Attempt
				err error
			}
			done := make(chan answer, 1)
			go func() { v, a, e := s.Start(t.Context(), r.ID, r.Binding, private()); done <- answer{v, a, e} }()
			// Holding the same serialization lock forces Start's data read after commit.
			time.Sleep(100 * time.Millisecond)
			if drift {
				_, err = tx.Exec(`UPDATE service_conduit_assignments SET app_revision=app_revision+1 WHERE id=$1`, r.ID)
			} else {
				_, err = tx.Exec(`WITH stamp AS(SELECT clock_timestamp()-interval '11 seconds' AS t) UPDATE service_conduit_assignments SET created_at=stamp.t,deadline_at=stamp.t+interval '10 seconds' FROM stamp WHERE id=$1`, r.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
			select {
			case v := <-done:
				if v.a != nil {
					t.Fatal("late/drift claim")
				}
				want := ErrUnavailable
				if drift {
					want = ErrConflict
				}
				if !errors.Is(v.err, want) {
					t.Fatal(v.err)
				}
			case <-time.After(6 * time.Second):
				t.Fatal("blocked")
			}
			if !drift {
				same, err := s.Prepare(t.Context(), r.Binding, private())
				if !errors.Is(err, ErrUnavailable) || same.ID != r.ID || same.DeadlineAt.After(r.DeadlineAt) {
					t.Fatal("expired duplicate extended", same, err)
				}
			}
		})
	}
}
func TestAssignmentLedgerRetentionAndCapacity(t *testing.T) {
	f, s := openFixture(t)
	r := prepared(t, s, "client")
	a := started(t, s, r)
	expire(t, f, r.ID)
	p := private()
	p.ShardID = "1"
	if _, err := s.Prepare(t.Context(), r.Binding, p); err != nil {
		t.Fatal(err)
	}
	old, err := s.Lookup(t.Context(), r.ID)
	if err != nil || old.State != "uncertain" {
		t.Fatal(old, err)
	}
	if _, err = s.Prepare(t.Context(), r.Binding, private()); !errors.Is(err, ErrUncertain) {
		t.Fatal("expired claim replaced", err)
	}
	if _, err = s.Complete(t.Context(), a, assigned(r), nil); !errors.Is(err, ErrUncertain) {
		t.Fatal("recovered claim completed", err)
	}
	for i := 1; i < 8; i++ {
		if _, err = s.Prepare(t.Context(), binding(fmt.Sprint("client", i)), private()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.Prepare(t.Context(), binding("ninth"), private()); !errors.Is(err, ErrCapacity) {
		t.Fatal("client cap", err)
	}
	p.ConduitID = "another"
	if _, err = s.Prepare(t.Context(), r.Binding, p); err != nil {
		t.Fatal("existing client charged twice", err)
	}
}
func TestAssignmentLedgerRoleAndSchemaDrift(t *testing.T) {
	for _, kind := range []string{"tenant", "owner", "column", "table", "policy", "rls", "constraint", "index", "default", "trigger", "membership", "original_session", "truncate", "grant_option", "insert_time"} {
		t.Run(kind, func(t *testing.T) {
			f, s := openFixture(t)
			switch kind {
			case "tenant":
				if _, err := Open(t.Context(), f.RuntimeURL); !errors.Is(err, ErrBoundary) {
					t.Fatal("tenant opened", err)
				}
				return
			case "owner":
				if err := validate(t.Context(), f.Admin); !errors.Is(err, ErrBoundary) {
					t.Fatal("admin opened", err)
				}
				return
			case "membership":
				u, e := url.Parse(f.RuntimeURL)
				if e != nil {
					t.Fatal(e)
				}
				exec(t, f.Admin, `GRANT `+u.User.Username()+` TO `+f.role+` WITH INHERIT FALSE`)
			case "original_session":
				u, e := url.Parse(f.RuntimeURL)
				if e != nil {
					t.Fatal(e)
				}
				exec(t, f.Admin, `GRANT `+f.role+` TO `+u.User.Username())
				db, e := sql.Open("pgx", f.RuntimeURL)
				if e != nil {
					t.Fatal(e)
				}
				defer func() { _ = db.Close() }()
				db.SetMaxOpenConns(1)
				exec(t, db, `SET ROLE `+f.role)
				if e = validate(t.Context(), db); !errors.Is(e, ErrBoundary) {
					t.Fatal("original session escaped", e)
				}
				return
			case "truncate":
				exec(t, f.Admin, `GRANT TRUNCATE ON service_conduit_assignments TO `+f.role)
			case "grant_option":
				exec(t, f.Admin, `GRANT SELECT ON service_conduit_assignments TO `+f.role+` WITH GRANT OPTION`)
			case "insert_time":
				exec(t, f.Admin, `GRANT INSERT(created_at) ON service_conduit_assignments TO `+f.role)
			case "column":
				exec(t, f.Admin, `GRANT UPDATE(session_hash) ON service_conduit_assignments TO `+f.role)
			case "table":
				exec(t, f.Admin, `CREATE TABLE private_probe(secret text); GRANT SELECT ON private_probe TO `+f.role)
			case "policy":
				exec(t, f.Admin, `CREATE POLICY extra ON service_conduit_assignments USING(true)`)
			case "rls":
				exec(t, f.Admin, `ALTER TABLE service_conduit_assignments NO FORCE ROW LEVEL SECURITY`)
			case "constraint":
				exec(t, f.Admin, `ALTER TABLE service_conduit_assignments DROP CONSTRAINT service_conduit_assignment_deadline`)
			case "index":
				exec(t, f.Admin, `DROP INDEX service_conduit_assignment_active`)
			case "default":
				exec(t, f.Admin, `ALTER TABLE service_conduit_assignments ALTER COLUMN deadline_at SET DEFAULT statement_timestamp()+interval '20 seconds'`)
			case "trigger":
				exec(t, f.Admin, `CREATE FUNCTION deny_assignment() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RETURN NEW; END'; CREATE TRIGGER assignment_drift BEFORE INSERT ON service_conduit_assignments FOR EACH ROW EXECUTE FUNCTION deny_assignment()`)
			}
			if err := s.Validate(t.Context()); !errors.Is(err, ErrBoundary) {
				t.Fatal("drift accepted", err)
			}
			if _, err := s.Prepare(t.Context(), binding("client"), private()); !errors.Is(err, ErrBoundary) {
				t.Fatal("mutation after drift", err)
			}
		})
	}
}
func TestAssignmentLedgerInvalidBoundaries(t *testing.T) {
	var missing *Store
	if err := missing.Validate(t.Context()); !errors.Is(err, ErrBoundary) {
		t.Fatal(err)
	}
	_, s := openFixture(t)
	b := binding("client")
	p := private()
	b.CreationID = "bad"
	if _, err := s.Prepare(t.Context(), b, p); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	p.ShardID = "00"
	if _, err := s.Prepare(t.Context(), binding("client"), p); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.Lookup(t.Context(), "bad"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, a, err := s.Start(t.Context(), "33333333-3333-4333-8333-333333333333", binding("client"), private()); !errors.Is(err, ErrConflict) || a != nil {
		t.Fatal(err)
	}
	if _, err := s.Complete(t.Context(), Attempt{}, twitchconduit.Assignment{}, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Prepare(ctx, binding("client"), private()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestAssignmentFingerprintContract(t *testing.T) {
	b, p := binding("client"), private()
	if fingerprint(b, p) != "fd647dac0bb830a908a8f357dd6d1ca2b761559c541f60c2efd1d25ebb9af680" {
		t.Fatal("persisted fingerprint contract changed")
	}
	p.SessionID = "other"
	if fingerprint(b, p) == fingerprint(b, private()) {
		t.Fatal("session collision")
	}
	if validPrivate(twitchconduit.ShardBinding{ConduitID: "c", ShardCount: 1, ShardID: "0", SessionID: "bad\nline"}) {
		t.Fatal("private input grammar")
	}
}
func TestAssignmentLedgerRowBound(t *testing.T) {
	f, s := openFixture(t)
	exec(t, f.Admin, `INSERT INTO service_conduit_assignments(app_id,client_id,app_revision,creation_id,conduit_id,shard_count,shard_id,session_hash) SELECT '11111111-1111-4111-8111-111111111111','client',1,'22222222-2222-4222-8222-222222222222','conduit',20000,n,lpad(to_hex(n),64,'0') FROM generate_series(0,999)n`)
	if _, err := s.Prepare(t.Context(), binding("client"), private()); !errors.Is(err, ErrCapacity) {
		t.Fatal("row capacity", err)
	}
}

func TestAssignmentLedgerRetentionPreservesClaimedAndAssigned(t *testing.T) {
	f, s := openFixture(t)
	refused := prepared(t, s, "refused")
	a := started(t, s, refused)
	if _, err := s.Complete(t.Context(), a, twitchconduit.Assignment{Outcome: twitchconduit.AssignmentRefused}, errors.New("PRIVATE")); err != nil {
		t.Fatal(err)
	}
	unknown := prepared(t, s, "unknown")
	_ = started(t, s, unknown)
	owned := prepared(t, s, "owned")
	a = started(t, s, owned)
	if _, err := s.Complete(t.Context(), a, assigned(owned), nil); err != nil {
		t.Fatal(err)
	}
	exec(t, f.Admin, `WITH stamp AS(SELECT clock_timestamp()-interval '25 hours' AS t) UPDATE service_conduit_assignments SET created_at=stamp.t,deadline_at=stamp.t+interval '10 seconds' FROM stamp`)
	if _, err := s.Prepare(t.Context(), binding("fresh"), private()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Lookup(t.Context(), refused.ID); !errors.Is(err, ErrConflict) {
		t.Fatal("old refusal retained", err)
	}
	for _, id := range []string{unknown.ID, owned.ID} {
		r, err := s.Lookup(t.Context(), id)
		if err != nil || (r.State != "uncertain" && r.State != "assigned") {
			t.Fatal("ownership erased", r, err)
		}
	}
}
func TestAssignmentLedgerColumnPrivilegesAndIdentity(t *testing.T) {
	f, s := openFixture(t)
	r := prepared(t, s, "client")
	if _, err := s.db.ExecContext(t.Context(), `UPDATE service_conduit_assignments SET session_hash=repeat('0',64) WHERE id=$1`, r.ID); err == nil {
		t.Fatal("immutable column writable")
	}
	if _, err := s.db.ExecContext(t.Context(), `INSERT INTO service_conduit_assignments(id) VALUES(gen_random_uuid())`); err == nil {
		t.Fatal("identity insert grant")
	}
	if _, err := s.db.QueryContext(t.Context(), `SELECT * FROM service_app_credentials`); err == nil {
		t.Fatal("custody table readable")
	}
	identity, err := s.DatabaseIdentity(t.Context())
	if err != nil || identity[0] == "" || identity[1] != f.Schema {
		t.Fatal(identity, err)
	}
}
