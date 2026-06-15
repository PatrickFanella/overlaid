package httpapi

import (
	"context"
	"errors"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

type eventHistoryListStub struct{ events []events.Event }

func (h eventHistoryListStub) Load() error                   { return nil }
func (h eventHistoryListStub) Append(events.Event) error     { return nil }
func (h eventHistoryListStub) List(limit int) []events.Event { return h.events }

func TestTwitchModerationRequiresScope(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client-id"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"user:read:chat"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123", Login: "broadcaster"}

	_, err := server.executeTwitchModerationAction(context.Background(), botModerationActionRequest{Action: "delete", MessageID: "msg-1"})
	if !errors.Is(err, errTwitchModerationMissingScope) {
		t.Fatalf("expected missing scope error, got %v", err)
	}
}

func TestTwitchModerationValidatesInputsBeforeHelix(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client-id"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"moderator:manage:banned_users", "moderator:manage:chat_messages"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123", Login: "broadcaster"}

	if _, err := server.executeTwitchModerationAction(context.Background(), botModerationActionRequest{Action: "delete"}); err == nil {
		t.Fatal("expected missing message id error")
	}
	if _, err := server.executeTwitchModerationAction(context.Background(), botModerationActionRequest{Action: "timeout", TargetUserID: "456", DurationSeconds: 0}); err == nil {
		t.Fatal("expected invalid timeout duration error")
	}
}

func TestPlanNukeDryRunMatchesRecentChatHistory(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*"}, events.NewBus(4))
	server.history = eventHistoryListStub{events: []events.Event{
		{ID: "event-1", Type: events.EventTypeChatMessage, Source: "twitch", Title: "Viewer", Message: "please BUY NOW", CreatedAt: time.Now().UTC(), Data: map[string]any{"twitchEvent": map[string]any{"message_id": "msg-1"}}},
		{ID: "event-2", Type: events.EventTypeAlert, Source: "twitch", Title: "Ignore", Message: "buy now", CreatedAt: time.Now().UTC()},
	}}
	matches, err := server.planNukeDryRun(botNukeDryRunRequest{Pattern: "buy now", LookbackMinutes: 10})
	if err != nil {
		t.Fatalf("plan nuke dry run: %v", err)
	}
	if len(matches) != 1 || matches[0].EventID != "event-1" || matches[0].MessageID != "msg-1" {
		t.Fatalf("unexpected matches: %#v", matches)
	}
}
