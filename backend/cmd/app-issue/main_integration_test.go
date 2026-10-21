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
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func realIssuer(ctx context.Context, dsn string) (issuer, error)      { return appvault.Open(ctx, dsn) }
func fixtureEnvironment(t *testing.T, dsn, command string) func(string) string {
	t.Helper()
	return func(name string) string {
		if command == "prepare" && strings.Contains(name, "VAULT") {
			t.Fatal("preparation accessed keys")
		}
		if name == "OVERLAID_APP_ISSUANCE_DATABASE_URL" {
			return dsn
		}
		return environment(name)
	}
}
func invoke(t *testing.T, ctx context.Context, args []string, env func(string) string, client *http.Client) (int, appvault.Issuance, string) {
	t.Helper()
	var out, diagnostic bytes.Buffer
	code := run(ctx, args, env, &out, &diagnostic, realIssuer, client)
	var result struct {
		Issuance appvault.Issuance `json:"issuance"`
	}
	if out.Len() != 0 {
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal("invalid metadata", err)
		}
	}
	for _, forbidden := range []string{private, "fixture-access", "ciphertext", "claim_hash", "secret_fingerprint", "key_version"} {
		if strings.Contains(out.String()+diagnostic.String(), forbidden) {
			t.Fatal("private data disclosed")
		}
	}
	return code, result.Issuance, diagnostic.String()
}
func syntheticClient(t *testing.T, fail bool) (*http.Client, *atomic.Int32) {
	t.Helper()
	posts := new(atomic.Int32)
	return &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		body := ""
		if r.Method == "POST" && r.URL.String() == "https://id.twitch.tv/oauth2/token" {
			posts.Add(1)
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				return nil, err
			}
			form, err := url.ParseQuery(string(raw))
			if err != nil || form.Get("client_id") != "Client-A" || form.Get("client_secret") != private || r.GetBody != nil {
				return nil, errors.New("wrong acquisition binding")
			}
			if fail {
				return nil, errors.New(private)
			}
			body = `{"access_token":"fixture-access","token_type":"bearer","expires_in":3600}`
		} else if r.Method == "GET" && r.URL.String() == "https://id.twitch.tv/oauth2/validate" {
			body = `{"client_id":"Client-A","expires_in":3600}`
		} else {
			return nil, errors.New("unexpected fixture provider request")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}, posts
}

func TestRealNativePrepareExecuteDuplicateAndRefusal(t *testing.T) {
	f := testpostgres.NewTenantFixture(t, "")
	client, posts := syntheticClient(t, false)
	prepareEnv := fixtureEnvironment(t, f.AppAdminURL, "prepare")
	executeEnv := fixtureEnvironment(t, f.AppAdminURL, "execute")
	code, prepared, diagnostic := invoke(t, t.Context(), arguments("prepare"), prepareEnv, client)
	if code != 0 || prepared.State != "prepared" || prepared.ID == "" || posts.Load() != 0 || diagnostic != "" {
		t.Fatal(code, prepared, diagnostic)
	}
	_, duplicate, _ := invoke(t, t.Context(), arguments("prepare"), prepareEnv, client)
	if duplicate != prepared || posts.Load() != 0 {
		t.Fatal("preparation changed identity or contacted provider")
	}
	args := arguments("execute")
	args[6] = prepared.ID
	stale := append([]string{}, args...)
	stale[4] = "1"
	if code, _, _ := invoke(t, t.Context(), stale, executeEnv, client); code != 1 || posts.Load() != 0 {
		t.Fatal("stale binding attempted acquisition", code, posts.Load())
	}
	if code, _, _ := invoke(t, t.Context(), args, fixtureEnvironment(t, f.RuntimeURL, "execute"), client); code != 1 || posts.Load() != 0 {
		t.Fatal("wrong role attempted acquisition")
	}
	wrongSecret := func(name string) string {
		if name == "OVERLAID_APP_ISSUANCE_CLIENT_SECRET" {
			return "other-secret"
		}
		return executeEnv(name)
	}
	if code, _, _ := invoke(t, t.Context(), args, wrongSecret, client); code != 1 || posts.Load() != 0 {
		t.Fatal("wrong secret attempted acquisition")
	}
	missing := append([]string{}, args...)
	missing[6] = requestID
	if code, _, _ := invoke(t, t.Context(), missing, executeEnv, client); code != 1 || posts.Load() != 0 {
		t.Fatal("missing request attempted acquisition")
	}
	code, issued, diagnostic := invoke(t, t.Context(), args, executeEnv, client)
	if code != 0 || issued.State != "issued" || issued.ID != prepared.ID || issued.InstalledRevision != 1 || posts.Load() != 1 || diagnostic != "" {
		t.Fatal(code, issued, diagnostic, posts.Load())
	}
	for range 2 {
		code, again, _ := invoke(t, t.Context(), args, executeEnv, client)
		if code != 0 || again != issued || posts.Load() != 1 {
			t.Fatal("duplicate replay", code, again)
		}
	}
	var credentials, receipts int
	if err := f.Admin.QueryRow(`SELECT (SELECT count(*) FROM service_app_credentials),(SELECT count(*) FROM service_app_credential_audit)`).Scan(&credentials, &receipts); err != nil || credentials != 1 || receipts != 1 {
		t.Fatal(credentials, receipts, err)
	}
	prepNext := arguments("prepare")
	prepNext[4] = "1"
	code, next, _ := invoke(t, t.Context(), prepNext, prepareEnv, client)
	if code != 0 || next.ID == prepared.ID {
		t.Fatal(code, next)
	}
	if _, err := f.Admin.Exec(`UPDATE service_app_issuance SET created_at=statement_timestamp()-interval '6 minutes',deadline_at=statement_timestamp()-interval '1 minute' WHERE id=$1`, next.ID); err != nil {
		t.Fatal(err)
	}
	expiredArgs := arguments("execute")
	expiredArgs[4], expiredArgs[6] = "1", next.ID
	code, expired, _ := invoke(t, t.Context(), expiredArgs, executeEnv, client)
	if code != 1 || expired.State != "refused" || expired.Failure != "expired" || posts.Load() != 1 {
		t.Fatal("expired source got new lifetime", code, expired, posts.Load())
	}
}

