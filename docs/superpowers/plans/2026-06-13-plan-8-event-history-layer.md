# Event History Layer Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Add a lightweight persisted event-history layer so operators can review recent stream events after overlay reconnects, dashboard reloads, or short API restarts.

**Architecture:** Keep the live SSE bus as the real-time transport and add a separate file-backed JSONL event history on the existing `api-data` volume. All backend producers publish through one server helper that records sanitized event summaries before fanout; dashboard clients read recent events via a read-only JSON endpoint.

**Tech Stack:** Go 1.26 backend, file-backed JSONL persistence, existing HTTP/SSE event model, Vite React TypeScript dashboard, Docker Compose `api-data` volume.

---

## Status

Plan status: ready for review.

## Acceptance Criteria

- `GET /api/events/history` returns HTTP 200 JSON with recent sanitized events ordered newest-first.
- Event history persists across API restart using `/data/event-history.jsonl` by default.
- History captures manual alerts, webhook alerts, clear events, BRB events, EventSub-generated events, and MPRIS now-playing events.
- History is bounded by count and disk rewrite behavior; default retention is 250 events.
- History does not store operator secrets, webhook secrets, Twitch token values, or raw unbounded Twitch payloads.
- EventSub chat messages and command diagnostics store only the same top-level event fields plus bounded/sanitized `data` fields.
- If the history file is missing or malformed, the API still boots; `/readyz` reports event history degraded when load failed.
- Runtime append errors are logged and skipped for live fanout safety; this sprint reports load-time history health only.
- `/dashboard/live` includes a compact recent event history panel.
- `/overlay/debug` keeps its real-time behavior and is documented separately from persisted history.
- `make verify` passes after every implementation task.

## Non-goals

- Do not add a database driver or move history into Postgres in this sprint.
- Do not replay persisted history into overlay browser sources.
- Do not add search, analytics, charts, or long-term reporting.
- Do not record full raw EventSub payloads.
- Do not expose a delete endpoint; history rotation is count-based only.

## File Structure

- Create `backend/internal/history/history.go`: file-backed history store, event sanitization, append/load/list/clear-in-memory helpers.
- Create `backend/internal/history/history_test.go`: persistence, malformed file, retention, sanitization tests.
- Modify `backend/internal/config/config.go`: add `EventHistoryFile` and `EventHistoryLimit` with env vars.
- Modify `.env.example` and `docker-compose.yml`: document and wire `EVENT_HISTORY_FILE=/data/event-history.jsonl`, `EVENT_HISTORY_LIMIT=250`.
- Modify `backend/internal/http/server.go`: initialize history store, capture load errors, route MPRIS/EventSub/manual publishers through a helper.
- Modify `backend/internal/http/handlers.go`: add `publishEvent`, `eventHistory`, readiness check, and replace direct `s.bus.Publish` calls.
- Modify `backend/internal/http/routes.go`: register `GET /api/events/history`.
- Modify `backend/internal/http/handlers_test.go`: endpoint, publisher recording, malformed history readiness, restart persistence tests.
- Modify `web/src/lib/api.ts`: add `EventHistoryItem`, `EventHistoryResponse`, `fetchEventHistory`.
- Create `web/src/routes/dashboard/components/EventHistoryPanel.tsx`: read-only compact history panel.
- Modify `web/src/routes/dashboard/Live.tsx`: fetch/render event history alongside readiness/preflight.
- Modify `web/src/App.test.tsx`: route/static panel tests and direct panel fixture tests.
- Modify `README.md`, `docs/runbooks/production-recovery.md`, and `docs/runbooks/live-stream-preflight.md`: document history behavior and recovery.
- Create `docs/runbooks/event-history.md`: focused operator runbook for persisted recent history.

## Data Contract

Backend response:

```json
{
  "events": [
    {
      "id": "event-id",
      "type": "alert",
      "source": "manual",
      "title": "Stream starting",
      "message": "Welcome in",
      "severity": "success",
      "data": { "retainedStateKey": "ticker" },
      "createdAt": "2026-06-13T12:34:56Z"
    }
  ]
}
```

