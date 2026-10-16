package twitchoutbound

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/moderationpacing"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformmoderation"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
)

func moderationFixture(t *testing.T, mode string, cap platform.Capability) (*fixture, *sharedFixture, platformmoderation.Target) {
	t.Helper()
	var x *fixture
	var shared *sharedFixture
	if mode == "shared" {
		shared = newSharedFixture(t)
		x = shared.fixture
	} else {
		x = newFixture(t, mode == "own")
	}
	for _, id := range []string{"456", "123", "999"} {
		x.p.scopes[id] = append(x.p.scopes[id], "moderator:manage:chat_messages", "moderator:manage:banned_users")
	}
	if shared != nil {
		var err error
		shared.delegation, err = x.store.GrantSharedBot(t.Context(), x.target.TenantID, owner, "456", shared.account.ID, shared.account.Revision, shared.catalog.Revision, shared.delegation.Revision, []platform.Capability{"chat.send", "chat.reply", cap})
		if err != nil {
			t.Fatal(err)
		}
	}
	a := platform.ModerationAction{ActionID: strings.Repeat("c", 64), ChannelID: "456", UserID: "789", Capability: cap}
	if cap == "message.delete" {
		a.UserID = ""
		a.MessageID = "source"
	}
	if cap == "user.timeout" {
		a.Duration = time.Minute
		a.Reason = "PRIVATE reason"
	}
	return x, shared, platformmoderation.Target{TenantID: x.target.TenantID, ConnectionID: x.target.ConnectionID, Deadline: x.target.Deadline, Action: a}
}

func TestModerationAuthorizationSelectedIdentities(t *testing.T) {
	for _, mode := range []string{"broadcaster", "own", "shared"} {
		for _, cap := range []platform.Capability{"message.delete", "user.timeout", "user.ban", "user.unban"} {
			t.Run(mode+"/"+string(cap), func(t *testing.T) {
				x, _, target := moderationFixture(t, mode, cap)
				auth, err := x.adapter.AuthorizeModeration(t.Context(), target)
				if err != nil || auth.Attempt == nil || auth.Binding == "" {
					t.Fatal("authorization failed", err)
				}
				want := map[string]string{"broadcaster": "456", "own": "123", "shared": "999"}[mode]
				if auth.ClientID != "client" || auth.ModeratorID != want || !auth.Capabilities.Check(cap).Available || auth.Capabilities.Check("chat.send").Available {
					t.Fatal("wrong moderator identity")
				}
				r, err := auth.Attempt(t.Context(), target.Action)
				if err != nil || r.Status != platform.Delivered || r.Validate(target.Action.ActionID) != nil || x.p.moderations != 1 || x.p.sends != 0 || x.p.sentSender != want {
					t.Fatal("moderation attempt failed", r, err)
				}
				wantValidations := 2
				if mode != "broadcaster" {
					wantValidations = 4
					if x.p.roles != 2 {
						t.Fatal("missing fresh role lookups")
					}
				}
				if x.p.validations != wantValidations {
					t.Fatal("credential validation was cached")
				}
			})
		}
	}
}

func TestModerationClosureRejectsChangedAuthorization(t *testing.T) {
	for _, mode := range []string{"revoke", "binding-cycle", "scope", "role", "plan", "client", "subject", "transport", "redirect", "malformed", "oversize", "payload"} {
		t.Run(mode, func(t *testing.T) {
			x, _, target := moderationFixture(t, "own", "user.timeout")
			auth, err := x.adapter.AuthorizeModeration(t.Context(), target)
			if err != nil {
				t.Fatal(err)
			}
			input := target.Action
			switch mode {
			case "revoke":
				err = x.store.RevokeBotAccount(t.Context(), target.TenantID, owner, x.botID)
			case "binding-cycle":
				_, err = x.f.Admin.Exec(`UPDATE platform_connections SET bot_account_id=NULL`)
				if err == nil {
					_, err = x.f.Admin.Exec(`UPDATE platform_connections SET bot_account_id=$1`, x.botID)
				}
			case "scope":
				x.p.scopes["123"] = []string{"user:write:chat"}
			case "role":
				x.p.moderator = false
			case "plan":
				_, err = x.f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{httpCallsPerHour}','0') WHERE id='unlimited'`)
			case "client":
				x.p.mode = "wrong-client"
			case "subject":
				x.p.mode = "wrong-subject"
			case "payload":
				input.Duration = 2 * time.Minute
			default:
				x.p.mode = mode
			}
			if err != nil {
				t.Fatal(err)
			}
			r, err := auth.Attempt(t.Context(), input)
			if err == nil || r.Status != platform.Rejected || x.p.moderations != 0 || x.p.sends != 0 || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("changed authorization reached provider", r, err)
			}
			wantFailure := platform.Unauthorized
			if mode == "transport" || mode == "redirect" || mode == "malformed" || mode == "oversize" {
				wantFailure = platform.Retryable
			}
			if r.Failure != wantFailure || r.Validate(input.ActionID) != nil {
				t.Fatal("incorrect pre-effect failure metadata", r)
			}
		})
	}
}

