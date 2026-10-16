package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/moderationpacing"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformmoderation"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchoutbound"
)

func TestSharedModerationTransportProgressesDuringRetainedChat(t *testing.T) {
	for _, quota := range []bool{false, true} {
		t.Run(map[bool]string{false: "delivered", true: "quota"}[quota], func(t *testing.T) {
			old, alpha, beta, f := applicationFixture(t)
			accounts := []accountFixture{alpha, beta}
			channels := []string{"123", "456"}
			for i, account := range accounts {
				token := twitchTokenInfo{AccessToken: "fixture-" + channels[i], UserID: channels[i], Login: "fixture", Scope: []string{"user:read:chat", "user:write:chat", "moderation:read"}, ValidatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
				raw, err := json.Marshal(tokenDisk(&token))
				if err != nil {
					t.Fatal(err)
				}
				if err := old.store.SaveConnection(t.Context(), old.keys, account.tenant, account.id, "twitch", channels[i], "fixture", token.Scope, raw); err != nil {
					t.Fatal(err)
				}
				s, err := old.runtime(t.Context(), account.tenant)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := s.commandsRegistry().SaveCommand(t.Context(), bot.Command{Name: "mixed", Action: "ticker", Enabled: true, RoleRequired: "viewer", ResponseMode: "chat", Response: "Chat " + channels[i]}); err != nil {
					t.Fatal(err)
				}
			}
			admin, err := tenancy.OpenBotAdminStore(t.Context(), f.BotAdminURL)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = admin.Close() }()
			shared, err := admin.SaveValidated(t.Context(), old.keys, tenancy.BotCredential{Version: 1, UserID: "999", Login: "shared", AccessToken: "shared-access", RefreshToken: "shared-refresh", ExpiresAt: time.Now().Add(time.Hour), Scopes: []string{"user:write:chat", "user:bot", "moderator:manage:chat_messages"}}, 0)
			if err != nil {
				t.Fatal(err)
			}
			catalog, err := admin.PublishMetadata(t.Context(), shared.ID, shared.Revision, 0, true)
			if err != nil {
				t.Fatal(err)
			}
			for i, account := range accounts {
				if _, err := old.store.GrantSharedBot(t.Context(), account.tenant, account.id, channels[i], shared.ID, shared.Revision, catalog.Revision, 0, []platform.Capability{"chat.send", "chat.reply", "message.delete"}); err != nil {
					t.Fatal(err)
				}
			}
			cfg := retainedSenderConfig(old.config, f)
			cfg.BotExecutionDatabaseURL = f.BotExecutorURL
			if err := old.Close(); err != nil {
				t.Fatal(err)
			}

			started, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			var chatCalls, modCalls atomic.Int32
			var inFlight atomic.Bool
			previous := twitchHTTPClient
			defer func() { twitchHTTPClient = previous }()
			twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				respond := func(code int, body string) *http.Response {
					return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
				}
				switch r.URL.Host + r.URL.Path {
				case "id.twitch.tv/oauth2/validate":
					id := strings.TrimPrefix(r.Header.Get("Authorization"), "OAuth fixture-")
					if r.Header.Get("Authorization") == "OAuth shared-access" {
						id = "999"
					}
					if id != "123" && id != "456" && id != "999" {
						return nil, errors.New("unexpected mixed validation identity")
					}
					return respond(200, fmt.Sprintf(`{"client_id":"fixture-client","user_id":"%s","login":"fixture","scopes":["user:read:chat","user:write:chat","moderation:read","user:bot","moderator:manage:chat_messages"],"expires_in":3600}`, id)), nil
				case "api.twitch.tv/helix/moderation/moderators":
					channel := r.URL.Query().Get("broadcaster_id")
					if (channel != "123" && channel != "456") || r.URL.Query().Get("user_id") != "999" || r.Header.Get("Authorization") != "Bearer fixture-"+channel {
						return nil, errors.New("mixed moderator lookup identity changed")
					}
					return respond(200, `{"data":[{"user_id":"999"}]}`), nil
				case "api.twitch.tv/helix/chat/messages":
					var body struct {
						Sender  string `json:"sender_id"`
						Channel string `json:"broadcaster_id"`
						Text    string `json:"message"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						return nil, err
					}
					if body.Sender != "999" || (body.Channel != "123" && body.Channel != "456") || body.Text != "Chat "+body.Channel || r.Header.Get("Authorization") != "Bearer shared-access" {
						return nil, errors.New("mixed chat selected identity or content changed")
					}
					n := chatCalls.Add(1)
					if n == 1 {
						inFlight.Store(true)
						defer inFlight.Store(false)
						close(started)
						select {
						case <-release:
						case <-r.Context().Done():
							return nil, r.Context().Err()
						}
					}
					return respond(200, fmt.Sprintf(`{"data":[{"message_id":"mixed-chat-%d","is_sent":true}]}`, n)), nil
				case "api.twitch.tv/helix/moderation/chat":
					modCalls.Add(1)
					q := r.URL.Query()
					if !inFlight.Load() || r.Method != http.MethodDelete || q.Get("broadcaster_id") != "456" || q.Get("moderator_id") != "999" || q.Get("message_id") != "moderation-target" || r.Header.Get("Authorization") != "Bearer shared-access" {
						return nil, errors.New("moderation waited for chat or changed selected target")
					}
					var claimed bool
					if err := f.Admin.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM platform_moderation_actions WHERE tenant_id=$1 AND state='running' AND message_id='moderation-target')`, beta.tenant).Scan(&claimed); err != nil || !claimed {
						return nil, errors.New("moderation transport without retained claim")
					}
					if quota {
						out := respond(429, `{"message":"PRIVATE_PROVIDER_QUOTA_BODY"}`)
						out.Header.Set("Retry-After", "20")
						return out, nil
					}
					return respond(204, ""), nil
				}
				return nil, errors.New("unexpected mixed provider endpoint")
			})}
			app, err := NewApplication(cfg)
			if err != nil {
				unblock()
				t.Fatal(err)
			}
			defer func() { unblock(); _ = app.Close() }()
			ingest := func(index int, id, text string) {
				t.Helper()
				s, err := app.runtime(t.Context(), accounts[index].tenant)
				if err != nil {
					t.Fatal(err)
				}
				message := packet(channels[index], id, "delivery-"+id, text)
				for range 2 {
					if err := s.ingestRetainedChat(t.Context(), message, twitch.Token{UserID: channels[index]}); err != nil {
						t.Fatal(err)
					}
				}
			}
			ingest(1, "moderation-target", "PRIVATE_RETAINED_TARGET")
			waitReceipt(t, app, beta.tenant, 1)
			db, err := app.store.Product.Scoped(beta.tenant)
			if err != nil {
				t.Fatal(err)
			}
			var raw []byte
			if err := db.QueryRowContext(t.Context(), `SELECT envelope FROM normalized_chat_events WHERE /*tenant:normalized_chat_events*/ AND envelope->'chat'->>'message_id'='moderation-target'`).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var source platform.ChatEvent
			if err := json.Unmarshal(raw, &source); err != nil {
				t.Fatal(err)
			}
			intent, err := platform.NewTwitchModerationIntent(source, "fixture-delete", platform.ModerationAction{Capability: "message.delete", ChannelID: "456", MessageID: "moderation-target"})
			if err != nil {
				t.Fatal(err)
			}
			ledger, err := platformmoderation.New(db)
			if err != nil {
				t.Fatal(err)
			}
			for i := range 2 {
				if _, created, err := ledger.Admit(t.Context(), intent); err != nil || created != (i == 0) {
					t.Fatal("moderation duplicate admission", created, err)
				}
			}
			pacer, err := moderationpacing.Open(t.Context(), f.ModerationPacerURL)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = pacer.Close() }()
			adapter, err := twitchoutbound.NewShared(t.Context(), app.store, app.keys, twitchoutbound.Config{ClientID: cfg.TwitchClientID, ClientSecret: cfg.TwitchClientSecret, Client: twitchHTTPClient}, app.outbound.shared)
			if err != nil {
				t.Fatal(err)
			}
			executor, err := platformmoderation.NewExecutorWithGate(t.Context(), app.store.Product, pacer, app.moderationAuthorize(adapter.AuthorizeModeration), app.moderationAttemptGate)
			if err != nil {
				t.Fatal(err)
			}
			service, err := sql.Open("pgx", f.ModerationWorkerURL)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = service.Close() }()
			client, err := platformmoderation.NewClient(t.Context(), service, executor)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := client.Stop(ctx); err != nil {
					t.Error(err)
				}
			}()
			// The fixture owns this explicit moderation client. Application startup
			// still constructs no moderation pool/worker or admission route.
			ingest(0, "chat-0", "!mixed")
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("chat transport never started")
			}
			for i := 1; i < 16; i++ {
				ingest(i%2, fmt.Sprintf("chat-%d", i), "!mixed")
			}
			if err := client.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			wantState, wantJob := "delivered", "completed"
			if quota {
				wantState, wantJob = "rejected", "cancelled"
			}
			deadline := time.Now().Add(4 * time.Second)
			for {
				var settled bool
				if err := f.Admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM platform_moderation_actions a JOIN river_job j ON j.overlaid_moderation_tenant_id=a.tenant_id AND j.overlaid_moderation_action_id=a.id WHERE a.id=$1 AND a.state=$2 AND j.state=$3)`, intent.Action().ActionID, wantState, wantJob).Scan(&settled); err != nil {
					t.Fatal(err)
				}
				if settled {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("moderation failed to settle during held chat")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if chatCalls.Load() != 1 || modCalls.Load() != 1 || !inFlight.Load() {
				t.Fatal("chat and moderation lanes interfered", chatCalls.Load(), modCalls.Load())
			}
			var charged, retained, unfinished int
			if err := f.Admin.QueryRow(`SELECT (SELECT coalesce(sum(jsonb_array_length(attempts)),0) FROM chat_pacing_accounts),(SELECT count(*) FROM normalized_chat_events WHERE envelope->'chat'->>'text'='!mixed'),(SELECT count(*) FROM river_job WHERE kind IN('overlaid_chat_dispatch','overlaid_chat_send') AND state NOT IN('completed','cancelled','discarded'))`).Scan(&charged, &retained, &unfinished); err != nil || charged != 1 || retained != 16 || unfinished < 16 {
				t.Fatal("retained chat backlog reserved slots or escaped held request", charged, retained, unfinished, err)
			}
			receipts, err := ledger.Receipts(t.Context(), 10)
			if err != nil || len(receipts) != 1 || receipts[0].State != wantState || receipts[0].ActionID != intent.Action().ActionID {
				t.Fatal("moderation receipt missing", receipts, err)
			}
			if quota && (receipts[0].Failure != platform.QuotaLimited || receipts[0].RetryAfter != 20*time.Second) {
				t.Fatal("moderation quota receipt collapsed", receipts[0])
			}
			encoded, err := json.Marshal(receipts)
			if err != nil || strings.Contains(string(encoded), "PRIVATE_") || strings.Contains(string(encoded), "shared-access") || strings.Contains(string(encoded), "moderation-target") {
				t.Fatal("moderation receipt leaked private input", err)
			}
			var chatBlocked, modBlocked bool
			if err := f.Admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM chat_pacing_accounts WHERE blocked_until>clock_timestamp()),EXISTS(SELECT 1 FROM moderation_pacing_buckets WHERE next_at>clock_timestamp()+interval '10 seconds')`).Scan(&chatBlocked, &modBlocked); err != nil || chatBlocked || modBlocked != quota {
				t.Fatal("moderation cooldown crossed budgets", chatBlocked, modBlocked, err)
			}
			unblock()
			drainDeadline := time.Now().Add(30 * time.Second)
			for {
				var delivered, failed int
				if err := f.Admin.QueryRow(`SELECT count(*) FILTER(WHERE state='delivered'),count(*) FILTER(WHERE state IN('rejected','uncertain')) FROM platform_outbound_actions`).Scan(&delivered, &failed); err != nil || failed != 0 {
					t.Fatal("chat drain failed", delivered, failed, err)
				}
				if delivered == 16 {
					break
				}
				if time.Now().After(drainDeadline) {
					t.Fatal("chat did not drain independently of moderation quota", delivered)
				}
				time.Sleep(20 * time.Millisecond)
			}
			for _, account := range accounts {
				waitOutboundState(t, app, account.tenant, 8, "delivered")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := client.Stop(ctx); err != nil {
				cancel()
				t.Fatal(err)
			}
			cancel()
			restarted, err := platformmoderation.NewClient(t.Context(), service, executor)
			if err != nil {
				t.Fatal(err)
			}
			if err := restarted.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
			if err := restarted.Stop(ctx); err != nil {
				cancel()
				t.Fatal(err)
			}
			cancel()
			if id, created, err := ledger.Admit(t.Context(), intent); err != nil || created || id != intent.Action().ActionID {
				t.Fatal("terminal moderation admission gained another effect", created, err)
			}
			var jobs, attempts int
			var originalDeadline bool
			if err := f.Admin.QueryRow(`SELECT count(*),coalesce(max(attempt),0),bool_and((args->>'deadline')::bigint=$1) FROM river_job WHERE kind='overlaid_moderation'`, intent.Deadline().Unix()).Scan(&jobs, &attempts, &originalDeadline); err != nil || jobs != 1 || attempts != 1 || !originalDeadline {
				t.Fatal("moderation restart renewed job, attempt or deadline", jobs, attempts, originalDeadline, err)
			}
			if err := app.Close(); err != nil {
				t.Fatal(err)
			}
			if chatCalls.Load() != 16 || modCalls.Load() != 1 {
				t.Fatal("terminal provider action replayed", chatCalls.Load(), modCalls.Load())
			}
			var runnable, permits int
			if err := f.Admin.QueryRow(`SELECT (SELECT count(*) FROM river_job WHERE state NOT IN('completed','cancelled','discarded')),(SELECT count(*) FROM chat_pacing_accounts WHERE lease_until IS NOT NULL)+(SELECT count(*) FROM moderation_pacing_buckets WHERE lease_until IS NOT NULL)`).Scan(&runnable, &permits); err != nil || runnable != 0 || permits != 0 {
				t.Fatal("mixed jobs or permits not released", runnable, permits, err)
			}
			for i, account := range accounts {
				var receipts, echoes, usage int
				if err := f.Admin.QueryRow(`SELECT (SELECT count(*) FROM platform_dispatch_receipts WHERE tenant_id=$1 AND state='completed'),(SELECT count(*) FROM twitch_sent_messages WHERE tenant_id=$1),(SELECT coalesce(max(used),0) FROM tenant_usage WHERE tenant_id=$1 AND kind='http')`, account.tenant).Scan(&receipts, &echoes, &usage); err != nil || receipts != 8+i || echoes != 8 || usage <= 0 || usage > 120 {
					t.Fatal("mixed receipt/echo/usage isolation", i, receipts, echoes, usage, err)
				}
			}
		})
	}
}