Sanitization rules:

- Preserve `id`, `type`, `source`, `title`, `message`, `severity`, `createdAt`.
- Trim string fields and cap `title` to 120 runes, `message` to 500 runes, and string `data` values to 240 runes.
- Preserve only scalar `data` values (`string`, `bool`, Go numeric types, numeric JSON types) plus `[]string` or JSON-decoded `[]any` containing strings, capped to 10 items.
- Drop nested maps/objects, raw Twitch payloads such as `twitchEvent`, and keys containing `secret`, `token`, `authorization`, `password`, or `cookie` case-insensitively.
- Keep at most `EVENT_HISTORY_LIMIT` events in memory and on rewritten file.

---

## Task 1: Backend History Store

**Files:**
- Create: `backend/internal/history/history.go`
- Create: `backend/internal/history/history_test.go`
- Modify: `backend/internal/config/config.go`
- Modify: `.env.example`
- Modify: `docker-compose.yml`

- [ ] **Step 1: Add history config fields**

Add to `backend/internal/config/config.go`:

```go
EventHistoryFile  string
EventHistoryLimit int
```

In `Load()`, set:

```go
EventHistoryFile:  env("EVENT_HISTORY_FILE", "/data/event-history.jsonl"),
EventHistoryLimit: envInt("EVENT_HISTORY_LIMIT", 250),
```

Add helper:

```go
func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
```

Import `strconv`.

- [ ] **Step 2: Wire compose/env examples**

Add to `docker-compose.yml` API environment:

```yaml
EVENT_HISTORY_FILE: ${EVENT_HISTORY_FILE:-/data/event-history.jsonl}
EVENT_HISTORY_LIMIT: ${EVENT_HISTORY_LIMIT:-250}
```

Add matching lines to `.env.example` near `OVERLAY_STATE_FILE`:

```dotenv
EVENT_HISTORY_FILE=/data/event-history.jsonl
EVENT_HISTORY_LIMIT=250
```

- [ ] **Step 3: Implement `history.Store`**

Create `backend/internal/history/history.go` with:

```go
package history

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

type Store struct {
	path  string
	limit int
	mu    sync.RWMutex
	items []events.Event
}

type Response struct {
	Events []events.Event `json:"events"`
}
```

Required exported functions/methods:

```go
func New(path string, limit int) *Store
func Sanitize(event events.Event) events.Event
func (s *Store) Load() error
func (s *Store) Append(event events.Event) error
func (s *Store) List(limit int) []events.Event
```

Implementation requirements:
- `New` treats `limit <= 0` as `250`.
- `Load` returns nil when path is empty or file missing.
- `Load` reads JSONL line-by-line, sanitizes each event, skips blank lines, returns an error for malformed JSON, and keeps only newest `s.limit` events in chronological in-memory order.
- `Append` sanitizes, appends to in-memory chronological list, trims old events, creates parent directory, and rewrites the JSONL file with current retained events. If the configured path is empty, `Append` updates memory only and returns nil.
- `List(limit)` returns newest-first, with `limit <= 0` meaning all retained events.
- `Sanitize` applies the data contract above and always preserves valid `CreatedAt`; if zero, set `time.Now().UTC()`. String caps are by rune count, not bytes.

- [ ] **Step 4: Add store tests**

Create tests in `backend/internal/history/history_test.go`:

```go
func TestStoreAppendLoadAndListNewestFirst(t *testing.T)
func TestStoreRetentionLimit(t *testing.T)
func TestStoreMissingFileDoesNotError(t *testing.T)
func TestStoreMalformedFileReturnsError(t *testing.T)
func TestSanitizeDropsSensitiveAndNestedData(t *testing.T)
```

Use `t.TempDir()`, synthetic events, and assert:
- loaded history persists across new store instances.
- `List(2)` returns newest two first.
- sensitive keys `operatorSecret`, `access_token`, `password`, `authorization` are absent.
- `twitchEvent` nested payload is absent.
- long strings are capped.
- `[]any{"one", "two"}` loaded from JSONL is retained as capped string data.
- Go numeric values like `int`, `int64`, and `float64` are retained.
- empty file path appends to memory and returns nil.

