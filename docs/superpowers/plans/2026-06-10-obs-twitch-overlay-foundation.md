# OBS/Twitch Overlay Foundation Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Build the first deployable overlay slice for OBS: a browser-source alert overlay fed by the Go backend, with a tiny control/test API that can later power Twitch, Gitea, n8n, FreshRSS, and ntfy integrations.

**Architecture:** Start overlay-first and keep the app minimal: React renders `/overlay/alerts` as a transparent OBS browser source, while the Go API exposes health, event publish, clear, and Server-Sent Events endpoints. The initial event model is generic enough for Twitch and self-hosted services, but adapters remain stubbed until the overlay pipeline is proven.

**Tech Stack:** Go 1.26, standard-library HTTP/SSE, Vite React TypeScript, Tailwind 4, Vitest, Postgres/Docker Compose later for durable config/state.

---

## Product Decisions

- **MVP priority:** overlays first.
- **Runtime target:** homelab deployment through existing Docker Compose, with browser-source URLs usable from OBS on the LAN.
- **OBS:** assume OBS WebSocket is available, but do not integrate it in this first slice. Add placeholders/config so Task 2 can add it later.
- **Twitch:** target channel `patrick__eff` first. Plan for chat, alerts, channel points, and commands, but do not require OAuth/EventSub secrets in this first slice. The app should keep Twitch config generic enough to sign in as any streamer once OAuth credentials are provided.
- **Self-hosted integrations:** first-class future adapters: Gitea, n8n, FreshRSS, ntfy. Initial implementation uses a generic webhook endpoint so n8n/ntfy/Gitea can push messages before bespoke adapters exist.

## Target Routes

### Frontend

- `GET /overlay/alerts` — transparent OBS overlay, subscribes to API event stream.
- `GET /overlay/chat` — Twitch chat overlay placeholder route, subscribes to the same event stream and filters chat events later.
- `GET /overlay/goals` — goal/progress overlay placeholder route for follower/sub/channel-point goals later.
- `GET /overlay/now-playing` — media/status overlay placeholder route for self-hosted services later.
- `GET /overlay/ticker` — rotating service/status ticker placeholder route for Gitea/n8n/FreshRSS/ntfy events later.
- `GET /overlay/brb` — BRB/intermission overlay placeholder route for OBS scenes later.
- `GET /overlay/debug` — development event inspector route.
- `GET /` — keep a small landing page with links to overlay and future dashboard.
- Later: `GET /dashboard` — command center.

Each overlay gets its own browser-source route. The MVP uses one shared backend event stream, and each route filters/renders the events it cares about. Do not create separate streams per overlay until there is a concrete scaling or authorization need.

### Backend

- `GET /healthz` — health check.
- `GET /api/events/stream` — SSE stream for overlays and future dashboard.
- `POST /api/events/test-alert` — publish a manual alert for overlay testing.
- `POST /api/events/clear` — publish a clear event.
- `POST /api/integrations/webhook/{source}` — generic integration ingress for Gitea/n8n/FreshRSS/ntfy experiments.

## File Structure

### Create

- `backend/internal/config/config.go` — environment parsing for API port, CORS, public URLs, future OBS/Twitch settings.
- `backend/internal/events/events.go` — event types and constructors.
- `backend/internal/events/bus.go` — in-memory pub/sub bus for SSE clients.
- `backend/internal/events/bus_test.go` — unit tests for subscribe/publish/unsubscribe.
- `backend/internal/http/server.go` — HTTP server wiring.
- `backend/internal/http/routes.go` — route registration and CORS handling.
- `backend/internal/http/handlers.go` — health, SSE, test alert, clear, webhook handlers.
- `backend/internal/http/handlers_test.go` — HTTP behavior tests.
- `web/src/lib/events.ts` — shared frontend event types and parsers.
- `web/src/lib/eventStream.ts` — `EventSource` wrapper.
- `web/src/routes/OverlayAlerts.tsx` — OBS browser-source alert overlay.
- `web/src/routes/Home.tsx` — lightweight landing/home page.
- `web/src/routes/OverlayAlerts.test.tsx` — overlay rendering tests.

