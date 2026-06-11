# Polished Stream Tool Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Bring `over-laid` from a working OBS/Twitch overlay MVP to a polished daily-use stream tool with protected controls, reliable overlay state, faster operator workflows, and deployment-ready defaults.

**Architecture:** Keep the app simple: Go API + in-memory event fanout + small retained/persisted state snapshots + Vite React HUD overlays. Protect mutating control endpoints with one operator secret rather than adding accounts/RBAC. Polish dashboard ergonomics and OBS controls without introducing a large state-management or component framework.

**Tech Stack:** Go 1.26, `net/http`, Vite React TypeScript, Vitest, Docker Compose, nginx, existing Twitch EventSub/OBS WebSocket/MPRIS integrations.

---

## Current baseline

- Branch: `main`, remote `origin=https://git.subcult.tv/PatrickFanella/over-laid.git`.
- Recent committed features: overlay MVP, Twitch EventSub, dashboard, webhook secret, OBS WebSocket controls, HUD visuals, data-driven goals/ticker, MPRIS now-playing, MPRIS active-player selection, BRB controls.
- User has already smoke-tested most core flows and says things look good.
- Always run `make verify` before each commit.
- Commit and push each slice independently.
- Never stage `.env` or secret values.

## File structure map

- `backend/internal/config/config.go` — env config surface. Add `OPERATOR_SECRET`, state file path if needed, HTTP limits/timeouts if configured.
- `backend/internal/events/events.go` — overlay event types and constructors. Add state-oriented event constructors only when needed.
- `backend/internal/events/bus.go` — transient SSE fanout. Keep simple; do not turn into an event store.
- `backend/internal/http/server.go` — shared server state, background workers, retained overlay snapshots.
- `backend/internal/http/routes.go` — route table and CORS headers.
- `backend/internal/http/handlers.go` — event/BRB/webhook handlers and SSE stream.
- `backend/internal/http/obs_handlers.go` — OBS HTTP command/status handlers.
- `backend/internal/obsws/client.go` — OBS WebSocket protocol client. Extend for input/scene item discovery.
- `web/src/lib/api.ts` — browser API helpers. Add operator auth header support and OBS inventory helpers.
- `web/src/lib/eventStream.ts` / `web/src/lib/events.ts` — SSE client and event parser.
- `web/src/routes/Dashboard.tsx` — main operator console; polish and workflow improvements go here.
- `web/src/routes/Overlay*.tsx` — browser-source overlays.
- `web/src/styles.css` — shared HUD design tokens and utilities.
- `.env.example`, `docker-compose.yml`, `README.md` — operational docs/config.

---

### Task 1: Operator Control-Plane Protection

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/http/routes.go`
- Modify: `backend/internal/http/handlers.go`
- Modify: `backend/internal/http/obs_handlers.go`
- Modify: `backend/internal/http/handlers_test.go`
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/routes/Dashboard.tsx`
- Modify: `.env.example`
- Modify: `docker-compose.yml`
- Modify: `README.md`

- [ ] **Step 1: Add failing backend tests for protected mutating routes**

Add tests to `backend/internal/http/handlers_test.go`:

```go
func TestOperatorSecretProtectsTestAlert(t *testing.T) {
    server := NewServer(config.Config{AllowedOrigin: "*", OperatorSecret: "operator-secret"}, events.NewBus(4))
    req := httptest.NewRequest(http.MethodPost, "/api/events/test-alert", bytes.NewBufferString(`{"title":"Nope","message":"Blocked"}`))
    rec := httptest.NewRecorder()

    server.Handler().ServeHTTP(rec, req)

    if rec.Code != http.StatusUnauthorized {
        t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
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
```

Run: `cd backend && go test ./internal/http -run 'TestOperatorSecret' -count=1`
Expected: FAIL because `OperatorSecret` and middleware do not exist.

- [ ] **Step 2: Implement operator secret config and middleware**

In `backend/internal/config/config.go`, add:

```go
OperatorSecret string
```

