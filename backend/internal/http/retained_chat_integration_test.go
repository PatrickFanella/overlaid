package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformdispatch"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
)

func packet(channel, id, delivery, text string) twitch.EventSubMessage {
	return twitch.EventSubMessage{Metadata: twitch.EventSubMetadata{MessageType: "notification", MessageID: delivery, MessageTimestamp: time.Now().UTC()}, Payload: twitch.EventSubPayload{Subscription: twitch.EventSubSubscription{Type: "channel.chat.message", Version: "1"}, Event: map[string]any{"message_id": id, "chatter_user_id": channel, "chatter_user_login": channel, "chatter_user_name": channel, "broadcaster_user_id": channel, "broadcaster_user_login": channel, "message": map[string]any{"text": text}}}}
}

func waitReceipt(t *testing.T, a *Application, tenant string, want int) {
	t.Helper()
	db, _ := a.store.Product.Scoped(tenant)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rs, err := platformdispatch.Receipts(t.Context(), db, 100)
		if err != nil {
			t.Fatal(err)
		}
		done := 0
		for _, r := range rs {
			if r.State == "completed" {
				done++
			} else if r.State != "running" {
				t.Fatalf("dispatch failed: %#v", r)
			}
		}
		if done >= want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("retained dispatch did not complete")
}

