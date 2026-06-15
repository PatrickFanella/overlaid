package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/history"
	"git.subcult.tv/PatrickFanella/over-laid/internal/interactions"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
	"github.com/gorilla/websocket"
)

type streamRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
}

type readyzResponse struct {
	Status string        `json:"status"`
	Checks []readyzCheck `json:"checks"`
}

type versionResponse struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuiltAt   string `json:"builtAt"`
	GoVersion string `json:"goVersion"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

type readyzCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Hint   string `json:"hint"`
}

func (r *streamRecorder) Flush() {
	r.flushed = true
}

func TestHealthz(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*"}, events.NewBus(4))
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "ok\n" {
		t.Fatalf("expected ok body, got %q", rec.Body.String())
	}
}

func TestVersion(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*"}, events.NewBus(4))
	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body versionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode version: %v", err)
	}
	if body.Name != "over-laid" {
		t.Fatalf("expected over-laid, got %q", body.Name)
	}
	if body.Version == "" || body.Commit == "" || body.BuiltAt == "" || body.GoVersion == "" || body.OS == "" || body.Arch == "" {
		t.Fatalf("expected non-empty version metadata, got %#v", body)
	}
}

func TestReadyzReportsProcessAndOptionalDependencies(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*"}, events.NewBus(4))
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body readyzResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode readyz: %v", err)
	}
	if body.Status == "" || len(body.Checks) == 0 {
		t.Fatalf("expected readiness status and checks, got %#v", body)
	}
	assertReadyzCheckSet(t, body.Checks, map[string]string{
		"process":             "ok",
		"exposure":            "ok",
		"event_pipeline":      "ok",
		"retained_state":      "ok",
		"event_history":       "ok",
		"twitch_interactions": "ok",
		"twitch_config":       "degraded",
		"twitch_auth":         "degraded",
		"twitch_eventsub":     "degraded",
		"obs_config":          "degraded",
		"obs_reachable":       "degraded",
		"obs_auth":            "degraded",
	})
	if body.Status != "degraded" {
		t.Fatalf("expected degraded overall status, got %#v", body)
	}
}

func TestReadyzReportsMissingSecretsForNonLocalExposure(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", ExposureMode: "public"}, events.NewBus(4))
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body readyzResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode readyz: %v", err)
	}
	if body.Status != "degraded" || !readyzHasCheck(body.Checks, "operator_secret", "degraded") || !readyzHasCheck(body.Checks, "webhook_secret", "degraded") {
		t.Fatalf("expected missing secret degradation, got %#v", body)
	}
}

func TestOBSInventoryRequiresOperatorAuthorization(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", OperatorSecret: "secret"}, events.NewBus(4))

	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/obs/scenes", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without operator secret, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/obs/scenes", nil)
	req.Header.Set("X-Overlay-Operator-Secret", "secret")
	server.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("expected authorized request to reach OBS handler, got 401")
	}
}

func TestReadyzReportsStateLoadFailure(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "overlay-state.json")
	if err := os.WriteFile(statePath, []byte("not-json"), 0o600); err != nil {
		t.Fatalf("write corrupt state: %v", err)
	}
	server := NewServer(config.Config{AllowedOrigin: "*", OverlayStateFile: statePath}, events.NewBus(4))
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 degraded report, got %d: %s", rec.Code, rec.Body.String())
	}

	var body readyzResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode readyz: %v", err)
	}
	if body.Status != "degraded" {
		t.Fatalf("expected degraded status, got %#v", body)
	}
	if !readyzHasCheck(body.Checks, "retained_state", "degraded") {
		t.Fatalf("expected retained_state degraded check, got %#v", body.Checks)
	}
	if !readyzHasCheck(body.Checks, "event_history", "ok") {
		t.Fatalf("expected event_history ok check, got %#v", body.Checks)
	}
}

func TestReadyzReportsHistoryLoadFailure(t *testing.T) {
	historyPath := filepath.Join(t.TempDir(), "event-history.jsonl")
	if err := os.WriteFile(historyPath, []byte("not-json"), 0o600); err != nil {
		t.Fatalf("write corrupt history: %v", err)
	}
	server := NewServer(config.Config{AllowedOrigin: "*", EventHistoryFile: historyPath}, events.NewBus(4))
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 degraded report, got %d: %s", rec.Code, rec.Body.String())
	}

	var body readyzResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode readyz: %v", err)
	}
	if body.Status != "degraded" {
		t.Fatalf("expected degraded status, got %#v", body)
	}
	if !readyzHasCheck(body.Checks, "event_history", "degraded") {
		t.Fatalf("expected event_history degraded check, got %#v", body.Checks)
	}
}

func TestTwitchInteractionsEndpointsAndReadiness(t *testing.T) {
	interactionsPath := filepath.Join(t.TempDir(), "twitch-interactions.json")
	if err := os.WriteFile(interactionsPath, []byte("{invalid"), 0o600); err != nil {
		t.Fatalf("write corrupt interactions: %v", err)
	}

	server := NewServer(config.Config{AllowedOrigin: "*", OperatorSecret: "operator-secret", TwitchInteractionsFile: interactionsPath}, events.NewBus(4))

	getReq := httptest.NewRequest(http.MethodGet, "/api/twitch/interactions", nil)
	getRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", getRec.Code, getRec.Body.String())
	}

	getReq = httptest.NewRequest(http.MethodGet, "/api/twitch/interactions", nil)
	getReq.Header.Set("X-Overlay-Operator-Secret", "operator-secret")
	getRec = httptest.NewRecorder()
	server.Handler().ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", getRec.Code, getRec.Body.String())
	}
	var got twitchInteractionsResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode interactions get: %v", err)
	}
	if got.LoadError == "" {
		t.Fatalf("expected load error, got %#v", got)
	}

	readyReq := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	readyRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(readyRec, readyReq)
	if readyRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", readyRec.Code, readyRec.Body.String())
	}
	var ready readyzResponse
	if err := json.Unmarshal(readyRec.Body.Bytes(), &ready); err != nil {
		t.Fatalf("decode readyz: %v", err)
	}
	if !readyzHasCheck(ready.Checks, "twitch_interactions", "degraded") {
		t.Fatalf("expected twitch_interactions degraded, got %#v", ready.Checks)
	}
	if idx := readyzCheckIndex(ready.Checks, "twitch_interactions"); idx == -1 || idx > readyzCheckIndex(ready.Checks, "twitch_config") {
		t.Fatalf("expected twitch_interactions check before twitch auth checks, got %#v", ready.Checks)
	}

	putReq := httptest.NewRequest(http.MethodPut, "/api/twitch/interactions", bytes.NewBufferString(`{"rules":{"chatAliases":[{"alias":"!panic","command":"clear","enabled":true},{"alias":"!clear","command":"back","enabled":true}]}}`))
	putRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", putRec.Code, putRec.Body.String())
	}

	putReq = httptest.NewRequest(http.MethodPut, "/api/twitch/interactions", bytes.NewBufferString(`{"rules":{"chatAliases":[{"alias":"!panic","command":"clear","enabled":true},{"alias":"!clear","command":"back","enabled":true}]}}`))
	putReq.Header.Set("X-Overlay-Operator-Secret", "operator-secret")
	putRec = httptest.NewRecorder()
	server.Handler().ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", putRec.Code, putRec.Body.String())
	}
	var saved twitchInteractionsResponse
	if err := json.Unmarshal(putRec.Body.Bytes(), &saved); err != nil {
		t.Fatalf("decode interactions put: %v", err)
	}
	if saved.LoadError != "" {
		t.Fatalf("expected cleared load error, got %#v", saved)
	}
	wantRules := interactions.Rules{ChatAliases: []interactions.ChatAliasRule{{Alias: "panic", Command: "clear", Enabled: true}}}
	if len(saved.Rules.ChatAliases) != len(wantRules.ChatAliases) || saved.Rules.ChatAliases[0] != wantRules.ChatAliases[0] {
		t.Fatalf("expected sanitized alias rule, got %#v", saved.Rules.ChatAliases)
	}

	commandEvents := server.twitchCommandProcessor.Handle(twitch.ChatCommand{Name: "panic", Args: "hydrate now"}, twitch.ChatCommandUser{Name: "Mod", IsModerator: true})
	if len(commandEvents) != 2 || commandEvents[1].Type != events.EventTypeClear {
		t.Fatalf("expected live alias update, got %#v", commandEvents)
	}
	if commandEvents[0].Data["command"] != "clear" || commandEvents[0].Data["originalCommand"] != "panic" || commandEvents[0].Data["alias"] != true {
		t.Fatalf("unexpected live alias diagnostics: %#v", commandEvents[0].Data)
	}

	getReq = httptest.NewRequest(http.MethodGet, "/api/twitch/interactions", nil)
	getReq.Header.Set("X-Overlay-Operator-Secret", "operator-secret")
	getRec = httptest.NewRecorder()
	server.Handler().ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", getRec.Code, getRec.Body.String())
	}
	got = twitchInteractionsResponse{}
	if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode interactions get after put: %v", err)
	}
	if got.LoadError != "" {
		t.Fatalf("expected cleared load error on GET, got %#v", got)
	}

	readyRec = httptest.NewRecorder()
	server.Handler().ServeHTTP(readyRec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if err := json.Unmarshal(readyRec.Body.Bytes(), &ready); err != nil {
		t.Fatalf("decode readyz after put: %v", err)
	}
	if !readyzHasCheck(ready.Checks, "twitch_interactions", "ok") {
		t.Fatalf("expected twitch_interactions ok after put, got %#v", ready.Checks)
	}

	optionsReq := httptest.NewRequest(http.MethodOptions, "/api/twitch/interactions", nil)
	optionsRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(optionsRec, optionsReq)
	if got := optionsRec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, "PUT") {
		t.Fatalf("expected PUT in CORS methods, got %q", got)
	}
}

func readyzHasCheck(checks []readyzCheck, name, status string) bool {
	for _, check := range checks {
		if check.Name == name && check.Status == status {
			return true
		}
	}
	return false
}

func readyzCheckIndex(checks []readyzCheck, name string) int {
	for i, check := range checks {
		if check.Name == name {
			return i
		}
	}
	return -1
}

func assertReadyzCheckSet(t *testing.T, checks []readyzCheck, expected map[string]string) {
	t.Helper()
	if len(checks) != len(expected) {
		t.Fatalf("expected %d checks, got %d: %#v", len(expected), len(checks), checks)
	}
	seen := make(map[string]struct{}, len(checks))
	for _, check := range checks {
		if want, ok := expected[check.Name]; !ok {
			t.Fatalf("unexpected check %q: %#v", check.Name, checks)
		} else if check.Status != want {
			t.Fatalf("unexpected status for %q: want %q got %q (%#v)", check.Name, want, check.Status, checks)
		}
		seen[check.Name] = struct{}{}
	}
	for name := range expected {
		if _, ok := seen[name]; !ok {
			t.Fatalf("missing check %q: %#v", name, checks)
		}
	}
}

func TestReadyzReturnsDegradedWhenOBSHelloStalls(t *testing.T) {
	obsServer := newStalledOBSWebSocketServer(t)
	defer obsServer.Close()

	server := NewServer(config.Config{AllowedOrigin: "*", OBSWebSocketURL: wsURL(obsServer.URL)}, events.NewBus(4))
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		server.Handler().ServeHTTP(rec, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("readyz timed out waiting for stalled OBS hello")
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body readyzResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode readyz: %v", err)
	}
	assertReadyzCheckSet(t, body.Checks, map[string]string{
		"process":             "ok",
		"exposure":            "ok",
		"event_pipeline":      "ok",
		"retained_state":      "ok",
		"event_history":       "ok",
		"twitch_interactions": "ok",
		"twitch_config":       "degraded",
		"twitch_auth":         "degraded",
		"twitch_eventsub":     "degraded",
		"obs_config":          "ok",
		"obs_reachable":       "degraded",
		"obs_auth":            "degraded",
	})
	if body.Status != "degraded" {
		t.Fatalf("expected degraded overall status, got %#v", body)
	}
}

func TestReadyzReportsOBSAuthFailureSeparately(t *testing.T) {
	obsServer := newAuthRequiredOBSWebSocketServer(t)
	defer obsServer.Close()

	server := NewServer(config.Config{AllowedOrigin: "*", OBSWebSocketURL: wsURL(obsServer.URL), OBSWebSocketPass: "wrong-password"}, events.NewBus(4))
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		server.Handler().ServeHTTP(rec, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("readyz timed out waiting for OBS auth failure")
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body readyzResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode readyz: %v", err)
	}
	if !readyzHasCheck(body.Checks, "obs_reachable", "ok") || !readyzHasCheck(body.Checks, "obs_auth", "degraded") {
		t.Fatalf("expected obs reachable ok and obs auth degraded, got %#v", body.Checks)
	}
	if !readyzHasCheck(body.Checks, "event_history", "ok") {
		t.Fatalf("expected event_history ok check, got %#v", body.Checks)
	}
	if body.Status != "degraded" {
		t.Fatalf("expected degraded overall status, got %#v", body)
	}
}

func TestOBSInputsReturnsInventory(t *testing.T) {
	obsServer := newFakeOBSWebSocketServer(t, func(requestType string, requestData map[string]any) (map[string]any, error) {
		if requestType != "GetInputList" {
			t.Fatalf("unexpected request type: %s", requestType)
		}
		if len(requestData) != 0 {
			t.Fatalf("expected empty request data, got %#v", requestData)
		}
		return map[string]any{"inputs": []map[string]any{{"inputName": "Mic/Aux", "inputKind": "wasapi_input_capture", "inputUuid": "abc"}}}, nil
	})
	defer obsServer.Close()

	server := NewServer(config.Config{AllowedOrigin: "*", OBSWebSocketURL: wsURL(obsServer.URL)}, events.NewBus(4))
	req := httptest.NewRequest(http.MethodGet, "/api/obs/inputs", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var inputs []struct {
		InputName string `json:"inputName"`
		InputKind string `json:"inputKind"`
		InputUuid string `json:"inputUuid"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &inputs); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if len(inputs) != 1 || inputs[0].InputName != "Mic/Aux" || inputs[0].InputKind != "wasapi_input_capture" || inputs[0].InputUuid != "abc" {
		t.Fatalf("unexpected inputs: %#v", inputs)
	}
}