Load it with:

```go
OperatorSecret: env("OPERATOR_SECRET", ""),
```

In `backend/internal/http/handlers.go`, add helper methods:

```go
func (s *Server) authorizeOperator(r *http.Request) bool {
    secret := strings.TrimSpace(s.config.OperatorSecret)
    if secret == "" {
        return true
    }
    provided := strings.TrimSpace(r.Header.Get("X-Overlay-Operator-Secret"))
    if provided == "" {
        provided = bearerToken(r.Header.Get("Authorization"))
    }
    if provided == "" || len(provided) != len(secret) {
        return false
    }
    return subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) == 1
}

func (s *Server) requireOperator(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if !s.authorizeOperator(r) {
            http.Error(w, "operator authorization required", http.StatusUnauthorized)
            return
        }
        next(w, r)
    }
}
```

In `backend/internal/http/routes.go`, wrap mutating operator routes:

```go
s.mux.HandleFunc("POST /api/events/test-alert", s.requireOperator(s.testAlert))
s.mux.HandleFunc("POST /api/events/clear", s.requireOperator(s.clearEvents))
s.mux.HandleFunc("POST /api/events/brb", s.requireOperator(s.brbStatus))
s.mux.HandleFunc("POST /api/obs/scene", s.requireOperator(s.obsSetScene))
s.mux.HandleFunc("POST /api/obs/input/mute", s.requireOperator(s.obsSetInputMute))
s.mux.HandleFunc("POST /api/obs/scene-item/enabled", s.requireOperator(s.obsSetSceneItemEnabled))
```

Leave `GET /api/events/stream` readable for OBS browser sources.

- [ ] **Step 3: Wire dashboard operator secret**

In `web/src/lib/api.ts`, extend `postJSON` with an optional operator secret header:

```ts
headers['X-Overlay-Operator-Secret'] = operatorSecret.trim();
```

Thread the value through `sendTestAlert`, `clearOverlays`, `setBRBStatus`, and OBS command helpers.

In `web/src/routes/Dashboard.tsx`, add a password input labeled `Operator secret` and keep it in component state. Do not persist it yet.

- [ ] **Step 4: Update env/docs**

Add to `.env.example` and `docker-compose.yml`:

```env
OPERATOR_SECRET=
```

Update `README.md` to state: if `OPERATOR_SECRET` is set, dashboard mutating actions must send it as `X-Overlay-Operator-Secret` or `Authorization: Bearer <secret>`.

- [ ] **Step 5: Verify and commit**

Run:

```bash
gofmt -w backend/internal/config/config.go backend/internal/http/*.go
make verify
git diff --check
```

Commit:

```bash
git add .env.example docker-compose.yml README.md backend/internal/config/config.go backend/internal/http web/src/lib/api.ts web/src/routes/Dashboard.tsx
git commit -m "feat: protect operator control endpoints"
git push origin main
```

---

### Task 2: Retained Overlay State and SSE Snapshots

**Files:**
- Create: `backend/internal/state/state.go`
- Create: `backend/internal/state/state_test.go`
- Modify: `backend/internal/http/server.go`
- Modify: `backend/internal/http/handlers.go`
- Modify: `backend/internal/http/handlers_test.go`
- Modify: `web/src/routes/OverlayGoals.tsx`
- Modify: `web/src/routes/OverlayTicker.tsx`
- Modify: `README.md`

- [ ] **Step 1: Add state store tests**

Create `backend/internal/state/state_test.go` with tests for setting, listing, deleting, and JSON-file persistence:

```go
func TestStoreSetListDelete(t *testing.T) { /* set brb/goals/ticker events, list in insertion order, delete one */ }
func TestFileStoreRoundTrip(t *testing.T) { /* save state file, load in new store, compare keys */ }
```

Run: `cd backend && go test ./internal/state -count=1`
Expected: FAIL until the package exists.

- [ ] **Step 2: Implement tiny retained state store**

Create `backend/internal/state/state.go` with:

```go
type Snapshot struct { Key string `json:"key"`; Event events.Event `json:"event"` }
type Store struct { mu sync.RWMutex; path string; items map[string]events.Event; order []string }
func New(path string) *Store
func (s *Store) Set(key string, event events.Event) error
func (s *Store) Delete(key string) error
func (s *Store) Clear() error
func (s *Store) List() []events.Event
func (s *Store) Load() error
func (s *Store) Save() error
```

Use one JSON file under `/data/overlay-state.json` by default. Keep file mode `0600`, parent dirs `0700`.

- [ ] **Step 3: Replace server-only BRB retention with state store snapshots**

In `backend/internal/config/config.go`, add `OverlayStateFile` default `/data/overlay-state.json`.

In `backend/internal/http/server.go`, add `state *state.Store`, load it in `NewServer`, and remove dedicated BRB mutex fields.

In `streamEvents`, after `: connected`, replay `s.state.List()` before flushing.

In `brbStatus`, `s.state.Set("brb", event)` before publish.

In `clearEvents`, `s.state.Clear()` before publishing clear.

- [ ] **Step 4: Store goal/ticker state from webhook/test-alert data**

When an alert/webhook includes `data.goal`, `data.goals`, or `data.ticker`, store a derived state event under keys `goals` and `ticker`. Keep this simple: retain the last event containing each state payload; do not persist chat/alerts.

- [ ] **Step 5: Update frontend empty/default behavior**

Remove fake demo defaults from `OverlayGoals.tsx` and `OverlayTicker.tsx`. Show intentional standby copy until retained state arrives:

```text
Waiting for goal events
Waiting for ticker events
```

- [ ] **Step 6: Verify and commit**

Run:

```bash
gofmt -w backend/internal/state/*.go backend/internal/http/*.go backend/internal/config/config.go
make verify
git diff --check
```

Commit:

```bash
git add backend/internal/state backend/internal/config/config.go backend/internal/http README.md web/src/routes/OverlayGoals.tsx web/src/routes/OverlayTicker.tsx .env.example docker-compose.yml
git commit -m "feat: retain overlay state snapshots"
git push origin main
```

---

### Task 3: Dashboard Console Polish and Safer Controls

**Files:**
- Modify: `web/src/routes/Dashboard.tsx`
- Modify: `web/src/styles.css`
- Modify: `web/src/App.test.tsx`

- [ ] **Step 1: Add dashboard route tests for status rail and confirmation**

In `web/src/App.test.tsx`, assert `/dashboard` renders:

```ts
expect(html).toContain('System status');
expect(html).toContain('Operator secret');
expect(html).toContain('Confirm clear');
expect(html).toContain('Overlay monitor');
```

Run: `pnpm --dir web run test -- --run src/App.test.tsx`
Expected: FAIL.

- [ ] **Step 2: Convert dashboard to HUD console layout**

In `Dashboard.tsx`, replace the generic glass top sections with `hud-panel`, `hud-corners`, `hud-label`, `hud-chip`, and a top `System status` rail containing Twitch, OBS, API, and action state.

- [ ] **Step 3: Add safer action behavior**

Disable mutating buttons while `actionState === 'running'`. Add a two-step clear overlays button:

```tsx
const [clearArmed, setClearArmed] = useState(false);
```

First click sets `clearArmed`; second click within the visible armed state calls `clearOverlays`.

- [ ] **Step 4: Add overlay monitor cards**

Add an `Overlay monitor` section with route cards for all browser sources, last-known status text, and direct open links. Keep it text-only; no iframes.

- [ ] **Step 5: Verify and commit**

Run:

```bash
pnpm --dir web run format
pnpm --dir web run lint
pnpm --dir web run test
pnpm --dir web run build
make verify
```

Commit:

```bash
git add web/src/routes/Dashboard.tsx web/src/styles.css web/src/App.test.tsx
git commit -m "feat: polish operator dashboard"
git push origin main
```

---

### Task 4: OBS Inventory and Control Quality-of-Life

