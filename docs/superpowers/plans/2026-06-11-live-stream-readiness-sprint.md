# Live Stream Readiness Sprint Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Make over-laid reliable and calm for one real stream: preflight checks, EventSub diagnostics, rehearsal controls, safer actions, and a live-run runbook.

**Architecture:** Keep the current Go API + SSE + React dashboard architecture. Add lightweight in-memory diagnostics and browser-local dashboard configuration instead of introducing accounts, a database admin model, or a workflow engine. Use existing protected endpoints and typed quick-action vocabulary.

**Tech Stack:** Go HTTP API, Gorilla WebSocket integrations, Vite React TypeScript dashboard, localStorage/sessionStorage dashboard config, file-backed overlay state, Docker Compose.

---

## Sprint Theme

**Live Stream Readiness: make one real stream boringly reliable.**

The foundation is already in place: Twitch OAuth/EventSub, OBS controls, MPRIS, retained state, multi-route dashboard, compact neubrutal Dracula HUD styling, and setup/source/control/config dashboard screens. This sprint should not add broad third-party adapters or a big persistence rewrite. It should make the existing tool dependable during a live session.

## Sprint-Level Acceptance Criteria

- `/dashboard` or `/dashboard/live` shows a clear ready/not-ready preflight summary.
- Twitch auth and Twitch EventSub connection/subscription state are distinguishable.
- OBS status failures show actionable recovery hints.
- One rehearsal button validates alert, ticker, goal, BRB, and optional OBS actions.
- Destructive quick actions can require confirmation.
- Alert/ticker/chat overlays handle bursty test traffic without unreadable spam.
- A runbook documents start, preflight, rehearsal, and recovery steps.

## Do Not Do This Sprint

- Do not build accounts/RBAC.
- Do not move dashboard config to Postgres.
- Do not build a generic integration/plugin system.
- Do not create a workflow scripting language.
- Do not split the SSE stream by overlay.
- Do not add Gitea/n8n/FreshRSS/ntfy-specific adapters yet; generic webhook is enough.

---

## File Structure

Create:

- `web/src/routes/dashboard/Live.tsx` — preflight route.
- `web/src/routes/dashboard/components/LivePreflight.tsx` — compact ready/not-ready checklist.
- `web/src/routes/dashboard/components/RehearsalRunner.tsx` — step-by-step rehearsal control.
- `web/src/routes/dashboard/components/EventSubDiagnostics.tsx` — Twitch/EventSub diagnostics panel.
- `web/src/routes/dashboard/components/ConfirmableActionButton.tsx` — reusable two-step action button.
- `web/src/lib/rehearsal.ts` — typed rehearsal step runner helpers.
- `docs/runbooks/live-stream-preflight.md` — operator runbook.

Modify:

- `backend/internal/twitch/eventsub.go` — collect in-memory connection/subscription diagnostics.
- `backend/internal/http/server.go` — own/pass EventSub diagnostics to HTTP handlers.
- `backend/internal/http/twitch_auth.go` — include EventSub diagnostics in status response or add status helper.
- `backend/internal/http/twitch_auth_test.go` — status shape tests.
- `web/src/lib/api.ts` — add EventSub diagnostics fields to `TwitchStatus`, add preflight/rehearsal helper calls if useful.
- `web/src/lib/dashboardConfig.ts` — add quick-action confirmation and overlay noise display preferences.
- `web/src/routes/dashboard/DashboardShell.tsx` — add `/dashboard/live` nav.
- `web/src/routes/dashboard/Overview.tsx` — link/summary for live preflight.
- `web/src/routes/dashboard/Control.tsx` and quick-action components — use confirmations and rehearsal runner.
- `web/src/routes/OverlayAlerts.tsx`, `OverlayTicker.tsx`, `OverlayChat.tsx` — apply simple display/noise controls.
- `web/src/App.tsx`, `web/src/App.test.tsx` — new route/tests.
- `README.md` — point to runbook and update dashboard route docs.

---

### Task 1: Clean Up Operator Secret Ownership

**Files:**
- Modify: `web/src/routes/dashboard/Control.tsx`
- Modify: `web/src/routes/dashboard/Config.tsx`
- Modify: `web/src/App.test.tsx`

