# Plan 4: OBS Automation Layer Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Let the operator run safe multi-step OBS/overlay workflows from one confirmed dashboard action.

**Architecture:** Keep automation browser-local and explicit. Add a `sequence` quick action variant that references existing atomic quick actions by ID, resolves those references at runtime, and executes non-sequence actions in order through the same protected API helpers used today. Do not add server-side workflow storage, arbitrary scripting, chat-triggered workflows, or a database.

**Tech Stack:** Vite React TypeScript dashboard, existing Go API/OBS protected routes, browser localStorage dashboard config, Vitest, Docker Compose.

---

## Current Baseline

- `web/src/lib/dashboardConfig.ts` owns local dashboard config and quick action definitions.
- `web/src/routes/dashboard/components/QuickActionRunner.tsx` executes quick actions through protected API helpers.
- `web/src/routes/dashboard/components/QuickActionsEditor.tsx` edits action labels, kinds, confirm flags, and payload fields.
- `web/src/routes/dashboard/Control.tsx` wraps actions with session-log status via `runAction`.
- Plan 2 added OBS source refresh and preset quick actions.
- Plan 3 added Twitch chat commands, but chat must not execute arbitrary dashboard workflows.

## Sprint Acceptance Criteria

- Add a new dashboard-only `sequence` quick action kind.
- A sequence stores ordered child quick action IDs and a `stopOnError` flag.
- Sequence execution runs existing atomic actions in order with existing API helpers and operator-secret protection.
- Missing IDs and nested/self sequence IDs are skipped by the resolver and surfaced to operators.
- `stopOnError: true` stops after the first failed child action and marks the parent failed.
- `stopOnError: false` runs remaining child actions and then marks the parent failed with all failed step labels.
- Child action `confirm` flags are not prompted inside a sequence; the parent sequence confirmation is the safety gate.
- Defaults include working `Go Live Main` and `Start BRB` sequence actions only after the runner can execute them.
- Existing persisted configs receive missing defaults by stable ID while preserving user edits/order.
- The editor can create/edit sequences with newline-separated action IDs and visible warnings.
- Tests cover resolver warnings, ordered execution, stop-on-error behavior, config migration, and route rendering.
- Runbook docs explain how to build and test sequences.
- `make verify`, compose config checks, rebuild, smoke, and route spot-checks pass.

## Do Not Do This Sprint

- Do not let Twitch chat run dashboard sequences.
- Do not add arbitrary JS, shell commands, user-authored scripts, or remote code execution.
- Do not store workflows on the backend.
- Do not add drag-and-drop editing.
- Do not add OBS scene-item auto-reconciliation by source ID; keep this sprint to explicit action chains.
- Do not change existing API auth boundaries.

---

## File Structure

Create:

- `web/src/lib/quickActionSequences.ts` — pure sequence resolver and async ordered runner.
- `docs/runbooks/obs-automation-sequences.md` — operator guide for safe sequence quick actions.

Modify:

- `web/src/lib/dashboardConfig.ts` — add `sequence` quick action type, defaults, migration support, and factory support.
- `web/src/routes/dashboard/Control.tsx` — allow action callbacks to return a success message for warnings.
- `web/src/routes/dashboard/components/QuickActionRunner.tsx` — execute sequence actions safely.
- `web/src/routes/dashboard/components/QuickActionsEditor.tsx` — edit sequence action IDs and warnings.
- `web/src/App.test.tsx` — add route/config/sequence tests.
- `README.md` — link the new OBS automation sequence runbook.
- `docs/superpowers/plans/2026-06-13-plan-4-obs-automation-layer.md` — mark completion when done.

---

### Task 1: Add Runnable Sequence Quick Actions

**Files:**
- Create: `web/src/lib/quickActionSequences.ts`
- Modify: `web/src/lib/dashboardConfig.ts`
- Modify: `web/src/routes/dashboard/Control.tsx`
- Modify: `web/src/routes/dashboard/components/QuickActionRunner.tsx`
- Modify: `web/src/App.test.tsx`

- [ ] **Step 1: Add failing tests**

Update imports in `web/src/App.test.tsx`:

