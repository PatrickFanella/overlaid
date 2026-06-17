# Stabilization Sprint Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Stabilize the recent Twitch/bot control-surface work with stronger QA checklists, route smoke coverage, docs consistency, and small operator UX hardening before adding more features.

**Architecture:** Treat this as a confidence sprint, not a feature sprint. Prefer lightweight tests and runbook updates that prove the shipped operator paths are discoverable, reversible, and documented. Do not add new Twitch capabilities, moderation actions, storage tables, or deployment architecture changes in this sprint.

**Tech Stack:** Go backend tests, React/Vite TypeScript dashboard, Playwright smoke tests, existing runbooks, `make verify`, `make smoke`, `make smoke-browser`.

---

## Current baseline

Recently shipped surfaces that need stabilization:

- `/dashboard/bot` bot ops: commands, timers, counters, quotes, API variables, moderation dry-run/actions, nuke dry-run/execute/radiation rollback, and audit.
- `/dashboard/config` Twitch interaction rules: aliases, reward rules, reward queue, fulfillment notes, reward create/delete, and reward pause/resume/enable/disable.
- `/dashboard/live` live cockpit: readiness, preflight rehearsal, session reports, recovery console, EventSub diagnostics, and event history.
- Existing automated browser smoke currently verifies route presence only: `web/e2e/smoke.e2e.ts`.

Out of scope for this stabilization sprint:

- Advanced Twitch reward fields.
- Cloud-hosted architecture changes.
- New bot feature breadth.
- Production deploy/push.

## Sprint closeout

- Docs QA checklists updated for bot ops, reward lifecycle, and live QA.
- Browser smoke covers route presence plus bot/reward safety surfaces.
- SSR tests lock key operator safety copy.
- Operator UI copy now emphasizes dry-run/capped nuke execution and delete-last reward lifecycle safety.
- Final verification passed: `make verify`, `make smoke`, and `make smoke-browser`.
- Accepted smoke environment note: `/readyz` reported degraded OBS checks because OBS WebSocket was not reachable locally; the smoke command still passed and the degradation is environmental.
- Deferred: advanced reward fields, cloud architecture, and broader productization/release notes.

---

## File structure

### QA and docs

- Modify `docs/runbooks/live-qa.md` — add explicit bot/reward queue live QA steps.
- Modify `docs/runbooks/visual-qa.md` — add visual checks for `/dashboard/bot` and reward lifecycle controls.
- Modify `docs/runbooks/bot-ops.md` — add a compact stabilization/live-QA checklist for bot features.
- Modify `docs/runbooks/twitch-interaction-rules.md` — add reward lifecycle QA evidence expectations.
- Modify `docs/superpowers/plans/2026-06-15-fossabot-parity-roadmap.md` only if status wording is discovered to be stale.

### Browser smoke and route shells

- Modify `web/e2e/smoke.e2e.ts` — split shallow route smoke into route presence plus operator-surface assertions.
- Modify `web/src/App.test.tsx` — add SSR copy checks for recent bot/reward safety language.

### Operator UX polish

- Modify `web/src/routes/dashboard/Bot.tsx` — small copy/status polish only; no new backend features.
- Modify `web/src/routes/dashboard/components/TwitchInteractionsManager.tsx` — small reward queue copy/layout polish only; no new Helix fields.

---

## Task 1: Update live QA and visual QA checklists

**Goal:** Make manual QA cover the bot/reward surfaces that changed most recently.

**Files:**

- Modify: `docs/runbooks/live-qa.md`
- Modify: `docs/runbooks/visual-qa.md`
- Modify: `docs/runbooks/bot-ops.md`
- Modify: `docs/runbooks/twitch-interaction-rules.md`

- [ ] **Step 1: Update live QA Twitch/EventSub phase**

In `docs/runbooks/live-qa.md`, replace Phase 4 steps 4-5 with:

```markdown
4. From a broadcaster/moderator account, send a safe chat command such as `!alert command smoke`.
5. Open `/dashboard/bot` and confirm bot status, recent audit, commands, timers, counters/quotes, API variables, moderation dry-run, and nuke planner panels render.
6. Open `/dashboard/config`, load Twitch rewards, and verify the reward queue shows manageable rewards without requiring deletion.
7. Trigger a configured channel-point test if available off-stream.
```

Then replace the Phase 4 expected evidence bullets with:

