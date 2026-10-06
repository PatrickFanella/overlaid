# Plan 5: Production Hardening and Recovery Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Make live-stream operations easier to diagnose and recover when OBS, Twitch, retained state, or smoke checks fail.

**Architecture:** Keep `/healthz` as a cheap process liveness endpoint and add a JSON `/readyz` readiness report for operator/deploy diagnostics. Surface dependency status from existing OBS/Twitch/state components without changing auth boundaries. Expand local smoke checks and add recovery tests/docs for state/token failure modes.

**Tech Stack:** Go 1.26 standard-library HTTP, existing OBS/Twitch/status clients, file-backed retained state, Make/Docker Compose, Vite React TypeScript dashboard, Vitest.

---

## Completion status

Status: complete. Implemented, reviewed, verified, committed, rebuilt/deployed locally, smoke-checked, and route spot-checked.

Verification passed:

- `make verify`
- `docker compose config --quiet`
- `docker compose -f docker-compose.yml -f docker-compose.override.example.yml config --quiet`
- `docker compose up -d --build`
- `make smoke`
- Route spot-checks: `/readyz`, `/dashboard/live`, `/overlay/alerts`

Live QA result: pending real outage/recovery rehearsal.

---

## Current Baseline

- `GET /healthz` in `backend/internal/http/handlers.go` returns plain `ok` and is used by Compose healthchecks.
- `/dashboard/live` combines Twitch and OBS status in the browser, but there is no backend readiness JSON endpoint.
- `Makefile` `smoke` checks only `/healthz`, `/overlay/alerts`, and `/dashboard`.
- `state.Store.Load()` tolerates missing files but returns an error for malformed JSON.
- `twitchAuthStore.loadPersistedToken()` tolerates missing token files and records `token_file_invalid` for malformed token files.
- Docs mention live preflight, visual QA, Twitch chat commands, and OBS sequences, but not a focused recovery checklist.

## Sprint Acceptance Criteria

- Keep `/healthz` unchanged as liveness.
- Add `GET /readyz` returning HTTP 200 JSON with `status`, `checks`, and terse recovery hints whenever the process can produce diagnostics.
- Overall `status: degraded` means one or more optional/runtime dependencies need operator attention; `/readyz` is not used by Compose healthchecks.
- Readiness includes separate checks for process, retained state file load status, Twitch config/auth/EventSub status, and OBS configured/reachable/authenticated status.
- A malformed retained state file does not crash startup; `/readyz` reports state as degraded until the file is fixed or cleared.
- Token file recovery behavior is covered by tests and documented: missing token is allowed, malformed token requires Twitch re-login.
- `make smoke` checks `/healthz`, `/readyz`, `/api/auth/twitch/status`, `/api/obs/status`, `/dashboard`, `/dashboard/live`, and `/overlay/alerts`.
- Docs explain how to recover from OBS disconnects, Twitch auth/token problems, EventSub issues, retained state corruption, and destructive Compose volume resets.
- `make verify`, compose config checks, rebuild, smoke, and route spot-checks pass.

## Do Not Do This Sprint

- Do not make `/healthz` depend on OBS or Twitch.
- Do not require Twitch or OBS to be configured for local development to boot.
- Do not expose secrets or token contents in readiness JSON.
- Do not add a database or persistent health history.
- Do not change operator-secret requirements for mutating routes.
- Do not make Compose healthcheck use `/readyz`; keep it on `/healthz` so optional dependency outages do not restart-loop the API.

---

## File Structure

Create:

- `docs/runbooks/production-recovery.md` — recovery procedures for OBS, Twitch, EventSub, retained state, and Docker volumes.

Modify:

- `backend/internal/http/server.go` — store startup state/token load errors for readiness reporting.
- `backend/internal/http/routes.go` — register `GET /readyz`.
- `backend/internal/http/handlers.go` — add readiness response types and handler.
- `backend/internal/http/twitch_auth.go` — factor Twitch status construction so `/readyz` can reuse it without calling the HTTP handler.
- `backend/internal/http/handlers_test.go` — readiness endpoint coverage.
- `backend/internal/state/state_test.go` — explicit malformed/missing retained-state recovery coverage.
- `backend/internal/http/twitch_auth_test.go` — token recovery coverage if any gap remains after inspection.
- `web/src/lib/api.ts` — add optional readiness response type/helper if dashboard display needs it in a later task.
- `Makefile` — expand `smoke` target.
- `README.md` — link the recovery runbook and clarify `/healthz` versus `/readyz`.
- `docs/superpowers/plans/2026-06-13-plan-5-production-hardening-recovery.md` — mark completion when done.

---

### Task 1: Add Backend Readiness Report

**Files:**
- Modify: `backend/internal/http/server.go`
- Modify: `backend/internal/http/routes.go`
- Modify: `backend/internal/http/handlers.go`
- Modify: `backend/internal/http/twitch_auth.go`
- Modify: `backend/internal/http/handlers_test.go`

- [x] **Step 1: Add failing readiness tests**

Add tests to `backend/internal/http/handlers_test.go` near `TestHealthz`:

```go
func TestReadyzReportsProcessAndOptionalDependencies(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*"}, events.NewBus(4))
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Status string `json:"status"`
		Checks []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			Hint   string `json:"hint"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode readyz: %v", err)
	}
	if body.Status == "" || len(body.Checks) == 0 {
		t.Fatalf("expected readiness status and checks, got %#v", body)
	}
	if !readyzHasCheck(body.Checks, "process", "ok") {
		t.Fatalf("expected process ok check, got %#v", body.Checks)
	}
}

func TestReadyzReportsStateLoadFailure(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "overlay-state.json")
	if err := os.WriteFile(statePath, []byte("not-json"), 0o600); err != nil {
		t.Fatalf("write corrupt state: %v", err)
	}
	server := NewServer(config.Config{AllowedOrigin: "*", OverlayStateFile: statePath}, events.NewBus(4))
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 degraded report, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Status string `json:"status"`
		Checks []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			Hint   string `json:"hint"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode readyz: %v", err)
	}
	if body.Status != "degraded" {
		t.Fatalf("expected degraded status, got %#v", body)
	}
	if !readyzHasCheck(body.Checks, "retained_state", "degraded") {
		t.Fatalf("expected retained_state degraded check, got %#v", body.Checks)
	}
}

func readyzHasCheck(checks []struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Hint   string `json:"hint"`
}, name, status string) bool {
	for _, check := range checks {
		if check.Name == name && check.Status == status {
			return true
		}
	}
	return false
}
```

Add `os` to the test imports if missing.

- [x] **Step 2: Run tests to verify failure**

Run:

```bash
(cd backend && go test ./internal/http -run 'Readyz|Healthz' -count=1)
```

Expected: FAIL because `/readyz` is not registered.

- [x] **Step 3: Capture startup load errors**

Modify `backend/internal/http/server.go` `Server`:

```go
type Server struct {
	config                    config.Config
	bus                       *events.Bus
	obs                       obsws.Client
	state                     *state.Store
	stateLoadError            string
	twitch                    *twitchAuthStore
	twitchTokenLoadError      string
	twitchEventSubDiagnostics *twitch.DiagnosticsStore
	mux                       *http.ServeMux
}
```

Update `NewServer`:

```go
store := state.New(cfg.OverlayStateFile)
stateLoadError := ""
if err := store.Load(); err != nil {
	stateLoadError = err.Error()
	log.Printf("failed to load overlay state: %v", err)
}
server := &Server{config: cfg, bus: bus, obs: obsws.Client{URL: cfg.OBSWebSocketURL, Password: cfg.OBSWebSocketPass}, state: store, stateLoadError: stateLoadError, twitch: newTwitchAuthStore(cfg.TwitchTokenFile), twitchEventSubDiagnostics: twitch.NewDiagnosticsStore(cfg.TwitchEventSub), mux: http.NewServeMux()}
if err := server.twitch.loadPersistedToken(); err != nil {
	server.twitchTokenLoadError = err.Error()
	log.Printf("failed to load twitch token: %v", err)
}
server.routes()
return server
```