- [ ] **Step 1: Remove editable operator secret input from stream control**

In `web/src/routes/dashboard/Control.tsx`, replace the password input block with this compact status/link block:

```tsx
<div className="mt-3 flex flex-wrap items-center gap-2">
  <span className={`hud-chip hud-chip--square ${operatorSecret ? 'hud-accent-emerald' : 'hud-accent-rose'}`}>
    {operatorSecret ? 'operator secret loaded' : 'operator secret missing'}
  </span>
  <a className="hud-chip hud-chip--square" href="/dashboard/config">Edit secret</a>
</div>
```

Remove `updateOperatorSecret()` from `Control.tsx` if unused.

- [ ] **Step 2: Make Config route the only edit surface**

In `web/src/routes/dashboard/Config.tsx`, keep the existing password input and add a small warning line:

```tsx
<p className="mt-2 text-xs text-zinc-400">
  This is the only dashboard route that edits the operator secret.
</p>
```

- [ ] **Step 3: Update tests**

In `web/src/App.test.tsx`, update the control route test:

```ts
expect(html).toContain('operator secret missing');
expect(html).toContain('Edit secret');
```

Keep config route expectations for `Secret header`.

- [ ] **Step 4: Verify**

Run:

```bash
pnpm --dir web run test
pnpm --dir web run build
```

Expected: all frontend tests/build pass.

- [ ] **Step 5: Commit**

```bash
git add web/src/routes/dashboard/Control.tsx web/src/routes/dashboard/Config.tsx web/src/App.test.tsx
git commit -m "refactor: centralize dashboard operator secret"
```

---

### Task 2: Add Twitch EventSub Runtime Diagnostics

**Files:**
- Modify: `backend/internal/twitch/eventsub.go`
- Modify: `backend/internal/http/server.go`
- Modify: `backend/internal/http/twitch_auth.go`
- Modify: `backend/internal/http/twitch_auth_test.go`
- Modify: `web/src/lib/api.ts`
- Create: `web/src/routes/dashboard/components/EventSubDiagnostics.tsx`

- [ ] **Step 1: Add diagnostics type to EventSub package**

In `backend/internal/twitch/eventsub.go`, add:

```go
type Diagnostics struct {
	Enabled           bool      `json:"enabled"`
	Connected         bool      `json:"connected"`
	LastConnectedAt   time.Time `json:"lastConnectedAt,omitempty"`
	LastEventAt       time.Time `json:"lastEventAt,omitempty"`
	LastError         string    `json:"lastError,omitempty"`
	SubscriptionsMade int       `json:"subscriptionsMade"`
	SubscriptionsPlan int       `json:"subscriptionsPlan"`
}

type DiagnosticsStore struct {
	mu sync.RWMutex
	d  Diagnostics
}

func NewDiagnosticsStore(enabled bool) *DiagnosticsStore {
	return &DiagnosticsStore{d: Diagnostics{Enabled: enabled}}
}

func (s *DiagnosticsStore) Snapshot() Diagnostics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.d
}
```

Add methods `MarkConnected(created, planned int)`, `MarkEvent()`, `MarkError(err error)`, and `MarkDisconnected(err error)`.

- [ ] **Step 2: Wire diagnostics into EventSub client**

Add `Diagnostics *DiagnosticsStore` to `twitch.Client` and call:

```go
c.Diagnostics.MarkConnected(created, planned)
c.Diagnostics.MarkEvent()
c.Diagnostics.MarkError(err)
c.Diagnostics.MarkDisconnected(err)
```

at the existing connection, notification, subscribe failure, and session-end points.

- [ ] **Step 3: Own diagnostics in HTTP server**

In `backend/internal/http/server.go`, add field:

```go
twitchEventSubDiagnostics *twitch.DiagnosticsStore
```

Initialize it in `NewServer`:

```go
twitchEventSubDiagnostics: twitch.NewDiagnosticsStore(cfg.TwitchEventSub),
```

Pass it into the EventSub client in `StartBackground`.

- [ ] **Step 4: Add diagnostics to Twitch status response**

In `backend/internal/http/twitch_auth.go`, add to `twitchStatusResponse`:

```go
EventSub twitch.Diagnostics `json:"eventSub"`
```

Set it in `twitchStatus` using `s.twitchEventSubDiagnostics.Snapshot()`.

- [ ] **Step 5: Add backend test**

In `backend/internal/http/twitch_auth_test.go`, assert `/api/auth/twitch/status` includes:

```go
if !body.EventSub.Enabled {
	t.Fatal("expected eventSub.enabled true")
}
```

- [ ] **Step 6: Add frontend type and diagnostics component**

In `web/src/lib/api.ts`, extend `TwitchStatus`:

```ts
eventSub?: {
  enabled: boolean;
  connected: boolean;
  lastConnectedAt?: string;
  lastEventAt?: string;
  lastError?: string;
  subscriptionsMade: number;
  subscriptionsPlan: number;
};
```

Create `web/src/routes/dashboard/components/EventSubDiagnostics.tsx`:

```tsx
import type { TwitchStatus } from '../../../lib/api';

export function EventSubDiagnostics({ status }: { status: TwitchStatus | null }) {
  const eventSub = status?.eventSub;
  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <p className="hud-label">Twitch EventSub</p>
      <h2 className="hud-section-title">{eventSub?.connected ? 'Connected' : 'Not connected'}</h2>
      <p className="mt-2 text-xs text-zinc-300">
        {eventSub ? `${eventSub.subscriptionsMade}/${eventSub.subscriptionsPlan} subscriptions active` : 'Diagnostics unavailable'}
      </p>
      {eventSub?.lastError ? <p className="mt-2 text-xs text-rose-200">{eventSub.lastError}</p> : null}
    </section>
  );
}
```

- [ ] **Step 7: Verify and commit**

Run:

```bash
make verify
```

Commit:

```bash
git add backend/internal/twitch/eventsub.go backend/internal/http/server.go backend/internal/http/twitch_auth.go backend/internal/http/twitch_auth_test.go web/src/lib/api.ts web/src/routes/dashboard/components/EventSubDiagnostics.tsx
git commit -m "feat: expose twitch eventsub diagnostics"
```

---

### Task 3: Add Live Preflight Route

**Files:**
- Create: `web/src/routes/dashboard/Live.tsx`
- Create: `web/src/routes/dashboard/components/LivePreflight.tsx`
- Modify: `web/src/routes/dashboard/DashboardShell.tsx`
- Modify: `web/src/routes/dashboard/Overview.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/App.test.tsx`

- [ ] **Step 1: Add nav route**

In `DashboardShell.tsx`, add:

```ts
['/dashboard/live', 'Live']
```

to `dashboardLinks` immediately after Overview.

- [ ] **Step 2: Create LivePreflight component**

Create `web/src/routes/dashboard/components/LivePreflight.tsx`:

```tsx
import type { OBSStatus, TwitchStatus } from '../../../lib/api';
import type { DashboardConfig } from '../../../lib/dashboardConfig';

export function LivePreflight({ twitch, obs, config, operatorSecret }: { twitch: TwitchStatus | null; obs: OBSStatus | null; config: DashboardConfig; operatorSecret: string }) {
  const checks = [
    { label: 'Twitch authenticated', ok: Boolean(twitch?.authenticated), fix: 'Open Twitch login from the header.' },
    { label: 'EventSub connected', ok: Boolean(twitch?.eventSub?.connected), fix: 'Wait for EventSub or re-login if scopes changed.' },
    { label: 'OBS reachable', ok: Boolean(obs?.reachable), fix: 'Check OBS WebSocket, host networking, and password.' },
    { label: 'Operator secret loaded', ok: Boolean(operatorSecret), fix: 'Open Config and enter the operator secret.' },
    { label: 'Setup checklist complete', ok: config.setupChecklist.every((item) => item.completed), fix: 'Open Setup and complete remaining items.' },
  ];

  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <p className="hud-label">Live preflight</p>
      <div className="mt-3 grid gap-2 md:grid-cols-2 xl:grid-cols-5">
        {checks.map((check) => (
          <article key={check.label} className={`hud-panel hud-corners hud-panel--flat hud-compact-card ${check.ok ? 'hud-accent-emerald' : 'hud-accent-rose'}`}>
            <p className="hud-kicker">{check.ok ? 'ready' : 'fix'}</p>
            <p className="mt-1 text-sm font-bold text-white">{check.label}</p>
            {!check.ok ? <p className="mt-1 text-xs text-zinc-300">{check.fix}</p> : null}
          </article>
        ))}
      </div>
    </section>
  );
}
```

