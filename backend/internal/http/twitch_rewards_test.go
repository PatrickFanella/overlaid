package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
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

func TestTwitchRewardCreateRequiresManageScope(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"channel:read:redemptions"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123", Login: "broadcaster"}

	_, err := server.createTwitchReward(t.Context(), twitchRewardCreateRequest{Title: "Hydrate", Cost: 100})
	if err == nil || !strings.Contains(err.Error(), "channel:manage:redemptions") {
		t.Fatalf("expected missing manage scope, got %v", err)
	}
}

func TestTwitchRewardCreateValidatesInputs(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"channel:manage:redemptions"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123", Login: "broadcaster"}

	_, err := server.createTwitchReward(t.Context(), twitchRewardCreateRequest{Title: "", Cost: 0})
	if err == nil || !strings.Contains(err.Error(), "title") {
		t.Fatalf("expected title validation error, got %v", err)
	}
}

func TestTwitchRewardCreatePostsReward(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client-id"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"channel:manage:redemptions"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123", Login: "broadcaster"}

	var body string
	oldClient := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", req.Method)
		}
		if got := req.URL.Query().Get("broadcaster_id"); got != "123" {
			t.Fatalf("expected broadcaster_id=123, got %q", got)
		}
		data, _ := io.ReadAll(req.Body)
		body = string(data)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"reward-1","title":"Hydrate","cost":100,"is_enabled":true}]}`)), Header: make(http.Header)}, nil
	})}
	t.Cleanup(func() { twitchHTTPClient = oldClient })

	reward, err := server.createTwitchReward(t.Context(), twitchRewardCreateRequest{Title: " Hydrate ", Cost: 100, Enabled: true, UserInputRequired: true})
	if err != nil {
		t.Fatalf("create reward: %v", err)
	}
	if reward.ID != "reward-1" || !reward.Manageable {
		t.Fatalf("unexpected reward: %#v", reward)
	}
	if !strings.Contains(body, `"title":"Hydrate"`) || !strings.Contains(body, `"is_user_input_required":true`) {
		t.Fatalf("unexpected create body: %s", body)
	}
}

func TestTwitchRewardDeleteRequiresConfirm(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*"}, events.NewBus(4))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/twitch/rewards/reward-1", strings.NewReader(`{}`))

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "confirm is required") {
		t.Fatalf("expected confirm error, got %q", rec.Body.String())
	}
}

func TestTwitchRewardDeleteCallsHelix(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client-id"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"channel:manage:redemptions"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123", Login: "broadcaster"}

	oldClient := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodDelete {
			t.Fatalf("expected DELETE, got %s", req.Method)
		}
		if got := req.URL.Query().Get("id"); got != "reward-1" {
			t.Fatalf("expected reward id, got %q", got)
		}
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader(``)), Header: make(http.Header)}, nil
	})}
	t.Cleanup(func() { twitchHTTPClient = oldClient })

	err := server.deleteTwitchReward(t.Context(), "reward-1")
	if err != nil {
		t.Fatalf("delete reward: %v", err)
	}
}