func TestModerationSharedConsentAndPublication(t *testing.T) {
	for _, mode := range []string{"consent", "publication", "scope", "role", "pool", "reauthorize"} {
		t.Run(mode, func(t *testing.T) {
			x, shared, target := moderationFixture(t, "shared", "user.ban")
			auth, err := x.adapter.AuthorizeModeration(t.Context(), target)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "consent":
				_, err = x.store.GrantSharedBot(t.Context(), target.TenantID, owner, "456", shared.account.ID, shared.account.Revision, shared.catalog.Revision, shared.delegation.Revision, []platform.Capability{"chat.reply"})
			case "publication":
				_, err = shared.admin.PublishMetadata(t.Context(), shared.account.ID, shared.account.Revision, shared.catalog.Revision, false)
			case "scope":
				x.p.scopes["999"] = []string{"user:write:chat", "user:bot"}
			case "role":
				x.p.moderator = false
			case "pool":
				x.adapter.shared = nil
			case "reauthorize":
				_, err = x.f.Admin.Exec(`UPDATE platform_connections SET granted_at=clock_timestamp()`)
			}
			if err != nil {
				t.Fatal(err)
			}
			r, err := auth.Attempt(t.Context(), target.Action)
			if err == nil || r.Status != platform.Rejected || x.p.moderations != 0 || x.p.sends != 0 {
				t.Fatal("shared selection fell back or survived revocation", r, err)
			}
		})
	}
}

