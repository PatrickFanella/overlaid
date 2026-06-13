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
		"process":         "ok",
		"retained_state":  "ok",
		"twitch_config":   "degraded",
		"twitch_auth":     "degraded",
		"twitch_eventsub": "degraded",
		"obs_config":      "degraded",
		"obs_reachable":   "degraded",
		"obs_auth":        "degraded",
	})
	if body.Status != "degraded" {
		t.Fatalf("expected degraded overall status, got %#v", body)
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
}

func readyzHasCheck(checks []readyzCheck, name, status string) bool {
	for _, check := range checks {
		if check.Name == name && check.Status == status {
			return true
		}
	}
	return false
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
		"process":         "ok",
		"retained_state":  "ok",
		"twitch_config":   "degraded",
		"twitch_auth":     "degraded",
		"twitch_eventsub": "degraded",
		"obs_config":      "ok",
		"obs_reachable":   "degraded",
		"obs_auth":        "degraded",
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
