# Plan 6: Live Ops Cockpit Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Turn `/dashboard/live` into a single operator cockpit that shows backend readiness, incidents, checklist state, and recent session actions before or during a stream.

**Architecture:** Reuse the existing backend `/readyz` endpoint and browser-local dashboard/session storage; do not add backend state or new mutating APIs. Add a typed readiness client, small focused React panels, and route/docs tests. Keep the cockpit read-only except for existing session-log clearing behavior.

**Tech Stack:** Vite React TypeScript, existing dashboard HUD components/styles, `fetch` API helpers, browser local/session storage, Go `/readyz` JSON contract, Vitest SSR route tests.

---

## Sprint Acceptance Criteria

- `/dashboard/live` renders a “Live ops cockpit” header and still includes the existing preflight and EventSub diagnostics.
- The dashboard fetches `/readyz` and renders overall status plus each check name/status/hint without exposing secrets.
- Degraded readiness checks are summarized as operator incidents with short recovery hints.
- The live page shows a read-only setup checklist summary using existing `dashboardConfig.setupChecklist` state.
- The live page shows recent operator actions using the existing session log store.
- Route tests cover the static cockpit shell; a component-level test covers degraded readiness incident rendering.
- README and live/precovery runbooks point operators to `/dashboard/live` as the cockpit.
- `make verify` passes after each implementation task; final compose/deploy smoke checks pass.

## Do Not Do This Sprint

- Do not add a database or persistent incident history.
- Do not add backend mutating controls for EventSub reconnect/reset.
- Do not move `/readyz` semantics into the frontend; frontend only displays backend checks.
- Do not expose raw token contents, OBS passwords, or full URLs beyond what existing APIs already return.
- Do not make the live page depend on successful Twitch/OBS calls to render.

---

## File Structure

Create:

- `web/src/routes/dashboard/components/ReadinessPanel.tsx` — renders `/readyz` overall status, checks, and degraded incident summary.
- `web/src/routes/dashboard/components/LiveChecklistSummary.tsx` — read-only setup checklist completion panel.

Modify:

- `web/src/lib/api.ts` — add `ReadinessStatus`, `ReadinessCheck`, `ReadinessReport`, and `fetchReadiness`.
- `web/src/routes/dashboard/Live.tsx` — fetch readiness + session log and compose the cockpit layout.
- `web/src/routes/dashboard/components/SessionLogPanel.tsx` — support read-only rendering by making `onClear` optional.
- `web/src/App.test.tsx` — route assertions and readiness rendering coverage.
- `README.md` — describe `/dashboard/live` as live ops cockpit.
- `docs/runbooks/live-stream-preflight.md` — point pre-stream workflow at cockpit.
- `docs/runbooks/production-recovery.md` — mention cockpit as first human-readable readiness surface.
- `docs/superpowers/plans/2026-06-13-plan-6-live-ops-cockpit.md` — mark completion when done.

---

### Task 1: Add Readiness API Client and Panel

**Files:**
- Modify: `web/src/lib/api.ts`
- Create: `web/src/routes/dashboard/components/ReadinessPanel.tsx`

- [ ] **Step 1: Add readiness types and fetch helper**

In `web/src/lib/api.ts`, add these exported types after `OBSStatus`:

```ts
export type ReadinessStatus = 'ok' | 'degraded';

export type ReadinessCheck = {
  name: string;
  status: ReadinessStatus;
  hint?: string;
};

export type ReadinessReport = {
  status: ReadinessStatus;
  checks: ReadinessCheck[];
};
```

Add this helper after `fetchOBSStatus`:

```ts
export async function fetchReadiness(explicitBaseUrl?: string) {
  const response = await fetch(apiUrl('/readyz', explicitBaseUrl), { headers: { Accept: 'application/json' } });
  if (!response.ok) {
    throw new Error(`Readiness failed: ${response.status}`);
  }
  return (await response.json()) as ReadinessReport;
}
```

- [ ] **Step 2: Create readiness panel component**

Create `web/src/routes/dashboard/components/ReadinessPanel.tsx`:

```tsx
import type { ReadinessReport } from '../../../lib/api';

type ReadinessPanelProps = {
  report: ReadinessReport | null;
  loading: boolean;
  error: string;
};

function formatCheckName(name: string) {
  return name.replaceAll('_', ' ');
}

export function ReadinessPanel({ report, loading, error }: ReadinessPanelProps) {
  const degraded = report?.checks.filter((check) => check.status !== 'ok') ?? [];

  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="hud-label">Readiness</p>
          <h2 className="hud-section-title">Backend readiness</h2>
          <p className="hud-copy mt-1 text-xs text-zinc-300">Mirrors <code>/readyz</code>; degraded means operator attention, not process death.</p>
        </div>
        <span className={`hud-chip hud-chip--square ${report?.status === 'ok' ? 'hud-accent-emerald' : 'hud-accent-amber'}`}>
          {loading ? 'checking' : report?.status ?? 'unknown'}
        </span>
      </div>

      {error ? <p className="mt-3 text-sm text-rose-300">{error}</p> : null}

      {degraded.length > 0 ? (
        <div className="mt-3 grid gap-2">
          <p className="hud-label">Operator incidents</p>
          {degraded.map((check) => (
            <article key={check.name} className="hud-panel hud-corners hud-panel--flat hud-compact-card hud-accent-amber">
              <div className="hud-terminal-line">
                <span className="hud-terminal-key">incident</span>
                <span className="hud-terminal-value font-bold">{formatCheckName(check.name)}</span>
              </div>
              <p className="mt-2 text-xs text-zinc-300">{check.hint || 'Check production recovery runbook.'}</p>
            </article>
          ))}
        </div>
      ) : null}

      <div className="mt-3 grid gap-2 md:grid-cols-2">
        {(report?.checks ?? []).map((check) => (
          <div key={check.name} className={`hud-terminal-line ${check.status === 'ok' ? 'text-emerald-200' : 'text-amber-200'}`}>
            <span className="hud-terminal-key">{formatCheckName(check.name)}</span>
            <span className="hud-terminal-value">{check.status}</span>
          </div>
        ))}
        {!loading && !report ? <p className="text-sm text-zinc-400">Readiness report unavailable.</p> : null}
      </div>
    </section>
  );
}
```

- [ ] **Step 3: Run tests and typecheck after component creation**

Run:

```bash
pnpm --dir web run test -- --run src/App.test.tsx
pnpm --dir web run build
```

Expected: existing route tests pass; component and API types compile.

- [ ] **Step 4: Commit**

Commit:

```bash
git add web/src/lib/api.ts web/src/routes/dashboard/components/ReadinessPanel.tsx
git commit -m "feat: add readiness cockpit panel"
```

---

### Task 2: Wire Live Ops Cockpit Layout

**Files:**
- Modify: `web/src/routes/dashboard/Live.tsx`
- Create: `web/src/routes/dashboard/components/LiveChecklistSummary.tsx`
- Modify: `web/src/routes/dashboard/components/SessionLogPanel.tsx`
- Modify: `web/src/App.test.tsx`

- [ ] **Step 1: Create checklist summary component**

Create `web/src/routes/dashboard/components/LiveChecklistSummary.tsx`:

```tsx
import type { SetupChecklistItem } from '../../../lib/dashboardConfig';

type LiveChecklistSummaryProps = {
  items: SetupChecklistItem[];
};

export function LiveChecklistSummary({ items }: LiveChecklistSummaryProps) {
  const completed = items.filter((item) => item.completed).length;
  const total = items.length;

  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="hud-label">Checklist</p>
          <h2 className="hud-section-title">Setup checklist summary</h2>
          <p className="hud-copy mt-1 text-xs text-zinc-300">Read-only mirror of the setup checklist.</p>
        </div>
        <span className={`hud-chip hud-chip--square ${completed === total ? 'hud-accent-emerald' : 'hud-accent-amber'}`}>
          {completed}/{total} done
        </span>
      </div>
      <div className="mt-3 grid gap-2">
        {items.map((item) => (
          <div key={item.id} className="hud-terminal-line">
            <span className="hud-terminal-key">{item.completed ? 'done' : 'open'}</span>
            <span className="hud-terminal-value">{item.title}</span>
          </div>
        ))}
      </div>
    </section>
  );
}
```

- [ ] **Step 2: Make session log panel support read-only rendering**

Modify `web/src/routes/dashboard/components/SessionLogPanel.tsx` so `onClear` is optional:

```tsx
type SessionLogPanelProps = {
  entries: SessionLogEntry[];
  onClear?: () => void;
};
```

Render the clear button only when `onClear` exists:

```tsx
{onClear ? (
  <button className="hud-chip hud-chip--square" type="button" onClick={onClear}>
    clear log
  </button>
) : null}
```

Keep `/dashboard/control` unchanged; it already passes `onClear`.

- [ ] **Step 3: Wire readiness + session log into live page**

Modify `web/src/routes/dashboard/Live.tsx`:

- Import `fetchReadiness` and `ReadinessReport` from `../../lib/api`.
- Import `loadSessionLog` from `../../lib/sessionLog`.
- Import `ReadinessPanel`, `LiveChecklistSummary`, and `SessionLogPanel`.
- Extend state with `readiness: ReadinessReport | null` and `readinessError: string`.
- Load Twitch, OBS, and readiness with `Promise.allSettled`.
- Render headline “Live ops cockpit”.
- Render `ReadinessPanel`, `LivePreflight`, `EventSubDiagnostics`, `LiveChecklistSummary`, and `SessionLogPanel`.
- Use `<SessionLogPanel entries={sessionLog} />` on `/dashboard/live` so no clear button appears in the read-only cockpit.

The key state shape should look like:

```ts
type LiveStatus = {
  twitch: TwitchStatus | null;
  obs: OBSStatus | null;
  readiness: ReadinessReport | null;
  loading: boolean;
  error: string;
  readinessError: string;
};
```

