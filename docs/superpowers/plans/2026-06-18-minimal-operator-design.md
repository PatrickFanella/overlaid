# Minimal Operator Design Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Add a very minimal dashboard design style while preserving controlled panel customization and locked safety-critical operator controls.

**Architecture:** Keep existing React routes, state, localStorage config, and visible safety copy. Implement the style as a dashboard-scoped CSS/token migration first, then adapt shell/primitives without renaming product concepts or changing API behavior. Existing `hud-*` class names stay as compatibility aliases, but minimal overrides must be scoped under `.hud-shell--dashboard` so overlay browser-source routes keep their current broadcast HUD look.

**Tech Stack:** Vite React TypeScript, Tailwind utility classes, project CSS in `web/src/styles.css`, Vitest SSR tests in `web/src/App.test.tsx`, Playwright smoke in `web/e2e/smoke.e2e.ts`.

---

## Design direction: Quiet operator minimal

Use a restrained, near-monochrome operator interface:

- Neutral charcoal surfaces instead of neon terminal gradients.
- Thin borders and soft radius instead of hard neubrutal frames.
- One quiet accent for navigation and active state.
- Monospace only for logs, IDs, payloads, and terminal-like values.
- Sans-serif for dashboard chrome and controls.
- Safety states remain visually strong: danger, warning, success, disabled, and locked controls must stay clear.

Do **not** remove:

- `Customize panel layout`, `Reset layout`, and locked-panel copy.
- `Operate with guardrails`, `Nuke dry-run planner`, `I reviewed this dry-run plan`, `Radiation minutes`.
- `Delete requires confirmation`.
- Operator/webhook secret controls.
- Twitch login route affordance.

Do **not** restyle overlays in this sprint. `OverlayAlerts`, `OverlayChat`, ticker, goals, now-playing, BRB, and debug overlays must keep the current broadcast HUD/Dracula identity.

## File structure

- `web/src/styles.css` — minimal tokens plus dashboard-scoped compatibility styling for `hud-*` primitives.
- `web/src/routes/dashboard/DashboardShell.tsx` — minimal dashboard frame and grouped navigation.
- `web/src/routes/dashboard/components/ConsolePageHeader.tsx` — minimal page intro primitive.
- `web/src/routes/dashboard/components/ConsoleSection.tsx` — minimal section primitive and danger-zone support.
- `web/src/routes/dashboard/components/CustomizablePanelLayout.tsx` — quiet layout editor that keeps layout controls obvious.
- `web/src/App.test.tsx` — SSR checks for stable safety/customization copy.
- `web/e2e/smoke.e2e.ts` — route smoke locators if visible text/strict-mode behavior changes.
- `docs/runbooks/visual-qa.md` — add minimal-style visual QA checklist.

## Safety boundaries

- This is a **visual style sprint**, not a behavior sprint.
- Do not change dashboard config persistence, panel layout state, API calls, Twitch reward management, nuke execution, moderation, or operator-secret handling.
- Minimalism must not make danger zones look neutral.
- Destructive controls must remain visually stronger than ordinary panels.
- Avoid free drag/drop, resizing, overlapping widgets, and changing locked-panel rules.

---

### Task 1: Dashboard-scoped minimal CSS token foundation

**Files:**
- Modify: `web/src/styles.css`
- Test: `web/src/App.test.tsx`

- [ ] **Step 1: Add minimal tokens without remapping global HUD variables**

In `web/src/styles.css`, keep existing Dracula variables and global `--hud-*` mappings in `:root` for overlays. Add these minimal variables inside `:root` after the existing `--hud-*` variables:

```css
  --minimal-bg: #0b0d10;
  --minimal-surface-0: #0f1216;
  --minimal-surface-1: #151922;
  --minimal-surface-2: #1d2430;
  --minimal-border-subtle: rgba(226, 232, 240, 0.12);
  --minimal-border-strong: rgba(226, 232, 240, 0.22);
  --minimal-text: #f4f7fb;
  --minimal-muted: #9aa4b2;
  --minimal-faint: #687385;
  --minimal-accent: #d7dde8;
  --minimal-success: #6ee7b7;
  --minimal-warning: #fbbf24;
  --minimal-danger: #fb7185;
```

