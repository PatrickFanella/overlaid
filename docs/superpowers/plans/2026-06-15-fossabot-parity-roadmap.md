# FossaBot Parity Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Expand over-laid from a stream operations cockpit into a Twitch bot platform with FossaBot-like timers, custom commands, variables, community utilities, channel-point workflows, and moderation, while keeping over-laid's OBS/overlay/session-report strengths.

**Architecture:** Build a small `bot` backend domain around Postgres-backed configuration, command/timer execution, permission checks, and audit logging. Existing Twitch EventSub command handling should route through this runtime instead of growing ad hoc. The frontend should add bot operator surfaces incrementally under the dashboard without making chat able to trigger arbitrary OBS or system actions by default.

**Tech Stack:** Go backend, Postgres migrations, React/Vite TypeScript dashboard, existing SSE/event-history/session-report pipeline, Docker Compose, `make verify`, `make smoke-browser`.

---

## Baseline comparison

FossaBot currently has strong public bot features: moderation filters/nukes, generic custom commands, variables, timers, counters/quotes/community utilities, channel-point reward workflows, audit logs, and cloud hosting.

over-laid currently has strong production-control features: OBS automation, browser overlays, Twitch EventSub ingestion, safe fixed chat commands, simple channel-point rules, dashboard quick actions, stream sessions, readiness snapshots, event history, one-click rehearsal, and self-hosted backups/runbooks.

The parity roadmap adds FossaBot-style bot breadth without weakening the existing over-laid safety model.

## Current status

Implemented and verified in committed slices:

- Bot audit storage, runtime permissions, existing Twitch command audit, and `/dashboard/bot` audit/status view.
- Timer CRUD, dashboard management, safe template rendering, enabled timer dispatch through ticker/alert events, and an audited Run once rehearsal action that preserves the schedule.
- Safe custom response commands with role/cooldown checks, template variables, dashboard management, and preview rendering.
- Guarded outbound Twitch chat replies for custom commands through `user:write:chat`, with per-command response mode `overlay`, `chat`, or `both`.
- Operator-configured custom API variables using `{api:name}` with GET-only HTTPS fetching, caps, caching, and private-host blocking.
- Counters and quotes with dashboard/API management, chat commands, `{count:name}` / `{quote:random}` variables, operator change audits, and session report/export coverage.
- Channel-point rule enrichment with cooldown seconds and fulfillment notes.
- Twitch reward inventory, reversible pause/disable lifecycle controls, guarded bounded reward fields, minimal reward creation/deletion, and unfulfilled redemption queue controls for fulfill/cancel actions.
- Moderation foundation: blocked-term rules, dry-run matching, opt-in automatic enforcement, guarded manual Twitch delete/timeout/ban/unban actions with required scopes, and nuke-style dry-run planning over recent chat history.

Deliberately carried forward:

- Arbitrary inline/custom API URLs supplied by chat or templates.
- Twitch reward fields still intentionally deferred: skip-request-queue, image/background styling, reward image upload, and any automatic fulfillment behavior. Common guarded fields now live in the reward queue runbook.
- Cloud-hosted uptime parity with FossaBot; see `docs/superpowers/plans/2026-06-16-fossabot-uptime-parity-options.md` before changing deployment architecture.

---

## October 5 overnight progress

The timer workflow now includes Run once for saved timers, including paused timers. Manual execution publishes the same safe ticker or alert event as the scheduler, records a dashboard/operator audit row with event and session IDs, and leaves the scheduled cadence unchanged. The dashboard shows the last scheduled run and refreshes audit after rehearsal and timer configuration changes. Create, update, pause, enable, and delete actions now record success or failure audit rows with safe scheduling metadata and active session ID.

Verification passed: `make verify`, `make smoke`, and all six `make smoke-browser` checks. Unit tests cover paused timers, missing timers, empty rendered responses, and audit-storage failure. Browser fixtures cover pending, success, audit refresh, and API failure without Twitch credentials.

