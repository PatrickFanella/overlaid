# Recovery Console Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Turn the existing dashboard primitives into one clear operator recovery console for live incidents, build identity, backups, and rule/config recovery.

**Architecture:** Keep this frontend/docs-only. Reuse existing `/readyz`, `/version`, `/api/events/history`, session log, and runbook links. Do not add backend backup/restore endpoints or expose backup contents in the browser.

**Tech Stack:** Vite React TypeScript, existing dashboard HUD components, existing API helpers, Markdown runbooks.

---

## Status

- **Status:** Complete
- **Created:** 2026-06-14
- **Completed:** 2026-06-14
- **Live QA:** Pending next real stream incident/rehearsal. Verify `/dashboard/live` as the single recovery cockpit.

## Completion Notes

- **Implemented:** Live recovery console panel, dashboard ops route guide, and recovery-console docs.
- **Verification:** `make verify`, compose config checks, `docker compose up -d --build`, `make smoke`, `/dashboard/live` and `/dashboard` route spot-checks.
- **Deploy smoke:** Passed. `/readyz` returned HTTP 200 with `status: degraded` only because `twitch_eventsub` was not connected; recovery-console routes loaded successfully.

---

## Acceptance Criteria

- `/dashboard/live` includes a compact recovery console section that tells operators what to inspect first and links to production recovery, packaging/backups, event history, Twitch interaction rules, and live preflight runbooks.
- `/dashboard/live` includes build identity using the existing build-info panel or a safe compact variant; it must not require operator secret.
- `/dashboard` overview clearly routes operators by intent: monitor/recover, edit rules/config, control stream, source setup, and backup/package.
- Recovery UI never automates destructive backup/restore operations and never reads or displays `api-data` contents.
- SSR route tests cover the new static shell copy and no-fetch behavior for any new components.
- Docs describe `/dashboard/live` as the canonical recovery cockpit and keep CLI backup/restore as command/runbook driven.
- `make verify`, compose config checks, rebuild, `make smoke`, and route spot-checks pass before marking complete.

## Do Not

- Do not add a browser-triggered backup or restore endpoint.
- Do not expose tokens, retained state, event history JSONL contents beyond existing sanitized history, or interaction-rule file contents outside the existing protected manager.
- Do not put backup archives in git or change `backups/` ignore behavior.
- Do not make `/healthz` depend on recovery-console state.
- Do not push commits unless explicitly asked.

## File Structure

- Create: `web/src/routes/dashboard/components/RecoveryConsolePanel.tsx`
  - Static/SSR-safe recovery guide panel with priority steps and runbook links.
- Create: `web/src/routes/dashboard/components/OpsRouteGuide.tsx`
  - Static/SSR-safe overview route guide mapping operator intent to dashboard routes/runbooks.
- Modify: `web/src/routes/dashboard/Live.tsx`
  - Render `RecoveryConsolePanel` and `BuildInfoPanel` in the cockpit.
- Modify: `web/src/routes/dashboard/Overview.tsx`
  - Render `OpsRouteGuide` and keep existing status/build panels.
- Modify: `web/src/App.test.tsx`
  - Add SSR assertions for recovery console and overview guide.
- Modify: `README.md`
  - Document `/dashboard/live` as the recovery cockpit and `/dashboard` as route guide.
- Modify: `docs/runbooks/production-recovery.md`
  - Point operators to the recovery console first and clarify which issues remain CLI-only.
- Modify: `docs/runbooks/production-packaging.md`
  - Cross-link dashboard build info and recovery console; keep backups CLI-only.
- Modify: `docs/runbooks/live-stream-preflight.md`
  - Clarify that live preflight lives inside the broader recovery cockpit.
- Modify: `docs/superpowers/plans/2026-06-14-plan-11-recovery-console.md`
  - Mark complete after final verification.

---

## Task 1: Recovery Console Panel on Live Cockpit

**Files:**
- Create: `web/src/routes/dashboard/components/RecoveryConsolePanel.tsx`
- Modify: `web/src/routes/dashboard/Live.tsx`
- Modify: `web/src/App.test.tsx`

- [x] **Step 1: Create the recovery console component**

