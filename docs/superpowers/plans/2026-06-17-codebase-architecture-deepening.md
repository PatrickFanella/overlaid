# Codebase Architecture Deepening Plan

> **For agentic workers:** Execute this plan slice-by-slice. Verify and commit after each slice. Use the architecture vocabulary below consistently: **Module**, **Interface**, **Implementation**, **Depth**, **Deep**, **Shallow**, **Seam**, **Adapter**, **Leverage**, and **Locality**.

**Goal:** Turn the main FossaBot parity and operator workflow surfaces from broad, Shallow Modules into deeper Modules with smaller Interfaces, stronger Locality, and better testability.

**Architecture:** Preserve over-laid's current Go/React/Postgres architecture and ADR decisions. Deepen behavior behind seams before adding new product breadth. Keep HTTP handlers and dashboard routes as Adapters where possible; put policy and workflow behavior in Modules that can be tested directly.

**Tech Stack:** Go backend, Postgres, React/Vite TypeScript dashboard, Playwright smoke tests, Vitest SSR tests, Docker Compose, `make verify`, `make smoke`, `make smoke-browser`.

---

## Source review notes

- No repo `LANGUAGE.md` or `CONTEXT.md` existed at review time, so the architecture vocabulary in the request is the source of truth for this plan.
- ADRs reviewed:
  - `docs/adr/0001-generic-base-template.md` — keep the template/process easy to audit and update.
  - `docs/adr/0002-postgres-product-storage.md` — Postgres is durable product storage; file/browser storage remains only for token material, transitional fallback, exports, recovery artifacts, and private/session state.
- Current domain docs reviewed:
  - `docs/superpowers/plans/2026-06-15-fossabot-parity-roadmap.md`
  - `docs/runbooks/bot-ops.md`
  - `docs/runbooks/twitch-interaction-rules.md`

---

## Findings to preserve

### 1. Moderation / nuke workflow Module

**Files:** `backend/internal/http/bot_handlers.go`, `backend/internal/http/twitch_moderation.go`, `backend/internal/http/bot_moderation_enforcement.go`, `backend/internal/bot/store.go`, `web/src/routes/dashboard/Bot.tsx`, `docs/runbooks/bot-ops.md`.

**Problem:** Moderation behavior spans HTTP handlers, rule matching, event-history scanning, Twitch calls, radiation creation, rollback, audit recording, and dashboard state. The current Interface is too wide: callers must know confirmation rules, caps, lookback windows, Twitch scopes, timeout defaults, radiation expiry, audit meanings, message-id extraction, and rule persistence.

**Deletion test:** Deleting `planNukeDryRun`, `botModerationNukeExecute`, or `enforceModerationForEvent` would not remove complexity; it would reappear across callers.

**Deepening direction:** Create a moderation workflow Module that owns dry-run, manual action, enforcement, nuke planning, nuke execution, radiation creation, rollback, and audit outcomes. Keep HTTP and EventSub as Adapters at the Seam.

**Benefits:** Higher Locality for dangerous moderation policy, more Leverage for callers, direct workflow tests, and better AI-navigability.

**ADR conflicts:** None; aligns with ADR-0002.

### 2. Twitch Helix Adapter Module

**Files:** `backend/internal/http/twitch_rewards.go`, `backend/internal/http/twitch_moderation.go`, token/scope helpers.

**Problem:** Twitch request construction is embedded inside HTTP-package Modules. Reward and moderation code duplicate token checks, scope checks, broadcaster identity, Client-Id headers, response status handling, error sanitization, and JSON decoding.

**Deletion test:** Deleting `twitchRewardsRequest` or `twitchModerationRequest` would scatter real complexity, but the current Modules still leak Helix details into surrounding code.

**Deepening direction:** Create a dedicated Twitch Helix Adapter Module that owns protocol details. Reward, redemption, moderation, and future Twitch calls should share one Implementation.

**Benefits:** Locality for Twitch protocol knowledge, Leverage for future Twitch calls, cleaner tests at the Adapter Seam, and one obvious home for Twitch behavior.

**ADR conflicts:** None.

### 3. Bot Store Module breadth

**Files:** `backend/internal/bot/store.go`, `backend/internal/bot/api_variables.go`, `backend/internal/bot/templates.go`, `backend/internal/bot/timers.go`.