- [ ] **Step 5: Verify and commit**

Run:

```bash
(cd backend && go test ./internal/history ./internal/config -count=1)
make verify
```

Expected: pass.

Commit:

```bash
git add backend/internal/history backend/internal/config/config.go .env.example docker-compose.yml
git commit -m "feat: add event history store"
```

---

## Task 2: Backend History Endpoint and Producer Wiring

**Files:**
- Modify: `backend/internal/http/server.go`
- Modify: `backend/internal/http/handlers.go`
- Modify: `backend/internal/http/routes.go`
- Modify: `backend/internal/http/handlers_test.go`

- [ ] **Step 1: Add history store to server**

In `backend/internal/http/server.go`, import `internal/history` and add fields:

```go
history          *history.Store
historyLoadError string
```

In `NewServer`, create and load:

```go
eventHistory := history.New(cfg.EventHistoryFile, cfg.EventHistoryLimit)
historyLoadError := ""
if err := eventHistory.Load(); err != nil {
	historyLoadError = err.Error()
	log.Printf("failed to load event history: %v", err)
}
```

Set these fields in `Server`.

- [ ] **Step 2: Centralize event publishing**

Add to `backend/internal/http/handlers.go`:

```go
func (s *Server) publishEvent(event events.Event) {
	if s.history != nil {
		if err := s.history.Append(event); err != nil {
			log.Printf("failed to append event history: %v", err)
		}
	}
	s.bus.Publish(event)
}
```

Replace direct `s.bus.Publish(...)` calls in this file with `s.publishEvent(...)`, including:
- `testAlert`
- `clearEvents`
- `brbStatus`
- `publishEventSubEvent`
- `webhook`

In `server.go`, change MPRIS poller `Publish` from `s.bus.Publish` to `s.publishEvent`. EventSub already uses `s.publishEventSubEvent`, so no separate change is needed there.

- [ ] **Step 3: Add history endpoint**

Add handler to `handlers.go`:

```go
func (s *Server) eventHistory(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			http.Error(w, "limit must be a positive integer", http.StatusBadRequest)
			return
		}
		if parsed > 250 {
			parsed = 250
		}
		limit = parsed
	}
	items := []events.Event{}
	if s.history != nil {
		items = s.history.List(limit)
	}
	writeJSON(w, history.Response{Events: items})
}
```

Import `strconv` and `internal/history`. Keep the `items` variable name to avoid colliding with the `events` package import.

Register in `routes.go`:

```go
s.mux.HandleFunc("GET /api/events/history", s.eventHistory)
```

- [ ] **Step 4: Add readiness check**

In `readyz`, append after retained state:

```go
if s.historyLoadError != "" {
	checks = append(checks, readinessCheck{Name: "event_history", Status: "degraded", Hint: "event history file could not be loaded; back it up or clear malformed JSONL"})
} else {
	checks = append(checks, readinessCheck{Name: "event_history", Status: "ok"})
}
```

Update tests expecting the readiness check set to include `event_history`.

- [ ] **Step 5: Add HTTP tests**

In `backend/internal/http/handlers_test.go`, add tests:

```go
func TestEventHistoryRecordsManualAlertAndClear(t *testing.T)
func TestEventHistoryEndpointLimitAndValidation(t *testing.T)
func TestEventHistoryPersistsAcrossServerRestart(t *testing.T)
func TestReadyzReportsHistoryLoadFailure(t *testing.T)
```

Use temp `EventHistoryFile` in config. For alert tests, set operator secret empty unless testing auth is needed. Assert endpoint order newest-first and that sensitive `data` keys sent in alert payload are not returned.

- [ ] **Step 6: Verify and commit**

Run:

```bash
(cd backend && go test ./internal/http ./internal/history -run 'EventHistory|Readyz|Healthz' -count=1)
make verify
```

Expected: pass.

