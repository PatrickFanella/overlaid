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
)

func unlockChatController(t *testing.T, app *Application) {
	t.Helper()
	var released bool
	if err := app.chatControl.lease.QueryRowContext(t.Context(), `SELECT pg_advisory_unlock(`+chatControllerKey+`)`).Scan(&released); err != nil || !released {
		t.Fatal("fixture did not release controller ownership", released, err)
	}
}

func controllerToken(t *testing.T, app *Application, account accountFixture, staleValidation ...bool) {
	t.Helper()
	token := twitchTokenInfo{AccessToken: "fixture-access", UserID: "123", Login: "fixture", Scope: []string{"user:read:chat", "user:write:chat"}, ValidatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	if len(staleValidation) > 0 && staleValidation[0] {
		token.ValidatedAt = time.Now().Add(-2 * time.Hour)
	}
	raw, err := json.Marshal(tokenDisk(&token))
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.SaveConnection(t.Context(), app.keys, account.tenant, account.id, "twitch", token.UserID, token.Login, token.Scope, raw); err != nil {
		t.Fatal(err)
	}
}

func TestManagedManualWriterRequiresControllerWithEventSubDisabled(t *testing.T) {
	app, alpha, _, _ := applicationFixture(t)
	if app.config.TwitchEventSub || !app.chatReady() {
		t.Fatal("EventSub-disabled fixture lacks ownership")
	}
	if duplicate, err := NewApplication(app.config); err == nil {
		_ = duplicate.Close()
		t.Fatal("EventSub-disabled writer bypassed competing controller")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := app.checkChatController(canceled); !errors.Is(err, context.Canceled) || !app.chatReady() {
		t.Fatal("canceled caller stopped healthy controller", err)
	}
	if err := app.checkChatController(t.Context()); err != nil {
		t.Fatal("contender disrupted owner", err)
	}
	s, err := app.runtime(t.Context(), alpha.tenant)
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	previous := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, errors.New("provider forbidden")
	})}
	defer func() { twitchHTTPClient = previous }()
	unlockChatController(t, app)
	if err := app.chatControl.lease.PingContext(t.Context()); err != nil {
		t.Fatal("fixture must retain healthy session", err)
	}
	if _, err := s.sendTwitchChatMessage(t.Context(), "manual announcement"); err == nil {
		t.Fatal("unowned live session sent chat")
	}
	if requests.Load() != 0 || app.chatReady() || app.ctx.Err() == nil {
		t.Fatal("ownership loss did not fence writer", requests.Load())
	}
	status := httptest.NewRecorder()
	app.publicStatus(status, httptest.NewRequest("GET", "/readyz", nil))
	if !strings.Contains(status.Body.String(), `"name":"chat_dispatch","status":"degraded"`) {
		t.Fatal(status.Body.String())
	}
	cfg := app.config
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewApplication(cfg)
	if err != nil {
		t.Fatal("compatible controller did not restart", err)
	}
	defer func() { _ = restarted.Close() }()
	if err := restarted.checkChatController(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestManagedSendRechecksOwnershipAtTransportBoundary(t *testing.T) {
	app, alpha, _, _ := applicationFixture(t)
	controllerToken(t, app, alpha)
	s, err := app.runtime(t.Context(), alpha.tenant)
	if err != nil {
		t.Fatal(err)
	}
	checks := 0
	fence := s.chatFence
	s.chatFence = func(ctx context.Context) error {
		checks++
		if checks == 2 {
			unlockChatController(t, app)
		}
		return fence(ctx)
	}
	requests := 0
	previous := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[{"message_id":"forbidden","is_sent":true}]}`)), Header: http.Header{}}, nil
	})}
	defer func() { twitchHTTPClient = previous }()
	if _, err := s.sendTwitchChatMessage(t.Context(), "announcement"); err == nil || checks != 2 || requests != 0 {
		t.Fatal("final ownership fence missing", checks, requests, err)
	}
}

func TestManagedControllerLossCancelsInFlightEffects(t *testing.T) {
	for _, stage := range []string{"send", "validation"} {
		t.Run(stage, func(t *testing.T) { testControllerLossCancelsEffect(t, stage) })
	}
}

func testControllerLossCancelsEffect(t *testing.T, stage string) {
	app, alpha, _, _ := applicationFixture(t)
	controllerToken(t, app, alpha, stage == "validation")
	s, err := app.runtime(t.Context(), alpha.tenant)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	var requests atomic.Int32
	previous := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		expected := "https://api.twitch.tv/helix/chat/messages"
		if stage == "validation" {
			expected = twitchValidateEndpoint
		}
		if r.URL.String() != expected {
			return nil, errors.New("unexpected fixture provider call")
		}
		if requests.Add(1) == 1 {
			close(started)
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	defer func() { twitchHTTPClient = previous }()
	defer func() { _ = app.Close() }()
	done := make(chan error, 1)
	go func() { _, err := s.sendTwitchChatMessage(t.Context(), "announcement"); done <- err }()
	select {
	case <-started:
	case err := <-done:
		t.Fatal("send failed before transport", err)
	case <-time.After(5 * time.Second):
		t.Fatal("send did not reach fixture transport")
	}
	unlockChatController(t, app)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("controller cancellation identity lost", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ownership heartbeat did not cancel in-flight send")
	}
	if requests.Load() != 1 || app.chatReady() {
		t.Fatal("controller loss replayed or remained ready", requests.Load())
	}
}
