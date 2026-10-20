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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/appvault"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchconduit"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func realInspection(ctx context.Context, dsn string) (inspection, error) {
	return appvault.OpenExecution(ctx, dsn)
}

func seedCustody(t *testing.T) (testpostgres.TenantFixture, *appvault.Store, appvault.Metadata) {
	t.Helper()
	f := testpostgres.NewTenantFixture(t, "")
	s, err := appvault.Open(t.Context(), f.AppAdminURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost && r.URL.String() == "https://id.twitch.tv/oauth2/token" {
			return response(`{"access_token":"fixture-access","expires_in":3600,"token_type":"bearer"}`), nil
		}
		if r.Method == http.MethodGet && r.URL.String() == "https://id.twitch.tv/oauth2/validate" {
			return response(`{"client_id":"Client-A","expires_in":3600}`), nil
		}
		return nil, errors.New("unexpected fixture acquisition")
	})}
	a, err := twitchconduit.NewAppAuthorizer(twitchconduit.AppAuthorizationConfig{ClientID: "Client-A", ClientSecret: func(context.Context) (string, error) { return "fixture-secret", nil }, HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	acquired, err := a.Acquire(t.Context())
	if err != nil || acquired.Outcome != twitchconduit.AcquisitionIssued {
		t.Fatal(acquired.Outcome, err)
	}
	keys, err := tenancy.ParseKeyring("v1", environment("OVERLAID_APP_EXECUTION_VAULT_KEYS"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.Install(t.Context(), keys, "fixture-secret", *acquired.Credential, 0)
	if err != nil {
		t.Fatal(err)
	}
	return f, s, m
}
func fixtureEnvironment(dsn string) func(string) string {
	return func(name string) string {
		if name == "OVERLAID_APP_EXECUTION_DATABASE_URL" {
			return dsn
		}
		return environment(name)
	}
}
func fixtureArguments(id string) []string {
	args := arguments()
	args[1] = id
	args[5] = "1"
	return args
}

func TestRealCustodiedCLIInspectionAndRefusals(t *testing.T) {
	f, s, m := seedCustody(t)
	calls := new(atomic.Int32)
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer fixture-access" {
			return nil, errors.New("unexpected fixture request")
		}
		switch r.URL.Host + r.URL.Path {
		case "id.twitch.tv/oauth2/validate":
			return response(`{"client_id":"Client-A","expires_in":3600}`), nil
		case "api.twitch.tv/helix/eventsub/conduits":
			return response(`{"data":[{"id":"conduit-A","shard_count":1}],"pagination":{}}`), nil
		case "api.twitch.tv/helix/eventsub/conduits/shards":
			if r.URL.Query().Get("conduit_id") != "conduit-A" {
				return nil, errors.New("wrong conduit")
			}
			return response(`{"data":[{"id":"0","status":"enabled","transport":{"method":"websocket","session_id":"PRIVATE-SESSION"}}],"pagination":{}}`), nil
		}
		return nil, errors.New("unexpected endpoint")
	})}
	snapshot := func() string {
		t.Helper()
		var result string
		if err := f.Admin.QueryRow(`SELECT row_to_json(c)::text || (SELECT count(*)::text FROM service_app_credential_audit WHERE app_id=$1) FROM service_app_credentials c WHERE id=$1`, m.ID).Scan(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := snapshot()
	var out, diagnostic bytes.Buffer
	code := run(t.Context(), fixtureArguments(m.ID), fixtureEnvironment(f.AppExecutionURL), &out, &diagnostic, realInspection, client)
	var result struct {
		Binding     appvault.Binding          `json:"binding"`
		Observation twitchconduit.Observation `json:"observation"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || code != 0 || diagnostic.Len() != 0 || result.Binding.AppID != m.ID || result.Binding.Revision != 1 || result.Observation.ShardCount != 1 || calls.Load() != 5 || snapshot() != before {
		t.Fatal(code, out.String(), diagnostic.String(), calls.Load(), err)
	}
	for _, forbidden := range []string{"fixture-access", "fixture-secret", "PRIVATE-SESSION", "ciphertext", "keyVersion", f.AppExecutionURL} {
		if strings.Contains(out.String()+diagnostic.String(), forbidden) {
			t.Fatal("private field disclosed")
		}
	}
	for _, name := range []string{"admin", "runtime", "stale", "missing", "client", "key"} {
		t.Run(name, func(t *testing.T) {
			args := fixtureArguments(m.ID)
			env := fixtureEnvironment(f.AppExecutionURL)
			switch name {
			case "admin":
				env = fixtureEnvironment(f.AppAdminURL)
			case "runtime":
				env = fixtureEnvironment(f.RuntimeURL)
			case "stale":
				args[5] = "2"
			case "missing":
				args[1] = appID
			case "client":
				args[3] = "Other-Client"
			case "key":
				env = func(name string) string {
					if name == "OVERLAID_APP_EXECUTION_DATABASE_URL" {
						return f.AppExecutionURL
					}
					if name == "OVERLAID_APP_EXECUTION_VAULT_ACTIVE_KEY" {
						return "v2"
					}
					return strings.Replace(environment(name), "v1:", "v2:", 1)
				}
			}
			calls.Store(0)
			out.Reset()
			diagnostic.Reset()
			if code := run(t.Context(), args, env, &out, &diagnostic, realInspection, client); code != 1 || out.Len() != 0 || calls.Load() != 0 || strings.Contains(diagnostic.String(), f.AppExecutionURL) || snapshot() != before {
				t.Fatal(code, out.String(), diagnostic.String(), calls.Load())
			}
		})
	}
	if _, err := s.Revoke(t.Context(), m.ID, 1); err != nil {
		t.Fatal(err)
	}
	calls.Store(0)
	out.Reset()
	diagnostic.Reset()
	if code := run(t.Context(), fixtureArguments(m.ID), fixtureEnvironment(f.AppExecutionURL), &out, &diagnostic, realInspection, client); code != 1 || out.Len() != 0 || calls.Load() != 0 {
		t.Fatal("revoked", code, calls.Load())
	}
}

func TestRealCLIInterruptedCustodyWait(t *testing.T) {
	f, _, m := seedCustody(t)
	tx, err := f.Admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended('service_app_custody:'||current_database()||':'||current_schema(),0))`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var out, diagnostic bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, fixtureArguments(m.ID), fixtureEnvironment(f.AppExecutionURL), &out, &diagnostic, realInspection, &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) { return nil, errors.New("provider should not be reached") })})
	}()
	dsn, err := url.Parse(f.AppExecutionURL)
	if err != nil {
		t.Fatal(err)
	}
	waiting := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err = f.Admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE usename=$1 AND wait_event='advisory')`, dsn.User.Username()).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("no actual custody wait")
	}
	cancel()
	select {
	case code := <-done:
		if code != 130 || out.Len() != 0 || strings.Contains(diagnostic.String(), f.AppExecutionURL) {
			t.Fatal(code, out.String(), diagnostic.String())
		}
	case <-time.After(time.Second):
		t.Fatal("SQL cancellation did not stop inspection")
	}
}

func TestRealCLIInterruptedProviderRead(t *testing.T) {
	f, _, m := seedCustody(t)
	entered, stopped := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(stopped) }))
	defer server.Close()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		copy := r.Clone(r.Context())
		u := *r.URL
		u.Scheme, u.Host = target.Scheme, target.Host
		copy.URL = &u
		return http.DefaultTransport.RoundTrip(copy)
	})}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var out, diagnostic bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, fixtureArguments(m.ID), fixtureEnvironment(f.AppExecutionURL), &out, &diagnostic, realInspection, client)
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("provider read not reached")
	}
	cancel()
	select {
	case code := <-done:
		if code != 130 || out.Len() != 0 {
			t.Fatal(code, out.String(), diagnostic.String())
		}
	case <-time.After(time.Second):
		t.Fatal("HTTP cancellation did not stop inspection")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("actual HTTP request not canceled")
	}
}
