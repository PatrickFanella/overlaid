package appvault

import (
	"context"
	"errors"
	"git.subcult.tv/PatrickFanella/over-laid/internal/conduitassignment"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchconduit"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOwnedSessionCustodiedAssignmentAndInvalidation(t *testing.T) {
	for _, kind := range []string{"success", "disconnect", "wrong_context"} {
		t.Run(kind, func(t *testing.T) {
			f := newShardFixture(t)
			connected := make(chan *websocket.Conn, 1)
			observed := make(chan twitchconduit.SessionNotification, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
					t.Error("private HTTP credentials sent to socket")
				}
				c, e := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if e != nil {
					return
				}
				defer func() { _ = c.Close() }()
				_ = c.WriteMessage(websocket.TextMessage, []byte(`{"metadata":{"message_id":"welcome","message_type":"session_welcome","message_timestamp":"2026-10-21T00:00:00Z"},"payload":{"session":{"id":"PRIVATE-SESSION","status":"connected","connected_at":"2026-10-21T00:00:00Z","keepalive_timeout_seconds":30,"reconnect_url":null}}}`))
				connected <- c
				_, _, _ = c.ReadMessage()
			}))
			defer server.Close()
			owner, err := twitchconduit.OpenSession(t.Context(), twitchconduit.SessionConfig{ConduitID: "owned", ShardCount: 2, ShardID: "0", Notify: func(_ context.Context, n twitchconduit.SessionNotification) error { observed <- n; return nil }, Dial: func(ctx context.Context, endpoint string) (*websocket.Conn, error) {
				if endpoint != "wss://eventsub.wss.twitch.tv/ws?keepalive_timeout_seconds=30" {
					t.Error("socket endpoint")
				}
				c, _, e := websocket.DefaultDialer.DialContext(ctx, strings.Replace(server.URL, "http", "ws", 1), nil)
				return c, e
			}})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := owner.Close(ctx); err != nil {
					t.Error(err)
				}
			}()
			socket := <-connected
			private, deadline, err := owner.Assignment()
			if err != nil || private != shardPrivate() {
				t.Fatal("real welcome scope", err)
			}
			r := f.prepare(t)
			g := AssignmentSession{Deadline: deadline, Acquire: owner.Acquire}
			before := shardSnapshot(t, f)
			client, patches, _ := shardClient(t, f, r, nil, func(stage string, req *http.Request) (*http.Response, error) {
				if !owner.Snapshot().Leased {
					t.Error("provider without actual session lease")
				}
				if d, ok := req.Context().Deadline(); !ok || d.After(deadline) {
					t.Error("original welcome deadline not inherited")
				}
				if stage != "assign" {
					return nil, nil
				}
				if kind == "disconnect" {
					_ = socket.Close()
					select {
					case <-req.Context().Done():
						return nil, req.Context().Err()
					case <-time.After(time.Second):
						t.Error("socket loss did not cancel provider")
						return nil, errors.New("PRIVATE")
					}
				}
				if kind == "success" {
					if e := socket.WriteMessage(websocket.TextMessage, []byte(`{"metadata":{"message_id":"event","message_type":"notification","message_timestamp":"2026-10-21T00:00:00Z","subscription_type":"channel.follow","subscription_version":"1"},"payload":{"subscription":{"id":"subscription","status":"enabled","type":"channel.follow","version":"1","transport":{"method":"conduit","conduit_id":"owned"}},"event":{"user_id":"PRIVATE-VIEWER"}}}`)); e != nil {
						t.Error(e)
					}
				}
				return nil, nil
			})
			ctx := owner.Context()
			if kind == "wrong_context" {
				ctx = t.Context()
			}
			result, err := f.run(ctx, keys(), r, g, client)
			switch kind {
			case "success":
				if err != nil || result.State != "assigned" || patches.Load() != 1 || owner.Snapshot().Leased {
					t.Fatal("actual lease assignment", result.State, err)
				}
				select {
				case n := <-observed:
					if n.ID != "event" || !strings.Contains(string(n.NativePayload()), "PRIVATE-VIEWER") {
						t.Fatal("private notification")
					}
				case <-time.After(time.Second):
					t.Fatal("notification not observed")
				}
			case "disconnect":
				if !errors.Is(err, context.Canceled) || !errors.Is(err, conduitassignment.ErrUncertain) || result.State != "uncertain" || patches.Load() != 1 {
					t.Fatal("actual session invalidation", result.State, err)
				}
			case "wrong_context":
				if err == nil || patches.Load() != 0 || f.state(t, r.ID) != "prepared" {
					t.Fatal("unbound context acquired actual owner", err)
				}
			}
			if before != shardSnapshot(t, f) {
				t.Fatal("private stores changed")
			}
		})
	}
}
