# Plan 2: Operator Workflow Polish Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Make the deployed stream tool faster and safer during a real stream by adding preset-driven OBS actions, source refresh controls, a local session action log, and a lightweight visual QA runbook.

**Architecture:** Keep the current Go API + OBS WebSocket + React dashboard structure. Add only bounded OBS request helpers and browser-local operator workflow state; do not add accounts, a database, a workflow scripting engine, or a plugin system. Treat this as a polish sprint after live QA, not a new product rewrite.

**Tech Stack:** Go 1.26, `net/http`, Gorilla WebSocket OBS integration, Vite React TypeScript, localStorage/sessionStorage dashboard config, Docker Compose, Vitest.

---

## Completion status

Status: complete. Implemented, reviewed, verified, committed, rebuilt/deployed locally, smoke-checked, and route spot-checked.

Verification passed:

- `make verify`
- `docker compose config --quiet`
- `docker compose -f docker-compose.yml -f docker-compose.override.example.yml config --quiet`
- `docker compose up -d --build`
- `make smoke`
- Route spot-checks: `/dashboard/control`, `/dashboard/live`, `/overlay/alerts`

Live QA result: OK so far.

---

## Current Baseline

- Branch: `main`, remote `origin=https://git.subcult.tv/PatrickFanella/over-laid.git`.
- Live QA status: OK so far.
- Recent completed work: operator-secret protection, retained overlay state, multi-route dashboard, OBS inventory controls, live preflight route, rehearsal runner, confirmable quick actions, overlay noise controls, Neubrutal Dracula terminal styling, deployment guardrails, and doc cleanup.
- Verification convention: run `make verify` before each commit. For deploy-facing changes also run `docker compose config --quiet` and `make smoke` after rebuild.
- Safety convention: never stage `.env`, token files, browser local data, generated caches, or secrets.

## Sprint Acceptance Criteria

- Stream presets can run as one-click OBS workflow actions instead of being only documentation/config metadata.
- OBS browser sources can be refreshed/reloaded from the dashboard through protected API endpoints.
- The dashboard keeps a local session action log showing operator actions, results, and timestamps.
- A visual QA checklist/runbook exists for overlays and dashboard routes after rebuilds or styling changes.
- All new mutating backend endpoints are protected by `OPERATOR_SECRET` through existing `requireOperator` middleware.
- Existing overlays remain minimal, terminal-style, and readable.

## Do Not Do This Sprint

- Do not add user accounts or RBAC.
- Do not add server-side operator config storage.
- Do not add arbitrary scripting for quick actions.
- Do not add a browser automation dependency unless a future sprint explicitly chooses screenshot automation.
- Do not expose mutating OBS controls publicly without the existing operator-secret protection.

---

## File Structure

Create:

- `web/src/lib/sessionLog.ts` — browser-local session action log helpers.
- `web/src/routes/dashboard/components/SessionLogPanel.tsx` — compact dashboard view for recent operator actions.
- `docs/runbooks/visual-qa.md` — manual route-by-route visual QA checklist.

Modify:

- `backend/internal/obsws/client.go` — add OBS browser-source refresh/reload helper.
- `backend/internal/obsws/client_test.go` — request-shape test for source refresh/reload.
- `backend/internal/http/obs_handlers.go` — add protected HTTP handler for source refresh/reload.
- `backend/internal/http/handlers_test.go` — route authorization and success tests for source refresh/reload.
- `backend/internal/http/routes.go` — register protected source refresh/reload route.
- `web/src/lib/api.ts` — add typed API helper for OBS source refresh/reload.
- `web/src/lib/dashboardConfig.ts` — add preset workflow action type and default quick actions.
- `web/src/routes/dashboard/components/QuickActionRunner.tsx` — execute preset workflow and source refresh actions.
- `web/src/routes/dashboard/components/QuickActionsEditor.tsx` — expose source refresh action fields and preset action selection.
- `web/src/routes/dashboard/components/OBSControls.tsx` — add per-source refresh buttons for scene items.
- `web/src/routes/dashboard/Control.tsx` — wire session log into `runAction` and render `SessionLogPanel`.
- `web/src/App.test.tsx` — assertions for session log and refresh controls.
- `README.md` — link the visual QA runbook.

