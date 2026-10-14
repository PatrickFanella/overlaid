package twitch

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
)

func TestNormalizeChatNotification(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	message := EventSubMessage{
		Metadata: EventSubMetadata{MessageID: "delivery-1", MessageType: "notification", MessageTimestamp: now},
		Payload: EventSubPayload{Subscription: EventSubSubscription{Type: "channel.chat.message"}, Event: map[string]any{
			"message_id": "chat-1", "chatter_user_id": "viewer", "chatter_user_login": "viewer_login", "chatter_user_name": "Viewer",
			"broadcaster_user_id": "channel", "broadcaster_user_name": "Channel", "color": "#123456",
			"message": map[string]any{"text": "hello"}, "reply": map[string]any{"parent_message_id": "parent-1"},
			"badges": []any{map[string]any{"set_id": "moderator", "version": "1"}}, "future_private_field": "private",
		}},
	}
	connection := platform.Connection{TenantID: "tenant", ID: "connection", Platform: "twitch", CredentialRef: "private-vault-reference"}
	event, err := NormalizeChatNotification(connection, message, now)
	if err != nil {
		t.Fatal(err)
	}
	if event.Chat.MessageID != "chat-1" || event.NativeEventID != "delivery-1" || event.Chat.ReplyToID != "parent-1" || event.Chat.Color != "#123456" || event.Chat.Badges[0].Version != "1" || event.Chat.Actor.Roles[0] != "moderator" {
		t.Fatalf("lost normalized fields: %#v", event)
	}
	duplicate, err := NormalizeChatNotification(connection, message, now.Add(time.Minute))
	if err != nil || duplicate.ID != event.ID {
		t.Fatalf("redelivery changed identity: %v", err)
	}
	encoded, err := json.Marshal(event)
	if err != nil || strings.Contains(string(encoded), "private") {
		t.Fatalf("raw data or credential reference entered envelope: %s %v", encoded, err)
	}
	legacy, ok := MapNotification(message)
	if !ok || legacy.ID != "delivery-1" || legacy.Title != event.Chat.Actor.DisplayName || legacy.Message != event.Chat.Text || legacy.Data["viewerId"] != "viewer" || legacy.Data["viewerLogin"] != "viewer_login" {
		t.Fatalf("legacy projection changed: %#v", legacy)
	}
	if _, leaked := legacy.Data["tenant_id"]; leaked {
		t.Fatal("internal envelope projected into legacy event")
	}
	message.Metadata.MessageID = ""
	if _, err := NormalizeChatNotification(connection, message, now); err == nil {
		t.Fatal("missing native identity accepted")
	}
	message.Metadata.MessageID = "delivery-1"
	message.Metadata.MessageTimestamp = time.Time{}
	if _, err := NormalizeChatNotification(connection, message, now); err == nil {
		t.Fatal("missing occurrence timestamp accepted")
	}
	message.Metadata.MessageTimestamp = now
	message.Payload.Subscription.Type = "stream.online"
	if _, err := NormalizeChatNotification(connection, message, now); err == nil {
		t.Fatal("wrong event kind accepted")
	}
	message.Payload.Subscription.Type = "channel.chat.message"
	connection.Platform = "kick"
	if _, err := NormalizeChatNotification(connection, message, now); err == nil {
		t.Fatal("wrong platform accepted")
	}
}
