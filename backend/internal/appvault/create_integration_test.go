package appvault

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/conduitownership"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
)

type creationLedger struct {
	*conduitownership.Store
	dsn, role string
}

func creationFixture(t *testing.T) (testpostgres.TenantFixture, *Store, *ExecutionStore, Binding, creationLedger) {
	t.Helper()
	f, admin, s, b := executionFixture(t)
	name := "conduit_admin_" + strings.TrimPrefix(f.Schema, "tenant_test_")
	_, source, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(source), "..", "..", "..", "migrations", "0037_conduit_creation.sql"))
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, f.Admin, strings.ReplaceAll(string(raw), "overlaid_conduit_admin", name))
	u, _ := url.Parse(f.RuntimeURL)
	password, _ := u.User.Password()
	u.User = url.UserPassword(name, password)
	execSQL(t, f.Admin, `ALTER ROLE `+name+` LOGIN PASSWORD '`+password+`'`)
	t.Cleanup(func() {
		if _, err := f.Admin.Exec(`DROP TABLE IF EXISTS service_conduit_creations; DROP OWNED BY ` + name + `; DROP ROLE ` + name); err != nil {
			t.Error(err)
		}
	})
	ledger, err := conduitownership.Open(t.Context(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := ledger.Close(); err != nil {
			t.Error(err)
		}
	})
	// The new ledger does not broaden either pre-existing custody role.
	if err = admin.Validate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = s.Validate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return f, admin, s, b, creationLedger{ledger, u.String(), name}
}
func prepareCreation(t *testing.T, l creationLedger, b Binding) conduitownership.Intent {
	t.Helper()
	r, err := l.Prepare(t.Context(), conduitownership.Binding{AppID: b.AppID, ClientID: b.ClientID, Revision: b.Revision}, 2)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func creationClient(t *testing.T, modify func(string, *http.Request) (string, int, error)) (*http.Client, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	posts, reads := new(atomic.Int32), new(atomic.Int32)
	return &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		stage, body := "before", `{"client_id":"client","expires_in":3600}`
		if r.Header.Get("Authorization") != "Bearer fixture-access" || r.URL.RawQuery != "" {
			t.Error("invalid private request binding")
		}
		switch r.Method + " " + r.URL.String() {
		case "GET https://id.twitch.tv/oauth2/validate":
			if reads.Add(1) > 1 {
				stage = "after"
			}
		case "POST https://api.twitch.tv/helix/eventsub/conduits":
			stage = "create"
			posts.Add(1)
			raw, err := io.ReadAll(r.Body)
			if err != nil || string(raw) != `{"shard_count":2}` || r.GetBody != nil || r.Header.Get("Client-Id") != "client" {
				t.Error("invalid creation body/header")
			}
			body = `{"data":[{"id":"owned","shard_count":2,"private":"PRIVATE"}]}`
		default:
			t.Error("unexpected provider operation")
		}
		status := 200
		if modify != nil {
			changed, code, err := modify(stage, r)
			if err != nil {
				return nil, err
			}
			if changed != "" {
				body = changed
			}
			if code != 0 {
				status = code
			}
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}, posts, reads
}

func TestCreationCustodyCompositionAndNoReplay(t *testing.T) {
	f, _, s, b, l := creationFixture(t)
	r := prepareCreation(t, l, b)
	var before, after string
	query := `SELECT json_build_object('credentials',(SELECT json_agg(t) FROM service_app_credentials t),'audit',(SELECT json_agg(t) FROM service_app_credential_audit t))::text`
	if err := f.Admin.QueryRow(query).Scan(&before); err != nil {
		t.Fatal(err)
	}
	client, posts, reads := creationClient(t, func(_ string, _ *http.Request) (string, int, error) {
		var state string
		if err := f.Admin.QueryRow(`SELECT state FROM service_conduit_creations WHERE id=$1`, r.ID).Scan(&state); err != nil || state != "started" {
			t.Error("provider access before committed claim", err)
		}
		return "", 0, nil
	})
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			result, err := s.CreateConduit(t.Context(), keys(), b, r.ID, 2, l.Store, client)
			if err != nil || result.State != "created" || result.ConduitID != "owned" || result.ID != r.ID {
				t.Error(result, err)
			}
		})
	}
	wg.Wait()
	if posts.Load() != 1 || reads.Load() != 2 {
		t.Fatal("provider replay", posts.Load(), reads.Load())
	}
	restarted, err := OpenExecution(t.Context(), f.AppExecutionURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restarted.Close() }()
	ledger, err := conduitownership.Open(t.Context(), l.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ledger.Close() }()
	result, err := restarted.CreateConduit(t.Context(), keys(), b, r.ID, 2, ledger, client)
	if err != nil || result.State != "created" || posts.Load() != 1 || reads.Load() != 2 {
		t.Fatal(result, err)
	}
	raw, _ := json.Marshal(result)
	if strings.Contains(string(raw), "PRIVATE") || strings.Contains(string(raw), fixtureSecret) || strings.Contains(string(raw), "fixture-access") {
		t.Fatal("private output")
	}
	if err = f.Admin.QueryRow(query).Scan(&after); err != nil || before != after {
		t.Fatal("custody or audit mutated", err)
	}
}