- [ ] **Step 2: Map HUD tokens only under `.hud-shell--dashboard`**

Replace the current `.hud-shell--dashboard` block with this dashboard-scoped mapping and flat background:

```css
.hud-shell--dashboard {
  --hud-bg: var(--minimal-bg);
  --hud-panel: var(--minimal-surface-1);
  --hud-panel-strong: var(--minimal-surface-2);
  --hud-terminal: var(--minimal-surface-0);
  --hud-line: var(--minimal-border-strong);
  --hud-line-soft: var(--minimal-border-subtle);
  --hud-text: var(--minimal-text);
  --hud-muted: var(--minimal-muted);
  --hud-cyan: var(--minimal-accent);
  --hud-fuchsia: #c4b5fd;
  --hud-purple: #a78bfa;
  --hud-emerald: var(--minimal-success);
  --hud-amber: var(--minimal-warning);
  --hud-orange: #fb923c;
  --hud-rose: var(--minimal-danger);
  --hud-shadow: none;
  padding: 12px;
  background: var(--minimal-bg);
  font-family:
    Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
}
```

Do not change global `body` typography in this task; overlay routes use global HUD classes too.

- [ ] **Step 3: Run focused checks**

Run:

```bash
pnpm --dir web run format
pnpm --dir web run test -- App.test.tsx
pnpm --dir web run build
```

Expected: TypeScript passes, `App.test.tsx` passes, and web build passes. No visible text should need changing in this task.

- [ ] **Step 4: Commit**

```bash
git add web/src/styles.css web/src/App.test.tsx
git commit -m "style: add minimal operator design tokens"
```

---

### Task 2: Dashboard-scoped minimal panel, chip, input, and button primitives

**Files:**
- Modify: `web/src/styles.css`
- Test: `web/src/App.test.tsx`

- [ ] **Step 1: Add dashboard-scoped quiet panel overrides**

In `web/src/styles.css`, do **not** replace global `.hud-panel`, `.hud-corners`, or `.hud-scanlines` definitions. Add these overrides after the existing primitive definitions so overlays keep their current styling:

```css
.hud-shell--dashboard .hud-panel {
  position: relative;
  overflow: hidden;
  border: 1px solid var(--hud-line-soft);
  border-radius: 14px !important;
  background: var(--hud-panel);
  box-shadow: none;
  backdrop-filter: none;
  -webkit-backdrop-filter: none;
}

.hud-shell--dashboard .hud-panel--flat {
  border: 1px solid var(--hud-line-soft);
  background: var(--hud-terminal);
  box-shadow: none;
  backdrop-filter: none;
  -webkit-backdrop-filter: none;
}

.hud-shell--dashboard .hud-panel::before,
.hud-shell--dashboard .hud-panel--flat::before,
.hud-shell--dashboard .hud-corners::after,
.hud-shell--dashboard.hud-scanlines::after,
.hud-shell--dashboard .hud-scanlines::after {
  display: none;
}
```

- [ ] **Step 2: Add dashboard-scoped quiet typography/control overrides**

Add these overrides after existing primitive blocks:

```css
.hud-shell--dashboard .hud-label {
  font-size: 0.72rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--hud-muted);
}

.hud-shell--dashboard .hud-kicker {
  font-size: 0.7rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--minimal-faint);
}

.hud-shell--dashboard .hud-title {
  margin: 0;
  font-size: clamp(1.7rem, 3vw, 2.75rem);
  line-height: 1;
  letter-spacing: -0.055em;
  text-wrap: balance;
}

.hud-shell--dashboard .hud-chip {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  border: 1px solid var(--hud-line-soft);
  background: rgba(255, 255, 255, 0.03);
  padding: 0.36rem 0.6rem;
  border-radius: 999px;
  font-size: 0.72rem;
  line-height: 1;
  letter-spacing: 0.02em;
  text-transform: none;
  color: var(--hud-muted);
}

.hud-shell--dashboard .hud-chip--square {
  border-radius: 999px;
  padding: 0.35rem 0.58rem;
  letter-spacing: 0.02em;
}

.hud-shell--dashboard .hud-input {
  width: 100%;
  border: 1px solid var(--hud-line-soft);
  border-radius: 10px;
  background: var(--minimal-surface-0);
  padding: 0.62rem 0.72rem;
  color: var(--hud-text);
  outline: none;
}

.hud-shell--dashboard .hud-input:focus {
  border-color: var(--minimal-border-strong);
  box-shadow: 0 0 0 3px rgba(215, 221, 232, 0.12);
}

.hud-shell--dashboard .hud-button,
.hud-shell--dashboard .hud-action-button {
  border: 1px solid var(--hud-line-soft);
  border-radius: 10px;
  background: rgba(255, 255, 255, 0.035);
  padding: 0.65rem 0.75rem;
  color: var(--hud-text);
  text-align: left;
  transition:
    background 140ms ease,
    border-color 140ms ease,
    color 140ms ease;
}

.hud-shell--dashboard .hud-button:hover,
.hud-shell--dashboard .hud-action-button:hover {
  border-color: var(--minimal-border-strong);
  background: rgba(255, 255, 255, 0.065);
  transform: none;
}

.hud-shell--dashboard .hud-button--danger {
  --hud-accent: var(--hud-rose);
  border-color: color-mix(in oklab, var(--hud-rose) 52%, transparent);
  background: color-mix(in oklab, var(--hud-rose) 12%, var(--minimal-surface-0));
  color: #ffe4e6;
}
```

- [ ] **Step 3: Add safety/focus overrides**

Add:

```css
.hud-shell--dashboard .hud-led {
  width: 0.55rem;
  height: 0.55rem;
  border-radius: 999px;
  background: var(--hud-accent, var(--hud-cyan));
  box-shadow: 0 0 0 2px rgba(255, 255, 255, 0.06);
}

.hud-shell--dashboard .hud-danger-zone {
  --hud-accent: var(--hud-rose);
  border-color: color-mix(in oklab, var(--hud-rose) 58%, transparent) !important;
  background: color-mix(in oklab, var(--hud-rose) 9%, var(--minimal-surface-0));
}

.hud-shell--dashboard .hud-button:focus-visible,
.hud-shell--dashboard .hud-action-button:focus-visible,
.hud-shell--dashboard .hud-chip:focus-visible,
.hud-shell--dashboard .hud-input:focus-visible {
  outline: 2px solid color-mix(in oklab, var(--hud-accent, var(--minimal-accent)) 78%, white);
  outline-offset: 3px;
}
```

- [ ] **Step 4: Run checks**

Run:

```bash
pnpm --dir web run format
pnpm --dir web run test -- App.test.tsx
pnpm --dir web run build
```

Expected: pass. If this task changes global overlay visuals in browser smoke or visual QA, revert and re-scope the offending selector under `.hud-shell--dashboard`.

- [ ] **Step 5: Commit**

```bash
git add web/src/styles.css web/src/App.test.tsx
git commit -m "style: quiet dashboard primitives"
```

---

### Task 3: Minimal dashboard shell and grouped navigation

**Files:**
- Modify: `web/src/routes/dashboard/DashboardShell.tsx`
- Modify: `web/src/App.test.tsx`
- Modify if needed: `web/e2e/smoke.e2e.ts`

- [ ] **Step 1: Simplify shell class names without changing route content**

In `DashboardShell.tsx`, remove `hud-scanlines` from the `<main>` class and use roomier minimal spacing:

```tsx
<main className="hud-shell hud-shell--dashboard min-h-screen text-zinc-100">
  <section className="hud-console-frame mx-auto max-w-[1200px]">
```