Local verification uses native API and Vite processes on loopback, with disposable file data under `/tmp`. Docker socket access is unavailable, but rootless Podman runs a disposable Postgres 17 database on loopback port 55439. Real Postgres checks passed for timer lookup, unchanged paused configuration, event history, manual audit, missing/empty timer errors, active-session report linkage, and configuration-change audit coverage without response text. `make smoke API_PORT=8081` passed with database, bot runtime, dashboard config, and session readiness checks healthy. Live Twitch/OBS execution remains unverified; readiness reports missing Twitch configuration and unreachable OBS.

---

## October 6 session audit progress

Operator counter and quote create/update/delete operations now record successful and failed changes with the active session ID. Quote text and author content stay out of audit metadata. Session reports include bot actions filtered by session before applying the row limit, along with a total count. The dashboard and Markdown/JSON exports show the loaded rows and flag unavailable audit data.

Verification passed: `make verify` (93 frontend tests and the Go suite), all nine `make smoke-browser` checks, `make smoke API_PORT=8081`, and the optional Postgres audit integration test. Native HTTP checks covered seven counter/quote actions, safe metadata, bounded counts, and an unchanged older report after newer activity in another session. The collaborative browser displayed all seven actions from that real database report. Live Twitch and OBS verification still needs those services and credentials.

---

## October 6 API-variable network checks

Private-host blocking now applies at connection time as well as URL validation. DNS answers are validated together, then connections use approved numeric addresses. Redirects retain those checks, environment proxies are bypassed, and oversized or interrupted response bodies fail. Diagnostics omit request URLs and query values. Offline DNS/HTTP tests and a local TLS fixture cover the behavior without external API credentials.

---

## October 6 role-policy audit

Milestone A2's privileged-role requirement is now enforced: broadcaster, operator, and system actors can run every configured bot role. The previous numeric ranking incorrectly denied broadcasters operator/system commands and operators system commands. Tests cover all 25 actor/required-role pairs, unknown-actor normalization, and actual custom-command execution for each role. Viewer/moderator denial remains covered for operator/system commands. HTTP operator authentication is separate from this bot command policy.

Verification passed: bot/Twitch race tests, `make verify` (93 frontend tests and the Go suite), all fourteen browser smoke checks, and `make smoke API_PORT=8081`.

---

## October 6 unavailable audit storage and template diagnostics

Server initialization now supplies a nil audit recorder when bot storage is unavailable, avoiding a typed-nil store panic while publishing Twitch command diagnostics. Local tests drive `!alert`, `!clear`, `!brb`, and `!back` through command processing and event publication without Postgres. Another fixture verifies that an audit write failure retains command delivery and exposes the audit error through runtime diagnostics.

Custom-command audit now retains the renderer's unknown-variable names. A command-to-audit test confirms that unknown placeholders render empty, diagnostics retain the active session ID, and injected token/raw EventSub fields stay out of audit data.

Verification passed: focused HTTP race tests, `make verify` (93 frontend tests and the Go suite), all fourteen browser smoke checks, Postgres cache/session-audit tests, and API smoke checks both with and without Postgres (`API_PORT=8081` and `API_PORT=8080`).

---

## October 6 scheduled timer and audit persistence contracts

Scheduler tests now cover periodic polling, publication before cadence/audit writes, ticker/alert output, empty rendered responses, active-session/event audit linkage, and storage failures. Cancellation after a publication stops the rest of the loaded batch; already-canceled or unconfigured schedulers do no work. The previous loop could publish the remaining timers after cancellation.

Postgres tests use private temporary schemas, leaving the running API's tables and worker untouched. They prove newest-first audit ordering with tied timestamps, safe metadata round trips, rejection before persistence, exact timer interval boundaries, paused/deleted timers, saved interval changes, and bounded due ordering. An end-to-end scheduler fixture publishes to the event bus and history, persists linked session audit, marks cadence, and dispatches the next run at the interval boundary.

