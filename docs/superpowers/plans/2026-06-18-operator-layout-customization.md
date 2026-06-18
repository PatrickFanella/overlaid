# Operator Layout Customization Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Add controlled FossaBot-style dashboard customization: operators can reorder, hide/show, and reset safe panels without moving destructive controls into unsafe places.

**Architecture:** Store layout preferences in the existing dashboard localStorage config (`web/src/lib/dashboardConfig.ts`) and expose a small reusable panel-layout Module for page-level composition. Start with `/dashboard/live`, then extend the same model to safe sections of `/dashboard/bot` and `/dashboard/config`. Safety-critical zones stay locked, visible, and visually guarded.

**Tech Stack:** Vite React TypeScript, existing HUD CSS primitives, Vitest SSR/unit tests, Playwright smoke tests, existing `make verify`/`make smoke` gates.

---

## Scope and safety boundaries

This sprint intentionally implements **controlled customization**, not a freeform design canvas.

Included:
- Reorder safe panels with explicit up/down controls.
- Hide/show safe panels.
- Persist layouts in localStorage through `DashboardConfig`.
- Reset a page layout to defaults.
- Add SSR/browser smoke coverage for the new controls.

Deferred:
- Free drag/drop anywhere.
- Overlapping widgets.
- Arbitrary pixel positioning.
- Resizable panels.
- Shared server-side operator profiles.
- Moving destructive controls such as nuke execution, reward deletion, stream-session lifecycle actions, or operator-secret controls.

Safety policy:
- **Safe panels** can be hidden/reordered.
- **Locked panels** are rendered in fixed safety lanes and cannot be hidden or moved.
- **Danger zones** stay visible, guarded, and styled with `hud-danger-zone`.

## Current panel map

### `/dashboard/live`

File: `web/src/routes/dashboard/Live.tsx`

Safe configurable panels:
- `ReadinessPanel`
- `LivePreflight`
- `EventSubDiagnostics`
- `BuildInfoPanel`
- `LiveChecklistSummary`
- `SessionLogPanel`
- `EventHistoryPanel`

Locked panels:
- `RecoveryConsolePanel` — recovery/runbook lane, deliberately stable.
- `StreamSessionPanel` — contains session lifecycle actions; keep fixed.

### `/dashboard/bot`

File: `web/src/routes/dashboard/Bot.tsx`

Safe configurable panel groups for this sprint:
- `BotCommandsPanel`
- `BotCommunityPanel`
- `BotAPIVariablesPanel`
- `BotTimersPanel`

Locked panels:
- `BotModerationPanel` and the `Nuke dry-run planner` danger zone.
- Existing status/audit summary at the top should remain visible.

### `/dashboard/config`

File: `web/src/routes/dashboard/Config.tsx`

Safe configurable panel groups for this sprint:
- `Shared dashboard config`
- `OBS import`
- `Overlay noise controls`
- `AutomationProfileManager`
- `MPRIS decision`

Locked panels:
- `Operator secret`
- `Webhook secret`
- `TwitchInteractionsManager` — contains reward lifecycle/delete controls; keep the whole Twitch interaction surface fixed for this sprint.

---

## Files to create or modify

Create:
- `web/src/routes/dashboard/components/CustomizablePanelLayout.tsx` — reusable page-local layout renderer and controls.
- `web/src/lib/dashboardLayout.ts` — pure helpers for default layouts, normalization, reordering, visibility, and reset.