Create `web/src/routes/dashboard/components/RecoveryConsolePanel.tsx`:

```tsx
const runbookBaseUrl = 'https://git.subcult.tv/PatrickFanella/over-laid/src/branch/main/docs/runbooks';

const recoverySteps = [
  {
    label: '1 · Readiness',
    text: 'Start with Backend readiness. Degraded checks identify OBS, Twitch, EventSub, retained state, event history, or interaction-rule failures.',
  },
  {
    label: '2 · Recent events',
    text: 'Use Event history to confirm what the API recorded before an overlay or dashboard reload.',
  },
  {
    label: '3 · Rules and controls',
    text: 'Use Config for Twitch interaction rules and Control for deliberate operator actions. Viewer rules never run arbitrary OBS actions.',
  },
  {
    label: '4 · Package and backup',
    text: 'Use CLI runbook commands for api-data backups and restores. The dashboard never displays token/state archives.',
  },
];

const runbookLinks = [
  { href: `${runbookBaseUrl}/production-recovery.md`, label: 'Production recovery' },
  { href: `${runbookBaseUrl}/production-packaging.md`, label: 'Packaging + backups' },
  { href: `${runbookBaseUrl}/event-history.md`, label: 'Event history' },
  { href: `${runbookBaseUrl}/twitch-interaction-rules.md`, label: 'Twitch rules' },
  { href: `${runbookBaseUrl}/live-stream-preflight.md`, label: 'Live preflight' },
];

export function RecoveryConsolePanel() {
  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="hud-label">recovery</p>
          <h2 className="hud-section-title">Recovery console</h2>
          <p className="hud-copy mt-1 text-xs text-zinc-300">
            One operator path for live incidents: diagnose first, then recover from the relevant runbook.
          </p>
        </div>
        <span className="hud-chip hud-chip--square hud-accent-amber">no destructive browser actions</span>
      </div>

      <div className="mt-4 grid gap-2 md:grid-cols-2">
        {recoverySteps.map((step) => (
          <article key={step.label} className="hud-panel hud-corners hud-panel--flat hud-compact-card">
            <div className="hud-terminal-line">
              <span className="hud-terminal-key">step</span>
              <span className="hud-terminal-value font-bold">{step.label}</span>
            </div>
            <p className="mt-2 text-xs text-zinc-300">{step.text}</p>
          </article>
        ))}
      </div>

      <div className="mt-4 flex flex-wrap gap-2">
        {runbookLinks.map((link) => (
          <a key={link.href} className="hud-action-button hud-action-button--compact" href={link.href}>
            {link.label}
          </a>
        ))}
      </div>
    </section>
  );
}
```

- [x] **Step 2: Wire it into `/dashboard/live` with build identity**

Modify `web/src/routes/dashboard/Live.tsx` imports:

```tsx
import { BuildInfoPanel } from './components/BuildInfoPanel';
import { RecoveryConsolePanel } from './components/RecoveryConsolePanel';
```

Render after the opening Live header and before readiness:

```tsx
<RecoveryConsolePanel />
<BuildInfoPanel apiBaseUrl={apiBaseUrl} />
<ReadinessPanel report={status.readiness} loading={status.loading} error={status.readinessError} />
```

- [x] **Step 3: Add SSR route assertions**

Modify the `/dashboard/live` test in `web/src/App.test.tsx` to assert:

```ts
expect(html).toContain('Recovery console');
expect(html).toContain('no destructive browser actions');
expect(html).toContain('Production recovery');
expect(html).toContain('Packaging + backups');
expect(html).toContain('Build info');
expect(html).toContain('Build report unavailable.');
```

Add a direct component SSR test if needed:

```tsx
it('renders the recovery console without browser APIs', () => {
  const html = renderToString(<RecoveryConsolePanel />);

  expect(html).toContain('Recovery console');
  expect(html).toContain('no destructive browser actions');
  expect(html).toContain('Production recovery');
  expect(html).toContain('https://git.subcult.tv/PatrickFanella/over-laid/src/branch/main/docs/runbooks/production-recovery.md');
});
```

Remember to import `RecoveryConsolePanel` in `App.test.tsx` if adding the direct test.