---

### Task 1: Add Local Session Action Log

**Files:**
- Create: `web/src/lib/sessionLog.ts`
- Create: `web/src/routes/dashboard/components/SessionLogPanel.tsx`
- Modify: `web/src/routes/dashboard/Control.tsx`
- Modify: `web/src/App.test.tsx`

- [x] **Step 1: Add route test for session log panel**

In `web/src/App.test.tsx`, update the `/dashboard/control` test to assert:

```ts
expect(html).toContain('Session log');
expect(html).toContain('Recent operator actions');
```

Run:

```bash
pnpm --dir web run test -- --run src/App.test.tsx
```

Expected: FAIL because `Session log` is not rendered yet.

- [x] **Step 2: Create session log helpers**

Create `web/src/lib/sessionLog.ts`:

```ts
export type SessionLogEntry = {
  id: string;
  label: string;
  status: 'running' | 'success' | 'error';
  message: string;
  createdAt: string;
};

const SESSION_LOG_KEY = 'overlaid.sessionLog.v1';
const maxEntries = 24;

export function loadSessionLog(): SessionLogEntry[] {
  if (typeof window === 'undefined') return [];
  const raw = window.sessionStorage.getItem(SESSION_LOG_KEY);
  if (!raw) return [];
  try {
    const parsed = JSON.parse(raw) as SessionLogEntry[];
    return Array.isArray(parsed) ? parsed.slice(0, maxEntries) : [];
  } catch {
    return [];
  }
}

export function saveSessionLog(entries: SessionLogEntry[]) {
  if (typeof window === 'undefined') return;
  window.sessionStorage.setItem(SESSION_LOG_KEY, JSON.stringify(entries.slice(0, maxEntries)));
}

export function createSessionLogEntry(label: string, status: SessionLogEntry['status'], message: string): SessionLogEntry {
  return {
    id: createLogID(),
    label,
    status,
    message,
    createdAt: new Date().toISOString(),
  };
}

export function prependSessionLogEntry(entries: SessionLogEntry[], entry: SessionLogEntry) {
  return [entry, ...entries].slice(0, maxEntries);
}

export function clearSessionLog() {
  if (typeof window === 'undefined') return;
  window.sessionStorage.removeItem(SESSION_LOG_KEY);
}

function createLogID() {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  return `log-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}
```

- [x] **Step 3: Create session log panel**

Create `web/src/routes/dashboard/components/SessionLogPanel.tsx`:

```tsx
import type { SessionLogEntry } from '../../../lib/sessionLog';

