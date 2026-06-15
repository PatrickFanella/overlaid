package httpapi

import (
	"context"
	"errors"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

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
