# Broadcast Ops Console UI Overhaul Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Overhaul the dashboard into a tighter broadcast operations console while preserving existing routes, backend behavior, operator safety copy, and overlay browser-source behavior.

**Architecture:** Keep the current HUD/Dracula identity, but deepen the frontend UI system around reusable console primitives, stronger dashboard shell hierarchy, and safer page layouts. The first sprint changes presentation and route composition only; API calls, backend contracts, and operator action semantics remain unchanged.

**Tech Stack:** Vite, React, TypeScript, Tailwind utility classes, project-local HUD CSS in `web/src/styles.css`, Vitest SSR tests, Playwright smoke tests.

---

## Chosen direction

Use **Broadcast Ops Console**:

- Keep the terminal/HUD visual identity.
- Add stronger command-center hierarchy: mission header, route grouping, primary status rail, and clear operational lanes.
- Separate dangerous actions visually from low-risk configuration.
- Fix missing primitives already used by TSX (`hud-button`, `hud-button--small`, `hud-led--warn`).
- Avoid a full interaction rewrite in this sprint.

## Files and responsibilities

- `web/src/styles.css` — HUD token/primitives foundation: buttons, led variants, dashboard layout helpers, danger-zone styling, route grouping helpers.
- `web/src/lib/dashboardRoutes.ts` — route metadata enriched with `group`, `priority`, and console copy used by shell/overview.
- `web/src/routes/dashboard/DashboardShell.tsx` — main console chrome: mission header, grouped nav, active route summary, Twitch login, compact operator status.
- `web/src/routes/dashboard/components/ConsolePageHeader.tsx` — reusable page-level hero for route title/description/chips/actions.
- `web/src/routes/dashboard/components/ConsoleSection.tsx` — reusable section wrapper with label/title/description/accent/danger styling.
- `web/src/routes/dashboard/Overview.tsx` — command-center landing page with grouped route cards and status priority.
- `web/src/routes/dashboard/Live.tsx` — first high-value cockpit route: compact mission header and prioritized readiness/recovery layout.
- `web/src/routes/dashboard/Config.tsx` — wrap major config areas in console sections, preserving all state/API behavior.
- `web/src/routes/dashboard/Bot.tsx` — add route-level console framing and visual separation for moderation/nuke danger areas without changing handlers.
- `web/src/App.test.tsx` — SSR contract tests for new shell/grouping/safety copy.
- `web/e2e/smoke.e2e.ts` — browser smoke checks for route group nav and always-rendered safety hierarchy.
- `docs/runbooks/visual-qa.md` — visual QA checklist for the new broadcast ops console.

---

## Task 1: HUD primitive foundation + dashboard shell grouping

**Files:**
- Modify: `web/src/styles.css`
- Modify: `web/src/lib/dashboardRoutes.ts`
- Modify: `web/src/routes/dashboard/DashboardShell.tsx`
- Test: `web/src/App.test.tsx`
- Test: `web/e2e/smoke.e2e.ts`

- [ ] **Step 1: Add SSR expectation for the new shell copy**

Add this test near existing dashboard routing tests in `web/src/App.test.tsx`:

```ts
it('renders the broadcast ops console shell hierarchy', () => {
  const html = renderToString(<App pathname="/dashboard" />);

  expect(html).toContain('Broadcast ops console');
  expect(html).toContain('Monitor');
  expect(html).toContain('Operate');
  expect(html).toContain('Configure');
});
```

- [ ] **Step 2: Update old shell-copy assertions in SSR tests**

In `web/src/App.test.tsx`, update the generic route assertion from:

```ts
expect(html).toContain('Dashboard');
```

to:

```ts
expect(html).toContain('Broadcast ops console');
```

In the dashboard overview test, update:

```ts
expect(html).toContain('over-laid command center');
```

to:

```ts
expect(html).toContain('Broadcast ops console');
```

- [ ] **Step 3: Update old shell-copy assertion in browser smoke**

In `web/e2e/smoke.e2e.ts`, update:

```ts
await expect(page.getByText('over-laid command center')).toBeVisible();
```

to:

```ts
await expect(page.getByText('Broadcast ops console')).toBeVisible();
```

- [ ] **Step 4: Run test to verify it fails before shell work**

Run:

```bash
pnpm --dir web run test -- App.test.tsx
```

Expected: failure because `Broadcast ops console` is not rendered yet.

- [ ] **Step 5: Define missing and new HUD primitives**

Append or merge these classes into `web/src/styles.css` near the existing button/LED/dashboard helpers:

```css
.hud-button,
.hud-action-button {
  border: 2px solid var(--hud-line);
  border-radius: 2px;
  background: #15161d;
  padding: 0.65rem 0.75rem;
  color: var(--hud-text);
  text-align: left;
  transition:
    background 160ms ease,
    border-color 160ms ease,
    transform 160ms ease;
}

.hud-button:hover,
.hud-action-button:hover {
  border-color: color-mix(in oklab, var(--hud-accent, var(--hud-cyan)) 50%, var(--hud-line));
  background: color-mix(in oklab, var(--hud-accent, var(--hud-cyan)) 11%, #000);
  transform: translate(-1px, -1px);
}

.hud-button:disabled,
.hud-action-button:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.hud-button--small,
.hud-action-button--compact {
  padding: 0.45rem 0.6rem;
  font-size: 0.82rem;
}

.hud-button--danger {
  --hud-accent: var(--hud-rose);
  border-color: color-mix(in oklab, var(--hud-rose) 58%, #000);
  background: color-mix(in oklab, var(--hud-rose) 10%, #000);
}

.hud-led--ok {
  --hud-accent: var(--hud-emerald);
}

.hud-led--warn {
  --hud-accent: var(--hud-amber);
}

.hud-led--danger {
  --hud-accent: var(--hud-rose);
}

.hud-console-frame {
  display: grid;
  gap: 0.85rem;
}

.hud-console-header {
  display: grid;
  gap: 0.85rem;
}

.hud-route-group {
  display: grid;
  gap: 0.4rem;
}

.hud-danger-zone {
  --hud-accent: var(--hud-rose);
  border-color: color-mix(in oklab, var(--hud-rose) 68%, #000) !important;
  background: color-mix(in oklab, var(--hud-rose) 7%, var(--hud-terminal));
}
```

Important: avoid duplicating incompatible `.hud-action-button` definitions. If the class already exists, merge only the missing aliases/variants.

- [ ] **Step 6: Extend dashboard route metadata**

Update `DashboardRoute` in `web/src/lib/dashboardRoutes.ts`:

```ts
export type DashboardRouteGroup = 'monitor' | 'setup' | 'operate' | 'configure';

export type DashboardRoute = {
  path: string;
  navLabel: string;
  title: string;
  description: string;
  landmarkText: string;
  intent: DashboardRouteGroup;
  groupLabel: 'Monitor' | 'Setup' | 'Operate' | 'Configure';
  priority: 'primary' | 'secondary';
};
```

For each route, add `groupLabel` matching `intent` and set priority:

- `/dashboard`, `/dashboard/live`, `/dashboard/control`, `/dashboard/bot`, `/dashboard/config`: `primary`
- `/dashboard/setup`, `/dashboard/sources`: `secondary`

Add helper:

```ts
export const dashboardRouteGroups: { label: DashboardRoute['groupLabel']; intent: DashboardRouteGroup }[] = [
  { label: 'Monitor', intent: 'monitor' },
  { label: 'Setup', intent: 'setup' },
  { label: 'Operate', intent: 'operate' },
  { label: 'Configure', intent: 'configure' },
];
```

- [ ] **Step 7: Replace flat shell nav with grouped console nav**

Update `DashboardShell.tsx` imports:

```ts
import { dashboardRouteGroups, dashboardRoutes, getDashboardRoute } from '../../lib/dashboardRoutes';
```

Inside `DashboardShell`, derive active route:

```ts
const activeRoute = getDashboardRoute(activePath) ?? dashboardRoutes[0];
```

Replace the header body with a console header that always renders the test copy:

```tsx
<header className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
  <div className="hud-console-header lg:grid lg:grid-cols-[minmax(260px,0.8fr)_minmax(0,1.4fr)_auto] lg:items-start">
    <div className="min-w-0">
      <p className="hud-label">Broadcast ops console</p>
      <h1 className="hud-title hud-title--compact mt-1">{activeRoute.title}</h1>
      <p className="hud-copy mt-2 text-xs text-zinc-300">{activeRoute.description}</p>
    </div>

    <nav className="grid gap-2" aria-label="Dashboard sections">
      {dashboardRouteGroups.map((group) => {
        const routes = dashboardRoutes.filter((route) => route.intent === group.intent);
        return (
          <div className="hud-route-group" key={group.intent}>
            <p className="hud-kicker">{group.label}</p>
            <div className="flex flex-wrap gap-1.5">
              {routes.map((route) => (
                <a
                  key={route.path}
                  className={`hud-chip hud-chip--square ${activePath === route.path ? 'border-cyan-200/80 bg-cyan-300/15 text-cyan-50' : ''}`}
                  href={route.path}
                  aria-current={activePath === route.path ? 'page' : undefined}
                >
                  {route.navLabel}
                </a>
              ))}
            </div>
          </div>
        );
      })}
    </nav>

    <div className="flex flex-wrap items-center gap-2 lg:justify-end">
      <span className="hud-chip hud-chip--square hud-accent-emerald">{activeRoute.groupLabel}</span>
      {loginState ? <span className="hud-chip hud-chip--square hud-accent-amber max-w-[18rem] truncate">{loginState}</span> : null}
      <button className="hud-button hud-button--small hud-accent-fuchsia" type="button" onClick={startTwitchLogin}>
        Twitch login
      </button>
    </div>
  </div>
</header>
```

- [ ] **Step 8: Run frontend format/test**

Run:

```bash
pnpm --dir web run format
pnpm --dir web run test
```

Expected: format and tests pass.

- [ ] **Step 9: Commit shell foundation**

```bash
git add web/src/styles.css web/src/App.test.tsx web/e2e/smoke.e2e.ts web/src/lib/dashboardRoutes.ts web/src/routes/dashboard/DashboardShell.tsx
git commit -m "feat: add broadcast console shell"
```

---
## Task 2: Reusable console section primitives

**Files:**
- Create: `web/src/routes/dashboard/components/ConsolePageHeader.tsx`
- Create: `web/src/routes/dashboard/components/ConsoleSection.tsx`
- Modify: `web/src/routes/dashboard/Overview.tsx`
- Test: `web/src/App.test.tsx`

- [ ] **Step 1: Add reusable page header component**

Create `web/src/routes/dashboard/components/ConsolePageHeader.tsx`:

```tsx
import type { ReactNode } from 'react';

type ConsolePageHeaderProps = {
  eyebrow: string;
  title: string;
  description: string;
  accent?: 'cyan' | 'fuchsia' | 'purple' | 'emerald' | 'amber' | 'rose';
  chips?: string[];
  actions?: ReactNode;
};

export function ConsolePageHeader({ eyebrow, title, description, accent = 'cyan', chips = [], actions }: ConsolePageHeaderProps) {
  return (
    <section className={`hud-panel hud-corners hud-panel--flat hud-accent-${accent} rounded-sm p-3`}>
      <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
        <div className="min-w-0">
          <p className="hud-label">{eyebrow}</p>
          <h2 className="hud-section-title">{title}</h2>
          <p className="hud-copy mt-1 text-xs text-zinc-300">{description}</p>
          {chips.length > 0 ? (
            <div className="mt-3 flex flex-wrap gap-2">
              {chips.map((chip) => <span className="hud-chip hud-chip--square" key={chip}>{chip}</span>)}
            </div>
          ) : null}
        </div>
        {actions ? <div className="flex flex-wrap gap-2 lg:justify-end">{actions}</div> : null}
      </div>
    </section>
  );
}
```

- [ ] **Step 2: Add reusable section component**

Create `web/src/routes/dashboard/components/ConsoleSection.tsx`:

```tsx
import type { ReactNode } from 'react';

type ConsoleSectionProps = {
  eyebrow: string;
  title?: string;
  description?: string;
  accent?: 'cyan' | 'fuchsia' | 'purple' | 'emerald' | 'amber' | 'rose';
  danger?: boolean;
  actions?: ReactNode;
  children: ReactNode;
};

export function ConsoleSection({ eyebrow, title, description, accent = 'cyan', danger = false, actions, children }: ConsoleSectionProps) {
  return (
    <section className={`hud-panel hud-corners hud-panel--flat hud-accent-${danger ? 'rose' : accent} ${danger ? 'hud-danger-zone' : ''} rounded-sm p-3`}>
      <div className="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
        <div className="min-w-0">
          <p className="hud-label">{eyebrow}</p>
          {title ? <h3 className="hud-section-title">{title}</h3> : null}
          {description ? <p className="hud-copy mt-1 text-xs text-zinc-300">{description}</p> : null}
        </div>
        {actions ? <div className="flex flex-wrap gap-2 md:justify-end">{actions}</div> : null}
      </div>
      <div className="mt-3">{children}</div>
    </section>
  );
}
```

- [ ] **Step 3: Convert Overview to the new page header**

