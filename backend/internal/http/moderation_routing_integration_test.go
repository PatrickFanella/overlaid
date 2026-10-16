package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformdispatch"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformmoderation"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
)

func retainedModerationRule(t *testing.T, h *moderationOwnerFixture, radiation bool) (*Server, bot.ModerationRule) {
	t.Helper()
	server, err := h.app.runtime(t.Context(), h.account.tenant)
	if err != nil {
		t.Fatal(err)
	}
	rule := bot.ModerationRule{Name: "retained_spam", Enabled: true, Enforce: true, Pattern: "buy now", Action: "delete"}
	if radiation {
		expires := time.Now().Add(time.Minute)
		rule.Source, rule.ExpiresAt = "nuke_radiation", &expires
	}
	rule, err = server.botStore.SaveModerationRule(t.Context(), rule)
	if err != nil {
		t.Fatal(err)
	}
	return server, rule
}

func moderationRulePacket() twitch.EventSubMessage {
	message := packet("123", "owner-source", "rule-delivery", "buy now")
	message.Payload.Event["chatter_user_id"] = "777"
	message.Payload.Event["chatter_user_login"] = "viewer"
	message.Payload.Event["chatter_user_name"] = "Viewer"
	return message
}

func TestRetainedModerationRuleDelivery(t *testing.T) {
	for _, mode := range []string{"broadcaster", "shared", "radiation"} {
		t.Run(mode, func(t *testing.T) {
			h := ownedModerationFixture(t, mode == "shared")
			server, rule := retainedModerationRule(t, h, mode == "radiation")
			entered, released := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(released) }) }
			defer unblock()
			h.onAttempt = func(r *http.Request) (*http.Response, error) {
				close(entered)
				select {
				case <-released:
					return &http.Response{StatusCode: 204, Header: http.Header{}, Body: http.NoBody}, nil
				case <-r.Context().Done():
					return nil, r.Context().Err()
				}
			}
			consumer, err := h.app.prepareModeration(t.Context(), h.database.ModerationWorkerURL, h.database.ModerationPacerURL)
			if err != nil {
				t.Fatal(err)
			}
			if err := consumer.start(t.Context()); err != nil {
				t.Fatal(err)
			}
			message := moderationRulePacket()
			for range 2 {
				if err := server.ingestRetainedChat(t.Context(), message, twitch.Token{UserID: "123"}); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-entered:
			case <-time.After(10 * time.Second):
				t.Fatal("retained rule did not reach transport")
			}
			waitReceipt(t, h.app, h.account.tenant, 1)
			flushBotAudit(t, server)
			var queued, running bool
			if err := h.database.Admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM bot_audit_events WHERE tenant_id=$1 AND trigger='moderation_rule' AND result='allowed' AND reason='moderation_queued'),EXISTS(SELECT 1 FROM platform_moderation_actions WHERE tenant_id=$1 AND state='running' AND action_key=$2)`, h.account.tenant, "rule:"+rule.ID).Scan(&queued, &running); err != nil || !queued || !running {
				t.Fatal("queued admission confused with delivery", queued, running, err)
			}
			unblock()
			waitOwnedModeration(t, h, "delivered")
			// Explicitly rehearse handler reentry: the retained dispatcher itself
			// never replays a completed handler. An unchanged rule is a no-op;
			// changed effect input under the same source/rule must conflict.
			var raw []byte
			if err := h.database.Admin.QueryRow(`SELECT envelope FROM normalized_chat_events WHERE tenant_id=$1`, h.account.tenant).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var source platform.ChatEvent
			if err := json.Unmarshal(raw, &source); err != nil {
				t.Fatal(err)
			}
			projected := twitch.EventsForNormalizedChat(source, server.twitchCommandProcessor, server.interactions.Rules())[0]
			bound := server.moderationSourceContext(t.Context(), source, projected)
			if err := server.enforceModerationForEvent(bound, projected); err != nil {
				t.Fatal("same rule replay was not a no-op", err)
			}
			rule.Action = "timeout"
			if _, err := server.botStore.SaveModerationRule(t.Context(), rule); err != nil {
				t.Fatal(err)
			}
			requests := h.requests.Load()
			if err := server.enforceModerationForEvent(bound, projected); !errors.Is(err, platformmoderation.ErrConflict) {
				t.Fatal("changed rule replay did not conflict", err)
			}
			if _, err := server.executeTwitchModerationAction(t.Context(), botModerationActionRequest{Action: "delete", MessageID: "owner-source"}); !errors.Is(err, errTwitchModerationUnavailable) {
				t.Fatal("unbound direct request bypassed owner", err)
			}
			var actions, jobs int
			if err := h.database.Admin.QueryRow(`SELECT (SELECT count(*) FROM platform_moderation_actions),(SELECT count(*) FROM river_job WHERE kind='overlaid_moderation')`).Scan(&actions, &jobs); err != nil || actions != 1 || jobs != 1 || h.attempts.Load() != 1 || h.requests.Load() != requests {
				t.Fatal("rule replay or direct fallback", actions, jobs, h.attempts.Load(), err)
			}
		})
	}
}

func TestRetainedModerationRuleEffectFreeAndFailure(t *testing.T) {
	for _, mode := range []string{"flag", "exempt", "disabled", "expired", "prepared", "projection", "unbound", "stopped"} {
		t.Run(mode, func(t *testing.T) {
			h := ownedModerationFixture(t, false)
			server, rule := retainedModerationRule(t, h, false)
			switch mode {
			case "flag":
				rule.Action = "flag"
			case "disabled":
				rule.Enabled = false
			case "expired":
				expires := time.Now().Add(-time.Second)
				rule.Source, rule.ExpiresAt = "nuke_radiation", &expires
			}
			if _, err := server.botStore.SaveModerationRule(t.Context(), rule); err != nil {
				t.Fatal(err)
			}
			consumer, err := h.app.prepareModeration(t.Context(), h.database.ModerationWorkerURL, h.database.ModerationPacerURL)
			if err != nil {
				t.Fatal(err)
			}
			if mode != "prepared" {
				if err := consumer.start(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "stopped" {
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				if err := consumer.close(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "projection" || mode == "unbound" {
				source := h.source(t, time.Now().UTC())
				event := events.Event{ID: "projection", Type: events.EventTypeChatMessage, Message: "buy now", Data: map[string]any{"messageId": "owner-source", "twitchEvent": map[string]any{"chatter_user_id": "777", "broadcaster_user_id": "123"}}}
				ctx := t.Context()
				if mode == "projection" {
					ctx = server.moderationSourceContext(ctx, source, event)
				}
				if err := server.enforceModerationForEvent(ctx, event); !errors.Is(err, platformmoderation.ErrInvalid) {
					t.Fatal("unbound projection admitted", err)
				}
			} else {
				message := moderationRulePacket()
				if mode == "exempt" {
					message.Payload.Event["chatter_user_id"] = "123"
				}
				for range 2 {
					if err := server.ingestRetainedChat(t.Context(), message, twitch.Token{UserID: "123"}); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "prepared" || mode == "stopped" {
					ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
					defer cancel()
					db, _ := h.app.store.Product.Scoped(h.account.tenant)
					for {
						receipts, err := platformdispatch.Receipts(ctx, db, 10)
						if err != nil {
							t.Fatal(err)
						}
						if len(receipts) == 1 && receipts[0].State == "failed" {
							break
						}
						select {
						case <-ctx.Done():
							t.Fatal("admission failure did not reach handler receipt", receipts)
						case <-time.After(20 * time.Millisecond):
						}
					}
				} else {
					waitReceipt(t, h.app, h.account.tenant, 1)
				}
			}
			var actions, jobs int
			if err := h.database.Admin.QueryRow(`SELECT (SELECT count(*) FROM platform_moderation_actions),(SELECT count(*) FROM river_job WHERE kind='overlaid_moderation')`).Scan(&actions, &jobs); err != nil || actions != 0 || jobs != 0 || h.requests.Load() != 0 {
				t.Fatal("effect-free rule spent work or provider calls", actions, jobs, h.requests.Load(), err)
			}
		})
	}
}

func TestModerationRoutingDrainsDirectBeforeHandoff(t *testing.T) {
	h := ownedModerationFixture(t, false)
	server, err := h.app.runtime(t.Context(), h.account.tenant)
	if err != nil {
		t.Fatal(err)
	}
	previous := twitchHTTPClient
	entered, released := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(released) }) }
	defer unblock()
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/helix/moderation/chat" {
			return previous.Transport.RoundTrip(r)
		}
		close(entered)
		select {
		case <-released:
			return &http.Response{StatusCode: 204, Header: http.Header{}, Body: http.NoBody}, nil
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	})}
	defer func() { twitchHTTPClient = previous }()
	direct := make(chan error, 1)
	go func() {
		_, err := server.executeTwitchModerationAction(t.Context(), botModerationActionRequest{Action: "delete", MessageID: "owner-source"})
		direct <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("direct moderation did not start")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if _, err := h.app.prepareModeration(ctx, h.database.ModerationWorkerURL, h.database.ModerationPacerURL); err == nil || h.app.moderation != nil {
		t.Fatal("handoff overlapped direct request", err)
	}
	prepared := make(chan error, 1)
	go func() {
		_, err := h.app.prepareModeration(t.Context(), h.database.ModerationWorkerURL, h.database.ModerationPacerURL)
		prepared <- err
	}()
	select {
	case err := <-prepared:
		t.Fatal("handoff did not drain direct request", err)
	case <-time.After(30 * time.Millisecond):
	}
	unblock()
	select {
	case err := <-direct:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("direct request did not finish")
	}
	select {
	case err := <-prepared:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handoff did not finish")
	}
	if _, err := server.executeTwitchModerationAction(t.Context(), botModerationActionRequest{Action: "delete", MessageID: "owner-source"}); !errors.Is(err, errTwitchModerationUnavailable) {
		t.Fatal("prepared owner allowed direct fallback", err)
	}
	if _, err := server.executeNukeWorkflow(t.Context(), botNukeDryRunRequest{Pattern: "buy now", RadiationMinutes: 1}, "delete"); !errors.Is(err, errTwitchModerationUnavailable) {
		t.Fatal("prepared owner allowed a nuke", err)
	}
	var rules, actions int
	if err := h.database.Admin.QueryRow(`SELECT (SELECT count(*) FROM bot_moderation_rules),(SELECT count(*) FROM platform_moderation_actions)`).Scan(&rules, &actions); err != nil || rules != 0 || actions != 0 {
		t.Fatal("refused nuke mutated radiation or ledger", rules, actions, err)
	}
}

func TestModerationRoutingFailedPreparationKeepsDirect(t *testing.T) {
	h := ownedModerationFixture(t, false)
	server, err := h.app.runtime(t.Context(), h.account.tenant)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.prepareModeration(t.Context(), h.database.OutboundWorkerURL, h.database.ModerationPacerURL); err == nil || h.app.moderation != nil {
		t.Fatal("invalid preparation installed owner", err)
	}
	// Reader cancellation while preparation owns the exclusive handoff cannot
	// escape to the provider. The refused writer above must also release it.
	h.app.moderationRouting.Lock()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	_, _, _, routeErr := h.app.moderationRoute(ctx)
	cancel()
	h.app.moderationRouting.Unlock()
	if !errors.Is(routeErr, errTwitchModerationUnavailable) {
		t.Fatal("routing ignored caller cancellation", routeErr)
	}
	previous := twitchHTTPClient
	calls := 0
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/helix/moderation/chat" {
			return previous.Transport.RoundTrip(r)
		}
		calls++
		if r.Method != http.MethodDelete || r.URL.Query().Get("broadcaster_id") != "123" || r.URL.Query().Get("moderator_id") != "123" {
			return nil, errors.New("invalid direct binding")
		}
		return &http.Response{StatusCode: 204, Header: http.Header{}, Body: http.NoBody}, nil
	})}
	defer func() { twitchHTTPClient = previous }()
	result, err := server.executeTwitchModerationAction(t.Context(), botModerationActionRequest{Action: "delete", MessageID: "owner-source"})
	if err != nil || result.Result != "accepted" || calls != 1 {
		t.Fatal("failed preparation disabled legacy owner", result, calls, err)
	}
}
