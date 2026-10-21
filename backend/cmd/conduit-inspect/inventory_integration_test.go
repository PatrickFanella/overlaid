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

func TestRealNativeInventoryEmptyCompleteAndRefusals(t *testing.T) {
	f, admin, m := seedCustody(t)
	a := fixtureArguments(m.ID)
	a = append(a[:6:6], "--inventory")
	var snapshot string
	query := `SELECT row_to_json(c)::text || (SELECT count(*)::text FROM service_app_credential_audit WHERE app_id=$1) FROM service_app_credentials c WHERE id=$1`
	if err := f.Admin.QueryRow(query, m.ID).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"data":[]}`, `{"data":[{"id":"unassigned","shard_count":2,"private":"PRIVATE-body"}]}`, `{"data":[{"id":"z","shard_count":2},{"id":"a","shard_count":1}]}`} {
		var calls atomic.Int32
		client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
			calls.Add(1)
			if r.Method != http.MethodGet || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer fixture-access" {
				return nil, errors.New("PRIVATE-request")
			}
			switch r.URL.String() {
			case "https://id.twitch.tv/oauth2/validate":
				return response(`{"client_id":"Client-A","expires_in":3600}`), nil
			case "https://api.twitch.tv/helix/eventsub/conduits":
				if r.Header.Get("Client-Id") != "Client-A" {
					t.Error("wrong client")
				}
				return response(body), nil
			}
			return nil, errors.New("PRIVATE-unexpected endpoint")
		})}
		var out, diag bytes.Buffer
		code := run(t.Context(), a, fixtureEnvironment(f.AppExecutionURL), &out, &diag, realInspection, client)
		var decoded struct {
			Inventory twitchconduit.InventoryObservation `json:"inventory"`
		}
		if err := json.Unmarshal(out.Bytes(), &decoded); err != nil || code != 0 || diag.Len() != 0 || decoded.Inventory.Conduits == nil || calls.Load() != 4 {
			t.Fatal(code, out.String(), diag.String(), err)
		}
		for _, bad := range []string{"PRIVATE", "fixture-access", "fixture-secret", "ciphertext", f.AppExecutionURL} {
			if strings.Contains(out.String()+diag.String(), bad) {
				t.Fatal("private output")
			}
		}
		var after string
		if err := f.Admin.QueryRow(query, m.ID).Scan(&after); err != nil || after != snapshot {
			t.Fatal("native inventory mutated custody", err)
		}
		for _, dsn := range []string{f.RuntimeURL, f.AppAdminURL} {
			out.Reset()
			diag.Reset()
			if code := run(t.Context(), a, fixtureEnvironment(dsn), &out, &diag, realInspection, client); code != 1 || out.Len() != 0 || calls.Load() != 4 {
				t.Fatal("foreign role inventory read", code)
			}
		}
	}
	if _, err := admin.Revoke(t.Context(), m.ID, m.Revision); err != nil {
		t.Fatal(err)
	}
	var out, diag bytes.Buffer
	client := &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
		t.Fatal("revoked inventory contacted provider")
		return nil, nil
	})}
	if code := run(t.Context(), a, fixtureEnvironment(f.AppExecutionURL), &out, &diag, realInspection, client); code != 1 || out.Len() != 0 {
		t.Fatal(code)
	}
}

func TestRealNativeInventoryActualHTTPCancellation(t *testing.T) {
	f, _, m := seedCustody(t)
	a := fixtureArguments(m.ID)
	a = append(a[:6:6], "--inventory")
	entered, stopped := make(chan struct{}), make(chan struct{})
	var lists atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/validate" {
			_, _ = w.Write([]byte(`{"client_id":"Client-A","expires_in":3600}`))
			return
		}
		lists.Add(1)
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
	done := make(chan int, 1)
	var out, diag bytes.Buffer
	go func() {
		done <- run(ctx, a, fixtureEnvironment(f.AppExecutionURL), &out, &diag, realInspection, client)
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("inventory request missing")
	}
	cancel()
	select {
	case code := <-done:
		if code != 130 || out.Len() != 0 || lists.Load() != 1 {
			t.Fatal(code, out.String(), diag.String())
		}
	case <-time.After(4 * time.Second):
		t.Fatal("inventory did not settle")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("HTTP request still active")
	}
}