```markdown
- [ ] `/overlay/debug` shows incoming Twitch diagnostics.
- [ ] Chat command from broadcaster/moderator produces the expected safe overlay or chat event.
- [ ] Viewer/non-moderator commands are denied and recorded as diagnostics/audit events.
- [ ] `/dashboard/bot` shows recent audit entries for command/moderation checks without exposing tokens or raw EventSub payloads.
- [ ] Reward queue can load manageable rewards and shows lifecycle state, fulfillment notes, and reversible pause/disable controls before delete.
- [ ] EventSub reconnect attempts and last disconnect remain stable during the pass.
```

- [ ] **Step 2: Add `/dashboard/bot` visual QA section**

In `docs/runbooks/visual-qa.md`, add this section after `/dashboard/config`:

```markdown
### `/dashboard/bot`

- [ ] Bot status, audit, commands, timers, counters/quotes, API variables, moderation dry-run, nuke planner, and active radiation windows are visible without horizontal scrolling.
- [ ] Destructive actions such as manual moderation, nuke execute, radiation rollback, and delete controls require clear confirmation or explicit operator input.
- [ ] Dry-run results and audit entries are readable in dense cases and do not expose secrets, OAuth tokens, cookies, or raw EventSub payloads.
- [ ] Empty states explain what to configure next instead of looking broken.
```

- [ ] **Step 3: Expand `/dashboard/config` visual QA checks**

In `docs/runbooks/visual-qa.md`, under `/dashboard/config`, append:

```markdown
- [ ] Reward queue lifecycle controls show pause/resume and enable/disable before the delete confirmation controls.
- [ ] Fulfillment notes are visible for matched rewards/redemptions and read as operator-only guidance.
```

- [ ] **Step 4: Add bot ops stabilization checklist**

In `docs/runbooks/bot-ops.md`, replace the stale “Not implemented yet” section:

```markdown
Not implemented yet:

- Nuke-style multi-message workflows.
- Twitch reward creation/deletion through Helix management APIs.
```

with:

```markdown
Implemented in later FossaBot parity slices:

- Nuke-style dry-run, guarded delete/timeout execution, temporary radiation windows, and rollback.
- Twitch reward inventory, minimal creation/deletion, pause/resume, enable/disable, redemption fulfillment/canceling, and fulfillment-note visibility through the Twitch interaction rules page.

Still carried forward:

- Advanced Twitch reward fields such as cooldowns, per-stream/user limits, and other non-minimal Helix settings.
- Cloud-hosted uptime parity; see `docs/superpowers/plans/2026-06-16-fossabot-uptime-parity-options.md` before changing deployment architecture.
```

Then add this section near the top after current scope:

```markdown
## Stabilization QA checklist

- [ ] `/dashboard/bot` loads with bot status and recent audit.
- [ ] A safe custom command can be previewed without sending chat.
- [ ] A timer can be enabled/paused without losing its response text.
- [ ] Counter and quote panels show empty states or configured items clearly.
- [ ] API variable cards show cache/status/error state without exposing secret-looking keys.
- [ ] Moderation dry-run can test a sample message without enforcing.
- [ ] Nuke planner confirmation resets after changing plan fields.
- [ ] Active radiation windows show expiry and rollback controls only for nuke radiation rules.
```

- [ ] **Step 5: Add reward lifecycle QA evidence**

In `docs/runbooks/twitch-interaction-rules.md`, add this under `## Reward queue` before “Safety boundaries”:

```markdown
Stabilization QA evidence:

- [ ] Loading rewards defaults to manageable rewards.
- [ ] Selected reward context shows enabled/disabled, paused/not paused, and out-of-stock state when Twitch returns it.
- [ ] Pause/resume and enable/disable controls preserve the selected reward after refresh.
- [ ] Delete remains separated behind an operator confirmation checkbox.
- [ ] Fulfillment notes appear for matched selected rewards and matching redemptions, but do not trigger fulfillment automatically.
```

- [ ] **Step 6: Review docs diff and commit**

Run:

```bash
git diff -- docs/runbooks/live-qa.md docs/runbooks/visual-qa.md docs/runbooks/bot-ops.md docs/runbooks/twitch-interaction-rules.md
git diff --check
```

Expected: docs mention reversible reward lifecycle controls before delete, no claim that advanced reward fields are implemented.

