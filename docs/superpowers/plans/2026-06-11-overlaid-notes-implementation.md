# Overlaid Notes Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Implement `docs/overlaid-notes.md` by turning the dashboard into a multi-route operator console with reusable HUD utilities, persisted setup/config state, editable stream presets, and configurable quick actions.

**Architecture:** Keep overlays and the read-only OBS browser-source routes unchanged. Split the current large `web/src/routes/Dashboard.tsx` into small dashboard routes and reusable components, backed by local browser storage first; add backend persistence only for data that must be shared across browser sessions or machines. Reuse existing operator-secret protection and existing API endpoints for all mutating actions.

**Tech Stack:** React 19 + Vite + TypeScript, Tailwind v4/global CSS HUD utilities, Go HTTP API, existing file-backed state store, Docker Compose.

---

## Source Notes Coverage

This plan covers every item from `docs/overlaid-notes.md`:

- Theme/style semantic tokens and reusable Tailwind-style utility classes.
- Neubrutal HUD theme consolidation.
- Custom CSS utility classes for stream elements.
- Dashboard split into multiple screens/routes.
- Setup workflow disappears when complete and green.
- Operator secret moved to config route and remembered safely.
- Compact sticky system status.
- Overlay URL cards and stream presets moved to their own route.
- Stream presets made editable.
- Stream control route for events, BRB, scene controls, and quick actions.
- Editable quick actions for scene buttons, refresh overlay/source, fire event, mute/toggle controls, clear/reset overlays, and alert controls.

## File Structure

Create:

- `web/src/lib/dashboardConfig.ts` — typed local-storage backed config for operator secret presence, setup checklist, editable stream presets, and quick actions.
- `web/src/lib/dashboardSamples.ts` — sample alert/ticker/goals/BRB payloads and default quick actions.
- `web/src/lib/overlayRoutes.ts` — shared overlay route metadata and browser-source URL builder.
- `web/src/routes/dashboard/DashboardShell.tsx` — shared dashboard layout, navigation, and sticky compact status rail.
- `web/src/routes/dashboard/Overview.tsx` — compact status overview and links into setup/control/config routes.
- `web/src/routes/dashboard/Setup.tsx` — setup checklist, source URLs, editable stream presets.
- `web/src/routes/dashboard/Control.tsx` — stream controls: fire events, BRB component, OBS scene/input/item controls, quick actions.
- `web/src/routes/dashboard/Config.tsx` — operator secret, webhook secret, API base guidance, MPRIS decision.
- `web/src/routes/dashboard/components/SetupChecklist.tsx` — checklist component that hides completed/green workflow by default.
- `web/src/routes/dashboard/components/OverlaySourceCards.tsx` — copyable source URL cards.
- `web/src/routes/dashboard/components/StreamPresetEditor.tsx` — editable preset cards.
- `web/src/routes/dashboard/components/QuickActionsEditor.tsx` — editable quick-action list.
- `web/src/routes/dashboard/components/QuickActionRunner.tsx` — runs configured quick actions.
- `web/src/routes/dashboard/components/SystemStatusRail.tsx` — compact sticky status rail.
- `web/src/routes/dashboard/components/BRBControl.tsx` — reusable BRB control component.
- `web/src/routes/dashboard/components/OBSControls.tsx` — OBS scene/input/item controls moved out of `Dashboard.tsx`.
- `web/src/routes/dashboard/components/SampleEventControls.tsx` — alert/ticker/goals sample buttons.

Modify:

- `web/src/App.tsx` — route `/dashboard`, `/dashboard/setup`, `/dashboard/sources`, `/dashboard/control`, `/dashboard/config`.
- `web/src/routes/Dashboard.tsx` — reduce to compatibility wrapper that renders `DashboardShell` + overview.
- `web/src/lib/api.ts` — add a `refreshOverlaySource` helper only if OBS refresh is implemented through OBS WebSocket; otherwise keep existing functions.
- `web/src/styles.css` — add semantic HUD utility classes extracted from repeated dashboard markup.
- `web/src/App.test.tsx` — route tests for all new dashboard screens and config persistence helpers.
- `README.md` — document new dashboard routes and setup workflow.
- `docs/overlaid-notes.md` — optionally append implementation status after completion.

Optional backend create/modify for Task 6 if cross-browser persistence is required:

- Create `backend/internal/http/config_handlers.go` — protected config-state endpoints.
- Modify `backend/internal/http/routes.go` — protected `GET/PUT /api/operator/config`.
- Modify `backend/internal/http/handlers_test.go` — config endpoint tests.

## Key Decisions

