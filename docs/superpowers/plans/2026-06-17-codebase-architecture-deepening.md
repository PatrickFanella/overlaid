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


## Current implementation map — October 6

This map describes the current code. The dated slice notes below preserve the verification history; references there to “next” work describe the state at that checkpoint.

| Plan area | Current ownership and evidence |
| --- | --- |
| Shared Helix adapter | `http/twitch_helix.go` owns token/scope checks and JSON request/response handling used by rewards and moderation; fake Helix contracts cover errors and authorization. |
| Moderation workflow | `http/bot_moderation_workflow.go` owns manual actions, enforcement, nuke planning/execution/radiation/rollback, and outcomes. `bot/moderation_rules.go` owns matching policy. Backend workflow contracts and browser confirmation/pending/retry checks cover the adapters. |
| Bot storage separation | `bot.Store` retains Postgres CRUD/schema/scanning. Configuration normalization, audit payloads, moderation matching, templates, and API fetching/cache policy live in separate modules. Existing offline validation tests and isolated Postgres CRUD/audit contracts cover both sides. |
| Templates and API variables | `bot.TemplateRenderer` owns dependency discovery/assembly and rendering caps; `APIVariableResolver` owns bounded resolution/cache/fallback through repository and HTTP adapters. Offline dependency/safety tests and command integration contracts cover this seam. |
| Commands and runtime/audit | The command dispatcher delegates to built-in, community, and custom executors. Shared role policy and outcome construction live in `bot/runtime.go`, with bounded audit queuing and explicit flush. Role/cooldown/reload/audit tests cover the public interfaces. |
| Bot dashboard | Feature hooks own commands, timers, community tools, API variables, and moderation state; `BotWorkspace` coordinates summary and layout. Browser workflows cover writes, failures, stale responses, confirmations, and receipts. |
| Twitch interaction split | `TwitchInteractionRulesEditor` owns durable editing; `TwitchRewardQueueManager` and `useRewardOperation` own live operations and serialization. Browser fixtures cover the independent failure modes. |
| Frontend transport | `apiRequest.ts` owns fetch/auth/base URL/body/error handling for all `api.ts` operation families. Request contracts cover protected/public operations, malformed JSON, and no-content responses. |
| Safety/runbook closeout | The bot and reward runbooks describe current guardrails; browser workflows exercise dangerous operations without live writes. Live Twitch/OBS verification still requires external credentials/services. |

The local architecture implementation is in place. Remaining external validation does not block offline work: real Twitch delivery/moderation/reward lifecycle, OBS integration, and deployed monitoring remain unverified. The frontend's TypeScript template preview remains a separate local implementation; no new shared cross-language renderer is required by this plan.

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

## October 6 API-variable operation state and cache behavior

`bot/useBotAPIVariables.ts` now owns API-variable loading, drafts, saved-record editing, mutations, pending controls, and local errors/results. Returned records update the list immediately. Failed saves retain drafts, Cancel edit abandons them, and deleting the selected record restores the new-variable form. A failed summary refresh preserves the successful write result. Moderation remains the last bot operation whose drafts and mutations belong to the workspace.

Editor verification exposed a cache defect: cached HTTP values bypassed current configuration, allowing disabled or deleted variables to keep supplying values until expiry. Resolution now loads configuration before checking cache, and cache entries match the saved record ID and configuration timestamp. Edits clear old diagnostics, and diagnostic writes from an older configuration are ignored. Ordinary diagnostic updates keep the configuration timestamp and its valid cache entry.

Verification passed: `make verify` (93 frontend tests and the Go suite), all twelve `make smoke-browser` checks, `make smoke API_PORT=8081`, and the optional Postgres cache/session-audit tests. Cache tests cover disabled/deleted variables, configuration changes, late old responses, expired values, and recreated names without external HTTP calls. The collaborative browser confirmed private-host rejection with a retained draft, a successful edit preserving the same ID and settings, and fixture deletion.

## October 6 API-variable network checks

API-variable HTTP connections now validate every DNS answer and dial the approved numeric address, preventing a hostname from selecting a private destination after URL validation. Mixed public/private answers, shared and reserved IPv4 ranges, and IPv6 translation/tunnel ranges are rejected. Connections bypass environment proxies and reapply the checks after the single permitted redirect. Oversized or interrupted responses fail explicitly, and request diagnostics omit URLs and query values.

