package twitch

import (
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
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
	}, processor)

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
	}, processor)

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
	}, processor)

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
	}, processor)

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
