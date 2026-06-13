# Plan 7: Automation Profiles Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Add browser-local automation profiles so the operator can switch between named show modes that expose only the relevant quick actions and stream presets.

**Architecture:** Keep profiles inside the existing `DashboardConfig` localStorage document. Add pure profile helpers for migration/filtering/import/export, then wire a Config-page profile manager and use the active profile to filter control quick actions and source presets. No backend, database, or shared sync is needed this sprint.

**Tech Stack:** Vite React TypeScript, existing dashboard localStorage config, existing quick-action/sequence utilities, React SSR route tests in Vitest, README/runbook markdown.

---

## Completion status

Status: complete. Implemented, reviewed, verified, committed, rebuilt/deployed locally, smoke-checked, and route spot-checked.

Verification passed:

- `make verify`
- `docker compose config --quiet`
- `docker compose -f docker-compose.yml -f docker-compose.override.example.yml config --quiet`
- `docker compose up -d --build`
- `make smoke`
- Route spot-checks: `/dashboard/config`, `/dashboard/control`, `/dashboard/sources`

Live QA result: profile switching/import-export browser interaction pending next operator pass.

---

## Sprint Acceptance Criteria

- `DashboardConfig` includes `automationProfiles` and `activeAutomationProfileId` with backwards-compatible loading of old localStorage configs.
- Default profiles include at least `Full console`, `Main live`, and `BRB ops`.
- `/dashboard/control` renders the active profile name and filters quick-action runner buttons to that profile, while keeping the quick-action editor available for all actions.
- `/dashboard/sources` renders the active profile name and filters the stream preset cards to that profile, while preserving custom CSS and overlay source cards.
- `/dashboard/config` lets the operator choose the active profile, edit profile action/preset IDs, and export/import the full dashboard config as JSON.
- Invalid/missing profile references do not crash the UI; helpers fall back to default/all lists and render short warnings where useful.
- Tests cover config migration, profile filtering, config route copy, and control/sources route copy.
- README and the OBS automation runbook document local profiles, active-profile filtering, and JSON backup/restore.
- `make verify` passes after each implementation task; final compose/smoke checks pass.

## Do Not Do This Sprint

- Do not add backend storage, authentication changes, or cross-device profile sync.
- Do not let profiles execute actions automatically. Profiles only filter/organize existing controls.
- Do not delete quick actions or presets when a profile stops referencing them.
- Do not include operator secret/sessionStorage content in JSON export.
- Do not change Twitch chat command behavior.

---

## File Structure

Create:

- `web/src/lib/automationProfiles.ts` — pure helpers for active profile lookup, quick-action filtering, stream-preset filtering, export/import serialization, and warnings.
- `web/src/routes/dashboard/components/AutomationProfileManager.tsx` — Config-page UI for selecting/editing profiles and JSON import/export.

Modify:

- `web/src/lib/dashboardConfig.ts` — add `AutomationProfile` type, defaults, migration merge, and config fields.
- `web/src/routes/dashboard/components/QuickActionRunner.tsx` — accept a full action catalog for sequence resolution while rendering only visible profile actions.
- `web/src/routes/dashboard/Config.tsx` — mount `AutomationProfileManager` and persist profile changes.
- `web/src/routes/dashboard/Control.tsx` — show active profile and pass filtered quick actions into `QuickActionRunner`.
- `web/src/routes/dashboard/Sources.tsx` — show active profile and pass filtered presets into `StreamPresetEditor`.
- `web/src/routes/dashboard/components/StreamPresetEditor.tsx` — accept optional preset override for filtered display without mutating hidden presets.
- `web/src/App.test.tsx` — add route, migration, helper, and import/export tests.
- `README.md` — describe automation profiles and JSON backup.
- `docs/runbooks/obs-automation-sequences.md` — explain profile-filtered sequences and safe backup/restore.
- `docs/superpowers/plans/2026-06-13-plan-7-automation-profiles.md` — mark completion when done.

---

### Task 1: Add Profile Data Model and Pure Helpers