Commit:

```bash
git add backend/internal/http backend/internal/history
git commit -m "feat: expose event history endpoint"
```

---

## Task 3: Dashboard Event History Panel

**Files:**
- Modify: `web/src/lib/api.ts`
- Create: `web/src/routes/dashboard/components/EventHistoryPanel.tsx`
- Modify: `web/src/routes/dashboard/Live.tsx`
- Modify: `web/src/App.test.tsx`

- [ ] **Step 1: Add API helper**

In `web/src/lib/api.ts`, import event-related primitive types from `./events`:

```ts
import type { OverlayEventType, OverlaySeverity } from './events';
```

Add:

```ts
export type EventHistoryItem = {
  id: string;
  type: OverlayEventType;
  source: string;
  title?: string;
  message?: string;
  severity?: OverlaySeverity;
  data?: Record<string, unknown>;
  createdAt: string;
};

export type EventHistoryResponse = {
  events: EventHistoryItem[];
};

export async function fetchEventHistory(limit = 24, explicitBaseUrl?: string) {
  const boundedLimit = Math.max(1, Math.min(limit, 250));
  const response = await fetch(apiUrl(`/api/events/history?limit=${boundedLimit}`, explicitBaseUrl), { headers: { Accept: 'application/json' } });
  if (!response.ok) {
    throw new Error(`Event history failed: ${response.status}`);
  }
  return (await response.json()) as EventHistoryResponse;
}
```

- [ ] **Step 2: Add panel component**

Create `web/src/routes/dashboard/components/EventHistoryPanel.tsx`:

```tsx
import type { EventHistoryItem } from '../../../lib/api';

function formatEventTypeLabel(type: string) {
  return type.replaceAll('_', ' ');
}

type EventHistoryPanelProps = {
  events: EventHistoryItem[];
  loading?: boolean;
  error?: string;
};

export function EventHistoryPanel({ events, loading = false, error = '' }: EventHistoryPanelProps) {
  return (
    <section className="hud-panel hud-corners p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="hud-label">persisted stream data</p>
          <h2 className="text-xl font-black uppercase tracking-[-0.04em]">Event history</h2>
          <p className="mt-1 text-xs text-zinc-400">Recent sanitized events from /api/events/history.</p>
        </div>
        <span className="hud-chip">{loading ? 'loading' : `${events.length} events`}</span>
      </div>
      {error ? <p className="mt-3 text-xs text-red-300">{error}</p> : null}
      {!loading && events.length === 0 ? <p className="mt-3 text-sm text-zinc-400">No persisted events yet.</p> : null}
      <div className="mt-4 grid gap-2">
        {events.slice(0, 12).map((event) => (
          <article key={event.id} className="hud-terminal-line">
            <span className="hud-terminal-key">{formatEventTypeLabel(event.type)}</span>
            <span className="hud-terminal-value">{event.title || event.message || event.source}</span>
            <span className="text-xs text-zinc-500">{event.source}</span>
          </article>
        ))}
      </div>
    </section>
  );
}
```

- [ ] **Step 3: Wire live cockpit**

In `web/src/routes/dashboard/Live.tsx`:
- import `fetchEventHistory`, `EventHistoryResponse`, and `EventHistoryPanel`.
- define state as:

```tsx
type LiveStatus = {
  twitch: TwitchStatus | null;
  obs: OBSStatus | null;
  readiness: ReadinessReport | null;
  history: EventHistoryResponse | null;
  loading: boolean;
  error: string;
  readinessError: string;
  historyError: string;
};
```

- include `fetchEventHistory(24)` in `Promise.allSettled`.
- render below `SessionLogPanel`:

```tsx
<EventHistoryPanel events={status.history?.events ?? []} loading={status.loading} error={status.historyError} />
```

Use `Promise.allSettled` exactly like current Twitch/OBS/readiness calls so history failures do not block the cockpit.

- [ ] **Step 4: Add tests**

