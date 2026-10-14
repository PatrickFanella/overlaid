package tenancy

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestServiceBotRoleBoundaryAndTenantIndependence(t *testing.T) {
	tenant, a, _, owner, _, f := tenantPair(t)
	ctx := t.Context()
	admin, err := OpenBotAdminStore(ctx, f.BotAdminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	for _, dsn := range []string{f.RuntimeURL, f.WorkerURL} {
		if unexpected, e := OpenBotAdminStore(ctx, dsn); e == nil {
			unexpected.Close()
			t.Fatal("unsafe service credentials accepted")
		}
	}
	ownerStore := &BotAdminStore{db: f.Admin}
	if err = ownerStore.validate(ctx); err == nil {
		t.Fatal("migration owner accepted")
	}
	account, err := admin.SaveValidated(ctx, botKeys(t), syntheticBot("shared"), 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, dsn := range []string{f.RuntimeURL, f.WorkerURL} {
		db, e := sql.Open("pgx", dsn)
		if e != nil {
			t.Fatal(e)
		}
		for _, table := range []string{"service_bot_accounts", "service_bot_account_audit"} {
			var count int
			if e = db.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&count); e == nil {
				t.Fatal("service table visible to product/worker", table)
			}
		}
		db.Close()
	}
	for _, table := range []string{"bot_accounts", "platform_connections", "accounts", "river_job"} {
		var count int
		if err = admin.db.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&count); err == nil {
			t.Fatal("admin accessed unrelated table", table)
		}
	}
	exported, err := tenant.Export(ctx, owner, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(exported)
	if bytes.Contains(raw, []byte(account.ID)) {
		t.Fatal("service identity entered tenant export")
	}
	if err = tenant.DeleteTenant(ctx, owner, a.ID); err != nil {
		t.Fatal(err)
	}
	c, err := admin.Resolve(ctx, botKeys(t), account.ID, 1, noRefresh)
	if err != nil || c.UserID != "shared" {
		t.Fatal("tenant deletion removed shared identity", err)
	}
	var receipt int
	if err = f.Admin.QueryRow(`SELECT count(*) FROM overlaid_migrations WHERE name='0011_service_bot_accounts.sql'`).Scan(&receipt); err != nil || receipt != 1 {
		t.Fatal("missing migration receipt", err)
	}
	// An inherited grant is rejected at startup even if FORCE RLS would deny rows.
	var role, runtimeRole string
	if err = admin.db.QueryRow(`SELECT current_user`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	runtimeDB, err := sql.Open("pgx", f.RuntimeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeDB.Close()
	if err = runtimeDB.QueryRow(`SELECT current_user`).Scan(&runtimeRole); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`ALTER ROLE ` + role + ` NOINHERIT; GRANT ` + runtimeRole + ` TO ` + role); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, e := f.Admin.Exec(`REVOKE ` + runtimeRole + ` FROM ` + role + `; ALTER ROLE ` + role + ` INHERIT`); e != nil {
			t.Error(e)
		}
	})
	if unexpected, e := OpenBotAdminStore(ctx, f.BotAdminURL); e == nil {
		unexpected.Close()
		t.Fatal("reachable NOINHERIT product role accepted")
	}
}

