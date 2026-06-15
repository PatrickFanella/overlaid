package twitch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/interactions"
	"github.com/gorilla/websocket"
)

func TestMapChatMessage(t *testing.T) {
	event, ok := MapNotification(EventSubMessage{
		Metadata: EventSubMetadata{MessageID: "msg-1"},
		Payload: EventSubPayload{
			Subscription: EventSubSubscription{Type: "channel.chat.message"},
			Event: map[string]any{
				"chatter_user_name": "Viewer",
				"message":           map[string]any{"text": "hello chat"},
			},
		},
	})

	if !ok {
		t.Fatal("expected mapped event")
	}
	if event.Type != "chat_message" || event.Source != "twitch" || event.Title != "Viewer" || event.Message != "hello chat" {
		t.Fatalf("unexpected event: %#v", event)
	}
}

func TestMapChannelPointRedemption(t *testing.T) {
	event, ok := MapNotification(EventSubMessage{
		Metadata: EventSubMetadata{MessageID: "msg-2"},
		Payload: EventSubPayload{
			Subscription: EventSubSubscription{Type: "channel.channel_points_custom_reward_redemption.add"},
			Event: map[string]any{
				"user_name":  "Viewer",
				"user_input": "make it red",
				"reward":     map[string]any{"title": "Change color"},
			},
		},
	})

	if !ok {
		t.Fatal("expected mapped event")
	}
	if event.Type != "alert" || event.Title != "Viewer redeemed channel points" || event.Message != "Change color: make it red" {
		t.Fatalf("unexpected event: %#v", event)
	}
}

func TestHasScopes(t *testing.T) {
	if !hasScopes([]string{"user:read:chat", "bits:read"}, []string{"user:read:chat"}) {
		t.Fatal("expected scope match")
	}
	if hasScopes([]string{"user:read:chat"}, []string{"bits:read"}) {
		t.Fatal("expected missing scope")
	}
}

func TestDiagnosticsSanitizeSecretLookingErrors(t *testing.T) {
	store := NewDiagnosticsStore(true)
	store.MarkError(errors.New("bearer token password authorization"))
	snapshot := store.Snapshot()
	if strings.Contains(strings.ToLower(snapshot.LastError), "token") || strings.Contains(strings.ToLower(snapshot.LastError), "password") || strings.Contains(strings.ToLower(snapshot.LastError), "authorization") {
		t.Fatalf("expected sanitized diagnostic error, got %q", snapshot.LastError)
	}
}

func TestEventsForNotificationChatCommand(t *testing.T) {
	processor := NewCommandProcessor(CommandProcessorConfig{Cooldown: 0, Now: func() time.Time { return time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC) }})
	eventsOut := EventsForNotification(EventSubMessage{
		Metadata: EventSubMetadata{MessageID: "msg-command"},
		Payload: EventSubPayload{
			Subscription: EventSubSubscription{Type: "channel.chat.message"},
			Event: map[string]any{
				"chatter_user_name":   "Moderator",
				"chatter_user_login":  "mod_login",
				"chatter_user_id":     "100",
				"broadcaster_user_id": "999",
				"badges":              []any{map[string]any{"set_id": "moderator"}},
				"message":             map[string]any{"text": "!alert hydrate now"},
			},
		},
	}, processor, interactions.Rules{})

	if len(eventsOut) != 3 {
		t.Fatalf("event count=%d, want 3", len(eventsOut))
	}
	if eventsOut[0].Type != events.EventTypeChatMessage {
		t.Fatalf("first event type=%s, want chat_message", eventsOut[0].Type)
	}
	if eventsOut[1].Type != events.EventTypeCommand || eventsOut[1].Severity != events.SeveritySuccess {
		t.Fatalf("diagnostic event=%#v", eventsOut[1])
	}
	if got := eventsOut[1].Data["isModerator"]; got != true {
		t.Fatalf("expected moderator diagnostic, got %#v", eventsOut[1].Data)
	}
	if eventsOut[2].Type != events.EventTypeAlert || eventsOut[2].Title != "Moderator" || eventsOut[2].Message != "hydrate now" {
		t.Fatalf("action event=%#v", eventsOut[2])
	}
}

