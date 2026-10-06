package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

func TestManualModerationScopePayloadAndAuditContracts(t *testing.T) {
	cases := []struct {
		action, scope, method, path string
		status                      int
	}{
		{"delete", "moderator:manage:chat_messages", http.MethodDelete, "/helix/moderation/chat", http.StatusNoContent},
		{"timeout", "moderator:manage:banned_users", http.MethodPost, "/helix/moderation/bans", http.StatusOK},
		{"ban", "moderator:manage:banned_users", http.MethodPost, "/helix/moderation/bans", http.StatusOK},
		{"unban", "moderator:manage:banned_users", http.MethodDelete, "/helix/moderation/bans", http.StatusNoContent},
	}
	for _, tc := range cases {
		for _, outcome := range []string{"accepted", "remote_failure", "missing_scope", "operator_denied"} {
			t.Run(tc.action+"/"+outcome, func(t *testing.T) {
				server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client-id", OperatorSecret: "operator-fixture"}, events.NewBus(4))
				recorder := &recordingBotAuditStore{}
				server.botRuntime = bot.NewRuntime(recorder)
				server.history = activeHistoryStub{sessionID: "session-1"}
				scopes := []string{tc.scope}
				if outcome == "missing_scope" {
					scopes = []string{"user:read:chat"}
				}
				server.twitch.token = &twitchTokenInfo{AccessToken: "access-fixture", Scope: scopes, ExpiresAt: time.Now().Add(time.Hour), UserID: "123"}
				calls := 0
				oldClient := twitchHTTPClient
				twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					calls++
					if req.Method != tc.method || req.URL.Path != tc.path || req.URL.Host != "api.twitch.tv" {
						t.Fatalf("unexpected endpoint: %s %s", req.Method, req.URL)
					}
					query := req.URL.Query()
					if query.Get("broadcaster_id") != "123" || query.Get("moderator_id") != "123" {
						t.Fatalf("actor query = %v", query)
					}
					if req.Header.Get("Authorization") != "Bearer access-fixture" || req.Header.Get("Client-Id") != "client-id" {
						t.Fatal("missing Helix authentication")
					}
					switch tc.action {
					case "delete":
						if query.Get("message_id") != "message-1" || req.Body != nil {
							t.Fatalf("single-message delete = %v", query)
						}
					case "unban":
						if query.Get("user_id") != "456" || req.Body != nil {
							t.Fatalf("unban target = %v", query)
						}
					default:
						var payload map[string]any
						if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
							t.Fatal(err)
						}
						expected := map[string]any{"data": map[string]any{"user_id": "456", "reason": "fixture reason"}}
						if tc.action == "timeout" {
							expected["data"].(map[string]any)["duration"] = float64(60)
						}
						if !reflect.DeepEqual(payload, expected) {
							t.Fatalf("payload = %#v, want %#v", payload, expected)
						}
					}
					status := tc.status
					if outcome == "remote_failure" {
						status = http.StatusForbidden
					}
					return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
				})}
				t.Cleanup(func() { twitchHTTPClient = oldClient })
				body, err := json.Marshal(botModerationActionRequest{Action: " " + strings.ToUpper(tc.action) + " ", TargetUserID: " 456 ", TargetUserLogin: " viewer ", MessageID: " message-1 ", DurationSeconds: 60, Reason: " fixture reason "})
				if err != nil {
					t.Fatal(err)
				}
				req := httptest.NewRequest(http.MethodPost, "/api/bot/moderation/action", bytes.NewReader(body))
				if outcome != "operator_denied" {
					req.Header.Set("X-Overlay-Operator-Secret", "operator-fixture")
				}
				rec := httptest.NewRecorder()
				server.Handler().ServeHTTP(rec, req)
				wantStatus := http.StatusOK
				switch outcome {
				case "operator_denied":
					wantStatus = http.StatusUnauthorized
				case "missing_scope":
					wantStatus = http.StatusServiceUnavailable
				case "remote_failure":
					wantStatus = http.StatusBadRequest
				}
				if rec.Code != wantStatus {
					t.Fatalf("status = %d, want %d: %s", rec.Code, wantStatus, rec.Body.String())
				}
				wantCalls := 1
				if outcome == "operator_denied" || outcome == "missing_scope" {
					wantCalls = 0
				}
				if calls != wantCalls {
					t.Fatalf("remote calls = %d, want %d", calls, wantCalls)
				}
				if outcome == "operator_denied" {
					if len(recorder.events) != 0 {
						t.Fatal("unauthorized request reached workflow audit")
					}
					return
				}
				if len(recorder.events) != 1 {
					t.Fatalf("audits = %#v", recorder.events)
				}
				audit := recorder.events[0]
				wantResult := "allowed"
				if outcome != "accepted" {
					wantResult = "failed"
				}
				if audit.Source != "dashboard" || audit.ActorRole != "operator" || audit.StreamSessionID != "session-1" || audit.Action != tc.action || audit.Result != wantResult {
					t.Fatalf("audit = %#v", audit)
				}
				var data map[string]any
				if err := json.Unmarshal(audit.Data, &data); err != nil {
					t.Fatal(err)
				}
				wantDuration := float64(0)
				if tc.action == "timeout" {
					wantDuration = 60
				}
				if data["targetUserId"] != "456" || data["targetUserLogin"] != "viewer" || data["messageId"] != "message-1" || data["durationSeconds"] != wantDuration {
					t.Fatalf("effective audit metadata = %#v", data)
				}
				if outcome == "accepted" {
					var receipt botModerationActionResponse
					if err := json.Unmarshal(rec.Body.Bytes(), &receipt); err != nil {
						t.Fatal(err)
					}
					if receipt.Action != tc.action || receipt.Result != "accepted" {
						t.Fatalf("receipt = %#v", receipt)
					}
				}
			})
		}
	}
}

func TestManualModerationRejectsInvalidTargetsAndDurationsWithoutDispatch(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client-id"}, events.NewBus(4))
	server.botRuntime = nil
	server.twitch.token = &twitchTokenInfo{AccessToken: "access-fixture", Scope: []string{"moderator:manage:chat_messages", "moderator:manage:banned_users"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123"}
	oldClient := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid moderation action reached Helix")
		return nil, nil
	})}
	t.Cleanup(func() { twitchHTTPClient = oldClient })
	for _, request := range []botModerationActionRequest{
		{Action: "delete", MessageID: " "},
		{Action: "timeout", TargetUserID: " ", DurationSeconds: 60},
		{Action: "ban", TargetUserID: " "},
		{Action: "unban", TargetUserID: " "},
		{Action: "timeout", TargetUserID: "456", DurationSeconds: 0},
		{Action: "timeout", TargetUserID: "456", DurationSeconds: -1},
		{Action: "timeout", TargetUserID: "456", DurationSeconds: 1209601},
		{Action: "clear"},
	} {
		body, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/bot/moderation/action", bytes.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("request %#v returned %d: %s", request, rec.Code, rec.Body.String())
		}
	}
}
