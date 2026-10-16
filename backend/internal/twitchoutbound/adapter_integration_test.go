package twitchoutbound

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/chatpacing"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformingest"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformoutbound"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
)

const owner = "00000000-0000-4000-8000-000000000002"

type provider struct {
	mu                                   sync.Mutex
	mode                                 string
	moderator                            bool
	sends, validations, refreshes, roles int
	moderations                          int
	scopes                               map[string][]string
	sentSender, sentChannel              string
}

func response(status int, raw string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(raw))}
}
func (p *provider) RoundTrip(r *http.Request) (*http.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.mode == "cancel" {
		<-r.Context().Done()
		return nil, r.Context().Err()
	}
	if p.mode == "transport" {
		return nil, errors.New("PRIVATE token transport")
	}
	if p.mode == "redirect" {
		out := response(302, "")
		out.Header.Set("Location", "https://unexpected.invalid/PRIVATE")
		return out, nil
	}
	if p.mode == "oversize" {
		return response(200, strings.Repeat("x", 32769)), nil
	}
	if p.mode == "malformed" {
		return response(200, `{"PRIVATE":`), nil
	}
	switch r.URL.Host + r.URL.Path {
	case "id.twitch.tv/oauth2/validate":
		p.validations++
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "OAuth ")
		subject := "456"
		if strings.HasPrefix(token, "shared") {
			subject = "999"
		}
		if strings.HasPrefix(token, "bot") {
			subject = "123"
		}
		if token == "expired" {
			return response(401, `{"message":"PRIVATE"}`), nil
		}
		client := "client"
		if p.mode == "wrong-client" {
			client = "wrong"
		}
		if p.mode == "wrong-subject" {
			subject = "999"
		}
		scopes := p.scopes[subject]
		if p.mode == "missing-scope" {
			scopes = []string{}
		}
		raw, _ := json.Marshal(map[string]any{"client_id": client, "user_id": subject, "login": "user_" + subject, "scopes": scopes, "expires_in": 3600})
		return response(200, string(raw)), nil
	case "id.twitch.tv/oauth2/token":
		p.refreshes++
		if err := r.ParseForm(); err != nil {
			return nil, err
		}
		if r.Form.Get("client_secret") != "client-secret" || r.Form.Get("grant_type") != "refresh_token" {
			return response(400, `{"message":"PRIVATE"}`), nil
		}
		access := "host-new"
		if strings.HasPrefix(r.Form.Get("refresh_token"), "shared") {
			access = "shared-new"
		}
		if strings.HasPrefix(r.Form.Get("refresh_token"), "bot") {
			access = "bot-new"
		}
		raw, _ := json.Marshal(map[string]any{"access_token": access, "refresh_token": r.Form.Get("refresh_token") + "x", "token_type": "bearer"})
		return response(200, string(raw)), nil
	case "api.twitch.tv/helix/moderation/moderators":
		p.roles++
		if r.URL.Query().Get("broadcaster_id") != "456" || (r.URL.Query().Get("user_id") != "123" && r.URL.Query().Get("user_id") != "999") || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer host") {
			return response(403, `{"message":"PRIVATE wrong role scope"}`), nil
		}
		if !p.moderator {
			return response(200, `{"data":[]}`), nil
		}
		return response(200, `{"data":[{"user_id":"`+r.URL.Query().Get("user_id")+`"}]}`), nil
	case "api.twitch.tv/helix/chat/messages":
		p.sends++
		var body struct {
			SenderID  string `json:"sender_id"`
			ChannelID string `json:"broadcaster_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return nil, err
		}
		p.sentSender = body.SenderID
		p.sentChannel = body.ChannelID
		return response(200, `{"data":[{"message_id":"sent-fixture","is_sent":true}]}`), nil
	case "api.twitch.tv/helix/moderation/chat", "api.twitch.tv/helix/moderation/bans":
		p.moderations++
		p.sentSender, p.sentChannel = r.URL.Query().Get("moderator_id"), r.URL.Query().Get("broadcaster_id")
		prefix := map[string]string{"456": "host", "123": "bot", "999": "shared"}[p.sentSender]
		if prefix == "" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer "+prefix) || p.sentChannel != "456" {
			return response(401, `{"message":"PRIVATE wrong credential"}`), nil
		}
		if r.Method == http.MethodDelete {
			return response(204, ""), nil
		}
		var body struct {
			Data struct {
				UserID string `json:"user_id"`
			} `json:"data"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			return response(400, ""), nil
		}
		raw, _ := json.Marshal(map[string]any{"data": []any{map[string]string{"broadcaster_id": p.sentChannel, "moderator_id": p.sentSender, "user_id": body.Data.UserID}}})
		return response(200, string(raw)), nil
	default:
		return nil, errors.New("unexpected provider request")
	}
}