**Problem:** `bot.Store` owns audit, commands, timers, counters, quotes, moderation, radiation rollback, table creation, validation, scanning, SQL, and pieces of runtime behavior. The Module earns its keep, but the Interface is now too broad.

**Deletion test:** Deleting `Store` would explode complexity everywhere. Deleting chunks like API variable fetching or moderation matching from `Store` would improve Locality rather than move pass-through code.

**Deepening direction:** Separate durable product storage from policy-heavy bot behavior while keeping Postgres as the durable source of truth.

**Benefits:** SQL/schema Locality, behavior Locality, better no-Postgres behavior tests, and less catch-all growth.

**ADR conflicts:** None if Postgres remains durable product storage.

### 4. Template rendering / variable resolution Module

**Files:** `backend/internal/bot/templates.go`, `backend/internal/twitch/commands.go`, `backend/internal/bot/api_variables.go`, `backend/internal/bot/timers.go`, `web/src/routes/dashboard/Bot.tsx`.

**Problem:** `RenderTemplate` hides string replacement, but not variable discovery, resolution, caps, missing-variable behavior, or caller-specific context differences. The Interface is mostly “bring your own fully assembled context.”

**Deletion test:** Deleting `RenderTemplate` would only move regex replacement around; the important complexity already lives in callers.

**Deepening direction:** Make the template Module own variable discovery, resolution policy, caps, missing-variable reporting, and caller-specific context assembly.

**Benefits:** Locality for template semantics, Leverage for commands/timers/previews, and one behavior test surface.

**ADR conflicts:** None.

### 5. Twitch command execution Module

**Files:** `backend/internal/twitch/commands.go`, `backend/internal/bot/runtime.go`, `backend/internal/bot/store.go`, `backend/internal/http/server.go`.

**Problem:** `CommandProcessor` parses chat text, resolves aliases, checks roles, handles cooldowns, mutates counters/quotes, renders templates, resolves API variables, emits overlay events, and handles custom response modes. It has Depth, but its Interface leaks too many concepts.

**Deletion test:** Deleting `CommandProcessor` would scatter real complexity, but community commands and custom command rendering are dense enough for deeper internal Modules.

**Deepening direction:** Deepen around chat command execution, with internal Modules for built-ins, community tools, custom response rendering, and event emission.

**Benefits:** Better Locality for command behavior, Leverage for EventSub callers, and easier tests of decisions/outcomes.

**ADR conflicts:** None.

### 6. Bot dashboard route Module

**Files:** `web/src/routes/dashboard/Bot.tsx`, `web/src/lib/api.ts`.

**Problem:** `Bot.tsx` owns loading, refresh groups, drafts, save/delete handlers, moderation actions, nuke state, rollback state, API variables, counters, quotes, commands, timers, and rendering. Each area’s Interface is nearly the full route state.

**Deletion test:** Deleting a small rendered section would not remove complexity; it would inline back into the route.

**Deepening direction:** Split bot operation areas into Modules: commands, timers, community tools, API variables, moderation/nuke, and audit/status. Keep the route as a small Adapter.

**Benefits:** Locality by feature area, route-level Leverage, smaller UI test surfaces, and easier AI navigation.

**ADR conflicts:** None.

### 7. Twitch interaction rules and reward queue Module split

**Files:** `web/src/routes/dashboard/components/TwitchInteractionsManager.tsx`, `web/src/lib/twitchInteractionRules.ts`, `backend/internal/interactions/rules.go`, `backend/internal/http/twitch_rewards.go`, `docs/runbooks/twitch-interaction-rules.md`.

**Problem:** One UI Module mixes durable Twitch interaction rule editing and live Twitch reward operations. Their failure modes and knowledge are different.

**Deletion test:** Deleting reward queue behavior from this Module would reduce cognitive load and reveal a separate behavior area.

**Deepening direction:** Split into rule-editing and reward-operations Modules. Keep reward-title matching as a small helper if it continues earning Leverage.

**Benefits:** Locality between config and live operations, independent evolution, and clearer tests.

**ADR conflicts:** None if Postgres-backed rules remain aligned with ADR-0002.

### 8. Frontend API client Module

**Files:** `web/src/lib/api.ts`.