func TestOBSSceneItemsReturnsInventory(t *testing.T) {
	obsServer := newFakeOBSWebSocketServer(t, func(requestType string, requestData map[string]any) (map[string]any, error) {
		if requestType != "GetSceneItemList" {
			t.Fatalf("unexpected request type: %s", requestType)
		}
		if got := requestData["sceneName"]; got != "Program" {
			t.Fatalf("expected sceneName Program, got %#v", got)
		}
		return map[string]any{"sceneItems": []map[string]any{{"sceneItemId": 7, "sourceName": "Mic/Aux", "sceneItemEnabled": true}}}, nil
	})
	defer obsServer.Close()

	server := NewServer(config.Config{AllowedOrigin: "*", OBSWebSocketURL: wsURL(obsServer.URL)}, events.NewBus(4))
	req := httptest.NewRequest(http.MethodGet, "/api/obs/scene-items?sceneName=Program", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var items []struct {
		SceneItemID      int    `json:"sceneItemId"`
		SourceName       string `json:"sourceName"`
		SceneItemEnabled bool   `json:"sceneItemEnabled"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if len(items) != 1 || items[0].SceneItemID != 7 || items[0].SourceName != "Mic/Aux" || !items[0].SceneItemEnabled {
		t.Fatalf("unexpected scene items: %#v", items)
	}
}

func TestOBSSceneItemsRequiresSceneName(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*"}, events.NewBus(4))
	req := httptest.NewRequest(http.MethodGet, "/api/obs/scene-items", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOBSInputsReturnsBadGatewayOnOBSFailure(t *testing.T) {
	obsServer := newFakeOBSWebSocketServer(t, func(requestType string, requestData map[string]any) (map[string]any, error) {
		return nil, context.Canceled
	})
	defer obsServer.Close()

	server := NewServer(config.Config{AllowedOrigin: "*", OBSWebSocketURL: wsURL(obsServer.URL)}, events.NewBus(4))
	req := httptest.NewRequest(http.MethodGet, "/api/obs/inputs", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOperatorSecretProtectsTestAlert(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", OperatorSecret: "operator-secret"}, events.NewBus(4))
	req := httptest.NewRequest(http.MethodPost, "/api/events/test-alert", bytes.NewBufferString(`{"title":"Nope","message":"Blocked"}`))
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func newFakeOBSWebSocketServer(t *testing.T, handle func(requestType string, requestData map[string]any) (map[string]any, error)) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade failed: %v", err)
		}
		defer conn.Close()

		if err := conn.WriteJSON(map[string]any{"op": 0, "d": map[string]any{"obsStudioVersion": "30.2.0", "obsWebSocketVersion": "5.5.0", "rpcVersion": 1}}); err != nil {
			t.Fatalf("write hello failed: %v", err)
		}

		var identify map[string]any
		if err := conn.ReadJSON(&identify); err != nil {
			t.Fatalf("read identify failed: %v", err)
		}
		if got := identify["op"]; got != float64(1) {
			t.Fatalf("unexpected identify op: %#v", identify)
		}
		if err := conn.WriteJSON(map[string]any{"op": 2, "d": map[string]any{}}); err != nil {
			t.Fatalf("write identified failed: %v", err)
		}

		var request map[string]any
		if err := conn.ReadJSON(&request); err != nil {
			t.Fatalf("read request failed: %v", err)
		}
		payload, _ := request["d"].(map[string]any)
		requestType, _ := payload["requestType"].(string)
		requestID, _ := payload["requestId"].(string)
		requestData, _ := payload["requestData"].(map[string]any)
		responseData, err := handle(requestType, requestData)
		status := map[string]any{"result": true, "code": 100, "comment": "OK"}
		if err != nil {
			status = map[string]any{"result": false, "code": 500, "comment": err.Error()}
			responseData = nil
		}
		response := map[string]any{"op": 7, "d": map[string]any{"requestType": requestType, "requestId": requestID, "requestStatus": status, "responseData": responseData}}
		if err := conn.WriteJSON(response); err != nil {
			t.Fatalf("write response failed: %v", err)
		}
	}))
}

func newStalledOBSWebSocketServer(t *testing.T) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade failed: %v", err)
		}
		defer conn.Close()

		<-r.Context().Done()
	}))
}

func newAuthRequiredOBSWebSocketServer(t *testing.T) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade failed: %v", err)
		}
		defer conn.Close()

		if err := conn.WriteJSON(map[string]any{"op": 0, "d": map[string]any{"obsStudioVersion": "30.2.0", "obsWebSocketVersion": "5.5.0", "rpcVersion": 1, "authentication": map[string]any{"challenge": "challenge", "salt": "salt"}}}); err != nil {
			t.Fatalf("write hello failed: %v", err)
		}

		var identify map[string]any
		if err := conn.ReadJSON(&identify); err != nil {
			t.Fatalf("read identify failed: %v", err)
		}
		if got := identify["op"]; got != float64(1) {
			t.Fatalf("unexpected identify op: %#v", identify)
		}

		<-r.Context().Done()
	}))
}

func wsURL(httpURL string) string {
	return strings.Replace(httpURL, "http://", "ws://", 1)
}

func TestOperatorSecretProtectsMutatingRoutes(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", OperatorSecret: "operator-secret"}, events.NewBus(4))
	tests := []struct {
		name string
		path string
		body string
	}{
		{name: "clear", path: "/api/events/clear"},
		{name: "brb", path: "/api/events/brb", body: `{"active":true,"title":"BRB","message":"Break"}`},
		{name: "obs scene", path: "/api/obs/scene", body: `{"sceneName":"Scene"}`},
		{name: "obs mute", path: "/api/obs/input/mute", body: `{"inputName":"Mic/Aux","muted":true}`},
		{name: "obs item", path: "/api/obs/scene-item/enabled", body: `{"sceneName":"Scene","sceneItemId":1,"enabled":true}`},
		{name: "obs source refresh", path: "/api/obs/source/refresh", body: `{"inputName":"Alerts Browser"}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, test.path, bytes.NewBufferString(test.body))
			rec := httptest.NewRecorder()

			server.Handler().ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestOperatorSecretAllowsBearerToken(t *testing.T) {
	bus := events.NewBus(4)
	client := bus.Subscribe()
	defer bus.Unsubscribe(client.ID)
	server := NewServer(config.Config{AllowedOrigin: "*", OperatorSecret: "operator-secret"}, bus)
	req := httptest.NewRequest(http.MethodPost, "/api/events/test-alert", bytes.NewBufferString(`{"title":"Allowed","message":"OK"}`))
	req.Header.Set("Authorization", "Bearer operator-secret")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	select {
	case got := <-client.Events:
		if got.Title != "Allowed" {
			t.Fatalf("unexpected event: %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestOperatorSecretAllowsHeader(t *testing.T) {
	bus := events.NewBus(4)
	client := bus.Subscribe()
	defer bus.Unsubscribe(client.ID)
	server := NewServer(config.Config{AllowedOrigin: "*", OperatorSecret: "operator-secret"}, bus)
	req := httptest.NewRequest(http.MethodPost, "/api/events/brb", bytes.NewBufferString(`{"active":true,"title":"Allowed","message":"OK"}`))
	req.Header.Set("X-Overlay-Operator-Secret", "operator-secret")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	select {
	case got := <-client.Events:
		if got.Type != events.EventTypeBRB {
			t.Fatalf("unexpected event: %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestOBSRefreshBrowserSourceRequiresOperator(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", OperatorSecret: "operator-secret"}, events.NewBus(4))
	req := httptest.NewRequest(http.MethodPost, "/api/obs/source/refresh", strings.NewReader(`{"inputName":"Alerts Browser"}`))
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOBSRefreshBrowserSource(t *testing.T) {
	obsServer := newFakeOBSWebSocketServer(t, func(requestType string, requestData map[string]any) (map[string]any, error) {
		if requestType != "PressInputPropertiesButton" {
			t.Fatalf("unexpected request type: %s", requestType)
		}
		if got := requestData["inputName"]; got != "Alerts Browser" {
			t.Fatalf("unexpected inputName: %#v", got)
		}
		if got := requestData["propertyName"]; got != "refreshnocache" {
			t.Fatalf("unexpected propertyName: %#v", got)
		}
		return map[string]any{}, nil
	})
	defer obsServer.Close()

	server := NewServer(config.Config{AllowedOrigin: "*", OperatorSecret: "operator-secret", OBSWebSocketURL: wsURL(obsServer.URL)}, events.NewBus(4))
	req := httptest.NewRequest(http.MethodPost, "/api/obs/source/refresh", strings.NewReader(`{"inputName":"Alerts Browser"}`))
	req.Header.Set("Authorization", "Bearer operator-secret")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTestAlertPublishesEvent(t *testing.T) {
	bus := events.NewBus(4)
	client := bus.Subscribe()
	defer bus.Unsubscribe(client.ID)
	server := NewServer(config.Config{AllowedOrigin: "*"}, bus)

	body := bytes.NewBufferString(`{"title":"Hello","message":"From test","severity":"success"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/events/test-alert", body)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	select {
	case got := <-client.Events:
		if got.Type != events.EventTypeAlert || got.Title != "Hello" || got.Source != "manual" {
			t.Fatalf("unexpected event: %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for alert event")
	}
}

func TestClearPublishesEvent(t *testing.T) {
	bus := events.NewBus(4)
	client := bus.Subscribe()
	defer bus.Unsubscribe(client.ID)
	server := NewServer(config.Config{AllowedOrigin: "*"}, bus)

	req := httptest.NewRequest(http.MethodPost, "/api/events/clear", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	select {
	case got := <-client.Events:
		if got.Type != events.EventTypeClear || got.Source != "manual" {
			t.Fatalf("unexpected clear event: %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for clear event")
	}
}

func TestBRBStatusPublishesEvent(t *testing.T) {
	bus := events.NewBus(4)
	client := bus.Subscribe()
	defer bus.Unsubscribe(client.ID)
	server := NewServer(config.Config{AllowedOrigin: "*"}, bus)

	body := bytes.NewBufferString(`{"active":true,"title":"Intermission","message":"Stretch break","eta":"5 min","items":["Hydrate","Ads running"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/events/brb", body)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	select {
	case got := <-client.Events:
		if got.Type != events.EventTypeBRB || got.Title != "Intermission" || got.Source != "dashboard" {
			t.Fatalf("unexpected event: %#v", got)
		}
		if got.Data["active"] != true || got.Data["eta"] != "5 min" {
			t.Fatalf("unexpected brb data: %#v", got.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for brb event")
	}
}

func TestPublishEventSubEventPersistsBRB(t *testing.T) {
	bus := events.NewBus(4)
	client := bus.Subscribe()
	defer bus.Unsubscribe(client.ID)
	server := NewServer(config.Config{AllowedOrigin: "*"}, bus)

	server.publishEventSubEvent(events.NewBRB("twitch", "BRB", "Back", map[string]any{"active": false}))

	select {
	case got := <-client.Events:
		if got.Type != events.EventTypeBRB || got.Source != "twitch" {
			t.Fatalf("unexpected published event: %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for brb event")
	}

	list := server.state.List()
	if len(list) != 1 {
		t.Fatalf("expected one retained event, got %#v", list)
	}
	if list[0].Type != events.EventTypeBRB || list[0].Data["retainedStateKey"] != "brb" || list[0].Data["active"] != false {
		t.Fatalf("unexpected retained brb event: %#v", list[0])
	}
}

func TestPublishEventSubEventClearsRetainedState(t *testing.T) {
	bus := events.NewBus(4)
	client := bus.Subscribe()
	defer bus.Unsubscribe(client.ID)
	server := NewServer(config.Config{AllowedOrigin: "*"}, bus)

	server.publishEventSubEvent(events.NewBRB("twitch", "BRB", "Intermission", map[string]any{"active": true}))
	select {
	case <-client.Events:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for setup brb event")
	}

	server.publishEventSubEvent(events.NewClear("twitch"))

	select {
	case got := <-client.Events:
		if got.Type != events.EventTypeClear || got.Source != "twitch" {
			t.Fatalf("unexpected clear event: %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for clear event")
	}

	if list := server.state.List(); len(list) != 0 {
		t.Fatalf("expected retained state to be cleared, got %#v", list)
	}
}

func TestBRBStatusCanonicalFieldsOverrideData(t *testing.T) {
	bus := events.NewBus(4)
	client := bus.Subscribe()
	defer bus.Unsubscribe(client.ID)
	server := NewServer(config.Config{AllowedOrigin: "*"}, bus)

	body := bytes.NewBufferString(`{"active":false,"title":"Pause","message":"Back soon","eta":"2 min","items":["One","Two","Three","Four","Five","Six"],"data":{"active":true,"eta":"wrong","items":["wrong"]}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/events/brb", body)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	select {
	case got := <-client.Events:
		if got.Data["active"] != false || got.Data["eta"] != "2 min" {
			t.Fatalf("canonical fields were not preserved: %#v", got.Data)
		}
		items, ok := got.Data["items"].([]string)
		if !ok || len(items) != 5 || items[0] != "One" {
			t.Fatalf("expected cleaned item list, got %#v", got.Data["items"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for brb event")
	}
}

func TestStreamEventsReplaysRetainedBRB(t *testing.T) {
	bus := events.NewBus(4)
	server := NewServer(config.Config{AllowedOrigin: "*"}, bus)

	body := bytes.NewBufferString(`{"active":true,"title":"Intermission","message":"Stretch break"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/events/brb", body)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected brb setup 202, got %d: %s", rec.Code, rec.Body.String())
	}

	ctx, cancel := context.WithCancel(context.Background())
	streamReq := httptest.NewRequest(http.MethodGet, "/api/events/stream", nil).WithContext(ctx)
	streamRec := &streamRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		server.Handler().ServeHTTP(streamRec, streamReq)
		close(done)
	}()

	deadline := time.After(time.Second)
	for !bytes.Contains(streamRec.Body.Bytes(), []byte("event: brb")) {
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatalf("expected retained brb replay, got %q", streamRec.Body.String())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	cancel()
	<-done
}

func TestOverlayStatePersistsAcrossServerRestart(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "overlay-state.json")
	bus := events.NewBus(4)
	server := NewServer(config.Config{AllowedOrigin: "*", OverlayStateFile: stateFile}, bus)

	body := bytes.NewBufferString(`{"title":"Goal update","message":"Followers moved","data":{"goal":{"id":"followers","label":"Follower goal","current":430,"target":620}}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/events/test-alert", body)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected stateful alert 202, got %d: %s", rec.Code, rec.Body.String())
	}

	restarted := NewServer(config.Config{AllowedOrigin: "*", OverlayStateFile: stateFile}, events.NewBus(4))
	ctx, cancel := context.WithCancel(context.Background())
	streamReq := httptest.NewRequest(http.MethodGet, "/api/events/stream", nil).WithContext(ctx)
	streamRec := &streamRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		restarted.Handler().ServeHTTP(streamRec, streamReq)
		close(done)
	}()

	deadline := time.After(time.Second)
	for !bytes.Contains(streamRec.Body.Bytes(), []byte("Follower goal")) {
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatalf("expected retained goal replay, got %q", streamRec.Body.String())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !bytes.Contains(streamRec.Body.Bytes(), []byte(`"retainedStateKey":"goals"`)) {
		cancel()
		<-done
		t.Fatalf("expected retained state marker, got %q", streamRec.Body.String())
	}
	cancel()
	<-done
}

func TestStateKeysForEventWithGoalAndTickerAreScoped(t *testing.T) {
	event := events.NewAlert("n8n", "Mixed", "Both", events.SeverityInfo)
	event.Data = map[string]any{
		"goal":   map[string]any{"id": "followers", "current": 10, "target": 20},
		"ticker": map[string]any{"label": "goal", "message": "Followers moved"},
	}

	keys := stateKeysForEvent(event)
	if len(keys) != 2 || keys[0] != "goals" || keys[1] != "ticker" {
		t.Fatalf("unexpected state keys: %#v", keys)
	}

	goalEvent := eventWithRetainedStateKey(event, "goals")
	tickerEvent := eventWithRetainedStateKey(event, "ticker")
	if goalEvent.Data["retainedStateKey"] != "goals" {
		t.Fatalf("expected goals marker, got %#v", goalEvent.Data)
	}
	if tickerEvent.Data["retainedStateKey"] != "ticker" {
		t.Fatalf("expected ticker marker, got %#v", tickerEvent.Data)
	}
	if _, ok := event.Data["retainedStateKey"]; ok {
		t.Fatalf("original event should not be mutated: %#v", event.Data)
	}
}

func TestClearResetsRetainedBRB(t *testing.T) {
	bus := events.NewBus(4)
	server := NewServer(config.Config{AllowedOrigin: "*"}, bus)

	brbReq := httptest.NewRequest(http.MethodPost, "/api/events/brb", bytes.NewBufferString(`{"active":true,"title":"Intermission","message":"Stretch break"}`))
	brbRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(brbRec, brbReq)
	if brbRec.Code != http.StatusAccepted {
		t.Fatalf("expected brb setup 202, got %d: %s", brbRec.Code, brbRec.Body.String())
	}

	clearReq := httptest.NewRequest(http.MethodPost, "/api/events/clear", nil)
	clearRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(clearRec, clearReq)
	if clearRec.Code != http.StatusAccepted {
		t.Fatalf("expected clear 202, got %d: %s", clearRec.Code, clearRec.Body.String())
	}

	ctx, cancel := context.WithCancel(context.Background())
	streamReq := httptest.NewRequest(http.MethodGet, "/api/events/stream", nil).WithContext(ctx)
	streamRec := &streamRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		server.Handler().ServeHTTP(streamRec, streamReq)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	if bytes.Contains(streamRec.Body.Bytes(), []byte("event: brb")) {
		t.Fatalf("expected no retained brb replay after clear, got %q", streamRec.Body.String())
	}
}

func TestWebhookPublishesSourceAlert(t *testing.T) {
	bus := events.NewBus(4)
	client := bus.Subscribe()
	defer bus.Unsubscribe(client.ID)
	server := NewServer(config.Config{AllowedOrigin: "*"}, bus)

	body := bytes.NewBufferString(`{"title":"Gitea","message":"Push received","severity":"info"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/webhook/gitea", body)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	select {
	case got := <-client.Events:
		if got.Source != "gitea" || got.Title != "Gitea" {
			t.Fatalf("unexpected event: %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for webhook event")
	}
}

func TestWebhookRequiresSharedSecretWhenConfigured(t *testing.T) {
	bus := events.NewBus(4)
	server := NewServer(config.Config{AllowedOrigin: "*", WebhookSecret: "super-secret"}, bus)

	body := bytes.NewBufferString(`{"title":"Gitea","message":"Push received","severity":"info"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/webhook/gitea", body)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestWebhookAcceptsSharedSecretHeader(t *testing.T) {
	bus := events.NewBus(4)
	client := bus.Subscribe()
	defer bus.Unsubscribe(client.ID)
	server := NewServer(config.Config{AllowedOrigin: "*", WebhookSecret: "super-secret"}, bus)

	body := bytes.NewBufferString(`{"title":"Gitea","message":"Push received","severity":"info"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/webhook/gitea", body)
	req.Header.Set("X-Overlay-Webhook-Secret", "super-secret")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	select {
	case got := <-client.Events:
		if got.Source != "gitea" || got.Title != "Gitea" {
			t.Fatalf("unexpected event: %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for webhook event")
	}
}

func TestWebhookPreservesDataPayload(t *testing.T) {
	bus := events.NewBus(4)
	client := bus.Subscribe()
	defer bus.Unsubscribe(client.ID)
	server := NewServer(config.Config{AllowedOrigin: "*"}, bus)

	body := bytes.NewBufferString(`{"title":"Goal update","message":"Followers moved","severity":"success","data":{"goal":{"id":"followers","label":"Follower goal","current":430,"target":620,"unit":"followers"}}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/webhook/n8n", body)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	select {
	case got := <-client.Events:
		goal, ok := got.Data["goal"].(map[string]any)
		if !ok {
			t.Fatalf("expected goal data, got %#v", got.Data)
		}
		if goal["id"] != "followers" || goal["unit"] != "followers" {
			t.Fatalf("unexpected goal data: %#v", goal)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for webhook event")
	}
}

func TestWebhookAcceptsBearerSecret(t *testing.T) {
	bus := events.NewBus(4)
	server := NewServer(config.Config{AllowedOrigin: "*", WebhookSecret: "super-secret"}, bus)

	body := bytes.NewBufferString(`{"title":"Gitea","message":"Push received","severity":"info"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/webhook/gitea", body)
	req.Header.Set("Authorization", "Bearer super-secret")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestStreamEventsWritesSSE(t *testing.T) {
	bus := events.NewBus(4)
	server := NewServer(config.Config{AllowedOrigin: "*"}, bus)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/events/stream", nil).WithContext(ctx)
	rec := &streamRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		server.Handler().ServeHTTP(rec, req)
		close(done)
	}()

	select {
	case <-time.After(100 * time.Millisecond):
	case <-done:
		t.Fatal("stream closed before event")
	}

	bus.Publish(events.NewAlert("manual", "stream", "hello", events.SeverityInfo))
	cancel()

	select {
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stream write")
	case <-done:
	}

	if !rec.flushed {
		t.Fatal("expected stream to flush")
	}
	if got := rec.Body.String(); got == "" || !bytes.Contains([]byte(got), []byte("event: alert")) {
		t.Fatalf("expected SSE alert payload, got %q", got)
	}
}

func TestEventHistoryEndpointRecordsAndSanitizes(t *testing.T) {
	historyFile := filepath.Join(t.TempDir(), "event-history.jsonl")
	stateFile := filepath.Join(t.TempDir(), "overlay-state.json")
	server := NewServer(config.Config{AllowedOrigin: "*", OverlayStateFile: stateFile, EventHistoryFile: historyFile, EventHistoryLimit: 10}, events.NewBus(4))

	manualReq := httptest.NewRequest(http.MethodPost, "/api/events/test-alert", bytes.NewBufferString(`{"title":"Manual alert","message":"Welcome back","severity":"success","data":{"safe":"keep","operatorSecret":"hide","access_token":"hide","password":"hide","authorization":"hide","twitchEvent":{"raw":true},"items":["one","two"],"count":2,"ratio":1.5}}`))
	manualRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(manualRec, manualReq)
	if manualRec.Code != http.StatusAccepted {
		t.Fatalf("expected manual alert 202, got %d: %s", manualRec.Code, manualRec.Body.String())
	}

	clearReq := httptest.NewRequest(http.MethodPost, "/api/events/clear", nil)
	clearRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(clearRec, clearReq)
	if clearRec.Code != http.StatusAccepted {
		t.Fatalf("expected clear 202, got %d: %s", clearRec.Code, clearRec.Body.String())
	}

	webhookReq := httptest.NewRequest(http.MethodPost, "/api/integrations/webhook/gitea", bytes.NewBufferString(`{"title":"Webhook alert","message":"Incoming","severity":"warning"}`))
	webhookRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(webhookRec, webhookReq)
	if webhookRec.Code != http.StatusAccepted {
		t.Fatalf("expected webhook 202, got %d: %s", webhookRec.Code, webhookRec.Body.String())
	}

	server.publishEventSubEvent(events.NewBRB("eventsub", "BRB", "Intermission", map[string]any{"active": true, "retainedStateKey": "ignore"}))
	server.publishEvent(events.NewNowPlaying("mpris", "Track title", "Artist name", map[string]any{"player": "Feishin", "secretToken": "hidden"}))

	queryReq := httptest.NewRequest(http.MethodGet, "/api/events/history?limit=10", nil)
	queryRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(queryRec, queryReq)
	if queryRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", queryRec.Code, queryRec.Body.String())
	}

	var body history.Response
	if err := json.Unmarshal(queryRec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode history response: %v", err)
	}
	if len(body.Events) != 5 {
		t.Fatalf("expected 5 events, got %#v", body.Events)
	}
	if body.Events[0].Type != events.EventTypeNowPlaying || body.Events[0].Source != "mpris" {
		t.Fatalf("unexpected newest event: %#v", body.Events[0])
	}
	if body.Events[1].Type != events.EventTypeBRB || body.Events[1].Source != "eventsub" {
		t.Fatalf("unexpected eventsub event: %#v", body.Events[1])
	}
	if body.Events[2].Type != events.EventTypeAlert || body.Events[2].Source != "gitea" {
		t.Fatalf("unexpected webhook event: %#v", body.Events[2])
	}
	if body.Events[3].Type != events.EventTypeClear || body.Events[3].Source != "manual" {
		t.Fatalf("unexpected clear event: %#v", body.Events[3])
	}
	if body.Events[4].Type != events.EventTypeAlert || body.Events[4].Source != "manual" {
		t.Fatalf("unexpected manual event: %#v", body.Events[4])
	}
	manualEvent := body.Events[4]
	for _, key := range []string{"operatorSecret", "access_token", "password", "authorization", "twitchEvent"} {
		if _, ok := manualEvent.Data[key]; ok {
			t.Fatalf("expected %s to be removed: %#v", key, manualEvent.Data)
		}
	}
	items, ok := manualEvent.Data["items"].([]any)
	if !ok || len(items) != 2 || items[0] != "one" {
		t.Fatalf("expected capped string list, got %#v", manualEvent.Data["items"])
	}
	if manualEvent.Data["count"] != float64(2) || manualEvent.Data["ratio"] != 1.5 {
		t.Fatalf("expected numeric values to be preserved, got %#v", manualEvent.Data)
	}
	if _, ok := body.Events[0].Data["secretToken"]; ok {
		t.Fatalf("expected MPRIS secretToken to be removed: %#v", body.Events[0].Data)
	}
}

func TestEventHistoryEndpointValidationAndNilSafety(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*"}, events.NewBus(4))
	server.history = nil

	validReq := httptest.NewRequest(http.MethodGet, "/api/events/history", nil)
	validRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(validRec, validReq)
	if validRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", validRec.Code, validRec.Body.String())
	}
	var body history.Response
	if err := json.Unmarshal(validRec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode empty history response: %v", err)
	}
	if len(body.Events) != 0 {
		t.Fatalf("expected empty events, got %#v", body.Events)
	}

	invalidReq := httptest.NewRequest(http.MethodGet, "/api/events/history?limit=bad", nil)
	invalidRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(invalidRec, invalidReq)
	if invalidRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", invalidRec.Code, invalidRec.Body.String())
	}
}

func TestEventHistoryPersistsAcrossServerRestart(t *testing.T) {
	historyFile := filepath.Join(t.TempDir(), "event-history.jsonl")
	stateFile := filepath.Join(t.TempDir(), "overlay-state.json")
	server := NewServer(config.Config{AllowedOrigin: "*", OverlayStateFile: stateFile, EventHistoryFile: historyFile, EventHistoryLimit: 10}, events.NewBus(4))

	req := httptest.NewRequest(http.MethodPost, "/api/events/test-alert", bytes.NewBufferString(`{"title":"Persisted","message":"Across restarts"}`))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	restarted := NewServer(config.Config{AllowedOrigin: "*", OverlayStateFile: stateFile, EventHistoryFile: historyFile, EventHistoryLimit: 10}, events.NewBus(4))
	query := httptest.NewRequest(http.MethodGet, "/api/events/history", nil)
	queryRec := httptest.NewRecorder()
	restarted.Handler().ServeHTTP(queryRec, query)
	if queryRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", queryRec.Code, queryRec.Body.String())
	}
	var persisted history.Response
	if err := json.Unmarshal(queryRec.Body.Bytes(), &persisted); err != nil {
		t.Fatalf("decode history response: %v", err)
	}
	if len(persisted.Events) != 1 || persisted.Events[0].Title != "Persisted" {
		t.Fatalf("expected persisted event, got %#v", persisted.Events)
	}
}

func TestMalformedHistoryDisablesWritesAndKeepsReadyzDegraded(t *testing.T) {
	historyFile := filepath.Join(t.TempDir(), "event-history.jsonl")
	if err := os.WriteFile(historyFile, []byte("{\"id\":\"ok\"}\n{\"id\":"), 0o600); err != nil {
		t.Fatalf("write malformed history: %v", err)
	}

	server := NewServer(config.Config{AllowedOrigin: "*", EventHistoryFile: historyFile}, events.NewBus(4))
	if server.historyWritable {
		t.Fatal("expected history writes to be disabled after load failure")
	}

	readyReq := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	readyRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(readyRec, readyReq)
	if readyRec.Code != http.StatusOK {
		t.Fatalf("expected 200 readyz response, got %d: %s", readyRec.Code, readyRec.Body.String())
	}
	var readyBody readyzResponse
	if err := json.Unmarshal(readyRec.Body.Bytes(), &readyBody); err != nil {
		t.Fatalf("decode readyz: %v", err)
	}
	if readyBody.Status != "degraded" || !readyzHasCheck(readyBody.Checks, "event_history", "degraded") {
		t.Fatalf("expected degraded event_history readiness, got %#v", readyBody)
	}

	before, err := os.ReadFile(historyFile)
	if err != nil {
		t.Fatalf("read malformed history before publish: %v", err)
	}

	client := server.bus.Subscribe()
	defer server.bus.Unsubscribe(client.ID)

	server.publishEvent(events.NewAlert("manual", "Recovered", "Still online", events.SeverityInfo))

	select {
	case got := <-client.Events:
		if got.Title != "Recovered" || got.Source != "manual" {
			t.Fatalf("unexpected published event: %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for published event")
	}

	after, err := os.ReadFile(historyFile)
	if err != nil {
		t.Fatalf("read malformed history after publish: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("expected malformed history file to remain unchanged, before=%q after=%q", before, after)
	}
}

func TestPublishEventPublishesBeforeHistoryAppendFailure(t *testing.T) {
	historyDir := filepath.Join(t.TempDir(), "history")
	if err := os.Mkdir(historyDir, 0o700); err != nil {
		t.Fatalf("mkdir history dir: %v", err)
	}
	if err := os.Chmod(historyDir, 0o500); err != nil {
		t.Fatalf("chmod history dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(historyDir, 0o700) })

	historyFile := filepath.Join(historyDir, "event-history.jsonl")
	server := NewServer(config.Config{AllowedOrigin: "*", EventHistoryFile: historyFile}, events.NewBus(4))
	client := server.bus.Subscribe()
	defer server.bus.Unsubscribe(client.ID)

	server.publishEvent(events.NewAlert("manual", "Fanout", "Before write", events.SeverityInfo))

	select {
	case got := <-client.Events:
		if got.Title != "Fanout" || got.Source != "manual" {
			t.Fatalf("unexpected published event: %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for published event")
	}

	if _, err := os.Stat(historyFile); err == nil {
		t.Fatalf("expected history append to fail, but file exists at %s", historyFile)
	}
}