- [ ] **Step 2: Make the shell header quieter**

Change the header wrapper class to:

```tsx
<header className="hud-panel hud-panel--flat rounded-2xl p-4">
```

Keep visible text unchanged: `Broadcast ops console`, active route title/description, nav group labels, active group chip, `Twitch login`.

- [ ] **Step 3: Convert nav links from chips to quiet buttons while preserving text**

Change the route `<a>` class expression to:

```tsx
className={`hud-button hud-button--small ${activePath === route.path ? 'border-zinc-100/40 bg-white/10 text-zinc-50' : 'text-zinc-400'}`}
```

Keep `aria-current` exactly as-is.

- [ ] **Step 4: Tighten route shell SSR coverage**

In `web/src/App.test.tsx`, the shell hierarchy test should assert all dashboard nav groups:

```ts
expect(html).toContain('Monitor');
expect(html).toContain('Setup');
expect(html).toContain('Operate');
expect(html).toContain('Configure');
```

- [ ] **Step 5: Run checks**

Run:

```bash
pnpm --dir web run format
pnpm --dir web run test -- App.test.tsx
pnpm --dir web run build
```

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add web/src/routes/dashboard/DashboardShell.tsx web/src/App.test.tsx web/e2e/smoke.e2e.ts
git commit -m "style: simplify dashboard shell"
```

---

### Task 4: Minimal console primitives and layout editor

**Files:**
- Modify: `web/src/routes/dashboard/components/ConsolePageHeader.tsx`
- Modify: `web/src/routes/dashboard/components/ConsoleSection.tsx`
- Modify: `web/src/routes/dashboard/components/CustomizablePanelLayout.tsx`
- Modify: `web/src/App.test.tsx`
- Modify if needed: `web/e2e/smoke.e2e.ts`

- [ ] **Step 1: Remove decorative corner classes from page/section primitives**

In `ConsolePageHeader.tsx`, change the section class to:

```tsx
<section className={`hud-panel hud-panel--flat hud-accent-${accent} rounded-2xl p-4`}>
```

In `ConsoleSection.tsx`, change the section class to:

```tsx
<section className={`hud-panel hud-panel--flat ${danger ? 'hud-danger-zone' : `hud-accent-${accent}`} rounded-2xl p-4`}>
```

- [ ] **Step 2: Quiet the customizable layout editor**

In `CustomizablePanelLayout.tsx`, change the layout editor panel class:

```tsx
<div className="hud-panel hud-panel--flat border border-zinc-700/50 p-4">
```

Change the workspace label from fuchsia to neutral while keeping text:

```tsx
<p className="hud-label">Workspace layout</p>
<h2 className="text-lg font-semibold text-zinc-100">{title}</h2>
```

Change each layout row to a quiet card:

```tsx
<article key={item.id} className="hud-compact-card flex items-center justify-between gap-2 border border-zinc-800/80 bg-black/10">
```

Keep all aria labels and button text unchanged.

- [ ] **Step 3: Keep safety copy exactly stable**

Verify these strings are unchanged:

- `Customize panel layout`
- `Reset layout`
- `Locked safety panels stay fixed outside this list.`
- `Moderation guardrails stay locked`
- `Secret controls stay locked`

- [ ] **Step 4: Run checks**

Run:

```bash
pnpm --dir web run format
pnpm --dir web run test -- App.test.tsx
pnpm --dir web run build
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add web/src/routes/dashboard/components/ConsolePageHeader.tsx web/src/routes/dashboard/components/ConsoleSection.tsx web/src/routes/dashboard/components/CustomizablePanelLayout.tsx web/src/App.test.tsx web/e2e/smoke.e2e.ts
git commit -m "style: simplify console primitives"
```

---

### Task 5: Visual QA docs and full verification

**Files:**
- Modify: `docs/runbooks/visual-qa.md`
- Modify: `docs/superpowers/plans/2026-06-18-minimal-operator-design.md`
- Modify if needed: `web/e2e/smoke.e2e.ts`

- [ ] **Step 1: Add visual QA checklist**

Append this section to `docs/runbooks/visual-qa.md`:

```markdown
## Minimal operator style checks