```ts
import { resolveQuickActionSequence, runQuickActionSequence } from './lib/quickActionSequences';
```

In the dashboard control route test, add:

```ts
expect(html).toContain('Go Live Main');
expect(html).toContain('Start BRB');
expect(html).toContain('Sequences run listed actions in order');
```

Update the persisted quick action test expected IDs:

```ts
expect(config.quickActions.map((action) => action.id)).toEqual([
  'main-scene',
  'custom-hide',
  'hide-brb',
  'preset-main',
  'refresh-alerts',
  'brb-scene',
  'sample-alert',
  'show-brb',
  'sequence-go-live-main',
  'sequence-start-brb',
]);
```

Append this suite to `web/src/App.test.tsx`:

```ts
describe('quick action sequence utilities', () => {
  it('resolves atomic quick actions in sequence order', () => {
    const actions = [
      { id: 'a', label: 'Go Main', kind: 'setScene' as const, sceneName: 'Main' },
      { id: 'b', label: 'Refresh Alerts', kind: 'refreshSource' as const, inputName: 'Alerts Browser' },
      { id: 'seq', label: 'Go Live', kind: 'sequence' as const, actionIds: ['a', 'b'], stopOnError: true },
    ];

    const resolved = resolveQuickActionSequence(actions[2], actions);

    expect(resolved.steps.map((action) => action.id)).toEqual(['a', 'b']);
    expect(resolved.warnings).toEqual([]);
  });

  it('skips missing and nested sequence actions with warnings', () => {
    const actions = [
      { id: 'a', label: 'Go Main', kind: 'setScene' as const, sceneName: 'Main' },
      { id: 'nested', label: 'Nested', kind: 'sequence' as const, actionIds: ['a'], stopOnError: true },
      { id: 'seq', label: 'Go Live', kind: 'sequence' as const, actionIds: ['a', 'missing', 'nested', 'seq'], stopOnError: true },
    ];

    const resolved = resolveQuickActionSequence(actions[2], actions);

    expect(resolved.steps.map((action) => action.id)).toEqual(['a']);
    expect(resolved.warnings).toContain('Missing action: missing');
    expect(resolved.warnings).toContain('Nested sequence skipped: nested');
    expect(resolved.warnings).toContain('Nested sequence skipped: seq');
  });

  it('runs sequence child actions in order', async () => {
    const actions = [
      { id: 'a', label: 'Go Main', kind: 'setScene' as const, sceneName: 'Main' },
      { id: 'b', label: 'Refresh Alerts', kind: 'refreshSource' as const, inputName: 'Alerts Browser' },
      { id: 'seq', label: 'Go Live', kind: 'sequence' as const, actionIds: ['a', 'b'], stopOnError: true },
    ];
    const calls: string[] = [];
    const sequence = actions[2];
    if (sequence.kind !== 'sequence') throw new Error('expected sequence');

    const result = await runQuickActionSequence(sequence, actions, async (action) => {
      calls.push(action.id);
    });

    expect(calls).toEqual(['a', 'b']);
    expect(result).toEqual({ warnings: [], failures: [], ran: 2 });
  });

  it('stops after the first failed step when stopOnError is true', async () => {
    const actions = [
      { id: 'a', label: 'Go Main', kind: 'setScene' as const, sceneName: 'Main' },
      { id: 'b', label: 'Refresh Alerts', kind: 'refreshSource' as const, inputName: 'Alerts Browser' },
      { id: 'seq', label: 'Go Live', kind: 'sequence' as const, actionIds: ['a', 'b'], stopOnError: true },
    ];
    const calls: string[] = [];
    const sequence = actions[2];
    if (sequence.kind !== 'sequence') throw new Error('expected sequence');

    const result = await runQuickActionSequence(sequence, actions, async (action) => {
      calls.push(action.id);
      throw new Error('boom');
    });

    expect(calls).toEqual(['a']);
    expect(result.failures).toEqual(['Go Main: boom']);
  });

  it('continues after failed steps when stopOnError is false', async () => {
    const actions = [
      { id: 'a', label: 'Go Main', kind: 'setScene' as const, sceneName: 'Main' },
      { id: 'b', label: 'Refresh Alerts', kind: 'refreshSource' as const, inputName: 'Alerts Browser' },
      { id: 'seq', label: 'Go Live', kind: 'sequence' as const, actionIds: ['a', 'b'], stopOnError: false },
    ];
    const calls: string[] = [];
    const sequence = actions[2];
    if (sequence.kind !== 'sequence') throw new Error('expected sequence');

    const result = await runQuickActionSequence(sequence, actions, async (action) => {
      calls.push(action.id);
      throw new Error('boom');
    });

    expect(calls).toEqual(['a', 'b']);
    expect(result.failures).toEqual(['Go Main: boom', 'Refresh Alerts: boom']);
  });
});
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
pnpm --dir web run test -- --run src/App.test.tsx
```

