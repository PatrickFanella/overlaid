package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
		{ID: "event-1", Type: events.EventTypeChatMessage, Source: "twitch", Title: "Viewer", Message: "please BUY NOW", CreatedAt: time.Now().UTC(), Data: map[string]any{"twitchEvent": map[string]any{"message_id": "msg-1", "chatter_user_id": "user-42"}}},
		{ID: "event-2", Type: events.EventTypeAlert, Source: "twitch", Title: "Ignore", Message: "buy now", CreatedAt: time.Now().UTC()},
	}}
	matches, err := server.planNukeDryRun(botNukeDryRunRequest{Pattern: "buy now", LookbackMinutes: 10})
	if err != nil {
		t.Fatalf("plan nuke dry run: %v", err)
	}
	if len(matches) != 1 || matches[0].EventID != "event-1" || matches[0].MessageID != "msg-1" {
		t.Fatalf("unexpected matches: %#v", matches)
	}
	if matches[0].UserID != "user-42" {
		t.Fatalf("expected twitch user id from event data, got %#v", matches[0])
	}
}

func TestBotModerationNukeExecuteRequiresConfirm(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*"}, events.NewBus(4))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/bot/moderation/nuke/execute", strings.NewReader(`{"pattern":"buy now"}`))

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !strings.Contains(got, "confirm is required") {
		t.Fatalf("expected confirm error, got %q", got)
	}
}

func TestBotModerationNukeExecuteRejectsInvalidAction(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*"}, events.NewBus(4))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/bot/moderation/nuke/execute", strings.NewReader(`{"pattern":"buy now","confirm":true,"action":"explode"}`))

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !strings.Contains(got, "nuke action must be delete or timeout") {
		t.Fatalf("expected invalid action error, got %q", got)
	}
}

func TestBotModerationNukeExecuteDefaultsTimeoutDuration(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client-id"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"moderator:manage:banned_users"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123", Login: "broadcaster"}
	server.botRuntime = nil
	server.history = eventHistoryListStub{events: []events.Event{{ID: "event-1", Type: events.EventTypeChatMessage, Source: "twitch", Title: "Viewer", Message: "buy now", CreatedAt: time.Now().UTC(), Data: map[string]any{"twitchEvent": map[string]any{"message_id": "msg-1", "chatter_user_id": "user-42"}}}}}

	var captured map[string]any
	oldClient := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", req.Method)
		}
		if req.URL.Path != "/helix/moderation/bans" {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("decode moderation body: %v", err)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Header: make(http.Header), Request: req}, nil
	})}
	defer func() { twitchHTTPClient = oldClient }()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/bot/moderation/nuke/execute", strings.NewReader(`{"pattern":"buy now","confirm":true,"action":"timeout"}`))

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := captured["data"].(map[string]any)["duration"]; got != float64(60) {
		t.Fatalf("expected timeout duration 60, got %#v", captured)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
