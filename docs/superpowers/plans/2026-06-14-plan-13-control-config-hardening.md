# Control and Config Hardening Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Harden the mutable dashboard control/config flows with pure helpers and focused regression tests, without adding browser automation or backend APIs.

**Architecture:** Keep the React UI behavior unchanged, but extract small pure helpers for confirmable actions, quick-action visibility, BRB payload construction, and Twitch interaction import normalization. Tests should exercise these helpers plus existing storage/API helper contracts in Node-safe Vitest. The dashboard remains browser-local for config and session secrets.

**Tech Stack:** Vite React TypeScript, Vitest, existing dashboard localStorage/sessionStorage helpers, existing protected API client helpers.

---

## Status

Complete.

## Completion Notes

- Added pure helpers and tests for confirmable actions, quick-action visibility, sequence result formatting, BRB payloads, storage contracts, and Twitch interaction rule normalization.
- Updated `docs/runbooks/visual-qa.md` with control/config interaction checks tailored to the current dashboard control and config flows.
- Verification passed: `make verify`, `docker compose config --quiet`, `docker compose -f docker-compose.yml -f docker-compose.override.example.yml config --quiet`, `docker compose up -d --build`, `make smoke`, and route spot checks for `/dashboard/control` and `/dashboard/config`.
- Live QA pending: off-stream operator click-through for confirmable actions, rehearsal logging, and Twitch interaction import/save.

## Acceptance Criteria

- Confirmable action behavior is covered without DOM/browser automation:
  - unconfirmed destructive actions arm on first click
  - armed buttons render `Confirm <label>`
  - second click disarms and runs
  - non-confirmed actions run immediately
- Quick-action visibility is covered by a pure helper:
  - atomic actions always render
  - runnable sequences render when children exist in the full catalog
  - no-op sequences remain hidden
- BRB payload construction is covered by tests:
  - blank title/message fall back to defaults
  - newline items are trimmed and empty lines removed
  - hide payload uses the known inactive/default BRB state
- Twitch interaction rule import normalization is covered without rendering the whole manager:
  - invalid JSON shape is rejected
  - unknown alias commands become `alert`
  - unknown reward actions become `ignore`
  - extra/nested fields do not survive import/export
- Browser storage contracts are covered:
  - dashboard config save/load round-trips through localStorage
  - operator secret save/load uses sessionStorage only
  - malformed dashboard config falls back to defaults
- `make verify` passes after every implementation task.
- Final deployment smoke still passes.

## Non-goals

- Do not add Playwright, jsdom, Testing Library, or screenshot automation.
- Do not change backend APIs.
- Do not change runtime dashboard UX except where needed to call extracted helpers.
- Do not add new persistence keys.
- Do not expose operator secrets in tests, exports, or docs.

## File Structure

- Create: `web/src/lib/controlActions.ts`
  - Pure helpers for confirmable-button state, visible quick actions, and sequence result messages.
- Create: `web/src/lib/brbPayloads.ts`
  - Pure helpers for parsing BRB item text and building show/hide payloads.
- Create: `web/src/lib/twitchInteractionRules.ts`
  - Pure frontend helpers/constants for normalizing imported Twitch interaction rules.
- Modify: `web/src/routes/dashboard/components/ConfirmableActionButton.tsx`
  - Use `controlActions` helpers without changing markup.
- Modify: `web/src/routes/dashboard/components/QuickActionRunner.tsx`
  - Use `getVisibleQuickActions` and `formatQuickActionRunResult` helpers.
- Modify: `web/src/routes/dashboard/components/BRBControl.tsx`
  - Use BRB payload helpers.
- Modify: `web/src/routes/dashboard/components/TwitchInteractionsManager.tsx`
  - Use shared interaction-rule normalization helpers.
- Modify: `web/src/App.test.tsx`
  - Add pure helper tests and storage/API contract tests.
- Modify: `docs/runbooks/visual-qa.md`
  - Add a short control/config interaction checklist.

---