Modify:
- `web/src/lib/dashboardConfig.ts` — add persisted `panelLayouts` to `DashboardConfig` and normalization.
- `web/src/lib/automationProfiles.ts` — preserve `panelLayouts` through shared dashboard config export/import serialization.
- `web/src/routes/dashboard/Live.tsx` — use the layout component for safe live panels while keeping locked panels fixed.
- `web/src/routes/dashboard/Bot.tsx` — use the layout component for safe bot panels while keeping moderation/nuke locked.
- `web/src/routes/dashboard/Config.tsx` — use the layout component for safe config panels while keeping secrets locked.
- `web/src/App.test.tsx` — add layout helper/storage tests and SSR copy tests.
- `web/e2e/smoke.e2e.ts` — assert layout controls render without performing destructive actions.
- `docs/runbooks/visual-qa.md` — add visual QA checklist for controlled customization.
- `docs/runbooks/live-qa.md` — add operator rehearsal evidence for reset/default layout.

Do not modify backend code in this sprint.

---

## Task 1: Add the panel layout model and pure helpers

**Files:**
- Create: `web/src/lib/dashboardLayout.ts`
- Modify: `web/src/lib/dashboardConfig.ts`
- Modify: `web/src/lib/automationProfiles.ts`
- Test: `web/src/App.test.tsx`

- [ ] **Step 1: Write failing helper/storage tests**

Add imports near the existing dashboard config tests in `web/src/App.test.tsx`:

```ts
import {
  defaultPanelLayouts,
  movePanelLayoutItem,
  normalizePanelLayouts,
  togglePanelLayoutItem,
} from './lib/dashboardLayout';
```

Add tests inside `describe('dashboard config loading', () => { ... })`:

```ts
  it('normalizes panel layouts by preserving locked defaults and merging safe persisted items', () => {
    const layouts = normalizePanelLayouts({
      live: [
        { id: 'event-history', visible: false, order: 1 },
        { id: 'unknown-panel', visible: false, order: 2 },
      ],
    });

    expect(layouts.live.find((item) => item.id === 'event-history')).toMatchObject({ visible: false });
    expect(layouts.live.some((item) => item.id === 'unknown-panel')).toBe(false);
    expect(layouts.live.map((item) => item.id)).toContain('readiness');
  });

  it('moves and toggles configurable panel layout items without dropping other panels', () => {
    const moved = movePanelLayoutItem(defaultPanelLayouts.live, 'event-history', 'up');
    const eventIndex = moved.findIndex((item) => item.id === 'event-history');
    const sessionLogIndex = moved.findIndex((item) => item.id === 'session-log');

    expect(eventIndex).toBeLessThan(sessionLogIndex);

    const toggled = togglePanelLayoutItem(moved, 'event-history');
    expect(toggled.find((item) => item.id === 'event-history')).toMatchObject({ visible: false });
    expect(toggled).toHaveLength(defaultPanelLayouts.live.length);
  });

  it('round-trips panel layouts through dashboard config localStorage', () => {
    const localStorage = new Map<string, string>();

    vi.stubGlobal('window', {
      localStorage: {
        getItem: (key: string) => localStorage.get(key) ?? null,
        setItem: (key: string, value: string) => localStorage.set(key, value),
        removeItem: (key: string) => localStorage.delete(key),
      },
      sessionStorage: {
        getItem: () => null,
        setItem: vi.fn(),
        removeItem: vi.fn(),
      },
    });

    const config = normalizeDashboardConfig({
      ...defaultDashboardConfig,
      panelLayouts: {
        ...defaultPanelLayouts,
        live: defaultPanelLayouts.live.map((item) => (item.id === 'event-history' ? { ...item, visible: false } : item)),
      },
    });
    saveDashboardConfig(config);

    expect(loadDashboardConfig().panelLayouts.live.find((item) => item.id === 'event-history')).toMatchObject({ visible: false });
  });

  it('preserves panel layouts through dashboard config export/import', () => {
    const config = normalizeDashboardConfig({
      ...defaultDashboardConfig,
      panelLayouts: {
        ...defaultPanelLayouts,
        live: defaultPanelLayouts.live.map((item) => (item.id === 'event-history' ? { ...item, visible: false } : item)),
      },
    });

    const imported = importDashboardConfig(exportDashboardConfig(config));

    expect(imported.panelLayouts.live.find((item) => item.id === 'event-history')).toMatchObject({ visible: false });
  });
```