- [x] **Step 4: Register and implement `/readyz`**

First factor the existing Twitch status HTTP handler in `backend/internal/http/twitch_auth.go`:

```go
func (s *Server) twitchStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.buildTwitchStatus(r.Context()))
}

func (s *Server) buildTwitchStatus(ctx context.Context) twitchStatusResponse {
	status := twitchStatusResponse{
		Configured:    s.twitchConfigured(),
		Authenticated: false,
		ChannelTarget: s.config.TwitchChannel,
		LoginURL:      "/api/auth/twitch/login",
		Scopes:        append([]string(nil), s.config.TwitchScopes...),
		StreamerLogin: s.config.TwitchChannel,
		EventSub:      s.twitchEventSubDiagnostics.Snapshot(),
	}

	if token, ok := s.ensureTwitchToken(ctx); ok {
		status.Authenticated = true
		status.StreamerLogin = token.Login
		if status.StreamerLogin == "" {
			status.StreamerLogin = s.config.TwitchChannel
		}
		status.StreamerUserID = token.UserID
		expiresAt := token.ExpiresAt
		status.TokenExpiresAt = &expiresAt
	}

	if reason := s.twitch.recentAuthError(); reason != "" {
		status.AuthError = reason
		status.LastAuthError = reason
	}

	return status
}
```

Add `context` to `twitch_auth.go` imports if it is not already imported.

In `backend/internal/http/routes.go`, add:

```go
s.mux.HandleFunc("GET /readyz", s.readyz)
```

In `backend/internal/http/handlers.go`, add response types and handler after `healthz`:

```go
type readinessResponse struct {
	Status string           `json:"status"`
	Checks []readinessCheck `json:"checks"`
}

type readinessCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Hint   string `json:"hint,omitempty"`
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	checks := []readinessCheck{{Name: "process", Status: "ok"}}

	if s.stateLoadError != "" {
		checks = append(checks, readinessCheck{Name: "retained_state", Status: "degraded", Hint: "overlay state file could not be loaded; back it up, inspect JSON, or clear retained overlay state"})
	} else {
		checks = append(checks, readinessCheck{Name: "retained_state", Status: "ok"})
	}

	checks = append(checks, s.buildTwitchReadinessChecks(r.Context())...)

	obsCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	obsStatus := s.obs.Status(obsCtx)
	checks = append(checks, buildOBSReadinessChecks(obsStatus)...)

	status := "ok"
	for _, check := range checks {
		if check.Status != "ok" {
			status = "degraded"
			break
		}
	}

	writeJSON(w, readinessResponse{Status: status, Checks: checks})
}
```

Add helper functions in the same file:

```go
func (s *Server) buildTwitchReadinessChecks(ctx context.Context) []readinessCheck {
	checks := []readinessCheck{}
	if !s.twitchConfigured() {
		return []readinessCheck{
			{Name: "twitch_config", Status: "degraded", Hint: "Twitch is not configured; set Twitch env vars before relying on EventSub"},
			{Name: "twitch_auth", Status: "degraded", Hint: "Twitch cannot authenticate until configured"},
			{Name: "twitch_eventsub", Status: "degraded", Hint: "EventSub cannot connect until Twitch is configured"},
		}
	}
	checks = append(checks, readinessCheck{Name: "twitch_config", Status: "ok"})
	if s.twitchTokenLoadError != "" {
		checks = append(checks,
			readinessCheck{Name: "twitch_auth", Status: "degraded", Hint: "saved Twitch token could not be loaded; re-run Twitch login"},
			readinessCheck{Name: "twitch_eventsub", Status: "degraded", Hint: "EventSub needs valid Twitch auth"},
		)
		return checks
	}
	status := s.buildTwitchStatus(ctx)
	if !status.Authenticated {
		checks = append(checks,
			readinessCheck{Name: "twitch_auth", Status: "degraded", Hint: "Twitch is not authenticated; open /dashboard/live and login"},
			readinessCheck{Name: "twitch_eventsub", Status: "degraded", Hint: "EventSub needs valid Twitch auth"},
		)
		return checks
	}
	checks = append(checks, readinessCheck{Name: "twitch_auth", Status: "ok"})
	if status.EventSub.Enabled && !status.EventSub.Connected {
		checks = append(checks, readinessCheck{Name: "twitch_eventsub", Status: "degraded", Hint: "EventSub is enabled but not connected; check token scopes and API logs"})
		return checks
	}
	checks = append(checks, readinessCheck{Name: "twitch_eventsub", Status: "ok"})
	return checks
}

func buildOBSReadinessChecks(status obsws.Status) []readinessCheck {
	checks := []readinessCheck{}
	if !status.Configured {
		return []readinessCheck{
			{Name: "obs_config", Status: "degraded", Hint: "OBS WebSocket URL is not configured"},
			{Name: "obs_reachable", Status: "degraded", Hint: "OBS cannot be reached until configured"},
			{Name: "obs_auth", Status: "degraded", Hint: "OBS authentication cannot be checked until configured"},
		}
	}
	checks = append(checks, readinessCheck{Name: "obs_config", Status: "ok"})
	if !status.Reachable {
		checks = append(checks,
			readinessCheck{Name: "obs_reachable", Status: "degraded", Hint: "OBS WebSocket is not reachable; check OBS, URL, Docker networking, and host override"},
			readinessCheck{Name: "obs_auth", Status: "degraded", Hint: "OBS auth cannot be checked until reachable"},
		)
		return checks
	}
	checks = append(checks, readinessCheck{Name: "obs_reachable", Status: "ok"})
	if !status.Authenticated {
		checks = append(checks, readinessCheck{Name: "obs_auth", Status: "degraded", Hint: "OBS WebSocket authentication failed; check OBS_WEBSOCKET_PASSWORD"})
		return checks
	}
	checks = append(checks, readinessCheck{Name: "obs_auth", Status: "ok"})
	return checks
}
```

Add `context` and `time` to `handlers.go` imports.

- [x] **Step 5: Run tests and commit**

Run:

```bash
gofmt -w backend/internal/http/server.go backend/internal/http/routes.go backend/internal/http/handlers.go backend/internal/http/twitch_auth.go backend/internal/http/handlers_test.go
(cd backend && go test ./internal/http -run 'Readyz|Healthz' -count=1)
make verify
```

Expected: PASS.

Commit:

```bash
git add backend/internal/http/server.go backend/internal/http/routes.go backend/internal/http/handlers.go backend/internal/http/twitch_auth.go backend/internal/http/handlers_test.go
git commit -m "feat: add readiness diagnostics endpoint"
```

---

### Task 2: Add State and Token Recovery Tests

**Files:**
- Modify: `backend/internal/state/state_test.go`
- Modify: `backend/internal/http/twitch_auth_test.go`

- [x] **Step 1: Add retained-state recovery tests**

Add imports to `backend/internal/state/state_test.go`:

```go
import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)
```

Add tests:

```go
func TestFileStoreLoadMissingFileIsEmpty(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "missing", "overlay-state.json"))
	if err := store.Load(); err != nil {
		t.Fatalf("load missing file: %v", err)
	}
	if got := store.List(); len(got) != 0 {
		t.Fatalf("expected empty state, got %#v", got)
	}
}

func TestFileStoreLoadMalformedFileReturnsErrorAndKeepsEmptyState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "overlay-state.json")
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatalf("write malformed state: %v", err)
	}
	store := New(path)
	err := store.Load()
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("expected JSON error, got %v", err)
	}
	if got := store.List(); len(got) != 0 {
		t.Fatalf("expected empty state after failed load, got %#v", got)
	}
}
```