export function SessionLogPanel({ entries, onClear }: { entries: SessionLogEntry[]; onClear: () => void }) {
  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <div className="flex items-center justify-between gap-3">
        <div>
          <p className="hud-label">Session log</p>
          <h2 className="hud-section-title">Recent operator actions</h2>
        </div>
        <button className="hud-chip hud-chip--square" type="button" onClick={onClear}>clear log</button>
      </div>
      <div className="mt-3 grid gap-2" role="log" aria-live="polite">
        {entries.length === 0 ? <p className="text-sm text-zinc-400">No actions recorded this session.</p> : null}
        {entries.map((entry) => (
          <article key={entry.id} className={`hud-panel hud-corners hud-panel--flat hud-compact-card ${entry.status === 'success' ? 'hud-accent-emerald' : entry.status === 'error' ? 'hud-accent-rose' : 'hud-accent-cyan'}`}>
            <div className="hud-terminal-line"><span className="hud-terminal-key">{entry.status}</span><span className="hud-terminal-value font-bold">{entry.label}</span></div>
            <div className="hud-terminal-line"><span className="hud-terminal-key">time</span><span className="hud-terminal-value">{entry.createdAt}</span></div>
            <p className="mt-2 text-xs text-zinc-300">{entry.message}</p>
          </article>
        ))}
      </div>
    </section>
  );
}
```

- [x] **Step 4: Wire session log into Control route action runner**

In `web/src/routes/dashboard/Control.tsx`, import:

```ts
import { clearSessionLog, createSessionLogEntry, loadSessionLog, prependSessionLogEntry, saveSessionLog, type SessionLogEntry } from '../../lib/sessionLog';
import { SessionLogPanel } from './components/SessionLogPanel';
```

Initialize state:

```ts
const [sessionLog, setSessionLog] = useState<SessionLogEntry[]>(() => loadSessionLog());
```

Add helper:

```ts
function recordLog(label: string, status: SessionLogEntry['status'], message: string) {
  const next = prependSessionLogEntry(sessionLog, createSessionLogEntry(label, status, message));
  setSessionLog(next);
  saveSessionLog(next);
}
```

Inside the existing `runAction(label, action)` function, record before and after the action:

```ts
recordLog(label, 'running', 'Action started.');
try {
  await action();
  recordLog(label, 'success', 'Action completed.');
} catch (error) {
  recordLog(label, 'error', error instanceof Error ? error.message : 'Action failed.');
}
```

Render near the top of the control route, after `RehearsalRunner`:

```tsx
<SessionLogPanel entries={sessionLog} onClear={() => { clearSessionLog(); setSessionLog([]); }} />
```

- [x] **Step 5: Verify and commit Task 1**

Run:

```bash
pnpm --dir web run test
pnpm --dir web run build
make verify
```

Expected: all commands pass.

Commit:

```bash
git add web/src/lib/sessionLog.ts web/src/routes/dashboard/components/SessionLogPanel.tsx web/src/routes/dashboard/Control.tsx web/src/App.test.tsx
git commit -m "feat: add operator session action log"
```

---

### Task 2: Add Protected OBS Source Refresh Control

**Files:**
- Modify: `backend/internal/obsws/client.go`
- Modify: `backend/internal/obsws/client_test.go`
- Modify: `backend/internal/http/obs_handlers.go`
- Modify: `backend/internal/http/handlers_test.go`
- Modify: `backend/internal/http/routes.go`
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/routes/dashboard/components/OBSControls.tsx`

- [x] **Step 1: Add failing OBS client test for source refresh request shape**

In `backend/internal/obsws/client_test.go`, add:

```go
func TestPressInputPropertiesButtonSendsExpectedRequest(t *testing.T) {
	client := newTestClient(t, func(requestType string, data map[string]any) map[string]any {
		if requestType != "PressInputPropertiesButton" {
			t.Fatalf("unexpected requestType: %s", requestType)
		}
		if data["inputName"] != "Alerts Browser" {
			t.Fatalf("unexpected inputName: %#v", data["inputName"])
		}
		if data["propertyName"] != "refreshnocache" {
			t.Fatalf("unexpected propertyName: %#v", data["propertyName"])
		}
		return map[string]any{}
	})

	if err := client.RefreshBrowserSource(context.Background(), " Alerts Browser "); err != nil {
		t.Fatalf("RefreshBrowserSource failed: %v", err)
	}
}

func TestRefreshBrowserSourceRequiresInputName(t *testing.T) {
	client := Client{}
	if err := client.RefreshBrowserSource(context.Background(), "   "); err == nil {
		t.Fatal("expected inputName validation error")
	}
}
```

Run:

```bash
cd backend && go test ./internal/obsws -run RefreshBrowserSource -count=1
```

Expected: FAIL because `RefreshBrowserSource` does not exist.

- [x] **Step 2: Implement OBS client refresh helper**

In `backend/internal/obsws/client.go`, add:

```go
func (c Client) RefreshBrowserSource(ctx context.Context, inputName string) error {
	inputName = strings.TrimSpace(inputName)
	if inputName == "" {
		return errors.New("inputName is required")
	}
	_, err := c.call(ctx, "PressInputPropertiesButton", map[string]any{"inputName": inputName, "propertyName": "refreshnocache"})
	return err
}
```

Run:

```bash
gofmt -w backend/internal/obsws/client.go backend/internal/obsws/client_test.go
cd backend && go test ./internal/obsws -run RefreshBrowserSource -count=1
```

Expected: PASS.

- [x] **Step 3: Add protected HTTP route test**

In `backend/internal/http/handlers_test.go`, add tests using the existing OBS test dialer pattern:

```go
func TestOBSRefreshBrowserSourceRequiresOperator(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", OperatorSecret: "operator-secret"}, events.NewBus(4))
	req := httptest.NewRequest(http.MethodPost, "/api/obs/source/refresh", strings.NewReader(`{"inputName":"Alerts Browser"}`))
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOBSRefreshBrowserSource(t *testing.T) {
	server := newTestServerWithOBS(t, func(requestType string, data map[string]any) map[string]any {
		if requestType != "PressInputPropertiesButton" {
			t.Fatalf("unexpected requestType: %s", requestType)
		}
		if data["inputName"] != "Alerts Browser" {
			t.Fatalf("unexpected inputName: %#v", data["inputName"])
		}
		if data["propertyName"] != "refreshnocache" {
			t.Fatalf("unexpected propertyName: %#v", data["propertyName"])
		}
		return map[string]any{}
	})
	server.config.OperatorSecret = "operator-secret"

	req := httptest.NewRequest(http.MethodPost, "/api/obs/source/refresh", strings.NewReader(`{"inputName":"Alerts Browser"}`))
	req.Header.Set("Authorization", "Bearer operator-secret")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
}
```

Run:

```bash
cd backend && go test ./internal/http -run OBSRefreshBrowserSource -count=1
```

Expected: FAIL because route and handler do not exist.

- [x] **Step 4: Implement HTTP handler and route**

In `backend/internal/http/obs_handlers.go`, add request type:

```go
type refreshOBSSourceRequest struct {
	InputName string `json:"inputName"`
}
```

Add handler:

```go
func (s *Server) obsRefreshSource(w http.ResponseWriter, r *http.Request) {
	var request refreshOBSSourceRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := s.obs.RefreshBrowserSource(r.Context(), request.InputName); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
```

In `backend/internal/http/routes.go`, add protected route next to other OBS mutating routes:

```go
s.mux.HandleFunc("POST /api/obs/source/refresh", s.requireOperator(s.obsRefreshSource))
```

Run:

```bash
gofmt -w backend/internal/http/obs_handlers.go backend/internal/http/routes.go backend/internal/http/handlers_test.go
cd backend && go test ./internal/http -run OBSRefreshBrowserSource -count=1
```

Expected: PASS.

- [x] **Step 5: Add frontend API helper**

In `web/src/lib/api.ts`, add:

```ts
export async function refreshOBSSource(inputName: string, explicitBaseUrl?: string, operatorSecret?: string) {
  await postJSON('/api/obs/source/refresh', { inputName }, explicitBaseUrl, operatorSecret);
}
```

- [x] **Step 6: Add per-source refresh buttons in OBS controls**

In `web/src/routes/dashboard/components/OBSControls.tsx`, import `refreshOBSSource` and add:

```ts
function refreshSource(inputName: string) {
  runAction(`Refresh source: ${inputName}`, () => refreshOBSSource(inputName, apiBaseUrl, operatorSecret));
}
```

In the `state.sceneItems.map` row, add a second button next to enable/disable:

```tsx
<button className="hud-chip" type="button" disabled={isRunning} onClick={() => refreshSource(item.sourceName)}>
  Refresh
</button>
```

- [x] **Step 7: Verify and commit Task 2**

Run:

```bash
make verify
```

Expected: all tests/build pass.

Commit:

```bash
git add backend/internal/obsws/client.go backend/internal/obsws/client_test.go backend/internal/http/obs_handlers.go backend/internal/http/handlers_test.go backend/internal/http/routes.go web/src/lib/api.ts web/src/routes/dashboard/components/OBSControls.tsx
git commit -m "feat: add obs source refresh controls"
```

---

### Task 3: Add Preset Workflow Quick Actions

**Files:**
- Modify: `web/src/lib/dashboardConfig.ts`
- Modify: `web/src/routes/dashboard/components/QuickActionRunner.tsx`
- Modify: `web/src/routes/dashboard/components/QuickActionsEditor.tsx`
- Modify: `web/src/App.test.tsx`

- [x] **Step 1: Add failing test for preset workflow action UI**

In `web/src/App.test.tsx`, update the `/dashboard/control` test to assert:

```ts
expect(html).toContain('Run preset');
expect(html).toContain('Refresh Alerts');
```

Run:

```bash
pnpm --dir web run test -- --run src/App.test.tsx
```