### Modify

- `backend/cmd/app/main.go` — replace placeholder greeting CLI with HTTP API startup.
- `backend/internal/app/app.go` — either remove from runtime path or leave only if tests still need it.
- `backend/internal/app/app_test.go` — keep until app package is removed in a later cleanup.
- `web/src/App.tsx` — route by `window.location.pathname` without adding a router dependency yet.
- `web/src/App.test.tsx` — update default app test.
- `web/package.json` — add React Testing Library only if needed by tests.
- `web/src/setupTests.ts` — add DOM matchers only if testing-library is added.
- `web/vite.config.ts` — add Vitest setup file only if needed.
- `.env.example` — document API/overlay variables and future OBS/Twitch placeholders.
- `docker-compose.yml` — expose backend config and Vite API base URL.
- `README.md` — document local run, OBS browser source URL, and webhook test commands.

---

## Phase 1: Overlay Event Pipeline

### Task 1: Backend Event Model and Bus

**Files:**
- Create: `backend/internal/events/events.go`
- Create: `backend/internal/events/bus.go`
- Create: `backend/internal/events/bus_test.go`

- [ ] **Step 1: Write the failing bus tests**

Create `backend/internal/events/bus_test.go`:

```go
package events

import (
	"testing"
	"time"
)

func TestBusPublishesToSubscribers(t *testing.T) {
	bus := NewBus(4)
	client := bus.Subscribe()
	defer bus.Unsubscribe(client.ID)

	event := NewAlert("manual", "Test alert", "Hello overlay", SeverityInfo)
	bus.Publish(event)

	select {
	case got := <-client.Events:
		if got.ID == "" {
			t.Fatal("expected event ID to be set")
		}
		if got.Type != EventTypeAlert {
			t.Fatalf("expected alert event, got %q", got.Type)
		}
		if got.Title != "Test alert" {
			t.Fatalf("expected title %q, got %q", "Test alert", got.Title)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestBusDoesNotBlockWhenSubscriberBufferIsFull(t *testing.T) {
	bus := NewBus(1)
	client := bus.Subscribe()
	defer bus.Unsubscribe(client.ID)

	bus.Publish(NewAlert("manual", "one", "one", SeverityInfo))
	bus.Publish(NewAlert("manual", "two", "two", SeverityInfo))

	select {
	case <-client.Events:
	case <-time.After(time.Second):
		t.Fatal("expected at least one buffered event")
	}
}

func TestBusUnsubscribeClosesChannel(t *testing.T) {
	bus := NewBus(1)
	client := bus.Subscribe()

	bus.Unsubscribe(client.ID)

	_, ok := <-client.Events
	if ok {
		t.Fatal("expected client event channel to be closed")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
cd backend && go test ./internal/events
```

Expected: FAIL because `backend/internal/events` does not exist yet.

- [ ] **Step 3: Implement the event model**

Create `backend/internal/events/events.go`:

```go
package events

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

type EventType string

const (
	EventTypeAlert       EventType = "alert"
	EventTypeClear       EventType = "clear"
	EventTypeService     EventType = "service_status"
	EventTypeChatMessage EventType = "chat_message"
	EventTypeCommand     EventType = "custom_command"
)

type Severity string

const (
	SeverityInfo    Severity = "info"
	SeveritySuccess Severity = "success"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

type Event struct {
	ID        string         `json:"id"`
	Type      EventType      `json:"type"`
	Source    string         `json:"source"`
	Title     string         `json:"title,omitempty"`
	Message   string         `json:"message,omitempty"`
	Severity  Severity       `json:"severity,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

func NewAlert(source, title, message string, severity Severity) Event {
	return Event{
		ID:        newID(),
		Type:      EventTypeAlert,
		Source:    source,
		Title:     title,
		Message:   message,
		Severity:  severity,
		CreatedAt: time.Now().UTC(),
	}
}

func NewClear(source string) Event {
	return Event{
		ID:        newID(),
		Type:      EventTypeClear,
		Source:    source,
		CreatedAt: time.Now().UTC(),
	}
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().UTC().Format("20060102T150405.000000000Z")
	}
	return hex.EncodeToString(b[:])
}
```

- [ ] **Step 4: Implement the bus**

Create `backend/internal/events/bus.go`:

```go
package events