Also import `exportDashboardConfig` and `importDashboardConfig` from `./lib/automationProfiles` in the test file.

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
pnpm --dir web run test -- App.test.tsx
```

Expected: FAIL because `dashboardLayout.ts`, `panelLayouts`, and helper exports do not exist yet.

- [ ] **Step 3: Implement `dashboardLayout.ts`**

Create `web/src/lib/dashboardLayout.ts`:

```ts
export type DashboardPanelPage = 'live' | 'bot' | 'config';

export type PanelLayoutItem = {
  id: string;
  title: string;
  visible: boolean;
  order: number;
};

export type PanelLayouts = Record<DashboardPanelPage, PanelLayoutItem[]>;

export const defaultPanelLayouts: PanelLayouts = {
  live: [
    { id: 'readiness', title: 'Readiness', visible: true, order: 1 },
    { id: 'preflight', title: 'Live preflight', visible: true, order: 2 },
    { id: 'eventsub', title: 'EventSub diagnostics', visible: true, order: 3 },
    { id: 'build-info', title: 'Build info', visible: true, order: 4 },
    { id: 'checklist', title: 'Checklist', visible: true, order: 5 },
    { id: 'session-log', title: 'Session log', visible: true, order: 6 },
    { id: 'event-history', title: 'Event history', visible: true, order: 7 },
  ],
  bot: [
    { id: 'commands', title: 'Commands', visible: true, order: 1 },
    { id: 'community', title: 'Community data', visible: true, order: 2 },
    { id: 'api-variables', title: 'API variables', visible: true, order: 3 },
    { id: 'timers', title: 'Timers', visible: true, order: 4 },
  ],
  config: [
    { id: 'dashboard-config', title: 'Shared dashboard config', visible: true, order: 1 },
    { id: 'obs-import', title: 'OBS import', visible: true, order: 2 },
    { id: 'overlay-noise', title: 'Overlay noise controls', visible: true, order: 3 },
    { id: 'automation-profiles', title: 'Automation profiles', visible: true, order: 4 },
    { id: 'mpris', title: 'MPRIS decision', visible: true, order: 5 },
  ],
};

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}

function normalizePageLayout(value: unknown, defaults: PanelLayoutItem[]) {
  const persisted = Array.isArray(value) ? value : [];
  const byId = new Map<string, Record<string, unknown>>();

  for (const item of persisted) {
    if (!isRecord(item) || typeof item.id !== 'string') continue;
    byId.set(item.id, item);
  }

  return defaults
    .map((fallback) => {
      const item = byId.get(fallback.id);
      return {
        ...fallback,
        visible: typeof item?.visible === 'boolean' ? item.visible : fallback.visible,
        order: typeof item?.order === 'number' && Number.isFinite(item.order) ? item.order : fallback.order,
      };
    })
    .sort((left, right) => left.order - right.order)
    .map((item, index) => ({ ...item, order: index + 1 }));
}

export function normalizePanelLayouts(value: unknown): PanelLayouts {
  const input = isRecord(value) ? value : {};
  return {
    live: normalizePageLayout(input.live, defaultPanelLayouts.live),
    bot: normalizePageLayout(input.bot, defaultPanelLayouts.bot),
    config: normalizePageLayout(input.config, defaultPanelLayouts.config),
  };
}

export function visiblePanelLayoutItems(items: PanelLayoutItem[]) {
  return items.filter((item) => item.visible).sort((left, right) => left.order - right.order);
}

export function movePanelLayoutItem(items: PanelLayoutItem[], id: string, direction: 'up' | 'down') {
  const ordered = [...items].sort((left, right) => left.order - right.order);
  const index = ordered.findIndex((item) => item.id === id);
  const targetIndex = direction === 'up' ? index - 1 : index + 1;
  if (index < 0 || targetIndex < 0 || targetIndex >= ordered.length) return ordered;

  const next = [...ordered];
  [next[index], next[targetIndex]] = [next[targetIndex], next[index]];
  return next.map((item, itemIndex) => ({ ...item, order: itemIndex + 1 }));
}