- [x] **Step 2: Inspect and fill Twitch token recovery coverage**

If `backend/internal/http/twitch_auth_test.go` already covers missing and malformed token files, add only a focused assertion that malformed files set `lastAuthError` to `token_file_invalid` and do not authenticate. Use this shape:

```go
func TestLoadPersistedTokenInvalidFileRecordsRecoveryReason(t *testing.T) {
	path := filepath.Join(t.TempDir(), "twitch-token.json")
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	store := newTwitchAuthStore(path)
	if err := store.loadPersistedToken(); err == nil {
		t.Fatal("expected invalid token file error")
	}
	if _, ok := store.snapshot(); ok {
		t.Fatal("expected no token after invalid file")
	}
	if got := store.recentAuthError(); got != "token_file_invalid" {
		t.Fatalf("expected token_file_invalid, got %q", got)
	}
}
```

If helper names differ, use the existing test helper pattern in `twitch_auth_test.go` instead of introducing duplicate helpers.

- [x] **Step 3: Run tests and commit**

Run:

```bash
gofmt -w backend/internal/state/state_test.go backend/internal/http/twitch_auth_test.go
(cd backend && go test ./internal/state ./internal/http -run 'FileStore|PersistedToken|Token' -count=1)
make verify
```

Expected: PASS.

Commit:

```bash
git add backend/internal/state/state_test.go backend/internal/http/twitch_auth_test.go
git commit -m "test: cover state and token recovery"
```

---

### Task 3: Expand Smoke Checks

**Files:**
- Modify: `Makefile`

- [x] **Step 1: Update smoke target**

Replace the `smoke` target commands in `Makefile` with:

```make
smoke: ## Smoke test API health, readiness, status APIs, and web shell
	curl --max-time 15 -fsS http://localhost:$(API_PORT)/healthz
	curl --max-time 15 -fsS http://localhost:$(API_PORT)/readyz >/dev/null
	curl --max-time 15 -fsS http://localhost:$(API_PORT)/api/auth/twitch/status >/dev/null
	curl --max-time 15 -fsS http://localhost:$(API_PORT)/api/obs/status >/dev/null
	curl --max-time 15 -fsS http://localhost:$(WEB_PORT)/overlay/alerts >/dev/null
	curl --max-time 15 -fsS http://localhost:$(WEB_PORT)/dashboard >/dev/null
	curl --max-time 15 -fsS http://localhost:$(WEB_PORT)/dashboard/live >/dev/null
```

- [x] **Step 2: Run smoke-compatible checks**

Run:

```bash
make verify
```

Expected: PASS. Do not run `make smoke` until the stack is running.

- [x] **Step 3: Commit**

```bash
git add Makefile
git commit -m "chore: expand smoke checks"
```

---

### Task 4: Add Production Recovery Runbook

**Files:**
- Create: `docs/runbooks/production-recovery.md`
- Modify: `README.md`

- [x] **Step 1: Create recovery runbook**

Create `docs/runbooks/production-recovery.md`:

```markdown
# Production Recovery Runbook

Use this when overlays are reachable but OBS, Twitch, EventSub, or retained state look wrong during a stream setup.

## Fast checks

```bash
make smoke
curl -fsS http://localhost:8080/readyz
docker compose ps
docker compose logs --tail=100 api
```

`/healthz` is liveness only. `/readyz` is the dependency report. `/readyz` returns HTTP 200 when the API can produce diagnostics; `status: degraded` means one or more optional/runtime dependencies need operator attention.

## OBS disconnected

1. Open `/dashboard/live` and check OBS status.
2. Confirm OBS is running and WebSocket is enabled in `Tools → WebSocket Server Settings`.
3. Verify `OBS_WEBSOCKET_URL` matches the runtime mode:
   - Docker default: `ws://host.docker.internal:4455`
   - Host networking override: `ws://localhost:4455`
4. If auth is enabled in OBS, verify `OBS_WEBSOCKET_PASSWORD` in `.env`.
5. Restart only the API if needed: `docker compose restart api`.

