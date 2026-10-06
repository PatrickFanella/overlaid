package obsws

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestCancellationInterruptsOBSReads(t *testing.T) {
	for _, stage := range []string{"hello", "identify", "response"} {
		t.Run(stage, func(t *testing.T) {
			ready := make(chan *websocket.Conn, 1)
			upgrader := websocket.Upgrader{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.Close()
				if stage != "hello" {
					if err := conn.WriteJSON(envelope{Op: opHello, Data: mustJSON(helloData{RPCVersion: 1})}); err != nil {
						t.Error(err)
						return
					}
					var identify envelope
					if err := conn.ReadJSON(&identify); err != nil {
						t.Error(err)
						return
					}
					if stage == "response" {
						if err := conn.WriteJSON(envelope{Op: opIdentified, Data: mustJSON(map[string]any{})}); err != nil {
							t.Error(err)
							return
						}
						var request envelope
						if err := conn.ReadJSON(&request); err != nil {
							t.Error(err)
							return
						}
					}
				}
				ready <- conn
				// Join when the client cancels and closes its connection.
				_, _, _ = conn.ReadMessage()
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := (Client{URL: wsURL(server.URL)}).GetSceneList(ctx); done <- err }()
			var connection *websocket.Conn
			select {
			case connection = <-ready:
			case <-time.After(time.Second):
				t.Fatal("fixture did not reach read stage")
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Errorf("cancellation error=%v", err)
				}
			case <-time.After(300 * time.Millisecond):
				t.Error("OBS read ignored cancellation")
				_ = connection.Close()
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Error("client did not exit after fixture closure")
				}
			}
		})
	}
}