Verification passed: the complete backend race suite with optional Postgres tests enabled, `make verify` (93 frontend tests and the Go suite), all fourteen browser smoke checks, and `make smoke API_PORT=8081`.

---

## File structure

### Backend domain

- Create `backend/internal/bot/store.go` — Postgres-backed bot config and audit storage.
- Create `backend/internal/bot/runtime.go` — permission checks, command/timer execution decisions, audit event construction.
- Create `backend/internal/bot/templates.go` — safe template variables for response commands.
- Create `backend/internal/bot/timers.go` — timer scheduler state and due-timer execution.
- Modify `backend/internal/http/server.go` — initialize bot store/runtime and wire it into Twitch/EventSub and HTTP handlers.
- Modify `backend/internal/http/routes.go` — add operator-protected bot endpoints.
- Create `backend/internal/http/bot_handlers.go` — bot command/timer/audit API.
- Modify `backend/internal/twitch/commands.go` and `backend/internal/twitch/eventsub.go` — route fixed Twitch commands and future custom commands through bot runtime.
- Modify `migrations/0001_init.sql` — add bot tables.

### Frontend dashboard

- Create `web/src/routes/dashboard/Bot.tsx` — bot overview, audit log, command/timer entry points.
- Create `web/src/routes/dashboard/components/BotAuditPanel.tsx`.
- Create `web/src/routes/dashboard/components/BotCommandsPanel.tsx`.
- Create `web/src/routes/dashboard/components/BotTimersPanel.tsx`.
- Modify `web/src/lib/api.ts` — bot API client types/functions.
- Modify `web/src/App.tsx` and route metadata files — add `/dashboard/bot`.

### Docs/tests

- Create `docs/runbooks/bot-ops.md` — bot commands/timers/moderation operation guide.
- Modify `docs/runbooks/twitch-chat-commands.md` — point fixed commands at bot runtime.
- Add backend tests under `backend/internal/bot/*_test.go` and `backend/internal/http/*_test.go`.
- Add frontend tests in `web/src/App.test.tsx` for bot dashboard panels.

---

## Milestone A: Bot Core

Build the spine before adding feature breadth.

### Task A1: Persist bot runtime plan and schema

**Files:**
- Modify: `migrations/0001_init.sql`
- Create: `backend/internal/bot/store.go`
- Test: `backend/internal/bot/store_test.go`

- [ ] Add tables:
  - `bot_audit_events(id, created_at, source, actor_id, actor_login, actor_display_name, actor_role, trigger, command, action, result, reason, event_id, stream_session_id, data jsonb)`
  - `bot_commands(id, name, enabled, response, action, role_required, cooldown_seconds, created_at, updated_at)`
  - `bot_timers(id, name, enabled, interval_seconds, response, action, created_at, updated_at, last_run_at)`
- [ ] Implement `bot.Store` with `NewPostgres(databaseURL string)`, `Load()`, `RecordAudit(ctx, AuditEvent)`, `RecentAudit(ctx, limit)`, and `Close()`.
- [x] Add tests proving audit events are inserted newest-first and secret-looking keys are rejected from audit `data` (October 6: isolated Postgres round trip, bounded ordering, ties, and recursive rejection).
- [ ] Run `cd backend && go test ./internal/bot`.
- [ ] Run `make verify`.
- [ ] Commit: `feat: add bot audit storage`.

### Task A2: Bot runtime roles and audit model

**Files:**
- Create: `backend/internal/bot/runtime.go`
- Test: `backend/internal/bot/runtime_test.go`

