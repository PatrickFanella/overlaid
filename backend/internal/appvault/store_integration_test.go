package appvault

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchconduit"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const fixtureSecret = "fixture-secret"

func credential(t *testing.T, client string, ttl int) twitchconduit.AppCredential {
	t.Helper()
	a, err := twitchconduit.NewAppAuthorizer(twitchconduit.AppAuthorizationConfig{ClientID: client, ClientSecret: func(context.Context) (string, error) { return fixtureSecret, nil }, HTTPClient: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		var body string
		switch r.URL.Path {
		case "/oauth2/token":
			body = fmt.Sprintf(`{"access_token":"fixture-access","token_type":"bearer","expires_in":%d}`, ttl)
		case "/oauth2/validate":
			body = fmt.Sprintf(`{"client_id":%q,"expires_in":%d}`, client, ttl)
		default:
			return nil, errors.New("unexpected fixture request")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.Acquire(t.Context())
	if err != nil || result.Credential == nil {
		t.Fatal(result, err)
	}
	return *result.Credential
}
func keys() tenancy.Keyring {
	return tenancy.Keyring{Active: "v1", Keys: map[string][]byte{"v1": []byte(strings.Repeat("1", 32)), "v2": []byte(strings.Repeat("2", 32))}}
}
func fixture(t *testing.T) (testpostgres.TenantFixture, *Store) {
	t.Helper()
	f := testpostgres.NewTenantFixture(t, "")
	s, err := Open(t.Context(), f.AppAdminURL)
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
func execSQL(t *testing.T, db *sql.DB, statement string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), statement, args...); err != nil {
		t.Fatal(err)
	}
}
func install(t *testing.T, s *Store, client string, expected int64) Metadata {
	t.Helper()
	m, err := s.Install(t.Context(), keys(), fixtureSecret, credential(t, client, 3600), expected)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func role(dsn string) string { u, _ := url.Parse(dsn); return u.User.Username() }

func TestCustodyEncryptionRevisionsRevocationAndReceipts(t *testing.T) {
	f, s := fixture(t)
	ctx := t.Context()
	k := keys()
	c := credential(t, "ExactClient", 3600)
	if _, err := s.Install(ctx, k, "other-secret", c, 0); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.Install(ctx, k, fixtureSecret, twitchconduit.AppCredential{}, 0); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	m, err := s.Install(ctx, k, fixtureSecret, c, 0)
	if err != nil {
		t.Fatal(err)
	}
	var secret, token []byte
	var version string
	if err := f.Admin.QueryRow(`SELECT secret_ciphertext,token_ciphertext,key_version FROM service_app_credentials WHERE id=$1`, m.ID).Scan(&secret, &token, &version); err != nil {
		t.Fatal(err)
	}
	for field, sealed := range map[string][]byte{"secret": secret, "token": token} {
		raw, err := k.Unseal(scope, purpose(m.ID, m.ClientID, m.Revision, field), version, sealed)
		if err != nil || !json.Valid(raw) {
			t.Fatal("valid seal failed", err)
		}
		for _, binding := range []struct{ scope, purpose, version string }{{"tenant", purpose(m.ID, m.ClientID, 1, field), version}, {scope, purpose(m.ID, m.ClientID, 2, field), version}, {scope, purpose(m.ID, "exactclient", 1, field), version}, {scope, purpose("other", m.ClientID, 1, field), version}, {scope, purpose(m.ID, m.ClientID, 1, field+"other"), version}, {scope, purpose(m.ID, m.ClientID, 1, field), "v2"}} {
			if _, err := k.Unseal(binding.scope, binding.purpose, binding.version, sealed); err == nil {
				t.Fatal("cross binding decrypted")
			}
		}
		tampered := append([]byte(nil), sealed...)
		tampered[len(tampered)-1] ^= 1
		if _, err := k.Unseal(scope, purpose(m.ID, m.ClientID, 1, field), version, tampered); err == nil {
			t.Fatal("tampering accepted")
		}
	}
	k.Active = "v2"
	next, err := s.Install(ctx, k, fixtureSecret, c, 1)
	if err != nil || next.ID != m.ID || next.Revision != 2 {
		t.Fatal(next, err)
	}
	var rotated []byte
	var rotatedVersion string
	if err := f.Admin.QueryRow(`SELECT token_ciphertext,key_version FROM service_app_credentials WHERE id=$1`, m.ID).Scan(&rotated, &rotatedVersion); err != nil || rotatedVersion != "v2" {
		t.Fatal("rotation", err)
	}
	if _, err := k.Unseal(scope, purpose(m.ID, m.ClientID, 2, "token"), rotatedVersion, rotated); err != nil {
		t.Fatal("reseal", err)
	}
	onlyNew := tenancy.Keyring{Active: "v2", Keys: map[string][]byte{"v2": k.Keys["v2"]}}
	if _, err := onlyNew.Unseal(scope, purpose(m.ID, m.ClientID, 1, "token"), "v1", token); err == nil {
		t.Fatal("old ciphertext opened without retained key")
	}

	if _, err := s.Install(ctx, k, fixtureSecret, c, 1); !errors.Is(err, ErrRevision) {
		t.Fatal(err)
	}
	revoked, err := s.Revoke(ctx, m.ID, 2)
	if err != nil || !revoked.Revoked || revoked.Revision != 3 || revoked.ExpiresAt != nil {
		t.Fatal(revoked, err)
	}
	var cleared bool
	if err := f.Admin.QueryRow(`SELECT secret_ciphertext IS NULL AND token_ciphertext IS NULL AND key_version IS NULL AND validated_at IS NULL AND expires_at IS NULL FROM service_app_credentials WHERE id=$1`, m.ID).Scan(&cleared); err != nil || !cleared {
		t.Fatal("revocation retained ciphertext", err)
	}
	if _, err := s.Revoke(ctx, m.ID, 3); err != nil {
		t.Fatal(err)
	}
	receipts, err := s.Receipts(ctx, m.ID, 1000)
	if err != nil || len(receipts) != 3 {
		t.Fatal(receipts, err)
	}
	for i, r := range receipts {
		if r.Revision != int64(3-i) || r.Principal != role(f.AppAdminURL) || r.CreatedAt.IsZero() {
			t.Fatal(r)
		}
	}
	accounts, err := s.Accounts(ctx)
	if err != nil || len(accounts) != 1 {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(struct {
		Accounts []Metadata
		Receipts []Receipt
	}{accounts, receipts})
	for _, private := range []string{fixtureSecret, "fixture-access", "ciphertext", "keyVersion"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("private material exposed")
		}
	}
	restored := install(t, s, "ExactClient", 3)
	if restored.ID != m.ID || restored.Revision != 4 || restored.Revoked {
		t.Fatal(restored)
	}
	// Exact case is an independent client identity, never an implicit replacement.
	different := install(t, s, "exactclient", 0)
	if different.ID == m.ID {
		t.Fatal("client case folded")
	}
}

func TestCustodyAtomicConflictAndCapacity(t *testing.T) {
	f, s := fixture(t)
	ctx := t.Context()
	c := credential(t, "concurrent", 3600)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := s.Install(ctx, keys(), fixtureSecret, c, 0)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrRevision) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatal(successes, conflicts)
	}
	accounts, err := s.Accounts(ctx)
	if err != nil || len(accounts) != 1 {
		t.Fatal(err)
	}
	m := accounts[0]
	// A receipt failure must roll back the preceding credential update.
	execSQL(t, f.Admin, `INSERT INTO service_app_credential_audit(app_id,revision,action) VALUES($1,2,'install')`, m.ID)
	if _, err := s.Install(ctx, keys(), fixtureSecret, c, 1); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	accounts, err = s.Accounts(ctx)
	if err != nil || accounts[0].Revision != 1 {
		t.Fatal(accounts, err)
	}
	execSQL(t, f.Admin, `DELETE FROM service_app_credential_audit WHERE app_id=$1 AND revision=2`, m.ID)
	for i := 1; i < 8; i++ {
		install(t, s, fmt.Sprintf("client%d", i), 0)
	}
	if _, err := s.Revoke(ctx, m.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Install(ctx, keys(), fixtureSecret, credential(t, "ninth", 3600), 0); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if restored := install(t, s, "concurrent", 2); restored.Revision != 3 {
		t.Fatal(restored)
	}
}

func TestCustodyRoleIsolationAndDrift(t *testing.T) {
	t.Run("separate roles", func(t *testing.T) {
		f, _ := fixture(t)
		for _, dsn := range []string{f.RuntimeURL, f.WorkerURL, f.BotAdminURL, f.BotExecutorURL, f.PacerURL, f.OutboundWorkerURL, f.ModerationWorkerURL, f.StreamWorkerURL, f.ModerationPacerURL} {
			if s, err := Open(t.Context(), dsn); !errors.Is(err, ErrBoundary) {
				if s != nil {
					_ = s.Close()
				}
				t.Fatal("other role accepted", err)
			}
			db, err := sql.Open("pgx", dsn)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(t.Context(), `SELECT token_ciphertext FROM service_app_credentials`); err == nil {
				t.Error("other role can read app credentials")
			}
			if _, err := db.ExecContext(t.Context(), `SET ROLE `+role(f.AppAdminURL)); err == nil {
				t.Error("other role can assume app admin")
			}
			_ = db.Close()
		}
		if err := validate(t.Context(), f.Admin); !errors.Is(err, ErrBoundary) {
			t.Fatal("owner accepted", err)
		}
	})
	for name, statement := range map[string]string{
		"grant option":     "GRANT SELECT(secret_ciphertext) ON service_app_credentials TO %s WITH GRANT OPTION",
		"client write":     "GRANT UPDATE(client_id) ON service_app_credentials TO %s",
		"receipt spoof":    "GRANT INSERT(principal) ON service_app_credential_audit TO %s",
		"truncate":         "GRANT TRUNCATE ON service_app_credentials TO %s",
		"delete":           "GRANT DELETE ON service_app_credentials TO %s",
		"private column":   "GRANT SELECT(ciphertext) ON service_bot_accounts TO %s",
		"schema create":    "GRANT CREATE ON SCHEMA %s TO %s",
		"unforced":         "ALTER TABLE service_app_credentials NO FORCE ROW LEVEL SECURITY",
		"public policy":    "CREATE POLICY extra ON service_app_credentials TO PUBLIC USING(true)",
		"default spoof":    "ALTER TABLE service_app_credential_audit ALTER COLUMN principal SET DEFAULT 'spoof'",
		"constraint drift": "ALTER TABLE service_app_credentials DROP CONSTRAINT service_app_revision; ALTER TABLE service_app_credentials ADD CONSTRAINT service_app_revision CHECK(revision>0) NOT VALID",
		"private view":     "CREATE VIEW app_custody_private AS SELECT ciphertext FROM service_bot_accounts; GRANT SELECT ON app_custody_private TO %s",
		"rewrite rule":     "CREATE RULE app_discard AS ON INSERT TO service_app_credential_audit DO INSTEAD NOTHING",
		"security definer": "CREATE FUNCTION app_custody_fixture() RETURNS int LANGUAGE sql SECURITY DEFINER AS 'SELECT 1'",
		"changed check":    "ALTER POLICY service_app_admin ON service_app_credentials WITH CHECK(false)",
		"database create":  "ALTER ROLE %s CREATEDB",
		"column drift":     "ALTER TABLE service_app_credentials ADD COLUMN private_extra text",
		"admin role":       "ALTER ROLE %s CREATEROLE",
	} {
		t.Run(name, func(t *testing.T) {
			f, s := fixture(t)
			sqlText := statement
			if name == "schema create" {
				sqlText = fmt.Sprintf(statement, f.Schema, role(f.AppAdminURL))
			} else if strings.Contains(statement, "%s") {
				sqlText = fmt.Sprintf(statement, role(f.AppAdminURL))
			}
			execSQL(t, f.Admin, sqlText)
			if err := s.Validate(t.Context()); !errors.Is(err, ErrBoundary) {
				t.Fatal("drift accepted", err)
			}
			if _, err := s.Install(t.Context(), keys(), fixtureSecret, credential(t, "client", 3600), 0); !errors.Is(err, ErrBoundary) {
				t.Fatal("write accepted drift", err)
			}
			var count int
			if err := f.Admin.QueryRow(`SELECT count(*) FROM service_app_credentials`).Scan(&count); err != nil || count != 0 {
				t.Fatal(count, err)
			}
		})
	}
}

func TestCustodyReachableRolesAndConcurrentRevoke(t *testing.T) {
	t.Run("noinherit membership", func(t *testing.T) {
		f, s := fixture(t)
		execSQL(t, f.Admin, `ALTER ROLE `+role(f.AppAdminURL)+` NOINHERIT`)
		execSQL(t, f.Admin, `GRANT `+role(f.RuntimeURL)+` TO `+role(f.AppAdminURL))
		if err := s.Validate(t.Context()); !errors.Is(err, ErrBoundary) {
			t.Fatal("reachable role accepted", err)
		}
	})
	t.Run("original session", func(t *testing.T) {
		f, _ := fixture(t)
		// A privileged original session is still refused after assuming the narrow role.
		conn, err := f.Admin.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close() }()
		if _, err := conn.ExecContext(t.Context(), `SET ROLE `+role(f.AppAdminURL)); err != nil {
			t.Fatal(err)
		}
		defer func() { _, _ = conn.ExecContext(context.Background(), `RESET ROLE`) }()
		var current, original string
		if err := conn.QueryRowContext(t.Context(), `SELECT current_user,session_user`).Scan(&current, &original); err != nil || current == original {
			t.Fatal("fixture identity", err)
		}
		// queryer deliberately accepts the same connection-bound QueryRow contract.
		if err := validate(t.Context(), conn); !errors.Is(err, ErrBoundary) {
			t.Fatal("privileged original session accepted", err)
		}
	})
	t.Run("install versus revoke", func(t *testing.T) {
		f, s := fixture(t)
		m := install(t, s, "client", 0)
		c := credential(t, "client", 3600)
		start := make(chan struct{})
		result := make(chan error, 2)
		go func() { <-start; _, err := s.Install(t.Context(), keys(), fixtureSecret, c, 1); result <- err }()
		go func() { <-start; _, err := s.Revoke(t.Context(), m.ID, 1); result <- err }()
		close(start)
		ok, conflict := 0, 0
		for range 2 {
			err := <-result
			if err == nil {
				ok++
			} else if errors.Is(err, ErrRevision) {
				conflict++
			} else {
				t.Fatal(err)
			}
		}
		if ok != 1 || conflict != 1 {
			t.Fatal(ok, conflict)
		}
		var count int
		if err := f.Admin.QueryRow(`SELECT count(*) FROM service_app_credential_audit WHERE app_id=$1 AND revision=2`, m.ID).Scan(&count); err != nil || count != 1 {
			t.Fatal(count, err)
		}
	})
}

func TestCustodyPostLockExpiryAndGrantDrift(t *testing.T) {
	for _, drift := range []bool{false, true} {
		t.Run(fmt.Sprint(drift), func(t *testing.T) {
			f, s := fixture(t)
			ttl := 33
			if drift {
				ttl = 3600
			}
			c := credential(t, "client", ttl)
			tx, err := f.Admin.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback() }()
			if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended('service_app_custody:'||current_database()||':'||current_schema(),0))`); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { _, err := s.Install(t.Context(), keys(), fixtureSecret, c, 0); done <- err }()
			deadline := time.Now().Add(time.Second)
			waiting := false
			for time.Now().Before(deadline) {
				if err := f.Admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE usename=$1 AND wait_event='advisory')`, role(f.AppAdminURL)).Scan(&waiting); err != nil {
					t.Fatal(err)
				}
				if waiting {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !waiting {
				t.Fatal("install never reached administrative lock")
			}
			expected := ErrInvalid
			if drift {
				execSQL(t, f.Admin, `GRANT UPDATE(client_id) ON service_app_credentials TO `+role(f.AppAdminURL))
				expected = ErrBoundary
			} else {
				time.Sleep(3100 * time.Millisecond)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if !errors.Is(err, expected) {
					t.Fatal("post-lock fence", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("blocked install did not complete")
			}
			var count int
			if err := f.Admin.QueryRow(`SELECT count(*) FROM service_app_credentials`).Scan(&count); err != nil || count != 0 {
				t.Fatal(count, err)
			}
		})
	}
}

func TestCustodyReceiptRetentionBound(t *testing.T) {
	f, s := fixture(t)
	m := install(t, s, "client", 0)
	// Owner-only history seeding avoids a thousand redundant encryption calls.
	execSQL(t, f.Admin, `INSERT INTO service_app_credential_audit(app_id,revision,action) SELECT $1,n,'install' FROM generate_series(2,1000) n`, m.ID)
	execSQL(t, f.Admin, `UPDATE service_app_credentials SET revision=1000 WHERE id=$1`, m.ID)
	if next := install(t, s, "client", 1000); next.Revision != 1001 {
		t.Fatal(next)
	}
	receipts, err := s.Receipts(t.Context(), m.ID, 1000)
	if err != nil || len(receipts) != 1000 || receipts[0].Revision != 1001 || receipts[999].Revision != 2 {
		t.Fatal("history bound", len(receipts), err)
	}
	for i := 1; i < len(receipts); i++ {
		if receipts[i-1].Revision <= receipts[i].Revision {
			t.Fatal("history not revision ordered")
		}
	}
}

func TestCustodyMigrationPreservesExistingData(t *testing.T) {
	f := testpostgres.NewTenantFixture(t, `INSERT INTO app_metadata(key,value) VALUES('app-custody-preservation','original-data')`)
	var value string
	if err := f.Admin.QueryRow(`SELECT value FROM app_metadata WHERE key='app-custody-preservation'`).Scan(&value); err != nil || value != "original-data" {
		t.Fatal("existing application data changed", err)
	}
	var applied int
	if err := f.Admin.QueryRow(`SELECT count(*) FROM overlaid_migrations WHERE name='0034_service_app_custody.sql'`).Scan(&applied); err != nil || applied != 1 {
		t.Fatal("missing additive migration", err)
	}
}

func TestCustodyInvalidInputsAndCancellation(t *testing.T) {
	var missing *Store
	if err := missing.Validate(t.Context()); !errors.Is(err, ErrBoundary) {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), ""); !errors.Is(err, ErrBoundary) {
		t.Fatal(err)
	}
	if err := missing.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := missing.Revoke(t.Context(), "bad-id", 1); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := missing.Receipts(t.Context(), "bad-id", 1); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	c := credential(t, "client", 3600)
	k := keys()
	k.Keys["v1"] = []byte("bad-key")
	if _, err := missing.Install(t.Context(), k, fixtureSecret, c, 0); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := missing.Install(ctx, keys(), fixtureSecret, c, 0); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