Expected: FAIL because sequence types, defaults, resolver, and runner do not exist yet.

- [ ] **Step 3: Add sequence config support**

Modify `web/src/lib/dashboardConfig.ts`:

```ts
export type QuickAction =
  | { id: string; label: string; kind: 'sequence'; actionIds: string[]; stopOnError: boolean; confirm?: boolean }
  | { id: string; label: string; kind: 'setScene'; sceneName: string; confirm?: boolean }
  | { id: string; label: string; kind: 'runPreset'; presetId: string; sceneName: string; confirm?: boolean }
  | { id: string; label: string; kind: 'refreshSource'; inputName: string; confirm?: boolean }
  | { id: string; label: string; kind: 'sendAlert'; title: string; message: string; severity: 'info' | 'success' | 'warning' | 'error'; confirm?: boolean }
  | { id: string; label: string; kind: 'showBRB'; title: string; message: string; eta: string; items: string[]; confirm?: boolean }
  | { id: string; label: string; kind: 'hideBRB'; confirm?: boolean }
  | { id: string; label: string; kind: 'clearOverlays'; confirm?: boolean }
  | { id: string; label: string; kind: 'muteInput'; inputName: string; muted: boolean; confirm?: boolean };

export const quickActionKinds = ['sequence', 'setScene', 'sendAlert', 'showBRB', 'hideBRB', 'clearOverlays', 'muteInput', 'runPreset', 'refreshSource'] as const;
```

Append these defaults after `hide-brb` in `defaultDashboardConfig.quickActions`:

```ts
{ id: 'sequence-go-live-main', label: 'Go Live Main', kind: 'sequence', actionIds: ['main-scene', 'refresh-alerts', 'hide-brb'], stopOnError: true, confirm: true },
{ id: 'sequence-start-brb', label: 'Start BRB', kind: 'sequence', actionIds: ['brb-scene', 'show-brb'], stopOnError: true, confirm: true },
```

Add this `createQuickAction` case:

```ts
case 'sequence':
  return { id, label, kind, actionIds: ['main-scene', 'refresh-alerts'], stopOnError: true, confirm: true };
```

- [ ] **Step 4: Add sequence resolver/runner utility**

Create `web/src/lib/quickActionSequences.ts`:

```ts
import type { QuickAction } from './dashboardConfig';

export type AtomicQuickAction = Exclude<QuickAction, { kind: 'sequence' }>;
export type SequenceQuickAction = Extract<QuickAction, { kind: 'sequence' }>;

export type ResolvedQuickActionSequence = {
  steps: AtomicQuickAction[];
  warnings: string[];
};

export function resolveQuickActionSequence(sequence: QuickAction, actions: QuickAction[]): ResolvedQuickActionSequence {
  if (sequence.kind !== 'sequence') return { steps: [sequence], warnings: [] };

  const byId = new Map(actions.map((action) => [action.id, action]));
  const steps: AtomicQuickAction[] = [];
  const warnings: string[] = [];

  for (const actionId of sequence.actionIds) {
    const trimmedId = actionId.trim();
    if (!trimmedId) continue;

    const action = byId.get(trimmedId);
    if (!action) {
      warnings.push(`Missing action: ${trimmedId}`);
      continue;
    }
    if (action.kind === 'sequence') {
      warnings.push(`Nested sequence skipped: ${trimmedId}`);
      continue;
    }
    steps.push(action);
  }

  return { steps, warnings };
}

export async function runQuickActionSequence(
  sequence: SequenceQuickAction,
  actions: QuickAction[],
  executeAtomic: (action: AtomicQuickAction) => Promise<void>,
) {
  const resolved = resolveQuickActionSequence(sequence, actions);
  const failures: string[] = [];
  let ran = 0;

  for (const step of resolved.steps) {
    try {
      ran += 1;
      await executeAtomic(step);
    } catch (error) {
      failures.push(`${step.label}: ${error instanceof Error ? error.message : 'failed'}`);
      if (sequence.stopOnError) break;
    }
  }

  return { warnings: resolved.warnings, failures, ran };
}
```