- **First implementation uses browser localStorage** for operator console preferences because this is an operator workstation feature, not stream state. Do not store secrets server-side unless later requested.
- **Operator secret persistence:** store in `sessionStorage` by default, not `localStorage`, so a browser restart drops it. Display “secret loaded for this tab/session” instead of showing the raw value outside the password input.
- **Setup complete behavior:** completed checklist stays hidden behind a “Show completed setup” toggle, not removed permanently.
- **Quick actions:** model them as typed JSON objects with a small fixed action vocabulary. Do not add arbitrary scripting.
- **Dashboard split:** keep `/dashboard` as a working overview route so existing bookmarks do not break.

---

### Task 1: Extract Shared Dashboard Data and Storage Helpers

**Files:**
- Create: `web/src/lib/overlayRoutes.ts`
- Create: `web/src/lib/dashboardSamples.ts`
- Create: `web/src/lib/dashboardConfig.ts`
- Modify: `web/src/routes/Dashboard.tsx`
- Test: `web/src/App.test.tsx`

- [ ] **Step 1: Create shared overlay route metadata**

Create `web/src/lib/overlayRoutes.ts`:

```ts
import { getDefaultApiBaseUrl } from './eventStream';

export type OverlayRoute = {
  path: string;
  title: string;
  description: string;
  defaultScene: 'main' | 'brb' | 'debug';
};

export const overlayRoutes: OverlayRoute[] = [
  { path: '/overlay/alerts', title: 'Alert feed', description: 'Live alert browser source', defaultScene: 'main' },
  { path: '/overlay/chat', title: 'Twitch chat', description: 'Live chat browser source', defaultScene: 'main' },
  { path: '/overlay/goals', title: 'Goal rail', description: 'Progress widget browser source', defaultScene: 'main' },
  { path: '/overlay/now-playing', title: 'Now playing', description: 'Music and media browser source', defaultScene: 'main' },
  { path: '/overlay/ticker', title: 'Ticker rail', description: 'Lower-third event ticker', defaultScene: 'main' },
  { path: '/overlay/brb', title: 'BRB overlay', description: 'Intermission scene control', defaultScene: 'brb' },
  { path: '/overlay/debug', title: 'Event inspector', description: 'SSE event stream debugger', defaultScene: 'debug' },
];

export function getBrowserOrigin() {
  if (typeof window !== 'undefined' && window.location.origin) {
    return window.location.origin;
  }
  return 'http://localhost:5173';
}

export function buildOverlaySourceUrl(path: string, apiBaseUrl: string) {
  const origin = getBrowserOrigin();
  const url = new URL(path, origin);
  const defaultApiBaseUrl = getDefaultApiBaseUrl(origin);
  if (apiBaseUrl && apiBaseUrl !== defaultApiBaseUrl) {
    url.searchParams.set('apiBaseUrl', apiBaseUrl);
  }
  return url.toString();
}
```

- [ ] **Step 2: Create sample payload module**

Create `web/src/lib/dashboardSamples.ts`:

```ts
import type { AlertPayload, BRBPayload } from './api';

export const sampleAlert: AlertPayload = {
  title: 'Sample alert',
  message: 'Overlay command center smoke test passed',
  severity: 'success',
};

export const sampleTicker: AlertPayload = {
  title: 'Sample ticker',
  message: 'Lower-third ticker is live',
  severity: 'info',
  data: { ticker: { id: 'sample-ticker', label: 'Ticker sample', message: 'Lower-third ticker is live' } },
};

export const sampleGoals: AlertPayload = {
  title: 'Sample goals',
  message: 'Goal rail accepted a live update',
  severity: 'success',
  data: {
    goal: {
      id: 'followers',
      label: 'Follower goal',
      current: 430,
      target: 620,
      unit: 'followers',
      note: 'Sample goal update',
      accent: 'var(--hud-emerald)',
    },
  },
};

export const sampleBRBShow: BRBPayload = {
  active: true,
  title: 'Be right back',
  message: 'Short intermission. Stream resumes soon.',
  eta: '5 min',
  items: ['Hydrate', 'Stretch', 'Chat stays live'],
  data: { preset: 'sample-show' },
};

export const sampleBRBHide: BRBPayload = {
  ...sampleBRBShow,
  active: false,
  data: { preset: 'sample-hide' },
};
```

- [ ] **Step 3: Create dashboard config/storage helper**

Create `web/src/lib/dashboardConfig.ts`:

```ts
export type SetupChecklistItem = {
  id: string;
  title: string;
  description: string;
  completed: boolean;
};

export type StreamPreset = {
  id: string;
  title: string;
  sources: string[];
};

export type QuickAction =
  | { id: string; label: string; kind: 'setScene'; sceneName: string }
  | { id: string; label: string; kind: 'sendAlert'; title: string; message: string; severity: 'info' | 'success' | 'warning' | 'error' }
  | { id: string; label: string; kind: 'showBRB'; title: string; message: string; eta: string; items: string[] }
  | { id: string; label: string; kind: 'hideBRB' }
  | { id: string; label: string; kind: 'clearOverlays' }
  | { id: string; label: string; kind: 'muteInput'; inputName: string; muted: boolean };

export type DashboardConfig = {
  setupChecklist: SetupChecklistItem[];
  streamPresets: StreamPreset[];
  quickActions: QuickAction[];
  mprisDecision: 'enabled' | 'disabled' | 'undecided';
};

const CONFIG_KEY = 'overlaid.dashboard.config.v1';
const OPERATOR_SECRET_KEY = 'overlaid.operatorSecret.session.v1';

export const defaultDashboardConfig: DashboardConfig = {
  setupChecklist: [
    { id: 'browser-sources', title: 'Browser sources added', description: 'Add every route once, then verify each source loads cleanly in OBS.', completed: false },
    { id: 'live-smoke', title: 'Live event smoke', description: 'Fire sample buttons and confirm alerts, ticker, goals, and BRB states appear.', completed: false },
    { id: 'mpris-decision', title: 'MPRIS decision', description: 'Decide whether now-playing should read from local MPRIS or stay disabled.', completed: false },
    { id: 'stream-presets', title: 'Stream presets', description: 'Pick scene-specific source sets so switching scenes is a single click.', completed: false },
    { id: 'controls-checked', title: 'Dashboard controls checked', description: 'Verify operator secret, OBS controls, and clear/reset behavior before going live.', completed: false },
  ],
  streamPresets: [
    { id: 'main', title: 'Main scene', sources: ['alerts', 'chat', 'goals', 'now-playing', 'ticker'] },
    { id: 'brb', title: 'BRB scene', sources: ['brb', 'ticker', 'now-playing'] },
    { id: 'debug', title: 'Debug scene', sources: ['debug', 'alerts', 'ticker'] },
  ],
  quickActions: [
    { id: 'main-scene', label: 'Go Main', kind: 'setScene', sceneName: 'Main' },
    { id: 'brb-scene', label: 'Go BRB', kind: 'setScene', sceneName: 'BRB' },
    { id: 'sample-alert', label: 'Sample Alert', kind: 'sendAlert', title: 'Sample alert', message: 'Quick action fired', severity: 'success' },
    { id: 'show-brb', label: 'Show BRB', kind: 'showBRB', title: 'Be right back', message: 'Short intermission. Stream resumes soon.', eta: '5 min', items: ['Hydrate', 'Stretch', 'Chat stays live'] },
    { id: 'hide-brb', label: 'Hide BRB', kind: 'hideBRB' },
  ],
  mprisDecision: 'undecided',
};

export function loadDashboardConfig(): DashboardConfig {
  if (typeof window === 'undefined') return defaultDashboardConfig;
  const raw = window.localStorage.getItem(CONFIG_KEY);
  if (!raw) return defaultDashboardConfig;
  try {
    return { ...defaultDashboardConfig, ...JSON.parse(raw) } as DashboardConfig;
  } catch {
    return defaultDashboardConfig;
  }
}

export function saveDashboardConfig(config: DashboardConfig) {
  if (typeof window === 'undefined') return;
  window.localStorage.setItem(CONFIG_KEY, JSON.stringify(config));
}

export function loadOperatorSecret() {
  if (typeof window === 'undefined') return '';
  return window.sessionStorage.getItem(OPERATOR_SECRET_KEY) || '';
}

export function saveOperatorSecret(secret: string) {
  if (typeof window === 'undefined') return;
  if (secret.trim()) window.sessionStorage.setItem(OPERATOR_SECRET_KEY, secret);
  else window.sessionStorage.removeItem(OPERATOR_SECRET_KEY);
}
```

- [ ] **Step 4: Replace duplicated constants in `Dashboard.tsx`**

Modify `web/src/routes/Dashboard.tsx` imports:

```ts
import { overlayRoutes, buildOverlaySourceUrl, getBrowserOrigin } from '../lib/overlayRoutes';
import { sampleAlert, sampleTicker, sampleGoals, sampleBRBShow, sampleBRBHide } from '../lib/dashboardSamples';
```

Remove local `overlayRoutes`, `sampleAlert`, `sampleTicker`, `sampleGoals`, `sampleBRBShow`, `sampleBRBHide`, `buildOverlaySourceUrl`, and `getBrowserOrigin` definitions.

- [ ] **Step 5: Run focused tests**

Run:

```bash
pnpm --dir web run test
```

Expected: all tests pass.

- [ ] **Step 6: Commit extraction**

```bash
git add web/src/lib/overlayRoutes.ts web/src/lib/dashboardSamples.ts web/src/lib/dashboardConfig.ts web/src/routes/Dashboard.tsx web/src/App.test.tsx
git commit -m "refactor: extract dashboard configuration helpers"
```

---

### Task 2: Split Dashboard Into Multi-Route Operator Console