**Files:**
- Modify: `web/src/lib/dashboardConfig.ts`
- Create: `web/src/lib/automationProfiles.ts`
- Test: `web/src/App.test.tsx`

- [x] **Step 1: Add profile types and defaults**

In `web/src/lib/dashboardConfig.ts`, add:

```ts
export type AutomationProfile = {
  id: string;
  title: string;
  description: string;
  quickActionIds: string[];
  streamPresetIds: string[];
};
```

Extend `DashboardConfig` with:

```ts
automationProfiles: AutomationProfile[];
activeAutomationProfileId: string;
```

Add default profiles to `defaultDashboardConfig`:

```ts
automationProfiles: [
  {
    id: 'full-console',
    title: 'Full console',
    description: 'Show every quick action and stream preset.',
    quickActionIds: ['*'],
    streamPresetIds: ['*'],
  },
  {
    id: 'main-live',
    title: 'Main live',
    description: 'Primary live scene controls and alert refresh flow.',
    quickActionIds: ['sequence-go-live-main', 'preset-main', 'refresh-alerts', 'sample-alert', 'hide-brb'],
    streamPresetIds: ['main', 'debug'],
  },
  {
    id: 'brb-ops',
    title: 'BRB ops',
    description: 'Intermission scene controls and BRB overlay state.',
    quickActionIds: ['sequence-start-brb', 'brb-scene', 'show-brb', 'hide-brb', 'refresh-alerts'],
    streamPresetIds: ['brb', 'debug'],
  },
],
activeAutomationProfileId: 'full-console',
```

- [x] **Step 2: Preserve defaults during config load**

Add a `mergeAutomationProfiles` helper mirroring `mergeQuickActions`: keep persisted profiles first by `id`, append missing defaults, and force `activeAutomationProfileId` to an existing profile or `full-console`.

- [x] **Step 3: Add pure profile helpers**

Create `web/src/lib/automationProfiles.ts` with exported helpers:

```ts
import type { AutomationProfile, DashboardConfig, QuickAction, StreamPreset } from './dashboardConfig';

export type AutomationProfileWarning = { profileId: string; message: string };

export function getActiveAutomationProfile(config: DashboardConfig): AutomationProfile;
export function selectQuickActionsForProfile(config: DashboardConfig): { actions: QuickAction[]; warnings: AutomationProfileWarning[] };
export function selectStreamPresetsForProfile(config: DashboardConfig): { presets: StreamPreset[]; warnings: AutomationProfileWarning[] };
export function exportDashboardConfig(config: DashboardConfig): string;
export function importDashboardConfig(raw: string): DashboardConfig;
```

Semantics:
- `['*']` means all actions/presets.
- Missing IDs are ignored and returned as warnings.
- If a profile selects zero valid actions/presets, fall back to all actions/presets with a warning.
- Import parses JSON then reuses the same normalization behavior as `loadDashboardConfig` without reading or writing storage.

Implementation detail: expose `normalizeDashboardConfig(parsed: Partial<DashboardConfig>): DashboardConfig` from `dashboardConfig.ts`. `loadDashboardConfig()` should parse localStorage then call this helper. `importDashboardConfig(raw)` should parse JSON and call the same helper. The normalizer should whitelist known `DashboardConfig` fields instead of preserving arbitrary imported keys.

- [x] **Step 4: Add tests**

In `web/src/App.test.tsx`, add tests that prove:
- old localStorage JSON without profiles receives the default profiles and `full-console` active ID.
- selecting `main-live` returns only configured quick actions in order.
- missing profile IDs produce warnings and fall back safely.
- export/import round-trips config without operator secret.

- [x] **Step 5: Verify and commit**

Run:

```bash
pnpm --dir web run test -- --run src/App.test.tsx
pnpm --dir web run build
make verify
```

Expected: all pass.

Commit:

```bash
git add web/src/lib/dashboardConfig.ts web/src/lib/automationProfiles.ts web/src/App.test.tsx
git commit -m "feat: add automation profile model"
```