Expected: FAIL until the editor exposes preset workflow fields.

- [x] **Step 2: Extend quick action type and defaults**

In `web/src/lib/dashboardConfig.ts`, extend `QuickAction` with:

```ts
| { id: string; label: string; kind: 'runPreset'; presetId: string; sceneName: string; confirm?: boolean }
| { id: string; label: string; kind: 'refreshSource'; inputName: string; confirm?: boolean }
```

Extend `quickActionKinds`:

```ts
export const quickActionKinds = ['setScene', 'sendAlert', 'showBRB', 'hideBRB', 'clearOverlays', 'muteInput', 'runPreset', 'refreshSource'] as const;
```

Add defaults to `defaultDashboardConfig.quickActions`:

```ts
{ id: 'preset-main', label: 'Run Main preset', kind: 'runPreset', presetId: 'main', sceneName: 'Main', confirm: true },
{ id: 'refresh-alerts', label: 'Refresh Alerts', kind: 'refreshSource', inputName: 'Alerts Browser' },
```

In `createQuickAction`, add cases:

```ts
case 'runPreset':
  return { id, label, kind, presetId: 'main', sceneName: 'Main', confirm: true };
case 'refreshSource':
  return { id, label, kind, inputName: 'Alerts Browser' };
```

- [x] **Step 3: Execute new quick action kinds**

In `web/src/routes/dashboard/components/QuickActionRunner.tsx`, import `refreshOBSSource` from API.

Add cases:

```ts
case 'runPreset':
  return runAction(action.label, () => setOBSScene(action.sceneName, apiBaseUrl, operatorSecret));
case 'refreshSource':
  return runAction(action.label, () => refreshOBSSource(action.inputName, apiBaseUrl, operatorSecret));
```

The first version of `runPreset` sets the OBS scene only. Source visibility stays controlled by explicit scene-item buttons and refresh-source actions so the sprint does not introduce an implicit scene-source orchestration engine.

- [x] **Step 4: Expose editor fields for preset and refresh actions**

In `web/src/routes/dashboard/components/QuickActionsEditor.tsx`, add form branches:

```tsx
{action.kind === 'runPreset' ? (
  <>
    <label className="grid gap-2 text-xs text-zinc-300">
      <span className="hud-kicker">Run preset</span>
      <input className="hud-input" value={action.presetId} onChange={(event) => updateAction(index, { presetId: event.target.value })} />
    </label>
    <label className="grid gap-2 text-xs text-zinc-300">
      <span className="hud-kicker">Scene name</span>
      <input className="hud-input" value={action.sceneName} onChange={(event) => updateAction(index, { sceneName: event.target.value })} />
    </label>
  </>
) : null}
{action.kind === 'refreshSource' ? (
  <label className="grid gap-2 text-xs text-zinc-300">
    <span className="hud-kicker">Source input name</span>
    <input className="hud-input" value={action.inputName} onChange={(event) => updateAction(index, { inputName: event.target.value })} />
  </label>
) : null}
```

- [x] **Step 5: Verify and commit Task 3**

Run:

```bash
pnpm --dir web run test
pnpm --dir web run build
make verify
```

Expected: all commands pass.

Commit:

```bash
git add web/src/lib/dashboardConfig.ts web/src/routes/dashboard/components/QuickActionRunner.tsx web/src/routes/dashboard/components/QuickActionsEditor.tsx web/src/App.test.tsx
git commit -m "feat: add preset workflow quick actions"
```

---

### Task 4: Add Visual QA Runbook

**Files:**
- Create: `docs/runbooks/visual-qa.md`
- Modify: `README.md`

- [x] **Step 1: Confirm visual QA runbook does not already exist**

Run:

```bash
test ! -f docs/runbooks/visual-qa.md
```

Expected: PASS before the file exists. This confirms the task creates the new runbook.

- [x] **Step 2: Create visual QA runbook**

Create `docs/runbooks/visual-qa.md`:

````md
# Visual QA Runbook

Use this after styling changes, overlay layout changes, or before a real stream.

## Start stack

```bash
docker compose up -d --build
make smoke
```

## Routes to inspect

Open each route in a browser at the deployed web origin:

- `/overlay/alerts`
- `/overlay/chat`
- `/overlay/goals`
- `/overlay/now-playing`
- `/overlay/ticker`
- `/overlay/brb`
- `/overlay/debug`
- `/dashboard`
- `/dashboard/live`
- `/dashboard/control`
- `/dashboard/setup`
- `/dashboard/config`

## Overlay checks

- Text is readable at OBS canvas scale.
- Panels use hard borders and Dracula/Neubrutal colors.
- No public overlay uses soft glassmorphism or heavy blur.
- Empty states are intentional and not fake live data.
- Alerts, chat, and ticker do not spam the screen beyond configured limits.
- BRB state appears and clears from dashboard controls.

## Dashboard checks

- `/dashboard/live` clearly distinguishes Twitch auth, EventSub, OBS, operator secret, and setup readiness.
- `/dashboard/control` shows quick actions, rehearsal runner, OBS controls, and session log.
- Mutating controls fail safely when the operator secret is missing and the API requires it.
- OBS inventory can be refreshed manually without page-load spam.
- All action buttons remain keyboard-focusable.

## Recovery notes

If a visual issue appears during QA, capture:

- Route path.
- Browser size or OBS source size.
- Screenshot or exact visible text.
- Whether `make verify` and `make smoke` passed.
````

- [x] **Step 3: Link visual QA runbook from README**

In `README.md`, near the existing live preflight runbook link, add:

```md
For visual checks after overlay or dashboard changes, follow `docs/runbooks/visual-qa.md`.
```

- [x] **Step 4: Verify docs and commit Task 4**

Run:

```bash
make verify
```

Expected: all commands pass.

Commit:

```bash
git add README.md docs/runbooks/visual-qa.md
git commit -m "docs: add visual qa runbook"
```

---

### Task 5: Final Integration, Deploy Check, and Plan Update

**Files:**
- Modify: `docs/superpowers/plans/2026-06-13-plan-2-operator-workflow-polish.md`

- [x] **Step 1: Run full verification**

Run:

```bash
make verify
docker compose config --quiet
docker compose -f docker-compose.yml -f docker-compose.override.example.yml config --quiet
```

Expected: all commands pass.

- [x] **Step 2: Rebuild and smoke local stack**

Run:

```bash
docker compose up -d --build
make smoke
```

Expected: `/healthz`, `/overlay/alerts`, and `/dashboard` respond.

- [x] **Step 3: Manual route spot-check**

Open:

```text
http://localhost:5173/dashboard/control
http://localhost:5173/dashboard/live
http://localhost:5173/overlay/alerts
```

Expected:

- Control route renders session log and OBS refresh controls.
- Live route still renders preflight checks.
- Alerts overlay stays minimal/readable.

- [x] **Step 4: Mark Plan 2 complete when deployed**

After implementation, commit, push, deploy, and manual spot-check, add this block under the first `---` near the top of this file:

```md
## Completion status

Status: complete. Implemented, verified, committed, pushed, deployed, and manually spot-checked.

Live QA result: OK so far.

---
```

- [x] **Step 5: Commit plan completion update**

Run:

```bash
git add docs/superpowers/plans/2026-06-13-plan-2-operator-workflow-polish.md
git commit -m "docs: mark plan 2 complete"
git push origin main
```

---

## Final Self-Review Checklist

- Spec coverage: preset workflow actions, source refresh controls, session logging, visual QA, and deploy verification are all represented.
- YAGNI check: no accounts, no scripting language, no server-side dashboard config persistence, no new database.
- Safety check: new OBS source refresh route is protected with `requireOperator`.
- Type consistency: new quick action kinds are represented in `QuickAction`, `quickActionKinds`, `createQuickAction`, `QuickActionRunner`, and `QuickActionsEditor`.
- Regression check: `make verify`, compose config checks, and smoke checks pass before final sync/deploy.

## Recommended Execution Order

1. Task 1 session log.
2. Task 2 OBS source refresh backend/frontend.
3. Task 3 preset workflow quick actions.
4. Task 4 visual QA runbook.
5. Task 5 integration/deploy and plan completion update.

Do not combine implementation tasks into one commit. Each task should be independently verified and committed.