**Files:**
- Create: `web/src/routes/dashboard/DashboardShell.tsx`
- Create: `web/src/routes/dashboard/Overview.tsx`
- Create: `web/src/routes/dashboard/Setup.tsx`
- Create: `web/src/routes/dashboard/Control.tsx`
- Create: `web/src/routes/dashboard/Config.tsx`
- Modify: `web/src/routes/Dashboard.tsx`
- Modify: `web/src/App.tsx`
- Test: `web/src/App.test.tsx`

- [ ] **Step 1: Add dashboard shell navigation**

Create `web/src/routes/dashboard/DashboardShell.tsx`:

```tsx
import type { ReactNode } from 'react';

const dashboardLinks = [
  ['/dashboard', 'Overview'],
  ['/dashboard/setup', 'Setup'],
  ['/dashboard/sources', 'Sources'],
  ['/dashboard/control', 'Stream control'],
  ['/dashboard/config', 'Config'],
] as const;

export function DashboardShell({ activePath, children }: { activePath: string; children: ReactNode }) {
  return (
    <main className="hud-shell hud-scanlines min-h-screen bg-[radial-gradient(circle_at_top_left,rgba(103,232,249,0.14),transparent_28%),radial-gradient(circle_at_top_right,rgba(244,114,182,0.12),transparent_34%),linear-gradient(135deg,#020617,#0b1020_54%,#020305)] px-6 py-8 text-zinc-100">
      <section className="mx-auto flex max-w-7xl flex-col gap-6">
        <header className="hud-panel hud-corners rounded-[1.8rem] p-5">
          <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
            <div>
              <p className="hud-label">over-laid command center</p>
              <h1 className="hud-title mt-3">Dashboard</h1>
            </div>
            <nav className="flex flex-wrap gap-2" aria-label="Dashboard sections">
              {dashboardLinks.map(([href, label]) => (
                <a key={href} className={`hud-chip ${activePath === href ? 'border-cyan-200/60 text-cyan-100' : ''}`} href={href}>{label}</a>
              ))}
            </nav>
          </div>
        </header>
        {children}
      </section>
    </main>
  );
}
```

- [ ] **Step 2: Add initial route components with real headings**

Create `web/src/routes/dashboard/Overview.tsx`:

```tsx
import { DashboardShell } from './DashboardShell';

export function DashboardOverviewPage() {
  return (
    <DashboardShell activePath="/dashboard">
      <section className="hud-panel hud-corners rounded-[1.6rem] p-5">
        <p className="hud-label">System status</p>
        <p className="hud-copy mt-2 text-sm">Compact overview of Twitch, OBS, API, setup, and action state.</p>
      </section>
    </DashboardShell>
  );
}
```

Create similarly named components for `Setup`, `Control`, and `Config` with headings `OBS setup workflow`, `Stream control`, and `Operator config`.

- [ ] **Step 3: Route dashboard paths in `App.tsx`**

Modify `web/src/App.tsx` imports:

```ts
import { DashboardOverviewPage } from './routes/dashboard/Overview';
import { DashboardSetupPage } from './routes/dashboard/Setup';
import { DashboardControlPage } from './routes/dashboard/Control';
import { DashboardConfigPage } from './routes/dashboard/Config';
```

Modify `getAppRoute()`:

```tsx
if (route === '/dashboard') return <DashboardOverviewPage />;
if (route === '/dashboard/setup' || route === '/dashboard/sources') return <DashboardSetupPage />;
if (route === '/dashboard/control') return <DashboardControlPage />;
if (route === '/dashboard/config') return <DashboardConfigPage />;
```

- [ ] **Step 4: Convert old `Dashboard.tsx` into a compatibility export**

Replace `web/src/routes/Dashboard.tsx` with:

```tsx
import { DashboardOverviewPage } from './dashboard/Overview';

export function DashboardPage() {
  return <DashboardOverviewPage />;
}
```

- [ ] **Step 5: Add route tests**

Modify `web/src/App.test.tsx`:

```ts
it('renders dashboard setup route', () => {
  const html = renderToString(<App pathname="/dashboard/setup" />);
  expect(html).toContain('OBS setup workflow');
});

it('renders dashboard control route', () => {
  const html = renderToString(<App pathname="/dashboard/control" />);
  expect(html).toContain('Stream control');
});

it('renders dashboard config route', () => {
  const html = renderToString(<App pathname="/dashboard/config" />);
  expect(html).toContain('Operator config');
});
```

- [ ] **Step 6: Run focused tests**

```bash
pnpm --dir web run test
```

Expected: all dashboard route tests pass.

- [ ] **Step 7: Commit route split skeleton**

```bash
git add web/src/App.tsx web/src/App.test.tsx web/src/routes/Dashboard.tsx web/src/routes/dashboard/
git commit -m "feat: split dashboard into operator routes"
```

---

### Task 3: Move Config and Remember Operator Secret State

**Files:**
- Modify: `web/src/routes/dashboard/Config.tsx`
- Modify: `web/src/lib/dashboardConfig.ts`
- Test: `web/src/App.test.tsx`