Offline tests cover DNS rejection, pinned-address retries, cancellation, redirects, body limits, and safe errors. A local TLS fixture verifies the complete HTTP path and confirms that an environment proxy receives no request. Verification passed: race testing, `make verify` (93 frontend tests and the Go suite), all fourteen `make smoke-browser` checks, optional Postgres cache/session-audit tests, and `make smoke API_PORT=8081`. Storage/cache ownership still belongs to `Store`; this closes a concrete network gap before the resolver extraction.

## October 6 API-variable resolver module

`api_variable_resolver.go` now owns name normalization, the three-variable limit, shared fetch deadline, fallback selection, and the mutex-protected configuration-revision cache. The command processor receives this resolver directly. `Store` supplies lookup and revision-conditional diagnostics through a small repository interface; HTTP/DNS/selector behavior lives in `api_variable_http.go`. Each Postgres store initializes one shared resolver, so later renders retain their cache. Missing rows return `missing`; storage failures now return `unavailable` without copying database errors into command data.

Offline resolver tests exercise fetch/cache/expiry, edits and recreated records, disabled/deleted variables, fallback retries, diagnostic-write failures, normalized render limits, and concurrent renders. Verification passed: `make verify` (93 frontend tests and the Go suite), all fourteen browser smoke checks, the Postgres cache/session-audit tests, and `make smoke API_PORT=8081`. Bot and Twitch race tests pass. Broader HTTP race testing exposed existing unsynchronized response-buffer reads in SSE replay tests; fixing those tests is the next verification slice.

## October 6 SSE verification synchronization

SSE tests now receive immutable snapshots from the handler's flush operation instead of polling a response buffer while it is being written. A shared fixture cancels and joins the handler during cleanup, including failed assertions. Retained BRB replay, persisted goal replay, cleared state, and live alert delivery now wait for the corresponding flush. The alert test confirms delivery before cancellation, removing its previous event/cancellation race.

Verification passed: the complete backend `go test -race ./... -count=1` suite, `make verify` (93 frontend tests and the Go suite), and all fourteen `make smoke-browser` checks. This resolves the HTTP test races discovered during the resolver extraction.

## October 6 moderation operation state

`bot/useBotModeration.ts` now owns moderation-rule loading and writes, test-message drafts/results, manual action state, and nuke planning/execution/rollback. The workspace coordinates panel layout and status/audit; each bot operation owns its requests and drafts. `Bot.tsx` is now 110 lines. Pending moderation operations disable overlapping moderation/nuke controls, and responses after navigation or API/credential changes are ignored.

Changing nuke settings clears the old plan and confirmation. Execution consumes confirmation at the start. Execution and rollback receipts survive summary-refresh failure, and returned radiation rules update immediately without a rule-list reload. Manual action receipts display the actual result/reason; failed results are not shown as successful. Failed rule writes keep their drafts, and test edits clear stale dry-run results.

Verification passed: `make verify` (93 frontend tests and the Go suite), all fourteen `make smoke-browser` checks, and `make smoke API_PORT=8081`. Browser fixtures cover stale-plan invalidation, pending execution, confirmation consumption, rollback after refresh failure, manual action failure/audit refresh, draft retry, and stale dry-run clearing. The collaborative browser created a dry-run rule in Postgres, matched a message, deleted the fixture, and confirmed that its old test result cleared. Live Twitch moderation remains unverified.

## October 6 reward queue operation ownership

`useRewardOperation.ts` is a small Module behind the live reward queue Adapter. Its Interface owns write serialization, pending state, receipts, error handling, and response invalidation when API/credentials change. Returned rewards and completed redemptions update immediately, while the receipt survives reload failure. Per-list request generations prevent late reads from replacing newer writes or reward selections. Queue controls disable throughout a write, and delete consumes confirmation at its start.

Browser workflow tests cover delayed create, failed-edit retry, late-list suppression, lifecycle/delete receipts, and fulfill/cancel receipts with unavailable reloads. These strengthen the live operations Seam without moving rule-editor behavior into the reward queue.

Verification for the reward operation slice passed: `make verify` (95 frontend tests and the Go suite), all seventeen `make smoke-browser` checks, and `make smoke API_PORT=8081`. Browser fixtures perform no live Twitch writes.