func TestCreationRefusesBeforeClaimOrProvider(t *testing.T) {
	f, _, s, b, l := creationFixture(t)
	r := prepareCreation(t, l, b)
	client, posts, reads := creationClient(t, nil)
	wrong := b
	wrong.Revision++
	for _, test := range []struct {
		id     string
		b      Binding
		shards int
		k      tenancy.Keyring
	}{
		{r.ID, wrong, 2, keys()}, {r.ID, b, 3, keys()}, {"22222222-2222-4222-8222-222222222222", b, 2, keys()}, {r.ID, b, 2, tenancy.Keyring{}},
	} {
		if _, err := s.CreateConduit(t.Context(), test.k, test.b, test.id, test.shards, l.Store, client); err == nil {
			t.Fatal("invalid selection accepted")
		}
	}
	var expiry time.Time
	if err := f.Admin.QueryRow(`SELECT expires_at FROM service_app_credentials WHERE id=$1`, b.AppID).Scan(&expiry); err != nil {
		t.Fatal(err)
	}
	execSQL(t, f.Admin, `UPDATE service_app_credentials SET expires_at=clock_timestamp()+interval '20 seconds' WHERE id=$1`, b.AppID)
	if _, err := s.CreateConduit(t.Context(), keys(), b, r.ID, 2, l.Store, client); err == nil {
		t.Fatal("expired credential accepted")
	}
	execSQL(t, f.Admin, `UPDATE service_app_credentials SET expires_at=$2 WHERE id=$1`, b.AppID, expiry)
	execSQL(t, f.Admin, `GRANT SELECT(secret_ciphertext) ON service_app_credentials TO `+role(f.AppExecutionURL))
	if _, err := s.CreateConduit(t.Context(), keys(), b, r.ID, 2, l.Store, client); !errors.Is(err, ErrBoundary) {
		t.Fatal(err)
	}
	execSQL(t, f.Admin, `REVOKE SELECT(secret_ciphertext) ON service_app_credentials FROM `+role(f.AppExecutionURL))
	execSQL(t, f.Admin, `GRANT SELECT ON service_app_credentials TO `+l.role)
	if _, err := s.CreateConduit(t.Context(), keys(), b, r.ID, 2, l.Store, client); !errors.Is(err, conduitownership.ErrBoundary) {
		t.Fatal(err)
	}
	execSQL(t, f.Admin, `REVOKE SELECT ON service_app_credentials FROM `+l.role)
	prior, err := l.Lookup(t.Context(), r.ID)
	if err != nil || prior != r || posts.Load() != 0 || reads.Load() != 0 {
		t.Fatal(prior, err)
	}
}

func TestCreationWrongSchemaLedgerRefused(t *testing.T) {
	_, _, s, b, l := creationFixture(t)
	r := prepareCreation(t, l, b)
	_, _, _, _, other := creationFixture(t)
	client, posts, reads := creationClient(t, nil)
	if _, err := s.CreateConduit(t.Context(), keys(), b, r.ID, 2, other.Store, client); !errors.Is(err, ErrBoundary) || posts.Load() != 0 || reads.Load() != 0 {
		t.Fatal(err)
	}
	prior, err := l.Lookup(t.Context(), r.ID)
	if err != nil || prior != r {
		t.Fatal(prior, err)
	}
}

