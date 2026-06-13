package obsws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestComputeAuthentication(t *testing.T) {
	got := ComputeAuthentication("password", "salt", "challenge")
	if got != "zTM5ki6L2vVvBQiTG9ckH1Lh64AbnCf6XZ226UmnkIA=" {
		t.Fatalf("unexpected auth response: %s", got)
	}
}

func TestGetInputListSendsExpectedRequest(t *testing.T) {
	server := newFakeOBSWebSocketServer(t, func(requestType string, requestData map[string]any) map[string]any {
		if requestType != "GetInputList" {
			t.Fatalf("unexpected request type: %s", requestType)
		}
		if len(requestData) != 0 {
			t.Fatalf("expected empty request data, got %#v", requestData)
		}
		return map[string]any{
			"inputs": []map[string]any{{"inputName": "Mic/Aux", "inputKind": "wasapi_input_capture", "inputUuid": "abc"}},
		}
	})
	defer server.Close()

	client := Client{URL: wsURL(server.URL)}
	inputs, err := client.GetInputList(context.Background())
	if err != nil {
		t.Fatalf("GetInputList failed: %v", err)
	}
	if len(inputs) != 1 || inputs[0].InputName != "Mic/Aux" || inputs[0].InputKind != "wasapi_input_capture" || inputs[0].InputUuid != "abc" {
		t.Fatalf("unexpected inputs: %#v", inputs)
	}
}

func TestGetSceneItemListSendsExpectedRequest(t *testing.T) {
	server := newFakeOBSWebSocketServer(t, func(requestType string, requestData map[string]any) map[string]any {
		if requestType != "GetSceneItemList" {
			t.Fatalf("unexpected request type: %s", requestType)
		}
		if got := requestData["sceneName"]; got != "Program" {
			t.Fatalf("expected sceneName Program, got %#v", got)
		}
		return map[string]any{
			"sceneItems": []map[string]any{{"sceneItemId": 7, "sourceName": "Mic/Aux", "sceneItemEnabled": true}},
		}
	})
	defer server.Close()

	client := Client{URL: wsURL(server.URL)}
	items, err := client.GetSceneItemList(context.Background(), " Program ")
	if err != nil {
		t.Fatalf("GetSceneItemList failed: %v", err)
	}
	if len(items) != 1 || items[0].SceneItemID != 7 || items[0].SourceName != "Mic/Aux" || !items[0].SceneItemEnabled {
		t.Fatalf("unexpected scene items: %#v", items)
	}
}

func TestGetSceneItemListRequiresSceneName(t *testing.T) {
	client := Client{}
	if _, err := client.GetSceneItemList(context.Background(), "   "); err == nil {
		t.Fatal("expected error for missing scene name")
	}
}

func TestRefreshBrowserSourceSendsExpectedRequest(t *testing.T) {
	server := newFakeOBSWebSocketServer(t, func(requestType string, requestData map[string]any) map[string]any {
		if requestType != "PressInputPropertiesButton" {
			t.Fatalf("unexpected request type: %s", requestType)
		}
		if got := requestData["inputName"]; got != "Alerts Browser" {
			t.Fatalf("unexpected inputName: %#v", got)
		}
		if got := requestData["propertyName"]; got != "refreshnocache" {
			t.Fatalf("unexpected propertyName: %#v", got)
		}
		return map[string]any{}
	})
	defer server.Close()

	client := Client{URL: wsURL(server.URL)}
	if err := client.RefreshBrowserSource(context.Background(), " Alerts Browser "); err != nil {
		t.Fatalf("RefreshBrowserSource failed: %v", err)
	}
}

func TestRefreshBrowserSourceRequiresInputName(t *testing.T) {
	client := Client{}
	if err := client.RefreshBrowserSource(context.Background(), "   "); err == nil {
		t.Fatal("expected error for missing input name")
	}
}

func TestStatusReturnsQuicklyWhenHelloStalls(t *testing.T) {
	server := newStalledOBSWebSocketServer(t)
	defer server.Close()

	client := Client{URL: wsURL(server.URL)}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	start := time.Now()
	status := client.Status(ctx)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Status took too long: %s", elapsed)
	}
	if status.Reachable {
		t.Fatalf("expected stalled OBS to be unreachable, got %#v", status)
	}
	if status.Error == "" {
		t.Fatalf("expected error for stalled OBS, got %#v", status)
	}
}

func TestStatusReportsReachableOnAuthFailure(t *testing.T) {
	server := newAuthRequiredOBSWebSocketServer(t)
	defer server.Close()

	client := Client{URL: wsURL(server.URL), Password: "wrong-password"}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	status := client.Status(ctx)
	if !status.Reachable {
		t.Fatalf("expected auth failure to remain reachable, got %#v", status)
	}
	if status.Authenticated {
		t.Fatalf("expected auth failure to be unauthenticated, got %#v", status)
	}
	if status.OBSStudioVersion != "30.2.0" || status.OBSWebSocketVersion != "5.5.0" || status.RPCVersion != 1 {
		t.Fatalf("expected hello metadata to be preserved, got %#v", status)
	}
	if status.Error == "" {
		t.Fatalf("expected error for auth failure, got %#v", status)
	}
}

func newFakeOBSWebSocketServer(t *testing.T, handle func(requestType string, requestData map[string]any) map[string]any) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade failed: %v", err)
		}
		defer conn.Close()

		if err := conn.WriteJSON(envelope{Op: opHello, Data: mustJSON(helloData{OBSStudioVersion: "30.2.0", OBSWebSocketVersion: "5.5.0", RPCVersion: 1})}); err != nil {
			t.Fatalf("write hello failed: %v", err)
		}

		var identify envelope
		if err := conn.ReadJSON(&identify); err != nil {
			t.Fatalf("read identify failed: %v", err)
		}
		if identify.Op != opIdentify {
			t.Fatalf("unexpected identify op: %#v", identify)
		}
		if err := conn.WriteJSON(envelope{Op: opIdentified, Data: json.RawMessage(`{}`)}); err != nil {
			t.Fatalf("write identified failed: %v", err)
		}

		var request envelope
		if err := conn.ReadJSON(&request); err != nil {
			t.Fatalf("read request failed: %v", err)
		}
		var requestPayload requestData
		if err := json.Unmarshal(request.Data, &requestPayload); err != nil {
			t.Fatalf("unmarshal request failed: %v", err)
		}

		response := requestResponse{RequestType: requestPayload.RequestType, RequestID: requestPayload.RequestID, RequestStatus: requestStatus{Result: true, Code: 100, Comment: "OK"}, ResponseData: handle(requestPayload.RequestType, requestPayload.RequestData)}
		if err := conn.WriteJSON(envelope{Op: opRequestResponse, Data: mustJSON(response)}); err != nil {
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

		if err := conn.WriteJSON(envelope{Op: opHello, Data: mustJSON(helloData{OBSStudioVersion: "30.2.0", OBSWebSocketVersion: "5.5.0", RPCVersion: 1, Authentication: &authentication{Challenge: "challenge", Salt: "salt"}})}); err != nil {
			t.Fatalf("write hello failed: %v", err)
		}

		var identify envelope
		if err := conn.ReadJSON(&identify); err != nil {
			t.Fatalf("read identify failed: %v", err)
		}
		if identify.Op != opIdentify {
			t.Fatalf("unexpected identify op: %#v", identify)
		}

		<-r.Context().Done()
	}))
}

func wsURL(httpURL string) string {
	return strings.Replace(httpURL, "http://", "ws://", 1)
}