### Task 1: Confirmable and quick-action helper coverage

**Files:**
- Create: `web/src/lib/controlActions.ts`
- Modify: `web/src/routes/dashboard/components/ConfirmableActionButton.tsx`
- Modify: `web/src/routes/dashboard/components/QuickActionRunner.tsx`
- Modify: `web/src/App.test.tsx`

- [x] **Step 1: Create pure control action helpers**

Create `web/src/lib/controlActions.ts`:

```ts
import type { QuickAction } from './dashboardConfig';
import { resolveQuickActionSequence } from './quickActionSequences';

type RunQuickActionSequenceResult = {
  warnings: string[];
  failures: string[];
  ran: number;
};

export function getConfirmableActionLabel(label: string, armed: boolean) {
  return armed ? `Confirm ${label}` : label;
}

export function nextConfirmableActionState({ confirm, armed }: { confirm?: boolean; armed: boolean }) {
  if (confirm && !armed) {
    return { armed: true, shouldRun: false };
  }

  return { armed: false, shouldRun: true };
}

export function getVisibleQuickActions(actions: QuickAction[], actionCatalog: QuickAction[] = actions) {
  return actions.filter((action) => action.kind !== 'sequence' || resolveQuickActionSequence(action, actionCatalog).steps.length > 0);
}

export function formatQuickActionRunResult(result: RunQuickActionSequenceResult) {
  if (result.ran === 0) {
    throw new Error(['Sequence has no runnable actions', ...result.warnings].join('; '));
  }

  if (result.failures.length > 0) {
    throw new Error([...result.failures, ...result.warnings].join('; '));
  }

  if (result.warnings.length > 0) {
    return `Action completed with warnings: ${result.warnings.join('; ')}`;
  }

  return undefined;
}
```

- [x] **Step 2: Update components to use helpers**

In `web/src/routes/dashboard/components/ConfirmableActionButton.tsx`, import:

```ts
import { getConfirmableActionLabel, nextConfirmableActionState } from '../../../lib/controlActions';
```

Replace the click body with:

```ts
const next = nextConfirmableActionState({ confirm, armed });
setArmed(next.armed);
if (next.shouldRun) onRun();
```

Replace button text with:

```tsx
{getConfirmableActionLabel(label, armed)}
```

In `web/src/routes/dashboard/components/QuickActionRunner.tsx`, import:

```ts
import { formatQuickActionRunResult, getVisibleQuickActions } from '../../../lib/controlActions';
```

Also replace the existing `quickActionSequences` import:

```ts
import { resolveQuickActionSequence, runQuickActionSequence, type AtomicQuickAction } from '../../../lib/quickActionSequences';
```

with:

```ts
import { runQuickActionSequence, type AtomicQuickAction } from '../../../lib/quickActionSequences';
```

Replace sequence result handling with:

```ts
const result = await runQuickActionSequence(action, sequenceCatalog, executeAtomic);
return formatQuickActionRunResult(result);
```

Replace visible action calculation with:

```ts
const visibleActions = getVisibleQuickActions(actions, sequenceCatalog);
```

- [x] **Step 3: Add focused tests**

In `web/src/App.test.tsx`, extend imports:

```ts
import {
  formatQuickActionRunResult,
  getConfirmableActionLabel,
  getVisibleQuickActions,
  nextConfirmableActionState,
} from './lib/controlActions';
```

Add tests under `describe('quick action sequence utilities', ...)`:

```ts
it('models confirmable action arming without DOM clicks', () => {
  expect(getConfirmableActionLabel('Clear overlays', false)).toBe('Clear overlays');
  expect(getConfirmableActionLabel('Clear overlays', true)).toBe('Confirm Clear overlays');
  expect(nextConfirmableActionState({ confirm: true, armed: false })).toEqual({ armed: true, shouldRun: false });
  expect(nextConfirmableActionState({ confirm: true, armed: true })).toEqual({ armed: false, shouldRun: true });
  expect(nextConfirmableActionState({ confirm: false, armed: false })).toEqual({ armed: false, shouldRun: true });
});

it('filters visible quick actions using the full catalog', () => {
  const actions: QuickAction[] = [
    { id: 'seq', label: 'Visible sequence', kind: 'sequence', actionIds: ['hidden-child'], stopOnError: true },
    { id: 'empty', label: 'Empty sequence', kind: 'sequence', actionIds: ['missing'], stopOnError: true },
    { id: 'alert', label: 'Sample alert', kind: 'sendAlert', title: 'A', message: 'B', severity: 'success' },
  ];
  const catalog: QuickAction[] = [{ id: 'hidden-child', label: 'Hidden child', kind: 'setScene', sceneName: 'Main' }, ...actions];

  expect(getVisibleQuickActions(actions, catalog).map((action) => action.id)).toEqual(['seq', 'alert']);
});

it('formats sequence run warnings and failures', () => {
  expect(formatQuickActionRunResult({ ran: 1, warnings: ['Missing action: old'], failures: [] })).toBe(
    'Action completed with warnings: Missing action: old',
  );
  expect(() => formatQuickActionRunResult({ ran: 0, warnings: ['Missing action: old'], failures: [] })).toThrow(
    'Sequence has no runnable actions; Missing action: old',
  );
  expect(() => formatQuickActionRunResult({ ran: 2, warnings: ['Missing action: old'], failures: ['Go Main: boom'] })).toThrow(
    'Go Main: boom; Missing action: old',
  );
});
```

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
git add web/src/lib/controlActions.ts web/src/routes/dashboard/components/ConfirmableActionButton.tsx web/src/routes/dashboard/components/QuickActionRunner.tsx web/src/App.test.tsx
git commit -m "test: harden quick action helpers"
```

---

### Task 2: BRB payload and storage contract coverage

**Files:**
- Create: `web/src/lib/brbPayloads.ts`
- Modify: `web/src/routes/dashboard/components/BRBControl.tsx`
- Modify: `web/src/App.test.tsx`

- [x] **Step 1: Create BRB payload helpers**

Create `web/src/lib/brbPayloads.ts`:

```ts
import { sampleBRBHide, sampleBRBShow } from './dashboardSamples';

export function parseBRBItems(value: string) {
  return value
    .split('\n')
    .map((item) => item.trim())
    .filter(Boolean);
}

export function buildShowBRBPayload({ title, message, eta, itemsText }: { title: string; message: string; eta: string; itemsText: string }) {
  return {
    active: true,
    title: title.trim() || sampleBRBShow.title,
    message: message.trim() || sampleBRBShow.message,
    eta: eta.trim(),
    items: parseBRBItems(itemsText),
  };
}

export function buildHideBRBPayload() {
  return { ...sampleBRBHide, active: false };
}
```

- [x] **Step 2: Update BRB control**

In `web/src/routes/dashboard/components/BRBControl.tsx`, remove the local `parseItems` helper and import:

```ts
import { buildHideBRBPayload, buildShowBRBPayload } from '../../../lib/brbPayloads';
```

Also replace the existing dashboard samples import:

```ts
import { sampleBRBHide, sampleBRBShow } from '../../../lib/dashboardSamples';
```

with:

```ts
import { sampleBRBShow } from '../../../lib/dashboardSamples';
```

Replace payload creation in `submitBRB` with:

```ts
const payload = nextActive ? buildShowBRBPayload({ title, message, eta, itemsText }) : buildHideBRBPayload();
```

- [x] **Step 3: Add storage and BRB tests**

In `web/src/App.test.tsx`, extend imports from `dashboardConfig`:

```ts
saveDashboardConfig,
saveOperatorSecret,
loadOperatorSecret,
```

Add import:

```ts
import { buildHideBRBPayload, buildShowBRBPayload, parseBRBItems } from './lib/brbPayloads';
```

Add tests under `describe('dashboard config loading', ...)`:

```ts
it('round-trips dashboard config through localStorage', () => {
  const storage = new Map<string, string>();
  vi.stubGlobal('window', {
    localStorage: {
      getItem: (key: string) => storage.get(key) ?? null,
      setItem: (key: string, value: string) => storage.set(key, value),
    },
  });

  const config = normalizeDashboardConfig({ ...defaultDashboardConfig, activeAutomationProfileId: 'main-live' });
  saveDashboardConfig(config);

  expect(loadDashboardConfig().activeAutomationProfileId).toBe('main-live');
});

