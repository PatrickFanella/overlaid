package tenancy

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/dashboardconfig"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/history"
	"git.subcult.tv/PatrickFanella/over-laid/internal/interactions"
	"git.subcult.tv/PatrickFanella/over-laid/internal/sessions"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
)

func tenantPair(t *testing.T) (*Store, Tenant, Tenant, string, string, testpostgres.TenantFixture) {
	t.Helper()
	f := testpostgres.NewTenantFixture(t, `INSERT INTO bot_commands(id,name,response,created_at) VALUES('legacy-command','legacy','Old response','2026-01-01');`)
	s, err := Open(f.RuntimeURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := t.Context()
	a, err := s.AccountForIdentity(ctx, "twitch", "one", "One")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.AccountForIdentity(ctx, "twitch", "two", "Two")
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.CreateTenant(ctx, a, "First", "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateTenant(ctx, b, "Second", "second")
	if err != nil {
		t.Fatal(err)
	}
	return s, first, second, a, b, f
}

func TestTenantStoresMigrationAndIsolation(t *testing.T) {
	s, a, b, _, _, f := tenantPair(t)
	ctx := t.Context()
	f.Migrate()
	var tenant, response string
	var at time.Time
	if err := f.Admin.QueryRow(`SELECT tenant_id,response,created_at FROM bot_commands WHERE id='legacy-command'`).Scan(&tenant, &response, &at); err != nil || tenant != tenantdb.BootstrapID || response != "Old response" || at.UTC().Year() != 2026 {
		t.Fatalf("migration lost row: %s %s %s %v", tenant, response, at, err)
	}
	dbs := []*tenantdb.DB{}
	stores := []*bot.Store{}
	histories := []*history.PostgresStore{}
	reports := []*sessions.Store{}
	for _, tenant := range []Tenant{a, b} {
		db, err := s.Product.Scoped(tenant.ID)
		if err != nil {
			t.Fatal(err)
		}
		dbs = append(dbs, db)
		store, err := bot.NewTenant(db)
		if err != nil {
			t.Fatal(err)
		}
		stores = append(stores, store)
		config, err := dashboardconfig.NewTenant(db)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = config.Replace(ctx, json.RawMessage(`{"display":{"chatMaxVisible":2}}`)); err != nil {
			t.Fatal(err)
		}
		if _, err = store.SaveCommand(ctx, bot.Command{Name: "same", Enabled: true, Response: tenant.Name, Action: "ticker", RoleRequired: "viewer"}); err != nil {
			t.Fatal(err)
		}
		h, err := history.NewTenant(db)
		if err != nil {
			t.Fatal(err)
		}
		histories = append(histories, h)
		session, err := sessions.NewTenant(db)
		if err != nil {
			t.Fatal(err)
		}
		reports = append(reports, session)
		rules, err := interactions.NewTenant(db)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = rules.Replace(interactions.Rules{ChatAliases: []interactions.ChatAliasRule{{Alias: "hello", Command: "alert", Enabled: true}}}); err != nil {
			t.Fatal(err)
		}
	}
	commands, err := stores[0].ListCommands(ctx)
	if err != nil || len(commands) != 1 || commands[0].Response != "First" {
		t.Fatalf("first commands %#v %v", commands, err)
	}
	if _, err = stores[1].SetCommandEnabled(ctx, commands[0].ID, false); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("guessed update err=%v", err)
	}
	if err = stores[1].DeleteCommand(ctx, commands[0].ID); err == nil {
		t.Fatal("guessed delete accepted")
	}
	start, err := reports[0].Start(ctx, "A", "rehearsal", "")
	if err != nil {
		t.Fatal(err)
	}
	histories[0].SetActiveSessionID(start.ID)
	event := events.NewAlert("test", "A", "Only A", events.SeverityInfo)
	if err = histories[0].Append(event); err != nil {
		t.Fatal(err)
	}
	if got := histories[1].List(10); len(got) != 0 {
		t.Fatalf("history leaked %#v", got)
	}
	if _, err = reports[1].Report(ctx, start.ID, 10); err == nil {
		t.Fatal("guessed report leaked")
	}
	if err = reports[1].AddReadinessSnapshot(ctx, start.ID, "start", "ok", []any{}); err == nil {
		t.Fatal("cross-tenant foreign key accepted")
	}
	// Alternate scopes on shared pooled connections; context must not leak.
	var work sync.WaitGroup
	for i := 0; i < 20; i++ {
		work.Add(1)
		go func(i int) {
			defer work.Done()
			scope := dbs[i%2]
			var n int
			if err := scope.QueryRowContext(context.Background(), `SELECT count(*) FROM bot_commands WHERE /*tenant:bot_commands*/`).Scan(&n); err != nil || n != 1 {
				t.Errorf("pooled context n=%d err=%v", n, err)
			}
		}(i)
	}
	work.Wait()
	rawRuntime, err := sql.Open("pgx", f.RuntimeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer rawRuntime.Close()
	var unscoped int
	if err = rawRuntime.QueryRow(`SELECT count(*) FROM bot_commands`).Scan(&unscoped); err != nil || unscoped != 0 {
		t.Fatalf("missing context n=%d err=%v", unscoped, err)
	}
}

func TestSessionsMembershipAndVault(t *testing.T) {
	s, a, b, owner, other, _ := tenantPair(t)
	ctx := t.Context()
	token, csrf, err := s.NewSession(ctx, owner, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.Session(ctx, token)
	if err != nil || session.CSRFHash != Hash(csrf) {
		t.Fatal("session missing")
	}
	if err = s.SelectTenant(ctx, token, owner, b.ID); err == nil {
		t.Fatal("selected foreign tenant")
	}
	if err = s.SelectTenant(ctx, token, owner, a.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.SetMember(ctx, owner, a.ID, other, "editor"); err != nil {
		t.Fatal(err)
	}
	if err = s.SetMember(ctx, other, a.ID, owner, "moderator"); err == nil {
		t.Fatal("editor escalated")
	}
	if err = s.SetMember(ctx, owner, a.ID, owner, ""); err == nil {
		t.Fatal("removed owner")
	}
	if err = s.TransferOwnership(ctx, owner, a.ID, other); err != nil {
		t.Fatal(err)
	}
	if err = s.SetMember(ctx, owner, a.ID, other, ""); err == nil {
		t.Fatal("old owner removed new owner")
	}
	keys, err := ParseKeyring("v1", "v1:"+base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(`{"access_token":"PRIVATE_ACCESS","refresh_token":"PRIVATE_REFRESH"}`)
	if err = s.SaveConnection(ctx, keys, b.ID, other, "twitch", "two", "Two", []string{"user:write:chat"}, raw); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveConnection(ctx, keys, a.ID, "twitch", func(raw json.RawMessage) (json.RawMessage, error) { return raw, nil }); err == nil {
		t.Fatal("foreign vault accessed")
	}
	resolved, err := s.ResolveConnection(ctx, keys, b.ID, "twitch", func(raw json.RawMessage) (json.RawMessage, error) { return raw, nil })
	if err != nil || string(resolved) != string(raw) {
		t.Fatal("vault round trip failed", err)
	}
	overlay, err := s.RotateCredential(ctx, b.ID, other, "overlay")
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.CredentialTenant(ctx, overlay, "overlay")
	if err != nil || id != b.ID {
		t.Fatalf("credential lookup %s %v", id, err)
	}
	if _, err = s.RotateCredential(ctx, b.ID, other, "overlay"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CredentialTenant(ctx, overlay, "overlay"); err == nil {
		t.Fatal("old overlay accepted")
	}
	if err = s.RevokeConnection(ctx, b.ID, other, "twitch"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveConnection(ctx, keys, b.ID, "twitch", func(raw json.RawMessage) (json.RawMessage, error) { return raw, nil }); err == nil {
		t.Fatal("revoked vault accessed")
	}
	if err = s.RevokeSession(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session(ctx, token); err == nil {
		t.Fatal("revoked session accepted")
	}
}
