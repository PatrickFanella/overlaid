# Route QA Hardening Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Centralize route metadata and harden route-level QA so dashboard and overlay surfaces stay discoverable, documented, and regression-tested as the stream ops console grows.

**Architecture:** Keep this sprint frontend/docs-only. Add shared route registries for dashboard and overlay routes, reuse them in navigation/home/docs-adjacent UI, and add SSR tests that render every declared route with route-specific landmarks. Do not add screenshot tooling, backend endpoints, or new operator features.

**Tech Stack:** Vite React TypeScript, SSR tests with `renderToString` + Vitest, existing Tailwind/HUD classes, Markdown docs.

---

## Current status

**Status:** Complete  
**Created:** 2026-06-14  
**Completed:** 2026-06-14  
**Branch state at planning:** `main...origin/main [ahead 35]`  
**Live QA:** Manual visual pass pending next operator browser session.

## Completion notes

- Centralized dashboard route metadata and reused it in dashboard nav, home route cards, and the overview route guide.
- Removed the local overlay route `Set` duplication in `App.tsx` and added metadata-driven SSR coverage for every dashboard and overlay route.
- Updated README and visual QA guidance for the final recovery-console/dashboard surfaces.
- Verification: compose config checks, `make verify`, `docker compose up -d --build`, `make smoke`, and route spot checks for `/dashboard`, `/dashboard/live`, `/dashboard/config`, `/overlay/alerts`, and `/overlay/debug` passed.

## Acceptance criteria

- Dashboard route metadata lives in one typed frontend module and includes `/dashboard`, `/dashboard/live`, `/dashboard/setup`, `/dashboard/sources`, `/dashboard/control`, and `/dashboard/config`.
- Overlay route metadata remains the single metadata source for overlay routes and includes `/overlay/alerts`, `/overlay/chat`, `/overlay/goals`, `/overlay/now-playing`, `/overlay/ticker`, `/overlay/brb`, and `/overlay/debug`.
- `App.tsx` no longer has a separate local overlay route `Set`; known overlay checks derive from `web/src/lib/overlayRoutes.ts`.
- Dashboard shell navigation renders from shared dashboard route metadata, not a local tuple list.
- Home route cards render from shared dashboard + overlay route metadata, not a duplicated local route list.
- `web/src/App.test.tsx` has table-driven SSR coverage that renders every dashboard route and every overlay route declared in metadata.
- Unknown paths still render the home fallback and show the missing path.
- Visual QA docs mention all current route surfaces, including recovery console, route guide, build info, Twitch interactions, event history, automation profiles, and persisted history.
- `README.md` route summary stays aligned with the metadata/runbook wording.
- `make verify` passes after every implementation task.
- Final checks include compose config, `make verify`, rebuild, `make smoke`, and spot checks for `/dashboard`, `/dashboard/live`, `/dashboard/config`, `/overlay/alerts`, and `/overlay/debug`.

## Non-goals

- Do not add Playwright, screenshots, browser automation, or visual snapshot tooling in this sprint.
- Do not change overlay behavior, EventSub behavior, Twitch rules, OBS controls, or API contracts.
- Do not make docs available through nginx; existing runbook links that point to Forgejo remain external links.
- Do not add auth or mutating controls.

## File structure

- Create: `web/src/lib/dashboardRoutes.ts` — typed dashboard route metadata and helpers.
- Modify: `web/src/routes/dashboard/DashboardShell.tsx` — render nav from `dashboardRoutes`.
- Modify: `web/src/routes/Home.tsx` — render home cards from `dashboardRoutes` + `overlayRoutes`.
- Modify: `web/src/routes/dashboard/components/OpsRouteGuide.tsx` — derive dashboard route links from `dashboardRoutes`.
- Modify: `web/src/App.tsx` — derive known overlay route checks from `overlayRoutes` metadata.
- Modify: `web/src/App.test.tsx` — add route metadata table tests and fallback test.
- Modify: `README.md` — tighten route summary to match final metadata wording.
- Modify: `docs/runbooks/visual-qa.md` — update manual QA checklist for final route set and recovery-console surfaces.
- Modify: `docs/superpowers/plans/2026-06-14-plan-12-route-qa-hardening.md` — mark completion at the end.

---

## Task 1: Shared dashboard route metadata

**Files:**
- Create: `web/src/lib/dashboardRoutes.ts`
- Modify: `web/src/routes/dashboard/DashboardShell.tsx`
- Modify: `web/src/routes/Home.tsx`
- Modify: `web/src/App.test.tsx`

- [x] **Step 1: Create the dashboard route registry**