In `web/src/App.test.tsx`:
- Update `/dashboard/live` SSR assertions to include `Event history` and `No persisted events yet.`.
- Add a direct `EventHistoryPanel` SSR fixture test with two events asserting newest labels/titles render.

- [ ] **Step 5: Verify and commit**

Run:

```bash
pnpm --dir web run test -- --run src/App.test.tsx
pnpm --dir web run build
make verify
```

Expected: pass.

Commit:

```bash
git add web/src/lib/api.ts web/src/routes/dashboard/Live.tsx web/src/routes/dashboard/components/EventHistoryPanel.tsx web/src/App.test.tsx
git commit -m "feat: add event history cockpit panel"
```

---

## Task 4: Documentation and Recovery Guidance

**Files:**
- Modify: `README.md`
- Modify: `docs/runbooks/live-stream-preflight.md`
- Modify: `docs/runbooks/production-recovery.md`
- Create: `docs/runbooks/event-history.md`

- [ ] **Step 1: Add event history runbook**

Create `docs/runbooks/event-history.md` with these sections:
- Purpose: persisted recent event history for operators; not an analytics database.
- Storage: `/data/event-history.jsonl` on `api-data`, controlled by `EVENT_HISTORY_FILE` and `EVENT_HISTORY_LIMIT`.
- What is stored: sanitized top-level event fields and bounded scalar data.
- What is not stored: secrets, tokens, raw EventSub payloads, long-term analytics.
- Visibility: `/api/events/history` is a public read endpoint like the SSE stream and `/overlay/debug`; it can expose recent sanitized chat/user/event metadata to anyone who can reach the API.
- Fast checks:

```bash
curl -fsS http://localhost:8080/api/events/history | jq .
curl -fsS 'http://localhost:8080/api/events/history?limit=5' | jq .
```

- Recovery:

```bash
docker compose exec api sh -c 'cp /data/event-history.jsonl /data/event-history.jsonl.bak'
docker compose exec api sh -c 'rm /data/event-history.jsonl'
docker compose restart api
```

- Explain `/overlay/debug` is live-only while `/api/events/history` is persisted recent history.

- [ ] **Step 2: Link docs**

Update README near live operation/runbook links with `docs/runbooks/event-history.md`.

Update `docs/runbooks/live-stream-preflight.md` to mention checking the event history panel after rehearsal.

Update `docs/runbooks/production-recovery.md` to include event history in API data backup/removal cautions and `/readyz` `event_history` degraded recovery.

- [ ] **Step 3: Verify and commit**

Run:

```bash
make verify
```

Expected: pass.

Commit:

```bash
git add README.md docs/runbooks/event-history.md docs/runbooks/live-stream-preflight.md docs/runbooks/production-recovery.md
git commit -m "docs: add event history runbook"
```

---

## Task 5: Final Integration and Plan Completion

**Files:**
- Modify: `docs/superpowers/plans/2026-06-13-plan-8-event-history-layer.md`

- [ ] **Step 1: Run compose and verification checks**

Run:

```bash
docker compose config --quiet
docker compose -f docker-compose.yml -f docker-compose.override.example.yml config --quiet
make verify
docker compose up -d --build
make smoke
curl --max-time 15 -fsS http://localhost:8080/api/events/history
curl --max-time 15 -fsS http://localhost:5173/dashboard/live >/dev/null
```

Expected: all pass. `/readyz` may still report `status: degraded` when EventSub is not connected; HTTP success is enough for smoke.

- [ ] **Step 2: Update plan completion status**

Update this file’s status block:

```markdown
Plan status: complete.

Completed:
- Event history store, endpoint, dashboard panel, and docs are implemented.
- Verification: make verify, compose config checks, docker compose up -d --build, make smoke, /api/events/history route check, /dashboard/live route check.

Live QA note: confirm history entries after the next real rehearsal or stream.
```

Check all task boxes in this plan.

- [ ] **Step 3: Commit completion**

Run:

```bash
git add docs/superpowers/plans/2026-06-13-plan-8-event-history-layer.md
git commit -m "docs: mark plan 8 complete"
```

Do not push unless explicitly requested.