- [x] **Step 4: Verify Task 1**

Run:

```bash
pnpm --dir web run test -- --run src/App.test.tsx
pnpm --dir web run build
make verify
```

Expected: all pass.

- [x] **Step 5: Commit Task 1**

```bash
git add web/src/routes/dashboard/components/RecoveryConsolePanel.tsx web/src/routes/dashboard/Live.tsx web/src/App.test.tsx
git commit -m "feat: add live recovery console"
```

---

## Task 2: Overview Operator Route Guide

**Files:**
- Create: `web/src/routes/dashboard/components/OpsRouteGuide.tsx`
- Modify: `web/src/routes/dashboard/Overview.tsx`
- Modify: `web/src/App.test.tsx`

- [x] **Step 1: Create route guide component**

Create `web/src/routes/dashboard/components/OpsRouteGuide.tsx`:

```tsx
const routeGuides = [
  { intent: 'monitor / recover', href: '/dashboard/live', label: 'Live ops cockpit', detail: 'Readiness, incidents, recent history, build identity, and recovery runbooks.' },
  { intent: 'edit rules / profiles', href: '/dashboard/config', label: 'Config', detail: 'Operator secret, show profiles, Twitch aliases, channel point reward rules.' },
  { intent: 'operate stream', href: '/dashboard/control', label: 'Stream control', detail: 'Quick actions, sequences, OBS controls, rehearsal, BRB.' },
  { intent: 'wire OBS sources', href: '/dashboard/sources', label: 'Sources', detail: 'Overlay URLs, presets, custom CSS.' },
  { intent: 'package / backup', href: 'https://git.subcult.tv/PatrickFanella/over-laid/src/branch/main/docs/runbooks/production-packaging.md', label: 'Packaging runbook', detail: 'Version checks and api-data backup/restore commands.' },
];

export function OpsRouteGuide() {
  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <p className="hud-label">operator map</p>
      <h2 className="hud-section-title">Ops route guide</h2>
      <p className="hud-copy mt-1 text-xs text-zinc-300">Pick the route by the job, not by guessing which panel owns the state.</p>
      <div className="mt-4 grid gap-2 md:grid-cols-2 xl:grid-cols-3">
        {routeGuides.map((item) => (
          <a key={item.href} className="hud-panel hud-corners hud-panel--flat hud-compact-card transition hover:border-[hsl(var(--signal))]" href={item.href}>
            <div className="hud-terminal-line">
              <span className="hud-terminal-key">intent</span>
              <span className="hud-terminal-value font-bold">{item.intent}</span>
            </div>
            <div className="hud-terminal-line">
              <span className="hud-terminal-key">route</span>
              <span className="hud-terminal-value">{item.label}</span>
            </div>
            <p className="mt-2 text-xs text-zinc-300">{item.detail}</p>
          </a>
        ))}
      </div>
    </section>
  );
}
```

- [x] **Step 2: Render route guide on overview**

Modify `web/src/routes/dashboard/Overview.tsx` imports:

```tsx
import { OpsRouteGuide } from './components/OpsRouteGuide';
```

Render after `BuildInfoPanel` and before existing route buttons:

```tsx
<BuildInfoPanel apiBaseUrl={apiBaseUrl} />
<OpsRouteGuide />
```

- [x] **Step 3: Update overview SSR assertions**

Modify the `/dashboard` test in `web/src/App.test.tsx`:

```ts
expect(html).toContain('Ops route guide');
expect(html).toContain('monitor / recover');
expect(html).toContain('edit rules / profiles');
expect(html).toContain('package / backup');
expect(html).toContain('https://git.subcult.tv/PatrickFanella/over-laid/src/branch/main/docs/runbooks/production-packaging.md');
```

- [x] **Step 4: Verify Task 2**

Run:

```bash
pnpm --dir web run test -- --run src/App.test.tsx
pnpm --dir web run build
make verify
```

Expected: all pass.

- [x] **Step 5: Commit Task 2**

```bash
git add web/src/routes/dashboard/components/OpsRouteGuide.tsx web/src/routes/dashboard/Overview.tsx web/src/App.test.tsx
git commit -m "feat: add dashboard ops route guide"
```

---

