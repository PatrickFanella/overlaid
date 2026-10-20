package appvault

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
)

func executionFixture(t *testing.T) (testpostgres.TenantFixture, *Store, *ExecutionStore, Binding) {
	t.Helper()
	f, admin := fixture(t)
	m := install(t, admin, "client", 0)
	execution, err := OpenExecution(t.Context(), f.AppExecutionURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := execution.Close(); err != nil {
			t.Error(err)
		}
	})
	return f, admin, execution, Binding{AppID: m.ID, ClientID: m.ClientID, Revision: m.Revision}
}
func inspectorClient(t *testing.T, modify func(*http.Request, int, string) (string, error)) (*http.Client, *atomic.Int32) {
	t.Helper()
	calls := new(atomic.Int32)
	validates := new(atomic.Int32)
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.Method != "GET" || r.URL.Scheme != "https" {
			return nil, errors.New("fixture forbidden transport")
		}
		expected := "Bearer fixture-access"
		if r.Header.Get("Authorization") != expected {
			return nil, errors.New("fixture wrong credential")
		}
		var body string
		n := 0
		switch r.URL.Host + r.URL.Path {
		case "id.twitch.tv/oauth2/validate":
			n = int(validates.Add(1))
			body = `{"client_id":"client","expires_in":3600}`
		case "api.twitch.tv/helix/eventsub/conduits":
			body = `{"data":[{"id":"selected","shard_count":1}],"pagination":{}}`
		case "api.twitch.tv/helix/eventsub/conduits/shards":
			if r.URL.Query().Get("conduit_id") != "selected" {
				return nil, errors.New("wrong conduit")
			}
			body = `{"data":[{"id":"0","status":"enabled","transport":{"method":"websocket","session_id":"PRIVATE-SESSION"}}],"pagination":{}}`
		default:
			return nil, errors.New("fixture unexpected provider request")
		}
		if r.URL.Host == "api.twitch.tv" && r.Header.Get("Client-Id") != "client" {
			return nil, errors.New("fixture client header")
		}
		var err error
		if modify != nil {
			body, err = modify(r, n, body)
			if err != nil {
				return nil, err
			}
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	return client, calls
}
func waitAppLock(t *testing.T, f testpostgres.TenantFixture, dsn string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if err := f.Admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE usename=$1 AND wait_event='advisory')`, role(dsn)).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("caller did not reach custody lock")
}

func TestExecutionInspectionCompositionPrivacyAndPreservation(t *testing.T) {
	f, _, s, b := executionFixture(t)
	client, calls := inspectorClient(t, nil)
	var beforeSecret, beforeToken []byte
	var beforeRevision, beforeReceipts int
	if err := f.Admin.QueryRow(`SELECT secret_ciphertext,token_ciphertext,revision,(SELECT count(*) FROM service_app_credential_audit WHERE app_id=$1) FROM service_app_credentials WHERE id=$1`, b.AppID).Scan(&beforeSecret, &beforeToken, &beforeRevision, &beforeReceipts); err != nil {
		t.Fatal(err)
	}
	observation, err := s.InspectConduit(t.Context(), keys(), b, "selected", client)
	if err != nil || observation.ClientID != "client" || observation.ConduitID != "selected" || len(observation.Shards) != 1 || calls.Load() != 5 {
		t.Fatal(observation, err, calls.Load())
	}
	raw, _ := json.Marshal(observation)
	for _, private := range []string{fixtureSecret, "fixture-access", "PRIVATE-SESSION", "ciphertext", "keyVersion"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("private output")
		}
	}
	var secret, token []byte
	var revision, receipts int
	if err := f.Admin.QueryRow(`SELECT secret_ciphertext,token_ciphertext,revision,(SELECT count(*) FROM service_app_credential_audit WHERE app_id=$1) FROM service_app_credentials WHERE id=$1`, b.AppID).Scan(&secret, &token, &revision, &receipts); err != nil || !bytes.Equal(secret, beforeSecret) || !bytes.Equal(token, beforeToken) || revision != beforeRevision || receipts != beforeReceipts {
		t.Fatal("inspection changed custody", err)
	}
	// Reapply only the additive policy/grant migration around existing input.
	execSQL(t, f.Admin, `DROP POLICY service_app_execution ON service_app_credentials; DELETE FROM overlaid_migrations WHERE name='0035_service_app_execution.sql'`)
	if err := s.Validate(t.Context()); !errors.Is(err, ErrBoundary) {
		t.Fatal("missing policy accepted", err)
	}
	migration, err := os.ReadFile("../../../migrations/0035_service_app_execution.sql")
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, f.Admin, strings.ReplaceAll(string(migration), "overlaid_app_execution", role(f.AppExecutionURL)))
	if err := s.Validate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := f.Admin.QueryRow(`SELECT token_ciphertext FROM service_app_credentials WHERE id=$1`, b.AppID).Scan(&token); err != nil || !bytes.Equal(token, beforeToken) {
		t.Fatal("migration changed credential", err)
	}
}

func TestExecutionRoleIsolationAndDrift(t *testing.T) {
	t.Run("roles", func(t *testing.T) {
		f, _, s, b := executionFixture(t)
		for _, dsn := range []string{f.AppAdminURL, f.RuntimeURL, f.WorkerURL, f.BotAdminURL, f.BotExecutorURL, f.PacerURL, f.OutboundWorkerURL, f.ModerationWorkerURL, f.StreamWorkerURL, f.ModerationPacerURL} {
			if store, err := OpenExecution(t.Context(), dsn); !errors.Is(err, ErrBoundary) {
				if store != nil {
					_ = store.Close()
				}
				t.Fatal("wrong execution role accepted", err)
			}
		}
		if err := validateExecution(t.Context(), f.Admin); !errors.Is(err, ErrBoundary) {
			t.Fatal("owner accepted", err)
		}
		for _, statement := range []string{`SELECT secret_ciphertext FROM service_app_credentials`, `SELECT * FROM service_app_credentials`, `SELECT * FROM service_app_credential_audit`, `UPDATE service_app_credentials SET revision=revision+1`, `DELETE FROM service_app_credentials`, `SET ROLE ` + role(f.AppAdminURL)} {
			if _, err := s.db.ExecContext(t.Context(), statement); err == nil {
				t.Fatal("forbidden grant", statement)
			}
		}
		execSQL(t, f.Admin, `UPDATE service_app_credentials SET secret_ciphertext=NULL,token_ciphertext=NULL,key_version=NULL,validated_at=NULL,expires_at=NULL,revoked_at=clock_timestamp() WHERE id=$1`, b.AppID)
		var count int
		if err := s.db.QueryRow(`SELECT count(*) FROM service_app_credentials WHERE id=$1`, b.AppID).Scan(&count); err != nil || count != 0 {
			t.Fatal("revoked row visible", count, err)
		}
	})
	t.Run("privileged original session", func(t *testing.T) {
		f, _, _, _ := executionFixture(t)
		conn, err := f.Admin.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close() }()
		if _, err := conn.ExecContext(t.Context(), `SET ROLE `+role(f.AppExecutionURL)); err != nil {
			t.Fatal(err)
		}
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, _ = conn.ExecContext(ctx, `RESET ROLE`)
		}()
		if err := validateExecution(t.Context(), conn); !errors.Is(err, ErrBoundary) {
			t.Fatal("privileged original identity accepted", err)
		}
	})

	for name, statement := range map[string]string{
		"grant option":     "GRANT SELECT(token_ciphertext) ON service_app_credentials TO %s WITH GRANT OPTION",
		"secret column":    "GRANT SELECT(secret_ciphertext) ON service_app_credentials TO %s",
		"table select":     "GRANT SELECT ON service_app_credentials TO %s",
		"audit read":       "GRANT SELECT ON service_app_credential_audit TO %s",
		"column write":     "GRANT UPDATE(id) ON service_app_credentials TO %s",
		"private view":     "CREATE VIEW app_execution_private AS SELECT ciphertext FROM service_bot_accounts; GRANT SELECT ON app_execution_private TO %s",
		"broad policy":     "ALTER POLICY service_app_execution ON service_app_credentials USING(true)",
		"public policy":    "ALTER POLICY service_app_execution ON service_app_credentials TO PUBLIC",
		"unforced":         "ALTER TABLE service_app_credentials NO FORCE ROW LEVEL SECURITY",
		"default":          "ALTER TABLE service_app_credentials ALTER COLUMN revision SET DEFAULT 2",
		"constraint":       "ALTER TABLE service_app_credentials DROP CONSTRAINT service_app_revision; ALTER TABLE service_app_credentials ADD CONSTRAINT service_app_revision CHECK(revision>=0)",
		"database create":  "ALTER ROLE %s CREATEDB",
		"schema create":    "GRANT CREATE ON SCHEMA %s TO %s",
		"truncate":         "GRANT TRUNCATE ON service_app_credentials TO %s",
		"admin flag":       "ALTER ROLE %s CREATEROLE",
		"security definer": "CREATE FUNCTION app_execution_private_fn() RETURNS int LANGUAGE sql SECURITY DEFINER AS 'SELECT 1'",
	} {
		t.Run(name, func(t *testing.T) {
			f, _, s, b := executionFixture(t)
			if name == "schema create" {
				statement = fmt.Sprintf(statement, f.Schema, role(f.AppExecutionURL))
			} else if strings.Contains(statement, "%s") {
				statement = fmt.Sprintf(statement, role(f.AppExecutionURL))
			}
			execSQL(t, f.Admin, statement)
			if err := s.Validate(t.Context()); !errors.Is(err, ErrBoundary) {
				t.Fatal("execution drift accepted", err)
			}
			client, calls := inspectorClient(t, nil)
			if _, err := s.InspectConduit(t.Context(), keys(), b, "selected", client); !errors.Is(err, ErrBoundary) || calls.Load() != 0 {
				t.Fatal("drift reached provider", err, calls.Load())
			}
		})
	}
	t.Run("combined reachable memberships", func(t *testing.T) {
		f, admin, s, _ := executionFixture(t)
		execSQL(t, f.Admin, `ALTER ROLE `+role(f.AppExecutionURL)+` NOINHERIT; GRANT `+role(f.AppAdminURL)+` TO `+role(f.AppExecutionURL))
		if err := s.Validate(t.Context()); !errors.Is(err, ErrBoundary) {
			t.Fatal("reachable admin accepted", err)
		}
		// Reverse the membership without an inherited grant so both directions are checked.
		execSQL(t, f.Admin, `REVOKE `+role(f.AppAdminURL)+` FROM `+role(f.AppExecutionURL)+`; GRANT `+role(f.AppExecutionURL)+` TO `+role(f.AppAdminURL)+` WITH INHERIT FALSE`)
		if err := admin.Validate(t.Context()); !errors.Is(err, ErrBoundary) {
			t.Fatal("admin can reach execution", err)
		}
	})
}

func TestExecutionPostLockFences(t *testing.T) {
	for _, drift := range []bool{false, true} {
		t.Run(fmt.Sprint(drift), func(t *testing.T) {
			f, admin := fixture(t)
			s, err := OpenExecution(t.Context(), f.AppExecutionURL)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = s.Close() }()
			ttl := 33
			if drift {
				ttl = 3600
			}
			m, err := admin.Install(t.Context(), keys(), fixtureSecret, credential(t, "client", ttl), 0)
			if err != nil {
				t.Fatal(err)
			}
			b := Binding{m.ID, m.ClientID, m.Revision}
			lock, err := f.Admin.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = lock.Rollback() }()
			if _, err := lock.Exec(`SELECT pg_advisory_xact_lock(hashtextextended('service_app_custody:'||current_database()||':'||current_schema(),0))`); err != nil {
				t.Fatal(err)
			}
			client, calls := inspectorClient(t, nil)
			done := make(chan error, 1)
			go func() { _, err := s.InspectConduit(t.Context(), keys(), b, "selected", client); done <- err }()
			waitAppLock(t, f, f.AppExecutionURL)
			expected := ErrInvalid
			if drift {
				execSQL(t, f.Admin, `GRANT SELECT(secret_ciphertext) ON service_app_credentials TO `+role(f.AppExecutionURL))
				expected = ErrBoundary
			} else {
				time.Sleep(3200 * time.Millisecond)
			}
			if err := lock.Commit(); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if !errors.Is(err, expected) || calls.Load() != 0 {
					t.Fatal(err, calls.Load())
				}
			case <-time.After(3 * time.Second):
				t.Fatal("inspection remained blocked")
			}
		})
	}
}

func TestExecutionInspectionSerializesRevocationAndCancellation(t *testing.T) {
	for _, interrupt := range []bool{false, true} {
		t.Run(fmt.Sprint(interrupt), func(t *testing.T) {
			f, admin, s, b := executionFixture(t)
			entered := make(chan struct{})
			release := make(chan struct{})
			client, _ := inspectorClient(t, func(r *http.Request, n int, body string) (string, error) {
				if n == 1 {
					close(entered)
					select {
					case <-release:
					case <-r.Context().Done():
						return "", r.Context().Err()
					}
				}
				return body, nil
			})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			inspected := make(chan error, 1)
			go func() { _, err := s.InspectConduit(ctx, keys(), b, "selected", client); inspected <- err }()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("provider read did not begin")
			}
			revoked := make(chan error, 1)
			go func() { _, err := admin.Revoke(t.Context(), b.AppID, 1); revoked <- err }()
			waitAppLock(t, f, f.AppAdminURL)
			select {
			case err := <-revoked:
				t.Fatal("revocation passed active inspection", err)
			default:
			}
			if interrupt {
				cancel()
			} else {
				close(release)
			}
			select {
			case err := <-inspected:
				if interrupt && !errors.Is(err, context.Canceled) || !interrupt && err != nil {
					t.Fatal("inspection outcome", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("inspection did not finish")
			}
			select {
			case err := <-revoked:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("revocation did not resume")
			}
			client, calls := inspectorClient(t, nil)
			observation, err := s.InspectConduit(t.Context(), keys(), b, "selected", client)
			if !errors.Is(err, ErrRevision) || observation.ClientID != "" || calls.Load() != 0 {
				t.Fatal("revoked inspection escaped", observation, err)
			}
		})
	}
}

func TestExecutionFinalCustodyAndRoleFences(t *testing.T) {
	for _, drift := range []string{"revision", "grant", "expiry"} {
		t.Run(drift, func(t *testing.T) {
			f, _, s, b := executionFixture(t)
			changed := new(atomic.Bool)
			client, _ := inspectorClient(t, func(r *http.Request, n int, body string) (string, error) {
				if n == 1 {
					changed.Store(true)
					switch drift {
					case "revision":
						execSQL(t, f.Admin, `UPDATE service_app_credentials SET revision=2 WHERE id=$1`, b.AppID)
					case "grant":
						execSQL(t, f.Admin, `GRANT SELECT(secret_ciphertext) ON service_app_credentials TO `+role(f.AppExecutionURL))
					case "expiry":
						execSQL(t, f.Admin, `UPDATE service_app_credentials SET expires_at=clock_timestamp()+interval '20 seconds' WHERE id=$1`, b.AppID)
					}
				}
				return body, nil
			})
			observation, err := s.InspectConduit(t.Context(), keys(), b, "selected", client)
			expected := ErrRevision
			if drift == "grant" {
				expected = ErrBoundary
			}
			if !changed.Load() || !errors.Is(err, expected) || observation.ClientID != "" || len(observation.Shards) != 0 {
				t.Fatal("partial report after drift", observation, err)
			}
		})
	}
}

func TestExecutionPrivateDocumentAndIdentityRefusal(t *testing.T) {
	for _, damage := range []string{"tamper", "purpose", "key", "client", "revision", "duplicate", "time", "invalid-json", "short-row"} {
		t.Run(damage, func(t *testing.T) {
			f, _, s, b := executionFixture(t)
			var cipher []byte
			var version string
			var validated, expires time.Time
			if err := f.Admin.QueryRow(`SELECT token_ciphertext,key_version,validated_at,expires_at FROM service_app_credentials WHERE id=$1`, b.AppID).Scan(&cipher, &version, &validated, &expires); err != nil {
				t.Fatal(err)
			}
			raw, err := keys().Unseal(scope, purpose(b.AppID, b.ClientID, b.Revision, "token"), version, cipher)
			if err != nil {
				t.Fatal(err)
			}
			field := "token"
			switch damage {
			case "tamper":
				cipher[len(cipher)-1] ^= 1
			case "purpose":
				field = "secret"
			case "key":
				version = "missing"
			case "client":
				raw = bytes.ReplaceAll(raw, []byte(`"clientId":"client"`), []byte(`"clientId":"other"`))
			case "revision":
				raw = bytes.ReplaceAll(raw, []byte(`"revision":1`), []byte(`"revision":2`))
			case "duplicate":
				raw = append([]byte(`{"clientId":"client",`), raw[1:]...)
			case "time":
				var doc map[string]any
				if err := json.Unmarshal(raw, &doc); err != nil {
					t.Fatal(err)
				}
				doc["validatedAt"] = validated.Add(2 * time.Microsecond)
				raw, _ = json.Marshal(doc)
			case "invalid-json":
				raw = []byte(`{"PRIVATE":"unfinished`)
			case "short-row":
				execSQL(t, f.Admin, `UPDATE service_app_credentials SET expires_at=clock_timestamp()+interval '20 seconds' WHERE id=$1`, b.AppID)
			}
			if damage != "tamper" && damage != "key" && damage != "short-row" {
				cipher, err = keys().Seal(scope, purpose(b.AppID, b.ClientID, b.Revision, field), raw)
				if err != nil {
					t.Fatal(err)
				}
			}
			execSQL(t, f.Admin, `UPDATE service_app_credentials SET token_ciphertext=$2,key_version=$3 WHERE id=$1`, b.AppID, cipher, version)
			client, calls := inspectorClient(t, nil)
			observation, err := s.InspectConduit(t.Context(), keys(), b, "selected", client)
			if !errors.Is(err, ErrInvalid) || observation.ClientID != "" || calls.Load() != 0 || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal(observation, err, calls.Load())
			}
		})
	}
	for _, last := range []bool{false, true} {
		t.Run(fmt.Sprint(last), func(t *testing.T) {
			_, _, s, b := executionFixture(t)
			client, _ := inspectorClient(t, func(r *http.Request, n int, body string) (string, error) {
				if n == 1 && !last || n == 2 && last {
					return `{"client_id":"client","login":"PRIVATE-USER","user_id":"PRIVATE-USER","expires_in":3600}`, nil
				}
				return body, nil
			})
			observation, err := s.InspectConduit(t.Context(), keys(), b, "selected", client)
			if err == nil || observation.ClientID != "" || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("provider identity escaped", observation, err)
			}
		})
	}
}