In `Overview.tsx`, import `ConsolePageHeader` and replace the loose quick links section with:

```tsx
<ConsolePageHeader
  eyebrow="Mission control"
  title="Broadcast ops console"
  description="Start with live health, jump to operation surfaces, or configure sources before a stream."
  chips={['Monitor first', 'Operate second', 'Configure deliberately']}
/>
```

Keep `SystemStatusRail`, `BuildInfoPanel`, and `OpsRouteGuide` rendered.

- [ ] **Step 4: Add SSR assertion for mission header**

Add to the dashboard overview test in `web/src/App.test.tsx`:

```ts
expect(html).toContain('Mission control');
expect(html).toContain('Monitor first');
```

- [ ] **Step 5: Verify and commit**

Run:

```bash
pnpm --dir web run format
pnpm --dir web run test
```

Commit:

```bash
git add web/src/routes/dashboard/components/ConsolePageHeader.tsx web/src/routes/dashboard/components/ConsoleSection.tsx web/src/routes/dashboard/Overview.tsx web/src/App.test.tsx
git commit -m "feat: add console dashboard primitives"
```

---

## Task 3: Live cockpit hierarchy pass

**Files:**
- Modify: `web/src/routes/dashboard/Live.tsx`
- Test: `web/src/App.test.tsx`

- [ ] **Step 1: Replace Live route header with `ConsolePageHeader`**

Import `ConsolePageHeader` and replace the current top `<section>` with:

```tsx
<ConsolePageHeader
  eyebrow="Live command"
  title="Live ops cockpit"
  description="Check readiness, recover production issues, review EventSub, and keep session evidence visible before going live."
  accent="emerald"
  chips={[status.loading ? 'checking' : 'checked', status.error || 'no destructive browser actions']}
/>
```

- [ ] **Step 2: Group Live panels into priority lanes**

Wrap existing children after the header in this layout:

```tsx
<div className="hud-dashboard-grid hud-dashboard-grid--two">
  <div className="grid gap-3">
    <RecoveryConsolePanel />
    <ReadinessPanel report={status.readiness} loading={status.loading} error={status.readinessError} />
    <LivePreflight twitch={status.twitch} obs={status.obs} config={config} operatorSecret={operatorSecret} />
    <EventSubDiagnostics status={status.twitch} />
  </div>
  <div className="grid gap-3">
    <BuildInfoPanel apiBaseUrl={apiBaseUrl} />
    <StreamSessionPanel apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} status={status.session} loading={status.loading} error={status.sessionError} onChange={(session) => setStatus((current) => ({ ...current, session }))} />
    <LiveChecklistSummary items={config.setupChecklist} />
    <SessionLogPanel entries={sessionLog} />
  </div>
</div>
<EventHistoryPanel events={status.history?.events ?? []} loading={status.loading} error={status.historyError} />
```

Do not change API loading or panel props.

- [ ] **Step 3: Update SSR expectations**

In the live route test, add:

```ts
expect(html).toContain('Live command');
expect(html).toContain('Check readiness, recover production issues');
```

- [ ] **Step 4: Verify and commit**

Run:

```bash
pnpm --dir web run format
pnpm --dir web run test
```

Commit:

```bash
git add web/src/routes/dashboard/Live.tsx web/src/App.test.tsx
git commit -m "feat: tighten live cockpit hierarchy"
```

---

## Task 4: Config and Bot safety hierarchy pass

**Files:**
- Modify: `web/src/routes/dashboard/Config.tsx`
- Modify: `web/src/routes/dashboard/Bot.tsx`
- Test: `web/src/App.test.tsx`
- Test: `web/e2e/smoke.e2e.ts`

- [ ] **Step 1: Add page headers without removing existing controls**

In `Config.tsx`, import `ConsolePageHeader` and place it before the existing config sections:

```tsx
<ConsolePageHeader
  eyebrow="Configure deliberately"
  title="Operator config"
  description="Edit local operator preferences, OBS mappings, Twitch interaction rules, and overlay display behavior. Secrets stay in browser session state."
  accent="purple"
  chips={[`API ${apiBaseUrl}`, operatorSecret ? 'operator secret loaded' : 'operator secret missing']}
/>
```

Do **not** delete the operator secret input, webhook secret input, or `Secret header` copy. The current first config section contains real controls; after adding `ConsolePageHeader`, keep those cards in a following section or rehome them inside a `ConsoleSection`.

In `Bot.tsx`, import `ConsolePageHeader` and add it as the first child under `DashboardShell`:

```tsx
<ConsolePageHeader
  eyebrow="Operate with guardrails"
  title="Bot operations"
  description="Manage bot commands, community data, API variables, timers, moderation dry-runs, and guarded destructive actions."
  accent="amber"
  chips={[state.loading ? 'loading bot state' : 'bot state loaded', operatorSecret ? 'operator secret loaded' : 'operator secret missing']}
/>
```

- [ ] **Step 2: Visually mark danger areas**

In `Bot.tsx`, add `hud-danger-zone` to the section that renders `Nuke dry-run planner`. Preserve the text `Nuke dry-run planner`, `I reviewed this dry-run plan`, and `Radiation minutes`.

In `Config.tsx` or `TwitchRewardQueueManager.tsx`, ensure the delete block remains visually separate and includes the existing text `Delete requires confirmation` and `Delete is last resort`.

- [ ] **Step 3: Add tests for safety hierarchy copy**

In `web/src/App.test.tsx`, add to existing bot/config tests:

```ts
expect(html).toContain('Operate with guardrails');
expect(html).toContain('guarded destructive actions');
```

and:

```ts
expect(html).toContain('Configure deliberately');
expect(html).toContain('Secrets stay in browser session state');
```

In `web/e2e/smoke.e2e.ts`, update static copy assertions on the correct routes. In the bot test after `page.goto('/dashboard/bot')`, use unique copy instead of duplicated page titles:

```ts
await expect(page.getByText('Operate with guardrails')).toBeVisible();
```

In the config/reward test after `page.goto('/dashboard/config')`, add:

```ts
await expect(page.getByText('Configure deliberately')).toBeVisible();
```

- [ ] **Step 4: Verify and commit**

Run:

```bash
pnpm --dir web run format
pnpm --dir web run test
```

Commit:

```bash
git add web/src/routes/dashboard/Config.tsx web/src/routes/dashboard/Bot.tsx web/src/App.test.tsx web/e2e/smoke.e2e.ts
git commit -m "feat: separate operator safety zones"
```

---

## Task 5: Visual QA docs and full verification

**Files:**
- Modify: `docs/runbooks/visual-qa.md`

- [ ] **Step 1: Document Broadcast Ops Console QA checks**

Add a section to `docs/runbooks/visual-qa.md`:

```md
## Broadcast ops console checks

- Dashboard shell renders `Broadcast ops console` and groups navigation into Monitor, Setup, Operate, and Configure.
- `/dashboard` shows the Mission control header, system status, build info, and route guide without pushing primary links below the fold on desktop.
- `/dashboard/live` prioritizes recovery/readiness/preflight before secondary evidence panels.
- `/dashboard/bot` shows `Operate with guardrails`; destructive nuke/radiation controls are visually distinct from commands, counters, quotes, and timers.
- `/dashboard/config` shows `Configure deliberately`; Twitch reward delete controls remain separated behind confirmation and lower visual priority than pause/disable/update controls.
- Keyboard focus remains visible on buttons, chips, inputs, and route navigation.
```

- [ ] **Step 2: Run full verification**

Ensure stack is running for smoke commands:

```bash
docker compose up -d --build
make verify
make smoke
make smoke-browser
```

If `web/test-results/` is generated, remove it before commit:

```bash
rm -rf web/test-results
```

- [ ] **Step 3: Inspect diff and commit docs/closeout**

Run:

```bash
git status --short
git diff --check
git diff --stat
```

Commit:

```bash
git add docs/runbooks/visual-qa.md
git commit -m "docs: add broadcast console visual qa"
```

---

## Deferred work

- Deeper `Bot.tsx` panel extraction beyond current route-local modules.
- Full overlay frame component shared by alert/chat/goals/ticker/now-playing/BRB/debug routes.
- Interaction-level Playwright tests for creating/updating rewards or executing moderation dry-runs with mocked API data.
- Design tokens migration to a formal component library.

## Self-review

- Spec coverage: requested UI overhaul is scoped to chosen Broadcast Ops Console direction; first sprint improves shell, hierarchy, primitives, high-value Live route, Bot/Config safety zones, and visual QA.
- Placeholder scan: no TBD/TODO/fill-in steps; concrete files, code snippets, commands, and expected outcomes are included.
- Type consistency: new `DashboardRouteGroup`, `groupLabel`, and `priority` fields are used consistently by `DashboardShell`; new component props use explicit accent union shared between `ConsolePageHeader` and `ConsoleSection`.