**Problem:** `api.ts` centralizes fetch calls and types, but most functions are Shallow endpoint wrappers. Error handling and operator-secret handling differ across calls.

**Deletion test:** Deleting `api.ts` would spread fetch calls everywhere, so centralization helps. But each function’s Interface is nearly as complex as its Implementation.

**Deepening direction:** Deepen around product operation families and consistent request/error behavior.

**Benefits:** Locality for auth/base URL/response parsing/error detail extraction, higher Leverage for UI Modules, and easier fake operation families in tests.

**ADR conflicts:** None.

### 9. Runtime permission / audit Module

**Files:** `backend/internal/bot/runtime.go`, `backend/internal/twitch/commands.go`, `backend/internal/http/twitch_moderation.go`, `backend/internal/bot/timers.go`.

**Problem:** `Runtime` defines roles, sources, results, and audit recording, but role checks are duplicated and audit construction is scattered. The Module looks important but is thin.

**Deletion test:** Deleting `Runtime` would mostly move `Record` and `Allowed`; most policy complexity already lives elsewhere.

**Deepening direction:** Either deepen `Runtime` so it owns more permission/audit policy, or absorb it into a deeper bot workflow Module.

**Benefits:** Locality for role/audit semantics, Leverage for callers recording bot outcomes, and one test surface for audit invariants.

**ADR conflicts:** None.

### 10. API variable resolution Module

**Files:** `backend/internal/bot/api_variables.go`, `backend/internal/bot/store.go`, `backend/internal/twitch/commands.go`, `docs/runbooks/bot-ops.md`.

**Problem:** API variables are safety-sensitive. Storage, validation, fetching, caching, selector rules, fallback behavior, and status writes live together under `Store`.

**Deletion test:** Deleting API variable resolution from `Store` would improve Locality. The complexity would move behind a better Seam.

**Deepening direction:** Make API variable resolution its own Module, with storage and HTTP fetching as Adapters.

**Benefits:** Locality for external-fetch safety rules, Leverage for template rendering, and no-Postgres tests for safety behavior.

**ADR conflicts:** None.

### 11. Test and runbook safety Module

**Files:** `web/src/App.test.tsx`, `web/e2e/smoke.e2e.ts`, `backend/internal/http/twitch_rewards_test.go`, `backend/internal/http/twitch_moderation_test.go`, `docs/runbooks/bot-ops.md`, `docs/runbooks/twitch-interaction-rules.md`, `docs/superpowers/plans/2026-06-15-fossabot-parity-roadmap.md`.

**Problem:** Stabilization improved smoke and SSR coverage, but many frontend tests still assert rendered safety copy. Backend behavior tests are deeper. UI and docs do not yet form a Deep safety Module.

**Deletion test:** Deleting SSR copy tests would not expose much behavior risk. Deleting backend moderation/reward tests would expose real risk.

**Deepening direction:** Add workflow-contract tests for dangerous operations and prune stale roadmap/runbook guidance.

**Benefits:** Locality for safety expectations, Leverage for future refactors, and tests/runbooks as executable map.

**ADR conflicts:** None; supports ADR-0001.

---

## Implementation order

1. **Record findings and plan** — this document.
2. **Twitch Helix Adapter Module** — create the shared Adapter first so later workflow Modules can lean on a deeper Twitch Seam.
3. **Moderation / nuke workflow Module** — highest risk and strongest Locality payoff.
4. **Template and API variable resolution Modules** — deepen shared bot text behavior before command/runtime cleanup.
5. **Command execution and runtime/audit Modules** — reduce duplicated role/audit policy and improve command Locality.
6. **Bot dashboard route split** — reduce frontend monolith once backend/product seams are clearer.
7. **Twitch interaction manager split** — separate durable rule editing from live reward operations.
8. **Frontend API client deepening** — group product operation families and normalize request/error behavior.
9. **Safety tests and runbook closeout** — lock workflow contracts and update docs to match the new seams.

Each slice must finish with focused checks, `make verify` when appropriate, and a commit.

---

## Slice 6 closeout

**Status:** Implemented.

**Committed in this slice:**

- Deepened `web/src/lib/api.ts` around a shared JSON request helper for bot operations plus Twitch interactions/reward flows.
- Kept exported API names/types intact while normalizing request method, base URL, headers, JSON body encoding, and non-OK detail extraction.
- Strengthened `web/src/App.test.tsx` with request-contract coverage for shared API encoding and error-detail propagation.