func TestCreationPostClaimDriftAndProviderAmbiguity(t *testing.T) {
	for _, kind := range []string{"revision", "ciphertext", "role", "provider", "wrong_client", "lost_claim", "expired_intent"} {
		t.Run(kind, func(t *testing.T) {
			f, _, s, b, l := creationFixture(t)
			r := prepareCreation(t, l, b)
			client, posts, _ := creationClient(t, func(stage string, _ *http.Request) (string, int, error) {
				if stage != "create" {
					return "", 0, nil
				}
				switch kind {
				case "revision":
					execSQL(t, f.Admin, `UPDATE service_app_credentials SET revision=revision+1 WHERE id=$1`, b.AppID)
				case "ciphertext":
					execSQL(t, f.Admin, `UPDATE service_app_credentials SET token_ciphertext=decode(repeat('ab',40),'hex') WHERE id=$1`, b.AppID)
				case "role":
					execSQL(t, f.Admin, `GRANT SELECT(secret_ciphertext) ON service_app_credentials TO `+role(f.AppExecutionURL))
				case "provider":
					return "", 0, errors.New("PRIVATE-transport")
				case "wrong_client":
					return `{"data":[{"id":"PRIVATE/url","shard_count":2}]}`, 200, nil
				case "lost_claim":
					execSQL(t, f.Admin, `UPDATE service_conduit_creations SET claim_hash=repeat('0',64) WHERE id=$1`, r.ID)
				case "expired_intent":
					execSQL(t, f.Admin, `UPDATE service_conduit_creations SET created_at=statement_timestamp()-interval '6 minutes',deadline_at=statement_timestamp()-interval '1 minute' WHERE id=$1`, r.ID)
				}
				return "", 0, nil
			})
			result, err := s.CreateConduit(t.Context(), keys(), b, r.ID, 2, l.Store, client)
			if !errors.Is(err, conduitownership.ErrUncertain) || result.ConduitID != "" || posts.Load() != 1 {
				t.Fatal(result, err)
			}
			if kind == "role" {
				execSQL(t, f.Admin, `REVOKE SELECT(secret_ciphertext) ON service_app_credentials FROM `+role(f.AppExecutionURL))
			}
			prior, lookupErr := l.Lookup(t.Context(), r.ID)
			if lookupErr != nil || (prior.State != "uncertain" && prior.State != "started") {
				t.Fatal(prior, lookupErr)
			}
			if _, err = s.CreateConduit(t.Context(), keys(), b, r.ID, 2, l.Store, client); !errors.Is(err, conduitownership.ErrUncertain) || posts.Load() != 1 {
				t.Fatal("attempt replayed", err)
			}
			if strings.Contains(fmt.Sprint(err), "PRIVATE") {
				t.Fatal("private error")
			}
		})
	}
}

func TestCreationCustodyLockExpiryAndRevocation(t *testing.T) {
	for _, kind := range []string{"expiry", "revision"} {
		t.Run(kind, func(t *testing.T) {
			f, _, s, b, l := creationFixture(t)
			r := prepareCreation(t, l, b)
			u, _ := url.Parse(f.AppExecutionURL)
			q := u.Query()
			q.Set("default_transaction_isolation", "repeatable read")
			u.RawQuery = q.Encode()
			repeatable, err := OpenExecution(t.Context(), u.String())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = repeatable.Close() }()
			owner, err := f.Admin.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = owner.Rollback() }()
			if _, err = owner.Exec(`SELECT pg_advisory_xact_lock(hashtextextended('service_app_custody:'||current_database()||':'||current_schema(),0))`); err != nil {
				t.Fatal(err)
			}
			client, posts, reads := creationClient(t, nil)
			done := make(chan error, 1)
			go func() {
				_, err := repeatable.CreateConduit(t.Context(), keys(), b, r.ID, 2, l.Store, client)
				done <- err
			}()
			waitAppLock(t, f, f.AppExecutionURL)
			if kind == "revision" {
				_, err = owner.Exec(`UPDATE service_app_credentials SET revision=revision+1 WHERE id=$1`, b.AppID)
			} else {
				_, err = owner.Exec(`UPDATE service_conduit_creations SET created_at=statement_timestamp()-interval '6 minutes',deadline_at=statement_timestamp()-interval '1 minute' WHERE id=$1`, r.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = owner.Commit(); err != nil {
				t.Fatal(err)
			}
			if err = <-done; err == nil || posts.Load() != 0 || reads.Load() != 0 {
				t.Fatal("stale lock admitted provider", err)
			}
			_ = s
		})
	}
}