- [ ] **Step 1: Implement config route with session-backed operator secret**

Replace `DashboardConfigPage` content in `web/src/routes/dashboard/Config.tsx` with:

```tsx
import { useEffect, useState } from 'react';
import { loadDashboardConfig, loadOperatorSecret, saveDashboardConfig, saveOperatorSecret, type DashboardConfig } from '../../lib/dashboardConfig';
import { DashboardShell } from './DashboardShell';

export function DashboardConfigPage() {
  const [operatorSecret, setOperatorSecret] = useState('');
  const [config, setConfig] = useState<DashboardConfig>(() => loadDashboardConfig());

  useEffect(() => setOperatorSecret(loadOperatorSecret()), []);

  function updateSecret(value: string) {
    setOperatorSecret(value);
    saveOperatorSecret(value);
  }

  function setMprisDecision(mprisDecision: DashboardConfig['mprisDecision']) {
    const next = { ...config, mprisDecision };
    setConfig(next);
    saveDashboardConfig(next);
  }

  return (
    <DashboardShell activePath="/dashboard/config">
      <section className="hud-panel hud-corners rounded-[1.6rem] p-5">
        <p className="hud-label">Operator config</p>
        <h2 className="mt-3 text-2xl font-black text-white">Config</h2>
        <p className="hud-copy mt-2 text-sm">Operator secret is remembered for this browser session only.</p>
        <label className="mt-5 grid gap-2 text-sm text-zinc-300">
          <span className="hud-kicker">Operator secret</span>
          <input className="rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-fuchsia-300/30 focus:ring-4" type="password" value={operatorSecret} onChange={(event) => updateSecret(event.target.value)} placeholder="optional unless API requires it" />
        </label>
        <p className="mt-3 text-sm text-zinc-400">{operatorSecret ? 'Secret loaded for this session.' : 'No operator secret stored for this session.'}</p>

        <div className="mt-6 grid gap-3 md:grid-cols-3">
          {(['enabled', 'disabled', 'undecided'] as const).map((value) => (
            <button key={value} className="hud-panel hud-corners rounded-2xl p-4 text-left" type="button" onClick={() => setMprisDecision(value)}>
              <span className="hud-kicker">MPRIS</span>
              <span className="mt-2 block text-lg font-semibold capitalize text-white">{value}</span>
              {config.mprisDecision === value ? <span className="hud-badge mt-3 inline-flex">selected</span> : null}
            </button>
          ))}
        </div>
      </section>
    </DashboardShell>
  );
}
```

- [ ] **Step 2: Remove operator secret input from overview/control pages**

When later moving controls into `/dashboard/control`, call `loadOperatorSecret()` inside that route and show only a status chip:

```tsx
const operatorSecret = loadOperatorSecret();
const operatorSecretLabel = operatorSecret ? 'operator secret loaded' : 'operator secret missing';
```

- [ ] **Step 3: Test config route**

Add to `web/src/App.test.tsx`:

```ts
it('renders operator config route', () => {
  const html = renderToString(<App pathname="/dashboard/config" />);
  expect(html).toContain('Operator config');
  expect(html).toContain('MPRIS');
});
```

- [ ] **Step 4: Verify**

```bash
pnpm --dir web run test
pnpm --dir web run build
```

- [ ] **Step 5: Commit config route**

```bash
git add web/src/routes/dashboard/Config.tsx web/src/lib/dashboardConfig.ts web/src/App.test.tsx
git commit -m "feat: add dashboard config route"
```

---

### Task 4: Implement Setup Route With Collapsible Completed Workflow and Editable Presets

**Files:**
- Create: `web/src/routes/dashboard/components/SetupChecklist.tsx`
- Create: `web/src/routes/dashboard/components/OverlaySourceCards.tsx`
- Create: `web/src/routes/dashboard/components/StreamPresetEditor.tsx`
- Modify: `web/src/routes/dashboard/Setup.tsx`
- Modify: `web/src/lib/dashboardConfig.ts`
- Test: `web/src/App.test.tsx`

- [ ] **Step 1: Build checklist component that hides when complete**

Create `web/src/routes/dashboard/components/SetupChecklist.tsx`:

```tsx
import type { DashboardConfig, SetupChecklistItem } from '../../../lib/dashboardConfig';

export function SetupChecklist({ config, onChange }: { config: DashboardConfig; onChange: (config: DashboardConfig) => void }) {
  const allComplete = config.setupChecklist.every((item) => item.completed);
  const visibleItems = allComplete ? [] : config.setupChecklist;

  function toggle(id: string) {
    onChange({
      ...config,
      setupChecklist: config.setupChecklist.map((item) => (item.id === id ? { ...item, completed: !item.completed } : item)),
    });
  }

  return (
    <section className="hud-panel hud-corners rounded-[1.6rem] p-5">
      <p className="hud-label">OBS setup workflow</p>
      {allComplete ? <p className="mt-4 rounded-2xl border border-emerald-300/30 bg-emerald-300/10 px-4 py-3 text-emerald-100">Setup complete. All workflow items are green.</p> : null}
      <div className="mt-5 grid gap-3">
        {visibleItems.map((item: SetupChecklistItem) => (
          <label key={item.id} className="hud-panel hud-corners flex cursor-pointer gap-3 rounded-[1.2rem] p-4">
            <input type="checkbox" checked={item.completed} onChange={() => toggle(item.id)} />
            <span>
              <span className="block font-semibold text-white">{item.title}</span>
              <span className="mt-1 block text-sm text-zinc-300">{item.description}</span>
            </span>
          </label>
        ))}
      </div>
    </section>
  );
}
```

- [ ] **Step 2: Build overlay source cards component**

Move copyable URL card logic from old dashboard into `OverlaySourceCards.tsx` with props:

```ts
type OverlaySourceCardsProps = {
  apiBaseUrl: string;
  onCopy: (label: string, value: string) => void;
};
```

Use `overlayRoutes` and `buildOverlaySourceUrl()` from `web/src/lib/overlayRoutes.ts`.

- [ ] **Step 3: Build editable preset editor**

Create `StreamPresetEditor.tsx` with simple text fields: preset title and comma-separated sources.

Core update logic:

```tsx
function updatePreset(id: string, patch: Partial<StreamPreset>) {
  onChange({
    ...config,
    streamPresets: config.streamPresets.map((preset) => (preset.id === id ? { ...preset, ...patch } : preset)),
  });
}

function parseSources(value: string) {
  return value.split(',').map((source) => source.trim()).filter(Boolean);
}
```

- [ ] **Step 4: Compose setup route**

Modify `web/src/routes/dashboard/Setup.tsx`:

```tsx
import { useState } from 'react';
import { getOverlayApiBaseUrl } from '../../lib/eventStream';
import { loadDashboardConfig, saveDashboardConfig } from '../../lib/dashboardConfig';
import { DashboardShell } from './DashboardShell';
import { SetupChecklist } from './components/SetupChecklist';
import { OverlaySourceCards } from './components/OverlaySourceCards';
import { StreamPresetEditor } from './components/StreamPresetEditor';

export function DashboardSetupPage() {
  const apiBaseUrl = getOverlayApiBaseUrl();
  const [config, setConfig] = useState(() => loadDashboardConfig());
  const [copyStatus, setCopyStatus] = useState('');

  function updateConfig(next: typeof config) {
    setConfig(next);
    saveDashboardConfig(next);
  }

  function copy(label: string, value: string) {
    void navigator.clipboard?.writeText(value);
    setCopyStatus(`${label} copied`);
  }

  return (
    <DashboardShell activePath="/dashboard/setup">
      <SetupChecklist config={config} onChange={updateConfig} />
      <OverlaySourceCards apiBaseUrl={apiBaseUrl} onCopy={copy} />
      <StreamPresetEditor config={config} onChange={updateConfig} />
      <span className="sr-only" aria-live="polite">{copyStatus}</span>
    </DashboardShell>
  );
}
```

- [ ] **Step 5: Verify setup route tests**

```bash
pnpm --dir web run test
```

Expected: tests include `OBS setup workflow`, `Overlay source URLs`, and `Stream presets` on `/dashboard/setup`.

- [ ] **Step 6: Commit setup route**

```bash
git add web/src/routes/dashboard/Setup.tsx web/src/routes/dashboard/components/SetupChecklist.tsx web/src/routes/dashboard/components/OverlaySourceCards.tsx web/src/routes/dashboard/components/StreamPresetEditor.tsx web/src/lib/dashboardConfig.ts web/src/App.test.tsx
git commit -m "feat: add editable dashboard setup route"
```

---

### Task 5: Implement Stream Control Route and Quick Actions

**Files:**
- Create: `web/src/routes/dashboard/components/SystemStatusRail.tsx`
- Create: `web/src/routes/dashboard/components/BRBControl.tsx`
- Create: `web/src/routes/dashboard/components/OBSControls.tsx`
- Create: `web/src/routes/dashboard/components/SampleEventControls.tsx`
- Create: `web/src/routes/dashboard/components/QuickActionRunner.tsx`
- Create: `web/src/routes/dashboard/components/QuickActionsEditor.tsx`
- Modify: `web/src/routes/dashboard/Control.tsx`
- Modify: `web/src/lib/dashboardConfig.ts`
- Test: `web/src/App.test.tsx`

- [ ] **Step 1: Move BRB form into component**

Create `BRBControl.tsx` with props:

```ts
type BRBControlProps = {
  apiBaseUrl: string;
  operatorSecret: string;
  isRunning: boolean;
  runAction: (label: string, action: () => Promise<void>) => void;
};
```