**Verification:**

- Passed: `pnpm --dir web run format`
- Passed: `pnpm --dir web run test`
- Passed: `make verify`

**Deferred intentionally:**

- More aggressive extraction of `Bot.tsx` panels and other frontend route Modules.
- Further API client deepening beyond bot/Twitch interaction and reward families.
- Any broader workflow-module refactors not required to lock the slice-6 contract.

## October 6 bot panel extraction

Moved commands, community tools, API variables, moderation/nuke, and timer rendering from `Bot.tsx` into separate components under `web/src/routes/dashboard/bot/`. Audit/status was already separate. Panel props, safety guards, layout configuration, and API handlers retain their existing behavior. `Bot.tsx` is now 427 lines rather than 1,099.

Verification passed: `make verify` and all six `make smoke-browser` checks, including timer pending/success/failure behavior. Runtime smoke also passed against the disposable Postgres API at port 8081.

This is the first part of the bot dashboard split. Request state, drafts, and refresh handlers still belong to the workspace; separating those by operation remains follow-up work.

## October 6 bot loading failures

Moved initial workspace loading into `bot/loadBotWorkspace.ts`. Each request result is handled separately. A failed operation now supplies a panel-specific load error while successful panels keep their data. Counters and quotes also display their load errors. The workspace keeps the cancellation guard for navigation.

Verification passed: `make verify`, including 92 frontend tests, and all seven `make smoke-browser` checks. Coverage includes an API-variable failure alongside a working saved timer and bot runtime, and denied operator access across all bot operations. Per-operation drafts and mutation state still belong to the workspace.

## October 6 timer operation state

`bot/useBotTimers.ts` now owns timer loading, drafts, saved timer editing, mutations, pending operations, and local errors/results. Timer requests no longer depend on the workspace's initial load. The hook retains drafts when the panel is hidden and ignores responses after unmount or a change of API/credentials. The route is now 351 lines. Commands, community tools, API variables, and moderation state remain to be separated.

The editor updates the existing timer ID, preserves paused state and the last scheduled run, supports cancel, and retains drafts after failed saves. Browser coverage exercises those behaviors, pending controls, and an initial load that finishes after a successful timer action. Newer status/audit data survives that late load.

Verification passed: `make verify`, all eight `make smoke-browser` checks, and `make smoke API_PORT=8081`. A collaborative-browser edit against disposable Postgres updated the same timer ID and wrote a `timer_update` audit row. Live Twitch/OBS verification remains pending.

## October 6 community operation state

`bot/useBotCommunity.ts` now owns separate counter and quote collections, drafts, loading, mutations, pending controls, and local errors/results. Community requests no longer depend on the workspace loader or API-variable availability. Drafts survive failed writes and panel hide/show; successful responses update rows immediately. Failed status/audit refreshes retain the success receipt. A failed initial list still carries its warning after a write, because the visible collection may be incomplete.

Shared summary refreshes ignore responses after a workspace change and responses from older overlapping refreshes. Commands, API variables, and moderation remain in workspace state and are the next parts of the route split.

Verification passed: `make verify` (93 frontend tests and the Go suite) and all ten `make smoke-browser` checks. Browser coverage includes quote writes during a pending counter request, failed counter draft retention, delete retry, audit refresh, and a successful receipt after a failed refresh. The collaborative browser created and deleted both community records against disposable Postgres.

## October 6 command operation state

`bot/useBotCommands.ts` now owns command loading, drafts, saved-command editing, mutations, pending controls, and local errors/results. Command requests no longer wait for the workspace loader or API variables. The editor preserves the existing ID, paused state, response mode, role, and action. Cancel, failed-save retry, toggle synchronization, and deletion of the selected command are covered by browser workflows. Returned records update the list immediately; a failed summary refresh retains the successful operation result.

Verification passed: `make verify` (93 frontend tests and the Go suite), all eleven `make smoke-browser` checks, and `make smoke API_PORT=8081`. The collaborative browser updated the response and cooldown of a paused Postgres command while preserving its ID, mode, role, and action, then deleted the fixture. API-variable and moderation state remain to be separated.
