package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestRetainedChannelSubscriptionSetCheckRepairAndFailures(t *testing.T) {
	for _, retained := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "retained"}[retained], func(t *testing.T) {
			subscriptions := []map[string]any{}
			sub := func(id, kind, version, channel, session string) map[string]any {
				return map[string]any{"id": id, "type": kind, "version": version, "status": "enabled", "condition": map[string]string{"broadcaster_user_id": channel}, "transport": map[string]string{"session_id": session}}
			}
			subscriptions = append(subscriptions, sub("online", "stream.online", "1", "owner", "socket"), sub("offline", "stream.offline", "1", "owner", "socket"), sub("metadata", "channel.update", "2", "owner", "socket"), sub("other", "channel.update", "2", "other", "socket"), sub("other-socket", "channel.update", "2", "owner", "other"), sub("chat", "channel.chat.message", "1", "owner", "socket"))
			var created, deleted []string
			fail := false
			lookupFail := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer fixture" || r.Header.Get("Client-Id") != "client" {
					t.Error("session authorization missing")
				}
				switch r.Method {
				case "GET":
					if lookupFail {
						w.WriteHeader(503)
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": subscriptions})
				case "DELETE":
					deleted = append(deleted, r.URL.Query().Get("id"))
					w.WriteHeader(204)
				case "POST":
					var body struct {
						Type, Version string
						Condition     map[string]string
						Transport     map[string]string
					}
					_ = json.NewDecoder(r.Body).Decode(&body)
					if body.Condition["broadcaster_user_id"] != "owner" || body.Transport["session_id"] != "socket" || body.Transport["method"] != "websocket" {
						t.Error("subscription binding", body)
					}
					created = append(created, body.Type+":"+body.Version)
					if fail && body.Type == "stream.offline" {
						w.WriteHeader(503)
						return
					}
					w.WriteHeader(202)
				}
			}))
			defer server.Close()
			client := Client{ClientID: "client", RetainedChannelUpdates: retained, SubscriptionURL: server.URL, HTTPClient: server.Client(), lifecycleSession: "socket", lifecycleToken: Token{UserID: "owner", AccessToken: "fixture"}}
			if ready, err := client.CheckLifecycle(t.Context()); err != nil || !ready {
				t.Fatal(ready, err)
			}
			want := []string{"stream.online:1", "stream.offline:1"}
			wantDelete := []string{"online", "offline"}
			if retained {
				want = append(want, "channel.update:2")
				wantDelete = append(wantDelete, "metadata")
			}
			if err := client.EnsureLifecycle(t.Context(), true); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(created, want) || !reflect.DeepEqual(deleted, wantDelete) {
				t.Fatal(created, deleted)
			}
			// Cached success must not hide failed authoritative checks or reauthorization.
			lookupFail = true
			if ready, err := client.CheckLifecycle(t.Context()); err == nil || ready {
				t.Fatal("failed lookup ready")
			}
			if _, ready := client.LifecycleStatus(); ready {
				t.Fatal("stale ready after lookup failure")
			}
			lookupFail = false
			fail = true
			created = nil
			if err := client.EnsureLifecycle(t.Context(), true); err == nil {
				t.Fatal("partial set accepted")
			}
			if _, ready := client.LifecycleStatus(); ready {
				t.Fatal("partial set ready")
			}
			fail = false
			client.TokenProvider = func(context.Context) (Token, bool) { return Token{UserID: "other", AccessToken: "fixture"}, true }
			before := len(created)
			if err := client.EnsureLifecycle(t.Context(), true); err == nil || len(created) != before {
				t.Fatal("changed subject acquired subscriptions")
			}
			client.TokenProvider = nil
			// Wrong version, missing update, disabled update and wrong broadcaster each fail retained checks.
			for _, change := range []func(){func() { subscriptions[2]["version"] = "1" }, func() { subscriptions = subscriptions[:2] }, func() { subscriptions = append(subscriptions, subscriptions[2]) }, func() { subscriptions[2]["transport"] = map[string]string{"session_id": "other"} }, func() { subscriptions[2]["status"] = "authorization_revoked" }, func() { subscriptions[2]["condition"] = map[string]string{"broadcaster_user_id": "other"} }} {
				subscriptions = []map[string]any{sub("online", "stream.online", "1", "owner", "socket"), sub("offline", "stream.offline", "1", "owner", "socket"), sub("metadata", "channel.update", "2", "owner", "socket")}
				change()
				if ready, err := client.CheckLifecycle(t.Context()); err != nil || ready == retained {
					t.Fatal("set isolation", ready, err)
				}
			}
		})
	}
}