func TestModerationScopePayloadAndCancellation(t *testing.T) {
	x, _, target := moderationFixture(t, "own", "message.delete")
	// Permission to ban is not permission to delete messages.
	x.p.scopes["123"] = []string{"moderator:manage:banned_users"}
	if _, err := x.adapter.AuthorizeModeration(t.Context(), target); !errors.Is(err, platformmoderation.ErrDenied) {
		t.Fatal("wrong scope accepted", err)
	}
	x.p.scopes["123"] = []string{"moderator:manage:chat_messages"}
	auth, err := x.adapter.AuthorizeModeration(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*platform.ModerationAction){func(a *platform.ModerationAction) { a.ActionID = strings.Repeat("b", 64) }, func(a *platform.ModerationAction) { a.ChannelID = "999" }, func(a *platform.ModerationAction) { a.MessageID = "different" }, func(a *platform.ModerationAction) { a.Reason = "PRIVATE" }, func(a *platform.ModerationAction) { a.Capability = "user.ban" }} {
		a := target.Action
		change(&a)
		if _, err := auth.Attempt(t.Context(), a); !errors.Is(err, platformmoderation.ErrDenied) {
			t.Fatal("changed effect accepted")
		}
	}
	bad := target
	bad.ConnectionID = "twitch:999"
	if _, err := x.adapter.AuthorizeModeration(t.Context(), bad); !errors.Is(err, platformmoderation.ErrDenied) {
		t.Fatal("mismatched channel accepted")
	}
	expired := target
	expired.Deadline = time.Now().Add(-time.Second)
	if _, err := x.adapter.AuthorizeModeration(t.Context(), expired); !errors.Is(err, platformmoderation.ErrDenied) {
		t.Fatal("expired action authorized")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := auth.Attempt(ctx, target.Action); err == nil || x.p.moderations != 0 {
		t.Fatal("canceled effect attempted")
	}
}

func TestModerationRefreshPreservesAuthorizedBinding(t *testing.T) {
	for _, mode := range []string{"own", "shared"} {
		t.Run(mode, func(t *testing.T) {
			x, shared, target := moderationFixture(t, mode, "user.timeout")
			auth, err := x.adapter.AuthorizeModeration(t.Context(), target)
			if err != nil {
				t.Fatal(err)
			}
			expire := func(c tenancy.BotCredential) (tenancy.BotCredential, error) {
				c.ExpiresAt = time.Now().Add(time.Second)
				return c, nil
			}
			if shared == nil {
				_, err = x.store.ResolveBoundBotAccount(t.Context(), x.keys, target.TenantID, "456", x.botID, expire)
			} else {
				_, err = shared.execution.Resolve(t.Context(), x.keys, shared.account.ID, shared.account.Revision, shared.catalog.Revision, expire)
			}
			if err != nil {
				t.Fatal(err)
			}
			r, err := auth.Attempt(t.Context(), target.Action)
			if err != nil || r.Status != platform.Delivered || x.p.moderations != 1 || x.p.refreshes != 1 {
				t.Fatal("refresh changed selection or reused expired credentials", r, err)
			}
			selection, err := x.store.ChatSenderSelection(t.Context(), target.TenantID, "456")
			if err != nil || selection.Binding != auth.Binding {
				t.Fatal("refresh changed binding", err)
			}
		})
	}
}

// Exercise the composed ledger/pacer/real authorization/transport path using
// only the fixture transport; no Twitch request or startup worker is involved.
func TestModerationExecutorSelectedTransport(t *testing.T) {
	for _, mode := range []string{"broadcaster", "own", "shared"} {
		for _, cap := range []platform.Capability{"message.delete", "user.timeout", "user.ban", "user.unban"} {
			t.Run(mode+"/"+string(cap), func(t *testing.T) {
				x, _, target := moderationFixture(t, mode, cap)
				var raw []byte
				if err := x.f.Admin.QueryRow(`SELECT envelope FROM normalized_chat_events LIMIT 1`).Scan(&raw); err != nil {
					t.Fatal(err)
				}
				var source platform.ChatEvent
				if err := json.Unmarshal(raw, &source); err != nil {
					t.Fatal(err)
				}
				input := target.Action
				input.ActionID = ""
				intent, err := platform.NewTwitchModerationIntent(source, "executor", input)
				if err != nil {
					t.Fatal(err)
				}
				ledger, err := platformmoderation.New(x.store.Product)
				if err != nil {
					t.Fatal(err)
				}
				id, created, err := ledger.Admit(t.Context(), intent)
				if err != nil || !created {
					t.Fatal("admission", created, err)
				}
				pacer, err := moderationpacing.Open(t.Context(), x.f.ModerationPacerURL)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = pacer.Close() }()
				executor, err := platformmoderation.NewExecutor(t.Context(), x.store.Product, pacer, x.adapter.AuthorizeModeration)
				if err != nil {
					t.Fatal(err)
				}
				args := platform.ModerationArgs{TenantID: target.TenantID, ActionID: id, Deadline: intent.Deadline().Unix()}
				r, err := executor.Execute(t.Context(), args)
				want := map[string]string{"broadcaster": "456", "own": "123", "shared": "999"}[mode]
				if err != nil || !r.Done || r.Status != platform.Delivered || x.p.moderations != 1 || x.p.sends != 0 || x.p.sentSender != want {
					t.Fatal("composed attempt", r, err)
				}
				if _, err = executor.Execute(t.Context(), args); err != nil || x.p.moderations != 1 {
					t.Fatal("composed replay", err)
				}
				rows, err := ledger.Receipts(t.Context(), 10)
				if err != nil || len(rows) != 1 || rows[0].State != "delivered" {
					t.Fatal("completion", err)
				}
			})
		}
	}
}