type fixture struct {
	f       testpostgres.TenantFixture
	store   *tenancy.Store
	keys    tenancy.Keyring
	pacer   *chatpacing.Store
	adapter *Adapter
	p       *provider
	args    platform.ChatSendArgs
	target  platformoutbound.Target
	botID   string
}

func newFixture(t *testing.T, own bool) *fixture {
	t.Helper()
	x := &fixture{f: testpostgres.NewTenantFixture(t, "")}
	var err error
	x.store, err = tenancy.Open(x.f.RuntimeURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = x.store.Close() })
	x.keys, err = tenancy.ParseKeyring("v1", "v1:"+base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	disk := broadcasterDisk{UserID: "456", Login: "host", AccessToken: "host-access", RefreshToken: "host-refresh", ExpiresAt: now.Add(time.Hour), Scope: []string{"user:write:chat", "moderation:read"}}
	raw, _ := json.Marshal(disk)
	if err = x.store.SaveConnection(t.Context(), x.keys, tenantdb.BootstrapID, owner, "twitch", "456", "host", disk.Scope, raw); err != nil {
		t.Fatal(err)
	}
	if own {
		x.botID, err = x.store.SaveValidatedBotAccount(t.Context(), x.keys, tenantdb.BootstrapID, owner, tenancy.BotCredential{Version: 1, UserID: "123", Login: "bot", AccessToken: "bot-access", RefreshToken: "bot-refresh", ExpiresAt: now.Add(time.Hour), Scopes: []string{"user:write:chat"}})
		if err != nil {
			t.Fatal(err)
		}
		if err = x.store.BindBotAccount(t.Context(), tenantdb.BootstrapID, owner, "456", x.botID); err != nil {
			t.Fatal(err)
		}
	}
	x.pacer, err = chatpacing.Open(t.Context(), x.f.PacerURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = x.pacer.Close() })
	x.p = &provider{moderator: true, scopes: map[string][]string{"456": {"user:write:chat", "moderation:read"}, "123": {"user:write:chat"}, "999": {"user:write:chat", "user:bot"}}}
	x.adapter, err = New(x.store, x.keys, Config{ClientID: "client", ClientSecret: "client-secret", Client: &http.Client{Transport: x.p}})
	if err != nil {
		t.Fatal(err)
	}
	event, err := platform.NewChatEvent(platform.Connection{TenantID: tenantdb.BootstrapID, ID: "twitch:456", Platform: "twitch"}, "auth-source", now, now, platform.ChatMessage{MessageID: "source", Text: "chat", Actor: platform.Identity{ID: "789"}, Channel: platform.Identity{ID: "456"}})
	if err != nil {
		t.Fatal(err)
	}
	ingest, _ := platformingest.New(x.store.Product)
	if _, err = ingest.IngestChat(t.Context(), event, nil); err != nil {
		t.Fatal(err)
	}
	ledger, _ := platformoutbound.New(x.store.Product)
	id, _, err := ledger.Admit(t.Context(), platformoutbound.Request{EventID: event.ID, ConnectionID: event.ConnectionID, Key: "response", Text: "hello", ReplyToID: "source"})
	if err != nil {
		t.Fatal(err)
	}
	var deadline time.Time
	if err = x.store.Product.QueryRowContext(t.Context(), `SELECT deadline FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/ AND id=$1`, id).Scan(&deadline); err != nil {
		t.Fatal(err)
	}
	x.args = platform.ChatSendArgs{TenantID: tenantdb.BootstrapID, ActionID: id, Deadline: deadline.Unix()}
	x.target = platformoutbound.Target{TenantID: tenantdb.BootstrapID, ActionID: id, ConnectionID: event.ConnectionID, ChannelID: "456", Capability: "chat.reply", Deadline: deadline}
	return x
}
func execute(t *testing.T, x *fixture, authorize platformoutbound.Authorize) (platformoutbound.Execution, error) {
	t.Helper()
	e, err := platformoutbound.NewExecutor(t.Context(), x.store.Product, x.pacer, authorize)
	if err != nil {
		t.Fatal(err)
	}
	return e.Execute(t.Context(), x.args)
}
func state(t *testing.T, x *fixture) string {
	t.Helper()
	ledger, _ := platformoutbound.New(x.store.Product)
	rows, err := ledger.Receipts(t.Context(), 10)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	return rows[0].State
}

