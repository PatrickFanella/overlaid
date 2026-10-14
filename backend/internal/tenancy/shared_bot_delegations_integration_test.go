package tenancy

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
)

func TestSharedBotPublicationConsentAndIndependentRevocation(t *testing.T) {
	s, a, b, owner, other, f := tenantPair(t)
	ctx := t.Context()
	k := botKeys(t)
	admin, err := OpenBotAdminStore(ctx, f.BotAdminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	account, err := admin.SaveValidated(ctx, k, syntheticBot("shared"), 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct{ tenant, actor, channel string }{{a.ID, owner, "channel_a"}, {b.ID, other, "channel_b"}} {
		if err = s.SaveConnection(ctx, k, v.tenant, v.actor, "twitch", v.channel, v.channel, nil, json.RawMessage(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	catalog, err := s.SharedBotCatalog(ctx, a.ID, owner)
	if err != nil || len(catalog) != 0 {
		t.Fatal("unpublished bot visible", catalog, err)
	}
	caps := []platform.Capability{"chat.send", "message.delete", "chat.send"}
	if _, err = s.GrantSharedBot(ctx, a.ID, owner, "channel_a", account.ID, 1, 1, 0, caps); !errors.Is(err, ErrDenied) {
		t.Fatal("unpublished grant", err)
	}
	metadata, err := admin.PublishMetadata(ctx, account.ID, 1, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	receipts, err := admin.Receipts(ctx, 100)
	if err != nil || len(receipts) != 2 || receipts[0].CatalogRevision == nil || *receipts[0].CatalogRevision != metadata.Revision {
		t.Fatal("publication receipt missing epoch", receipts, err)
	}
	catalog, err = s.SharedBotCatalog(ctx, a.ID, owner)
	if err != nil || len(catalog) != 1 || catalog[0].Revision != metadata.Revision {
		t.Fatal(catalog, err)
	}
	if _, err = admin.PublishMetadata(ctx, account.ID, 1, 0, false); !errors.Is(err, ErrBotRevision) {
		t.Fatal("stale publication accepted", err)
	}
	if _, err = s.SharedBotCatalog(ctx, a.ID, other); !errors.Is(err, ErrDenied) {
		t.Fatal("foreign catalog access", err)
	}
	if _, err = s.GrantSharedBot(ctx, a.ID, other, "channel_a", account.ID, 1, 1, 0, caps); !errors.Is(err, ErrDenied) {
		t.Fatal("foreign consent accepted", err)
	}
	if _, err = s.GrantSharedBot(ctx, a.ID, owner, "wrong", account.ID, 1, 1, 0, caps); !errors.Is(err, ErrDenied) {
		t.Fatal("stale channel accepted", err)
	}
	if _, err = s.GrantSharedBot(ctx, a.ID, owner, "channel_a", account.ID, 1, 1, 0, []platform.Capability{"role.moderator.manage"}); !errors.Is(err, ErrDenied) {
		t.Fatal("capability escalation", err)
	}
	grantA, err := s.GrantSharedBot(ctx, a.ID, owner, "channel_a", account.ID, 1, 1, 0, caps)
	if err != nil || len(grantA.Capabilities) != 2 {
		t.Fatal(grantA, err)
	}
	grantB, err := s.GrantSharedBot(ctx, b.ID, other, "channel_b", account.ID, 1, 1, 0, caps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveSharedBot(ctx, a.ID, "channel_a", account.ID, grantA.Revision, "chat.send"); err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct {
		tenant, channel string
		rev             int64
		cap             platform.Capability
	}{{b.ID, "channel_a", 1, "chat.send"}, {a.ID, "wrong", 1, "chat.send"}, {a.ID, "channel_a", 2, "chat.send"}, {a.ID, "channel_a", 1, "user.ban"}, {a.ID, "channel_a", 1, "unknown"}} {
		if _, err = s.ResolveSharedBot(ctx, v.tenant, v.channel, account.ID, v.rev, v.cap); !errors.Is(err, ErrDenied) {
			t.Fatal("invalid execution context", v, err)
		}
	}
	if _, err = s.GrantSharedBot(ctx, a.ID, owner, "channel_a", account.ID, 1, 1, 0, caps); !errors.Is(err, ErrBotRevision) {
		t.Fatal("stale consent changed", err)
	}
	if _, err = s.RevokeSharedBot(ctx, a.ID, other, grantA.Revision); !errors.Is(err, ErrDenied) {
		t.Fatal("foreign revoke", err)
	}
	if _, err = s.RevokeSharedBot(ctx, a.ID, owner, grantA.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveSharedBot(ctx, a.ID, "channel_a", account.ID, grantA.Revision, "chat.send"); !errors.Is(err, ErrDenied) {
		t.Fatal("revoked tenant resolved", err)
	}
	if _, err = s.ResolveSharedBot(ctx, b.ID, "channel_b", account.ID, grantB.Revision, "chat.send"); err != nil {
		t.Fatal("other tenant revoked", err)
	}
	if _, err = admin.Resolve(ctx, k, account.ID, 1, noRefresh); err != nil {
		t.Fatal("tenant revoke affected credentials", err)
	}
	// Removing publication fences every old tenant consent, including after re-publish.
	metadata, err = admin.PublishMetadata(ctx, account.ID, 1, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveSharedBot(ctx, b.ID, "channel_b", account.ID, 1, "chat.send"); !errors.Is(err, ErrDenied) {
		t.Fatal("unpublished execution", err)
	}
	metadata, err = admin.PublishMetadata(ctx, account.ID, 1, metadata.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveSharedBot(ctx, b.ID, "channel_b", account.ID, 1, "chat.send"); !errors.Is(err, ErrDenied) {
		t.Fatal("republish revived consent", err)
	}
	grantB, err = s.GrantSharedBot(ctx, b.ID, other, "channel_b", account.ID, 1, metadata.Revision, 1, caps)
	if err != nil {
		t.Fatal(err)
	}
	exported, err := s.Export(ctx, other, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(exported)
	if !bytes.Contains(raw, []byte(account.ID)) {
		t.Fatal("delegation missing from export")
	}
	for _, secret := range []string{"private-access", "private-refresh", "ciphertext", "key_version"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatal("export leaked", secret)
		}
	}
	if err = s.DeleteTenant(ctx, owner, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveSharedBot(ctx, b.ID, "channel_b", account.ID, grantB.Revision, "chat.send"); err != nil {
		t.Fatal("other tenant affected by deletion", err)
	}
	account, err = admin.SaveValidated(ctx, k, syntheticBot("shared"), 1)
	if err != nil || account.Revision != 2 {
		t.Fatal(account, err)
	}
	if _, err = s.ResolveSharedBot(ctx, b.ID, "channel_b", account.ID, grantB.Revision, "chat.send"); !errors.Is(err, ErrDenied) {
		t.Fatal("reauth revived old grant", err)
	}
	adminCatalog, err := admin.Catalog(ctx)
	if err != nil || len(adminCatalog) != 1 || adminCatalog[0].Available {
		t.Fatal("reauth left published", adminCatalog, err)
	}
	metadata, err = admin.PublishMetadata(ctx, account.ID, 2, adminCatalog[0].Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	grantB, err = s.GrantSharedBot(ctx, b.ID, other, "channel_b", account.ID, 2, metadata.Revision, grantB.Revision, caps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Revoke(ctx, account.ID, 2); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveSharedBot(ctx, b.ID, "channel_b", account.ID, grantB.Revision, "chat.send"); !errors.Is(err, ErrDenied) {
		t.Fatal("revoked service resolved", err)
	}
}

func TestSharedBotBroadcasterAndOwnBotTransitions(t *testing.T) {
	s, a, _, owner, other, f := tenantPair(t)
	ctx := t.Context()
	k := botKeys(t)
	admin, err := OpenBotAdminStore(ctx, f.BotAdminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	account, err := admin.SaveValidated(ctx, k, syntheticBot("shared"), 0)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := admin.PublishMetadata(ctx, account.ID, 1, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveConnection(ctx, k, a.ID, owner, "twitch", "channel", "channel", nil, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	own, err := s.SaveValidatedBotAccount(ctx, k, a.ID, owner, syntheticBot("own"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BindBotAccount(ctx, a.ID, owner, "channel", own); err != nil {
		t.Fatal(err)
	}
	grant, err := s.GrantSharedBot(ctx, a.ID, owner, "channel", account.ID, 1, metadata.Revision, 0, []platform.Capability{"chat.send"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveBoundBotAccount(ctx, k, a.ID, "channel", own, noRefresh); !errors.Is(err, ErrDenied) {
		t.Fatal("shared grant left own binding", err)
	}
	if err = s.BindBotAccount(ctx, a.ID, owner, "channel", own); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveSharedBot(ctx, a.ID, "channel", account.ID, grant.Revision, "chat.send"); !errors.Is(err, ErrDenied) {
		t.Fatal("own bind left shared grant", err)
	}
	grant, err = s.SharedBotDelegation(ctx, a.ID, owner)
	if err != nil || !grant.Revoked || grant.Revision != 2 {
		t.Fatal(grant, err)
	}
	grant, err = s.GrantSharedBot(ctx, a.ID, owner, "channel", account.ID, 1, metadata.Revision, grant.Revision, []platform.Capability{"chat.send"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveConnection(ctx, k, a.ID, owner, "twitch", "channel", "channel", nil, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveSharedBot(ctx, a.ID, "channel", account.ID, grant.Revision, "chat.send"); !errors.Is(err, ErrDenied) {
		t.Fatal("reauth retained shared grant", err)
	}
	grant, err = s.SharedBotDelegation(ctx, a.ID, owner)
	if err != nil || !grant.Revoked {
		t.Fatal(grant, err)
	}
	grant, err = s.GrantSharedBot(ctx, a.ID, owner, "channel", account.ID, 1, metadata.Revision, grant.Revision, []platform.Capability{"chat.send"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RevokeConnection(ctx, a.ID, owner, "twitch"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveSharedBot(ctx, a.ID, "channel", account.ID, grant.Revision, "chat.send"); !errors.Is(err, ErrDenied) {
		t.Fatal("revoked broadcaster retained grant", err)
	}
	if err = s.SetMember(ctx, owner, a.ID, other, "manager"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GrantSharedBot(ctx, a.ID, other, "channel", account.ID, 1, metadata.Revision, grant.Revision, []platform.Capability{"chat.send"}); !errors.Is(err, ErrDenied) {
		t.Fatal("manager consented", err)
	}
}

func TestSharedBotCatalogRoleIsolationAndConcurrentConsent(t *testing.T) {
	s, a, b, owner, other, f := tenantPair(t)
	ctx := t.Context()
	k := botKeys(t)
	admin, err := OpenBotAdminStore(ctx, f.BotAdminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	account, err := admin.SaveValidated(ctx, k, syntheticBot("shared"), 0)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := admin.PublishMetadata(ctx, account.ID, 1, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveConnection(ctx, k, a.ID, owner, "twitch", "channel", "channel", nil, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	success := make(chan SharedBotDelegation, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g, e := s.GrantSharedBot(ctx, a.ID, owner, "channel", account.ID, 1, metadata.Revision, 0, []platform.Capability{"chat.send"})
			if e == nil {
				success <- g
			} else if !errors.Is(e, ErrBotRevision) {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	close(success)
	if len(success) != 1 {
		t.Fatal("concurrent consent count", len(success))
	}
	runtimeDB, err := sql.Open("pgx", f.RuntimeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeDB.Close()
	if _, err = runtimeDB.Exec(`UPDATE service_bot_catalog SET available=false`); err == nil {
		t.Fatal("runtime wrote catalog")
	}
	var count int
	if err = runtimeDB.QueryRow(`SELECT count(*) FROM shared_bot_delegations`).Scan(&count); err != nil || count != 0 {
		t.Fatal("missing scope exposed grants", count, err)
	}
	db, _ := s.Product.Scoped(b.ID)
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM shared_bot_delegations WHERE /*tenant:shared_bot_delegations*/`).Scan(&count); err != nil || count != 0 {
		t.Fatal("foreign grants visible", count, err)
	}
	if _, err = admin.db.Exec(`SELECT * FROM shared_bot_delegations`); err == nil {
		t.Fatal("vault admin accessed tenant grants")
	}
	worker, err := sql.Open("pgx", f.WorkerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	if err = worker.QueryRow(`SELECT count(*) FROM service_bot_catalog`).Scan(&count); err == nil {
		t.Fatal("worker accessed catalog")
	}
	if _, err = s.SharedBotDelegation(ctx, a.ID, other); !errors.Is(err, ErrDenied) {
		t.Fatal("foreign consent inspection", err)
	}
	// Publication receipt failure leaves availability and epoch unchanged.
	var role string
	if err = admin.db.QueryRow(`SELECT current_user`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`REVOKE INSERT ON service_bot_account_audit FROM ` + role); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.PublishMetadata(ctx, account.ID, 1, 1, false); err == nil {
		t.Fatal("publication survived missing receipt")
	}
	if _, err = f.Admin.Exec(`GRANT INSERT ON service_bot_account_audit TO ` + role); err != nil {
		t.Fatal(err)
	}
	catalog, err := s.SharedBotCatalog(ctx, a.ID, owner)
	if err != nil || len(catalog) != 1 || !catalog[0].Available || catalog[0].Revision != 1 {
		t.Fatal("publication rollback failed", catalog, err)
	}
}