it('stores operator secret in sessionStorage only', () => {
  const localStorageSet = vi.fn();
  const session = new Map<string, string>();
  vi.stubGlobal('window', {
    localStorage: { getItem: () => null, setItem: localStorageSet },
    sessionStorage: {
      getItem: (key: string) => session.get(key) ?? null,
      setItem: (key: string, value: string) => session.set(key, value),
    },
  });

  saveOperatorSecret('operator-secret');

  expect(loadOperatorSecret()).toBe('operator-secret');
  expect(localStorageSet).not.toHaveBeenCalled();
});

it('falls back to defaults for malformed dashboard config JSON', () => {
  vi.stubGlobal('window', { localStorage: { getItem: () => '{not-json' } });

  expect(loadDashboardConfig().activeAutomationProfileId).toBe(defaultDashboardConfig.activeAutomationProfileId);
});
```

Add tests under a new `describe('BRB payload helpers', ...)`:

```ts
it('builds trimmed BRB show payloads with fallbacks', () => {
  expect(parseBRBItems(' Hydrate \n\n Stretch \n')).toEqual(['Hydrate', 'Stretch']);
  expect(buildShowBRBPayload({ title: ' ', message: ' ', eta: ' 5 min ', itemsText: ' A \n B ' })).toMatchObject({
    active: true,
    title: 'Be right back',
    message: 'Short intermission. Stream resumes soon.',
    eta: '5 min',
    items: ['A', 'B'],
  });
});

it('builds the inactive BRB hide payload', () => {
  expect(buildHideBRBPayload()).toEqual({
    active: false,
    title: 'Be right back',
    message: 'Short intermission. Stream resumes soon.',
    eta: '5 min',
    items: ['Hydrate', 'Stretch', 'Chat stays live'],
    data: { preset: 'sample-hide' },
  });
});
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
git add web/src/lib/brbPayloads.ts web/src/routes/dashboard/components/BRBControl.tsx web/src/App.test.tsx
git commit -m "test: harden brb and storage helpers"
```

---

### Task 3: Twitch interaction import normalization coverage

**Files:**
- Create: `web/src/lib/twitchInteractionRules.ts`
- Modify: `web/src/routes/dashboard/components/TwitchInteractionsManager.tsx`
- Modify: `web/src/App.test.tsx`

- [x] **Step 1: Extract interaction-rule frontend helpers**

Create `web/src/lib/twitchInteractionRules.ts`:

```ts
import type { ChatAliasRule, ChannelPointRule, TwitchInteractionRules } from './api';

export const aliasCommands = ['alert', 'clear', 'brb', 'back'] as const;
export const rewardActions = ['alert', 'brb', 'clear', 'ignore'] as const;

export function createEmptyTwitchInteractionRules(): TwitchInteractionRules {
  return { chatAliases: [], channelPointRules: [] };
}

export function cloneTwitchInteractionRules(rules: TwitchInteractionRules): TwitchInteractionRules {
  return {
    chatAliases: rules.chatAliases.map((rule) => ({ ...rule })),
    channelPointRules: rules.channelPointRules.map((rule) => ({ ...rule })),
  };
}

function isRules(value: unknown): value is TwitchInteractionRules {
  if (typeof value !== 'object' || value === null) return false;
  const record = value as Record<string, unknown>;
  return Array.isArray(record.chatAliases) && Array.isArray(record.channelPointRules);
}