Commit:

```bash
git add docs/runbooks/live-qa.md docs/runbooks/visual-qa.md docs/runbooks/bot-ops.md docs/runbooks/twitch-interaction-rules.md
git commit -m "docs: expand stabilization qa checklists"
```

---

## Task 2: Expand browser smoke for bot and reward surfaces

**Goal:** Make browser smoke catch missing critical operator panels and safety copy, not just route existence.

**Files:**

- Modify: `web/e2e/smoke.e2e.ts`

- [ ] **Step 1: Split route smoke into focused tests**

Replace `web/e2e/smoke.e2e.ts` with:

```ts
import { expect, test } from '@playwright/test';

test.describe('operator browser smoke', () => {
  test('loads critical dashboard and overlay routes', async ({ page }) => {
    await page.goto('/dashboard');
    await expect(page.getByText('over-laid command center')).toBeVisible();

    await page.goto('/dashboard/live');
    await expect(page.getByText('Live ops cockpit')).toBeVisible();
    await expect(page.getByText('Rehearsal audit')).toBeVisible();

    await page.goto('/dashboard/control');
    await expect(page.getByRole('heading', { name: 'Stream control', exact: true })).toBeVisible();
    await expect(page.getByText('Quick actions', { exact: true })).toBeVisible();

    await page.goto('/overlay/alerts');
    await expect(page.getByText('Alert feed')).toBeVisible();
  });

  test('shows bot operations safety surfaces', async ({ page }) => {
    await page.goto('/dashboard/bot');
    await expect(page.getByText('Bot operations')).toBeVisible();
    await expect(page.getByText('Bot commands', { exact: true })).toBeVisible();
    await expect(page.getByText('Bot counters and quotes', { exact: true })).toBeVisible();
    await expect(page.getByText('Bot API variables', { exact: true })).toBeVisible();
    await expect(page.getByText('Bot moderation dry-run', { exact: true })).toBeVisible();
    await expect(page.getByText('Nuke dry-run planner', { exact: true })).toBeVisible();
    await expect(page.getByText('I reviewed this dry-run plan')).toBeVisible();
    await expect(page.getByText('Bot timers', { exact: true })).toBeVisible();
    await expect(page.getByText('Bot audit', { exact: true })).toBeVisible();
  });

  test('shows twitch reward queue lifecycle controls', async ({ page }) => {
    await page.goto('/dashboard/config');
    await expect(page.getByText('OBS import')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Load from OBS' })).toBeVisible();
    await expect(page.getByText('Reward queue', { exact: true })).toBeVisible();
    await expect(page.getByText('Load manageable rewards, create simple rewards')).toBeVisible();
    await expect(page.getByText('Delete requires confirmation')).toBeVisible();
    await expect(page.getByText('Delete requires confirmation')).toBeVisible();
  });
});
```

- [ ] **Step 2: Run smoke and fix exact text only if needed**

Run:

```bash
docker compose up -d --build
make smoke-browser
```

Expected: three Playwright smoke tests pass. If a text selector fails because copy differs, update the selector to the exact current user-facing text; do not weaken assertions to generic CSS selectors.

- [ ] **Step 3: Remove generated Playwright artifact**

If generated, remove:

```bash
rm -f web/test-results/.last-run.json
```

- [ ] **Step 4: Commit browser smoke expansion**

```bash
git add web/e2e/smoke.e2e.ts
git commit -m "test: expand operator browser smoke"
```

---

## Task 3: Lock key safety copy in SSR tests

**Goal:** Prevent accidental removal of critical operator guidance from server-rendered route shells.

**Files:**

- Modify: `web/src/App.test.tsx`

- [ ] **Step 1: Add bot safety copy assertions**

In the existing dashboard bot route test in `web/src/App.test.tsx`, add assertions for nuke confirmation and radiation rollback copy. If the route test is not grouped separately, add this test near other App routing tests:

```ts
it('renders bot operations safety guidance', () => {
  const html = renderToString(<App pathname="/dashboard/bot" />);

  expect(html).toContain('Bot operations');
  expect(html).toContain('Nuke dry-run planner');
  expect(html).toContain('I reviewed this dry-run plan');
  expect(html).toContain('Radiation minutes');
  expect(html).toContain('Active radiation windows');
});
```

- [ ] **Step 2: Add reward queue safety copy assertions**