export function togglePanelLayoutItem(items: PanelLayoutItem[], id: string) {
  return items.map((item) => (item.id === id ? { ...item, visible: !item.visible } : item));
}
```

- [ ] **Step 4: Add `panelLayouts` to dashboard config**

Modify `web/src/lib/dashboardConfig.ts`:

```ts
import { defaultPanelLayouts, normalizePanelLayouts, type PanelLayouts } from './dashboardLayout';
```

Add to `DashboardConfig`:

```ts
  panelLayouts: PanelLayouts;
```

Add to `defaultDashboardConfig`:

```ts
  panelLayouts: defaultPanelLayouts,
```

Add to `normalizeDashboardConfig` return object:

```ts
    panelLayouts: normalizePanelLayouts(parsed.panelLayouts),
```

Modify `web/src/lib/automationProfiles.ts` so `serializeDashboardConfig(config: DashboardConfig): DashboardConfig` includes normalized copied layouts:

```ts
    panelLayouts: normalizePanelLayouts(config.panelLayouts),
```

Import `normalizePanelLayouts` from `./dashboardLayout` at the top of that file.

- [ ] **Step 5: Run tests to verify pass**

Run:

```bash
pnpm --dir web run format
pnpm --dir web run test -- App.test.tsx
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/dashboardLayout.ts web/src/lib/dashboardConfig.ts web/src/lib/automationProfiles.ts web/src/App.test.tsx
git commit -m "feat: add dashboard panel layout model"
```

---

## Task 2: Add reusable controlled panel layout UI

**Files:**
- Create: `web/src/routes/dashboard/components/CustomizablePanelLayout.tsx`
- Modify: `web/src/App.test.tsx`

- [ ] **Step 1: Add SSR copy test for layout controls**

In `web/src/App.test.tsx`, update the `/dashboard/live` SSR test to assert:

```ts
expect(html).toContain('Customize panel layout');
expect(html).toContain('Reset layout');
expect(html).toContain('Locked safety panels stay fixed');
```

This will fail until Task 3 wires the component into `/dashboard/live`. If executing Task 2 separately, add a temporary component-level SSR test by importing `renderToString` and `CustomizablePanelLayout` after creating it; otherwise bundle Task 2 and Task 3 in one commit.

- [ ] **Step 2: Create `CustomizablePanelLayout`**

Create `web/src/routes/dashboard/components/CustomizablePanelLayout.tsx`:

```tsx
import type { ReactNode } from 'react';
import {
  defaultPanelLayouts,
  movePanelLayoutItem,
  togglePanelLayoutItem,
  visiblePanelLayoutItems,
  type DashboardPanelPage,
  type PanelLayoutItem,
} from '../../../lib/dashboardLayout';

type PanelDefinition = {
  id: string;
  node: ReactNode;
};

type CustomizablePanelLayoutProps = {
  page: DashboardPanelPage;
  items: PanelLayoutItem[];
  panels: PanelDefinition[];
  onChange: (items: PanelLayoutItem[]) => void;
  title?: string;
  description?: string;
};