## Twitch token or auth failure

1. Open `/dashboard/live`.
2. If Twitch says unauthenticated or `/readyz` reports token problems, run Twitch login again from the dashboard.
3. If the token file is malformed, stop the API, back up `/data/twitch-token.json`, remove the bad file, restart the API, then login again:
   ```bash
   docker compose exec api sh -c 'cp /data/twitch-token.json /data/twitch-token.json.bak'
   docker compose exec api sh -c 'rm /data/twitch-token.json'
   docker compose restart api
   ```
4. Confirm EventSub shows `connected: true`; `lastError` clears after a successful subscription reconnect where all planned subscriptions are created.

## EventSub not connected

1. Confirm Twitch auth is valid on `/dashboard/live`.
2. Check API logs for subscription failures.
3. Re-run Twitch login if scopes changed.
4. Restart API to rebuild EventSub subscriptions: `docker compose restart api`.

## Retained overlay state is stale or corrupt

1. Use dashboard clear controls first.
2. If `/readyz` reports retained state load failure, back up `/data/overlay-state.json`:
   ```bash
   docker compose exec api sh -c 'cp /data/overlay-state.json /data/overlay-state.json.bak'
   ```
3. Inspect the JSON. If it is not recoverable, remove the file and restart API:
   ```bash
   docker compose exec api sh -c 'rm /data/overlay-state.json'
   docker compose restart api
   ```
4. Recreate BRB/ticker/goal state from the dashboard if needed.

## Docker volume caution

- `make db-reset-postgres` removes only Postgres data and preserves API token/state data.
- `make db-reset` runs `docker compose down -v` and destroys API token/state data too.
- Back up the `api-data` volume before destructive resets if Twitch auth/state continuity matters.
```

- [x] **Step 2: Link runbook and clarify health/readiness in README**

In `README.md`, add near live-operation runbooks:

```markdown
For production hardening and recovery steps, use `docs/runbooks/production-recovery.md`.
```

Add a short note near deploy modes or guardrails:

```markdown
`/healthz` is process liveness for Compose healthchecks. `/readyz` is a JSON dependency report for OBS, Twitch/EventSub, and retained state diagnostics.
`/readyz` always returns HTTP 200 when the process can produce diagnostics; inspect the JSON `status` and `checks` fields for degraded optional dependencies.
```

- [x] **Step 3: Run verification and commit**

Run:

```bash
make verify
```

Expected: PASS.

Commit:

```bash
git add docs/runbooks/production-recovery.md README.md
git commit -m "docs: add production recovery runbook"
```

---

### Task 5: Final Integration, Deploy, and Plan Update

**Files:**
- Modify: `docs/superpowers/plans/2026-06-13-plan-5-production-hardening-recovery.md`

- [x] **Step 1: Run full local checks**

Run:

```bash
docker compose config --quiet
docker compose -f docker-compose.yml -f docker-compose.override.example.yml config --quiet
make verify
```

Expected: all pass.

- [x] **Step 2: Rebuild and smoke-check**

Run:

```bash
docker compose up -d --build
make smoke
```

Expected: image rebuild succeeds and expanded smoke checks pass.

- [x] **Step 3: Spot-check recovery routes**

Open or curl:

```text
/readyz
/dashboard/live
/overlay/alerts
```

Expected: `/readyz` returns JSON with `status` and `checks`; web routes load without SPA fallback errors.

- [x] **Step 4: Update this plan completion status**

Add this block near the top of this plan after the header:

```markdown
## Completion status

Status: complete. Implemented, reviewed, verified, committed, rebuilt/deployed locally, smoke-checked, and route spot-checked.

Verification passed:

- `make verify`
- `docker compose config --quiet`
- `docker compose -f docker-compose.yml -f docker-compose.override.example.yml config --quiet`
- `docker compose up -d --build`
- `make smoke`
- Route spot-checks: `/readyz`, `/dashboard/live`, `/overlay/alerts`