- [ ] **Step 5: Wire session log success messages and sequence execution**

In `web/src/routes/dashboard/Control.tsx`, change `runAction` to accept success text:

```ts
  function runAction(label: string, action: () => Promise<string | void>) {
    setRunningAction(label);
    recordLog(label, 'running', 'Action started.');
    void action()
      .then((message) => {
        recordLog(label, 'success', message || 'Action completed.');
      })
      .catch((error) => {
        recordLog(label, 'error', error instanceof Error ? error.message : 'Action failed.');
      })
      .finally(() => {
        setRunningAction((current) => (current === label ? null : current));
      });
  }
```

In `web/src/routes/dashboard/components/QuickActionRunner.tsx`, update imports and `runAction` prop type:

```ts
import { runQuickActionSequence, type AtomicQuickAction } from '../../../lib/quickActionSequences';

runAction: (label: string, action: () => Promise<string | void>) => void;
```

Replace the action execution logic with atomic + sequence paths:

```ts
  function executeAtomic(action: AtomicQuickAction) {
    switch (action.kind) {
      case 'setScene':
        return setOBSScene(action.sceneName, apiBaseUrl, operatorSecret);
      case 'runPreset':
        return setOBSScene(action.sceneName, apiBaseUrl, operatorSecret);
      case 'refreshSource':
        return refreshOBSSource(action.inputName, apiBaseUrl, operatorSecret);
      case 'sendAlert':
        return sendTestAlert({ title: action.title, message: action.message, severity: action.severity }, apiBaseUrl, operatorSecret);
      case 'showBRB':
        return setBRBStatus({ active: true, title: action.title, message: action.message, eta: action.eta, items: action.items }, apiBaseUrl, operatorSecret);
      case 'hideBRB':
        return setBRBStatus({ active: false, title: 'BRB overlay', message: 'Waiting for intermission command', eta: '', items: [] }, apiBaseUrl, operatorSecret);
      case 'clearOverlays':
        return clearOverlays(apiBaseUrl, operatorSecret);
      case 'muteInput':
        return setOBSInputMute(action.inputName, action.muted, apiBaseUrl, operatorSecret);
    }
  }

  function execute(action: QuickAction) {
    if (action.kind !== 'sequence') {
      return runAction(action.label, () => executeAtomic(action));
    }

    return runAction(action.label, async () => {
      const result = await runQuickActionSequence(action, actions, executeAtomic);
      if (result.ran === 0) {
        throw new Error(['Sequence has no runnable actions', ...result.warnings].join('; '));
      }
      if (result.failures.length > 0) {
        throw new Error([...result.failures, ...result.warnings].join('; '));
      }
      if (result.warnings.length > 0) {
        return `Action completed with warnings: ${result.warnings.join('; ')}`;
      }
    });
  }
```

Make the runner explanatory copy render even when the widget is compact:

```tsx
<p className="hud-copy mt-1 text-xs text-zinc-300">Stored in browser local storage and executed through protected API helpers. Sequences run listed actions in order, skip nested sequences, and write warnings to the session log.</p>
```

- [ ] **Step 6: Run tests, build, and commit**

Run:

```bash
pnpm --dir web run test -- --run src/App.test.tsx
pnpm --dir web run build
make verify
```

Expected: PASS.

Commit:

```bash
git add web/src/lib/dashboardConfig.ts web/src/lib/quickActionSequences.ts web/src/routes/dashboard/Control.tsx web/src/routes/dashboard/components/QuickActionRunner.tsx web/src/App.test.tsx
git commit -m "feat: add obs automation sequences"
```

---

### Task 2: Add Sequence Editor Controls

**Files:**
- Modify: `web/src/routes/dashboard/components/QuickActionsEditor.tsx`
- Modify: `web/src/App.test.tsx`

- [ ] **Step 1: Add failing editor assertions**

In the dashboard control route test in `web/src/App.test.tsx`, add:

```ts
expect(html).toContain('Action IDs');
expect(html).toContain('Stop sequence on first failed step');
```

- [ ] **Step 2: Run test to verify failure**

Run:

```bash
pnpm --dir web run test -- --run src/App.test.tsx
```

Expected: FAIL because the editor does not render sequence fields yet.

- [ ] **Step 3: Add editor sequence fields and warnings**

Update imports in `web/src/routes/dashboard/components/QuickActionsEditor.tsx`:

```ts
import { resolveQuickActionSequence } from '../../../lib/quickActionSequences';
```

Add this branch inside the payload field grid before the `setScene` branch:

```tsx
{action.kind === 'sequence' ? (
  <>
    <label className="grid gap-2 text-sm text-zinc-300 lg:col-span-2">
      <span className="hud-kicker">Action IDs</span>
      <textarea
        className="min-h-24 rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-cyan-300/30 focus:ring-4"
        value={action.actionIds.join('\n')}
        onChange={(event) => updateAction(index, { actionIds: event.target.value.split('\n').map((value) => value.trim()).filter(Boolean) })}
      />
      <span className="text-xs text-zinc-400">One existing non-sequence quick action ID per line. Nested sequences are skipped.</span>
      {resolveQuickActionSequence(action, config.quickActions).warnings.length > 0 ? (
        <ul className="grid gap-1 text-xs text-amber-300">
          {resolveQuickActionSequence(action, config.quickActions).warnings.map((warning) => (
            <li key={warning}>{warning}</li>
          ))}
        </ul>
      ) : null}
    </label>
    <label className="flex items-center gap-3 rounded-2xl border border-white/10 bg-black/20 px-4 py-3 text-sm text-zinc-300">
      <input
        className="size-4 accent-cyan-300"
        type="checkbox"
        checked={action.stopOnError}
        onChange={(event) => updateAction(index, { stopOnError: event.target.checked })}
      />
      <span>Stop sequence on first failed step</span>
    </label>
  </>
) : null}
```

Add this case to `mergeAction` before `sendAlert`:

```ts
if (next.kind === 'sequence') {
  return {
    ...next,
    actionIds: next.actionIds?.length ? next.actionIds : ['main-scene', 'refresh-alerts'],
    stopOnError: next.stopOnError ?? true,
    confirm: next.confirm ?? true,
  };
}
```

- [ ] **Step 4: Run tests, build, and commit**

Run:

```bash
pnpm --dir web run test -- --run src/App.test.tsx
pnpm --dir web run build
make verify
```

Expected: PASS.

Commit:

```bash
git add web/src/routes/dashboard/components/QuickActionsEditor.tsx web/src/App.test.tsx
git commit -m "feat: add sequence quick action editor"
```

---

### Task 3: Add OBS Automation Runbook

**Files:**
- Create: `docs/runbooks/obs-automation-sequences.md`
- Modify: `README.md`

- [ ] **Step 1: Create the runbook**

Create `docs/runbooks/obs-automation-sequences.md`:

```markdown
# OBS Automation Sequences

Use sequence quick actions when one live operation should run several existing dashboard actions in a fixed order.

## Safety model

- Sequences are dashboard-only. Twitch chat commands cannot run sequences.
- Sequences reference existing quick action IDs; they do not contain scripts.
- Nested sequence actions are skipped.
- Mutating steps still use the protected API helpers and require the operator secret when configured.
- Child action confirmation prompts are not shown while a sequence runs; the parent sequence confirmation is the safety gate.
- Enable `require confirm` for sequences that change scenes, clear overlays, or show/hide BRB.

## Default sequences

- `Go Live Main` runs `main-scene`, `refresh-alerts`, then `hide-brb`.
- `Start BRB` runs `brb-scene`, then `show-brb`.

## Editing a sequence

1. Open `/dashboard/control`.
2. In **Editable quick actions**, set kind to `sequence`.
3. Enter one existing non-sequence quick action ID per line in **Action IDs**.
4. Keep **Stop sequence on first failed step** enabled for scene changes and BRB transitions.
5. Run the sequence once off-stream and verify the session log.

## Pre-stream check

1. Enter the operator secret on `/dashboard/config` or `/dashboard/control`.
2. Confirm `/dashboard/live` shows OBS reachable.
3. Run `Go Live Main`.
4. Confirm OBS is on the Main scene, alerts refresh cleanly, and BRB is hidden.
5. Run `Start BRB` and confirm the BRB scene and overlay state are correct.
6. Run `Go Live Main` again before going live.

## Troubleshooting

- If a step fails with `401`, re-enter the operator secret.
- If scene changes fail, verify OBS WebSocket status on `/dashboard/live`.
- If a source refresh fails, confirm the source input name matches OBS exactly.
- If a sequence does nothing, check that every listed action ID exists and is not another sequence.
```

- [ ] **Step 2: Link the runbook from README**

In `README.md`, add this line near the other live-operation runbooks:

```markdown
For dashboard OBS automation chains, use `docs/runbooks/obs-automation-sequences.md`.
```

- [ ] **Step 3: Run verification and commit**

Run:

```bash
make verify
```

Expected: PASS.

Commit:

```bash
git add docs/runbooks/obs-automation-sequences.md README.md
git commit -m "docs: add obs automation sequence runbook"
```

---

### Task 4: Final Integration, Deploy, and Plan Update

**Files:**
- Modify: `docs/superpowers/plans/2026-06-13-plan-4-obs-automation-layer.md`

- [ ] **Step 1: Run full local checks**

Run:

```bash
docker compose config --quiet
docker compose -f docker-compose.yml -f docker-compose.override.example.yml config --quiet
make verify
```

Expected: all pass.

- [ ] **Step 2: Rebuild and smoke-check**

Run:

```bash
docker compose up -d --build
make smoke
```

Expected: image rebuild succeeds and smoke checks pass.

- [ ] **Step 3: Spot-check dashboard routes**

Open or curl the deployed web origin routes:

```text
/dashboard/control
/dashboard/live
/overlay/alerts
```

Expected: routes load without SPA fallback errors. `/dashboard/control` contains `Go Live Main` and `Start BRB`.

- [ ] **Step 4: Update this plan completion status**

Add this block near the top of this plan after the header:

```markdown
## Completion status

Status: complete. Implemented, reviewed, verified, committed, rebuilt/deployed locally, smoke-checked, and route spot-checked.

Verification passed:

- `make verify`
- `docker compose config --quiet`
- `docker compose -f docker-compose.yml -f docker-compose.override.example.yml config --quiet`
- `docker compose up -d --build`
- `make smoke`
- Route spot-checks: `/dashboard/control`, `/dashboard/live`, `/overlay/alerts`

Live QA result: pending off-stream OBS sequence rehearsal.

---
```

Check every task checkbox that was completed.

- [ ] **Step 5: Commit and push**

Run the required pre-commit inspection first:

```bash
git status --short
git diff --stat
git log --oneline -10
```

Then commit and push:

```bash
git add docs/superpowers/plans/2026-06-13-plan-4-obs-automation-layer.md
git commit -m "docs: mark plan 4 complete"
git push
```

Expected: `main` is synced to `origin/main` and working tree is clean.

---

## Self-Review

- Spec coverage: the tasks cover sequence typing, default config migration, editor controls, runner execution, docs, and deployment verification.
- Placeholder scan: no `TBD`, `TODO`, or unspecified implementation placeholders remain.
- Type consistency: the new quick action kind is always `sequence`, uses `actionIds: string[]`, and `stopOnError: boolean` across config, resolver, editor, runner, and tests.