import "sync"

type Client struct {
	ID     string
	Events chan Event
}

type Bus struct {
	mu         sync.RWMutex
	bufferSize int
	clients    map[string]Client
}

func NewBus(bufferSize int) *Bus {
	if bufferSize < 1 {
		bufferSize = 1
	}
	return &Bus{
		bufferSize: bufferSize,
		clients:    map[string]Client{},
	}
}

func (b *Bus) Subscribe() Client {
	client := Client{ID: newID(), Events: make(chan Event, b.bufferSize)}

	b.mu.Lock()
	b.clients[client.ID] = client
	b.mu.Unlock()

	return client
}

func (b *Bus) Unsubscribe(id string) {
	b.mu.Lock()
	client, ok := b.clients[id]
	if ok {
		delete(b.clients, id)
		close(client.Events)
	}
	b.mu.Unlock()
}

func (b *Bus) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, client := range b.clients {
		select {
		case client.Events <- event:
		default:
		}
	}
}
```

- [ ] **Step 5: Run test to verify it passes**

Run:

```bash
cd backend && go test ./internal/events
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/events
git commit -m "feat: add overlay event bus"
```

---

### Task 2: Backend HTTP API and SSE

**Files:**
- Create: `backend/internal/config/config.go`
- Create: `backend/internal/http/server.go`
- Create: `backend/internal/http/routes.go`
- Create: `backend/internal/http/handlers.go`
- Create: `backend/internal/http/handlers_test.go`
- Modify: `backend/cmd/app/main.go`

- [ ] **Step 1: Write failing HTTP tests**

Create `backend/internal/http/handlers_test.go`:

```go
package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

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

	got := <-client.Events
	if got.Type != events.EventTypeAlert || got.Title != "Hello" || got.Source != "manual" {
		t.Fatalf("unexpected event: %#v", got)
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

	got := <-client.Events
	if got.Source != "gitea" || got.Title != "Gitea" {
		t.Fatalf("unexpected event: %#v", got)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
cd backend && go test ./internal/http
```

Expected: FAIL because packages do not exist yet.

- [ ] **Step 3: Add config loader**

Create `backend/internal/config/config.go`:

```go
package config

import "os"

type Config struct {
	Addr          string
	AllowedOrigin string
	PublicWebURL  string
	OBSWebSocketURL string
	TwitchClientID  string
}

func Load() Config {
	return Config{
		Addr:            env("API_ADDR", ":8080"),
		AllowedOrigin:   env("CORS_ALLOWED_ORIGIN", "*"),
		PublicWebURL:    env("PUBLIC_WEB_URL", "http://localhost:5173"),
		OBSWebSocketURL: env("OBS_WEBSOCKET_URL", "ws://localhost:4455"),
		TwitchClientID:  env("TWITCH_CLIENT_ID", ""),
	}
}

func env(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
```

- [ ] **Step 4: Add HTTP server and routes**

Create `backend/internal/http/server.go`:

```go
package httpapi

import (
	"net/http"

	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

type Server struct {
	config config.Config
	bus    *events.Bus
	mux    *http.ServeMux
}

func NewServer(cfg config.Config, bus *events.Bus) *Server {
	server := &Server{config: cfg, bus: bus, mux: http.NewServeMux()}
	server.routes()
	return server
}

func (s *Server) Handler() http.Handler {
	return s.withCORS(s.mux)
}
```

Create `backend/internal/http/routes.go`:

```go
package httpapi

import "net/http"

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.healthz)
	s.mux.HandleFunc("GET /api/events/stream", s.streamEvents)
	s.mux.HandleFunc("POST /api/events/test-alert", s.testAlert)
	s.mux.HandleFunc("POST /api/events/clear", s.clearEvents)
	s.mux.HandleFunc("POST /api/integrations/webhook/{source}", s.webhook)
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", s.config.AllowedOrigin)
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

- [ ] **Step 5: Add handlers**

Create `backend/internal/http/handlers.go`:

```go
package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

type alertRequest struct {
	Title    string          `json:"title"`
	Message  string          `json:"message"`
	Severity events.Severity `json:"severity"`
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintln(w, "ok")
}

func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	client := s.bus.Subscribe()
	defer s.bus.Unsubscribe(client.ID)

	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-client.Events:
			if !ok {
				return
			}
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "id: %s\n", event.ID)
			fmt.Fprintf(w, "event: %s\n", event.Type)
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

func (s *Server) testAlert(w http.ResponseWriter, r *http.Request) {
	request, ok := decodeAlert(w, r)
	if !ok {
		return
	}
	s.bus.Publish(events.NewAlert("manual", request.Title, request.Message, normalizeSeverity(request.Severity)))
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) clearEvents(w http.ResponseWriter, _ *http.Request) {
	s.bus.Publish(events.NewClear("manual"))
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) webhook(w http.ResponseWriter, r *http.Request) {
	source := strings.TrimSpace(r.PathValue("source"))
	if source == "" {
		http.Error(w, "source is required", http.StatusBadRequest)
		return
	}
	request, ok := decodeAlert(w, r)
	if !ok {
		return
	}
	s.bus.Publish(events.NewAlert(source, request.Title, request.Message, normalizeSeverity(request.Severity)))
	w.WriteHeader(http.StatusAccepted)
}

func decodeAlert(w http.ResponseWriter, r *http.Request) (alertRequest, bool) {
	defer r.Body.Close()
	var request alertRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return request, false
	}
	request.Title = strings.TrimSpace(request.Title)
	request.Message = strings.TrimSpace(request.Message)
	if request.Title == "" || request.Message == "" {
		http.Error(w, "title and message are required", http.StatusBadRequest)
		return request, false
	}
	return request, true
}

func normalizeSeverity(severity events.Severity) events.Severity {
	switch severity {
	case events.SeveritySuccess, events.SeverityWarning, events.SeverityError:
		return severity
	default:
		return events.SeverityInfo
	}
}
```

- [ ] **Step 6: Replace main entrypoint**

Modify `backend/cmd/app/main.go`:

```go
package main

import (
	"log"
	"net/http"

	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	httpapi "git.subcult.tv/PatrickFanella/over-laid/internal/http"
)

func main() {
	cfg := config.Load()
	bus := events.NewBus(32)
	server := httpapi.NewServer(cfg, bus)

	log.Printf("over-laid API listening on %s", cfg.Addr)
	if err := http.ListenAndServe(cfg.Addr, server.Handler()); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 7: Run tests**

Run:

```bash
cd backend && go test ./...
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add backend/cmd/app/main.go backend/internal/config backend/internal/http
git commit -m "feat: expose overlay event API"
```

---

### Task 3: Frontend Overlay Route

**Files:**
- Create: `web/src/lib/events.ts`
- Create: `web/src/lib/eventStream.ts`
- Create: `web/src/routes/OverlayAlerts.tsx`
- Create: `web/src/routes/Home.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/App.test.tsx`

- [ ] **Step 1: Write/update frontend tests**

If the repo does not already have DOM test helpers, install them first:

```bash
pnpm --dir web add -D @testing-library/react @testing-library/jest-dom jsdom
```

Update `web/vite.config.ts` only if needed:

```ts
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  test: {
    environment: 'jsdom',
    setupFiles: './src/setupTests.ts',
  },
})
```

Create `web/src/setupTests.ts` only if needed:

```ts
import '@testing-library/jest-dom/vitest'
```

Update `web/src/App.test.tsx`:

```tsx
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import App from './App'

describe('App', () => {
  it('renders the home page by default', () => {
    window.history.pushState({}, '', '/')
    render(<App />)
    expect(screen.getByText('over-laid')).toBeInTheDocument()
    expect(screen.getByText('OBS overlay command center')).toBeInTheDocument()
  })

  it('renders the alert overlay route', () => {
    window.history.pushState({}, '', '/overlay/alerts')
    render(<App />)
    expect(screen.getByLabelText('OBS alert overlay')).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
pnpm --dir web run test
```

Expected: FAIL because routes/components do not exist or old copy is still rendered.

- [ ] **Step 3: Add frontend event types**

Create `web/src/lib/events.ts`:

```ts
export type OverlayEventType = 'alert' | 'clear' | 'service_status' | 'chat_message' | 'custom_command'
export type OverlaySeverity = 'info' | 'success' | 'warning' | 'error'

export type OverlayEvent = {
  id: string
  type: OverlayEventType
  source: string
  title?: string
  message?: string
  severity?: OverlaySeverity
  data?: Record<string, unknown>
  createdAt: string
}

export function parseOverlayEvent(raw: string): OverlayEvent | null {
  try {
    const parsed = JSON.parse(raw) as OverlayEvent
    if (!parsed.id || !parsed.type || !parsed.source || !parsed.createdAt) {
      return null
    }
    return parsed
  } catch {
    return null
  }
}
```

Create `web/src/lib/eventStream.ts`:

```ts
import { parseOverlayEvent, type OverlayEvent } from './events'

export type EventStreamHandlers = {
  onEvent: (event: OverlayEvent) => void
  onStatus?: (status: 'connecting' | 'open' | 'error') => void
}

export function createOverlayEventStream(handlers: EventStreamHandlers): EventSource {
  const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? 'http://localhost:8080'
  handlers.onStatus?.('connecting')
  const source = new EventSource(`${apiBaseUrl}/api/events/stream`)

  source.onopen = () => handlers.onStatus?.('open')
  source.onerror = () => handlers.onStatus?.('error')
  source.onmessage = (message) => {
    const event = parseOverlayEvent(message.data)
    if (event) {
      handlers.onEvent(event)
    }
  }

  source.addEventListener('alert', (message) => {
    const event = parseOverlayEvent((message as MessageEvent).data)
    if (event) {
      handlers.onEvent(event)
    }
  })

  source.addEventListener('clear', (message) => {
    const event = parseOverlayEvent((message as MessageEvent).data)
    if (event) {
      handlers.onEvent(event)
    }
  })

  return source
}
```

- [ ] **Step 4: Add overlay and home routes**

Create `web/src/routes/OverlayAlerts.tsx`:

```tsx
import { useEffect, useState } from 'react'
import { createOverlayEventStream } from '../lib/eventStream'
import type { OverlayEvent } from '../lib/events'

const visibleMs = 8000

export default function OverlayAlerts() {
  const [status, setStatus] = useState<'connecting' | 'open' | 'error'>('connecting')
  const [alert, setAlert] = useState<OverlayEvent | null>(null)

  useEffect(() => {
    const stream = createOverlayEventStream({
      onStatus: setStatus,
      onEvent: (event) => {
        if (event.type === 'clear') {
          setAlert(null)
          return
        }
        if (event.type === 'alert') {
          setAlert(event)
        }
      },
    })
    return () => stream.close()
  }, [])

  useEffect(() => {
    if (!alert) {
      return
    }
    const timeout = window.setTimeout(() => setAlert(null), visibleMs)
    return () => window.clearTimeout(timeout)
  }, [alert])

  return (
    <main
      aria-label="OBS alert overlay"
      className="pointer-events-none fixed inset-0 overflow-hidden bg-transparent text-white"
      data-connection-status={status}
    >
      <div className="absolute left-1/2 top-10 w-[min(90vw,720px)] -translate-x-1/2">
        {alert ? (
          <article className={`rounded-3xl border px-8 py-6 shadow-2xl backdrop-blur-md ${severityClass(alert.severity)}`}>
            <p className="text-sm uppercase tracking-[0.35em] text-white/70">{alert.source}</p>
            <h1 className="mt-2 text-5xl font-black leading-tight drop-shadow">{alert.title}</h1>
            <p className="mt-3 text-2xl text-white/90">{alert.message}</p>
          </article>
        ) : null}
      </div>
    </main>
  )
}

function severityClass(severity: OverlayEvent['severity']) {
  switch (severity) {
    case 'success':
      return 'border-emerald-300/60 bg-emerald-500/75'
    case 'warning':
      return 'border-amber-300/60 bg-amber-500/75'
    case 'error':
      return 'border-rose-300/60 bg-rose-600/75'
    default:
      return 'border-cyan-300/60 bg-slate-950/75'
  }
}
```

Create `web/src/routes/Home.tsx`:

```tsx
export default function Home() {
  return (
    <main className="min-h-screen bg-slate-950 px-6 py-16 text-slate-100">
      <section className="mx-auto max-w-3xl">
        <p className="text-sm uppercase tracking-[0.35em] text-cyan-300">over-laid</p>
        <h1 className="mt-4 text-5xl font-black">OBS overlay command center</h1>
        <p className="mt-6 text-lg text-slate-300">
          Browser-source overlays first. Dashboard, Twitch, OBS WebSocket, and self-hosted integrations next.
        </p>
        <div className="mt-8 rounded-2xl border border-slate-800 bg-slate-900 p-5">
          <p className="font-semibold">OBS browser source URL</p>
          <code className="mt-3 block rounded-xl bg-black/40 p-4 text-cyan-200">/overlay/alerts</code>
        </div>
      </section>
    </main>
  )
}
```

Modify `web/src/App.tsx`:

```tsx
import Home from './routes/Home'
import OverlayAlerts from './routes/OverlayAlerts'

export default function App() {
  if (window.location.pathname === '/overlay/alerts') {
    return <OverlayAlerts />
  }

  return <Home />
}
```

- [ ] **Step 5: Run tests and build**

Run:

```bash
pnpm --dir web run test
pnpm --dir web run build
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/package.json web/pnpm-lock.yaml web/vite.config.ts web/src
git commit -m "feat: add OBS alert overlay route"
```

---

### Task 4: Local and Homelab Configuration

**Files:**
- Modify: `.env.example`
- Modify: `docker-compose.yml`
- Modify: `README.md`

- [ ] **Step 1: Update `.env.example`**

Replace or extend `.env.example` with:

```dotenv
PROJECT_NAME=over-laid

POSTGRES_DB=app
POSTGRES_USER=app
POSTGRES_PASSWORD=change-me
POSTGRES_PORT=55432

API_PORT=8080
API_ADDR=:8080
CORS_ALLOWED_ORIGIN=*

WEB_PORT=5173
PUBLIC_WEB_URL=http://localhost:5173
VITE_API_BASE_URL=http://localhost:8080

# Future OBS WebSocket integration.
OBS_WEBSOCKET_URL=ws://localhost:4455

# Future Twitch integration.
TWITCH_CLIENT_ID=
TWITCH_CHANNEL=patrick__eff
TWITCH_REDIRECT_URL=
```

- [ ] **Step 2: Update Docker Compose API env**

Modify `docker-compose.yml` API service:

```yaml
  api:
    build: ./backend
    environment:
      API_ADDR: ${API_ADDR:-:8080}
      CORS_ALLOWED_ORIGIN: ${CORS_ALLOWED_ORIGIN:-*}
      PUBLIC_WEB_URL: ${PUBLIC_WEB_URL:-http://localhost:5173}
      OBS_WEBSOCKET_URL: ${OBS_WEBSOCKET_URL:-ws://host.docker.internal:4455}
      TWITCH_CLIENT_ID: ${TWITCH_CLIENT_ID:-}
      DATABASE_URL: postgres://${POSTGRES_USER:-app}:${POSTGRES_PASSWORD:-change-me}@postgres:5432/${POSTGRES_DB:-app}?sslmode=disable
    depends_on:
      postgres:
        condition: service_healthy
    ports:
      - "${API_PORT:-8080}:8080"
```

Keep future OAuth secrets out of committed files; only document non-secret placeholders in the checked-in env example.

- [ ] **Step 3: Update README**

Add this section to `README.md` after Quick start:

```markdown
## Overlay MVP

Run the API and web app with Docker Compose:

```bash
cp .env.example .env
docker compose up --build
```

Add this as an OBS browser source:

```text
http://<homelab-host>:5173/overlay/alerts
```

Recommended OBS browser-source settings:

- Width: `1920`
- Height: `1080`
- Custom CSS: leave empty initially
- Shutdown source when not visible: off
- Refresh browser when scene becomes active: off

Send a test alert:

```bash
curl -X POST http://localhost:8080/api/events/test-alert \
  -H 'Content-Type: application/json' \
  -d '{"title":"Test alert","message":"Overlay pipeline is live","severity":"success"}'
```

Clear the overlay:

```bash
curl -X POST http://localhost:8080/api/events/clear
```

Send an integration-style webhook:

```bash
curl -X POST http://localhost:8080/api/integrations/webhook/gitea \
  -H 'Content-Type: application/json' \
  -d '{"title":"Gitea","message":"Push received","severity":"info"}'
```
```

- [ ] **Step 4: Verify compose config**

Run:

```bash
docker compose config --quiet
```

Expected: no output and exit code 0.

- [ ] **Step 5: Commit**

```bash
git add .env.example docker-compose.yml README.md
git commit -m "docs: document overlay homelab setup"
```

---

### Task 5: End-to-End Verification

**Files:**
- No new files.

- [ ] **Step 1: Run full verification**

Run:

```bash
make verify
```

Expected: PASS.

- [ ] **Step 2: Run stack manually**

Run:

```bash
docker compose up --build
```

Expected:

- API logs `over-laid API listening on :8080`.
- Web is reachable on `http://localhost:5173`.
- Overlay is reachable on `http://localhost:5173/overlay/alerts`.

- [ ] **Step 3: Send manual alert while overlay is open**

Run:

```bash
curl -X POST http://localhost:8080/api/events/test-alert \
  -H 'Content-Type: application/json' \
  -d '{"title":"OBS test","message":"This should appear in the browser source","severity":"success"}'
```

Expected: overlay displays the alert for about 8 seconds.

- [ ] **Step 4: Send generic Gitea-style webhook**

Run:

```bash
curl -X POST http://localhost:8080/api/integrations/webhook/gitea \
  -H 'Content-Type: application/json' \
  -d '{"title":"Gitea","message":"Repository event reached over-laid","severity":"info"}'
```

Expected: overlay displays a Gitea-sourced alert.

- [ ] **Step 5: Commit any verification fixes**

```bash
git status --short
git add <only-intended-files>
git commit -m "fix: stabilize overlay MVP"
```

---

## Phase 2 Roadmap: Dashboard / Command Center

Build only after Phase 1 is usable in OBS.

1. Add `web/src/routes/Dashboard.tsx` with buttons for test alert, clear overlays, and webhook samples.
2. Add `GET /api/status` for API, OBS, Twitch, and integration statuses.
3. Add `POST /api/commands/{command}` for named macros.
4. Add Postgres migrations for saved overlay settings and commands.
5. Add local auth or LAN allowlist before exposing controls outside the private network.

## Phase 3 Roadmap: OBS + Twitch

1. Add OBS WebSocket client package at `backend/internal/integrations/obs`.
2. Add scene/source commands: switch scene, show/hide source, get current scene.
3. Add Twitch OAuth storage and token refresh.
4. Add Twitch chat/EventSub handlers for chat, follows/subs/raids, channel points, and commands.
5. Normalize Twitch events into the existing `events.Event` model.

## Phase 4 Roadmap: Self-Hosted Services

1. Keep `POST /api/integrations/webhook/{source}` as the stable generic adapter.
2. Add `backend/internal/integrations/gitea` for signed Gitea webhooks.
3. Add `backend/internal/integrations/n8n` examples/workflows that call the generic webhook.
4. Add `backend/internal/integrations/freshrss` polling or webhook bridge for feed alerts.
5. Add `backend/internal/integrations/ntfy` publish/subscribe integration for homelab events.

## Self-Review

- **Spec coverage:** overlays first is implemented in Phase 1. Dashboard, OBS WebSocket, Twitch all-features, and Gitea/n8n/FreshRSS/ntfy integrations are explicitly sequenced after the overlay pipeline.
- **YAGNI check:** no router, no database migrations, no OAuth, and no OBS client in the first slice.
- **Verification:** every implementation task includes targeted tests plus final `make verify`.
