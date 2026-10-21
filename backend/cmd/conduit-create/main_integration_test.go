package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/appvault"
	"git.subcult.tv/PatrickFanella/over-laid/internal/conduitownership"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchconduit"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type nativeFixture struct {
	testpostgres.TenantFixture
	ledgerDSN string
	app       appvault.Binding
}

func fixture(t *testing.T) nativeFixture {
	t.Helper()
	f := testpostgres.NewTenantFixture(t, "")
	name := "conduit_admin_" + strings.TrimPrefix(f.Schema, "tenant_test_")
	_, source, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(source), "..", "..", "..", "migrations", "0037_conduit_creation.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(strings.ReplaceAll(string(raw), "overlaid_conduit_admin", name)); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(f.RuntimeURL)
	password, _ := u.User.Password()
	u.User = url.UserPassword(name, password)
	if _, err = f.Admin.Exec(`ALTER ROLE ` + name + ` LOGIN PASSWORD '` + password + `'`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.Admin.Exec(`DROP TABLE IF EXISTS service_conduit_creations; DROP OWNED BY ` + name + `; DROP ROLE ` + name); err != nil {
			t.Error(err)
		}
	})
	admin, err := appvault.Open(t.Context(), f.AppAdminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close() }()
	a, err := twitchconduit.NewAppAuthorizer(twitchconduit.AppAuthorizationConfig{ClientID: "Client-A", ClientSecret: func(context.Context) (string, error) { return private, nil }, HTTPClient: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		body := `{"client_id":"Client-A","expires_in":3600}`
		if r.Method == http.MethodPost {
			body = `{"access_token":"fixture-access","token_type":"bearer","expires_in":3600}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	acquired, err := a.Acquire(t.Context())
	if err != nil || acquired.Credential == nil {
		t.Fatal(err)
	}
	k, err := tenancy.ParseKeyring(environment("OVERLAID_CONDUIT_VAULT_ACTIVE_KEY"), environment("OVERLAID_CONDUIT_VAULT_KEYS"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := admin.Install(t.Context(), k, private, *acquired.Credential, 0)
	if err != nil {
		t.Fatal(err)
	}
	return nativeFixture{f, u.String(), appvault.Binding{AppID: m.ID, ClientID: m.ClientID, Revision: m.Revision}}
}
func (f nativeFixture) environment(t *testing.T, command string) func(string) string {
	t.Helper()
	return func(name string) string {
		if command != "execute" && name != "OVERLAID_CONDUIT_DATABASE_URL" {
			t.Fatal("metadata command accessed custody or keyring", name)
		}
		switch name {
		case "OVERLAID_CONDUIT_DATABASE_URL":
			return f.ledgerDSN
		case "OVERLAID_CONDUIT_EXECUTION_DATABASE_URL":
			return f.AppExecutionURL
		}
		return environment(name)
	}
}
func (f nativeFixture) arguments(command, id string) []string {
	a := arguments(command)
	if command != "status" {
		a[2] = f.app.AppID
	}
	switch command {
	case "status":
		a[2] = id
	case "execute":
		a[len(a)-1] = id
	}
	return a
}
func invoke(t *testing.T, ctx context.Context, args []string, env func(string) string, client *http.Client) (int, conduitownership.Intent, string) {
	t.Helper()
	var out, diag bytes.Buffer
	code := run(ctx, args, env, &out, &diag, openOperator, client)
	var result struct {
		Intent conduitownership.Intent `json:"intent"`
	}
	if out.Len() != 0 {
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []string{private, "fixture-access", "ciphertext", "claim_hash", "PRIVATE-provider"} {
		if strings.Contains(out.String()+diag.String(), value) {
			t.Fatal("private data disclosed")
		}
	}
	return code, result.Intent, diag.String()
}
func syntheticClient(t *testing.T, uncertain bool) (*http.Client, *atomic.Int32) {
	t.Helper()
	posts := new(atomic.Int32)
	return &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer fixture-access" {
			t.Error("wrong private authorization")
		}
		body := `{"client_id":"Client-A","expires_in":3600}`
		if r.Method == http.MethodPost && r.URL.String() == "https://api.twitch.tv/helix/eventsub/conduits" {
			posts.Add(1)
			raw, err := io.ReadAll(r.Body)
			if err != nil || string(raw) != `{"shard_count":2}` || r.GetBody != nil || r.Header.Get("Client-Id") != "Client-A" {
				t.Error("wrong creation binding")
			}
			if uncertain {
				return nil, errors.New("PRIVATE-provider")
			}
			body = `{"data":[{"id":"owned","shard_count":2,"private":"PRIVATE-provider"}]}`
		} else if r.Method != http.MethodGet || r.URL.String() != "https://id.twitch.tv/oauth2/validate" {
			t.Error("unexpected provider request")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}, posts
}

func TestRealNativePreparationStatusExecutionAndRestart(t *testing.T) {
	f := fixture(t)
	client, posts := syntheticClient(t, false)
	var before, after string
	query := `SELECT json_build_object('credentials',(SELECT json_agg(t) FROM service_app_credentials t),'audit',(SELECT json_agg(t) FROM service_app_credential_audit t))::text`
	if err := f.Admin.QueryRow(query).Scan(&before); err != nil {
		t.Fatal(err)
	}
	code, prepared, diag := invoke(t, t.Context(), f.arguments("prepare", ""), f.environment(t, "prepare"), client)
	if code != 0 || prepared.State != "prepared" || prepared.ID == "" || posts.Load() != 0 || diag != "" {
		t.Fatal(code, prepared, diag)
	}
	code, duplicate, _ := invoke(t, t.Context(), f.arguments("prepare", ""), f.environment(t, "prepare"), client)
	if code != 0 || duplicate != prepared || posts.Load() != 0 {
		t.Fatal("original preparation changed")
	}
	code, status, _ := invoke(t, t.Context(), f.arguments("status", prepared.ID), f.environment(t, "status"), client)
	if code != 0 || status != prepared || posts.Load() != 0 {
		t.Fatal("status changed preparation")
	}
	code, created, diag := invoke(t, t.Context(), f.arguments("execute", prepared.ID), f.environment(t, "execute"), client)
	if code != 0 || created.State != "created" || created.ID != prepared.ID || created.ConduitID != "owned" || posts.Load() != 1 || diag != "" {
		t.Fatal(code, created, diag)
	}
	for range 2 {
		code, again, _ := invoke(t, t.Context(), f.arguments("execute", prepared.ID), f.environment(t, "execute"), client)
		if code != 0 || again != created || posts.Load() != 1 {
			t.Fatal("restarted execution replay")
		}
	}
	code, again, _ := invoke(t, t.Context(), f.arguments("prepare", ""), f.environment(t, "prepare"), client)
	if code != 0 || again != created || posts.Load() != 1 {
		t.Fatal("created preparation replaced original")
	}
	if err := f.Admin.QueryRow(query).Scan(&after); err != nil || before != after {
		t.Fatal("native creation mutated custody/audit", err)
	}
}

func TestRealNativeUncertaintyStaysReadOnlyAndDoesNotReplay(t *testing.T) {
	f := fixture(t)
	client, posts := syntheticClient(t, true)
	_, prepared, _ := invoke(t, t.Context(), f.arguments("prepare", ""), f.environment(t, "prepare"), client)
	code, uncertain, diag := invoke(t, t.Context(), f.arguments("execute", prepared.ID), f.environment(t, "execute"), client)
	if code != 1 || uncertain.State != "uncertain" || uncertain.ID != prepared.ID || posts.Load() != 1 || !strings.Contains(diag, "original") {
		t.Fatal(code, uncertain, diag)
	}
	code, status, diag := invoke(t, t.Context(), f.arguments("status", prepared.ID), f.environment(t, "status"), client)
	if code != 0 || status != uncertain || diag != "" || posts.Load() != 1 {
		t.Fatal(code, status, diag)
	}
	code, again, _ := invoke(t, t.Context(), f.arguments("execute", prepared.ID), f.environment(t, "execute"), client)
	if code != 1 || again != uncertain || posts.Load() != 1 {
		t.Fatal("uncertain execution replay")
	}
	code, again, _ = invoke(t, t.Context(), f.arguments("prepare", ""), f.environment(t, "prepare"), client)
	if code != 1 || again != uncertain || posts.Load() != 1 {
		t.Fatal("uncertain owner replaced")
	}
}

func TestRealNativeWrongRoleBindingKeyAndOriginalExpiry(t *testing.T) {
	f := fixture(t)
	client, posts := syntheticClient(t, false)
	_, prepared, _ := invoke(t, t.Context(), f.arguments("prepare", ""), f.environment(t, "prepare"), client)
	args := f.arguments("execute", prepared.ID)
	stale := append([]string{}, args...)
	stale[6] = "2"
	if code, _, _ := invoke(t, t.Context(), stale, f.environment(t, "execute"), client); code != 1 || posts.Load() != 0 {
		t.Fatal("stale binding reached provider")
	}
	for _, field := range []string{"OVERLAID_CONDUIT_DATABASE_URL", "OVERLAID_CONDUIT_EXECUTION_DATABASE_URL", "OVERLAID_CONDUIT_VAULT_KEYS", "OVERLAID_CONDUIT_VAULT_ACTIVE_KEY"} {
		env := func(name string) string {
			if name == field {
				if strings.Contains(name, "DATABASE") {
					return f.RuntimeURL
				}
				if strings.HasSuffix(name, "KEYS") {
					return "v2:" + strings.TrimPrefix(environment(name), "v1:")
				}
				return "v2"
			}
			if strings.HasPrefix(field, "OVERLAID_CONDUIT_VAULT") && strings.HasPrefix(name, "OVERLAID_CONDUIT_VAULT") {
				if strings.HasSuffix(name, "KEYS") {
					return "v2:" + strings.TrimPrefix(environment(name), "v1:")
				}
				return "v2"
			}
			return f.environment(t, "execute")(name)
		}
		if code, _, _ := invoke(t, t.Context(), args, env, client); code != 1 || posts.Load() != 0 {
			t.Fatal("wrong custody selection reached provider", field, code)
		}
	}
	if code, _, _ := invoke(t, t.Context(), f.arguments("status", requestID), f.environment(t, "status"), client); code != 1 || posts.Load() != 0 {
		t.Fatal("missing status attempted work")
	}
	if _, err := f.Admin.Exec(`UPDATE service_conduit_creations SET created_at=statement_timestamp()-interval '6 minutes',deadline_at=statement_timestamp()-interval '1 minute' WHERE id=$1`, prepared.ID); err != nil {
		t.Fatal(err)
	}
	code, expired, _ := invoke(t, t.Context(), args, f.environment(t, "execute"), client)
	if code != 1 || expired.State != "refused" || expired.Failure != "expired" || expired.ID != prepared.ID || posts.Load() != 0 {
		t.Fatal("original expiry got new lifetime", code, expired)
	}
}

func TestRealNativeHTTPCancellationRetainsOriginalStatus(t *testing.T) {
	f := fixture(t)
	_, prepared, _ := invoke(t, t.Context(), f.arguments("prepare", ""), f.environment(t, "prepare"), nil)
	entered, stopped := make(chan struct{}), make(chan struct{})
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/validate" {
			_, _ = io.WriteString(w, `{"client_id":"Client-A","expires_in":3600}`)
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
	type answer struct {
		code int
		r    conduitownership.Intent
	}
	done := make(chan answer, 1)
	go func() {
		code, r, _ := invoke(t, ctx, f.arguments("execute", prepared.ID), f.environment(t, "execute"), client)
		done <- answer{code, r}
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("no provider request")
	}
	cancel()
	select {
	case a := <-done:
		if a.code != 130 || a.r.State != "uncertain" || a.r.ID != prepared.ID {
			t.Fatal(a)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("cancellation did not settle")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("HTTP request still active")
	}
	if code, status, _ := invoke(t, t.Context(), f.arguments("status", prepared.ID), f.environment(t, "status"), client); code != 0 || status.State != "uncertain" || posts.Load() != 1 {
		t.Fatal(code, status)
	}
}