Move the existing BRB state, `submitBRB`, `hideBRB`, and text-area parsing logic out of `Dashboard.tsx` into this component.

- [ ] **Step 2: Move OBS controls into component**

Create `OBSControls.tsx` with the existing scene/input/scene-item selection logic and props:

```ts
type OBSControlsProps = {
  apiBaseUrl: string;
  operatorSecret: string;
  isRunning: boolean;
  runAction: (label: string, action: () => Promise<void>) => void;
};
```

Move OBS fetching from dashboard into this component for now. Keep existing fallback behavior when inventories fail.

- [ ] **Step 3: Move sample buttons into component**

Create `SampleEventControls.tsx` using `sampleAlert`, `sampleTicker`, `sampleGoals`, `sampleBRBShow`, `sampleBRBHide`.

- [ ] **Step 4: Implement quick action runner**

Create `QuickActionRunner.tsx`:

```tsx
import { clearOverlays, sendTestAlert, setBRBStatus, setOBSInputMute, setOBSScene } from '../../../lib/api';
import type { QuickAction } from '../../../lib/dashboardConfig';

export function QuickActionRunner({ actions, apiBaseUrl, operatorSecret, runAction }: { actions: QuickAction[]; apiBaseUrl: string; operatorSecret: string; runAction: (label: string, action: () => Promise<void>) => void }) {
  function execute(action: QuickAction) {
    if (action.kind === 'setScene') return runAction(action.label, () => setOBSScene(action.sceneName, apiBaseUrl, operatorSecret));
    if (action.kind === 'sendAlert') return runAction(action.label, () => sendTestAlert({ title: action.title, message: action.message, severity: action.severity }, apiBaseUrl, operatorSecret));
    if (action.kind === 'showBRB') return runAction(action.label, () => setBRBStatus({ active: true, title: action.title, message: action.message, eta: action.eta, items: action.items }, apiBaseUrl, operatorSecret));
    if (action.kind === 'hideBRB') return runAction(action.label, () => setBRBStatus({ active: false, title: 'BRB overlay', message: 'Waiting for intermission command', eta: '', items: [] }, apiBaseUrl, operatorSecret));
    if (action.kind === 'clearOverlays') return runAction(action.label, () => clearOverlays(apiBaseUrl, operatorSecret));
    if (action.kind === 'muteInput') return runAction(action.label, () => setOBSInputMute(action.inputName, action.muted, apiBaseUrl, operatorSecret));
  }

  return (
    <section className="hud-panel hud-corners rounded-[1.6rem] p-5">
      <p className="hud-label">Quick actions</p>
      <div className="mt-4 grid gap-3 md:grid-cols-3">
        {actions.map((action) => <button key={action.id} className="hud-panel hud-corners rounded-2xl p-4 text-left" type="button" onClick={() => execute(action)}>{action.label}</button>)}
      </div>
    </section>
  );
}
```

- [ ] **Step 5: Implement minimal quick actions editor**

Create `QuickActionsEditor.tsx` with fields for label and kind. For the first pass, support editing label and scene name for `setScene`, input name for `muteInput`, and alert title/message for `sendAlert`. Add/remove buttons update localStorage config.

- [ ] **Step 6: Compose control route**

Modify `Control.tsx` to load operator secret from session storage, config from local storage, and render:

```tsx
<DashboardShell activePath="/dashboard/control">
  <SystemStatusRail />
  <QuickActionRunner actions={config.quickActions} apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} runAction={runAction} />
  <SampleEventControls apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} isRunning={isRunning} runAction={runAction} />
  <BRBControl apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} isRunning={isRunning} runAction={runAction} />
  <OBSControls apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} isRunning={isRunning} runAction={runAction} />
  <QuickActionsEditor config={config} onChange={updateConfig} />
</DashboardShell>
```

- [ ] **Step 7: Verify**

```bash
pnpm --dir web run test
pnpm --dir web run build
```

Expected: `/dashboard/control` renders `Stream control`, `Quick actions`, `BRB overlay`, and `Scene controls`.

- [ ] **Step 8: Commit control route**

```bash
git add web/src/routes/dashboard/Control.tsx web/src/routes/dashboard/components/BRBControl.tsx web/src/routes/dashboard/components/OBSControls.tsx web/src/routes/dashboard/components/SampleEventControls.tsx web/src/routes/dashboard/components/QuickActionRunner.tsx web/src/routes/dashboard/components/QuickActionsEditor.tsx web/src/routes/dashboard/components/SystemStatusRail.tsx web/src/lib/dashboardConfig.ts web/src/App.test.tsx
git commit -m "feat: add stream control route and quick actions"
```

---

### Task 6: Consolidate Semantic HUD Utilities

**Files:**
- Modify: `web/src/styles.css`
- Modify: dashboard components created above
- Test: `web/src/App.test.tsx`

