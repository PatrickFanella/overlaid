package tenancy

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
)

func executionFixture(t *testing.T) (testpostgres.TenantFixture, *BotAdminStore, *BotExecutionStore, ServiceBotAccount, SharedBotMetadata) {
	t.Helper()
	f := testpostgres.NewTenantFixture(t, "")
	admin, err := OpenBotAdminStore(t.Context(), f.BotAdminURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	execution, err := OpenBotExecutionStore(t.Context(), f.BotExecutorURL)
	if err != nil {
		var expression string
		_ = f.Admin.QueryRow(`SELECT pg_get_expr(polqual,polrelid) FROM pg_policy WHERE polname='service_bot_executor' AND polrelid='service_bot_accounts'::regclass`).Scan(&expression)
		t.Fatalf("execution constructor: %v; policy: %s", err, expression)
	}
	t.Cleanup(func() { _ = execution.Close() })
	account, err := admin.SaveValidated(t.Context(), botKeys(t), syntheticBot("123"), 0)
	if err != nil {
		t.Fatal(err)
	}
	var visible int
	if err = execution.db.QueryRow(`SELECT count(*) FROM service_bot_accounts`).Scan(&visible); err != nil || visible != 0 {
		t.Fatal("unpublished vault visible", visible, err)
	}
	catalog, err := admin.PublishMetadata(t.Context(), account.ID, account.Revision, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	return f, admin, execution, account, catalog
}

func TestServiceExecutionRefreshPublicationAndReceipts(t *testing.T) {
	f, admin, execution, account, catalog := executionFixture(t)
	key := make([]byte, 32)
	key[0] = 1
	rotated, err := ParseKeyring("v2", "v1:"+base64.StdEncoding.EncodeToString(make([]byte, 32))+",v2:"+base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := execution.Resolve(t.Context(), rotated, account.ID, 1, catalog.Revision, func(c BotCredential) (BotCredential, error) { c.AccessToken += "x"; return c, nil })
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	restarted, err := OpenBotExecutionStore(t.Context(), f.BotExecutorURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restarted.Close() }()
	onlyNew, err := ParseKeyring("v2", "v2:"+base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	c, err := restarted.Resolve(t.Context(), onlyNew, account.ID, 1, catalog.Revision, noRefresh)
	if err != nil || c.AccessToken != "private-accessxxxxxxxx" {
		t.Fatal("serialized refresh/restart failed", err)
	}
	var count, refreshes int
	var grant, published int64
	var principal string
	if err = f.Admin.QueryRow(`SELECT count(*),max(refreshes),max(revision),max(catalog_revision),max(principal) FROM service_bot_execution_receipts`).Scan(&count, &refreshes, &grant, &published, &principal); err != nil {
		t.Fatal(err)
	}
	role, _ := url.Parse(f.BotExecutorURL)
	if count != 1 || refreshes != 9 || grant != 1 || published != catalog.Revision || principal != role.User.Username() {
		t.Fatal("receipt fields", count, refreshes, grant, published, principal)
	}
	rows, err := execution.db.Query(`SELECT * FROM service_bot_execution_receipts`)
	if err != nil {
		t.Fatal(err)
	}
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	if strings.Join(columns, ",") != "bot_account_id,revision,catalog_revision,refreshes,principal,refreshed_at" {
		t.Fatal("receipt allowlist", columns)
	}
	var ciphertext []byte
	if err = f.Admin.QueryRow(`SELECT ciphertext FROM service_bot_accounts WHERE id=$1`, account.ID).Scan(&ciphertext); err != nil || bytes.Contains(ciphertext, []byte("private")) {
		t.Fatal("plaintext stored", err)
	}
	unpub, err := admin.PublishMetadata(t.Context(), account.ID, 1, catalog.Revision, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = execution.Resolve(t.Context(), rotated, account.ID, 1, catalog.Revision, noRefresh); !errors.Is(err, ErrDenied) {
		t.Fatal("unpublished resolve", err)
	}
	repub, err := admin.PublishMetadata(t.Context(), account.ID, 1, unpub.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = execution.Resolve(t.Context(), rotated, account.ID, 1, catalog.Revision, noRefresh); !errors.Is(err, ErrBotRevision) {
		t.Fatal("stale publication revived", err)
	}
	if err = execution.Published(t.Context(), account.ID, 1, repub.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Revoke(t.Context(), account.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err = execution.Published(t.Context(), account.ID, 1, repub.Revision); !errors.Is(err, ErrDenied) {
		t.Fatal("revoked published", err)
	}
}

func TestServiceExecutionRestrictedWritesAndReachableRoles(t *testing.T) {
	f, _, execution, account, _ := executionFixture(t)
	for _, dsn := range []string{f.RuntimeURL, f.WorkerURL, f.OutboundWorkerURL, f.PacerURL, f.BotAdminURL} {
		if unexpected, err := OpenBotExecutionStore(t.Context(), dsn); err == nil {
			_ = unexpected.Close()
			t.Fatal("wrong execution role accepted")
		}
	}
	if err := (&BotExecutionStore{db: f.Admin}).validate(t.Context()); err == nil {
		t.Fatal("owner accepted")
	}
	for _, table := range []string{"accounts", "platform_connections", "shared_bot_delegations", "bot_accounts", "river_job", "chat_pacing_accounts", "service_bot_account_audit"} {
		var count int
		if err := execution.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err == nil {
			t.Fatal("private table read", table)
		}
	}
	for _, query := range []string{`UPDATE service_bot_accounts SET revision=revision+1`, `UPDATE service_bot_accounts SET revoked_at=now()`, `DELETE FROM service_bot_accounts`, `UPDATE service_bot_catalog SET available=false`, `DELETE FROM service_bot_execution_receipts`, `UPDATE service_bot_execution_receipts SET principal='forged'`, `TRUNCATE service_bot_accounts`} {
		if _, err := execution.db.Exec(query); err == nil {
			t.Fatal("administrative write accepted", query)
		}
	}
	if _, err := execution.db.Exec(`INSERT INTO service_bot_execution_receipts(bot_account_id,revision,catalog_revision,principal) VALUES($1,1,1,'forged')`, account.ID); err == nil {
		t.Fatal("forged principal inserted")
	}
	execURL, _ := url.Parse(f.BotExecutorURL)
	runtimeURL, _ := url.Parse(f.RuntimeURL)
	adminURL, _ := url.Parse(f.BotAdminURL)
	execRole, runtimeRole, adminRole := execURL.User.Username(), runtimeURL.User.Username(), adminURL.User.Username()
	for _, other := range []string{runtimeRole, adminRole} {
		if _, err := f.Admin.Exec(`GRANT ` + other + ` TO ` + execRole + ` WITH INHERIT FALSE`); err != nil {
			t.Fatal(err)
		}
		if err := execution.validate(t.Context()); err == nil {
			t.Fatal("reachable privileged role accepted")
		}
		if _, err := f.Admin.Exec(`REVOKE ` + other + ` FROM ` + execRole); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.Admin.Exec(`GRANT ` + execRole + ` TO ` + adminRole + ` WITH INHERIT FALSE`); err != nil {
		t.Fatal(err)
	}
	if unexpected, err := OpenBotAdminStore(t.Context(), f.BotAdminURL); err == nil {
		_ = unexpected.Close()
		t.Fatal("combined admin/executor accepted")
	}
	if _, err := f.Admin.Exec(`REVOKE ` + execRole + ` FROM ` + adminRole); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(`GRANT SELECT(ciphertext) ON platform_connections TO ` + execRole); err != nil {
		t.Fatal(err)
	}
	if err := execution.validate(t.Context()); err == nil {
		t.Fatal("private column grant accepted")
	}
	if _, err := f.Admin.Exec(`REVOKE SELECT(ciphertext) ON platform_connections FROM ` + execRole + `; ALTER POLICY service_bot_executor ON service_bot_accounts USING(true) WITH CHECK(true)`); err != nil {
		t.Fatal(err)
	}
	if err := execution.validate(t.Context()); err == nil {
		t.Fatal("broad vault policy accepted")
	}
}

func TestServiceExecutionInvalidRefreshAndAtomicReceipt(t *testing.T) {
	f, _, execution, account, catalog := executionFixture(t)
	for _, change := range []func(BotCredential) (BotCredential, error){
		func(c BotCredential) (BotCredential, error) { c.UserID = "other"; return c, nil },
		func(c BotCredential) (BotCredential, error) { c.ExpiresAt = time.Now().Add(-time.Hour); return c, nil },
		func(c BotCredential) (BotCredential, error) { return c, errors.New("PRIVATE token detail") },
	} {
		if _, err := execution.Resolve(t.Context(), botKeys(t), account.ID, 1, catalog.Revision, change); err == nil || strings.Contains(err.Error(), "PRIVATE") {
			t.Fatal("invalid refresh accepted/leaked", err)
		}
	}
	execURL, _ := url.Parse(f.BotExecutorURL)
	role := execURL.User.Username()
	if _, err := f.Admin.Exec(`REVOKE INSERT(bot_account_id,revision,catalog_revision) ON service_bot_execution_receipts FROM ` + role); err != nil {
		t.Fatal(err)
	}
	if _, err := execution.Resolve(t.Context(), botKeys(t), account.ID, 1, catalog.Revision, func(c BotCredential) (BotCredential, error) { c.AccessToken = "changed"; return c, nil }); err == nil {
		t.Fatal("receipt failure lost atomicity")
	}
	if _, err := f.Admin.Exec(`GRANT INSERT(bot_account_id,revision,catalog_revision) ON service_bot_execution_receipts TO ` + role); err != nil {
		t.Fatal(err)
	}
	c, err := execution.Resolve(t.Context(), botKeys(t), account.ID, 1, catalog.Revision, noRefresh)
	if err != nil || c.AccessToken != "private-access" {
		t.Fatal("failed refresh persisted", err)
	}
	var raw []byte
	if err = f.Admin.QueryRow(`SELECT row_to_json(r) FROM service_bot_execution_receipts r`).Scan(&raw); err != nil || !json.Valid(raw) || bytes.Contains(raw, []byte("private")) {
		t.Fatal("receipt leaked", err)
	}
}

func TestExecutionReceiptTriggerCannotBeShadowed(t *testing.T) {
	f, _, execution, account, _ := executionFixture(t)
	conn, err := execution.db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	// A caller with TEMP permission must not redirect the receipt trigger's
	// account/catalog lookup through temporary relations.
	if _, err = conn.ExecContext(t.Context(), `CREATE TEMP TABLE service_bot_accounts(id uuid,revision bigint,revoked_at timestamptz); CREATE TEMP TABLE service_bot_catalog(id uuid,grant_revision bigint,revision bigint,available boolean)`); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(t.Context(), `INSERT INTO pg_temp.service_bot_accounts VALUES($1,999,NULL)`, account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(t.Context(), `INSERT INTO pg_temp.service_bot_catalog VALUES($1,999,999,true)`, account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(t.Context(), `INSERT INTO `+f.Schema+`.service_bot_execution_receipts(bot_account_id,revision,catalog_revision) VALUES($1,999,999)`, account.ID); err == nil {
		t.Fatal("temporary relations forged receipt")
	}
	if _, err = conn.ExecContext(t.Context(), `DROP TABLE pg_temp.service_bot_accounts,pg_temp.service_bot_catalog`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`ALTER TABLE service_bot_execution_receipts DISABLE TRIGGER service_bot_execution_receipt_stamp`); err != nil {
		t.Fatal(err)
	}
	if err = execution.validate(t.Context()); err == nil {
		t.Fatal("disabled receipt trigger accepted")
	}
}

func TestServiceExecutionRefreshAndRevocationSerialize(t *testing.T) {
	_, admin, execution, account, catalog := executionFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := execution.Resolve(t.Context(), botKeys(t), account.ID, 1, catalog.Revision, func(c BotCredential) (BotCredential, error) { close(entered); <-release; return c, nil })
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("refresh did not enter")
	}
	revoked := make(chan error, 1)
	go func() { _, err := admin.Revoke(t.Context(), account.ID, 1); revoked <- err }()
	select {
	case err := <-revoked:
		close(release)
		t.Fatal("revocation bypassed refresh lock", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-revoked; err != nil {
		t.Fatal(err)
	}
	if err := execution.Published(context.Background(), account.ID, 1, catalog.Revision); !errors.Is(err, ErrDenied) {
		t.Fatal("revocation did not fence later lookup", err)
	}
}
