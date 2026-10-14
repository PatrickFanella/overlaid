package twitch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/interactions"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"github.com/gorilla/websocket"
)

func retainedMessage() EventSubMessage {
	return EventSubMessage{Metadata: EventSubMetadata{MessageID: "delivery-a", MessageType: "notification", MessageTimestamp: time.Now().UTC()}, Payload: EventSubPayload{Subscription: EventSubSubscription{Type: "channel.chat.message"}, Event: map[string]any{"message_id": "native-chat", "chatter_user_id": "viewer", "chatter_user_name": "Viewer", "chatter_user_login": "viewer", "broadcaster_user_id": "channel", "message": map[string]any{"text": "!count hype"}, "badges": []any{map[string]any{"set_id": "moderator", "version": "1"}}, "private_extension": "excluded"}}}
}

func TestRetainedNormalizationAndSharedProcessorParity(t *testing.T) {
	m := retainedMessage()
	connection := platform.Connection{TenantID: "tenant", ID: "twitch:channel", Platform: "twitch"}
	normalized, err := NormalizeChatIngest(connection, m, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	m.Metadata.MessageID = "another-subscription-delivery"
	duplicate, err := NormalizeChatIngest(connection, m, time.Now().Add(time.Second))
	if err != nil || duplicate.ID != normalized.ID {
		t.Fatal("native chat redelivery changed identity", err)
	}
	legacyStore, retainedStore := &fakeCommunityStore{}, &fakeCommunityStore{}
	legacy := EventsForNotification(m, NewCommandProcessor(CommandProcessorConfig{Community: legacyStore}), interactions.Rules{})
	got := EventsForNormalizedChat(normalized, NewCommandProcessor(CommandProcessorConfig{Community: retainedStore}), interactions.Rules{})
	if len(got) != len(legacy) || retainedStore.counter.Value != legacyStore.counter.Value || retainedStore.counter.Value != 1 {
		t.Fatal("processor parity failed", len(got), len(legacy))
	}
	for i := range got {
		if got[i].Type != legacy[i].Type || got[i].Title != legacy[i].Title || got[i].Message != legacy[i].Message {
			t.Fatalf("projection mismatch %#v %#v", got[i], legacy[i])
		}
		if got[i].Data["viewerId"] != "viewer" {
			t.Fatal("viewer identity lost")
		}
	}
	if raw := got[0].Data["twitchEvent"].(map[string]any); raw["private_extension"] != nil {
		t.Fatal("extension entered legacy bridge")
	}
}

func TestChatIngestInterceptionDoesNotExecuteLegacyOnFailure(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		mustWriteEventSubMessage(t, conn, EventSubMessage{Metadata: EventSubMetadata{MessageType: "session_welcome"}, Payload: EventSubPayload{Session: EventSubSession{ID: "session", KeepaliveTimeoutSeconds: 30}}})
		mustWriteEventSubMessage(t, conn, retainedMessage())
		_, _, _ = conn.ReadMessage()
	}))
	defer server.Close()
	community := &fakeCommunityStore{}
	published, ingested := 0, 0
	client := Client{Dialer: websocket.DefaultDialer, CommandProcessor: NewCommandProcessor(CommandProcessorConfig{Community: community}), Publish: func(events.Event) { published++ }, ChatIngest: func(context.Context, EventSubMessage, Token) error {
		ingested++
		return errors.New("PRIVATE ingest failure")
	}}
	ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	err := client.runSession(ctx, Token{}, strings.Replace(server.URL, "http", "ws", 1), false)
	if err == nil || strings.Contains(err.Error(), "PRIVATE") || ingested != 1 || published != 0 || community.counter.Value != 0 {
		t.Fatal("ingest failure fell back or leaked", err, ingested, published, community.counter.Value)
	}
}
