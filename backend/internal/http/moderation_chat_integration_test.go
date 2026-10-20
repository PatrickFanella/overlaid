package httpapi

import (
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
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformmoderation"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
)

func TestConfiguredSharedModerationProgressesDuringRetainedChat(t *testing.T) {
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
			cfg.TwitchModerationMode = "retained"
			cfg.ModerationWorkerURL = f.ModerationWorkerURL
			cfg.ModerationPacingURL = f.ModerationPacerURL
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
					if !quota && n > 1 {
						// Fifteen sequential successful sends take at least 31.5s.
						// Exercise progress beyond the former aggregate 30s
						// cutoff without coupling it to the quota cooldown's lifetime.
						delay := time.NewTimer(2100 * time.Millisecond)
						defer delay.Stop()
						select {
						case <-delay.C:
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
						out.Header.Set("Retry-After", "60")
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
			if app.moderation == nil || !app.moderation.isReady() || moderationOwnerCheck(t, app) != "ok" {
				t.Fatal("configured moderation owner not ready")
			}
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
			ingest(0, "chat-0", "!mixed")
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("chat transport never started")
			}
			for i := 1; i < 16; i++ {
				ingest(i%2, fmt.Sprintf("chat-%d", i), "!mixed")
			}
			// The production-configured owner is already running. Admit only
			// after chat transport is held so its real queue/guard path must
			// make progress concurrently, without borrowing chat's echo lock.
			admit := func(expectedCreated bool) {
				t.Helper()
				server, err := app.runtime(t.Context(), beta.tenant)
				if err != nil {
					t.Fatal(err)
				}
				id, created, err := app.moderation.admit(t.Context(), server, source.ID, "fixture-delete", botModerationActionRequest{Action: "delete", MessageID: "moderation-target"})
				if err != nil || created != expectedCreated || id != intent.Action().ActionID {
					t.Fatal("configured moderation admission changed identity", id, created, err)
				}
			}
			admit(true)
			admit(false)
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
			if quota && (receipts[0].Failure != platform.QuotaLimited || receipts[0].RetryAfter != 60*time.Second) {
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
			// Poll-only leasing, short snoozes and fair admission can each add
			// a scheduling interval between sends. Require continued progress,
			// not an aggregate throughput that the production policy does not
			// promise, while retaining the immutable source deadline.
			drainStarted := time.Now()
			progressDeadline := drainStarted.Add(30 * time.Second)
			lastDelivered := 0
			for {
				var delivered, failed int
				if err := f.Admin.QueryRow(`SELECT count(*) FILTER(WHERE state='delivered'),count(*) FILTER(WHERE state IN('rejected','uncertain')) FROM platform_outbound_actions`).Scan(&delivered, &failed); err != nil || failed != 0 {
					t.Fatal("chat drain failed", delivered, failed, err)
				}
				if delivered == 16 {
					t.Log("chat drain completed", "elapsed", time.Since(drainStarted))
					break
				}
				if delivered > lastDelivered {
					lastDelivered = delivered
					progressDeadline = time.Now().Add(30 * time.Second)
				}
				if time.Now().After(progressDeadline) || !time.Now().Before(intent.Deadline()) {
					var metadata string
					diagnosticErr := f.Admin.QueryRow(`SELECT jsonb_build_object(
      'ledger',(SELECT coalesce(jsonb_agg(x),'[]'::jsonb) FROM (SELECT state,code,failure,count(*) AS count FROM platform_outbound_actions GROUP BY state,code,failure) x),
      'jobs',(SELECT coalesce(jsonb_agg(x),'[]'::jsonb) FROM (SELECT kind,state,attempt,count(*) AS count,min(extract(epoch FROM scheduled_at-clock_timestamp()))::int AS scheduled_in_seconds,max(coalesce(cardinality(errors),0)) AS error_count,max(coalesce((metadata->>'snoozes')::int,0)) AS snoozes,max(CASE WHEN errors[cardinality(errors)]->>'error' IN('outbound_prepare_unavailable','outbound_expiry_bookkeeping_unavailable','outbound_invalid_job','outbound_bookkeeping_failed','outbound_deadline_elapsed','outbound_terminal','outbound_invalid_result','dispatch_runtime_unavailable','dispatch_prepare_unavailable','handler_failed','handler_cancelled') THEN errors[cardinality(errors)]->>'error' ELSE 'redacted_or_none' END) AS last_error FROM river_job GROUP BY kind,state,attempt) x),
      'dispatch',(SELECT coalesce(jsonb_agg(x),'[]'::jsonb) FROM (SELECT state,code,count(*) AS count FROM platform_dispatch_receipts GROUP BY state,code) x),
      'pacing',(SELECT coalesce(jsonb_agg(x),'[]'::jsonb) FROM (SELECT jsonb_array_length(attempts) AS charged,extract(epoch FROM clock_timestamp()-to_timestamp((attempts->>-1)::numeric/1000000))::int AS last_charge_age_seconds,(SELECT max(gap)/1000000.0 FROM (SELECT value::bigint-lag(value::bigint) OVER(ORDER BY ord) AS gap FROM jsonb_array_elements_text(attempts) WITH ORDINALITY a(value,ord)) gaps) AS max_charge_gap_seconds,lease_until>clock_timestamp() AS leased,extract(epoch FROM lease_until-clock_timestamp())::int AS lease_seconds,(SELECT count(*) FROM jsonb_array_elements(fair_state->'waiters') w WHERE NOT (w->>'granted')::boolean) AS waiting,(SELECT count(*) FROM jsonb_array_elements(fair_state->'waiters') w WHERE NOT (w->>'granted')::boolean AND (w->>'active_until')::timestamptz>clock_timestamp()) AS active_waiting,(SELECT max(extract(epoch FROM next_at-clock_timestamp()))::int FROM chat_pacing_channels) AS channel_wait_seconds FROM chat_pacing_accounts) x))::text`).Scan(&metadata)
					t.Log("mixed drain metadata", metadata, diagnosticErr, "controller_ready", app.chatReady(), "sender_ready", app.outboundLeaseReady())
					t.Fatal("chat drain stopped progressing or exceeded original deadline", delivered)
				}
				time.Sleep(20 * time.Millisecond)
			}
			for _, account := range accounts {
				waitOutboundState(t, app, account.tenant, 8, "delivered")
			}
			// Restart the entire configured owner/controller, preserving both
			// pacing lanes and the already terminal original action/job.
			if err := app.Close(); err != nil {
				t.Fatal(err)
			}
			pacingSnapshot := func() string {
				t.Helper()
				var raw string
				if err := f.Admin.QueryRow(`SELECT jsonb_build_object('chat',(SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY a.account_key),'[]'::jsonb) FROM chat_pacing_accounts a),'channels',(SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY c.account_key,c.channel_key),'[]'::jsonb) FROM chat_pacing_channels c),'moderation',(SELECT coalesce(jsonb_agg(to_jsonb(b) ORDER BY b.kind,b.key),'[]'::jsonb) FROM moderation_pacing_buckets b))::text`).Scan(&raw); err != nil {
					t.Fatal(err)
				}
				return raw
			}
			before := pacingSnapshot()
			restarted, err := NewApplication(cfg)
			if err != nil {
				t.Fatal(err)
			}
			app = restarted
			if !app.moderation.isReady() || moderationOwnerCheck(t, app) != "ok" {
				t.Fatal("restarted configured owner not ready")
			}
			if after := pacingSnapshot(); after != before {
				t.Fatal("restart rewrote pacing/cooldown state")
			}
			var activeCooldown bool
			if err := f.Admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM moderation_pacing_buckets WHERE next_at>clock_timestamp())`).Scan(&activeCooldown); err != nil || activeCooldown != quota {
				t.Fatal("restart lost active moderation cooldown", activeCooldown, err)
			}
			admit(false)
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