---

### Task 2: Wire Profile Filtering Into Control and Sources

**Files:**
- Modify: `web/src/routes/dashboard/Control.tsx`
- Modify: `web/src/routes/dashboard/Sources.tsx`
- Modify: `web/src/routes/dashboard/components/QuickActionRunner.tsx`
- Modify: `web/src/routes/dashboard/components/StreamPresetEditor.tsx`
- Test: `web/src/App.test.tsx`

- [x] **Step 1: Teach quick-action runner about visible actions vs full catalog**

In `QuickActionRunner.tsx`, keep `actions` as the visible actions to render and add optional `actionCatalog?: QuickAction[]` for sequence resolution/execution:

```ts
type QuickActionRunnerProps = {
  actions: QuickAction[];
  actionCatalog?: QuickAction[];
  apiBaseUrl: string;
  operatorSecret: string;
  isRunning: boolean;
  compact?: boolean;
  runAction: (label: string, action: () => Promise<string | void>) => void;
};
```

Inside the component:

```ts
const catalog = actionCatalog ?? actions;
```

Use `catalog` for `runQuickActionSequence(action, catalog, executeAtomic)` and for `resolveQuickActionSequence(action, catalog)`. Continue rendering only `actions`. This prevents profile-filtered sequence buttons from losing hidden child actions like `main-scene`.

- [x] **Step 2: Filter control quick-action runner only**

In `Control.tsx`, import `getActiveAutomationProfile` and `selectQuickActionsForProfile`. Compute `activeProfile`, `profileActions`, and warnings. Render a compact `Active profile` panel/chip above the widget grid. Pass `profileActions` to `QuickActionRunner`. Keep `QuickActionsEditor` receiving the full `config` so hidden actions remain editable.

When rendering the runner, pass the full catalog too:

```tsx
<QuickActionRunner
  actions={profileActions}
  actionCatalog={config.quickActions}
  apiBaseUrl={apiBaseUrl}
  operatorSecret={operatorSecret}
  isRunning={runningAction !== null}
  runAction={runAction}
  compact={compact}
/>
```

- [x] **Step 3: Filter stream presets on Sources**

In `StreamPresetEditor.tsx`, add optional `presets?: StreamPreset[]`; render `presets ?? config.streamPresets`; update existing edits against full `config.streamPresets` by `id`.

In `Sources.tsx`, import `getActiveAutomationProfile` and `selectStreamPresetsForProfile`; render active profile copy and pass filtered presets to `StreamPresetEditor`.

- [x] **Step 4: Add route and runner tests**

Update SSR route tests:
- `/dashboard/control` contains `Active profile` and `Full console`.
- `/dashboard/sources` contains `Active profile` and `Full console`.

Add a component/helper test if needed to prove editor still receives all actions while runner gets filtered actions.

Add a direct `QuickActionRunner` SSR test proving a visible sequence still renders when its child action is only present in `actionCatalog`, not `actions`:

```tsx
const visibleActions: QuickAction[] = [{ id: 'seq', label: 'Go Live', kind: 'sequence', actionIds: ['hidden-child'], stopOnError: true }];
const actionCatalog: QuickAction[] = [...visibleActions, { id: 'hidden-child', label: 'Hidden Child', kind: 'setScene', sceneName: 'Main' }];
const html = renderToString(<QuickActionRunner actions={visibleActions} actionCatalog={actionCatalog} ... />);
expect(html).toContain('Go Live');
```

- [x] **Step 5: Verify and commit**

Run:

```bash
pnpm --dir web run test -- --run src/App.test.tsx
pnpm --dir web run build
make verify
```

Commit:

```bash
git add web/src/routes/dashboard/Control.tsx web/src/routes/dashboard/Sources.tsx web/src/routes/dashboard/components/QuickActionRunner.tsx web/src/routes/dashboard/components/StreamPresetEditor.tsx web/src/App.test.tsx
git commit -m "feat: apply automation profiles to dashboard"
```