func TestRetainedApplicationRegistryNewsroomEchoAndRestart(t *testing.T) {
	old, alpha, beta, f := applicationFixture(t)
	cfg := old.config
	_ = old.Close()
	cfg.TwitchChatDispatchMode = "retained"
	cfg.WorkerDatabaseURL = f.WorkerURL
	app, err := NewApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Close() })
	for _, account := range []accountFixture{alpha, beta} {
		name := "alpha"
		if account.id == beta.id {
			name = "beta"
		}
		token := twitchTokenInfo{AccessToken: "fixture-access", UserID: name, Login: name, Scope: []string{"user:read:chat", "user:write:chat"}, ValidatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
		raw, _ := json.Marshal(tokenDisk(&token))
		if err := app.store.SaveConnection(t.Context(), app.keys, account.tenant, account.id, "twitch", name, name, token.Scope, raw); err != nil {
			t.Fatal(err)
		}
	}
	s, err := app.runtime(t.Context(), alpha.tenant)
	if err != nil {
		t.Fatal(err)
	}
	message := packet("alpha", "counter-source", "delivery-one", "!count hype")
	if err := s.ingestRetainedChat(t.Context(), message, twitch.Token{UserID: "alpha"}); err != nil {
		t.Fatal(err)
	}
	message.Metadata.MessageID = "delivery-two"
	if err := s.ingestRetainedChat(t.Context(), message, twitch.Token{UserID: "alpha"}); err != nil {
		t.Fatal(err)
	}
	waitReceipt(t, app, alpha.tenant, 1)
	var leaders int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := f.Admin.QueryRow(`SELECT count(*) FROM river_leader WHERE expires_at>now()`).Scan(&leaders); err != nil {
			t.Fatal(err)
		}
		if leaders == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if leaders != 1 {
		t.Fatal("River maintenance leader not elected", leaders)
	}
	if err := s.ingestRetainedChat(t.Context(), packet("alpha", "empty-source", "empty-delivery", "  "), twitch.Token{UserID: "alpha"}); err != nil {
		t.Fatal("empty chat compatibility changed", err)
	}
	var value int
	db, _ := app.store.Product.Scoped(alpha.tenant)
	if err := db.QueryRowContext(t.Context(), `SELECT value FROM bot_counters WHERE name='hype'`).Scan(&value); err != nil || value != 1 {
		t.Fatal("duplicate command executed", value, err)
	}
	question, err := s.commandsRegistry().SaveCommand(t.Context(), bot.Command{Name: "ask", Action: "newsroom_question", Enabled: true, RoleRequired: "viewer", ResponseMode: "overlay"})
	if err != nil {
		t.Fatal(err)
	}
	_ = question
	s.refreshTwitchCustomCommands(t.Context())
	if err := s.ingestRetainedChat(t.Context(), packet("alpha", "question-source", "question-delivery", "!ask Why?"), twitch.Token{UserID: "alpha"}); err != nil {
		t.Fatal(err)
	}
	waitReceipt(t, app, alpha.tenant, 2)
	d, err := s.newsroom.Get(t.Context())
	if err != nil || len(d.Questions) != 1 || d.Questions[0].Text != "Why?" {
		t.Fatal("shared registry newsroom dispatch failed", d.Questions, err)
	}
	var sends atomic.Int32
	previous := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://api.twitch.tv/helix/chat/messages" {
			return nil, errors.New("unexpected fixture provider call")
		}
		sends.Add(1)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[{"message_id":"echo-source","is_sent":true}]}`)), Header: http.Header{}}, nil
	})}
	defer func() { twitchHTTPClient = previous }()
	if _, err := s.commandsRegistry().SaveCommand(t.Context(), bot.Command{Name: "hello", Action: "ticker", Enabled: true, RoleRequired: "viewer", ResponseMode: "chat", Response: "!count hype"}); err != nil {
		t.Fatal(err)
	}
	s.refreshTwitchCustomCommands(t.Context())
	if err := s.ingestRetainedChat(t.Context(), packet("alpha", "reply-source", "reply-delivery", "!hello"), twitch.Token{UserID: "alpha"}); err != nil {
		t.Fatal(err)
	}
	waitReceipt(t, app, alpha.tenant, 3)
	if sends.Load() != 1 {
		t.Fatal("mocked send count")
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { restarted.Close() })
	r, err := restarted.runtime(t.Context(), alpha.tenant)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ingestRetainedChat(t.Context(), packet("alpha", "echo-source", "echo-delivery", "!count hype"), twitch.Token{UserID: "alpha"}); err != nil {
		t.Fatal(err)
	}
	if err := r.ingestRetainedChat(t.Context(), message, twitch.Token{UserID: "alpha"}); err != nil {
		t.Fatal(err)
	}
	if err := r.ingestRetainedChat(t.Context(), packet("alpha", "manual-source", "manual-delivery", "!count hype"), twitch.Token{UserID: "alpha"}); err != nil {
		t.Fatal(err)
	}
	waitReceipt(t, restarted, alpha.tenant, 4)
	if err := db.QueryRowContext(t.Context(), `SELECT value FROM bot_counters WHERE name='hype'`).Scan(&value); err == nil {
		t.Fatal("closed product pool unexpectedly usable")
	}
	db, _ = restarted.store.Product.Scoped(alpha.tenant)
	if err := db.QueryRowContext(t.Context(), `SELECT value FROM bot_counters WHERE name='hype'`).Scan(&value); err != nil || value != 2 {
		t.Fatal("echo or manual command behavior changed", value, err)
	}
	bdb, _ := restarted.store.Product.Scoped(beta.tenant)
	var count int
	if err := bdb.QueryRowContext(t.Context(), `SELECT count(*) FROM normalized_chat_events`).Scan(&count); err != nil || count != 0 {
		t.Fatal("chat crossed tenants", count, err)
	}
}

func TestRetainedModeControllerGuardAndReadiness(t *testing.T) {
	old, _, _, f := applicationFixture(t)
	cfg := old.config
	old.Close()
	cfg.TwitchChatDispatchMode = "retained"
	cfg.WorkerDatabaseURL = f.WorkerURL
	app, err := NewApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Close() })
	if duplicate, err := NewApplication(cfg); err == nil {
		duplicate.Close()
		t.Fatal("competing controller accepted")
	}
	status := httptest.NewRecorder()
	app.publicStatus(status, httptest.NewRequest("GET", "/readyz", nil))
	if !strings.Contains(status.Body.String(), `"name":"chat_dispatch","status":"ok"`) {
		t.Fatal("chat readiness missing", status.Body.String())
	}
	if err := app.chatControl.lease.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for app.chatReady() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if app.chatReady() {
		t.Fatal("lost controller remained ready")
	}
	status = httptest.NewRecorder()
	app.publicStatus(status, httptest.NewRequest("GET", "/readyz", nil))
	if !strings.Contains(status.Body.String(), `"name":"chat_dispatch","status":"degraded"`) {
		t.Fatal("controller loss not reported", status.Body.String())
	}
	app.Close()
	cfg.TwitchChatDispatchMode = "legacy"
	cfg.TwitchEventSub = true
	if legacy, err := NewApplication(cfg); err == nil {
		legacy.Close()
		t.Fatal("mode marker allowed legacy ingestion")
	}
}

func TestRetainedConfigurationFailsClosed(t *testing.T) {
	for _, cfg := range []config.Config{{DeploymentMode: "selfhost", TwitchChatDispatchMode: "retained"}, {DeploymentMode: "selfhost", TwitchChatDispatchMode: "invalid"}} {
		if app, err := NewApplication(cfg); err == nil {
			app.Close()
			t.Fatal("invalid chat configuration accepted")
		}
	}
	old, _, _, f := applicationFixture(t)
	cfg := old.config
	old.Close()
	cfg.TwitchChatDispatchMode = "retained"
	cfg.WorkerDatabaseURL = f.RuntimeURL
	if app, err := NewApplication(cfg); err == nil {
		app.Close()
		t.Fatal("product credentials accepted as worker credentials")
	}
}

func TestRetainedPublicationHonorsCancellation(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	stop()
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	if _, err := acquireGate(ctx, gate); err == nil {
		t.Fatal("cancelled gate wait accepted")
	}
	s := NewServer(config.Config{TwitchClientID: "fixture-client"}, events.NewBus(4))
	s.twitch.token = &twitchTokenInfo{AccessToken: "fixture-token", UserID: "channel", Scope: []string{"user:write:chat"}, ValidatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	requestStarted := make(chan struct{})
	previous := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		close(requestStarted)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	defer func() { twitchHTTPClient = previous }()
	ctx, stop = context.WithCancel(t.Context())
	defer stop()
	done := make(chan error, 1)
	go func() {
		done <- s.publishEventSubContext(ctx, events.Event{ID: "cancel-publication", Type: events.EventTypeCommand, Data: map[string]any{"chatResponse": map[string]any{"message": "fixture reply"}}}, true)
	}()
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("mock send did not start")
	}
	stop()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("publication ignored cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled publication stalled")
	}
}
