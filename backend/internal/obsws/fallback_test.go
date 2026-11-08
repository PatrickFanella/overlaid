package obsws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func fallbackResponse(r requestData, data any) envelope {
	return envelope{Op: opRequestResponse, Data: mustJSON(map[string]any{"requestType": r.RequestType, "requestId": r.RequestID, "requestStatus": map[string]any{"result": true, "code": 100}, "responseData": data})}
}
func fallbackData(kind string, present, active, muted bool) any {
	switch kind {
	case "GetSceneCollectionList":
		return map[string]any{"currentSceneCollectionName": "Collection"}
	case "GetInputList":
		inputs := []map[string]any{}
		if present {
			inputs = append(inputs, map[string]any{"inputName": "Input"})
		}
		return map[string]any{"inputs": inputs}
	case "GetSceneList":
		return map[string]any{"scenes": []map[string]any{{"sceneName": "Fallback"}}}
	case "GetSourceActive":
		return map[string]any{"videoActive": active}
	case "GetInputMute":
		return map[string]any{"inputMuted": muted}
	default:
		return map[string]any{}
	}
}
func fallbackServer(t *testing.T, handle func(requestData) envelope) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		if conn.WriteJSON(envelope{Op: opHello, Data: mustJSON(helloData{RPCVersion: 1})}) != nil {
			return
		}
		var msg envelope
		if conn.ReadJSON(&msg) != nil || msg.Op != opIdentify {
			return
		}
		if conn.WriteJSON(envelope{Op: opIdentified, Data: json.RawMessage(`{}`)}) != nil {
			return
		}
		for {
			if conn.ReadJSON(&msg) != nil {
				return
			}
			var request requestData
			if msg.Op != opRequest || json.Unmarshal(msg.Data, &request) != nil {
				return
			}
			if conn.WriteJSON(handle(request)) != nil {
				return
			}
		}
	}))
}

func TestFallbackSnapshotClassifiesReadOnlyEvidence(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		present, active, muted bool
	}{
		{"missing", false, false, false}, {"present", true, true, false}, {"hidden", true, false, false}, {"muted", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mutations atomic.Int32
			server := fallbackServer(t, func(r requestData) envelope {
				if !strings.HasPrefix(r.RequestType, "Get") {
					mutations.Add(1)
				}
				return fallbackResponse(r, fallbackData(r.RequestType, tc.present, tc.active, tc.muted))
			})
			defer server.Close()
			got, err := (Client{URL: wsURL(server.URL)}).InspectFallback(t.Context(), "Collection", "Input", "Fallback")
			if err != nil || got.InputPresent != tc.present || got.InputActive != tc.active || got.InputMuted != tc.muted || !got.SceneAvailable || mutations.Load() != 0 {
				t.Fatal("read-only evidence", got, err)
			}
		})
	}
}

