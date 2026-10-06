package httpapi

import (
	"encoding/json"
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

func TestTwitchRewardCreatePostsAdvancedLimits(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client-id"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"channel:manage:redemptions"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123", Login: "broadcaster"}

	var body string
	oldClient := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		data, _ := io.ReadAll(req.Body)
		body = string(data)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"reward-1","title":"Hydrate","cost":100,"is_global_cooldown_enabled":true,"global_cooldown_seconds":60,"is_max_per_stream_enabled":true,"max_per_stream":3,"is_max_per_user_per_stream_enabled":true,"max_per_user_per_stream":1}]}`)), Header: make(http.Header)}, nil
	})}
	t.Cleanup(func() { twitchHTTPClient = oldClient })

	reward, err := server.createTwitchReward(t.Context(), twitchRewardCreateRequest{Title: "Hydrate", Cost: 100, GlobalCooldownSeconds: 60, MaxPerStream: 3, MaxPerUserPerStream: 1})
	if err != nil {
		t.Fatalf("create reward: %v", err)
	}
	if reward.GlobalCooldownSeconds != 60 || reward.MaxPerStream != 3 || reward.MaxPerUserPerStream != 1 {
		t.Fatalf("decoded advanced limits = %#v", reward)
	}
	for _, want := range []string{`"is_global_cooldown_enabled":true`, `"global_cooldown_seconds":60`, `"is_max_per_stream_enabled":true`, `"max_per_stream":3`, `"is_max_per_user_per_stream_enabled":true`, `"max_per_user_per_stream":1`} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %s in body: %s", want, body)
		}
	}
}

func TestTwitchRewardUpdatePatchesAdvancedLimits(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client-id"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"channel:manage:redemptions"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123", Login: "broadcaster"}

	var body string
	oldClient := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		data, _ := io.ReadAll(req.Body)
		body = string(data)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"reward-1","title":"Hydrate","cost":100,"prompt":"Bring water","is_user_input_required":true,"is_global_cooldown_enabled":false,"is_max_per_stream_enabled":true,"max_per_stream":5,"is_max_per_user_per_stream_enabled":false}]}`)), Header: make(http.Header)}, nil
	})}
	t.Cleanup(func() { twitchHTTPClient = oldClient })

	prompt := "Bring water"
	userInputRequired := true
	globalCooldownSeconds := 0
	maxPerStream := 5
	maxPerUserPerStream := 0
	reward, err := server.updateTwitchReward(t.Context(), "reward-1", twitchRewardUpdateRequest{Prompt: &prompt, UserInputRequired: &userInputRequired, GlobalCooldownSeconds: &globalCooldownSeconds, MaxPerStream: &maxPerStream, MaxPerUserPerStream: &maxPerUserPerStream})
	if err != nil {
		t.Fatalf("update reward: %v", err)
	}
	if reward.Prompt != prompt || !reward.UserInputRequired || reward.MaxPerStream != 5 {
		t.Fatalf("decoded updated reward = %#v", reward)
	}
	for _, want := range []string{`"prompt":"Bring water"`, `"is_user_input_required":true`, `"is_global_cooldown_enabled":false`, `"is_max_per_stream_enabled":true`, `"max_per_stream":5`, `"is_max_per_user_per_stream_enabled":false`} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %s in body: %s", want, body)
		}
	}
	if strings.Contains(body, `"global_cooldown_seconds":`) || strings.Contains(body, `"max_per_user_per_stream":`) {
		t.Fatalf("disabled limits should not send value fields: %s", body)
	}
}

func TestTwitchRewardCreateRejectsInvalidAdvancedLimits(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client-id"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"channel:manage:redemptions"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123", Login: "broadcaster"}

	if _, err := server.createTwitchReward(t.Context(), twitchRewardCreateRequest{Title: "Hydrate", Cost: 100, GlobalCooldownSeconds: -1}); err == nil || !strings.Contains(err.Error(), "global cooldown seconds must be zero or greater") {
		t.Fatalf("expected global cooldown validation error, got %v", err)
	}
	if _, err := server.createTwitchReward(t.Context(), twitchRewardCreateRequest{Title: "Hydrate", Cost: 100, MaxPerStream: -1}); err == nil || !strings.Contains(err.Error(), "max per stream must be zero or greater") {
		t.Fatalf("expected max per stream validation error, got %v", err)
	}
	if _, err := server.createTwitchReward(t.Context(), twitchRewardCreateRequest{Title: "Hydrate", Cost: 100, MaxPerUserPerStream: -1}); err == nil || !strings.Contains(err.Error(), "max per user per stream must be zero or greater") {
		t.Fatalf("expected max per user validation error, got %v", err)
	}
	if _, err := server.createTwitchReward(t.Context(), twitchRewardCreateRequest{Title: "Hydrate", Cost: 100, GlobalCooldownSeconds: 30}); err == nil || !strings.Contains(err.Error(), "global cooldown seconds must be 0 or at least 60") {
		t.Fatalf("expected global cooldown minimum error, got %v", err)
	}
}