export function CustomizablePanelLayout({
  page,
  items,
  panels,
  onChange,
  title = 'Customize panel layout',
  description = 'Reorder or hide safe panels. Locked safety panels stay fixed.',
}: CustomizablePanelLayoutProps) {
  const panelMap = new Map(panels.map((panel) => [panel.id, panel.node]));
  const visibleItems = visiblePanelLayoutItems(items).filter((item) => panelMap.has(item.id));

  return (
    <section className="grid gap-3">
      <div className="hud-panel hud-panel--flat hud-corners border border-fuchsia-400/30 p-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <p className="hud-section-title text-fuchsia-200">Workspace layout</p>
            <h2 className="text-lg font-semibold text-zinc-100">{title}</h2>
            <p className="mt-1 max-w-2xl text-sm text-zinc-400">{description}</p>
          </div>
          <button className="hud-button hud-button--small" type="button" onClick={() => onChange(defaultPanelLayouts[page])}>
            Reset layout
          </button>
        </div>
        <div className="mt-3 grid gap-2 md:grid-cols-2">
          {items.map((item, index) => (
            <div className="hud-compact-card flex items-center justify-between gap-2" key={item.id}>
              <div>
                <p className="text-sm font-semibold text-zinc-100">{item.title}</p>
                <p className="text-xs uppercase tracking-[0.18em] text-zinc-500">{item.visible ? 'visible' : 'hidden'}</p>
              </div>
              <div className="flex flex-wrap justify-end gap-1">
                <button aria-label={`Move ${item.title} up`} className="hud-button hud-button--small" type="button" disabled={index === 0} onClick={() => onChange(movePanelLayoutItem(items, item.id, 'up'))}>
                  Up
                </button>
                <button aria-label={`Move ${item.title} down`} className="hud-button hud-button--small" type="button" disabled={index === items.length - 1} onClick={() => onChange(movePanelLayoutItem(items, item.id, 'down'))}>
                  Down
                </button>
                <button aria-label={`${item.visible ? 'Hide' : 'Show'} ${item.title}`} className="hud-button hud-button--small" type="button" onClick={() => onChange(togglePanelLayoutItem(items, item.id))}>
                  {item.visible ? 'Hide' : 'Show'}
                </button>
              </div>
            </div>
          ))}
        </div>
      </div>
      {visibleItems.map((item) => (
        <div key={item.id}>{panelMap.get(item.id)}</div>
      ))}
    </section>
  );
}
```

- [ ] **Step 3: Run format/type check**

Run:

```bash
pnpm --dir web run format
```

Expected: PASS.

- [ ] **Step 4: Do not commit alone if SSR test is red**

If Step 1 was added before Task 3, wait to commit until `/dashboard/live` renders the component.

---

## Task 3: Wire controlled customization into `/dashboard/live`

**Files:**
- Modify: `web/src/routes/dashboard/Live.tsx`
- Modify: `web/src/App.test.tsx`
- Modify: `web/e2e/smoke.e2e.ts`

- [ ] **Step 1: Add Live layout state and persistence**

In `web/src/routes/dashboard/Live.tsx`, import:

```ts
import { saveDashboardConfig } from '../../lib/dashboardConfig';
import type { PanelLayoutItem } from '../../lib/dashboardLayout';
import { CustomizablePanelLayout } from './components/CustomizablePanelLayout';
```

Change config loading from a plain const to state:

```tsx
  const [config, setConfig] = useState(() => loadDashboardConfig());
```

Add handler inside `DashboardLivePage`:

```tsx
  function updateLivePanelLayout(items: PanelLayoutItem[]) {
    setConfig((current) => {
      const next = { ...current, panelLayouts: { ...current.panelLayouts, live: items } };
      saveDashboardConfig(next);
      return next;
    });
  }