func TestFallbackSnapshotRejectsIncompleteOrChangingEvidence(t *testing.T) {
	for _, scenario := range []string{"collection", "no inputs", "null inputs", "no scenes", "duplicate input", "duplicate scene", "event", "wrong id", "wrong type", "no result", "changed input", "changed collection", "target absent"} {
		t.Run(scenario, func(t *testing.T) {
			var inputs, collections int
			server := fallbackServer(t, func(r requestData) envelope {
				data := fallbackData(r.RequestType, false, false, false)
				if r.RequestType == "GetInputList" {
					inputs++
				}
				if r.RequestType == "GetSceneCollectionList" {
					collections++
				}
				switch scenario {
				case "collection":
					if r.RequestType == "GetSceneCollectionList" {
						data = map[string]any{"currentSceneCollectionName": "Other"}
					}
				case "no inputs":
					if r.RequestType == "GetInputList" {
						data = map[string]any{}
					}
				case "null inputs":
					if r.RequestType == "GetInputList" {
						data = map[string]any{"inputs": nil}
					}
				case "no scenes":
					if r.RequestType == "GetSceneList" {
						data = map[string]any{}
					}
				case "duplicate input":
					if r.RequestType == "GetInputList" {
						data = map[string]any{"inputs": []map[string]any{{"inputName": "Other"}, {"inputName": "Other"}}}
					}
				case "duplicate scene":
					if r.RequestType == "GetSceneList" {
						data = map[string]any{"scenes": []map[string]any{{"sceneName": "Fallback"}, {"sceneName": "Fallback"}}}
					}
				case "event":
					return envelope{Op: 5, Data: json.RawMessage(`{"eventType":"InputRemoved"}`)}
				case "wrong id":
					r.RequestID = "other"
				case "wrong type":
					r.RequestType = "Other"
				case "no result":
					return envelope{Op: opRequestResponse, Data: mustJSON(map[string]any{"requestType": r.RequestType, "requestId": r.RequestID, "requestStatus": map[string]any{"code": 100}})}
				case "changed input":
					if r.RequestType == "GetInputList" && inputs > 1 {
						data = fallbackData(r.RequestType, true, true, false)
					}
				case "changed collection":
					if r.RequestType == "GetSceneCollectionList" && collections > 1 {
						data = map[string]any{"currentSceneCollectionName": "Other"}
					}
				case "target absent":
					if r.RequestType == "GetSceneList" {
						data = map[string]any{"scenes": []any{}}
					}
				}
				return fallbackResponse(r, data)
			})
			defer server.Close()
			got, err := (Client{URL: wsURL(server.URL)}).InspectFallback(t.Context(), "Collection", "Input", "Fallback")
			if scenario == "target absent" {
				if err != nil || got.SceneAvailable {
					t.Fatal("target", got, err)
				}
				return
			}
			if !errors.Is(err, ErrFallbackEvidence) || got != (FallbackSnapshot{}) {
				t.Fatal("incomplete evidence accepted", got, err)
			}
		})
	}
}

func TestFallbackSceneAttemptRechecksAndSendsOnce(t *testing.T) {
	for _, scenario := range []string{"success", "present", "target absent", "guard refused", "canceled guard", "uncertain response"} {
		t.Run(scenario, func(t *testing.T) {
			var sets, guards atomic.Int32
			server := fallbackServer(t, func(r requestData) envelope {
				if r.RequestType == "SetCurrentProgramScene" {
					sets.Add(1)
					if r.RequestData["sceneName"] != "Fallback" {
						t.Error("changed selected scene")
					}
					if scenario == "uncertain response" {
						r.RequestID = "lost"
					}
				}
				data := fallbackData(r.RequestType, scenario == "present", true, false)
				if scenario == "target absent" && r.RequestType == "GetSceneList" {
					data = map[string]any{"scenes": []any{}}
				}
				return fallbackResponse(r, data)
			})
			defer server.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			err := (Client{URL: wsURL(server.URL)}).SetFallbackScene(ctx, "Collection", "Input", "Fallback", func(context.Context) error {
				guards.Add(1)
				if scenario == "guard refused" {
					return errors.New("PRIVATE_GUARD")
				}
				if scenario == "canceled guard" {
					cancel()
				}
				return nil
			})
			if scenario == "success" {
				if err != nil || sets.Load() != 1 || guards.Load() != 1 {
					t.Fatal("success", err)
				}
				return
			}
			if err == nil || strings.Contains(err.Error(), "PRIVATE_") {
				t.Fatal("fixed failure", err)
			}
			if scenario == "canceled guard" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation", err)
			}
			want := int32(0)
			if scenario == "uncertain response" {
				want = 1
			}
			if sets.Load() != want {
				t.Fatal("unexpected scene attempts", sets.Load())
			}
		})
	}
}

func TestFallbackSnapshotCancellation(t *testing.T) {
	blocked := make(chan struct{})
	server := fallbackServer(t, func(r requestData) envelope { <-blocked; return fallbackResponse(r, map[string]any{}) })
	defer server.Close()
	defer close(blocked)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if _, err := (Client{URL: wsURL(server.URL)}).InspectFallback(ctx, "Collection", "Input", "Fallback"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("bounded read", err)
	}
	if _, err := (Client{}).InspectFallback(t.Context(), "Collection", "Input", "Fallback"); !errors.Is(err, ErrFallbackConnection) {
		t.Fatal("disconnected", err)
	}
}
