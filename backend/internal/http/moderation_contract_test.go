package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/history"
)

func TestManualModerationScopePayloadAndAuditContracts(t *testing.T) {
	raw, err := os.ReadFile("testdata/direct-moderation-receipts.json")
	if err != nil {
		t.Fatal(err)
	}
	var receipts map[string]botModerationActionResponse
	if err := json.Unmarshal(raw, &receipts); err != nil {
		t.Fatal(err)
	}
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
				if outcome == "accepted" {
					var actual botModerationActionResponse
					if err := json.Unmarshal(rec.Body.Bytes(), &actual); err != nil {
						t.Fatal(err)
					}
					expected, ok := receipts[tc.action]
					if !ok || actual != expected {
						t.Fatalf("direct HTTP receipt = %#v, shared browser fixture = %#v", actual, expected)
					}
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

func TestModerationConfigurationHTTPAuditReceipts(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*"}, events.NewBus(4))
	server.botStore = newModerationRuleTestStore(t)
	recorder := &recordingBotAuditStore{}
	server.botRuntime = bot.NewRuntime(recorder)
	server.history = activeHistoryStub{sessionID: "session-1"}
	post := func(request botModerationRuleRequest, status int) bot.ModerationRule {
		t.Helper()
		body, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/bot/moderation/rules", bytes.NewReader(body)))
		if rec.Code != status {
			t.Fatalf("save status=%d want=%d: %s", rec.Code, status, rec.Body.String())
		}
		var rule bot.ModerationRule
		if status == http.StatusOK {
			if err := json.Unmarshal(rec.Body.Bytes(), &rule); err != nil {
				t.Fatal(err)
			}
		}
		return rule
	}
	request := botModerationRuleRequest{Name: "spam", Enabled: true, Enforce: true, Pattern: "private pattern content", MatchTarget: "message", Action: "delete", Reason: "private reason content"}
	created := post(request, http.StatusOK)
	request.ID, request.Enabled = created.ID, false
	updated := post(request, http.StatusOK)
	if updated.ID != created.ID || updated.Enabled || updated.Pattern != created.Pattern {
		t.Fatalf("pause = %#v", updated)
	}
	request.Regex, request.Pattern = true, "["
	post(request, http.StatusBadRequest)
	request.ID = ""
	post(request, http.StatusBadRequest)
	request.ID, request.Regex, request.Pattern = "missing", false, "valid"
	post(request, http.StatusNotFound)
	for _, status := range []int{http.StatusNoContent, http.StatusNotFound} {
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/bot/moderation/rules/"+created.ID, nil))
		if rec.Code != status {
			t.Fatalf("delete status=%d want=%d: %s", rec.Code, status, rec.Body.String())
		}
	}
	if len(recorder.events) != 7 {
		t.Fatalf("configuration audits = %#v", recorder.events)
	}
	for i, audit := range recorder.events {
		action := "moderation_update"
		if i == 0 || i == 3 {
			action = "moderation_create"
		}
		if i >= 5 {
			action = "moderation_delete"
		}
		allowed := i == 0 || i == 1 || i == 5
		result, reason := "failed", "moderation_configuration_failed"
		if allowed {
			result, reason = "allowed", "moderation_configuration_changed"
		}
		if audit.Action != action || audit.Trigger != "moderation_configuration" || audit.Source != "dashboard" || audit.ActorRole != "operator" || audit.StreamSessionID != "session-1" || audit.Result != result || audit.Reason != reason {
			t.Fatalf("audit %d = %#v", i, audit)
		}
		var data map[string]any
		if err := json.Unmarshal(audit.Data, &data); err != nil {
			t.Fatal(err)
		}
		id := created.ID
		if i == 3 {
			id = ""
		}
		if i == 4 {
			id = "missing"
		}
		if data["ruleId"] != id {
			t.Fatalf("audit %d ID=%#v", i, data)
		}
		if i == 0 || i == 1 {
			if data["exemptRole"] != "moderator" || data["timeoutSeconds"] != float64(60) || data["enabled"] != (i == 0) || data["enforce"] != true || data["moderationAction"] != "delete" || data["matchTarget"] != "message" || audit.Command != "spam" {
				t.Fatalf("saved metadata %d = %#v", i, audit)
			}
		} else if len(data) != 1 || audit.Command != "" {
			t.Fatalf("failed/delete metadata %d = %#v", i, audit)
		}
		encoded, err := json.Marshal(audit)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "private pattern content") || strings.Contains(string(encoded), "private reason content") {
			t.Fatal("free-form rule content copied into configuration audit")
		}
	}
}

func TestNukeInvalidSettingsCannotCreateRadiationOrDispatch(t *testing.T) {
	for _, request := range []botNukeDryRunRequest{
		{Action: "timeout", DurationSeconds: -1, RadiationMinutes: 5},
		{Action: "timeout", DurationSeconds: 1209601, RadiationMinutes: 5},
		{Action: "delete", RadiationMinutes: -1},
		{Action: "delete", RadiationMinutes: 121},
	} {
		t.Run(fmt.Sprintf("%s/duration_%d/radiation_%d", request.Action, request.DurationSeconds, request.RadiationMinutes), func(t *testing.T) {
			server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client-id"}, events.NewBus(4))
			server.botStore = newModerationRuleTestStore(t)
			server.botRuntime = nil
			server.twitch.token = &twitchTokenInfo{AccessToken: "access-fixture", Scope: []string{"moderator:manage:banned_users", "moderator:manage:chat_messages"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123"}
			server.history = eventHistoryListStub{events: []events.Event{{ID: "chat-1", Type: events.EventTypeChatMessage, Message: "spam", CreatedAt: time.Now().UTC(), Data: map[string]any{"messageId": "message-1", "twitchEvent": map[string]any{"chatter_user_id": "456"}}}}}
			oldClient := twitchHTTPClient
			twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid nuke request reached Helix")
				return nil, nil
			})}
			t.Cleanup(func() { twitchHTTPClient = oldClient })
			request.Pattern, request.Confirm = "spam", true
			body, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/bot/moderation/nuke/execute", bytes.NewReader(body)))
			rules, err := server.botStore.ListModerationRules(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if len(rules) != 0 {
				t.Fatalf("invalid request created active radiation: %#v", rules)
			}
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("invalid settings returned %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestNukeValidExecutionSettingBoundaries(t *testing.T) {
	for _, duration := range []int{0, 1, 1209600} {
		for _, minutes := range []int{0, 1, 120} {
			t.Run(fmt.Sprintf("duration_%d/radiation_%d", duration, minutes), func(t *testing.T) {
				server := NewServer(config.Config{AllowedOrigin: "*"}, events.NewBus(4))
				server.botStore = newModerationRuleTestStore(t)
				server.botRuntime = nil
				server.history = eventHistoryListStub{}
				before := time.Now().UTC()
				response, err := server.executeNukeWorkflow(t.Context(), botNukeDryRunRequest{Pattern: "spam", DurationSeconds: duration, RadiationMinutes: minutes}, " TIMEOUT ")
				if err != nil || response.Action != "timeout" || response.Attempted != 0 {
					t.Fatalf("valid boundary response=%#v err=%v", response, err)
				}
				if minutes == 0 {
					if response.RadiationRule != nil {
						t.Fatal("zero radiation created a rule")
					}
					return
				}
				rule := response.RadiationRule
				earliest := before.Add(time.Duration(minutes) * time.Minute)
				latest := time.Now().UTC().Add(time.Duration(minutes) * time.Minute)
				if rule == nil || rule.ExpiresAt == nil || rule.ExpiresAt.Before(earliest) || rule.ExpiresAt.After(latest) || !rule.Enabled || !rule.Enforce {
					t.Fatalf("radiation boundary rule=%#v", rule)
				}
			})
		}
	}
}

func TestNukePlanRegexCaseLookbackAndHistoryLimit(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*"}, events.NewBus(4))
	eventStore := history.New("", 250)
	server.history = eventStore
	now := time.Now().UTC()
	for _, event := range []events.Event{
		{ID: "old-chat", Type: events.EventTypeChatMessage, Message: "spam1", CreatedAt: now.Add(-2 * time.Hour)},
		{ID: "alert", Type: events.EventTypeAlert, Message: "spam2", CreatedAt: now},
		{ID: "lower", Type: events.EventTypeChatMessage, Message: "spam3", CreatedAt: now, Data: map[string]any{"messageId": "message-lower"}},
		{ID: "upper", Type: events.EventTypeChatMessage, Message: "SPAM4", CreatedAt: now, Data: map[string]any{"messageId": "message-upper"}},
	} {
		if err := eventStore.Append(event); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		request botNukeDryRunRequest
		ids     []string
	}{
		{botNukeDryRunRequest{Pattern: "^spam[0-9]+$", Regex: true}, []string{"upper", "lower"}},
		{botNukeDryRunRequest{Pattern: "^(?-i:SPAM)[0-9]+$", Regex: true}, []string{"upper"}},
		{botNukeDryRunRequest{Pattern: "^spam[0-9]+$", Regex: true, CaseSensitive: true}, []string{"lower"}},
		{botNukeDryRunRequest{Pattern: "spam", Limit: 1}, []string{"upper"}},
		{botNukeDryRunRequest{Pattern: "spam", LookbackMinutes: 180}, []string{"upper", "lower", "old-chat"}},
	} {
		matches, err := server.planNukeDryRun(tc.request)
		if err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, match := range matches {
			ids = append(ids, match.EventID)
		}
		if !reflect.DeepEqual(ids, tc.ids) {
			t.Fatalf("plan %#v IDs=%v want=%v", tc.request, ids, tc.ids)
		}
	}
	if _, err := server.planNukeDryRun(botNukeDryRunRequest{Pattern: "[", Regex: true}); err == nil {
		t.Fatal("invalid regex accepted")
	}
	if got := len(eventStore.List(250)); got != 4 {
		t.Fatalf("dry run changed history count: %d", got)
	}
}