- [x] Define roles: `viewer`, `moderator`, `broadcaster`, `operator`, `system`.
- [x] Define sources: `chat`, `channel_point`, `dashboard`, `timer`, `webhook`.
- [x] Implement role comparison so broadcaster/operator/system can run everything, moderator can run moderator-safe commands, viewer can only run viewer-safe response commands (October 6: full role matrix and custom-command execution tests).
- [ ] Implement `Runtime.Record(ctx, AuditEvent)` wrapper that never blocks command execution if audit storage is unavailable.
- [ ] Test allowed/denied role decisions and audit fallback behavior.
- [ ] Run `cd backend && go test ./internal/bot`.
- [ ] Commit: `feat: add bot runtime permissions`.

### Task A3: Route existing Twitch commands through bot audit

**Files:**
- Modify: `backend/internal/twitch/commands.go`
- Modify: `backend/internal/twitch/eventsub.go`
- Modify: `backend/internal/http/server.go`
- Test: existing Twitch tests plus new command audit tests.

- [ ] Keep current behavior for `!alert`, `!clear`, `!brb`, and `!back`.
- [ ] Emit bot audit events for allowed, denied, cooldown, and invalid command attempts.
- [ ] Include active `sessionId` when a stream session is active.
- [ ] Do not expose Twitch tokens or raw EventSub payloads in audit data.
- [ ] Run `cd backend && go test ./internal/twitch ./internal/http ./internal/bot`.
- [ ] Commit: `feat: audit twitch bot commands`.

### Task A4: Bot status/audit API and dashboard visibility

**Files:**
- Modify: `backend/internal/http/routes.go`
- Create: `backend/internal/http/bot_handlers.go`
- Modify: `web/src/lib/api.ts`
- Create: `web/src/routes/dashboard/Bot.tsx`
- Modify: `web/src/App.tsx`
- Test: backend HTTP tests and frontend route tests.

- [ ] Add `GET /api/bot/status` with storage readiness, command count, timer count, and recent audit count.
- [ ] Add operator-protected `GET /api/bot/audit?limit=50`.
- [ ] Add `/dashboard/bot` route with a bot overview and recent audit panel.
- [ ] Link Bot route from dashboard navigation/route guide.
- [ ] Run `make verify` and `make smoke-browser`.
- [ ] Commit: `feat: add bot dashboard audit view`.

---

## Milestone B: Timers

### Task B1: Timer CRUD API

**Files:**
- Extend: `backend/internal/bot/store.go`
- Extend: `backend/internal/http/bot_handlers.go`
- Extend: `web/src/lib/api.ts`
- Test: `backend/internal/bot/store_test.go`, HTTP handler tests.

- [ ] Implement list/create/update/delete for timers.
- [ ] Validate timer interval minimum 30 seconds.
- [ ] Validate timer response/action cannot contain secret-looking keys.
- [x] Record audit events for timer create/update/pause/enable/delete and failed configuration changes.
- [ ] Commit: `feat: add bot timer api`.

### Task B2: Timer runtime execution

**Files:**
- Create: `backend/internal/bot/timers.go`
- Modify: `backend/internal/http/server.go`
- Test: `backend/internal/bot/timers_test.go`

- [x] Add a scheduler that polls due enabled timers (October 6: polling and Postgres cadence tests).
- [x] Publish timer output into existing event pipeline as safe overlay/ticker/alert events (October 6: real bus/history scheduler fixture).
- [x] Record audit event for each timer execution (October 6: persisted event/session links and template diagnostics).
- [x] Keep scheduler disabled when bot storage is unavailable (startup guard and scheduler dependency checks).
- [ ] Commit: `feat: run bot timers`.

### Task B3: Timer dashboard

**Files:**
- Create: `web/src/routes/dashboard/components/BotTimersPanel.tsx`
- Modify: `web/src/routes/dashboard/Bot.tsx`
- Test: `web/src/App.test.tsx`

- [x] Add timer list, enable/disable, and a saved-timer interval/response/action editor with cancellation and retry.
- [x] Add a manual “Run once” operator action backed by `POST /api/bot/timers/{id}/run`.
- [x] Show last scheduled run time and refresh the audit after manual execution.
- [ ] Commit: `feat: manage bot timers`.