func TestServiceBotRefreshRevisionRotationAndReceipts(t *testing.T) {
	_, _, _, _, _, f := tenantPair(t)
	ctx := t.Context()
	k := botKeys(t)
	admin, err := OpenBotAdminStore(ctx, f.BotAdminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	a, err := admin.SaveValidated(ctx, k, syntheticBot("one"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.SaveValidated(ctx, k, syntheticBot("one"), 0); !errors.Is(err, ErrBotRevision) {
		t.Fatal("stale grant overwrote", err)
	}
	newKey := make([]byte, 32)
	newKey[0] = 1
	rotated, _ := ParseKeyring("v2", "v1:"+base64.StdEncoding.EncodeToString(make([]byte, 32))+",v2:"+base64.StdEncoding.EncodeToString(newKey))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := admin.Resolve(ctx, rotated, a.ID, a.Revision, func(c BotCredential) (BotCredential, error) { c.AccessToken += "x"; return c, nil })
			if e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	restarted, err := OpenBotAdminStore(ctx, f.BotAdminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	onlyNew, _ := ParseKeyring("v2", "v2:"+base64.StdEncoding.EncodeToString(newKey))
	c, err := restarted.Resolve(ctx, onlyNew, strings.ToUpper(a.ID), 1, noRefresh)
	if err != nil || c.AccessToken != "private-accessxxxxxxxx" {
		t.Fatal("refresh/restart lost", err)
	}
	for _, refresh := range []func(BotCredential) (BotCredential, error){
		func(c BotCredential) (BotCredential, error) { c.UserID = "wrong"; return c, nil },
		func(c BotCredential) (BotCredential, error) { c.ExpiresAt = time.Now().Add(-time.Hour); return c, nil },
		func(c BotCredential) (BotCredential, error) { return c, errors.New("private provider detail") },
	} {
		if _, err = admin.Resolve(ctx, onlyNew, a.ID, 1, refresh); err == nil || strings.Contains(err.Error(), "private") {
			t.Fatal("bad refresh accepted/leaked", err)
		}
	}
	after, err := admin.Resolve(ctx, onlyNew, a.ID, 1, noRefresh)
	if err != nil || after.AccessToken != c.AccessToken {
		t.Fatal("refresh rollback lost", err)
	}
	var sealed []byte
	var version string
	if err = f.Admin.QueryRow(`SELECT ciphertext,key_version FROM service_bot_accounts WHERE id=$1`, a.ID).Scan(&sealed, &version); err != nil || version != "v2" || bytes.Contains(sealed, []byte("private")) {
		t.Fatal("ciphertext/rotation", err)
	}
	if _, err = onlyNew.open(serviceBotScope, serviceBotPurpose("wrong"), version, sealed); err == nil {
		t.Fatal("purpose binding absent")
	}
	if _, err = onlyNew.open("tenant", serviceBotPurpose(a.ID), version, sealed); err == nil {
		t.Fatal("scope binding absent")
	}
	revision, err := admin.Revoke(ctx, a.ID, 1)
	if err != nil || revision != 2 {
		t.Fatal(revision, err)
	}
	if _, err = admin.Resolve(ctx, onlyNew, a.ID, 1, noRefresh); !errors.Is(err, ErrDenied) {
		t.Fatal("revoked bot resolved", err)
	}
	if _, err = admin.Revoke(ctx, a.ID, 1); !errors.Is(err, ErrBotRevision) {
		t.Fatal("stale revoke accepted", err)
	}
	var cleared bool
	if err = f.Admin.QueryRow(`SELECT ciphertext IS NULL AND key_version IS NULL AND scopes='[]' FROM service_bot_accounts WHERE id=$1`, a.ID).Scan(&cleared); err != nil || !cleared {
		t.Fatal("revocation kept secrets", err)
	}
	a, err = admin.SaveValidated(ctx, onlyNew, syntheticBot("one"), 2)
	if err != nil || a.Revision != 3 {
		t.Fatal("reauth revision", a, err)
	}
	if _, err = admin.Resolve(ctx, onlyNew, a.ID, 1, noRefresh); !errors.Is(err, ErrBotRevision) {
		t.Fatal("old grant resolved after reauth", err)
	}
	receipts, err := admin.Receipts(ctx, 100)
	if err != nil || len(receipts) != 13 {
		t.Fatal("audit receipt count", len(receipts), err)
	}
	for _, r := range receipts {
		if r.Principal == "" || r.BotAccountID != a.ID || r.CreatedAt.IsZero() {
			t.Fatal("invalid receipt", r)
		}
	}
	raw, _ := json.Marshal(receipts)
	metadata, _ := admin.Accounts(ctx)
	meta, _ := json.Marshal(metadata)
	for _, secret := range []string{"private", "ciphertext", "key_version"} {
		if bytes.Contains(raw, []byte(secret)) || bytes.Contains(meta, []byte(secret)) {
			t.Fatal("leaked metadata", secret)
		}
	}
	if _, err = admin.Receipts(ctx, 101); err == nil {
		t.Fatal("unbounded receipt list")
	}
	// Receipt failure rolls back both the credential grant and its revision.
	var role string
	if err = admin.db.QueryRow(`SELECT current_user`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`REVOKE INSERT ON service_bot_account_audit FROM ` + role); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.SaveValidated(ctx, onlyNew, syntheticBot("one"), 3); err == nil {
		t.Fatal("grant survived missing receipt")
	}
	if _, err = f.Admin.Exec(`GRANT INSERT ON service_bot_account_audit TO ` + role); err != nil {
		t.Fatal(err)
	}
	if err = f.Admin.QueryRow(`SELECT revision FROM service_bot_accounts WHERE id=$1`, a.ID).Scan(&revision); err != nil || revision != 3 {
		t.Fatal("failed receipt changed grant", revision, err)
	}
}

func TestServiceBotAdmissionAndAuditBounds(t *testing.T) {
	_, _, _, _, _, f := tenantPair(t)
	ctx := t.Context()
	k := botKeys(t)
	admin, err := OpenBotAdminStore(ctx, f.BotAdminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	var wg sync.WaitGroup
	for i := 0; i < 36; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := admin.SaveValidated(ctx, k, syntheticBot(fmt.Sprint(i)), 0)
			if e != nil && !errors.Is(e, ErrLimit) {
				t.Error(e)
			}
		}(i)
	}
	wg.Wait()
	accounts, err := admin.Accounts(ctx)
	if err != nil || len(accounts) != 32 {
		t.Fatal("unbounded admission", len(accounts), err)
	}
	// Seed only the owned fixture audit table to exercise bounded pruning.
	if _, err = f.Admin.Exec(`INSERT INTO service_bot_account_audit(bot_account_id,revision,action,created_at) SELECT $1,1,'refresh',now()-interval '91 days' FROM generate_series(1,10)`, accounts[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`INSERT INTO service_bot_account_audit(bot_account_id,revision,action) SELECT $1,1,'refresh' FROM generate_series(1,1100)`, accounts[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Resolve(ctx, k, accounts[0].ID, 1, noRefresh); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = f.Admin.QueryRow(`SELECT count(*) FROM service_bot_account_audit`).Scan(&count); err != nil || count != 1024 {
		t.Fatal("unbounded audit", count, err)
	}
	if err = f.Admin.QueryRow(`SELECT count(*) FROM service_bot_account_audit WHERE created_at<now()-interval '90 days'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("old audit retained", count, err)
	}
}

func TestServiceBotRevocationDuringRefreshAndUnsafeRole(t *testing.T) {
	_, _, _, _, _, f := tenantPair(t)
	ctx := t.Context()
	k := botKeys(t)
	admin, err := OpenBotAdminStore(ctx, f.BotAdminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	a, err := admin.SaveValidated(ctx, k, syntheticBot("one"), 0)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, e := admin.Resolve(ctx, k, a.ID, 1, func(c BotCredential) (BotCredential, error) {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return c, ctx.Err()
			}
			return c, nil
		})
		done <- e
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh did not start")
	}
	revoked := make(chan error, 1)
	go func() { _, e := admin.Revoke(ctx, a.ID, 1); revoked <- e }()
	close(release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if err = <-revoked; err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Resolve(ctx, k, a.ID, 1, noRefresh); !errors.Is(err, ErrDenied) {
		t.Fatal("revocation resurrected", err)
	}
	var role string
	if err = admin.db.QueryRow(`SELECT current_user`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`ALTER ROLE ` + role + ` BYPASSRLS`); err != nil {
		t.Fatal(err)
	}
	if unexpected, e := OpenBotAdminStore(ctx, f.BotAdminURL); e == nil {
		unexpected.Close()
		t.Fatal("bypass role accepted")
	}
	if _, err = f.Admin.Exec(`ALTER ROLE ` + role + ` NOBYPASSRLS`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`CREATE POLICY unexpected_policy ON service_bot_accounts TO ` + role + ` USING(true)`); err != nil {
		t.Fatal(err)
	}
	if unexpected, e := OpenBotAdminStore(ctx, f.BotAdminURL); e == nil {
		unexpected.Close()
		t.Fatal("unexpected policy accepted")
	}
	if _, err = f.Admin.Exec(`DROP POLICY unexpected_policy ON service_bot_accounts; ALTER TABLE service_bot_accounts NO FORCE ROW LEVEL SECURITY`); err != nil {
		t.Fatal(err)
	}
	if unexpected, e := OpenBotAdminStore(ctx, f.BotAdminURL); e == nil {
		unexpected.Close()
		t.Fatal("unforced RLS accepted")
	}
}

func TestServiceBotRequiresExplicitDSN(t *testing.T) {
	if s, err := OpenBotAdminStore(t.Context(), ""); err == nil {
		s.Close()
		t.Fatal("implicit database credentials accepted")
	}
}
