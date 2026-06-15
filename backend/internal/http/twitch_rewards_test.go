package httpapi

import (
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

func TestTwitchRewardsRequireRedemptionScope(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"user:read:chat"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123", Login: "broadcaster"}

	_, err := server.listTwitchRewards(t.Context(), true)
	if err == nil || !strings.Contains(err.Error(), "channel:read:redemptions") {
		t.Fatalf("expected missing redemption scope, got %v", err)
	}
}

func TestTwitchRedemptionStatusRequiresManageScope(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"channel:read:redemptions"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123", Login: "broadcaster"}

	err := server.updateTwitchRedemptionStatus(t.Context(), "reward", "redemption", "FULFILLED")
	if err == nil || !strings.Contains(err.Error(), "channel:manage:redemptions") {
		t.Fatalf("expected missing manage scope, got %v", err)
	}
}