Create `web/src/lib/dashboardRoutes.ts`:

```ts
export type DashboardRoute = {
  path: string;
  navLabel: string;
  title: string;
  description: string;
  landmarkText: string;
  intent: 'monitor' | 'setup' | 'operate' | 'configure';
};

export const dashboardRoutes: DashboardRoute[] = [
  {
    path: '/dashboard',
    navLabel: 'Overview',
    title: 'Dashboard overview',
    description: 'Status, build identity, and route guide for choosing the right operator surface.',
    landmarkText: 'Ops route guide',
    intent: 'monitor',
  },
  {
    path: '/dashboard/live',
    navLabel: 'Live',
    title: 'Live ops cockpit',
    description: 'Recovery cockpit with readiness, runbook links, build info, EventSub diagnostics, checklist, session log, and event history.',
    landmarkText: 'Recovery console',
    intent: 'monitor',
  },
  {
    path: '/dashboard/setup',
    navLabel: 'Setup',
    title: 'OBS setup workflow',
    description: 'Setup checklist for OBS onboarding, smoke checks, and setup decisions.',
    landmarkText: 'Checklist',
    intent: 'setup',
  },
  {
    path: '/dashboard/sources',
    navLabel: 'Sources',
    title: 'Overlay source URLs',
    description: 'OBS browser-source URLs, active-profile preset view, and custom CSS copy helpers.',
    landmarkText: 'Stream presets',
    intent: 'setup',
  },
  {
    path: '/dashboard/control',
    navLabel: 'Stream control',
    title: 'Stream control',
    description: 'Operator quick actions, rehearsals, BRB controls, OBS controls, and session log.',
    landmarkText: 'Session log',
    intent: 'operate',
  },
  {
    path: '/dashboard/config',
    navLabel: 'Config',
    title: 'Operator config',
    description: 'Operator secret, automation profiles, Twitch interaction rules, MPRIS decision, and overlay noise controls.',
    landmarkText: 'Twitch interaction rules',
    intent: 'configure',
  },
];

export function getDashboardRoute(path: string) {
  return dashboardRoutes.find((route) => route.path === path);
}
```

- [x] **Step 2: Update dashboard navigation to use metadata**

In `web/src/routes/dashboard/DashboardShell.tsx`:

```ts
import { dashboardRoutes } from '../../lib/dashboardRoutes';
```

Remove the local `dashboardLinks` tuple and replace the nav map with:

```tsx
{dashboardRoutes.map((route) => (
  <a
    key={route.path}
    className={`hud-chip hud-chip--square ${activePath === route.path ? 'border-cyan-200/80 bg-cyan-300/15 text-cyan-50' : ''}`}
    href={route.path}
  >
    {route.navLabel}
  </a>
))}
```

- [x] **Step 3: Update home route cards to use route metadata**

In `web/src/routes/Home.tsx`, import:

```ts
import { dashboardRoutes } from '../lib/dashboardRoutes';
import { overlayRoutes } from '../lib/overlayRoutes';
```

Remove the local `routes` tuple and replace the home card data with:

```ts
const routes = [
  ...dashboardRoutes.map((route) => [route.path, route.title, route.description] as const),
  ...overlayRoutes.map((route) => [route.path, route.title, route.description] as const),
];
```

- [x] **Step 4: Derive the overview route guide from metadata**

In `web/src/routes/dashboard/components/OpsRouteGuide.tsx`, import:

```ts
import { getDashboardRoute } from '../../../lib/dashboardRoutes';
```

Keep the packaging runbook entry as a separate external link. Replace dashboard route labels/details with metadata lookups so the guide reuses the route registry:

```ts
const liveRoute = getDashboardRoute('/dashboard/live');
const configRoute = getDashboardRoute('/dashboard/config');
const controlRoute = getDashboardRoute('/dashboard/control');
const sourcesRoute = getDashboardRoute('/dashboard/sources');

const routeGuides = [
  {
    intent: 'monitor / recover',
    href: liveRoute?.path ?? '/dashboard/live',
    label: liveRoute?.title ?? 'Live ops cockpit',
    detail: liveRoute?.description ?? 'Readiness, incidents, build identity, event history, and recovery runbooks.',
  },
  {
    intent: 'edit rules / profiles',
    href: configRoute?.path ?? '/dashboard/config',
    label: configRoute?.navLabel ?? 'Config',
    detail: configRoute?.description ?? 'Operator secret, show profiles, Twitch aliases, and channel point reward rules.',
  },
  {
    intent: 'operate stream',
    href: controlRoute?.path ?? '/dashboard/control',
    label: controlRoute?.title ?? 'Stream control',
    detail: controlRoute?.description ?? 'Quick actions, sequences, OBS controls, rehearsal, and BRB.',
  },
  {
    intent: 'wire OBS sources',
    href: sourcesRoute?.path ?? '/dashboard/sources',
    label: sourcesRoute?.navLabel ?? 'Sources',
    detail: sourcesRoute?.description ?? 'Overlay URLs, presets, and custom CSS.',
  },
  {
    intent: 'package / backup',
    href: `${runbookBaseUrl}/production-packaging.md`,
    label: 'Packaging runbook',
    detail: 'Version checks and api-data backup / restore commands.',
  },
] as const;
```

