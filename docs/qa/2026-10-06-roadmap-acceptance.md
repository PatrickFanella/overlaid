# Roadmap acceptance map, October 6, 2026

Initial source audited: `f094155`; Unicode reward validation was verified in `a723719`. The profile browser acceptance addendum is committed with the current update. This record distinguishes local implementation/contract acceptance from real-service and production acceptance. It does not mark the overnight goal or live deployment complete.

## FossaBot parity requirements

The [parent roadmap](../superpowers/plans/2026-06-15-fossabot-parity-roadmap.md) defines milestones A–F. Every implementation checkbox has current code and behavior evidence:

| Requirement | Current implementation and inspected evidence |
| --- | --- |
| A1: durable audit schema/store, newest-first reads, secret-key rejection | `migrations/001_init.sql`, `bot/store.go`, and `bot/audit.go`; isolated Postgres audit round trips/order/bounds/validation in `bot/store_integration_test.go`, and payload-policy tests in `bot/store_test.go`. |
| A2: five roles/sources, permissions, nonblocking audit fallback | `bot/runtime.go` implements role policy, outcome construction, bounded FIFO writes, overflow readiness, and flush. `runtime_test.go` covers the role matrix, absent/failed/blocked storage, snapshots, queue order/capacity, cancellation, and shutdown. |
| A3: preserve fixed Twitch commands, record outcomes/session metadata without raw secrets | Built-in dispatcher/executor and `http/bot_audit_test.go` cover allowed, denied, invalid, unsupported, cooldown, alias, event/session links, history-before-audit, and delivery while audit is blocked. |
| A4: operator status/audit API, counts, linked dashboard | `http/routes.go`, bot handlers, Chat/BotWorkspace and route metadata. Auth/unavailable-storage contracts cover the API; browser routes/status/audit and session export workflows cover the operator surface. |
| B1: timer CRUD, interval/action/text safety, configuration audits | Configuration normalization plus Store CRUD; boundary/duplicate/escaped JSON-key tests and isolated Postgres timer CRUD/rejected-write preservation. HTTP audit fixtures cover safe configuration metadata. |
| B2: due enabled scheduling, safe publication, every-attempt audit, unavailable-store guard | `bot/timers.go` and scheduler tests cover polling/cancellation/publication, storage failures, empty attempts, cadence/fairness, and persisted event/session audit links. |
| B3: saved editor, enable/pause, Run once without schedule mutation, last-run/audit | `useBotTimers`, timer panel, manual runner, paused-timer unit/Postgres fixtures, and browser edit/retry/run-once workflows. |
| C1: command CRUD/name bounds/fields, no arbitrary OBS actions | Shared configuration policy and isolated Postgres CRUD prove normalized 1–32 safe names, field/identity preservation, enable/pause/delete, missing IDs, and rejection of OBS actions. |
| C2: actor/channel/args/command/media variables, unknown diagnostics, Unicode cap | TemplateRenderer and command-media integration; dependency/unknown-variable/context-isolation/literal-value tests and browser preview verify 400 Unicode characters. |
| C3: custom dispatch before unknown handling, enabled/role/cooldown, delivery/audit | Custom executor and public command contracts cover response modes, empty-render failure, atomic cooldown reservation, reload snapshots, alias identity, and HTTP audit outcomes. Outbound chat uses a separate scope-gated Helix adapter. |
| C4: command editor/preview/role/cooldown | `useBotCommands` and command panel; browser workflow verifies saved editing, pending controls, failed draft retry, preview, and receipts after refresh failure. |
| D: durable counters/quotes, role-gated commands, variables, audit/session/export | Migration and isolated concurrent counter/CRUD tests, community permission-before-storage tests, template dependency tests, HTTP operator audit fixtures, and browser CRUD/session export workflows. |
| E: richer reward mapping, cooldown/templates, fulfillment notes | Stable-ID matching with title fallback, client/broadcaster/reward-isolated atomic cooldowns, notification templates, persisted rules, and browser renamed-reward/editor/queue-note contracts. |
| F: rule matching/CRUD, scoped moderation, operator audit, guarded nuke/radiation/rollback | Moderation policy/workflow contracts cover targets, expiry, regex flags, scoped action/outcome matrix, validation before side effects, history/lookback/caps, confirmation, partial cancellation/failures, persisted radiation and rollback. Browser workflows cover stale-plan invalidation, confirmations, pending controls, receipts, retries, and session audit export. |

Postgres-enabled tests create/drop their own schemas. The complete backend race run at this source used `OVERLAID_TEST_DATABASE_URL` and passed all packages; it did not silently substitute in-memory tests for persistence gates.

## Follow-on and operator gates

