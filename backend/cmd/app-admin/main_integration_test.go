package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/appvault"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
)

func realCustody(ctx context.Context, dsn string) (custody, error) { return appvault.Open(ctx, dsn) }
func seed(t *testing.T, db *sql.DB) string {
	t.Helper()
	var id string
	// Owner-only synthetic ciphertext: the CLI never decrypts or installs it.
	if err := db.QueryRow(`INSERT INTO service_app_credentials(client_id,secret_ciphertext,token_ciphertext,key_version,validated_at,expires_at) VALUES('fixture-client',decode(repeat('aa',28),'hex'),decode(repeat('bb',28),'hex'),'fixture-v1',clock_timestamp(),clock_timestamp()+interval '1 hour') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO service_app_credential_audit(app_id,revision,action) VALUES($1,1,'install')`, id); err != nil {
		t.Fatal(err)
	}
	return id
}
func TestRealCustodyCLIReadRevokeAndRefusal(t *testing.T) {
	f := testpostgres.NewTenantFixture(t, "")
	id := seed(t, f.Admin)
	invoke := func(dsn string, args ...string) (int, string, string) {
		t.Helper()
		var out, diagnostic bytes.Buffer
		code := run(t.Context(), args, func(name string) string {
			if name != "OVERLAID_APP_ADMIN_DATABASE_URL" {
				t.Fatal(name)
			}
			return dsn
		}, &out, &diagnostic, realCustody)
		return code, out.String(), diagnostic.String()
	}
	code, out, diagnostic := invoke(f.AppAdminURL, "list")
	if code != 0 || !strings.Contains(out, id) || strings.Contains(out, "ciphertext") || strings.Contains(out, "fixture-v1") || diagnostic != "" {
		t.Fatal(code, out, diagnostic)
	}
	code, out, diagnostic = invoke(f.AppAdminURL, "receipts", "--app", id, "--limit", "1000")
	if code != 0 || !strings.Contains(out, `"revision":1`) || diagnostic != "" {
		t.Fatal(code, out, diagnostic)
	}
	for range 2 {
		code, out, diagnostic = invoke(f.AppAdminURL, "revoke", "--app", id, "--revision", "2")
		if code != 1 || out != "" || diagnostic == "" {
			t.Fatal("stale/unknown future revision", code, out, diagnostic)
		}
	}
	code, out, diagnostic = invoke(f.AppAdminURL, "revoke", "--app", id, "--revision", "1")
	var result struct {
		Account appvault.Metadata `json:"account"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil || code != 0 || result.Account.ID != id || result.Account.Revision != 2 || !result.Account.Revoked || diagnostic != "" {
		t.Fatal(code, out, diagnostic, err)
	}
	code, out, diagnostic = invoke(f.AppAdminURL, "revoke", "--app", id, "--revision", "2")
	if code != 0 || diagnostic != "" {
		t.Fatal("matching revoked no-op", code, out, diagnostic)
	}
	var count int
	var clear bool
	if err := f.Admin.QueryRow(`SELECT count(*) FROM service_app_credential_audit WHERE app_id=$1`, id).Scan(&count); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	if err := f.Admin.QueryRow(`SELECT secret_ciphertext IS NULL AND token_ciphertext IS NULL AND key_version IS NULL AND validated_at IS NULL AND expires_at IS NULL FROM service_app_credentials WHERE id=$1`, id).Scan(&clear); err != nil || !clear {
		t.Fatal(clear, err)
	}
	code, out, diagnostic = invoke(f.AppAdminURL, "receipts", "--app", appID)
	if code != 0 || strings.TrimSpace(out) != `{"receipts":[]}` || diagnostic != "" {
		t.Fatal("missing history semantics", code, out, diagnostic)
	}
	code, out, diagnostic = invoke(f.RuntimeURL, "list")
	if code != 1 || out != "" || strings.Contains(diagnostic, f.RuntimeURL) || strings.Contains(diagnostic, "password") {
		t.Fatal("runtime role accepted/leaked", code, out, diagnostic)
	}
}
func TestRealCustodyCLIInterruptedLockWait(t *testing.T) {
	f := testpostgres.NewTenantFixture(t, "")
	id := seed(t, f.Admin)
	tx, err := f.Admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended('service_app_custody:'||current_database()||':'||current_schema(),0))`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var out, diagnostic bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, []string{"revoke", "--app", id, "--revision", "1"}, func(string) string { return f.AppAdminURL }, &out, &diagnostic, realCustody)
	}()
	dsn, err := url.Parse(f.AppAdminURL)
	if err != nil {
		t.Fatal(err)
	}
	waiting := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := f.Admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE usename=$1 AND wait_event='advisory')`, dsn.User.Username()).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("no blocked custody write")
	}
	cancel()
	select {
	case code := <-done:
		if code != 130 || out.Len() != 0 || strings.Contains(diagnostic.String(), f.AppAdminURL) {
			t.Fatal(code, out.String(), diagnostic.String())
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not stop real SQL wait")
	}
	var revision int
	if err := f.Admin.QueryRow(`SELECT revision FROM service_app_credentials WHERE id=$1`, id).Scan(&revision); err != nil || revision != 1 {
		t.Fatal("canceled write changed revision", revision, err)
	}
}