```

- [ ] **Step 2: Replace only safe Live panels with layout component**

Keep `RecoveryConsolePanel` and `StreamSessionPanel` outside the configurable layout.

Replace the current two-column grid body with:

```tsx
      <div className="hud-dashboard-grid hud-dashboard-grid--two">
        <div className="grid gap-3">
          <RecoveryConsolePanel />
          <CustomizablePanelLayout
            page="live"
            items={config.panelLayouts.live}
            onChange={updateLivePanelLayout}
            panels={[
              { id: 'readiness', node: <ReadinessPanel report={status.readiness} loading={status.loading} error={status.readinessError} /> },
              { id: 'preflight', node: <LivePreflight twitch={status.twitch} obs={status.obs} config={config} operatorSecret={operatorSecret} /> },
              { id: 'eventsub', node: <EventSubDiagnostics status={status.twitch} /> },
              { id: 'build-info', node: <BuildInfoPanel apiBaseUrl={apiBaseUrl} /> },
              { id: 'checklist', node: <LiveChecklistSummary items={config.setupChecklist} /> },
              { id: 'session-log', node: <SessionLogPanel entries={sessionLog} /> },
              { id: 'event-history', node: <EventHistoryPanel events={status.history?.events ?? []} loading={status.loading} error={status.historyError} /> },
            ]}
          />
        </div>
        <div className="grid gap-3 content-start">
          <StreamSessionPanel apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} status={status.session} loading={status.loading} error={status.sessionError} onChange={(session) => setStatus((current) => ({ ...current, session }))} />
        </div>
      </div>
```

Remove the standalone `<EventHistoryPanel ... />` below the grid because it is now configurable.

- [ ] **Step 3: Update smoke test with non-destructive layout assertions**

In `web/e2e/smoke.e2e.ts`, in the live route test, assert:

```ts
await expect(page.getByText('Customize panel layout')).toBeVisible();
await expect(page.getByRole('button', { name: 'Reset layout' })).toBeVisible();
await expect(page.getByText('Locked safety panels stay fixed')).toBeVisible();
```

Do not click hide/show/reorder in smoke; keep smoke non-mutating.

- [ ] **Step 4: Run verification**

Run:

```bash
pnpm --dir web run format
pnpm --dir web run test
```

Expected: PASS.

- [ ] **Step 5: Commit Tasks 2-3 together if needed**

```bash
git add web/src/routes/dashboard/components/CustomizablePanelLayout.tsx web/src/routes/dashboard/Live.tsx web/src/App.test.tsx web/e2e/smoke.e2e.ts
git commit -m "feat: customize live dashboard panels"
```

---

## Task 4: Extend controlled customization to safe Bot panels

**Files:**
- Modify: `web/src/routes/dashboard/Bot.tsx`
- Modify: `web/src/App.test.tsx`

- [ ] **Step 1: Add Bot SSR assertions**

In the `/dashboard/bot` SSR test in `web/src/App.test.tsx`, add:

```ts
expect(html).toContain('Customize panel layout');
expect(html).toContain('Moderation guardrails stay locked');
```

- [ ] **Step 2: Import layout helpers**

In `web/src/routes/dashboard/Bot.tsx`, import `loadDashboardConfig`, `saveDashboardConfig`, `PanelLayoutItem`, and `CustomizablePanelLayout` following the Live pattern.

- [ ] **Step 3: Add config state and bot layout update handler**

Where Bot currently loads dashboard config, make sure it uses state:

```tsx
const [config, setConfig] = useState(() => loadDashboardConfig());
```

Add:

```tsx
function updateBotPanelLayout(items: PanelLayoutItem[]) {
  setConfig((current) => {
    const next = { ...current, panelLayouts: { ...current.panelLayouts, bot: items } };
    saveDashboardConfig(next);
    return next;
  });
}
```

- [ ] **Step 4: Wrap only safe bot panels**

Replace the rendered stack for `BotCommandsPanel`, `BotCommunityPanel`, `BotAPIVariablesPanel`, and `BotTimersPanel` with:

```tsx
<CustomizablePanelLayout
  page="bot"
  items={config.panelLayouts.bot}
  onChange={updateBotPanelLayout}
  description="Reorder or hide safe bot panels. Moderation guardrails stay locked."
  panels={[
    {
      id: 'commands',
      node: (
        <BotCommandsPanel
          draft={commandDraft}
          commands={state.commands}
          saving={savingCommand}
          onDraftChange={setCommandDraft}
          onSave={handleSaveCommand}
          onToggle={handleToggleCommand}
          onDelete={handleDeleteCommand}
        />
      ),
    },
    {
      id: 'community',
      node: (
        <BotCommunityPanel
          counters={state.counters}
          quotes={state.quotes}
          counterDraft={counterDraft}
          quoteDraft={quoteDraft}
          onCounterDraftChange={setCounterDraft}
          onQuoteDraftChange={setQuoteDraft}
          onSaveCounter={handleSaveCounter}
          onDeleteCounter={handleDeleteCounter}
          onSaveQuote={handleSaveQuote}
          onDeleteQuote={handleDeleteQuote}
        />
      ),
    },
    {
      id: 'api-variables',
      node: (
        <BotAPIVariablesPanel
          variables={state.apiVariables}
          draft={apiVariableDraft}
          onDraftChange={setAPIVariableDraft}
          onSave={handleSaveAPIVariable}
          onDelete={handleDeleteAPIVariable}
        />
      ),
    },
    {
      id: 'timers',
      node: (
        <BotTimersPanel
          draft={draft}
          timers={state.timers}
          saving={savingTimer}
          onDraftChange={setDraft}
          onSave={handleSaveTimer}
          onToggle={handleToggleTimer}
          onDelete={handleDeleteTimer}
        />
      ),
    },
  ]}
