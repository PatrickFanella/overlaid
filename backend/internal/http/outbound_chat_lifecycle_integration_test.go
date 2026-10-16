package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
)

// The provider barrier releases credential locks before the mutation. The
// consumer's gate then holds the real River executor after its first trusted
// authorization and pacing permit, but before its durable action claim.
func TestRetainedSenderQueuedLifecycleChanges(t *testing.T) {
	for _, change := range []string{"revoke", "reauthorize", "remove-source", "own-delivery", "own-revoke", "own-reauthorize", "shared-delivery", "shared-consent", "shared-unpublish", "shared-revoke", "shared-reauthorize"} {
		t.Run(change, func(t *testing.T) {
			old, alpha, _, f := applicationFixture(t)
			controllerToken(t, old, alpha)
			cfg := retainedSenderConfig(old.config, f)
			selected := strings.HasPrefix(change, "own-") || strings.HasPrefix(change, "shared-")
			credential := tenancy.BotCredential{Version: 1, UserID: "999", Login: "selected", AccessToken: "selected-access", RefreshToken: "selected-refresh", ExpiresAt: time.Now().Add(time.Hour), Scopes: []string{"user:write:chat", "user:bot"}}
			var botID string
			var admin *tenancy.BotAdminStore
			var account tenancy.ServiceBotAccount
			var catalog tenancy.SharedBotMetadata
			var delegation tenancy.SharedBotDelegation
			if selected {
				token := twitchTokenInfo{AccessToken: "fixture-access", UserID: "123", Login: "fixture", Scope: []string{"user:read:chat", "user:write:chat", "moderation:read"}, ValidatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
				raw, err := json.Marshal(tokenDisk(&token))
				if err != nil {
					t.Fatal(err)
				}
				if err := old.store.SaveConnection(t.Context(), old.keys, alpha.tenant, alpha.id, "twitch", token.UserID, token.Login, token.Scope, raw); err != nil {
					t.Fatal(err)
				}
			}
			if strings.HasPrefix(change, "own-") {
				var err error
				botID, err = old.store.SaveValidatedBotAccount(t.Context(), old.keys, alpha.tenant, alpha.id, credential)
				if err != nil {
					t.Fatal(err)
				}
				if err := old.store.BindBotAccount(t.Context(), alpha.tenant, alpha.id, "123", botID); err != nil {
					t.Fatal(err)
				}
			}
			if strings.HasPrefix(change, "shared-") {
				var err error
				admin, err = tenancy.OpenBotAdminStore(t.Context(), f.BotAdminURL)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = admin.Close() }()
				account, err = admin.SaveValidated(t.Context(), old.keys, credential, 0)
				if err != nil {
					t.Fatal(err)
				}
				catalog, err = admin.PublishMetadata(t.Context(), account.ID, account.Revision, 0, true)
				if err != nil {
					t.Fatal(err)
				}
				delegation, err = old.store.GrantSharedBot(t.Context(), alpha.tenant, alpha.id, "123", account.ID, account.Revision, catalog.Revision, 0, []platform.Capability{"chat.send", "chat.reply"})
				if err != nil {
					t.Fatal(err)
				}
				cfg.BotExecutionDatabaseURL = f.BotExecutorURL
			}
			if err := old.Close(); err != nil {
				t.Fatal(err)
			}
			validation := make(chan struct{})
			resume := make(chan struct{})
			var released atomic.Bool
			var validations, sends, roles atomic.Int32
			previous := twitchHTTPClient
			twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				switch r.URL.Host + r.URL.Path {
				case "id.twitch.tv/oauth2/validate":
					if validations.Add(1) == 1 {
						close(validation)
						select {
						case <-resume:
						case <-r.Context().Done():
							return nil, r.Context().Err()
						}
					}
					subject := "123"
					if r.Header.Get("Authorization") == "OAuth selected-access" {
						subject = "999"
					}
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"client_id":"fixture-client","user_id":"%s","login":"fixture","scopes":["user:read:chat","user:write:chat","moderation:read","user:bot"],"expires_in":3600}`, subject)))}, nil
				case "api.twitch.tv/helix/moderation/moderators":
					roles.Add(1)
					if r.URL.Query().Get("broadcaster_id") != "123" || r.URL.Query().Get("user_id") != "999" || r.Header.Get("Authorization") != "Bearer fixture-access" {
						return nil, errors.New("unexpected moderator lookup identity")
					}
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":[{"user_id":"999"}]}`))}, nil
				case "api.twitch.tv/helix/chat/messages":
					sends.Add(1)
					if !strings.HasSuffix(change, "-delivery") {
						return nil, errors.New("lifecycle fixture forbids transport")
					}
					var body struct {
						Sender  string `json:"sender_id"`
						Channel string `json:"broadcaster_id"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Sender != "999" || body.Channel != "123" || r.Header.Get("Authorization") != "Bearer selected-access" {
						return nil, errors.New("selected sender transport mismatch")
					}
					var claimed bool
					if err := f.Admin.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM platform_outbound_actions WHERE tenant_id=$1 AND state='running')`, alpha.tenant).Scan(&claimed); err != nil || !claimed {
						return nil, errors.New("selected transport without ledger claim")
					}
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":[{"message_id":"selected-reply","is_sent":true}]}`))}, nil
				}
				return nil, errors.New("unexpected fixture request")
			})}
			defer func() {
				if !released.Load() {
					close(resume)
				}
				twitchHTTPClient = previous
			}()
			app, err := NewApplication(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = app.Close() }()
			s, err := app.runtime(t.Context(), alpha.tenant)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.commandsRegistry().SaveCommand(t.Context(), bot.Command{Name: "lifecycle", Action: "ticker", Enabled: true, RoleRequired: "viewer", ResponseMode: "chat", Response: "Private queued reply"}); err != nil {
				t.Fatal(err)
			}
			s.refreshTwitchCustomCommands(t.Context())
			if err := s.ingestRetainedChat(t.Context(), packet("123", "lifecycle-source", "lifecycle-delivery", "!lifecycle"), twitch.Token{UserID: "123"}); err != nil {
				t.Fatal(err)
			}
			select {
			case <-validation:
			case <-time.After(10 * time.Second):
				t.Fatal("outbound authorization did not start")
			}
			waitReceipt(t, app, alpha.tenant, 1)
			gateCtx, cancelGate := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancelGate()
			releaseGate, err := acquireGate(gateCtx, s.chatSendGate)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if releaseGate != nil {
					releaseGate()
				}
			}()
			close(resume)
			released.Store(true)
			deadline := time.Now().Add(5 * time.Second)
			for {
				var permitted bool
				if err := f.Admin.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM chat_pacing_accounts WHERE lease_until>clock_timestamp())`).Scan(&permitted); err != nil {
					t.Fatal(err)
				}
				if permitted {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("executor did not reach pre-claim gate")
				}
				time.Sleep(10 * time.Millisecond)
			}
			waitOutboundState(t, app, alpha.tenant, 1, "pending")
			switch change {
			case "revoke":
				err = app.store.RevokeConnection(t.Context(), alpha.tenant, alpha.id, "twitch")
			case "reauthorize":
				// Same subject and scopes still produce a new sender binding.
				controllerToken(t, app, alpha)
			case "remove-source":
				db, _ := app.store.Product.Scoped(alpha.tenant)
				_, err = db.ExecContext(t.Context(), `DELETE FROM normalized_chat_events WHERE /*tenant:normalized_chat_events*/`)
			case "own-revoke":
				err = app.store.RevokeBotAccount(t.Context(), alpha.tenant, alpha.id, botID)
			case "own-reauthorize":
				_, err = app.store.SaveValidatedBotAccount(t.Context(), app.keys, alpha.tenant, alpha.id, credential)
			case "shared-consent":
				_, err = app.store.RevokeSharedBot(t.Context(), alpha.tenant, alpha.id, delegation.Revision)
			case "shared-unpublish":
				_, err = admin.PublishMetadata(t.Context(), account.ID, account.Revision, catalog.Revision, false)
			case "shared-revoke":
				_, err = admin.Revoke(t.Context(), account.ID, account.Revision)
			case "shared-reauthorize":
				_, err = admin.SaveValidated(t.Context(), app.keys, credential, account.Revision)
			}
			if err != nil {
				t.Fatal(err)
			}
			releaseGate()
			releaseGate = nil
			if change == "remove-source" {
				waitOutboundState(t, app, alpha.tenant, 0, "pending")
			} else if strings.HasSuffix(change, "-delivery") {
				waitOutboundState(t, app, alpha.tenant, 1, "delivered")
			} else {
				waitOutboundState(t, app, alpha.tenant, 1, "rejected")
			}
			deadline = time.Now().Add(5 * time.Second)
			for {
				var permits, attempts int
				if err := f.Admin.QueryRowContext(t.Context(), `SELECT count(*) FILTER (WHERE lease_until IS NOT NULL),coalesce(sum(jsonb_array_length(attempts)),0) FROM chat_pacing_accounts`).Scan(&permits, &attempts); err != nil {
					t.Fatal(err)
				}
				if permits == 0 {
					if attempts != 1 {
						t.Fatal("lifecycle change refunded pacing charge", attempts)
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("executor did not release its pacing permit")
				}
				time.Sleep(10 * time.Millisecond)
			}
			// Close waits for both real clients before the final no-effect proof.
			if err := app.Close(); err != nil {
				t.Fatal(err)
			}
			wantSends := int32(0)
			if strings.HasSuffix(change, "-delivery") {
				wantSends = 1
			}
			if sends.Load() != wantSends {
				t.Fatal("changed lifecycle reached provider transport", sends.Load())
			}
			if selected && roles.Load() < 1 || wantSends == 1 && (roles.Load() != 2 || validations.Load() != 4) {
				t.Fatal("selected identity missed fresh authorization", roles.Load(), validations.Load())
			}
			var echoes int32
			if err := f.Admin.QueryRow(`SELECT count(*) FROM twitch_sent_messages WHERE tenant_id=$1 AND expires_at>now()`, alpha.tenant).Scan(&echoes); err != nil || echoes != wantSends {
				t.Fatal("delivery/echo bookkeeping mismatch", echoes, wantSends, err)
			}
			var count int
			if err := f.Admin.QueryRow(`SELECT count(*) FROM platform_outbound_actions WHERE tenant_id=$1`, alpha.tenant).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if change == "remove-source" && count != 0 || change != "remove-source" && count != 1 {
				t.Fatal("lifecycle action retention mismatch", count)
			}
			if change != "remove-source" {
				var started bool
				var code, failure string
				if err := f.Admin.QueryRow(`SELECT started_at IS NOT NULL,code,failure FROM platform_outbound_actions WHERE tenant_id=$1`, alpha.tenant).Scan(&started, &code, &failure); err != nil {
					t.Fatal(err)
				}
				if change == "revoke" && (started || code != "connection_unavailable" || failure != "permanent") {
					t.Fatal("revocation did not reject before claim", started, code, failure)
				}
				if change == "reauthorize" && (!started || code != "rejected" || failure != "unauthorized" || validations.Load() < 2) {
					t.Fatal("reauthorization did not reject changed post-claim binding", started, code, failure, validations.Load())
				}
				if selected && wantSends == 0 && (!started || code != "rejected" || failure != "unauthorized") {
					t.Fatal("selected lifecycle did not reject after claim", started, code, failure)
				}
			}
		})
	}
}

func TestRetainedSenderGeneralRegistryPauseAndCooldown(t *testing.T) {
	old, alpha, _, f := applicationFixture(t)
	controllerToken(t, old, alpha)
	s, err := old.runtime(t.Context(), alpha.tenant)
	if err != nil {
		t.Fatal(err)
	}
	command, err := s.commandsRegistry().SaveCommand(t.Context(), bot.Command{Name: "guarded", Action: "ticker", Enabled: true, RoleRequired: "viewer", ResponseMode: "chat", Response: "One allowed reply", CooldownSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	// Pause after input retention, before a handler starts. Already-admitted
	// replies are snapshots and this test does not claim pause retracts them.
	if err := s.ingestRetainedChat(t.Context(), packet("123", "paused-source", "paused-delivery", "!guarded"), twitch.Token{UserID: "123"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.commandsRegistry().SetCommandEnabled(t.Context(), command.ID, false); err != nil {
		t.Fatal(err)
	}
	cfg := retainedSenderConfig(old.config, f)
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	previous := twitchHTTPClient
	var sends atomic.Int32
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host + r.URL.Path {
		case "id.twitch.tv/oauth2/validate":
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"client_id":"fixture-client","user_id":"123","login":"fixture","scopes":["user:read:chat","user:write:chat"],"expires_in":3600}`))}, nil
		case "api.twitch.tv/helix/chat/messages":
			sends.Add(1)
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":[{"message_id":"guarded-reply","is_sent":true}]}`))}, nil
		}
		return nil, errors.New("unexpected fixture request")
	})}
	defer func() { twitchHTTPClient = previous }()
	app, err := NewApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Close() }()
	waitReceipt(t, app, alpha.tenant, 1)
	waitOutboundState(t, app, alpha.tenant, 0, "pending")
	s, err = app.runtime(t.Context(), alpha.tenant)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.commandsRegistry().SetCommandEnabled(t.Context(), command.ID, true); err != nil {
		t.Fatal(err)
	}
	s.refreshTwitchCustomCommands(t.Context())
	if err := s.ingestRetainedChat(t.Context(), packet("123", "enabled-source", "enabled-delivery", "!guarded"), twitch.Token{UserID: "123"}); err != nil {
		t.Fatal(err)
	}
	waitReceipt(t, app, alpha.tenant, 2)
	waitOutboundState(t, app, alpha.tenant, 1, "delivered")
	if err := s.ingestRetainedChat(t.Context(), packet("123", "cooldown-source", "cooldown-delivery", "!guarded"), twitch.Token{UserID: "123"}); err != nil {
		t.Fatal(err)
	}
	waitReceipt(t, app, alpha.tenant, 3)
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	var actions int
	if err := f.Admin.QueryRow(`SELECT count(*) FROM platform_outbound_actions WHERE tenant_id=$1`, alpha.tenant).Scan(&actions); err != nil || actions != 1 || sends.Load() != 1 {
		t.Fatal("general registry pause/cooldown admitted extra replies", actions, sends.Load(), err)
	}
}