## October 6 custom command execution ownership

`custom_commands.go` now owns saved-command snapshots, live replacement, role checks, template/API/community/media dependencies, response modes, event decisions, and atomic custom cooldown reservations. `CommandProcessor` retains its public constructor/Handle/SetCustomCommands interface and delegates resolved custom identities to this executor after community dispatch. Custom state uses its own mutex and cooldown map; aliases and built-in reservations remain in the dispatcher. Shared diagnostic constructors are pure package helpers.

Existing response-mode, alias, role, empty-response, unknown-variable, dependency, concurrency, and HTTP audit contracts continue to exercise the public processor. A new concurrent reload workflow verifies one in-flight configuration snapshot, caller-slice isolation, cooldown preservation across replacement, independent built-in cooldowns, exact expiry, new response-mode delivery, and removal. Community/built-in extraction remains the next command architecture work; this slice closes custom rendering ownership only.

Validation: `make verify`, all 17 browser smoke workflows, Twitch/HTTP race tests with the local Postgres fixture URL, and `make smoke API_PORT=8081` passed. Live Twitch/OBS integration remains unverified.


## October 6 command dispatcher closeout

`commands.go` is now the public parsing/alias/dispatch Adapter. `community_commands.go` owns role-gated counter/quote storage operations and their outcomes. `builtin_commands.go` owns the fixed action allowlist, validation, permission decisions, and per-command global cooldown state. `custom_commands.go` retains independent saved-command/template/delivery/cooldown ownership. `command_events.go` contains pure diagnostic and Unicode truncation helpers shared by all executors. Permission decisions use `bot.RoleAllows` through the shared chat actor role conversion.

Dispatch preserves community precedence, then custom lookup, then built-in/unsupported handling. Existing public processor and HTTP audit contracts remain applicable; added compatibility tests prove zero-value processor behavior, alias/custom configuration on that value, community precedence over a same-name custom record, alias-shared built-in cooldowns, and independent community/custom availability during a built-in cooldown. This closes the command-family extraction from implementation-order item 5; runtime/audit policy deepening remains documented in the earlier bounded asynchronous audit and shared role-policy work.

Validation: `make verify`, all 17 browser smoke workflows, Twitch/HTTP race tests with the local Postgres fixture URL, and `make smoke API_PORT=8081` passed. Live Twitch/OBS behavior remains unavailable for the known external setup.


## October 6 template dependency ownership

`bot.TemplateRenderer` now owns referenced media, counter, random-quote, and API-variable assembly behind read-only interfaces. Custom execution supplies actor, channel, arguments, and resolved command identity; it no longer assembles dependency maps. Timer rendering uses the same entry point with its existing channel/command snapshot and no external adapters. `RenderTemplate` remains available for already assembled snapshots.

Dependencies are loaded only when referenced, repeated normalized names share one lookup, and API-variable discovery is bounded to three names before invoking its resolver. Counter failures remain zero, missing quotes/API values remain empty, and API fallback values continue to render without diagnostic details. Rendering copies supplied maps before merging live values, does not recursively interpret dependency values, caps responses at 400 Unicode characters, and sorts unknown-variable diagnostics for stable audit output. No timer network fetches or new template syntax were introduced.

Direct offline workflow tests cover combined resolution, unused dependencies, missing/error/fallback behavior, context isolation, API limits, literal returned values, and the shared cap. Existing command reload/cooldown/audit contracts cover the integration. Validation: `make verify`, all 17 browser smoke workflows, Bot/Twitch/HTTP race tests with isolated Postgres fixtures, and `make smoke API_PORT=8081` passed. Frontend preview uses its existing local snapshot; unifying that separate TypeScript implementation remains future work.


## October 6 moderation rule policy seam

`moderation_rules.go` now owns rule types, normalization, target selection, enabled/expiry policy, and match outcomes. `Store` retains SQL CRUD, scans, rollback persistence, and its dry-run adapter. Each rule batch uses a single time snapshot, and a rule expires exactly at its deadline. This removes policy-heavy moderation behavior from the storage module while preserving its public API.

