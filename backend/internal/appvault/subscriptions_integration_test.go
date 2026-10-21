package appvault

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/conduitownership"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchconduit"
)

const subscriptionBody = `{"data":[{"id":"sub","type":"stream.online","version":"1","status":"enabled","condition":{"broadcaster_user_id":"channel"},"transport":{"method":"conduit","conduit_id":"selected"},"cost":0,"created_at":"2026-10-21T00:00:00Z","private":"PRIVATE-DATA"}],"total":1,"pagination":{}}`

func subscriptionInspectionClient(t *testing.T, modify func(*http.Request, int, string) (string, error)) (*http.Client, *atomic.Int32) {
	t.Helper()
	calls := new(atomic.Int32)
	return &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		n := int(calls.Add(1))
		if r.Method != http.MethodGet || r.URL.Scheme != "https" || r.Header.Get("Authorization") != "Bearer fixture-access" {
			t.Error("unexpected credential/request")
		}
		body := ""
		switch r.URL.Host + r.URL.Path {
		case "id.twitch.tv/oauth2/validate":
			body = `{"client_id":"client","expires_in":3600}`
		case "api.twitch.tv/helix/eventsub/conduits":
			body = `{"data":[{"id":"selected","shard_count":1}]}`
		case "api.twitch.tv/helix/eventsub/subscriptions":
			if r.URL.Query().Get("conduit_id") != "selected" {
				t.Error("wrong conduit filter")
			}
			body = subscriptionBody
		default:
			t.Error("unexpected provider endpoint")
		}
		if r.URL.Host == "api.twitch.tv" && r.Header.Get("Client-Id") != "client" {
			t.Error("wrong client header")
		}
		if modify != nil {
			var e error
			body, e = modify(r, n, body)
			if e != nil {
				return nil, e
			}
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}, calls
}
func TestCustodiedSubscriptionsPreserveCredentialAndOwnership(t *testing.T) {
	f, _, s, b, ledger := creationFixture(t)
	original := prepareCreation(t, ledger, b)
	_, attempt, e := ledger.Start(t.Context(), original.ID, original.Binding, 2)
	if e != nil || attempt == nil {
		t.Fatal(e)
	}
	if _, e := ledger.Complete(t.Context(), *attempt, twitchconduit.Creation{Outcome: twitchconduit.CreationUncertain}, twitchconduit.ErrUnavailable); !errors.Is(e, conduitownership.ErrUncertain) {
		t.Fatal(e)
	}
	query := `SELECT json_build_object('credentials',(SELECT json_agg(t) FROM service_app_credentials t),'audit',(SELECT json_agg(t) FROM service_app_credential_audit t),'issuance',(SELECT json_agg(t) FROM service_app_issuance t),'ownership',(SELECT json_agg(t) FROM service_conduit_creations t))::text`
	var before, after string
	if e := f.Admin.QueryRow(query).Scan(&before); e != nil {
		t.Fatal(e)
	}
	client, calls := subscriptionInspectionClient(t, nil)
	r, e := s.InspectConduitSubscriptions(t.Context(), keys(), b, "selected", client)
	if e != nil || !r.ValidFor("client", "selected") || len(r.Subscriptions) != 1 || calls.Load() != 6 {
		t.Fatal(r, e, calls.Load())
	}
	if e := f.Admin.QueryRow(query).Scan(&after); e != nil || after != before {
		t.Fatal("read changed custody/receipts/ownership", e)
	}
}
func TestCustodiedSubscriptionsRefuseAndFinalDrift(t *testing.T) {
	for _, kind := range []string{"revision", "keyring", "expired", "privilege", "ciphertext", "final_revision", "final_expiry", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			f, _, s, b := executionFixture(t)
			k := keys()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch kind {
			case "revision":
				b.Revision++
			case "keyring":
				k = tenancy.Keyring{}
			case "expired":
				execSQL(t, f.Admin, `UPDATE service_app_credentials SET expires_at=clock_timestamp()+interval '20 seconds' WHERE id=$1`, b.AppID)
			case "privilege":
				execSQL(t, f.Admin, `GRANT SELECT(secret_ciphertext) ON service_app_credentials TO `+role(f.AppExecutionURL))
				defer execSQL(t, f.Admin, `REVOKE SELECT(secret_ciphertext) ON service_app_credentials FROM `+role(f.AppExecutionURL))
			}
			client, calls := subscriptionInspectionClient(t, func(r *http.Request, n int, body string) (string, error) {
				if n == 6 {
					switch kind {
					case "ciphertext":
						execSQL(t, f.Admin, `UPDATE service_app_credentials SET token_ciphertext=decode(repeat('ab',40),'hex') WHERE id=$1`, b.AppID)
					case "final_revision":
						execSQL(t, f.Admin, `UPDATE service_app_credentials SET revision=revision+1 WHERE id=$1`, b.AppID)
					case "final_expiry":
						execSQL(t, f.Admin, `UPDATE service_app_credentials SET expires_at=clock_timestamp()+interval '20 seconds' WHERE id=$1`, b.AppID)
					case "cancel":
						cancel()
					}
				}
				return body, nil
			})
			r, e := s.InspectConduitSubscriptions(ctx, k, b, "selected", client)
			if e == nil || r.Subscriptions != nil {
				t.Fatal("partial/stale custody report", r, e)
			}
			if kind == "cancel" && !errors.Is(e, context.Canceled) {
				t.Fatal("cancellation identity", e)
			}
			if (kind == "revision" || kind == "keyring" || kind == "expired" || kind == "privilege") && calls.Load() != 0 {
				t.Fatal("provider reached refused custody")
			}
		})
	}
}
func TestCustodiedSubscriptionsPostLockOriginalRevision(t *testing.T) {
	f, _, _, b := executionFixture(t)
	u, _ := url.Parse(f.AppExecutionURL)
	q := u.Query()
	q.Set("default_transaction_isolation", "repeatable read")
	u.RawQuery = q.Encode()
	s, e := OpenExecution(t.Context(), u.String())
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = s.Close() }()
	owner, e := f.Admin.Begin()
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = owner.Rollback() }()
	if _, e := owner.Exec(`SELECT pg_advisory_xact_lock(hashtextextended('service_app_custody:'||current_database()||':'||current_schema(),0))`); e != nil {
		t.Fatal(e)
	}
	client, calls := subscriptionInspectionClient(t, nil)
	done := make(chan error, 1)
	go func() { _, e := s.InspectConduitSubscriptions(t.Context(), keys(), b, "selected", client); done <- e }()
	waitAppLock(t, f, f.AppExecutionURL)
	if _, e := owner.Exec(`UPDATE service_app_credentials SET revision=revision+1 WHERE id=$1`, b.AppID); e != nil {
		t.Fatal(e)
	}
	if e := owner.Commit(); e != nil {
		t.Fatal(e)
	}
	if e := <-done; e == nil || calls.Load() != 0 {
		t.Fatal("stale post-lock snapshot", e, calls.Load())
	}
}