- [x] **Step 5: Remove local overlay route duplication in App routing**

In `web/src/App.tsx`, import overlay metadata with an alias to avoid naming conflicts:

```ts
import { overlayRoutes as overlayRouteMetadata } from './lib/overlayRoutes';
```

Replace the local `overlayRoutes` `Set` with:

```ts
const overlayRoutePaths = new Set(overlayRouteMetadata.map((route) => route.path));
```

Update the placeholder check:

```ts
if (overlayRoutePaths.has(route)) {
  return <OverlayPlaceholderPage pathname={route} />;
}
```

Keep the explicit component branches for now; they are the route-to-component map. Metadata owns route discovery, nav/home/docs-adjacent UI, and route test inputs.

- [x] **Step 6: Add metadata-driven SSR route tests**

In `web/src/App.test.tsx`, import:

```ts
import { dashboardRoutes } from './lib/dashboardRoutes';
import { overlayRoutes } from './lib/overlayRoutes';
```

Add these tests near the existing routing tests:

```tsx
it.each(dashboardRoutes)('renders declared dashboard route $path', (route) => {
  const html = renderToString(<App pathname={route.path} />);

  expect(html).toContain('Dashboard');
  expect(html).toContain(route.navLabel);
  expect(html).toContain(route.landmarkText);
});

it.each(overlayRoutes)('renders declared overlay route $path', (route) => {
  const html = renderToString(<App pathname={route.path} />);

  expect(html).toContain(route.title);
});

it('renders home fallback for unknown paths', () => {
  const html = renderToString(<App pathname="/not-a-real-route" />);

  expect(html).toContain('Overlay routes');
  expect(html).toContain('/not-a-real-route');
  expect(html).toContain('pick a route below');
});
```

- [x] **Step 7: Run verification**

Run:

```bash
pnpm --dir web run test -- --run src/App.test.tsx
pnpm --dir web run build
make verify
```

Expected: all commands pass.

- [x] **Step 8: Review and commit**

Request review focused on SSR safety, route metadata duplication removal, and test brittleness. After review passes:

```bash
git add web/src/lib/dashboardRoutes.ts web/src/routes/dashboard/DashboardShell.tsx web/src/routes/Home.tsx web/src/routes/dashboard/components/OpsRouteGuide.tsx web/src/App.tsx web/src/App.test.tsx
git commit -m "feat: centralize dashboard route metadata"
```

---

## Task 2: Visual QA docs and route guide alignment

**Files:**
- Modify: `README.md`
- Modify: `docs/runbooks/visual-qa.md`
- Modify: `web/src/App.test.tsx`

- [x] **Step 1: Update README route wording**

In `README.md`, keep the route list but align text with the new metadata wording:

```md
- `/dashboard/live` — canonical recovery cockpit with readiness, recovery runbook links, build info, EventSub diagnostics, setup checklist status, recent operator actions, and persisted event history.
- `/dashboard` — overview, system status, build identity, and route guide for choosing the right runbook or dashboard path.
- `/dashboard/setup` — setup checklist for OBS onboarding, smoke checks, and setup decisions.
- `/dashboard/sources` — OBS browser-source URLs, active-profile preset view, and custom CSS copy helpers.
- `/dashboard/control` — operator quick actions, rehearsals, BRB controls, OBS controls, and session log.
- `/dashboard/config` — operator secret session storage, automation profiles/show modes, Twitch interaction rules, MPRIS decision, and overlay noise controls.
```

- [x] **Step 2: Refresh visual QA dashboard checklist**

In `docs/runbooks/visual-qa.md`, update the dashboard route checks to include final Plan 6-11 surfaces:

```md
### `/dashboard`

- [x] Page loads without a blank screen or console overlay.
- [x] Header, status cards, build-info card, and ops route guide are readable.
- [x] Route guide points operators to live recovery, config/rules, stream control, sources, and packaging/backup guidance.
- [x] No text overflows or clipped panels at the default desktop width.

### `/dashboard/live`

- [x] Recovery console, build info, backend readiness, live preflight, EventSub diagnostics, setup checklist, session log, and event history panels are visible.
- [x] Recovery-console runbook links are clearly external and do not look like destructive browser actions.
- [x] Twitch, EventSub, OBS, operator-secret, event-history, and interaction-rule indicators are easy to scan.
- [x] Degraded states are obvious without horizontal scrolling.

### `/dashboard/config`

- [x] Operator secret inputs, automation profiles, Twitch interaction rules, MPRIS decision, and overlay noise controls are aligned.
- [x] Import/export textareas stay readable and do not expose operator secret values.
- [x] Configuration panels stay readable on a standard laptop viewport.
- [x] No form controls are clipped by cards or columns.
```

Keep existing overlay route checks, but add this note under `## Overlay routes`:

```md
Overlay routes should remain sparse and broadcast-safe. Generic diagnostic or command events may appear in ticker/debug routes; production overlays should not expose raw secrets or token material.
```

- [x] **Step 3: Add README/QA route text assertions**

In `web/src/App.test.tsx`, do not parse Markdown. Instead strengthen SSR tests for the key UI copy that README/QA describes:

```tsx
expect(renderToString(<App pathname="/dashboard" />)).toContain('Ops route guide');
expect(renderToString(<App pathname="/dashboard/live" />)).toContain('Recovery console');
expect(renderToString(<App pathname="/dashboard/config" />)).toContain('Twitch interaction rules');
```

If these assertions already exist, leave the test file unchanged.

- [x] **Step 4: Run verification**

Run:

```bash
pnpm --dir web run test -- --run src/App.test.tsx
make verify
```

Expected: all commands pass.

- [x] **Step 5: Review and commit**

Request docs consistency review focused on README vs visual QA vs implemented routes. After review passes:

```bash
git add README.md docs/runbooks/visual-qa.md web/src/App.test.tsx
git commit -m "docs: align route visual qa guidance"
```

---

## Task 3: Final verification and plan completion

**Files:**
- Modify: `docs/superpowers/plans/2026-06-14-plan-12-route-qa-hardening.md`

- [x] **Step 1: Run compose config checks**

Run:

```bash
docker compose config --quiet
docker compose -f docker-compose.yml -f docker-compose.override.example.yml config --quiet
```

Expected: both pass with no output.

- [x] **Step 2: Run full verification and deploy-smoke**

Run:

```bash
make verify
docker compose up -d --build
make smoke
curl --max-time 15 -fsS http://localhost:5173/dashboard >/dev/null
curl --max-time 15 -fsS http://localhost:5173/dashboard/live >/dev/null
curl --max-time 15 -fsS http://localhost:5173/dashboard/config >/dev/null
curl --max-time 15 -fsS http://localhost:5173/overlay/alerts >/dev/null
curl --max-time 15 -fsS http://localhost:5173/overlay/debug >/dev/null
```

Expected: all commands pass. `/readyz` may still return JSON `status: degraded` when EventSub is disconnected, but the HTTP smoke check must succeed.

- [x] **Step 3: Mark this plan complete**

Update this file near the top:

```md
**Status:** Complete  
**Completed:** 2026-06-14  
**Live QA:** Manual visual pass pending next operator browser session.

## Completion notes

- Centralized dashboard route metadata and reused it in dashboard nav/home cards.
- Added metadata-driven SSR coverage for every dashboard and overlay route.
- Updated route/visual QA documentation for the final recovery-console/dashboard surfaces.
- Verification: `make verify`, compose config checks, `docker compose up -d --build`, `make smoke`, and route spot checks passed.
```

Check every task box in this plan. Confirm no incomplete task boxes remain:

```bash
grep -R "- \[ \]" docs/superpowers/plans/2026-06-14-plan-12-route-qa-hardening.md
```

Expected: only the standard header prose may include the literal checkbox example; no actual incomplete task lines remain.

- [x] **Step 4: Commit completion**

Run:

```bash
git add docs/superpowers/plans/2026-06-14-plan-12-route-qa-hardening.md
git commit -m "docs: mark plan 12 complete"
```

---

## Review checklist

- [x] Route metadata does not introduce routing behavior changes beyond nav/home/route-guide data and App known-route checks.
- [x] Metadata-driven tests are not brittle to transient fetched data.
- [x] Visual QA docs mention final dashboard surfaces built in Plans 6-11.
- [x] No secrets, backup archive contents, token files, or `api-data` contents are exposed.
- [x] `make verify` passes after every implementation task.