The load call should look like:

```ts
const [twitchResult, obsResult, readinessResult] = await Promise.allSettled([
  fetchTwitchStatus(apiBaseUrl),
  fetchOBSStatus(apiBaseUrl),
  fetchReadiness(apiBaseUrl),
]);
```

- [ ] **Step 4: Strengthen route and component assertions**

Update `/dashboard/live` route test in `web/src/App.test.tsx` to assert:

```ts
expect(html).toContain('Live ops cockpit');
expect(html).toContain('Backend readiness');
expect(html).toContain('Readiness report unavailable');
expect(html).toContain('Setup checklist summary');
expect(html).toContain('Recent operator actions');
```

Do not assert fetched readiness data in the route SSR test because `useEffect` does not run during `renderToString`.

Add a direct component test for degraded incident rendering in `web/src/App.test.tsx`:

```tsx
import { ReadinessPanel } from './routes/dashboard/components/ReadinessPanel';
```

```tsx
it('renders degraded readiness incidents', () => {
  const html = renderToString(
    <ReadinessPanel
      loading={false}
      error=""
      report={{ status: 'degraded', checks: [{ name: 'obs_auth', status: 'degraded', hint: 'Check OBS password.' }] }}
    />,
  );

  expect(html).toContain('Operator incidents');
  expect(html).toContain('obs auth');
  expect(html).toContain('Check OBS password.');
});
```

- [ ] **Step 5: Verify and commit**

Run:

```bash
pnpm --dir web run test -- --run src/App.test.tsx
pnpm --dir web run build
make verify
```

Expected: all pass.

Commit:

```bash
git add web/src/routes/dashboard/Live.tsx web/src/routes/dashboard/components/LiveChecklistSummary.tsx web/src/routes/dashboard/components/SessionLogPanel.tsx web/src/App.test.tsx
git commit -m "feat: add live ops cockpit"
```

---

### Task 3: Add Cockpit Documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/runbooks/live-stream-preflight.md`
- Modify: `docs/runbooks/production-recovery.md`

- [ ] **Step 1: Update README dashboard route copy**

In `README.md`, update the `/dashboard/live` or live-operation section to say:

```markdown
Use `/dashboard/live` as the live ops cockpit: it combines preflight checks, `/readyz` readiness, EventSub diagnostics, setup checklist status, and recent operator actions.
```

- [ ] **Step 2: Update live preflight runbook**

In `docs/runbooks/live-stream-preflight.md`, add near the top:

```markdown
Start at `/dashboard/live`. Treat it as the live ops cockpit: fix any readiness incidents before relying on OBS browser sources during a stream.
```

- [ ] **Step 3: Update production recovery runbook**

In `docs/runbooks/production-recovery.md`, add to fast checks:

```bash
curl http://localhost:5173/dashboard/live
```

And add one sentence:

```markdown
For a human-readable view of the same readiness data, open `/dashboard/live`.
```

- [ ] **Step 4: Verify and commit**

Run:

```bash
make verify
```

Commit:

```bash
git add README.md docs/runbooks/live-stream-preflight.md docs/runbooks/production-recovery.md
git commit -m "docs: document live ops cockpit"
```

---

### Task 4: Final Integration, Deploy, and Plan Update

**Files:**
- Modify: `docs/superpowers/plans/2026-06-13-plan-6-live-ops-cockpit.md`

- [ ] **Step 1: Run final checks**

Run:

```bash
docker compose config --quiet
docker compose -f docker-compose.yml -f docker-compose.override.example.yml config --quiet
make verify
docker compose up -d --build
make smoke
curl --max-time 15 -fsS http://localhost:5173/dashboard/live >/dev/null
```

Expected: all pass.

- [ ] **Step 2: Update completion status**

Add near the top after the header:

```markdown
## Completion status

Status: complete. Implemented, reviewed, verified, committed, rebuilt/deployed locally, smoke-checked, and route spot-checked.

Verification passed:

- `make verify`
- `docker compose config --quiet`
- `docker compose -f docker-compose.yml -f docker-compose.override.example.yml config --quiet`
- `docker compose up -d --build`
- `make smoke`
- Route spot-check: `/dashboard/live`

Live QA result: pending next real stream.

---
```

Check every completed task checkbox.

- [ ] **Step 3: Commit completion**

Run:

```bash
git status --short
git diff --stat
git log --oneline -10
git add docs/superpowers/plans/2026-06-13-plan-6-live-ops-cockpit.md
git commit -m "docs: mark plan 6 complete"
```

Do not push unless explicitly requested by the user.

---

## Self-Review

- Spec coverage: tasks cover readiness display, incident hints, checklist summary, session actions, tests, docs, and final verification.
- Placeholder scan: no `TBD`, `TODO`, or unspecified implementation placeholders remain.
- Type consistency: frontend readiness types match backend `/readyz` JSON (`status`, `checks`, `name`, `status`, `hint`).