func TestCreationActualHTTPCancellationRetainsUncertainty(t *testing.T) {
	_, _, s, b, l := creationFixture(t)
	r := prepareCreation(t, l, b)
	entered, stopped := make(chan struct{}), make(chan struct{})
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/validate" {
			_, _ = io.WriteString(w, `{"client_id":"client","expires_in":3600}`)
			return
		}
		posts.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		<-r.Context().Done()
		close(stopped)
	}))
	defer server.Close()
	local, _ := url.Parse(server.URL)
	tr := http.DefaultTransport.(*http.Transport).Clone()
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		copyRequest := r.Clone(r.Context())
		copyRequest.URL.Scheme, copyRequest.URL.Host = local.Scheme, local.Host
		return tr.RoundTrip(copyRequest)
	})}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.CreateConduit(ctx, keys(), b, r.ID, 2, l.Store, client); done <- err }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("no provider request")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || !errors.Is(err, conduitownership.ErrUncertain) {
			t.Fatal(err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("cancellation did not settle")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("HTTP request still active")
	}
	prior, err := l.Lookup(t.Context(), r.ID)
	if err != nil || prior.State != "uncertain" {
		t.Fatal(prior, err)
	}
	if _, err = s.CreateConduit(t.Context(), keys(), b, r.ID, 2, l.Store, client); !errors.Is(err, conduitownership.ErrUncertain) || posts.Load() != 1 {
		t.Fatal(err)
	}
}