export function normalizeImportedTwitchInteractionRules(value: unknown): TwitchInteractionRules {
  if (!isRules(value)) {
    throw new Error('invalid rules JSON');
  }

  const chatAliases = value.chatAliases
    .filter((entry): entry is ChatAliasRule => typeof entry === 'object' && entry !== null && typeof entry.alias === 'string' && typeof entry.command === 'string' && typeof entry.enabled === 'boolean')
    .map((entry) => ({ alias: entry.alias, command: aliasCommands.includes(entry.command as (typeof aliasCommands)[number]) ? (entry.command as ChatAliasRule['command']) : 'alert', enabled: entry.enabled }));
  const channelPointRules = value.channelPointRules
    .filter(
      (entry): entry is ChannelPointRule =>
        typeof entry === 'object' &&
        entry !== null &&
        typeof entry.rewardTitle === 'string' &&
        typeof entry.action === 'string' &&
        typeof entry.enabled === 'boolean',
    )
    .map((entry) => ({
      rewardTitle: entry.rewardTitle,
      action: rewardActions.includes(entry.action as (typeof rewardActions)[number]) ? (entry.action as ChannelPointRule['action']) : 'ignore',
      title: typeof entry.title === 'string' ? entry.title : undefined,
      message: typeof entry.message === 'string' ? entry.message : undefined,
      enabled: entry.enabled,
    }));

  return { chatAliases, channelPointRules };
}

export function updateTwitchAliasRule(rules: TwitchInteractionRules, index: number, patch: Partial<ChatAliasRule>) {
  const next = cloneTwitchInteractionRules(rules);
  next.chatAliases[index] = { ...next.chatAliases[index], ...patch };
  return next;
}

export function updateTwitchRewardRule(rules: TwitchInteractionRules, index: number, patch: Partial<ChannelPointRule>) {
  const next = cloneTwitchInteractionRules(rules);
  next.channelPointRules[index] = { ...next.channelPointRules[index], ...patch };
  return next;
}
```

- [x] **Step 2: Update manager imports**

In `web/src/routes/dashboard/components/TwitchInteractionsManager.tsx`, remove local `aliasCommands`, `rewardActions`, `createEmptyRules`, `cloneRules`, `isRules`, `normalizeImportedRules`, `updateAlias`, and `updateReward` definitions.

Import:

```ts
import {
  aliasCommands,
  createEmptyTwitchInteractionRules,
  normalizeImportedTwitchInteractionRules,
  rewardActions,
  updateTwitchAliasRule,
  updateTwitchRewardRule,
} from '../../../lib/twitchInteractionRules';
```

Replace calls:
- `createEmptyRules()` → `createEmptyTwitchInteractionRules()`
- `normalizeImportedRules(parsed)` → `normalizeImportedTwitchInteractionRules(parsed)`
- `updateAlias(current, index, patch)` → `updateTwitchAliasRule(current, index, patch)`
- `updateReward(current, index, patch)` → `updateTwitchRewardRule(current, index, patch)`

- [x] **Step 3: Add normalization tests**

In `web/src/App.test.tsx`, import:

```ts
import { normalizeImportedTwitchInteractionRules, updateTwitchAliasRule, updateTwitchRewardRule } from './lib/twitchInteractionRules';
```

Add tests under a new `describe('twitch interaction rule helpers', ...)`:

```ts
it('normalizes imported Twitch interaction rules safely', () => {
  const rules = normalizeImportedTwitchInteractionRules({
    chatAliases: [{ alias: 'panic', command: 'unsafe', enabled: true, operatorSecret: 'hidden' }],
    channelPointRules: [{ rewardTitle: 'Hydrate', action: 'unsafe', title: 'T', message: 'M', enabled: true, nested: { token: 'hidden' } }],
  });

  expect(rules).toEqual({
    chatAliases: [{ alias: 'panic', command: 'alert', enabled: true }],
    channelPointRules: [{ rewardTitle: 'Hydrate', action: 'ignore', title: 'T', message: 'M', enabled: true }],
  });
  expect(JSON.stringify(rules)).not.toContain('operatorSecret');
  expect(JSON.stringify(rules)).not.toContain('nested');
});