func TestTwitchRewardUpdateRejectsInvalidAdvancedLimits(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client-id"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"channel:manage:redemptions"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123", Login: "broadcaster"}

	negativeGlobalCooldown := -1
	if _, err := server.updateTwitchReward(t.Context(), "reward-1", twitchRewardUpdateRequest{GlobalCooldownSeconds: &negativeGlobalCooldown}); err == nil || !strings.Contains(err.Error(), "global cooldown seconds must be zero or greater") {
		t.Fatalf("expected global cooldown validation error, got %v", err)
	}
	shortGlobalCooldown := 30
	if _, err := server.updateTwitchReward(t.Context(), "reward-1", twitchRewardUpdateRequest{GlobalCooldownSeconds: &shortGlobalCooldown}); err == nil || !strings.Contains(err.Error(), "global cooldown seconds must be 0 or at least 60") {
		t.Fatalf("expected global cooldown minimum error, got %v", err)
	}
	negativeMaxPerStream := -1
	if _, err := server.updateTwitchReward(t.Context(), "reward-1", twitchRewardUpdateRequest{MaxPerStream: &negativeMaxPerStream}); err == nil || !strings.Contains(err.Error(), "max per stream must be zero or greater") {
		t.Fatalf("expected max per stream validation error, got %v", err)
	}
	negativeMaxPerUser := -1
	if _, err := server.updateTwitchReward(t.Context(), "reward-1", twitchRewardUpdateRequest{MaxPerUserPerStream: &negativeMaxPerUser}); err == nil || !strings.Contains(err.Error(), "max per user per stream must be zero or greater") {
		t.Fatalf("expected max per user validation error, got %v", err)
	}
}

func TestTwitchRewardUpdateRequiresManageScope(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"channel:read:redemptions"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123", Login: "broadcaster"}

	_, err := server.updateTwitchReward(t.Context(), "reward-1", twitchRewardUpdateRequest{Enabled: boolPtr(true)})
	if err == nil || !strings.Contains(err.Error(), "channel:manage:redemptions") {
		t.Fatalf("expected missing manage scope, got %v", err)
	}
}

func TestTwitchRewardUpdateValidatesPatch(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"channel:manage:redemptions"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123", Login: "broadcaster"}

	if _, err := server.updateTwitchReward(t.Context(), "   ", twitchRewardUpdateRequest{Enabled: boolPtr(true)}); err == nil || !strings.Contains(err.Error(), "reward id is required") {
		t.Fatalf("expected reward id validation error, got %v", err)
	}
	if _, err := server.updateTwitchReward(t.Context(), "reward-1", twitchRewardUpdateRequest{}); err == nil || !strings.Contains(err.Error(), "at least one reward field is required") {
		t.Fatalf("expected payload validation error, got %v", err)
	}
}

