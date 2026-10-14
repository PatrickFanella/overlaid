package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLifecycleSubscriptionsRegisterPairAndRepairOnlyOwnedPair(t *testing.T) {
	var mu sync.Mutex
	created := []string{}
	deleted := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("missing authenticated session")
		}
		switch r.Method {
		case "GET":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":[{"id":"owned-online","type":"stream.online","status":"enabled","condition":{"broadcaster_user_id":"owner"},"transport":{"session_id":"socket"}},{"id":"owned-offline","type":"stream.offline","status":"enabled","condition":{"broadcaster_user_id":"owner"},"transport":{"session_id":"socket"}},{"id":"chat","type":"channel.chat.message","condition":{"broadcaster_user_id":"owner"},"transport":{"session_id":"socket"}},{"id":"other","type":"stream.online","condition":{"broadcaster_user_id":"other"},"transport":{"session_id":"socket"}},{"id":"other-session","type":"stream.offline","condition":{"broadcaster_user_id":"owner"},"transport":{"session_id":"other-socket"}}]}`))
		case "DELETE":
			deleted = append(deleted, r.URL.Query().Get("id"))
			w.WriteHeader(204)
		case "POST":
			var body struct{ Type string }
			json.NewDecoder(r.Body).Decode(&body)
			created = append(created, body.Type)
			w.WriteHeader(202)
		}
	}))
	defer server.Close()
	client := Client{ClientID: "client", SubscriptionURL: server.URL, HTTPClient: server.Client(), TokenProvider: func(context.Context) (Token, bool) { return Token{AccessToken: "fixture", UserID: "owner"}, true }, lifecycleSession: "socket", lifecycleToken: Token{AccessToken: "expired", UserID: "owner"}}
	if err := client.EnsureLifecycle(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if len(created) != 2 || created[0] != "stream.online" || created[1] != "stream.offline" {
		t.Fatal(created)
	}
	if registered, err := client.CheckLifecycle(context.Background()); err != nil || !registered {
		t.Fatal(registered, err)
	}
	if err := client.EnsureLifecycle(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 2 || deleted[0] != "owned-online" || deleted[1] != "owned-offline" || len(created) != 4 {
		t.Fatal(created, deleted)
	}
}

func TestLifecycleGraceTransferDoesNotRegisterDuplicatePair(t *testing.T) {
	calls := 0
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			t.Error("grace transfer registered duplicate lifecycle subscriptions")
			w.WriteHeader(409)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		mustWriteEventSubMessage(t, conn, EventSubMessage{Metadata: EventSubMetadata{MessageType: "session_welcome"}, Payload: EventSubPayload{Session: EventSubSession{ID: "new-session", KeepaliveTimeoutSeconds: 30}}})
		for _, broadcaster := range []string{"other", "owner"} {
			mustWriteEventSubMessage(t, conn, EventSubMessage{Metadata: EventSubMetadata{MessageID: "online", MessageType: "notification", MessageTimestamp: time.Now()}, Payload: EventSubPayload{Subscription: EventSubSubscription{Type: "stream.online"}, Event: map[string]any{"broadcaster_user_id": broadcaster}}})
		}
	}))
	defer server.Close()
	client := Client{SubscriptionURL: server.URL, HTTPClient: server.Client(), LifecycleWanted: func() bool { return true }, lifecycleRegistered: true, Lifecycle: func(context.Context, EventSubMessage, Token) error { calls++; return errors.New("fixture finished") }}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = client.runSession(ctx, Token{UserID: "owner"}, strings.Replace(server.URL, "http", "ws", 1), false)
	if calls != 1 {
		t.Fatal("lifecycle broadcaster boundary", calls)
	}
}