it('rejects invalid Twitch interaction rule imports', () => {
  expect(() => normalizeImportedTwitchInteractionRules({ chatAliases: [] })).toThrow('invalid rules JSON');
});

it('updates Twitch interaction rules immutably', () => {
  const original = {
    chatAliases: [{ alias: 'panic', command: 'alert' as const, enabled: true }],
    channelPointRules: [{ rewardTitle: 'Hydrate', action: 'ignore' as const, enabled: true }],
  };

  expect(updateTwitchAliasRule(original, 0, { command: 'brb' }).chatAliases[0].command).toBe('brb');
  expect(updateTwitchRewardRule(original, 0, { action: 'alert' }).channelPointRules[0].action).toBe('alert');
  expect(original.chatAliases[0].command).toBe('alert');
  expect(original.channelPointRules[0].action).toBe('ignore');
});
```

- [x] **Step 4: Verify Task 3**

Run:

```bash
pnpm --dir web run test -- --run src/App.test.tsx
pnpm --dir web run build
make verify
```

Expected: all pass.

- [x] **Step 5: Commit Task 3**

```bash
git add web/src/lib/twitchInteractionRules.ts web/src/routes/dashboard/components/TwitchInteractionsManager.tsx web/src/App.test.tsx
git commit -m "test: harden twitch interaction rule helpers"
```

---

### Task 4: QA docs and final completion

**Files:**
- Modify: `docs/runbooks/visual-qa.md`
- Modify: `docs/superpowers/plans/2026-06-14-plan-13-control-config-hardening.md`

- [x] **Step 1: Update visual QA control/config checklist**

Add a subsection under dashboard QA in `docs/runbooks/visual-qa.md`:

```md
### Control/config interaction checks

- On `/dashboard/control`, confirm destructive quick actions show the `Confirm ...` armed state before running.
- Confirm quick-action sequences only show when at least one runnable child action is available.
- Run a rehearsal off-stream and confirm the session log records success/failure summaries.
- On `/dashboard/config`, confirm operator secret text remains session-only and is not present in dashboard config export JSON.
- Import a Twitch interaction rules JSON sample and confirm invalid actions are normalized before save.
```

- [x] **Step 2: Verify docs and full stack**

Run:

```bash
docker compose config --quiet
docker compose -f docker-compose.yml -f docker-compose.override.example.yml config --quiet
make verify
docker compose up -d --build
make smoke
curl --max-time 15 -fsS http://localhost:5173/dashboard/control >/dev/null
curl --max-time 15 -fsS http://localhost:5173/dashboard/config >/dev/null
```

Expected: orchestrator completes these stack checks. `/readyz` may still report `status: degraded` if Twitch EventSub is disconnected, but HTTP must succeed.

- [x] **Step 3: Mark plan complete**

Update this plan:

```md
## Status

Complete.

## Completion Notes

- Added pure helpers and tests for confirmable actions, quick-action visibility, sequence result formatting, BRB payloads, storage contracts, and Twitch interaction rule normalization.
- Updated visual QA control/config interaction checks.
- Verification passed: `make verify`, compose config checks, Docker rebuild, `make smoke`, and dashboard control/config route spot checks.
- Live QA pending: off-stream operator click-through of confirmable actions, rehearsal logging, and Twitch interaction import/save.
```

Check every task box in this file.

- [x] **Step 4: Commit docs and completion**

```bash
git add docs/runbooks/visual-qa.md docs/superpowers/plans/2026-06-14-plan-13-control-config-hardening.md
git commit -m "docs: mark plan 13 complete"
```

---

## Review Checklist

- [x] No backend/API files changed.
- [x] No browser automation dependencies added.
- [x] No operator secrets added to localStorage/export JSON/docs.
- [x] Mutable behavior tests are pure helper tests or SSR-safe tests.
- [x] `make verify` passes.
- [x] Deploy smoke passes.