func TestRealNativeUncertaintyRetainsOriginalMetadataWithoutReplay(t *testing.T) {
	f := testpostgres.NewTenantFixture(t, "")
	client, posts := syntheticClient(t, true)
	prepareEnv := fixtureEnvironment(t, f.AppAdminURL, "prepare")
	executeEnv := fixtureEnvironment(t, f.AppAdminURL, "execute")
	_, r, _ := invoke(t, t.Context(), arguments("prepare"), prepareEnv, client)
	args := arguments("execute")
	args[6] = r.ID
	code, uncertain, diagnostic := invoke(t, t.Context(), args, executeEnv, client)
	if code != 1 || uncertain.ID != r.ID || uncertain.State != "uncertain" || posts.Load() != 1 || !strings.Contains(diagnostic, "original") {
		t.Fatal(code, uncertain, diagnostic)
	}
	code, again, _ := invoke(t, t.Context(), args, executeEnv, client)
	if code != 1 || again != uncertain || posts.Load() != 1 {
		t.Fatal("uncertain execution replayed")
	}
	code, again, _ = invoke(t, t.Context(), arguments("prepare"), prepareEnv, client)
	if code != 1 || again != uncertain || posts.Load() != 1 {
		t.Fatal("uncertain ownership replaced")
	}
}

func TestRealNativeInterruptionStopsProviderAndRetainsUncertainty(t *testing.T) {
	f := testpostgres.NewTenantFixture(t, "")
	_, r, _ := invoke(t, t.Context(), arguments("prepare"), fixtureEnvironment(t, f.AppAdminURL, "prepare"), nil)
	entered, stopped := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = io.Copy(io.Discard, req.Body)
		close(entered)
		<-req.Context().Done()
		close(stopped)
	}))
	defer server.Close()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
		copy := req.Clone(req.Context())
		u := *req.URL
		u.Scheme, u.Host = target.Scheme, target.Host
		copy.URL = &u
		return http.DefaultTransport.RoundTrip(copy)
	})}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	args := arguments("execute")
	args[6] = r.ID
	var out, diagnostic bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, args, fixtureEnvironment(t, f.AppAdminURL, "execute"), &out, &diagnostic, realIssuer, client)
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("provider request not reached")
	}
	cancel()
	select {
	case code := <-done:
		if code != 130 || !strings.Contains(out.String(), r.ID) || !strings.Contains(out.String(), "uncertain") {
			t.Fatal(code, out.String(), diagnostic.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("interruption did not complete bookkeeping")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("actual HTTP request was not canceled")
	}
	var state string
	if err := f.Admin.QueryRow(`SELECT state FROM service_app_issuance WHERE id=$1`, r.ID).Scan(&state); err != nil || state != "uncertain" {
		t.Fatal(state, err)
	}
}