func TestEventsForNotificationViewerDenied(t *testing.T) {
	processor := NewCommandProcessor(CommandProcessorConfig{Cooldown: 0, Now: func() time.Time { return time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC) }})
	eventsOut := EventsForNotification(EventSubMessage{
		Metadata: EventSubMetadata{MessageID: "msg-viewer"},
		Payload: EventSubPayload{
			Subscription: EventSubSubscription{Type: "channel.chat.message"},
			Event: map[string]any{
				"chatter_user_name":   "Viewer",
				"chatter_user_login":  "viewer_login",
				"chatter_user_id":     "200",
				"broadcaster_user_id": "999",
				"badges":              []map[string]any{},
				"message":             map[string]any{"text": "!clear"},
			},
		},
	}, processor, interactions.Rules{})

	if len(eventsOut) != 2 {
		t.Fatalf("event count=%d, want 2", len(eventsOut))
	}
	if eventsOut[1].Type != events.EventTypeCommand || eventsOut[1].Severity != events.SeverityWarning {
		t.Fatalf("diagnostic event=%#v", eventsOut[1])
	}
	if got := eventsOut[1].Message; got != "Permission denied" {
		t.Fatalf("diagnostic message=%q, want Permission denied", got)
	}
}

func TestEventsForNotificationRecognizesModeratorBadgeJSONShape(t *testing.T) {
	processor := NewCommandProcessor(CommandProcessorConfig{Cooldown: 0, Now: func() time.Time { return time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC) }})
	eventsOut := EventsForNotification(EventSubMessage{
		Metadata: EventSubMetadata{MessageID: "msg-moderator"},
		Payload: EventSubPayload{
			Subscription: EventSubSubscription{Type: "channel.chat.message"},
			Event: map[string]any{
				"chatter_user_name":   "Mod",
				"chatter_user_login":  "mod_login",
				"chatter_user_id":     "200",
				"broadcaster_user_id": "999",
				"badges": []any{
					map[string]any{"set_id": "subscriber"},
					map[string]any{"set_id": "moderator"},
				},
				"message": map[string]any{"text": "!clear"},
			},
		},
	}, processor, interactions.Rules{})

	if len(eventsOut) != 3 {
		t.Fatalf("event count=%d, want 3", len(eventsOut))
	}
	if got := eventsOut[1].Data["isModerator"]; got != true {
		t.Fatalf("expected moderator badge to be recognized, got %#v", eventsOut[1].Data)
	}
}

func TestEventsForNotificationBroadcasterByUserID(t *testing.T) {
	processor := NewCommandProcessor(CommandProcessorConfig{Cooldown: 0, Now: func() time.Time { return time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC) }})
	eventsOut := EventsForNotification(EventSubMessage{
		Metadata: EventSubMetadata{MessageID: "msg-broadcaster"},
		Payload: EventSubPayload{
			Subscription: EventSubSubscription{Type: "channel.chat.message"},
			Event: map[string]any{
				"chatter_user_name":   "Broadcaster",
				"chatter_user_login":  "streamer",
				"chatter_user_id":     "900",
				"broadcaster_user_id": "900",
				"badges":              []any{},
				"message":             map[string]any{"text": "!back"},
			},
		},
	}, processor, interactions.Rules{})

	if len(eventsOut) != 3 {
		t.Fatalf("event count=%d, want 3", len(eventsOut))
	}
	if got := eventsOut[1].Data["isBroadcaster"]; got != true {
		t.Fatalf("expected broadcaster to be recognized, got %#v", eventsOut[1].Data)
	}
	if eventsOut[2].Type != events.EventTypeBRB || eventsOut[2].Data["active"] != false {
		t.Fatalf("expected inactive BRB action, got %#v", eventsOut[2])
	}
}

