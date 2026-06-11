package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

type streamRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
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