---

### Task 3: Add Config-Page Profile Manager and Import/Export

**Files:**
- Create: `web/src/routes/dashboard/components/AutomationProfileManager.tsx`
- Modify: `web/src/routes/dashboard/Config.tsx`
- Test: `web/src/App.test.tsx`

- [x] **Step 1: Create profile manager component**

Create a component that receives `config` and `onChange`. It should render:
- active-profile selector.
- per-profile cards with editable `title`, `description`, newline-separated `quickActionIds`, and newline-separated `streamPresetIds`.
- warnings from `selectQuickActionsForProfile` / `selectStreamPresetsForProfile` for the active profile.
- export textarea containing `exportDashboardConfig(config)`.
- import textarea + button that calls `importDashboardConfig(raw)` and then `onChange(next)`.
- copy explaining export excludes session-only operator secret.

- [x] **Step 2: Mount on Config page**

In `Config.tsx`, import and render `AutomationProfileManager` after operator secret/webhook cards and before overlay noise controls. Use existing `setConfig(next); saveDashboardConfig(next)` pattern.

- [x] **Step 3: Add SSR route assertions**

Update `/dashboard/config` route test to contain:
- `Automation profiles`
- `Active profile`
- `Export dashboard config`
- `Import dashboard config`
- `operator secret is not exported`

- [x] **Step 4: Verify and commit**

Run:

```bash
pnpm --dir web run test -- --run src/App.test.tsx
pnpm --dir web run build
make verify
```

Commit:

```bash
git add web/src/routes/dashboard/components/AutomationProfileManager.tsx web/src/routes/dashboard/Config.tsx web/src/App.test.tsx
git commit -m "feat: add automation profile manager"
```

---

### Task 4: Document Automation Profiles

**Files:**
- Modify: `README.md`
- Modify: `docs/runbooks/obs-automation-sequences.md`

- [x] **Step 1: Update README**

Document that `/dashboard/config` manages local automation profiles/show modes, `/dashboard/control` and `/dashboard/sources` respect the active profile, and export/import backs up dashboard config only, not operator secret.

- [x] **Step 2: Update OBS automation runbook**

Add a section explaining recommended profiles (`Full console`, `Main live`, `BRB ops`), how profile filtering affects quick actions/sequences, and how to export JSON before changing a show setup.

- [x] **Step 3: Verify and commit**

Run:

```bash
make verify
```

Commit:

```bash
git add README.md docs/runbooks/obs-automation-sequences.md
git commit -m "docs: add automation profile runbook guidance"
```

---

### Task 5: Final Integration and Completion

**Files:**
- Modify: `docs/superpowers/plans/2026-06-13-plan-7-automation-profiles.md`

- [x] **Step 1: Run final checks**

Run:

```bash
docker compose config --quiet
docker compose -f docker-compose.yml -f docker-compose.override.example.yml config --quiet
make verify
docker compose up -d --build
make smoke
curl --max-time 15 -fsS http://localhost:5173/dashboard/config >/dev/null
curl --max-time 15 -fsS http://localhost:5173/dashboard/control >/dev/null
curl --max-time 15 -fsS http://localhost:5173/dashboard/sources >/dev/null
```

Expected: all pass. `/readyz` may report JSON `status: degraded`; smoke only requires HTTP success.

- [x] **Step 2: Mark plan complete**

Add a completion block near the top of this plan with the final verification commands and any live-QA follow-up. Check all task boxes.

- [x] **Step 3: Commit completion**

Run:

```bash
git add docs/superpowers/plans/2026-06-13-plan-7-automation-profiles.md
git commit -m "docs: mark plan 7 complete"
```

---

## Review Checklist

- [x] No task leaves the repo in a knowingly broken or failing intermediate state.
- [x] Existing saved configs without profiles load with default profiles.
- [x] Profile filtering never deletes hidden actions/presets.
- [x] Export JSON contains `DashboardConfig` only and does not include session-only operator secret.
- [x] `make verify` passes after every implementation commit.