func TestCreationDriftWhileClaimWaitsStopsBeforeProvider(t *testing.T) {
	f, _, s, b, l := creationFixture(t)
	r := prepareCreation(t, l, b)
	owner, err := f.Admin.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Rollback() }()
	var id string
	if err = owner.QueryRow(`SELECT id FROM service_conduit_creations WHERE id=$1 FOR UPDATE`, r.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	client, posts, reads := creationClient(t, nil)
	done := make(chan error, 1)
	go func() { _, err := s.CreateConduit(t.Context(), keys(), b, r.ID, 2, l.Store, client); done <- err }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		var waiting bool
		if err = f.Admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE usename=$1 AND wait_event_type='Lock' AND query LIKE '%service_conduit_creations%FOR UPDATE%')`, l.role).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("claim did not reach original row lock")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err = owner.Exec(`UPDATE service_app_credentials SET revision=revision+1 WHERE id=$1`, b.AppID); err != nil {
		t.Fatal(err)
	}
	if err = owner.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-done; !errors.Is(err, conduitownership.ErrUncertain) || posts.Load() != 0 || reads.Load() != 0 {
		t.Fatal("post-claim drift reached provider", err)
	}
	prior, err := l.Lookup(t.Context(), r.ID)
	if err != nil || prior.State != "uncertain" {
		t.Fatal(prior, err)
	}
}

func TestCreationSerializesLibraryRevocationThroughCompletion(t *testing.T) {
	f, admin, s, b, l := creationFixture(t)
	r := prepareCreation(t, l, b)
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	client, posts, _ := creationClient(t, func(stage string, request *http.Request) (string, int, error) {
		if stage == "create" {
			close(entered)
			select {
			case <-release:
			case <-request.Context().Done():
				return "", 0, request.Context().Err()
			}
		}
		return "", 0, nil
	})
	createdDone, revokedDone := make(chan error, 1), make(chan error, 1)
	go func() {
		result, err := s.CreateConduit(t.Context(), keys(), b, r.ID, 2, l.Store, client)
		if err == nil && result.State != "created" {
			err = errors.New("creation result missing")
		}
		createdDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("creation did not reach provider")
	}
	go func() { _, err := admin.Revoke(t.Context(), b.AppID, b.Revision); revokedDone <- err }()
	waitAppLock(t, f, f.AppAdminURL)
	select {
	case err := <-revokedDone:
		t.Fatal("revoke did not serialize", err)
	default:
	}
	close(release)
	if err := <-createdDone; err != nil {
		t.Fatal(err)
	}
	if err := <-revokedDone; err != nil {
		t.Fatal(err)
	}
	prior, err := l.Lookup(t.Context(), r.ID)
	if err != nil || prior.State != "created" || posts.Load() != 1 {
		t.Fatal(prior, err)
	}
	// A later call reads the retained result without obtaining or using the
	// revoked credential. Creation success metadata is not new effect permission.
	if result, err := s.CreateConduit(t.Context(), keys(), b, r.ID, 2, l.Store, client); err != nil || result.State != "created" || posts.Load() != 1 {
		t.Fatal(result, err)
	}
}

func TestCreationOriginalDatabaseDeadlineBoundsProvider(t *testing.T) {
	f, _, s, b, l := creationFixture(t)
	r := prepareCreation(t, l, b)
	execSQL(t, f.Admin, `UPDATE service_conduit_creations SET created_at=statement_timestamp()-interval '5 minutes'+interval '1 second',deadline_at=statement_timestamp()+interval '1 second' WHERE id=$1`, r.ID)
	client, posts, reads := creationClient(t, func(stage string, r *http.Request) (string, int, error) {
		if stage == "before" {
			<-r.Context().Done()
			return "", 0, r.Context().Err()
		}
		return "", 0, nil
	})
	result, err := s.CreateConduit(t.Context(), keys(), b, r.ID, 2, l.Store, client)
	if !errors.Is(err, conduitownership.ErrUncertain) || !errors.Is(err, context.DeadlineExceeded) || result.State != "uncertain" || posts.Load() != 0 || reads.Load() != 1 {
		t.Fatal(result, err, posts.Load(), reads.Load())
	}
}

func TestCreationWrongDatabaseLedgerRefused(t *testing.T) {
	f, _, s, b, l := creationFixture(t)
	r := prepareCreation(t, l, b)
	database := "conduit_db_" + strings.TrimPrefix(f.Schema, "tenant_test_")
	execSQL(t, f.Admin, `CREATE DATABASE `+database)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := f.Admin.ExecContext(ctx, `DROP DATABASE `+database); err != nil {
			t.Error(err)
		}
	})
	u, err := url.Parse(os.Getenv("OVERLAID_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + database
	q := u.Query()
	q.Set("search_path", f.Schema+",public")
	u.RawQuery = q.Encode()
	owner, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := owner.Close(); err != nil {
			t.Error(err)
		}
	})
	if err = testpostgres.EnsurePGCrypto(t.Context(), owner); err != nil {
		t.Fatal(err)
	}
	execSQL(t, owner, `CREATE SCHEMA `+f.Schema+`; CREATE TABLE `+f.Schema+`.overlaid_migrations(name text PRIMARY KEY)`)
	_, source, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(source), "..", "..", "..", "migrations", "0037_conduit_creation.sql"))
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, owner, strings.ReplaceAll(string(raw), "overlaid_conduit_admin", l.role))
	otherURL, _ := url.Parse(l.dsn)
	otherURL.Path = "/" + database
	other, err := conduitownership.Open(t.Context(), otherURL.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := other.Close(); err != nil {
			t.Error(err)
		}
	})
	client, posts, reads := creationClient(t, nil)
	if _, err = s.CreateConduit(t.Context(), keys(), b, r.ID, 2, other, client); !errors.Is(err, ErrBoundary) || posts.Load() != 0 || reads.Load() != 0 {
		t.Fatal(err)
	}
	prior, err := l.Lookup(t.Context(), r.ID)
	if err != nil || prior != r {
		t.Fatal(prior, err)
	}
}
