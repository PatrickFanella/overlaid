package twitch

import (
	"testing"
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
