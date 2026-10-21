package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/appvault"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
)

func TestRealIssuanceStatusCLILeavesEveryStateUnchanged(t *testing.T) {
	f := testpostgres.NewTenantFixture(t, "")
	var id string
	if err := f.Admin.QueryRow(`INSERT INTO service_app_issuance(client_id,expected_revision,secret_fingerprint) VALUES('fixture-client',0,repeat('a',64)) RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	app := seed(t, f.Admin)
	for _, state := range []string{"prepared", "started", "issued", "refused", "uncertain"} {
		t.Run(state, func(t *testing.T) {
			_, err := f.Admin.Exec(`UPDATE service_app_issuance SET state=$2,claim_hash=CASE WHEN $2='prepared' THEN NULL ELSE repeat('b',64) END,started_at=CASE WHEN $2='prepared' THEN NULL ELSE clock_timestamp() END,completed_at=CASE WHEN $2 IN('prepared','started') THEN NULL ELSE clock_timestamp() END,failure_code=CASE WHEN $2='refused' THEN 'provider_refused' WHEN $2='uncertain' THEN 'uncertain' ELSE NULL END,installed_app_id=CASE WHEN $2='issued' THEN $3::uuid ELSE NULL END,installed_revision=CASE WHEN $2='issued' THEN 1 ELSE NULL END WHERE id=$1`, id, state, app)
			if err != nil {
				t.Fatal(err)
			}
			snapshot := func() string {
				var result string
				err := f.Admin.QueryRow(`SELECT row_to_json(i)::text || (SELECT row_to_json(c)::text FROM service_app_credentials c WHERE id=$2) || (SELECT count(*)::text FROM service_app_credential_audit) FROM service_app_issuance i WHERE id=$1`, id, app).Scan(&result)
				if err != nil {
					t.Fatal(err)
				}
				return result
			}
			before := snapshot()
			for _, selection := range []struct {
				dsn, id string
				want    int
			}{{f.AppAdminURL, id, 0}, {f.AppAdminURL, requestID, 1}, {f.AppExecutionURL, id, 1}, {f.RuntimeURL, id, 1}} {
				var out, diagnostic bytes.Buffer
				code := run(t.Context(), []string{"issuance", "--request", selection.id}, func(name string) string {
					if name != "OVERLAID_APP_ADMIN_DATABASE_URL" {
						t.Fatal(name)
					}
					return selection.dsn
				}, &out, &diagnostic, realCustody)
				if code != selection.want || snapshot() != before || strings.Contains(out.String()+diagnostic.String(), selection.dsn) {
					t.Fatal(code, out.String(), diagnostic.String())
				}
				if code == 0 {
					var result struct {
						Issuance appvault.Issuance `json:"issuance"`
					}
					if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Issuance.ID != id || result.Issuance.State != state || diagnostic.Len() != 0 {
						t.Fatal(out.String(), err)
					}
					for _, private := range []string{"ciphertext", "secret_fingerprint", "claim_hash", "key_version", strings.Repeat("a", 64), strings.Repeat("b", 64)} {
						if strings.Contains(out.String(), private) {
							t.Fatal("private issuance data leaked")
						}
					}
				} else if out.Len() != 0 || !strings.Contains(diagnostic.String(), "original") {
					t.Fatal(out.String(), diagnostic.String())
				}
			}
		})
	}
}

func TestRealIssuanceStatusCLIInterruptsSQLWait(t *testing.T) {
	f := testpostgres.NewTenantFixture(t, "")
	tx, err := f.Admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.Exec(`LOCK TABLE service_app_issuance IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var out, diagnostic bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, []string{"issuance", "--request", requestID}, func(string) string { return f.AppAdminURL }, &out, &diagnostic, realCustody)
	}()
	dsn, err := url.Parse(f.AppAdminURL)
	if err != nil {
		t.Fatal(err)
	}
	waiting := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err = f.Admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE usename=$1 AND wait_event_type='Lock')`, dsn.User.Username()).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("no blocked issuance lookup")
	}
	cancel()
	select {
	case code := <-done:
		if code != 130 || out.Len() != 0 || !strings.Contains(diagnostic.String(), "original") {
			t.Fatal(code, out.String(), diagnostic.String())
		}
	case <-time.After(time.Second):
		t.Fatal("issuance SQL wait ignored cancellation")
	}
}
