package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchconduit"
)

func TestRealNativeSubscriptionsPrivacyAndRefusal(t *testing.T) {
	f, admin, m := seedCustody(t)
	args := append(fixtureArguments(m.ID), "--subscriptions")
	var calls atomic.Int32
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer fixture-access" {
			return nil, errors.New("PRIVATE request")
		}
		if r.URL.Host == "api.twitch.tv" && r.Header.Get("Client-Id") != "Client-A" {
			t.Error("wrong client")
		}
		switch r.URL.String() {
		case "https://id.twitch.tv/oauth2/validate":
			return response(`{"client_id":"Client-A","expires_in":3600}`), nil
		case "https://api.twitch.tv/helix/eventsub/conduits":
			return response(`{"data":[{"id":"conduit-A","shard_count":1}]}`), nil
		case "https://api.twitch.tv/helix/eventsub/subscriptions?conduit_id=conduit-A":
			return response(`{"data":[{"id":"sub","type":"stream.offline","version":"1","status":"enabled","condition":{"broadcaster_user_id":"channel"},"transport":{"method":"conduit","conduit_id":"conduit-A"},"created_at":"2026-10-21T00:00:00Z","cost":0,"private":"PRIVATE"}],"total":1,"pagination":{}}`), nil
		}
		return nil, errors.New("PRIVATE unexpected endpoint")
	})}
	snapshot := func() string {
		t.Helper()
		var s string
		if e := f.Admin.QueryRow(`SELECT json_build_object('credentials',(SELECT json_agg(t) FROM service_app_credentials t),'audit',(SELECT json_agg(t) FROM service_app_credential_audit t))::text`).Scan(&s); e != nil {
			t.Fatal(e)
		}
		return s
	}
	before := snapshot()
	var out, diag bytes.Buffer
	code := run(t.Context(), args, fixtureEnvironment(f.AppExecutionURL), &out, &diag, realInspection, client)
	var decoded struct {
		Subscriptions twitchconduit.SubscriptionObservation `json:"subscriptions"`
	}
	if e := json.Unmarshal(out.Bytes(), &decoded); e != nil || code != 0 || diag.Len() != 0 || !decoded.Subscriptions.ValidFor("Client-A", "conduit-A") || calls.Load() != 6 {
		t.Fatal(code, out.String(), diag.String(), e)
	}
	if snapshot() != before {
		t.Fatal("native observation mutated custody/receipts/ownership")
	}
	for _, private := range []string{"PRIVATE", "fixture-access", "fixture-secret", "ciphertext", f.AppExecutionURL} {
		if strings.Contains(out.String()+diag.String(), private) {
			t.Fatal("private output")
		}
	}
	for _, dsn := range []string{f.RuntimeURL, f.AppAdminURL} {
		out.Reset()
		diag.Reset()
		if code := run(t.Context(), args, fixtureEnvironment(dsn), &out, &diag, realInspection, client); code != 1 || out.Len() != 0 || calls.Load() != 6 {
			t.Fatal("foreign role used subscription reader", code)
		}
	}
	if _, e := admin.Revoke(t.Context(), m.ID, m.Revision); e != nil {
		t.Fatal(e)
	}
	out.Reset()
	diag.Reset()
	if code := run(t.Context(), args, fixtureEnvironment(f.AppExecutionURL), &out, &diag, realInspection, client); code != 1 || out.Len() != 0 || calls.Load() != 6 {
		t.Fatal("revoked reader reached provider", code)
	}
}
func TestRealNativeSubscriptionHTTPCancellation(t *testing.T) {
	f, _, m := seedCustody(t)
	args := append(fixtureArguments(m.ID), "--subscriptions")
	entered, stopped := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/validate":
			_, _ = w.Write([]byte(`{"client_id":"Client-A","expires_in":3600}`))
		case "/helix/eventsub/conduits":
			_, _ = w.Write([]byte(`{"data":[{"id":"conduit-A","shard_count":1}]}`))
		case "/helix/eventsub/subscriptions":
			close(entered)
			<-r.Context().Done()
			close(stopped)
		default:
			t.Error("unexpected HTTP path")
		}
	}))
	defer server.Close()
	local, _ := url.Parse(server.URL)
	tr := http.DefaultTransport.(*http.Transport).Clone()
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		copy := r.Clone(r.Context())
		copy.URL.Scheme, copy.URL.Host = local.Scheme, local.Host
		return tr.RoundTrip(copy)
	})}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	done := make(chan int, 1)
	var out, diag bytes.Buffer
	go func() {
		done <- run(ctx, args, fixtureEnvironment(f.AppExecutionURL), &out, &diag, realInspection, client)
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("subscription request missing")
	}
	cancel()
	select {
	case code := <-done:
		if code != 130 || out.Len() != 0 {
			t.Fatal(code, out.String(), diag.String())
		}
	case <-time.After(4 * time.Second):
		t.Fatal("native cancellation did not settle")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("private HTTP request remained active")
	}
}