- [ ] **Step 1: Add reusable semantic classes**

Append to `web/src/styles.css`:

```css
.hud-dashboard-grid {
  display: grid;
  gap: 1rem;
}

@media (min-width: 1024px) {
  .hud-dashboard-grid--two {
    grid-template-columns: minmax(0, 1.05fr) minmax(320px, 0.95fr);
  }

  .hud-dashboard-grid--three {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}

.hud-sticky-rail {
  position: sticky;
  top: 1rem;
  z-index: 20;
}

.hud-action-button {
  border: 1px solid var(--hud-line);
  border-radius: 1rem;
  background: rgba(255, 255, 255, 0.05);
  padding: 0.85rem 1rem;
  color: var(--hud-text);
  text-align: left;
  transition: background 160ms ease, border-color 160ms ease, transform 160ms ease;
}

.hud-action-button:hover {
  border-color: color-mix(in oklab, var(--hud-accent, var(--hud-cyan)) 50%, var(--hud-line));
  background: rgba(255, 255, 255, 0.1);
}

.hud-action-button:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}
```

- [ ] **Step 2: Replace repeated dashboard grid/button classes**

In new dashboard components, replace repeated class strings like:

```tsx
className="mt-5 grid gap-3 lg:grid-cols-3"
```

with:

```tsx
className="hud-dashboard-grid hud-dashboard-grid--three mt-5"
```

Replace repeated rounded action button classes with `hud-action-button` plus accent-specific classes only where needed.

- [ ] **Step 3: Add sticky compact system status rail**

In `SystemStatusRail.tsx`, use:

```tsx
<section className="hud-panel hud-corners hud-sticky-rail rounded-[1.35rem] p-3">
  <div className="grid gap-2 md:grid-cols-4">...</div>
</section>
```

- [ ] **Step 4: Verify visual utility compile**

```bash
pnpm --dir web run build
```

Expected: CSS builds without errors.

- [ ] **Step 5: Commit CSS utilities**

```bash
git add web/src/styles.css web/src/routes/dashboard/
git commit -m "style: consolidate dashboard hud utilities"
```

---

### Task 7: Documentation and Final Verification

**Files:**
- Modify: `README.md`
- Modify: `docs/overlaid-notes.md`

- [ ] **Step 1: Document new dashboard routes**

Add to README dashboard section:

```md
Dashboard routes:

- `/dashboard` — compact overview and status.
- `/dashboard/setup` — setup checklist, browser-source URLs, editable stream presets.
- `/dashboard/sources` — alias for setup/source URLs.
- `/dashboard/control` — stream controls, BRB, OBS controls, sample events, quick actions.
- `/dashboard/config` — operator secret session storage, MPRIS decision, local console preferences.
```

- [ ] **Step 2: Mark notes implementation status**

Append to `docs/overlaid-notes.md`:

```md
---

## Implementation status

- Theme/style: implemented via semantic HUD utility classes in `web/src/styles.css`.
- Dashboard split: implemented with `/dashboard`, `/dashboard/setup`, `/dashboard/sources`, `/dashboard/control`, and `/dashboard/config`.
- Setup workflow: persists checklist state locally and hides completed workflow behind completion state.
- Operator secret: moved to config route and stored in session storage.
- Overlay URLs/presets: moved to setup/source route with editable presets.
- Stream control route: includes samples, BRB, OBS controls, and editable quick actions.
```

- [ ] **Step 3: Full verification**

Run:

```bash
make verify
docker compose config --quiet
docker compose -f docker-compose.yml -f docker-compose.override.example.yml config --quiet
```

Expected: all commands pass.

- [ ] **Step 4: Rebuild web and smoke local routes**

Run:

```bash
docker compose up -d --build web
make smoke
```

Expected: `/dashboard`, `/overlay/alerts`, and `/healthz` respond.

- [ ] **Step 5: Commit docs**

```bash
git add README.md docs/overlaid-notes.md
git commit -m "docs: document dashboard operator routes"
```

---

## Final Self-Review Checklist

- Spec coverage: every bullet from `docs/overlaid-notes.md` has a task above.
- YAGNI check: no backend persistence or RBAC added unless browser-local storage proves insufficient.
- Safety check: all mutating actions still use `OPERATOR_SECRET` through existing API helpers.
- Regression check: existing overlay routes and OBS browser-source URLs remain unchanged.
- Maintainability check: `Dashboard.tsx` should shrink from ~900 lines to a small wrapper; new files should each have one responsibility.

## Recommended Execution Order

1. Task 1 extraction.
2. Task 2 route split skeleton.
3. Task 3 config route/operator secret session storage.
4. Task 4 setup route with checklist/source URLs/editable presets.
5. Task 5 stream control route and editable quick actions.
6. Task 6 CSS utility consolidation.
7. Task 7 docs/final verification.

Do not combine all tasks into one commit. Each task should be independently verified and committed.