- [ ] **Step 3: Create live route**

Create `web/src/routes/dashboard/Live.tsx` to load Twitch/OBS status with `fetchTwitchStatus`, `fetchOBSStatus`, local config, operator secret, then render `LivePreflight` and `EventSubDiagnostics`.

- [ ] **Step 4: Route it**

In `web/src/App.tsx`:

```tsx
if (route === '/dashboard/live') return <DashboardLivePage />;
```

- [ ] **Step 5: Link from overview**

In `Overview.tsx`, add shortcut link:

```tsx
<a className="hud-action-button hud-action-button--compact" href="/dashboard/live">Live preflight</a>
```

- [ ] **Step 6: Test**

In `App.test.tsx`:

```ts
it('renders the dashboard live preflight route', () => {
  const html = renderToString(<App pathname="/dashboard/live" />);
  expect(html).toContain('Live preflight');
});
```

- [ ] **Step 7: Verify and commit**

```bash
pnpm --dir web run test
pnpm --dir web run build
git add web/src/routes/dashboard/Live.tsx web/src/routes/dashboard/components/LivePreflight.tsx web/src/routes/dashboard/DashboardShell.tsx web/src/routes/dashboard/Overview.tsx web/src/App.tsx web/src/App.test.tsx
git commit -m "feat: add live preflight dashboard route"
```

---

### Task 4: Add Rehearsal Runner

**Files:**
- Create: `web/src/lib/rehearsal.ts`
- Create: `web/src/routes/dashboard/components/RehearsalRunner.tsx`
- Modify: `web/src/routes/dashboard/Control.tsx`
- Modify: `web/src/App.test.tsx`

- [ ] **Step 1: Add rehearsal helper**

Create `web/src/lib/rehearsal.ts`:

```ts
import { sendTestAlert, setBRBStatus, type AlertPayload } from './api';
import { sampleAlert, sampleBRBHide, sampleBRBShow, sampleGoals, sampleTicker } from './dashboardSamples';

export type RehearsalStep = {
  id: string;
  label: string;
  run: () => Promise<void>;
};

export function createRehearsalSteps(apiBaseUrl: string, operatorSecret: string): RehearsalStep[] {
  const alertStep = (id: string, label: string, payload: AlertPayload): RehearsalStep => ({
    id,
    label,
    run: () => sendTestAlert(payload, apiBaseUrl, operatorSecret),
  });

  return [
    alertStep('alert', 'Sample alert', sampleAlert),
    alertStep('ticker', 'Sample ticker', sampleTicker),
    alertStep('goals', 'Sample goals', sampleGoals),
    { id: 'brb-show', label: 'Show BRB', run: () => setBRBStatus(sampleBRBShow, apiBaseUrl, operatorSecret) },
    { id: 'brb-hide', label: 'Hide BRB', run: () => setBRBStatus(sampleBRBHide, apiBaseUrl, operatorSecret) },
  ];
}
```

- [ ] **Step 2: Add RehearsalRunner component**

Create `web/src/routes/dashboard/components/RehearsalRunner.tsx`:

```tsx
import { useState } from 'react';
import { createRehearsalSteps } from '../../../lib/rehearsal';

type StepState = 'pending' | 'running' | 'pass' | 'fail';

export function RehearsalRunner({ apiBaseUrl, operatorSecret }: { apiBaseUrl: string; operatorSecret: string }) {
  const steps = createRehearsalSteps(apiBaseUrl, operatorSecret);
  const [states, setStates] = useState<Record<string, StepState>>({});
  const [running, setRunning] = useState(false);

  async function runAll() {
    setRunning(true);
    const next: Record<string, StepState> = {};
    for (const step of steps) {
      next[step.id] = 'running';
      setStates({ ...next });
      try {
        await step.run();
        next[step.id] = 'pass';
      } catch {
        next[step.id] = 'fail';
        setStates({ ...next });
        setRunning(false);
        return;
      }
      setStates({ ...next });
    }
    setRunning(false);
  }

  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <div className="flex items-center justify-between gap-2">
        <div>
          <p className="hud-label">Rehearsal</p>
          <h2 className="hud-section-title">Run overlay smoke sequence</h2>
        </div>
        <button className="hud-action-button hud-action-button--compact" type="button" disabled={running} onClick={() => void runAll()}>
          {running ? 'Running' : 'Run rehearsal'}
        </button>
      </div>
      <div className="mt-3 grid gap-2 md:grid-cols-5">
        {steps.map((step) => <span key={step.id} className="hud-chip hud-chip--square">{step.label}: {states[step.id] || 'pending'}</span>)}
      </div>
    </section>
  );
}
```

- [ ] **Step 3: Mount it on control route**

In `Control.tsx`, render `RehearsalRunner` near the top, after `ControlLayoutEditor`.

- [ ] **Step 4: Test and commit**

Update `/dashboard/control` test to expect `Run overlay smoke sequence`. Run:

```bash
pnpm --dir web run test
pnpm --dir web run build
```

Commit:

```bash
git add web/src/lib/rehearsal.ts web/src/routes/dashboard/components/RehearsalRunner.tsx web/src/routes/dashboard/Control.tsx web/src/App.test.tsx
git commit -m "feat: add stream rehearsal runner"
```

---

### Task 5: Add Safer Quick Actions

**Files:**
- Modify: `web/src/lib/dashboardConfig.ts`
- Create: `web/src/routes/dashboard/components/ConfirmableActionButton.tsx`
- Modify: `web/src/routes/dashboard/components/QuickActionRunner.tsx`
- Modify: `web/src/routes/dashboard/components/QuickActionsEditor.tsx`
- Modify: `web/src/App.test.tsx`

- [ ] **Step 1: Add confirmation flag**

In `dashboardConfig.ts`, add `confirm?: boolean` to every `QuickAction` variant:

```ts
| { id: string; label: string; kind: 'setScene'; sceneName: string; confirm?: boolean }
```

Set defaults:

```ts
{ id: 'brb-scene', label: 'Go BRB', kind: 'setScene', sceneName: 'BRB', confirm: true }
{ id: 'hide-brb', label: 'Hide BRB', kind: 'hideBRB', confirm: false }
```

- [ ] **Step 2: Add confirmable button**

Create `ConfirmableActionButton.tsx`:

```tsx
import { useState } from 'react';

export function ConfirmableActionButton({ label, confirm, disabled, onRun }: { label: string; confirm?: boolean; disabled?: boolean; onRun: () => void }) {
  const [armed, setArmed] = useState(false);
  return (
    <button
      className={`hud-action-button hud-action-button--compact ${armed ? 'hud-accent-rose' : ''}`}
      type="button"
      disabled={disabled}
      onClick={() => {
        if (confirm && !armed) {
          setArmed(true);
          return;
        }
        setArmed(false);
        onRun();
      }}
    >
      {armed ? `Confirm ${label}` : label}
    </button>
  );
}
```

- [ ] **Step 3: Use it in QuickActionRunner**

Replace quick-action buttons with `ConfirmableActionButton`, passing `confirm={action.confirm}`.

- [ ] **Step 4: Expose confirm toggle in editor**

In `QuickActionsEditor.tsx`, add checkbox:

```tsx
<label className="flex items-center gap-2 text-xs text-zinc-300">
  <input type="checkbox" checked={Boolean(action.confirm)} onChange={(event) => updateAction(index, { confirm: event.target.checked } as Partial<QuickAction>)} />
  require confirm
</label>
```

- [ ] **Step 5: Verify and commit**

```bash
pnpm --dir web run test
pnpm --dir web run build
git add web/src/lib/dashboardConfig.ts web/src/routes/dashboard/components/ConfirmableActionButton.tsx web/src/routes/dashboard/components/QuickActionRunner.tsx web/src/routes/dashboard/components/QuickActionsEditor.tsx web/src/App.test.tsx
git commit -m "feat: add confirmable quick actions"
```