Live QA result: pending real outage/recovery rehearsal.

---
```

Check every task checkbox that was completed.

- [x] **Step 5: Commit and push**

Run pre-commit inspection:

```bash
git status --short
git diff --stat
git log --oneline -10
```

Then commit and push:

```bash
git add docs/superpowers/plans/2026-06-13-plan-5-production-hardening-recovery.md
git commit -m "docs: mark plan 5 complete"
git push
```

Expected: `main` is synced to `origin/main` and working tree is clean.

---

## Self-Review

- Spec coverage: tasks cover readiness diagnostics, state/token recovery tests, expanded smoke, recovery docs, final deploy verification, and plan completion.
- Placeholder scan: no `TBD`, `TODO`, or unspecified implementation placeholders remain.
- Type consistency: readiness JSON consistently uses `status`, `checks`, `name`, `status`, and `hint`; retained-state and token recovery semantics match existing startup behavior.


## October 6 OBS cancellation contract

OBS operations now close their WebSocket when the request context is canceled during Hello, Identify, or response reads. Handshake and request cancellation callbacks have separate lifetimes, so the handshake cleanup does not close a successfully returned connection. Existing deadlines remain in place. Errors support `errors.Is(..., context.Canceled)`; an Identify failure still preserves the established reachable-but-not-authenticated readiness distinction and Hello metadata.

A local WebSocket regression failed before the change at all three blocked read stages. It now verifies prompt return after explicit cancellation without waiting for the default ten-second deadline. Existing request/auth/status fixtures and HTTP readiness tests cover successful handoff and error classification. OBS/HTTP race tests, `make verify` (113 frontend tests), all 19 browser workflows, and runtime smoke at port 8081 passed. Live OBS testing remains pending its external connection.


## October 6 concurrent session persistence

A failing isolated Postgres fixture reproduced eight simultaneous starts leaving eight active sessions. Start and end now acquire the same transaction advisory lock derived from the session table identity, so separate stores/processes serialize lifecycle writes. Starting a new session retains the existing documented rollover behavior. Timestamps are captured after the lock, avoiding transaction-start times from earlier waits and preserving recent-session ordering. End commits before reading its receipt.

Integration contracts verify eight successful starts with one active session, retained history, recent ordering, nonnegative durations, eight end calls with exactly one accepted end, and deadline cancellation while another transaction holds the lock. Fixtures create and remove their own schema, including an insert-delay trigger used only to reproduce overlap. `make verify` passed with 113 frontend tests, all 21 browser workflows passed, Session/Bot/HTTP race tests with disposable Postgres passed, and rebuilt API runtime smoke passed at port 8081.


## October 6 coherent session report evidence

Session reports now use a read-only repeatable-read transaction for the session summary, event list, and readiness snapshots, with one five-second deadline. Private query readers support both ordinary reads and the report transaction. The event list remains bounded independently of the total count. A canceled/failed read rolls back, and session lookups preserve query errors instead of turning them into missing-row responses.

A controlled isolated Postgres fixture gates event projection after the summary read, then commits a new event, end snapshot, and ended session. Before the fix the report returned a count of one with two events and the later end snapshot. The corrected report contains the original state throughout; its next bounded read contains the new committed evidence. The same gate verifies deadline cancellation. Validation: `make verify`, all 21 browser workflows, Sessions/HTTP race tests with Postgres fixtures, and rebuilt runtime smoke at port 8081 passed.


## October 6 local process recovery drill

The native API was stopped and restarted against the disposable Postgres and retained-state files. A real stopped-process probe failure preceded a successful recovery probe. BRB/goals/ticker SSE replay and the open-session report matched exactly across the restart; a new event retained the active session tag, and ending the rehearsal produced both readiness phases with zero dropped deliveries. Fixtures were cleared and no session remained active. [Recorded evidence](../../qa/2026-10-06-local-recovery-drill.md) distinguishes these completed local gates from pending live OBS/Twitch and deployed outage recovery.