func TestTenantSenderDeliveryAndDurableRestart(t *testing.T) {
	for _, own := range []bool{false, true} {
		t.Run(map[bool]string{false: "broadcaster", true: "own-bot"}[own], func(t *testing.T) {
			x := newFixture(t, own)
			result, err := execute(t, x, x.adapter.Authorize)
			expected := "456"
			if own {
				expected = "123"
			}
			if err != nil || !result.Done || result.Status != platform.Delivered || x.p.sends != 1 || x.p.sentSender != expected || x.p.sentChannel != "456" {
				t.Fatal("delivery", result, err, x.p.sends)
			}
			if _, err = execute(t, x, x.adapter.Authorize); err != nil || x.p.sends != 1 {
				t.Fatal("replay", err, x.p.sends)
			}
			expectedRoles := 0
			expectedValidations := 2
			if own {
				expectedRoles = 2
				expectedValidations = 4
			}
			if x.p.roles != expectedRoles || x.p.validations != expectedValidations {
				t.Fatal("stale authorization", x.p.roles, x.p.validations)
			}
		})
	}
}

func TestPostClaimAuthorizationChangesNeverSend(t *testing.T) {
	for _, mode := range []string{"revoke", "rebind-cycle", "same-subject-reauthorize", "role", "plan", "scope", "subject"} {
		t.Run(mode, func(t *testing.T) {
			x := newFixture(t, true)
			calls := 0
			authorize := func(ctx context.Context, target platformoutbound.Target) (platformoutbound.Authorization, error) {
				calls++
				if calls == 2 {
					switch mode {
					case "revoke":
						if err := x.store.RevokeBotAccount(ctx, target.TenantID, owner, x.botID); err != nil {
							t.Error(err)
						}
					case "rebind-cycle":
						if _, err := x.f.Admin.Exec(`UPDATE platform_connections SET bot_account_id=NULL`); err != nil {
							t.Error(err)
						}
						if _, err := x.f.Admin.Exec(`UPDATE platform_connections SET bot_account_id=$1`, x.botID); err != nil {
							t.Error(err)
						}
					case "same-subject-reauthorize":
						if _, err := x.f.Admin.Exec(`UPDATE platform_connections SET granted_at=clock_timestamp()`); err != nil {
							t.Error(err)
						}
					case "role":
						x.p.moderator = false
					case "plan":
						if _, err := x.f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{httpCallsPerHour}','0') WHERE id='unlimited'`); err != nil {
							t.Error(err)
						}
					case "scope":
						x.p.scopes["123"] = []string{}
					case "subject":
						x.p.mode = "wrong-subject"
					}
				}
				return x.adapter.Authorize(ctx, target)
			}
			result, err := execute(t, x, authorize)
			if !result.Done || x.p.sends != 0 || state(t, x) != "rejected" || err != nil && strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("unsafe changed authorization", result, err, x.p.sends)
			}
			_, _ = execute(t, x, authorize)
			if x.p.sends != 0 {
				t.Fatal("replayed rejected action")
			}
		})
	}
}

func TestAuthorizationProviderFailuresAreBoundedAndRedacted(t *testing.T) {
	for _, mode := range []string{"wrong-client", "wrong-subject", "missing-scope", "transport", "redirect", "oversize", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			x := newFixture(t, false)
			x.p.mode = mode
			result, err := execute(t, x, x.adapter.Authorize)
			if x.p.sends != 0 || err != nil && strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("unsafe provider error", result, err)
			}
			if mode == "wrong-client" || mode == "wrong-subject" || mode == "missing-scope" {
				if !result.Done || state(t, x) != "rejected" {
					t.Fatal(result, err)
				}
			} else if err == nil || result.Done || state(t, x) != "pending" {
				t.Fatal("preparation should remain pending", result, err)
			}
		})
	}
}

