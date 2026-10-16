package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformoutbound"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
)

func TestPostgresRegistryReplyCollection(t *testing.T) {
	app, alpha, beta, _ := applicationFixture(t)
	ctx := t.Context()
	s, err := app.runtime(ctx, alpha.tenant)
	if err != nil {
		t.Fatal(err)
	}
	db, err := app.store.Product.Scoped(alpha.tenant)
	if err != nil {
		t.Fatal(err)
	}
	s.retainedReplies, err = newRegistryReplyAdmission(db)
	if err != nil {
		t.Fatal(err)
	}
	token := twitchTokenInfo{AccessToken: "fixture", UserID: "alpha", Login: "alpha", Scope: []string{"user:read:chat", "user:write:chat"}, ValidatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	raw, err := json.Marshal(tokenDisk(&token))
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.SaveConnection(ctx, app.keys, alpha.tenant, alpha.id, "twitch", "alpha", "alpha", token.Scope, raw); err != nil {
		t.Fatal(err)
	}
	var sends atomic.Int32
	previous := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		sends.Add(1)
		return nil, errors.New("provider must not be called")
	})}
	defer func() {
		twitchHTTPClient = previous
		if sends.Load() != 0 {
			t.Error("collector reached provider", sends.Load())
		}
	}()
	save := func(t *testing.T, c bot.Command) bot.Command {
		t.Helper()
		saved, err := s.commandsRegistry().SaveCommand(ctx, c)
		if err != nil {
			t.Fatal(err)
		}
		s.refreshTwitchCustomCommands(ctx)
		return saved
	}
	source := func(t *testing.T, id, text string, age ...time.Duration) platform.ChatEvent {
		t.Helper()
		message := packet("alpha", id, "delivery-"+id, text)
		if len(age) > 0 {
			message.Metadata.MessageTimestamp = time.Now().Add(-age[0])
		}
		message.Payload.Event["chatter_user_id"] = "7"
		message.Payload.Event["chatter_user_login"] = "viewer"
		event, err := twitch.NormalizeChatIngest(platform.Connection{TenantID: alpha.tenant, ID: "twitch:alpha", Platform: "twitch"}, message, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.platformIngest.IngestChat(ctx, event, nil); err != nil {
			t.Fatal(err)
		}
		return event
	}
	count := func(t *testing.T, event platform.ChatEvent, want int) {
		t.Helper()
		var actions, jobs int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/ AND event_id=$1`, event.ID).Scan(&actions); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM river_job WHERE kind='overlaid_chat_send' AND overlaid_action_id IN (SELECT id FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/ AND event_id=$1)`, event.ID).Scan(&jobs); err != nil {
			t.Fatal(err)
		}
		if actions != want || jobs != want {
			t.Fatalf("actions/jobs=%d/%d want=%d", actions, jobs, want)
		}
	}
	for _, test := range []struct {
		name, mode, response, role string
		want                       int
	}{
		{"replychat", "chat", "Hello chat", "viewer", 1},
		{"replyboth", "both", "Hello both", "viewer", 1},
		{"replyoverlay", "overlay", "Overlay only", "viewer", 0},
		{"replyempty", "chat", "{args}", "viewer", 0},
		{"replydenied", "chat", "No permission", "moderator", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := s.bus.Subscribe()
			defer s.bus.Unsubscribe(client.ID)
			save(t, bot.Command{Name: test.name, Enabled: true, Action: "ticker", ResponseMode: test.mode, Response: test.response, RoleRequired: test.role})
			event := source(t, test.name, "!"+test.name)
			if err := s.dispatchRetainedChat(ctx, event); err != nil {
				t.Fatal(err)
			}
			count(t, event, test.want)
			if test.want == 1 {
				if err := s.dispatchRetainedChat(ctx, event); err != nil {
					t.Fatal("duplicate changed identity", err)
				}
				count(t, event, 1)
				var text, reply string
				if err := db.QueryRowContext(ctx, `SELECT message,reply_to_id FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/ AND event_id=$1`, event.ID).Scan(&text, &reply); err != nil || text != test.response || reply != "" {
					t.Fatal("reply payload changed", text, reply, err)
				}
			}
			if test.mode == "both" || test.mode == "overlay" {
				found := false
				for len(client.Events) > 0 {
					out := <-client.Events
					if stringData(out.Data, "command") == test.name && out.Data["ticker"] != nil {
						found = true
					}
				}
				if !found {
					t.Fatal("overlay effect lost")
				}
			}
		})
	}
	t.Run("newsroom-current", func(t *testing.T) {
		save(t, bot.Command{Name: "replycurrent", Enabled: true, Action: "newsroom_current", RoleRequired: "viewer"})
		event := source(t, "current", "!replycurrent")
		if err := s.dispatchRetainedChat(ctx, event); err != nil {
			t.Fatal(err)
		}
		count(t, event, 1)
		var text string
		if err := db.QueryRowContext(ctx, `SELECT message FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/ AND event_id=$1`, event.ID).Scan(&text); err != nil || text != "No source is currently on the Newsroom desk." {
			t.Fatal("Newsroom reply missing", text, err)
		}
	})
	t.Run("cooldown", func(t *testing.T) {
		save(t, bot.Command{Name: "replycooldown", Enabled: true, Action: "ticker", ResponseMode: "chat", Response: "Once", RoleRequired: "viewer", CooldownSeconds: 60})
		first := source(t, "cooldown-first", "!replycooldown")
		second := source(t, "cooldown-second", "!replycooldown")
		if err := s.dispatchRetainedChat(ctx, first); err != nil {
			t.Fatal(err)
		}
		if err := s.dispatchRetainedChat(ctx, second); err != nil {
			t.Fatal(err)
		}
		count(t, first, 1)
		count(t, second, 0)
	})
	command := save(t, bot.Command{Name: "replyfence", Enabled: true, Action: "ticker", ResponseMode: "chat", Response: "Fence", RoleRequired: "viewer"})
	collect := func(t *testing.T, event platform.ChatEvent, text string) *registryReplyCollection {
		t.Helper()
		c, err := s.beginRegistryReplies(ctx, event)
		if err != nil {
			t.Fatal(err)
		}
		out := events.NewCommand("twitch", "reply", "", events.SeveritySuccess, map[string]any{"chatResponse": map[string]any{"message": text}, "registryCommandId": command.ID, "registryCommandFingerprint": bot.CommandSnapshotFingerprint(s.tenantID, command)})
		if _, err := s.sendTwitchChatAction(c.outputContext(ctx, out), out.ID, text); err != nil {
			t.Fatal(err)
		}
		return c
	}
	t.Run("conflicting-rerender", func(t *testing.T) {
		event := source(t, "conflict", "!replyfence")
		if err := collect(t, event, "First rendered text").admit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := collect(t, event, "Changed rendered text").admit(ctx); !errors.Is(err, platformoutbound.ErrConflict) {
			t.Fatal("immutable conflict missing", err)
		}
		count(t, event, 1)
	})
	t.Run("snapshot-change", func(t *testing.T) {
		event := source(t, "snapshot", "!replyfence")
		c := collect(t, event, "Fence")
		command.Response = "Changed"
		command = save(t, command)
		if err := c.admit(ctx); err == nil {
			t.Fatal("stale snapshot admitted")
		}
		count(t, event, 0)
	})
	t.Run("cancellation", func(t *testing.T) {
		event := source(t, "canceled", "!replyfence")
		c := collect(t, event, "Fence")
		canceled, stop := context.WithCancel(ctx)
		stop()
		if err := c.admit(canceled); !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation identity lost", err)
		}
		count(t, event, 0)
		if err := s.dispatchRetainedChat(canceled, event); !errors.Is(err, context.Canceled) {
			t.Fatal("canceled dispatch executed", err)
		}
	})
	t.Run("source-binding", func(t *testing.T) {
		event := source(t, "bound", "!replyfence")
		modified := event
		modified.Chat.Text = "!replycurrent"
		if _, err := s.beginRegistryReplies(ctx, modified); err == nil {
			t.Fatal("modified retained input accepted")
		}
		modified = event
		modified.ID = strings.Repeat("a", 64)
		if _, err := s.beginRegistryReplies(ctx, modified); err == nil {
			t.Fatal("invalid identity accepted")
		}
		b, err := app.runtime(ctx, beta.tenant)
		if err != nil {
			t.Fatal(err)
		}
		bdb, err := app.store.Product.Scoped(beta.tenant)
		if err != nil {
			t.Fatal(err)
		}
		b.retainedReplies, err = newRegistryReplyAdmission(bdb)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := b.beginRegistryReplies(ctx, event); err == nil {
			t.Fatal("foreign source accepted")
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM normalized_chat_events WHERE /*tenant:normalized_chat_events*/ AND id=$1`, event.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.beginRegistryReplies(ctx, event); err == nil {
			t.Fatal("removed source accepted")
		}
	})
	t.Run("source-removed-after-collection", func(t *testing.T) {
		event := source(t, "removed-after", "!replyfence")
		c := collect(t, event, "Fence")
		if _, err := db.ExecContext(ctx, `DELETE FROM normalized_chat_events WHERE /*tenant:normalized_chat_events*/ AND id=$1`, event.ID); err != nil {
			t.Fatal(err)
		}
		if err := c.admit(ctx); err == nil {
			t.Fatal("removed source admitted")
		}
		count(t, event, 0)
	})
	t.Run("unbound-and-closed", func(t *testing.T) {
		if _, err := s.sendTwitchChatMessage(ctx, "Manual announcement"); err == nil {
			t.Fatal("unbound callback accepted")
		}
		event := source(t, "closed", "!replyfence")
		c := collect(t, event, "Fence")
		c.close()
		if err := c.admit(ctx); err == nil {
			t.Fatal("closed collector admitted")
		}
		count(t, event, 0)
	})
	t.Run("publication-failure-after-collection", func(t *testing.T) {
		event := source(t, "publication-failure", "!replyfence")
		previous := s.history
		s.history = registryReplyFailingHistory{previous}
		defer func() { s.history = previous }()
		if err := s.dispatchRetainedChat(ctx, event); err == nil {
			t.Fatal("publication failure ignored")
		}
		count(t, event, 0)
	})
	t.Run("durable-echo", func(t *testing.T) {
		event := source(t, "known-echo", "!replyfence")
		if err := s.platformIngest.RememberSentMessage(ctx, event.Chat.MessageID); err != nil {
			t.Fatal(err)
		}
		if err := s.dispatchRetainedChat(ctx, event); err != nil {
			t.Fatal(err)
		}
		count(t, event, 0)
	})
	t.Run("admission-failure-no-fallback", func(t *testing.T) {
		event := source(t, "expired", "!replyfence", 3*time.Minute)
		if err := s.dispatchRetainedChat(ctx, event); err == nil {
			t.Fatal("expired source admitted")
		}
		count(t, event, 0)
	})
}

// Fail only after a chat reply has reached the central collection boundary.
type registryReplyFailingHistory struct{ eventHistoryStore }

func (h registryReplyFailingHistory) Append(event events.Event) error {
	if _, ok := outboundChatResponseFromEvent(event); ok {
		return errors.New("fixture publication failed")
	}
	return h.eventHistoryStore.Append(event)
}