func TestTwitchRewardUpdatePatchesLifecycle(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client-id"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"channel:manage:redemptions"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123", Login: "broadcaster"}

	var body string
	oldClient := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPatch {
			t.Fatalf("expected PATCH, got %s", req.Method)
		}
		if got := req.URL.Query().Get("broadcaster_id"); got != "123" {
			t.Fatalf("expected broadcaster_id=123, got %q", got)
		}
		if got := req.URL.Query().Get("id"); got != "reward-1" {
			t.Fatalf("expected reward id, got %q", got)
		}
		data, _ := io.ReadAll(req.Body)
		body = string(data)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"reward-1","title":"Hydrate","cost":100,"is_enabled":false,"is_paused":true}]}`)), Header: make(http.Header)}, nil
	})}
	t.Cleanup(func() { twitchHTTPClient = oldClient })

	reward, err := server.updateTwitchReward(t.Context(), " reward-1 ", twitchRewardUpdateRequest{Enabled: boolPtr(false), Paused: boolPtr(true)})
	if err != nil {
		t.Fatalf("update reward: %v", err)
	}
	if reward.ID != "reward-1" || !reward.Manageable || !reward.Paused || reward.Enabled {
		t.Fatalf("unexpected reward: %#v", reward)
	}
	if !strings.Contains(body, `"is_enabled":false`) || !strings.Contains(body, `"is_paused":true`) {
		t.Fatalf("unexpected update body: %s", body)
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

func boolPtr(value bool) *bool {
	return &value
}

// Exercise the Helix boundary through both writes: the maximum is accepted,
// while the next second is rejected before a remote request can be sent.
func TestTwitchRewardCooldownMaximum(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client-id"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"channel:manage:redemptions"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123"}
	calls := 0
	oldClient := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		var payload map[string]any
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["global_cooldown_seconds"] != float64(604800) || payload["is_global_cooldown_enabled"] != true {
			t.Fatalf("cooldown payload = %#v", payload)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"reward-1","is_global_cooldown_enabled":true,"global_cooldown_seconds":604800}]}`)), Header: make(http.Header)}, nil
	})}
	t.Cleanup(func() { twitchHTTPClient = oldClient })
	for _, update := range []bool{false, true} {
		for _, seconds := range []int{604800, 604801} {
			before := calls
			var reward *twitchReward
			var err error
			if update {
				reward, err = server.updateTwitchReward(t.Context(), "reward-1", twitchRewardUpdateRequest{GlobalCooldownSeconds: &seconds})
			} else {
				reward, err = server.createTwitchReward(t.Context(), twitchRewardCreateRequest{Title: "Hydrate", Cost: 100, GlobalCooldownSeconds: seconds})
			}
			if seconds == 604800 {
				if err != nil || reward == nil || reward.GlobalCooldownSeconds != seconds || calls != before+1 {
					t.Fatalf("update=%t maximum: reward=%#v err=%v calls=%d", update, reward, err, calls)
				}
			} else if err == nil || !strings.Contains(err.Error(), "604800 or fewer") || calls != before {
				t.Fatalf("update=%t oversized: err=%v calls=%d", update, err, calls)
			}
		}
	}
}

func TestTwitchRewardUnicodeTextLimits(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"channel:manage:redemptions"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123", Login: "broadcaster"}
	var payload map[string]any
	calls := 0
	oldClient := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"reward-1"}]}`)), Header: make(http.Header)}, nil
	})}
	t.Cleanup(func() { twitchHTTPClient = oldClient })
	for _, character := range []string{"水", "🌊"} {
		t.Run(character, func(t *testing.T) {
			title, prompt := strings.Repeat(character, 45), strings.Repeat(character, 200)
			before := calls
			_, err := server.createTwitchReward(t.Context(), twitchRewardCreateRequest{Title: " " + title + " ", Prompt: " " + prompt + " ", Cost: 100})
			if err != nil || calls != before+1 || payload["title"] != title || payload["prompt"] != prompt {
				t.Fatalf("create boundary: calls=%d payload=%v err=%v", calls, payload, err)
			}
			before = calls
			for _, request := range []twitchRewardCreateRequest{{Title: title + character, Cost: 100}, {Title: title, Prompt: prompt + character, Cost: 100}} {
				if _, err := server.createTwitchReward(t.Context(), request); err == nil {
					t.Fatal("accepted excessive Unicode text")
				}
			}
			if calls != before {
				t.Fatal("invalid create dispatched to Twitch")
			}
			paddedPrompt := " " + prompt + " "
			_, err = server.updateTwitchReward(t.Context(), "reward-1", twitchRewardUpdateRequest{Prompt: &paddedPrompt})
			if err != nil || calls != before+1 || payload["prompt"] != prompt {
				t.Fatalf("update boundary: calls=%d payload=%v err=%v", calls, payload, err)
			}
			before = calls
			longPrompt := prompt + character
			if _, err := server.updateTwitchReward(t.Context(), "reward-1", twitchRewardUpdateRequest{Prompt: &longPrompt}); err == nil {
				t.Fatal("accepted excessive Unicode prompt")
			}
			if calls != before {
				t.Fatal("invalid update dispatched to Twitch")
			}
		})
	}
}