/>
```

Keep `BotModerationPanel`, the nuke danger zone, status summary, and audit visible outside this layout. Preserve every existing prop exactly.

- [ ] **Step 5: Verify and commit**

Run:

```bash
pnpm --dir web run format
pnpm --dir web run test
```

Commit:

```bash
git add web/src/routes/dashboard/Bot.tsx web/src/App.test.tsx
git commit -m "feat: customize safe bot panels"
```

---

## Task 5: Extend controlled customization to safe Config panels

**Files:**
- Modify: `web/src/routes/dashboard/Config.tsx`
- Modify: `web/src/App.test.tsx`
- Modify: `web/e2e/smoke.e2e.ts`

- [ ] **Step 1: Add Config SSR assertions**

In the `/dashboard/config` SSR test in `web/src/App.test.tsx`, add:

```ts
expect(html).toContain('Customize panel layout');
expect(html).toContain('Secret controls stay locked');
expect(html).toContain('Delete requires confirmation');
```

- [ ] **Step 2: Add config layout state/handler**

In `Config.tsx`, import `saveDashboardConfig`, `PanelLayoutItem`, and `CustomizablePanelLayout`, then add:

```tsx
function updateConfigPanelLayout(items: PanelLayoutItem[]) {
  setConfig((current) => {
    const next = { ...current, panelLayouts: { ...current.panelLayouts, config: items } };
    saveDashboardConfig(next);
    return next;
  });
}
```

- [ ] **Step 3: Keep secrets locked and wrap safe config panels**

Keep the existing operator/webhook secret section at `Config.tsx:132-164` outside the layout. The `Operator secret`, `Webhook secret`, and `Secret header` copy must remain before the customizable panel layout. Keep `TwitchInteractionsManager` outside the layout too because it contains reward lifecycle/delete controls.

Wrap the remaining safe sections by moving their existing inline JSX into a local array called `safeConfigPanels` before the `return`. Do not invent component names such as `SharedDashboardConfigSection`; use the current inline sections from `Config.tsx:166-251` as the `node` values.

Use this shape:

```tsx
const safeConfigPanels = [
  { id: 'dashboard-config', node: <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">...existing Shared dashboard config JSX...</section> },
  { id: 'obs-import', node: <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">...existing OBS import JSX...</section> },
  { id: 'overlay-noise', node: <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">...existing Overlay noise controls JSX...</section> },
  { id: 'automation-profiles', node: <AutomationProfileManager config={config} onChange={updateConfig} /> },
  { id: 'mpris', node: <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">...existing MPRIS decision JSX...</section> },
];

<CustomizablePanelLayout
  page="config"
  items={config.panelLayouts.config}
  onChange={updateConfigPanelLayout}
  description="Reorder or hide safe configuration panels. Secret controls stay locked."
  panels={safeConfigPanels}
/>
```

Render `<TwitchInteractionsManager apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} />` after the customizable config layout in a fixed locked position. Keep the visible `Delete requires confirmation` copy and confirmation behavior intact.

- [ ] **Step 4: Update smoke test**

In `web/e2e/smoke.e2e.ts`, on `/dashboard/config`, assert:

```ts
await expect(page.getByText('Secret controls stay locked')).toBeVisible();
```

- [ ] **Step 5: Verify and commit**

Run:

```bash
pnpm --dir web run format
pnpm --dir web run test
```

Commit:

```bash
git add web/src/routes/dashboard/Config.tsx web/src/App.test.tsx web/e2e/smoke.e2e.ts
git commit -m "feat: customize safe config panels"
```

---

## Task 6: Docs, browser smoke, and closeout

**Files:**
- Modify: `docs/runbooks/visual-qa.md`
- Modify: `docs/runbooks/live-qa.md`
- Modify: `docs/superpowers/plans/2026-06-18-operator-layout-customization.md`

- [ ] **Step 1: Update visual QA runbook**

Add a “Controlled workspace customization” subsection to `docs/runbooks/visual-qa.md`:

```markdown
### Controlled workspace customization

- `/dashboard/live` shows `Customize panel layout`, `Reset layout`, and copy that locked safety panels stay fixed.
- Reorder/hide controls affect only safe panels; recovery and stream-session controls remain fixed.
- `/dashboard/bot` keeps moderation/nuke guardrails visible outside customizable bot panels.
- `/dashboard/config` keeps operator secret and webhook secret controls locked outside customizable config panels.
- Reset restores default order and visibility.
- Keyboard focus remains visible on Up, Down, Hide/Show, and Reset buttons.
```

- [ ] **Step 2: Update live QA runbook**

Add evidence bullets to `docs/runbooks/live-qa.md`:

```markdown
- Capture the default `/dashboard/live` layout before rehearsal.
- If layout customization is used, reset to default before handoff or document the operator-specific order in evidence notes.
- Confirm locked safety panels remain visible: recovery links and stream session controls.
```

- [ ] **Step 3: Run full verification**

Run from repo root:

```bash
make verify
docker compose up -d --build
make smoke
make smoke-browser
```

Expected:
- `make verify` passes.
- `make smoke` passes. If `/readyz` is degraded only because local OBS/EventSub is unreachable in the same known local environment, record that note.
- `make smoke-browser` passes.

- [ ] **Step 4: Remove generated browser artifacts**

If Playwright writes `web/test-results`, remove it before committing:

```bash
rm -rf web/test-results
```

- [ ] **Step 5: Append sprint closeout**

Append to this plan:

```markdown
---

## Sprint closeout

- Added controlled panel customization for Live, Bot, and Config safe panels.
- Locked safety-critical panels remained fixed: recovery/session controls, bot moderation/nuke guardrails, and secret controls.
- Added dashboard config persistence and reset behavior.
- Verification run: `make verify`, `make smoke`, `make smoke-browser`.
- Deferred: free drag/drop, resizing, overlapping widgets, shared server-side operator profiles, and customization of destructive controls.
```

- [ ] **Step 6: Commit docs closeout**

```bash
git add docs/runbooks/visual-qa.md docs/runbooks/live-qa.md docs/superpowers/plans/2026-06-18-operator-layout-customization.md
git commit -m "docs: close operator layout customization sprint"
```

---

## Self-review checklist

- The sprint starts with controlled order/hide/reset, not free drag/drop.
- Live customization ships first because it has the clearest safe/locked panel split.
- Bot and Config explicitly keep destructive or secret-bearing controls locked.
- Layout state uses existing localStorage-backed `DashboardConfig`, avoiding backend schema churn.
- Verification uses repo-standard gates: `make verify`, `make smoke`, `make smoke-browser`.