Add this test near existing Twitch interaction manager tests:

```ts
it('renders reward queue lifecycle safety guidance', () => {
  const html = renderToString(<TwitchInteractionsManager operatorSecret="secret" />);

  expect(html).toContain('Reward queue');
  expect(html).toContain('Load manageable rewards, create simple rewards');
  expect(html).toContain('Delete requires confirmation');
  expect(html).toContain('New reward title');
});
```

- [ ] **Step 3: Run frontend tests**

Run:

```bash
pnpm --dir web run test
```

Expected: all frontend tests pass.

- [ ] **Step 4: Commit SSR safety tests**

```bash
git add web/src/App.test.tsx
git commit -m "test: lock operator safety copy"
```

---

## Task 4: Small operator UX polish pass

**Goal:** Make dense operator panels easier to scan without changing behavior.

**Files:**

- Modify: `web/src/routes/dashboard/Bot.tsx`
- Modify: `web/src/routes/dashboard/components/TwitchInteractionsManager.tsx`

- [ ] **Step 1: Review current dense panels before editing**

Read the current bot nuke/moderation section and reward queue section:

```bash
git diff -- web/src/routes/dashboard/Bot.tsx web/src/routes/dashboard/components/TwitchInteractionsManager.tsx
```

Expected before editing: no pending diff from earlier tasks.

- [ ] **Step 2: Add a visible safety label to reward delete controls**

In `TwitchInteractionsManager.tsx`, ensure the delete controls block includes this copy directly above or beside the checkbox:

```tsx
<p className="text-xs text-amber-100">Delete is last resort. Pause or disable first when the reward should be reversible.</p>
```

- [ ] **Step 3: Add compact nuke safety copy if missing**

In `Bot.tsx`, ensure the nuke panel includes copy equivalent to:

```tsx
<p className="text-xs text-zinc-400">Dry-run first. Execute requires confirmation, is capped, and only delete/timeout are supported.</p>
```

If identical guidance already exists, do not duplicate it; leave the file unchanged for this step.

- [ ] **Step 4: Run frontend checks**

Run:

```bash
pnpm --dir web run format
pnpm --dir web run test
```

Expected: PASS.

- [ ] **Step 5: Commit UX polish if any files changed**

If files changed:

```bash
git add web/src/routes/dashboard/Bot.tsx web/src/routes/dashboard/components/TwitchInteractionsManager.tsx
git commit -m "fix: clarify operator safety controls"
```

If no files changed because current copy already satisfies the task, record that in the final sprint summary and do not make an empty commit.

---

## Task 5: Final stabilization verification and sprint closeout

**Goal:** End with a clean tree, passing checks, and an explicit note of deferred work.

**Files:**

- Modify: `docs/superpowers/plans/2026-06-16-stabilization-sprint.md`

- [ ] **Step 1: Run full verification**

Run:

```bash
make verify
docker compose up -d --build
make smoke
make smoke-browser
```

Expected: all pass. Remove generated browser artifacts after smoke:

```bash
rm -f web/test-results/.last-run.json
```

- [ ] **Step 2: Add sprint closeout status to this plan**

Append this section near the top of this file after “Current baseline” once tasks are complete:

```markdown
## Sprint closeout

- Docs QA checklists updated for bot ops, reward lifecycle, and live QA.
- Browser smoke covers route presence plus bot/reward safety surfaces.
- SSR tests lock key operator safety copy.
- Final verification: `make verify`, `make smoke`, and `make smoke-browser` passed.
- Deferred: advanced reward fields, cloud architecture, and broader productization/release notes.
```

- [ ] **Step 3: Commit closeout**

```bash
git add docs/superpowers/plans/2026-06-16-stabilization-sprint.md
git commit -m "docs: close stabilization sprint"
```

- [ ] **Step 4: Confirm clean working tree**

Run:

```bash
git status --short
```

Expected: no output.

---

## Recommended execution order

1. Task 1 — QA/runbook coverage.
2. Task 2 — browser smoke expansion.
3. Task 3 — SSR safety-copy tests.
4. Task 4 — small operator UX polish only if needed.
5. Task 5 — final verification and closeout.

## Verification policy

Every committed code/test slice must pass:

```bash
make verify
make smoke-browser
```

The final sprint closeout must also pass:

```bash
make smoke
```

Do not push or deploy unless explicitly requested.