func TestEventsForChannelPointRedemptionRules(t *testing.T) {
	message := EventSubMessage{
		Metadata: EventSubMetadata{MessageID: "msg-redemption"},
		Payload: EventSubPayload{
			Subscription: EventSubSubscription{Type: "channel.channel_points_custom_reward_redemption.add"},
			Event: map[string]any{
				"user_name":  "Viewer",
				"user_input": "make it red",
				"reward":     map[string]any{"title": "Change color"},
			},
		},
	}

	t.Run("unmatched keeps alert", func(t *testing.T) {
		eventsOut := EventsForNotification(message, nil, interactions.Rules{})
		if len(eventsOut) != 1 || eventsOut[0].Type != events.EventTypeAlert || eventsOut[0].Title != "Viewer redeemed channel points" {
			t.Fatalf("unexpected events: %#v", eventsOut)
		}
		if eventsOut[0].Message != "Change color: make it red" {
			t.Fatalf("unexpected unmatched message: %#v", eventsOut[0])
		}
	})

	t.Run("alert formats templates", func(t *testing.T) {
		rules := interactions.Rules{ChannelPointRules: []interactions.ChannelPointRule{{RewardTitle: "change color", Action: "alert", Title: "Thanks {user}", Message: "{reward} -> {input}", Enabled: true}}}
		eventsOut := EventsForNotification(message, nil, rules)
		if len(eventsOut) != 1 || eventsOut[0].Type != events.EventTypeAlert {
			t.Fatalf("unexpected events: %#v", eventsOut)
		}
		if eventsOut[0].Title != "Thanks Viewer" || eventsOut[0].Message != "Change color -> make it red" {
			t.Fatalf("unexpected formatted event: %#v", eventsOut[0])
		}
	})

	t.Run("ignore suppresses alert", func(t *testing.T) {
		rules := interactions.Rules{ChannelPointRules: []interactions.ChannelPointRule{{RewardTitle: "CHANGE COLOR", Action: "ignore", Enabled: true}}}
		eventsOut := EventsForNotification(message, nil, rules)
		if len(eventsOut) != 1 {
			t.Fatalf("expected one diagnostic event, got %#v", eventsOut)
		}
		if eventsOut[0].Type != events.EventTypeCommand {
			t.Fatalf("expected diagnostic command event, got %#v", eventsOut[0])
		}
		if eventsOut[0].Title != "Channel point reward ignored" || eventsOut[0].Message != "Matched reward ignored" {
			t.Fatalf("unexpected diagnostic event text: %#v", eventsOut[0])
		}
		if eventsOut[0].Data["action"] != "ignore" || eventsOut[0].Data["reward"] != "Change color" || eventsOut[0].Data["user"] != "Viewer" {
			t.Fatalf("unexpected diagnostic data: %#v", eventsOut[0].Data)
		}
		if _, ok := eventsOut[0].Data["twitchEvent"]; ok {
			t.Fatalf("diagnostic data should not include raw twitchEvent: %#v", eventsOut[0].Data)
		}
	})

	t.Run("brb emits active state", func(t *testing.T) {
		rules := interactions.Rules{ChannelPointRules: []interactions.ChannelPointRule{{RewardTitle: "Change Color", Action: "brb", Title: "BRB {user}", Message: "{reward}: {input}", Enabled: true}}}
		eventsOut := EventsForNotification(message, nil, rules)
		if len(eventsOut) != 1 || eventsOut[0].Type != events.EventTypeBRB {
			t.Fatalf("unexpected events: %#v", eventsOut)
		}
		if eventsOut[0].Data["active"] != true || eventsOut[0].Title != "BRB Viewer" || eventsOut[0].Message != "Change color: make it red" {
			t.Fatalf("unexpected brb event: %#v", eventsOut[0])
		}
	})

	t.Run("cooldown emits diagnostic", func(t *testing.T) {
		channelPointCooldowns = sync.Map{}
		rules := interactions.Rules{ChannelPointRules: []interactions.ChannelPointRule{{RewardTitle: "Change Color", Action: "alert", CooldownSeconds: 60, FulfillmentNote: "check after stream", Enabled: true}}}
		first := EventsForNotification(message, nil, rules)
		second := EventsForNotification(message, nil, rules)
		if len(first) != 1 || first[0].Type != events.EventTypeAlert {
			t.Fatalf("expected first redemption alert, got %#v", first)
		}
		if len(second) != 1 || second[0].Type != events.EventTypeCommand || second[0].Data["action"] != "cooldown" {
			t.Fatalf("expected cooldown diagnostic, got %#v", second)
		}
	})

	t.Run("clear emits clear event", func(t *testing.T) {
		rules := interactions.Rules{ChannelPointRules: []interactions.ChannelPointRule{{RewardTitle: "Change Color", Action: "clear", Enabled: true}}}
		eventsOut := EventsForNotification(message, nil, rules)
		if len(eventsOut) != 1 || eventsOut[0].Type != events.EventTypeClear {
			t.Fatalf("unexpected events: %#v", eventsOut)
		}
	})

	t.Run("case insensitive exact match", func(t *testing.T) {
		rules := interactions.Rules{ChannelPointRules: []interactions.ChannelPointRule{{RewardTitle: "change color", Action: "ignore", Enabled: true}}}
		matched, ok := interactions.MatchChannelPointRule(rules, "CHANGE COLOR")
		if !ok || matched.Action != "ignore" {
			t.Fatalf("expected case-insensitive exact match, got %#v %v", matched, ok)
		}
	})
}