func TestSendClosureRechecksBindingPlanAndPayload(t *testing.T) {
	for _, mode := range []string{"binding", "plan", "quota", "payload"} {
		t.Run(mode, func(t *testing.T) {
			x := newFixture(t, false)
			auth, err := x.adapter.Authorize(t.Context(), x.target)
			if err != nil {
				t.Fatal(err)
			}
			message := platform.OutboundMessage{ActionID: x.target.ActionID, ChannelID: "456", Text: "hello", ReplyToID: "source"}
			switch mode {
			case "binding":
				if err = x.store.RevokeConnection(t.Context(), x.target.TenantID, owner, "twitch"); err != nil {
					t.Fatal(err)
				}
			case "plan":
				if _, err = x.f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{httpCallsPerHour}','0') WHERE id='unlimited'`); err != nil {
					t.Fatal(err)
				}
			case "quota":
				if _, err = x.f.Admin.Exec(`UPDATE tenants SET plan_id='free'; UPDATE plans SET limits=jsonb_set(limits,'{httpCallsPerHour}','1') WHERE id='free'`); err != nil {
					t.Fatal(err)
				}
				if err = x.store.Product.ReserveHTTP(t.Context()); err != nil {
					t.Fatal(err)
				}
			case "payload":
				message.ChannelID = "999"
			}
			receipt, err := auth.Send(t.Context(), message)
			if err == nil || receipt.Status != platform.Rejected || x.p.sends != 0 {
				t.Fatal("stale closure sent", receipt, err)
			}
		})
	}
}