**Files:**
- Modify: `backend/internal/obsws/client.go`
- Modify: `backend/internal/obsws/client_test.go`
- Modify: `backend/internal/http/obs_handlers.go`
- Modify: `backend/internal/http/routes.go`
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/routes/Dashboard.tsx`
- Modify: `README.md`

- [ ] **Step 1: Add OBS client tests for input and scene item request shapes**

Add tests around request envelopes for:

```text
GetInputList
GetSceneItemList
```

Run: `cd backend && go test ./internal/obsws -count=1`
Expected: FAIL until implemented.

- [ ] **Step 2: Implement OBS inventory methods**

Add to `backend/internal/obsws/client.go`:

```go
type Input struct { InputName string `json:"inputName"`; InputKind string `json:"inputKind,omitempty"`; InputUuid string `json:"inputUuid,omitempty"` }
type SceneItem struct { SceneItemID int `json:"sceneItemId"`; SourceName string `json:"sourceName"`; SceneItemEnabled bool `json:"sceneItemEnabled"` }
func (c Client) GetInputList(ctx context.Context) ([]Input, error)
func (c Client) GetSceneItemList(ctx context.Context, sceneName string) ([]SceneItem, error)
```

- [ ] **Step 3: Add HTTP routes and frontend selectors**

Routes:

```go
GET /api/obs/inputs
GET /api/obs/scene-items?sceneName=<name>
```

Dashboard should use dropdowns instead of free-text for OBS input and scene item where inventory is available, with free-text fallback if OBS is offline.

- [ ] **Step 4: Verify and commit**

Run `make verify`, then commit:

```bash
git add backend/internal/obsws backend/internal/http web/src/lib/api.ts web/src/routes/Dashboard.tsx README.md
git commit -m "feat: add obs inventory controls"
git push origin main
```

---

### Task 5: Production Guardrails and Deploy Documentation

**Files:**
- Modify: `backend/cmd/app/main.go`
- Modify: `backend/internal/http/handlers.go`
- Modify: `backend/internal/http/obs_handlers.go`
- Modify: `backend/Dockerfile`
- Modify: `web/nginx.conf`
- Modify: `docker-compose.yml`
- Create: `docker-compose.override.example.yml`
- Modify: `README.md`

- [ ] **Step 1: Add HTTP server timeouts and body limits**

In `backend/cmd/app/main.go`, set:

```go
ReadHeaderTimeout: 5 * time.Second,
ReadTimeout:       15 * time.Second,
WriteTimeout:      0, // keep SSE alive; document why
IdleTimeout:       60 * time.Second,
```

In JSON handlers, wrap request bodies with `http.MaxBytesReader(w, r.Body, 1<<20)`.

- [ ] **Step 2: Add Compose restart/health checks**

In `docker-compose.yml`, add `restart: unless-stopped` and health checks for API and web. Avoid exposing Postgres by default unless the project still needs host access.

- [ ] **Step 3: Add deployment modes doc**

In `README.md`, add sections:

```text
Local dev
LAN-only streaming
Public reverse proxy
```

Document that dashboard/API mutating routes require `OPERATOR_SECRET`, overlays need readable web route + SSE, and public exposure should use reverse proxy auth.

- [ ] **Step 4: Verify and commit**

Run:

```bash
make verify
docker compose config --quiet
git diff --check
```

Commit:

```bash
git add backend/cmd/app/main.go backend/internal/http backend/Dockerfile web/nginx.conf docker-compose.yml docker-compose.override.example.yml README.md
git commit -m "chore: harden deployment defaults"
git push origin main
```

---

## Self-review notes

- Spec coverage: plan covers control-plane protection, retained state/reconnect reliability, dashboard polish, OBS usability, production/deploy hardening, and docs.
- YAGNI checks: no user accounts/RBAC, no Redis/Kafka, no plugin architecture, no full event sourcing.
- Commit strategy: each task is a separate verified commit pushed to `origin/main`.