func TestExecutionBindingAndContextGuards(t *testing.T) {
	_, _, s, b := executionFixture(t)
	client, calls := inspectorClient(t, nil)
	for _, binding := range []Binding{{}, {AppID: b.AppID, ClientID: "client", Revision: 0}, {AppID: b.AppID, ClientID: "other", Revision: 1}, {AppID: b.AppID, ClientID: b.ClientID, Revision: 2}} {
		if observation, err := s.InspectConduit(t.Context(), keys(), binding, "selected", client); err == nil || observation.ClientID != "" || calls.Load() != 0 {
			t.Fatal(observation, err, calls.Load())
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.InspectConduit(ctx, keys(), b, "selected", client); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	var missing *ExecutionStore
	if err := missing.Close(); err != nil {
		t.Fatal(err)
	}
	if err := missing.Validate(t.Context()); !errors.Is(err, ErrBoundary) {
		t.Fatal(err)
	}
	if store, err := OpenExecution(t.Context(), ""); !errors.Is(err, ErrBoundary) || store != nil {
		t.Fatal(err)
	}
}

func TestExecutionRotationRequiresExactRevisionAndRetainedKey(t *testing.T) {
	_, admin, s, b := executionFixture(t)
	k := keys()
	k.Active = "v2"
	next, err := admin.Install(t.Context(), k, fixtureSecret, credential(t, "client", 3600), 1)
	if err != nil {
		t.Fatal(err)
	}
	current := Binding{next.ID, next.ClientID, next.Revision}
	client, calls := inspectorClient(t, nil)
	if _, err := s.InspectConduit(t.Context(), k, b, "selected", client); !errors.Is(err, ErrRevision) || calls.Load() != 0 {
		t.Fatal("stale revision inspected", err)
	}
	onlyOld := keys()
	delete(onlyOld.Keys, "v2")
	if _, err := s.InspectConduit(t.Context(), onlyOld, current, "selected", client); !errors.Is(err, ErrInvalid) || calls.Load() != 0 {
		t.Fatal("missing rotation key accepted", err)
	}
	onlyCurrent := keys()
	delete(onlyCurrent.Keys, "v1")
	onlyCurrent.Active = "v2"
	if report, err := s.InspectConduit(t.Context(), onlyCurrent, current, "selected", client); err != nil || report.ClientID != "client" || calls.Load() != 5 {
		t.Fatal("current rotation unavailable", report, err)
	}
}

func TestExecutionOriginalLifetimeStillAppliesAfterProviderReads(t *testing.T) {
	f, admin := fixture(t)
	s, err := OpenExecution(t.Context(), f.AppExecutionURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	m, err := admin.Install(t.Context(), keys(), fixtureSecret, credential(t, "client", 33), 0)
	if err != nil {
		t.Fatal(err)
	}
	client, calls := inspectorClient(t, func(r *http.Request, n int, body string) (string, error) {
		if n == 1 {
			select {
			case <-time.After(3200 * time.Millisecond):
			case <-r.Context().Done():
				return "", r.Context().Err()
			}
		}
		return body, nil
	})
	report, err := s.InspectConduit(t.Context(), keys(), Binding{m.ID, m.ClientID, m.Revision}, "selected", client)
	if !errors.Is(err, ErrInvalid) || report.ClientID != "" || calls.Load() != 5 {
		t.Fatal("provider lifetime extended original custody", report, err, calls.Load())
	}
}

func TestCustodyLoginMembershipCannotDelegateItsRole(t *testing.T) {
	for _, execution := range []bool{false, true} {
		t.Run(fmt.Sprint(execution), func(t *testing.T) {
			f, _ := fixture(t)
			base := f.AppAdminURL
			if execution {
				base = f.AppExecutionURL
			}
			u, err := url.Parse(base)
			if err != nil {
				t.Fatal(err)
			}
			password, _ := u.User.Password()
			policyRole := u.User.Username()
			alias := f.Schema + "_app_login"
			execSQL(t, f.Admin, `CREATE ROLE `+alias+` LOGIN PASSWORD '`+password+`'; GRANT `+policyRole+` TO `+alias)
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if _, err := f.Admin.ExecContext(ctx, `DROP OWNED BY `+alias+`; DROP ROLE `+alias); err != nil {
					t.Error(err)
				}
			})
			u.User = url.UserPassword(alias, password)
			var check func(context.Context) error
			var closeStore func() error
			if execution {
				store, err := OpenExecution(t.Context(), u.String())
				if err != nil {
					t.Fatal("valid login delegation refused", err)
				}
				check = store.Validate
				closeStore = store.Close
			} else {
				store, err := Open(t.Context(), u.String())
				if err != nil {
					t.Fatal("valid admin login refused", err)
				}
				check = store.Validate
				closeStore = store.Close
			}
			defer func() { _ = closeStore() }()
			execSQL(t, f.Admin, `GRANT `+policyRole+` TO `+alias+` WITH ADMIN OPTION`)
			if err := check(t.Context()); !errors.Is(err, ErrBoundary) {
				t.Fatal("role delegation option accepted", err)
			}
		})
	}
}

func TestExecutionFinalFenceDoesNotInheritRepeatableRead(t *testing.T) {
	f, _, old, b := executionFixture(t)
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	execSQL(t, f.Admin, `ALTER ROLE `+role(f.AppExecutionURL)+` SET default_transaction_isolation='repeatable read'`)
	reader, err := OpenExecution(t.Context(), f.AppExecutionURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	var mode string
	if err := reader.db.QueryRow(`SHOW default_transaction_isolation`).Scan(&mode); err != nil || mode != "repeatable read" {
		t.Fatal("fixture isolation", mode, err)
	}
	client, _ := inspectorClient(t, func(r *http.Request, n int, body string) (string, error) {
		if n == 1 {
			execSQL(t, f.Admin, `UPDATE service_app_credentials SET revision=2 WHERE id=$1`, b.AppID)
		}
		return body, nil
	})
	report, err := reader.InspectConduit(t.Context(), keys(), b, "selected", client)
	if !errors.Is(err, ErrRevision) || report.ClientID != "" {
		t.Fatal("stale snapshot escaped final fence", report, err)
	}
}
