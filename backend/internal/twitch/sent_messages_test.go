package twitch

import (
	"fmt"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/interactions"
)

func TestSentMessageBounds(t *testing.T) {
	var cache SentMessages
	now := time.Now()
	cache.rememberAt("", now)
	for i := 0; i < sentMessageLimit+20; i++ {
		cache.rememberAt(fmt.Sprint(i), now.Add(time.Duration(i)*time.Millisecond))
	}
	if len(cache.entries) != sentMessageLimit {
		t.Fatalf("cache size=%d", len(cache.entries))
	}
	if cache.containsAt("0", now) {
		t.Fatal("oldest entry not evicted")
	}
	if !cache.containsAt("1043", now.Add(time.Second)) {
		t.Fatal("recent ID missing")
	}
	if cache.containsAt("1043", now.Add(sentMessageTTL+2*time.Second)) || len(cache.entries) != 0 {
		t.Fatal("expired entries retained")
	}
}

func TestSentReplyClearEchoIgnoredBeforeCommands(t *testing.T) {
	var cache SentMessages
	cache.Remember("sent-id")
	client := Client{CommandProcessor: NewCommandProcessor(CommandProcessorConfig{Custom: []CustomCommand{{Name: "reply", Enabled: true, Response: "!clear", ResponseMode: "chat", RoleRequired: "viewer"}}}), IgnoreChatMessage: cache.Contains}
	message := EventSubMessage{Metadata: EventSubMetadata{MessageID: "event-id"}, Payload: EventSubPayload{Subscription: EventSubSubscription{Type: "channel.chat.message"}, Event: map[string]any{"message_id": "sent-id", "chatter_user_id": "host", "broadcaster_user_id": "host", "chatter_user_name": "Host", "message": map[string]any{"text": "!clear"}}}}
	message.Payload.Event["message_id"] = "request-id"
	message.Payload.Event["message"] = map[string]any{"text": "!reply"}
	response := client.eventsForNotification(message, interactions.Rules{})
	replied := false
	for _, event := range response {
		if chat, ok := event.Data["chatResponse"].(map[string]any); ok && chat["message"] == "!clear" {
			replied = true
		}
	}
	if !replied {
		t.Fatalf("custom command did not produce !clear: %#v", response)
	}
	message.Payload.Event["message_id"] = "sent-id"
	message.Payload.Event["message"] = map[string]any{"text": "!clear"}
	if out := client.eventsForNotification(message, interactions.Rules{}); len(out) != 0 {
		t.Fatalf("echo produced events: %#v", out)
	}
	message.Payload.Event["message_id"] = "manual-id"
	out := client.eventsForNotification(message, interactions.Rules{})
	found := false
	for _, event := range out {
		if event.Type == events.EventTypeClear {
			found = true
		}
	}
	if !found {
		t.Fatalf("manual broadcaster clear did not execute: %#v", out)
	}
}