`ModerationPattern` owns literal/regex case behavior and is shared by saved-rule matching and nuke history previews. Regex matching now preserves original input instead of lowercasing it first; the former behavior broke explicit case-sensitive groups within an otherwise insensitive pattern. Compiled patterns are reused across message/username matching, and nuke previews compile once per history scan.

A failing-before-fix regression demonstrates the inline-case defect. Direct tests cover targets, paused rules, exact expiry, Unicode regex folding, literal metacharacters, explicit flags, and invalid regexes. The nuke contract verifies the same inline flag behavior. A disposable Postgres rule matched uppercase `ABC` and rejected lowercase `abc` through the public dry-run API, then was deleted. Validation passed: `make verify`, all 17 browser smoke workflows, Bot/Twitch/HTTP race tests with isolated Postgres fixtures, and runtime smoke at port 8081. Live enforcement remains unverified without Twitch credentials.


## October 6 frontend transport closeout

`apiRequest.ts` now owns the single fetch implementation used by every operation in `api.ts`: base URL selection, HTTP methods, trimmed operator/webhook credentials, JSON body encoding, HTTP failure details, optional response bodies, and JSON parsing errors. Public operation names/types and their encoded paths remain intact. OBS, readiness/build/history, sessions, dashboard config, login, overlay actions, and webhook calls now use the same transport as bot and reward operations. Duplicate `postJSON`/`sendJSON` implementations were removed.

Previously several reads discarded backend error details, while malformed successful JSON surfaced an unlabelled parser error. Reads now retain operation/status/details, and parse failures identify the operation without exposing the malformed response. No-content writes skip JSON parsing, and webhook calls use only webhook credentials. Existing status-only bot reads retain their explicit detail policy.

Request-contract tests cover public/protected reads, encoded scene/session/source parameters, bounded history limits, session/login receipts, empty writes, separate credential headers, HTTP detail/body-read failures, and malformed JSON. `make verify` passed with 111 frontend tests, all 17 browser smoke workflows passed, and runtime smoke passed at port 8081. The collaborative browser loaded Health and Sessions against the disposable Postgres API, including readiness/build/history/session data and the expected unavailable Twitch/OBS state. Product-family request ownership remains in the dashboard hooks; this closes the remaining duplicate transport and response policy.


## October 6 audit payload policy ownership

`audit.go` now owns the audit record type, field normalization, and complete-object payload validation; `Store` keeps SQL persistence and reads. `json_policy.go` supplies one token-based secret-key scanner for audit data and timer JSON text. This scanner checks every object member before duplicate-key normalization can discard earlier nested content. Safe duplicate members retain normal Postgres JSONB semantics; literal words in string values remain allowed.

Failing-before-fix tests reproduced overwritten nested secret keys and acceptance of trailing JSON/text. The validator now requires one complete valid JSON object and rejects secret-looking keys, including escaped keys and nested arrays. Errors omit payload content. An isolated Postgres test confirms rejected writes create no rows and safe duplicate members round-trip through JSONB. `make verify` passed with 113 frontend tests, all 19 browser workflows passed, Bot/HTTP/Twitch race tests with Postgres fixtures passed, and rebuilt runtime smoke passed at port 8081.


## October 6 bot configuration policy ownership

`configuration.go` now owns command, timer, counter, and quote types and normalization, including safe names, role/action/mode allowlists, reserved command names, duration/text limits, and timer secret-key policy. `Store` invokes those functions at persistence boundaries and retains only storage behavior for these records. Public types/functions and behavior are unchanged. This closes the remaining configuration policy in finding 3 without adding pass-through repository abstractions.

Existing offline normalization and isolated Postgres CRUD contracts exercise the same public interfaces after the move. Validation: `make verify`, all 21 browser smoke workflows, and Bot race tests with isolated Postgres fixtures passed.


## October 6 resumed fresh-database verification

Recreating the disposable stack after the server restart exposed test setup assumptions. Cache and session-audit integration tests now use owned schemas; shared fixture initialization keeps `pgcrypto` in public under a cross-package transaction lock. A complete race run against an unmigrated database passed and left no product tables or temporary schemas. Full verification, 23 browser workflows, and local runtime smoke passed. [Recorded evidence](../../qa/2026-10-06-fresh-postgres-test-isolation.md) distinguishes this new fixture run from the unavailable earlier temporary data and live integration gates.