---

## Milestone C: Safe custom commands

### Task C1: Command CRUD API

**Files:**
- Extend: `backend/internal/bot/store.go`
- Extend: `backend/internal/http/bot_handlers.go`
- Test: backend bot and HTTP tests.

- [ ] Implement list/create/update/delete for response commands.
- [ ] Enforce command names matching `^[a-z0-9][a-z0-9_-]{0,31}$`.
- [ ] Support fields: `enabled`, `response`, `roleRequired`, `cooldownSeconds`.
- [ ] Do not allow custom commands to trigger OBS actions in this milestone.
- [ ] Commit: `feat: add custom command api`.

### Task C2: Template variables

**Files:**
- Create: `backend/internal/bot/templates.go`
- Test: `backend/internal/bot/templates_test.go`

- [ ] Implement safe variables: `{user}`, `{displayName}`, `{channel}`, `{args}`, `{command}`, `{nowPlaying.title}`, `{nowPlaying.artist}`.
- [x] Unknown variables render as empty strings and are recorded in audit data (October 6: command-to-audit test and session linkage).
- [ ] Limit rendered response length to 400 characters.
- [ ] Commit: `feat: render bot command templates`.

### Task C3: Chat execution for custom response commands

**Files:**
- Modify: `backend/internal/twitch/eventsub.go`
- Modify: `backend/internal/twitch/commands.go`
- Test: Twitch command tests.

- [ ] Resolve custom command before reporting unknown command.
- [ ] Enforce enabled flag, role, and cooldown.
- [ ] Publish response as a safe overlay/ticker/chat diagnostic event first; outbound chat replies can be a later milestone.
- [ ] Record audit event for success/denial/failure.
- [ ] Commit: `feat: run safe custom chat commands`.

### Task C4: Command dashboard

**Files:**
- Create: `web/src/routes/dashboard/components/BotCommandsPanel.tsx`
- Modify: `web/src/routes/dashboard/Bot.tsx`
- Test: frontend tests.

- [x] Add command list and saved-record editor (October 6: edit/cancel/retry with existing ID and pending controls).
- [x] Add test-render preview for template variables.
- [x] Show cooldown/role fields.
- [ ] Commit: `feat: manage custom bot commands`.

---

## Milestone D: Counters and quotes

- Add Postgres tables `bot_counters` and `bot_quotes`.
- Add dashboard panels for counters and quotes.
- Add commands: `!count`, `!addquote`, `!quote`, `!delquote` gated by role.
- Add template variables `{count:name}` and `{quote:random}`.
- [x] Include counter/quote actions in audit/session reports (October 6: operator CRUD audit, session filtering before row limits, loaded/total counts, and JSON/Markdown exports).

---

## Milestone E: Channel points parity

- Expand exact-title rules into richer reward mappings.
- Add per-reward cooldowns and templates.
- Reward queue shows fulfillment notes alongside selected rewards and matching redemptions.
- Twitch reward management has minimal create/delete controls after scoped API research; advanced fields and safer disable/pause lifecycle remain follow-up.

---

## Milestone F: Moderation

- Add blocked-term rules and dry-run matching.
- Add Twitch moderation API actions: delete message, timeout, ban only with explicit scopes.
- Add dashboard moderation audit.
- “Nuke” workflow has regex/plain matching, lookback window, dry-run preview, guarded delete/timeout execution, audit rows, optional temporary radiation/future-enforcement windows, and rollback that disables those windows.

---

## Verification policy

Every committed slice must pass:

```bash
make verify
make smoke-browser
```

Runtime-affecting slices must also pass:

```bash
make smoke
curl -fsS http://localhost:8080/readyz | jq .
```

Do not push or deploy unless explicitly requested. Do not log or commit secrets.
