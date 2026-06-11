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

func TestOperatorSecretProtectsTestAlert(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", OperatorSecret: "operator-secret"}, events.NewBus(4))
	req := httptest.NewRequest(http.MethodPost, "/api/events/test-alert", bytes.NewBufferString(`{"title":"Nope","message":"Blocked"}`))
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
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