func TestRetainedChannelSocketColdAndGraceAndLegacyIsolation(t *testing.T) {
	for _, mode := range []string{"cold", "grace", "legacy", "disabled", "revoked"} {
		t.Run(mode, func(t *testing.T) {
			posts := 0
			calls := 0
			revokedReady := true
			upgrader := websocket.Upgrader{}
			var client Client
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					posts++
					w.WriteHeader(202)
					return
				}
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer func() { _ = conn.Close() }()
				mustWriteEventSubMessage(t, conn, EventSubMessage{Metadata: EventSubMetadata{MessageType: "session_welcome"}, Payload: EventSubPayload{Session: EventSubSession{ID: "socket", KeepaliveTimeoutSeconds: 30}}})
				// Wait for registration to finish by reading the first callback through the socket loop.
				if mode == "revoked" {
					revoked := EventSubMessage{Metadata: EventSubMetadata{MessageType: "revocation"}}
					revoked.Payload.Subscription.Type = "channel.update"
					revoked.Payload.Subscription.Condition.BroadcasterUserID = "owner"
					mustWriteEventSubMessage(t, conn, revoked)
				}
				for _, channel := range []string{"other", "owner"} {
					message := EventSubMessage{Metadata: EventSubMetadata{MessageType: "notification", MessageID: "update", MessageTimestamp: time.Now()}, Payload: EventSubPayload{Subscription: EventSubSubscription{Type: "channel.update"}, Event: map[string]any{"broadcaster_user_id": channel}}}
					mustWriteEventSubMessage(t, conn, message)
				}
			}))
			defer server.Close()
			retained := mode != "legacy"
			client = Client{RetainedChannelUpdates: retained, SubscriptionURL: server.URL, HTTPClient: server.Client(), LifecycleWanted: func() bool { return mode != "disabled" }, Lifecycle: func(context.Context, EventSubMessage, Token) error {
				calls++
				_, revokedReady = client.LifecycleStatus()
				return errors.New("fixture ended")
			}, lifecycleRegistered: mode == "grace"}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			_ = client.runSession(ctx, Token{UserID: "owner"}, strings.Replace(server.URL, "http", "ws", 1), mode != "grace")
			expectedPosts := 3
			if mode == "legacy" {
				expectedPosts = 2
			}
			if mode == "disabled" || mode == "grace" {
				expectedPosts = 0
			}
			expectedCalls := 1
			if mode == "legacy" {
				expectedCalls = 0
			}
			if posts != expectedPosts || calls != expectedCalls {
				t.Fatal("socket set", posts, calls)
			}
			if mode == "revoked" && revokedReady {
				t.Fatal("revocation retained ready")
			}
		})
	}
}

func TestRetainedChannelProviderDirectedGraceReconnect(t *testing.T) {
	var subscriptions, sockets atomic.Int32
	upgrader := websocket.Upgrader{}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			var request struct{ Type string }
			_ = json.NewDecoder(r.Body).Decode(&request)
			if request.Type == "stream.online" || request.Type == "stream.offline" || request.Type == "channel.update" {
				subscriptions.Add(1)
			}
			w.WriteHeader(202)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		n := sockets.Add(1)
		session := "first"
		if n == 2 {
			session = "migrated"
		}
		mustWriteEventSubMessage(t, conn, EventSubMessage{Metadata: EventSubMetadata{MessageType: "session_welcome"}, Payload: EventSubPayload{Session: EventSubSession{ID: session, KeepaliveTimeoutSeconds: 30}}})
		if n == 1 {
			mustWriteEventSubMessage(t, conn, EventSubMessage{Metadata: EventSubMetadata{MessageType: "session_reconnect"}, Payload: EventSubPayload{Session: EventSubSession{ReconnectURL: strings.Replace(server.URL, "http", "ws", 1)}}})
		} else {
			mustWriteEventSubMessage(t, conn, EventSubMessage{Metadata: EventSubMetadata{MessageType: "notification"}, Payload: EventSubPayload{Subscription: EventSubSubscription{Type: "channel.update"}, Event: map[string]any{"broadcaster_user_id": "owner"}}})
		}
	}))
	defer server.Close()
	calls := 0
	client := Client{RetainedChannelUpdates: true, SubscriptionURL: server.URL, HTTPClient: server.Client(), LifecycleWanted: func() bool { return true }, Lifecycle: func(context.Context, EventSubMessage, Token) error { calls++; return errors.New("fixture finished") }}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_ = client.runSession(ctx, Token{UserID: "owner"}, strings.Replace(server.URL, "http", "ws", 1), true)
	if sockets.Load() != 2 || subscriptions.Load() != 3 || calls != 1 {
		t.Fatal("grace duplicated or lost set", sockets.Load(), subscriptions.Load(), calls)
	}
	if connected, ready := client.LifecycleStatus(); connected || ready {
		t.Fatal("disconnected socket retained readiness")
	}
}