## Task 3: Recovery Console Documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/runbooks/production-recovery.md`
- Modify: `docs/runbooks/production-packaging.md`
- Modify: `docs/runbooks/live-stream-preflight.md`

- [x] **Step 1: Update README route guidance**

Patch `README.md` route/runbook section so it states:

```md
- `/dashboard/live` — canonical recovery cockpit with preflight checks, `/readyz`, build identity, EventSub diagnostics, setup checklist status, recent operator actions, and persisted event history.
- `/dashboard` — compact overview, status, build metadata, and route guide for choosing monitor/recover/config/control/source/backup flows.
```

Also add one sentence near runbook links:

```md
When something degrades during a stream, open `/dashboard/live` first; use `/dashboard` when you need to choose the right operator route or runbook.
```

- [x] **Step 2: Update production recovery runbook**

Patch `docs/runbooks/production-recovery.md` after the fast checks section:

```md
Use `/dashboard/live` as the recovery console during an incident. It combines readiness incidents, build identity, EventSub diagnostics, recent persisted events, and links to recovery runbooks. Destructive recovery actions such as token/state/history/rules removal and `api-data` restore remain CLI-only.
```

- [x] **Step 3: Update production packaging runbook**

Patch `docs/runbooks/production-packaging.md` near `/version` guidance:

```md
The dashboard overview and live recovery cockpit both surface build metadata from `/version`. Use the dashboard panel for human checks and `curl /version` or `make smoke` for scriptable deploy verification.
```

Keep existing backup sensitivity warnings intact.

- [x] **Step 4: Update live preflight runbook**

Patch `docs/runbooks/live-stream-preflight.md` near the first dashboard step:

```md
`/dashboard/live` is both the preflight page and the recovery cockpit. During setup, read the preflight cards first; during incidents, use the recovery console and readiness incidents first.
```

- [x] **Step 5: Verify docs**

Run:

```bash
make verify
```

Expected: passes.

- [x] **Step 6: Commit Task 3**

```bash
git add README.md docs/runbooks/production-recovery.md docs/runbooks/production-packaging.md docs/runbooks/live-stream-preflight.md
git commit -m "docs: document recovery console workflow"
```

---

## Task 4: Final Verification and Completion

**Files:**
- Modify: `docs/superpowers/plans/2026-06-14-plan-11-recovery-console.md`

- [x] **Step 1: Run compose config checks**

Run:

```bash
docker compose config --quiet
docker compose -f docker-compose.yml -f docker-compose.override.example.yml config --quiet
```

Expected: both pass.

- [x] **Step 2: Run full verify, rebuild, smoke, and route checks**

Run:

```bash
make verify
docker compose up -d --build
make smoke
curl --max-time 15 -fsS http://localhost:5173/dashboard/live >/dev/null
curl --max-time 15 -fsS http://localhost:5173/dashboard >/dev/null
```

Expected: all pass. `/readyz` may still return JSON `status: degraded` if EventSub is not connected; smoke only requires HTTP success.

- [x] **Step 3: Update this plan completion block**

At the top of this file, change status to complete and add:

```md
## Completion Notes

- **Completed:** 2026-06-14
- **Implemented:** Live recovery console panel, dashboard ops route guide, and recovery-console docs.
- **Verification:** `make verify`, compose config checks, `docker compose up -d --build`, `make smoke`, `/dashboard/live` and `/dashboard` route spot-checks.
- **Live QA:** Confirm `/dashboard/live` recovery flow during the next real incident/rehearsal.
```

Check every task box.

- [x] **Step 4: Commit completion**

```bash
git add docs/superpowers/plans/2026-06-14-plan-11-recovery-console.md
git commit -m "docs: mark plan 11 complete"
```

---

## Review Checklist

- [x] No browser backup/restore automation was introduced.
- [x] No secrets, tokens, state archives, or raw `api-data` contents are displayed.
- [x] `/dashboard/live` remains SSR-safe and fetch failures remain non-blocking.
- [x] `/dashboard` still renders without requiring API calls during server render.
- [x] All new UI copy points to existing runbooks and does not invent unsupported recovery actions.
- [x] `make verify` passes before each implementation commit.