func TestCredentialRefreshSerializesAndPreservesSelection(t *testing.T) {
	x := newFixture(t, true)
	selection, err := x.store.ChatSenderSelection(t.Context(), x.target.TenantID, "456")
	if err != nil {
		t.Fatal(err)
	}
	// Both credential forms are expired, so the next authorization must refresh
	// and then validate each exact subject before persisting encrypted changes.
	raw, _ := json.Marshal(broadcasterDisk{UserID: "456", Login: "host", AccessToken: "host-access", RefreshToken: "host-refresh", ExpiresAt: time.Now().Add(-time.Hour)})
	if err = x.store.SaveConnection(t.Context(), x.keys, x.target.TenantID, owner, "twitch", "456", "host", nil, raw); err != nil {
		t.Fatal(err)
	}
	x.botID, err = x.store.SaveValidatedBotAccount(t.Context(), x.keys, x.target.TenantID, owner, tenancy.BotCredential{Version: 1, UserID: "123", Login: "bot", AccessToken: "bot-access", RefreshToken: "bot-refresh", ExpiresAt: time.Now().Add(time.Second), Scopes: []string{"user:write:chat"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = x.store.BindBotAccount(t.Context(), x.target.TenantID, owner, "456", x.botID); err != nil {
		t.Fatal(err)
	}
	selection, err = x.store.ChatSenderSelection(t.Context(), x.target.TenantID, "456")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := x.adapter.Authorize(t.Context(), x.target); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if x.p.refreshes != 2 {
		t.Fatal("refresh was not serialized", x.p.refreshes)
	}
	after, err := x.store.ChatSenderSelection(t.Context(), x.target.TenantID, "456")
	if err != nil || selection.Binding != after.Binding {
		t.Fatal("refresh changed selection", err)
	}
	result, err := execute(t, x, x.adapter.Authorize)
	if err != nil || result.Status != platform.Delivered || x.p.sends != 1 {
		t.Fatal(result, err)
	}
}

func TestActiveSharedSelectionNeverFallsBack(t *testing.T) {
	x := newFixture(t, false)
	admin, err := tenancy.OpenBotAdminStore(t.Context(), x.f.BotAdminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close() }()
	account, err := admin.SaveValidated(t.Context(), x.keys, tenancy.BotCredential{Version: 1, UserID: "999", Login: "shared", AccessToken: "shared-access", RefreshToken: "shared-refresh", ExpiresAt: time.Now().Add(time.Hour), Scopes: []string{"user:write:chat"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := admin.PublishMetadata(t.Context(), account.ID, 1, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = x.store.GrantSharedBot(t.Context(), x.target.TenantID, owner, "456", account.ID, 1, catalog.Revision, 0, []platform.Capability{"chat.reply"}); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.PublishMetadata(t.Context(), account.ID, 1, catalog.Revision, false); err != nil {
		t.Fatal(err)
	}
	result, err := execute(t, x, x.adapter.Authorize)
	if err != nil || !result.Done || x.p.sends != 0 || x.p.validations != 0 || state(t, x) != "rejected" {
		t.Fatal("shared fallback", result, err)
	}
}

func TestSelectionScopeCancellationAndUnexpectedExpiry(t *testing.T) {
	x := newFixture(t, false)
	initial, err := x.store.ChatSenderSelection(t.Context(), x.target.TenantID, "456")
	if err != nil {
		t.Fatal(err)
	}
	// Refresh-only updates cannot forge or advance the generation, even if a
	// trusted runtime accidentally writes the revision column itself.
	if _, err = x.f.Admin.Exec(`UPDATE platform_connections SET sender_revision=999`); err != nil {
		t.Fatal(err)
	}
	current, err := x.store.ChatSenderSelection(t.Context(), x.target.TenantID, "456")
	if err != nil || current.Binding != initial.Binding {
		t.Fatal("revision tampering", err)
	}
	if _, err = x.f.Admin.Exec(`INSERT INTO tenants(id,slug,name,owner_account_id,plan_id) VALUES('00000000-0000-4000-8000-000000000099','other-auth','Other',$1,'unlimited')`, owner); err != nil {
		t.Fatal(err)
	}
	foreign := x.target
	foreign.TenantID = "00000000-0000-4000-8000-000000000099"
	if _, err = x.adapter.Authorize(t.Context(), foreign); !errors.Is(err, platformoutbound.ErrDenied) {
		t.Fatal("foreign sender resolved", err)
	}
	// Token metadata can be stale: a validation 401 refreshes once and validates
	// the refreshed token before any send, even when the saved expiry is future.
	raw, _ := json.Marshal(broadcasterDisk{UserID: "456", Login: "host", AccessToken: "expired", RefreshToken: "host-refresh", ExpiresAt: time.Now().Add(time.Hour)})
	if err = x.store.SaveConnection(t.Context(), x.keys, x.target.TenantID, owner, "twitch", "456", "host", nil, raw); err != nil {
		t.Fatal(err)
	}
	if _, err = x.adapter.Authorize(t.Context(), x.target); err != nil || x.p.refreshes != 1 || x.p.sends != 0 {
		t.Fatal("unexpected expiry", err, x.p.refreshes)
	}
	x.p.mode = "cancel"
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err = x.adapter.Authorize(ctx, x.target); err == nil || strings.Contains(err.Error(), "PRIVATE") || time.Since(start) > time.Second || x.p.sends != 0 {
		t.Fatal("unbounded cancellation", err)
	}
}

func TestPacingSenderIsMetadataOnlyAndDoesNotAuthorize(t *testing.T) {
	for _, own := range []bool{false, true} {
		t.Run(map[bool]string{false: "broadcaster", true: "own-bot"}[own], func(t *testing.T) {
			x := newFixture(t, own)
			sender, err := x.adapter.PacingSender(t.Context(), x.target)
			want := "456"
			if own {
				want = "123"
			}
			if err != nil || sender != want || x.p.validations != 0 || x.p.roles != 0 || x.p.refreshes != 0 || x.p.sends != 0 {
				t.Fatal("metadata hint contacted provider", sender, err, x.p)
			}
			x.p.mode = "wrong-subject"
			if _, err := x.adapter.Authorize(t.Context(), x.target); !errors.Is(err, platformoutbound.ErrDenied) || x.p.validations == 0 {
				t.Fatal("hint bypassed fresh provider authorization", err)
			}
			if err := x.store.RevokeConnection(t.Context(), x.target.TenantID, owner, "twitch"); err != nil {
				t.Fatal(err)
			}
			if _, err := x.adapter.PacingSender(t.Context(), x.target); !errors.Is(err, platformoutbound.ErrDenied) {
				t.Fatal("revoked local selection accepted", err)
			}
		})
	}
}