---

### Task 6: Add Basic Overlay Noise Controls

**Files:**
- Modify: `web/src/lib/dashboardConfig.ts`
- Modify: `web/src/routes/OverlayAlerts.tsx`
- Modify: `web/src/routes/OverlayChat.tsx`
- Modify: `web/src/routes/OverlayTicker.tsx`
- Modify: `web/src/routes/dashboard/Config.tsx`
- Modify: `web/src/App.test.tsx`

- [ ] **Step 1: Add display config**

In `DashboardConfig`, add:

```ts
display: {
  alertMaxVisible: number;
  alertDedupeSeconds: number;
  chatMaxVisible: number;
  tickerMaxItems: number;
};
```

Default:

```ts
display: { alertMaxVisible: 3, alertDedupeSeconds: 8, chatMaxVisible: 8, tickerMaxItems: 8 }
```

- [ ] **Step 2: Load display config in overlays**

In each overlay component, import `loadDashboardConfig()` and use display limits. For alerts, skip identical title/message within dedupe window:

```ts
const display = loadDashboardConfig().display;
```

Keep fallbacks if localStorage config is missing.

- [ ] **Step 3: Add compact controls on Config route**

In `Config.tsx`, add number inputs for `alertMaxVisible`, `alertDedupeSeconds`, `chatMaxVisible`, `tickerMaxItems`, saving via `saveDashboardConfig`.

- [ ] **Step 4: Verify and commit**

```bash
make verify
git add web/src/lib/dashboardConfig.ts web/src/routes/OverlayAlerts.tsx web/src/routes/OverlayChat.tsx web/src/routes/OverlayTicker.tsx web/src/routes/dashboard/Config.tsx web/src/App.test.tsx
git commit -m "feat: add overlay noise controls"
```

---

### Task 7: Write Live Runbook and Final Smoke

**Files:**
- Create: `docs/runbooks/live-stream-preflight.md`
- Modify: `README.md`

- [ ] **Step 1: Create runbook**

Create `docs/runbooks/live-stream-preflight.md`:

```md
# Live Stream Preflight Runbook

## Start

```bash
make docker-build
make smoke
```

## Dashboard

Open:

```text
http://localhost:5173/dashboard/live
```

Expected ready checks:

- Twitch authenticated
- EventSub connected
- OBS reachable
- Operator secret loaded if configured
- Setup checklist complete

## Rehearsal

Open `/dashboard/control` and run `Run rehearsal`.

Expected sequence:

1. Alert appears.
2. Ticker updates.
3. Goal rail updates.
4. BRB appears.
5. BRB hides.

## Common failures

### Twitch authenticated but EventSub not connected

Re-run Twitch login from the dashboard header. If scopes changed, re-consent.

### OBS unreachable

Check OBS WebSocket settings, host networking override, and `OBS_WEBSOCKET_PASSWORD`.

### Operator controls return 401

Open `/dashboard/config` and enter `OPERATOR_SECRET`.
```

- [ ] **Step 2: Link runbook from README**

Add near dashboard route docs:

```md
For live operation, follow `docs/runbooks/live-stream-preflight.md`.
```

- [ ] **Step 3: Final verification**

Run:

```bash
make verify
docker compose config --quiet
docker compose -f docker-compose.yml -f docker-compose.override.example.yml config --quiet
docker compose up -d --build web
make smoke
```

- [ ] **Step 4: Commit**

```bash
git add README.md docs/runbooks/live-stream-preflight.md
git commit -m "docs: add live stream preflight runbook"
```

---

## Sprint Execution Order

1. Operator secret cleanup.
2. EventSub diagnostics.
3. Live preflight route.
4. Rehearsal runner.
5. Confirmable quick actions.
6. Overlay noise controls.
7. Live runbook and final smoke.

## Final Self-Review

- Check every new mutating action still uses `OPERATOR_SECRET`.
- Check dashboard stays compact and neubrutal-Dracula styled.
- Check no new persistent backend configuration is added unless justified.
- Check `make verify` and compose config checks pass after every backend change.
- Check `/dashboard/live`, `/dashboard/control`, `/overlay/alerts`, and `/overlay/chat` manually after web rebuild.
