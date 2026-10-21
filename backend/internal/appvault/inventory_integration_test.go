package appvault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/conduitownership"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchconduit"
)

func TestCustodiedInventoryLeavesUnknownOwnershipAndCredentialsUnchanged(t *testing.T) {
	f, _, s, b, l := creationFixture(t)
	r := prepareCreation(t, l, b)
	_, attempt, err := l.Start(t.Context(), r.ID, r.Binding, 2)
	if err != nil || attempt == nil {
		t.Fatal(err)
	}
	if _, err = l.Complete(t.Context(), *attempt, twitchconduit.Creation{Outcome: twitchconduit.CreationUncertain}, twitchconduit.ErrUnavailable); !errors.Is(err, conduitownership.ErrUncertain) {
		t.Fatal(err)
	}
	query := `SELECT json_build_object('credentials',(SELECT json_agg(t) FROM service_app_credentials t),'audit',(SELECT json_agg(t) FROM service_app_credential_audit t),'issuance',(SELECT json_agg(t) FROM service_app_issuance t),'ownership',(SELECT json_agg(t) FROM service_conduit_creations t))::text`
	var before, after string
	if err = f.Admin.QueryRow(query).Scan(&before); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"data":[]}`, `{"data":[{"id":"unknown","shard_count":2,"private":"PRIVATE"}]}`, `{"data":[{"id":"z","shard_count":2},{"id":"a","shard_count":1}]}`} {
		client, calls := inspectorClient(t, func(request *http.Request, _ int, original string) (string, error) {
			if request.URL.Path == "/helix/eventsub/conduits/shards" {
				t.Error("inventory fetched shards")
			}
			if request.URL.Path == "/helix/eventsub/conduits" {
				return body, nil
			}
			return original, nil
		})
		result, err := s.InventoryConduits(t.Context(), keys(), b, client)
		if err != nil || result.ClientID != b.ClientID || result.Conduits == nil || calls.Load() != 4 {
			t.Fatal(result, err, calls.Load())
		}
		raw, _ := json.Marshal(result)
		if strings.Contains(string(raw), "PRIVATE") {
			t.Fatal("private result")
		}
		if err = f.Admin.QueryRow(query).Scan(&after); err != nil || after != before {
			t.Fatal("inventory changed custody or unresolved ownership", err)
		}
		prior, err := l.Lookup(t.Context(), r.ID)
		if err != nil || prior.State != "uncertain" {
			t.Fatal(prior, err)
		}
	}
}

func TestCustodiedInventoryRefusesBeforeProvider(t *testing.T) {
	f, admin, s, b := executionFixture(t)
	client, calls := inspectorClient(t, nil)
	wrong := b
	wrong.Revision++
	for _, test := range []struct {
		b Binding
		k tenancy.Keyring
	}{{wrong, keys()}, {b, tenancy.Keyring{}}} {
		result, err := s.InventoryConduits(t.Context(), test.k, test.b, client)
		if err == nil || result.Conduits != nil || calls.Load() != 0 {
			t.Fatal(result, err)
		}
	}
	var expiry time.Time
	if err := f.Admin.QueryRow(`SELECT expires_at FROM service_app_credentials WHERE id=$1`, b.AppID).Scan(&expiry); err != nil {
		t.Fatal(err)
	}
	execSQL(t, f.Admin, `UPDATE service_app_credentials SET expires_at=clock_timestamp()+interval '20 seconds' WHERE id=$1`, b.AppID)
	if result, err := s.InventoryConduits(t.Context(), keys(), b, client); err == nil || result.Conduits != nil || calls.Load() != 0 {
		t.Fatal(result, err)
	}
	execSQL(t, f.Admin, `UPDATE service_app_credentials SET expires_at=$2 WHERE id=$1`, b.AppID, expiry)
	execSQL(t, f.Admin, `GRANT SELECT(secret_ciphertext) ON service_app_credentials TO `+role(f.AppExecutionURL))
	if result, err := s.InventoryConduits(t.Context(), keys(), b, client); !errors.Is(err, ErrBoundary) || result.Conduits != nil || calls.Load() != 0 {
		t.Fatal(result, err)
	}
	execSQL(t, f.Admin, `REVOKE SELECT(secret_ciphertext) ON service_app_credentials FROM `+role(f.AppExecutionURL))
	if _, err := admin.Revoke(t.Context(), b.AppID, b.Revision); err != nil {
		t.Fatal(err)
	}
	if result, err := s.InventoryConduits(t.Context(), keys(), b, client); err == nil || result.Conduits != nil || calls.Load() != 0 {
		t.Fatal(result, err)
	}
}

func TestCustodiedInventoryFinalDriftDiscardsReport(t *testing.T) {
	for _, kind := range []string{"revision", "ciphertext", "expiry", "role", "inventory", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			f, _, s, b := executionFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			client, calls := inspectorClient(t, func(request *http.Request, n int, body string) (string, error) {
				if n != 2 {
					return body, nil
				}
				switch kind {
				case "revision":
					execSQL(t, f.Admin, `UPDATE service_app_credentials SET revision=revision+1 WHERE id=$1`, b.AppID)
				case "ciphertext":
					execSQL(t, f.Admin, `UPDATE service_app_credentials SET token_ciphertext=decode(repeat('ab',40),'hex') WHERE id=$1`, b.AppID)
				case "expiry":
					execSQL(t, f.Admin, `UPDATE service_app_credentials SET expires_at=clock_timestamp()+interval '20 seconds' WHERE id=$1`, b.AppID)
				case "role":
					execSQL(t, f.Admin, `GRANT SELECT(secret_ciphertext) ON service_app_credentials TO `+role(f.AppExecutionURL))
				case "cancel":
					cancel()
				}
				return body, nil
			})
			// The provider helper's list index is not a validation index. A separate
			// count makes list drift deterministic without adding any provider write.
			if kind == "inventory" {
				var list int
				client, calls = inspectorClient(t, func(request *http.Request, _ int, body string) (string, error) {
					if request.URL.Path == "/helix/eventsub/conduits" {
						list++
						return fmt.Sprintf(`{"data":[{"id":"changed-%d","shard_count":1}]}`, list), nil
					}
					return body, nil
				})
			}
			result, err := s.InventoryConduits(ctx, keys(), b, client)
			if err == nil || result.Conduits != nil || calls.Load() < 3 {
				t.Fatal(result, err)
			}
			if kind == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation identity lost", err)
			}
			if kind == "role" {
				execSQL(t, f.Admin, `REVOKE SELECT(secret_ciphertext) ON service_app_credentials FROM `+role(f.AppExecutionURL))
			}
		})
	}
}

func TestCustodiedInventoryPostLockRevisionUnderInheritedIsolation(t *testing.T) {
	f, _, _, b := executionFixture(t)
	u, _ := url.Parse(f.AppExecutionURL)
	q := u.Query()
	q.Set("default_transaction_isolation", "repeatable read")
	u.RawQuery = q.Encode()
	s, err := OpenExecution(t.Context(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	owner, err := f.Admin.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Rollback() }()
	if _, err = owner.Exec(`SELECT pg_advisory_xact_lock(hashtextextended('service_app_custody:'||current_database()||':'||current_schema(),0))`); err != nil {
		t.Fatal(err)
	}
	client, calls := inspectorClient(t, nil)
	done := make(chan error, 1)
	go func() { _, err := s.InventoryConduits(t.Context(), keys(), b, client); done <- err }()
	waitAppLock(t, f, f.AppExecutionURL)
	if _, err = owner.Exec(`UPDATE service_app_credentials SET revision=revision+1 WHERE id=$1`, b.AppID); err != nil {
		t.Fatal(err)
	}
	if err = owner.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-done; !errors.Is(err, ErrRevision) || calls.Load() != 0 {
		t.Fatal("stale inventory authorization", err, calls.Load())
	}
}
