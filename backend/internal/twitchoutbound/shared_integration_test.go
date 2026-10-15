package twitchoutbound

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformoutbound"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
)

type sharedFixture struct {
	*fixture
	admin      *tenancy.BotAdminStore
	execution  *tenancy.BotExecutionStore
	account    tenancy.ServiceBotAccount
	catalog    tenancy.SharedBotMetadata
	delegation tenancy.SharedBotDelegation
}

func newSharedFixture(t *testing.T) *sharedFixture {
	t.Helper()
	x := &sharedFixture{fixture: newFixture(t, false)}
	var err error
	x.admin, err = tenancy.OpenBotAdminStore(t.Context(), x.f.BotAdminURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = x.admin.Close() })
	x.execution, err = tenancy.OpenBotExecutionStore(t.Context(), x.f.BotExecutorURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = x.execution.Close() })
	x.account, err = x.admin.SaveValidated(t.Context(), x.keys, tenancy.BotCredential{Version: 1, UserID: "999", Login: "shared", AccessToken: "shared-access", RefreshToken: "shared-refresh", ExpiresAt: time.Now().Add(time.Hour), Scopes: []string{"user:write:chat", "user:bot"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	x.catalog, err = x.admin.PublishMetadata(t.Context(), x.account.ID, 1, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	x.delegation, err = x.store.GrantSharedBot(t.Context(), x.target.TenantID, owner, "456", x.account.ID, 1, x.catalog.Revision, 0, []platform.Capability{"chat.send", "chat.reply"})
	if err != nil {
		t.Fatal(err)
	}
	x.adapter, err = NewShared(t.Context(), x.store, x.keys, Config{ClientID: "client", ClientSecret: "client-secret", Client: &http.Client{Transport: x.p}}, x.execution)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func TestSharedIdentityDeliveryAndRestart(t *testing.T) {
	x := newSharedFixture(t)
	result, err := execute(t, x.fixture, x.adapter.Authorize)
	if err != nil || !result.Done || result.Status != platform.Delivered || x.p.sends != 1 || x.p.sentSender != "999" || x.p.sentChannel != "456" || x.p.roles != 2 || x.p.validations != 4 {
		t.Fatal("shared delivery", result, err, x.p.sends, x.p.roles, x.p.validations)
	}
	if _, err = execute(t, x.fixture, x.adapter.Authorize); err != nil || x.p.sends != 1 {
		t.Fatal("shared replay", err)
	}
	var receipts int
	if err = x.f.Admin.QueryRow(`SELECT count(*) FROM service_bot_execution_receipts`).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatal("missing bounded receipt", receipts, err)
	}
}

func TestSharedPostClaimChangesNeverSend(t *testing.T) {
	for _, mode := range []string{"consent-revoke", "unpublish", "republish", "service-revoke", "reauthorize", "broadcaster-reauthorize", "own-selection", "capability", "role", "scope", "plan"} {
		t.Run(mode, func(t *testing.T) {
			x := newSharedFixture(t)
			calls := 0
			authorize := func(ctx context.Context, target platformoutbound.Target) (platformoutbound.Authorization, error) {
				calls++
				if calls == 2 {
					var err error
					switch mode {
					case "consent-revoke":
						_, err = x.store.RevokeSharedBot(ctx, target.TenantID, owner, x.delegation.Revision)
					case "unpublish":
						_, err = x.admin.PublishMetadata(ctx, x.account.ID, 1, x.catalog.Revision, false)
					case "republish":
						var c tenancy.SharedBotMetadata
						c, err = x.admin.PublishMetadata(ctx, x.account.ID, 1, x.catalog.Revision, false)
						if err == nil {
							_, err = x.admin.PublishMetadata(ctx, x.account.ID, 1, c.Revision, true)
						}
					case "service-revoke":
						_, err = x.admin.Revoke(ctx, x.account.ID, 1)
					case "reauthorize":
						_, err = x.admin.SaveValidated(ctx, x.keys, tenancy.BotCredential{Version: 1, UserID: "999", Login: "shared", AccessToken: "shared-new", RefreshToken: "shared-refresh", ExpiresAt: time.Now().Add(time.Hour), Scopes: []string{"user:write:chat", "user:bot"}}, 1)
					case "broadcaster-reauthorize":
						_, err = x.f.Admin.Exec(`UPDATE platform_connections SET granted_at=clock_timestamp()`)
					case "own-selection":
						var id string
						id, err = x.store.SaveValidatedBotAccount(ctx, x.keys, target.TenantID, owner, tenancy.BotCredential{Version: 1, UserID: "123", Login: "own", AccessToken: "bot-access", RefreshToken: "bot-refresh", ExpiresAt: time.Now().Add(time.Hour), Scopes: []string{"user:write:chat"}})
						if err == nil {
							err = x.store.BindBotAccount(ctx, target.TenantID, owner, "456", id)
						}
					case "capability":
						_, err = x.store.GrantSharedBot(ctx, target.TenantID, owner, "456", x.account.ID, 1, x.catalog.Revision, x.delegation.Revision, []platform.Capability{"chat.send"})
					case "role":
						x.p.moderator = false
					case "scope":
						x.p.scopes["999"] = []string{"user:write:chat"}
					case "plan":
						_, err = x.f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{httpCallsPerHour}','0') WHERE id='unlimited'`)
					}
					if err != nil {
						t.Error(err)
					}
				}
				return x.adapter.Authorize(ctx, target)
			}
			result, err := execute(t, x.fixture, authorize)
			if !result.Done || x.p.sends != 0 || state(t, x.fixture) != "rejected" || err != nil && strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("stale shared authorization sent", result, err, x.p.sends)
			}
			_, _ = execute(t, x.fixture, authorize)
			if x.p.sends != 0 {
				t.Fatal("shared replay after rejection")
			}
		})
	}
}

func TestSharedSendClosureRechecksIndependentRevocation(t *testing.T) {
	for _, mode := range []string{"consent", "publication", "grant"} {
		t.Run(mode, func(t *testing.T) {
			x := newSharedFixture(t)
			auth, err := x.adapter.Authorize(t.Context(), x.target)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "consent":
				_, err = x.store.RevokeSharedBot(t.Context(), x.target.TenantID, owner, x.delegation.Revision)
			case "publication":
				_, err = x.admin.PublishMetadata(t.Context(), x.account.ID, 1, x.catalog.Revision, false)
			case "grant":
				_, err = x.admin.Revoke(t.Context(), x.account.ID, 1)
			}
			if err != nil {
				t.Fatal(err)
			}
			r, err := auth.Send(t.Context(), platform.OutboundMessage{ActionID: x.target.ActionID, ChannelID: "456", Text: "hello", ReplyToID: "source"})
			if err == nil || r.Status != platform.Rejected || x.p.sends != 0 {
				t.Fatal("stale shared closure sent", r, err)
			}
		})
	}
}

func TestSharedRefreshAndDatabaseIdentity(t *testing.T) {
	x := newSharedFixture(t)
	// Reauthorization invalidates publication and consent. Renew both explicitly
	// before testing refresh through the execution-only pool.
	updated, err := x.admin.SaveValidated(t.Context(), x.keys, tenancy.BotCredential{Version: 1, UserID: "999", Login: "shared", AccessToken: "shared-old", RefreshToken: "shared-refresh", ExpiresAt: time.Now().Add(time.Second), Scopes: []string{"user:write:chat", "user:bot"}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := x.admin.Catalog(t.Context())
	if err != nil || len(metadata) != 1 {
		t.Fatal(err)
	}
	catalog, err := x.admin.PublishMetadata(t.Context(), x.account.ID, updated.Revision, metadata[0].Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = x.store.GrantSharedBot(t.Context(), x.target.TenantID, owner, "456", x.account.ID, updated.Revision, catalog.Revision, x.delegation.Revision, []platform.Capability{"chat.reply"}); err != nil {
		t.Fatal(err)
	}
	result, err := execute(t, x.fixture, x.adapter.Authorize)
	if err != nil || result.Status != platform.Delivered || x.p.refreshes != 1 || x.p.sends != 1 {
		t.Fatal("shared refresh delivery", result, err, x.p.refreshes)
	}
	other := testpostgres.NewTenantFixture(t, "")
	foreign, err := tenancy.OpenBotExecutionStore(t.Context(), other.BotExecutorURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = foreign.Close() }()
	if _, err = NewShared(t.Context(), x.store, x.keys, Config{ClientID: "client", ClientSecret: "client-secret"}, foreign); err == nil {
		t.Fatal("foreign service vault accepted")
	}
	if _, err = NewShared(t.Context(), x.store, x.keys, Config{ClientID: "client", ClientSecret: "client-secret"}, nil); !errors.Is(err, errUnavailable) {
		t.Fatal("missing service pool accepted", err)
	}
}