func TestRunSessionUsesLatestInteractionRulesSnapshot(t *testing.T) {
	var mu sync.Mutex
	rules := interactions.Rules{ChannelPointRules: []interactions.ChannelPointRule{{RewardTitle: "Hydrate", Action: "alert", Title: "First {user}", Message: "{reward}", Enabled: true}}}
	allowSecond := make(chan struct{})
	published := make([]events.Event, 0, 2)

	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		mustWriteEventSubMessage(t, conn, EventSubMessage{Metadata: EventSubMetadata{MessageType: "session_welcome"}, Payload: EventSubPayload{Session: EventSubSession{ID: "session-1", KeepaliveTimeoutSeconds: 30}}})
		mustWriteEventSubMessage(t, conn, EventSubMessage{Metadata: EventSubMetadata{MessageID: "msg-1", MessageType: "notification"}, Payload: EventSubPayload{Subscription: EventSubSubscription{Type: "channel.channel_points_custom_reward_redemption.add"}, Event: map[string]any{"user_name": "Viewer", "user_input": "first", "reward": map[string]any{"title": "Hydrate"}}}})
		<-allowSecond
		mustWriteEventSubMessage(t, conn, EventSubMessage{Metadata: EventSubMetadata{MessageID: "msg-2", MessageType: "notification"}, Payload: EventSubPayload{Subscription: EventSubSubscription{Type: "channel.channel_points_custom_reward_redemption.add"}, Event: map[string]any{"user_name": "Viewer", "user_input": "second", "reward": map[string]any{"title": "Hydrate"}}}})
	}))
	defer server.Close()

	client := Client{
		Dialer: websocket.DefaultDialer,
		Publish: func(event events.Event) {
			mu.Lock()
			published = append(published, event)
			mu.Unlock()
		},
		InteractionRules: func() interactions.Rules {
			mu.Lock()
			defer mu.Unlock()
			return rules
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- client.runSession(ctx, Token{}, strings.Replace(server.URL, "http", "ws", 1), false)
	}()

	deadline := time.After(time.Second)
	for {
		mu.Lock()
		count := len(published)
		mu.Unlock()
		if count >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for first event")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	mu.Lock()
	rules = interactions.Rules{ChannelPointRules: []interactions.ChannelPointRule{{RewardTitle: "Hydrate", Action: "ignore", Enabled: true}}}
	mu.Unlock()
	close(allowSecond)

	select {
	case err := <-done:
		if err == nil {
			// websocket closure is expected to stop the session with an error
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for session to finish")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(published) != 2 {
		t.Fatalf("expected latest rules snapshot to emit diagnostic for ignored redemption, got %#v", published)
	}
	if published[0].Title != "First Viewer" {
		t.Fatalf("unexpected first event: %#v", published[0])
	}
	if published[1].Type != events.EventTypeCommand || published[1].Severity != events.SeverityWarning {
		t.Fatalf("unexpected second event: %#v", published[1])
	}
	if published[1].Data["action"] != "ignore" || published[1].Data["reward"] != "Hydrate" || published[1].Data["user"] != "Viewer" {
		t.Fatalf("unexpected ignored redemption diagnostics: %#v", published[1].Data)
	}
	if _, ok := published[1].Data["twitchEvent"]; ok {
		t.Fatalf("ignored redemption diagnostics should not include raw twitchEvent: %#v", published[1].Data)
	}
}

func mustWriteEventSubMessage(t *testing.T, conn *websocket.Conn, message EventSubMessage) {
	t.Helper()
	if err := conn.WriteJSON(message); err != nil {
		panic(err)
	}
}