| Plan/gate | Evidence and scope |
| --- | --- |
| Phase 2 and advanced rewards | Shared Helix contracts and browser lifecycle/write/redemption tests cover pause/resume, enable/disable, guarded cooldown/max limits, prompt/input fields, manual fulfillment/cancel, and delete confirmation. Cooldown 604800/604801 dispatch bounds are tested. Reward title/prompt limits count Unicode code points after trimming, with CJK/emoji create and edit boundaries covered in API and browser tests. Live Twitch writes remain unverified. |
| Architecture deepening | The plan's current implementation map identifies Helix, moderation, configuration policy, templates/API resolver, command executors/runtime, feature hooks, reward split, and shared transport. Direct contract tests cover those seams. Historical “next” notes are checkpoint history. |
| Operator workflow/layout/session reports | Browser tests cover confirmed sequential actions, failure/retry, pending report reads, accurate failed-report receipts, partial-route layout preservation/reset, and demo overlays. Postgres tests cover lifecycle serialization/cancellation and coherent report snapshots under concurrent writes. |
| Local automation profiles | Browser import/export/switch/reload, Scenes action filtering, Overlays preset filtering, hidden-item preservation, invalid JSON preservation, missing-reference warnings/fallback, and secret exclusion/no API writes close the [Plan 7 local interaction gate](../superpowers/plans/2026-06-13-plan-7-automation-profiles.md). Personal OBS/live-show use remains external. |
| Readiness monitoring | Probe CLI tests, selected/full readiness checks, signal behavior, native startup measurement, and cross-compilation are recorded. No persistent alert endpoint or deployment was installed. |
| Process recovery | [Local restart drill](2026-10-06-local-recovery-drill.md): outage/recovery probe transition, exact retained replay/report, restored active-session tags, start/end snapshots, zero pipeline drops, cleanup. |
| Backup/restore | [Isolated restore](2026-10-06-local-backup-restore-drill.md) and [actual Make recipes](2026-10-06-backup-make-targets.md): exact restored product responses/audits, corrected absolute paths, valid nonempty custom dumps, and fixture row restoration. |
| Image/runtime packaging | [Container drill](2026-10-06-container-packaging.md): Go 1.26 image build/runtime, Nginx routes/assets/headers, UID 100, root-owned volume repair, and retained restart evidence. |
| Compose wiring/exposure | [Compose drill](2026-10-06-compose-wiring.md): health dependencies, product service DNS/storage, named volumes, restart/report persistence, exposure forwarding and nonlocal secret warnings. |
| Current routes/styles/user notes | `dashboardRoutes.ts`, Overlays/Settings/Scenes/Health/Sessions, stylesheet imports, and browser route/demo checks are authoritative. Original notes now describe current primary routes and preserve legacy/design history. |

## Verification record

At the audited source, required verification passed: `make verify` (113 frontend tests, Go vet/tests, backend/probe/frontend builds, Compose config, and Open Pilot templates), all 23 `make smoke-browser` workflows, and `make smoke API_PORT=8081`. The complete Postgres-enabled `go test -race ./... -count=1` passed before the Unicode change. The affected HTTP package race suite passed again with Postgres enabled after that change. Packaging, Compose, backup/restore, and native browser checks are linked above; their engine/environment qualifications remain part of the evidence.

The official [Twitch reward reference](https://dev.twitch.tv/docs/api/reference#create-custom-rewards) defines a 45-character title and 200-character prompt limit. Local validation treats a Unicode code point as a character; a combining mark counts separately. This corrects the previous API byte count and browser UTF-16 input cap. Live Twitch acceptance of Unicode boundary text is still part of the external reward lifecycle gate.

## Remaining gates and deliberate exclusions

- Actual Twitch OAuth/re-consent, EventSub delivery/reconnect, outbound chat, scoped moderation, and reward/redemption lifecycle need a configured broadcaster/app with the intended scopes.
- Actual OBS scene/source control, browser-source rendering/reload, and host networking need the operator's OBS instance and collection.
- Production Docker build/deployment, reverse-proxy/auth/exposure, real backup/credential restore, and deployed external monitoring need the actual target and release/deployment authorization. Local Podman evidence does not claim those environments.
- Arbitrary chat/template-supplied API URLs, reward queue skipping/automatic fulfillment, reward image/color styling, and hosted control-plane architecture are explicitly carried forward, not missing approved implementation. Optional theme/docking/resizing/shared layout expansion is also deferred by its source plans.

Current loopback readiness keeps product storage/runtime checks healthy and reports expected missing Twitch configuration and unreachable OBS. These are real external acceptance gaps; the local implementation and fixtures cannot establish that live services work.
