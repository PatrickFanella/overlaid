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

func syntheticBot(user string) BotCredential {
	return BotCredential{Version: 1, UserID: user, Login: "bot_" + user, AccessToken: "private-access", RefreshToken: "private-refresh", ExpiresAt: time.Now().Add(time.Hour), Scopes: []string{"user:write:chat", "user:read:chat", "user:read:chat"}}
}
func botKeys(t *testing.T) Keyring {
	t.Helper()
	k, err := ParseKeyring("v1", "v1:"+base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	return k
}
func noRefresh(c BotCredential) (BotCredential, error) { return c, nil }

func TestBotAccountBindingIsolationRevocationAndLifecycle(t *testing.T) {
	s, a, b, owner, other, f := tenantPair(t)
	ctx := t.Context()
	k := botKeys(t)
	broadcaster := json.RawMessage(`{"access_token":"broadcaster-private","refresh_token":"original"}`)
	if err := s.SaveConnection(ctx, k, a.ID, owner, "twitch", "channel_a", "channel", nil, broadcaster); err != nil {
		t.Fatal(err)
	}
	id, err := s.SaveValidatedBotAccount(ctx, k, a.ID, owner, syntheticBot("one"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveValidatedBotAccount(ctx, k, a.ID, other, syntheticBot("other")); !errors.Is(err, ErrDenied) {
		t.Fatal("nonowner saved", err)
	}
	for _, v := range []struct{ tenant, actor string }{{a.ID, other}, {b.ID, other}} {
		if err = s.BindBotAccount(ctx, v.tenant, v.actor, "channel_a", id); !errors.Is(err, ErrDenied) {
			t.Fatal("foreign binding", err)
		}
		if err = s.RevokeBotAccount(ctx, v.tenant, v.actor, id); !errors.Is(err, ErrDenied) {
			t.Fatal("foreign revoke", err)
		}
	}
	if _, err = s.ResolveBoundBotAccount(ctx, k, a.ID, "channel_a", id, noRefresh); !errors.Is(err, ErrDenied) {
		t.Fatal("unbound resolved", err)
	}
	if err = s.BindBotAccount(ctx, a.ID, owner, "channel_a", id); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveBoundBotAccount(ctx, k, strings.ToUpper(a.ID), "channel_a", strings.ToUpper(id), noRefresh); err != nil {
		t.Fatal("UUID case changed credential binding", err)
	}
	for _, v := range []struct{ tenant, channel string }{{b.ID, "channel_a"}, {a.ID, "wrong"}} {
		if _, err = s.ResolveBoundBotAccount(ctx, k, v.tenant, v.channel, id, noRefresh); !errors.Is(err, ErrDenied) {
			t.Fatal("wrong context", err)
		}
	}
	missingScope, err := sql.Open("pgx", f.RuntimeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer missingScope.Close()
	var unseen int
	if err = missingScope.QueryRowContext(ctx, `SELECT count(*) FROM bot_accounts`).Scan(&unseen); err != nil || unseen != 0 {
		t.Fatal("absent scope exposed bot", unseen, err)
	}
	if err = f.Admin.QueryRow(`SELECT count(*) FROM overlaid_migrations WHERE name='0010_bot_accounts.sql'`).Scan(&unseen); err != nil || unseen != 1 {
		t.Fatal("migration receipt missing", unseen, err)
	}
	db, _ := s.Product.Scoped(b.ID)
	var count int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM bot_accounts WHERE /*tenant:bot_accounts*/ AND id=$1`, id).Scan(&count); err != nil || count != 0 {
		t.Fatal("RLS leak", count, err)
	}
	// Even bypassing the store cannot forge a foreign bot reference.
	if err = s.SaveConnection(ctx, k, b.ID, other, "twitch", "channel_b", "channelb", nil, broadcaster); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE platform_connections SET bot_account_id=$1 WHERE /*tenant:platform_connections*/`, id); err == nil {
		t.Fatal("cross-tenant FK accepted")
	}
	raw, err := s.ResolveConnection(ctx, k, a.ID, "twitch", func(raw json.RawMessage) (json.RawMessage, error) { return raw, nil })
	if err != nil || !bytes.Equal(raw, broadcaster) {
		t.Fatal("broadcaster was changed", string(raw), err)
	}
	items, err := s.BotAccounts(ctx, a.ID, owner)
	if err != nil || len(items) != 1 || !items[0].Bound || len(items[0].Scopes) != 2 {
		t.Fatal(items, err)
	}
	if _, err = s.BotAccounts(ctx, a.ID, other); !errors.Is(err, ErrDenied) {
		t.Fatal("unauthorized list", err)
	}
	exported, err := s.Export(ctx, owner, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(exported)
	for _, secret := range []string{"private-access", "private-refresh", "ciphertext", "key_version"} {
		if bytes.Contains(encoded, []byte(secret)) {
			t.Fatal("export leaked", secret)
		}
	}
	var ciphertext []byte
	if err = f.Admin.QueryRow(`SELECT ciphertext FROM bot_accounts WHERE tenant_id=$1 AND id=$2`, a.ID, id).Scan(&ciphertext); err != nil || bytes.Contains(ciphertext, []byte("private")) {
		t.Fatal("plaintext retained", err)
	}
	if _, err = k.open(b.ID, botPurpose(id), "v1", ciphertext); err == nil {
		t.Fatal("cross-tenant decrypt")
	}
	if _, err = k.open(a.ID, botPurpose("wrong"), "v1", ciphertext); err == nil {
		t.Fatal("cross-purpose decrypt")
	}
	if err = s.RevokeBotAccount(ctx, a.ID, owner, id); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveBoundBotAccount(ctx, k, a.ID, "channel_a", id, noRefresh); !errors.Is(err, ErrDenied) {
		t.Fatal("revoked resolved", err)
	}
	var cleared bool
	if err = f.Admin.QueryRow(`SELECT ciphertext IS NULL AND key_version IS NULL AND scopes='[]' FROM bot_accounts WHERE tenant_id=$1 AND id=$2`, a.ID, id).Scan(&cleared); err != nil || !cleared {
		t.Fatal("revocation kept secrets", err)
	}
	again, err := s.SaveValidatedBotAccount(ctx, k, a.ID, owner, syntheticBot("one"))
	if err != nil || again != id {
		t.Fatal("identity changed", err)
	}
	if _, err = s.ResolveBoundBotAccount(ctx, k, a.ID, "channel_a", id, noRefresh); !errors.Is(err, ErrDenied) {
		t.Fatal("reauth restored binding", err)
	}
	if err = s.BindBotAccount(ctx, a.ID, owner, "channel_a", id); err != nil {
		t.Fatal(err)
	}
	if err = s.RevokeConnection(ctx, a.ID, owner, "twitch"); err != nil {
		t.Fatal(err)
	}
	if err = s.BindBotAccount(ctx, a.ID, owner, "channel_a", id); !errors.Is(err, ErrDenied) {
		t.Fatal("revoked channel bound", err)
	}
	if err = s.SaveConnection(ctx, k, a.ID, owner, "twitch", "new_channel", "new", nil, broadcaster); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveBoundBotAccount(ctx, k, a.ID, "new_channel", id, noRefresh); !errors.Is(err, ErrDenied) {
		t.Fatal("new channel inherited binding", err)
	}
	if err = s.BindBotAccount(ctx, a.ID, owner, "channel_a", id); !errors.Is(err, ErrDenied) {
		t.Fatal("stale channel bound", err)
	}
	if err = s.BindBotAccount(ctx, a.ID, owner, "new_channel", id); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteTenant(ctx, owner, a.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.Admin.QueryRow(`SELECT count(*) FROM bot_accounts WHERE tenant_id=$1`, a.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("tenant left bots", count, err)
	}
}

func TestBotAccountRefreshRotationRestartAndRollback(t *testing.T) {
	s, a, _, owner, _, f := tenantPair(t)
	ctx := t.Context()
	k := botKeys(t)
	if err := s.SaveConnection(ctx, k, a.ID, owner, "twitch", "channel", "channel", nil, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	id, err := s.SaveValidatedBotAccount(ctx, k, a.ID, owner, syntheticBot("one"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BindBotAccount(ctx, a.ID, owner, "channel", id); err != nil {
		t.Fatal(err)
	}
	newKey := make([]byte, 32)
	newKey[0] = 1
	rotated, err := ParseKeyring("v2", "v1:"+base64.StdEncoding.EncodeToString(make([]byte, 32))+",v2:"+base64.StdEncoding.EncodeToString(newKey))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.ResolveBoundBotAccount(ctx, rotated, a.ID, "channel", id, func(c BotCredential) (BotCredential, error) { c.AccessToken += "x"; return c, nil })
			if e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	restarted, err := Open(f.RuntimeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	onlyNew, _ := ParseKeyring("v2", "v2:"+base64.StdEncoding.EncodeToString(newKey))
	c, err := restarted.ResolveBoundBotAccount(ctx, onlyNew, a.ID, "channel", id, noRefresh)
	if err != nil || c.AccessToken != "private-accessxxxxxxxx" {
		t.Fatal("lost refresh", c.AccessToken, err)
	}
	for _, refresh := range []func(BotCredential) (BotCredential, error){
		func(c BotCredential) (BotCredential, error) { c.UserID = "two"; return c, nil },
		func(c BotCredential) (BotCredential, error) { c.AccessToken = ""; return c, nil },
		func(c BotCredential) (BotCredential, error) { c.ExpiresAt = time.Now().Add(-time.Hour); return c, nil },
		func(c BotCredential) (BotCredential, error) { return c, errors.New("private provider response") },
	} {
		if _, err = restarted.ResolveBoundBotAccount(ctx, onlyNew, a.ID, "channel", id, refresh); err == nil || strings.Contains(err.Error(), "private") {
			t.Fatal("refresh accepted/leaked", err)
		}
	}
	after, err := restarted.ResolveBoundBotAccount(ctx, onlyNew, a.ID, "channel", id, noRefresh)
	if err != nil || after.AccessToken != c.AccessToken {
		t.Fatal("rollback failed", err)
	}
	if _, err = restarted.ResolveBoundBotAccount(ctx, k, a.ID, "channel", id, noRefresh); err == nil {
		t.Fatal("old key accepted")
	}
}

func TestBotAccountBoundedConcurrentAdmission(t *testing.T) {
	s, a, _, owner, _, _ := tenantPair(t)
	ctx := t.Context()
	k := botKeys(t)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.SaveValidatedBotAccount(ctx, k, a.ID, owner, syntheticBot(fmt.Sprint(i)))
			if err != nil && !errors.Is(err, ErrLimit) {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	bots, err := s.BotAccounts(ctx, a.ID, owner)
	if err != nil || len(bots) != 8 {
		t.Fatal("admission exceeded", len(bots), err)
	}
}

func TestBotAccountRefreshFencesRevocationAndOwnerTransfer(t *testing.T) {
	s, a, _, owner, other, _ := tenantPair(t)
	ctx := t.Context()
	k := botKeys(t)
	if err := s.SaveConnection(ctx, k, a.ID, owner, "twitch", "channel", "channel", nil, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	id, err := s.SaveValidatedBotAccount(ctx, k, a.ID, owner, syntheticBot("one"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BindBotAccount(ctx, a.ID, owner, "channel", id); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	refreshed := make(chan error, 1)
	go func() {
		_, e := s.ResolveBoundBotAccount(ctx, k, a.ID, "channel", id, func(c BotCredential) (BotCredential, error) {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return c, ctx.Err()
			}
			c.AccessToken = "rotated"
			return c, nil
		})
		refreshed <- e
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh did not start")
	}
	revoked := make(chan error, 1)
	go func() { revoked <- s.RevokeBotAccount(ctx, a.ID, owner, id) }()
	close(release)
	if err = <-refreshed; err != nil {
		t.Fatal(err)
	}
	if err = <-revoked; err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveBoundBotAccount(ctx, k, a.ID, "channel", id, noRefresh); !errors.Is(err, ErrDenied) {
		t.Fatal("refresh resurrected revoked identity", err)
	}
	if err = s.SetMember(ctx, owner, a.ID, other, "manager"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveValidatedBotAccount(ctx, k, a.ID, other, syntheticBot("two")); !errors.Is(err, ErrDenied) {
		t.Fatal("manager changed credentials", err)
	}
	if err = s.TransferOwnership(ctx, owner, a.ID, other); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveValidatedBotAccount(ctx, k, a.ID, owner, syntheticBot("two")); !errors.Is(err, ErrDenied) {
		t.Fatal("old owner changed credentials", err)
	}
	if _, err = s.SaveValidatedBotAccount(ctx, k, a.ID, other, syntheticBot("two")); err != nil {
		t.Fatal(err)
	}
}

func TestBotCredentialValidation(t *testing.T) {
	for _, mutate := range []func(*BotCredential){
		func(c *BotCredential) { c.Version = 2 }, func(c *BotCredential) { c.UserID = "" }, func(c *BotCredential) { c.Login = "bad login" },
		func(c *BotCredential) { c.AccessToken = "" }, func(c *BotCredential) { c.RefreshToken = strings.Repeat("x", 4097) },
		func(c *BotCredential) { c.Scopes = []string{"bad scope"} }, func(c *BotCredential) { c.ExpiresAt = time.Time{} },
	} {
		c := syntheticBot("one")
		mutate(&c)
		if _, err := normalizeBotCredential(c); err == nil {
			t.Fatal("invalid credential accepted")
		}
	}
}