- Dashboard shell uses a quiet near-monochrome style: no scanline overlay, no neon corner brackets, no heavy offset shadows.
- Overlay routes retain their existing broadcast HUD style; this sprint only applies minimal styling under `.hud-shell--dashboard`.
- Grouped dashboard navigation remains visible and keyboard-focusable.
- `Customize panel layout`, `Reset layout`, and locked-panel copy remain visible on Live/Bot/Config.
- Danger zones remain visually distinct from ordinary panels, especially the bot nuke planner and delete-confirmation areas.
- Minimal style does not remove operator/webhook secret controls, Twitch login, reward queue safety copy, or moderation confirmations.
- Monospace remains available for logs, IDs, JSON, terminal rows, and payload-like data.
```

- [ ] **Step 2: Run full verification**

Run:

```bash
make verify
make docker-build
make smoke
make smoke-browser
```

Expected:

- `make verify` passes.
- `make smoke` passes. If `/readyz` is degraded only for known local EventSub/OBS reachability/auth, record that note but do not treat it as a style regression.
- `make smoke-browser` passes.

- [ ] **Step 3: Remove generated browser artifacts**

If Playwright created `web/test-results/`, remove it before committing.

- [ ] **Step 4: Append closeout note**

Append a closeout section to this plan with:

- commits created,
- verification commands actually run,
- any accepted environmental readiness notes,
- deferred items: theme toggle, per-operator theme choice, bounded panel resizing, drag/drop docking, shared server-side layouts.

- [ ] **Step 5: Commit**

```bash
git add docs/runbooks/visual-qa.md docs/superpowers/plans/2026-06-18-minimal-operator-design.md web/e2e/smoke.e2e.ts
git commit -m "docs: close minimal operator design sprint"
```

---

## Self-review

- Spec coverage: User asked to keep iterating on controlled customization and requested “a new design style, something very minimal.” This plan preserves layout customization and safety while making the dashboard visually minimal.
- Placeholder scan: No task uses “TBD,” “TODO,” “similar to,” or unspecified code changes.
- Type consistency: Existing component names and strings match current files. Plan avoids class-name tests and keeps copy stable unless explicitly noted.
- Risk posture: This plan changes dashboard visual primitives broadly but scopes CSS under `.hud-shell--dashboard` to avoid overlay regressions. It intentionally keeps behavior stable and commits in CSS/primitive slices so regressions are easy to isolate.

## Closeout

- Commits completed so far, per `git log --oneline -10`:
  - `1e1b3e0` `style: simplify console primitives`
  - `de9d1ab` `style: simplify dashboard shell`
  - `51807f1` `style: quiet dashboard primitives`
  - `6a49694` `style: add minimal operator design tokens`
  - Earlier planning docs remain in history, including `23f084e` `docs: plan minimal operator design` and `f89790a` `docs: close operator layout customization sprint`.
- Final verification passed:
  - `make verify` passed: frontend install/typecheck/lint, Go vet/tests, Vitest 78 tests, Go build, and web build.
  - `make docker-build` rebuilt/restarted the API and web containers successfully.
  - `make smoke` passed: health/version/dashboard/overlay checks passed. `/readyz` remained degraded only for local Twitch EventSub not connected; OBS reachability/auth were OK in this run. Twitch auth still reported moderator scopes in `missingScopes`, matching the known local auth-state issue from prior sprints.
  - `make smoke-browser` passed: 3 Playwright operator smoke tests passed.
  - Removed generated `web/test-results/.last-run.json` from the working tree.
- Deferred items carried forward from the plan: theme toggle, per-operator theme choice, bounded panel resizing, drag/drop docking, and shared server-side layouts.
