# Multi-platform Phase 0 acceptance evidence

Observed 2026-10-06 in worktree `t3-9734b305`, branch `feat/implement-roadmap`. Scope is the October 5 roadmap, issues #3–7 and #9, plus six Proposed ADR drafts for #10–12. This is separate from the June FossaBot acceptance record.

## Implemented criteria

| Issue | Behavior and evidence |
| --- | --- |
| #3 | Stream/display validate the separate overlay query key; history and token retrieval require operator headers. LAN/public missing credentials fail closed and readiness reports missing overlay token. History projection removes raw Twitch payloads without mutating legacy stored events. Unit tests cover wrong/missing keys, header boundary, readiness and missing storage; browser tests cover keyed source URLs and offline demos. |
| #4 | Successful Helix send IDs enter a 1,024-entry, two-minute cache. EventSub ignores them before command parsing/publication/moderation; publication also checks the cache defensively. Tests generate a custom `!clear` reply, ignore its echo, retain manual broadcaster `!clear`, check sent-versus-dropped receipts and cache expiry/eviction. |
| #5 | Rules migrate to moderator exemption and 60-second timeout defaults. Role thresholds and 1–1,209,600 second durations are validated; eligible enforcement uses the configured timeout. Dry run labels exempt matches. Save/edit audit receipts include policy metadata. Browser test edits the same rule ID, displays policy and exemption note; PostgreSQL test applies the migration twice to legacy rows and checks defaults, updates and reload. |
| #6 | Config, Compose, example and README contain the same scope sequence, including both moderation scopes. Config test checks parity; README requires renewed login/consent for existing grants. |
| #7 | Read-only keyed endpoint returns only shared display settings. Alerts/chat/ticker fetch on load and status changes/reconnect; abort prevents stale responses. Demo and unavailable API use built-in defaults rather than browser storage. Browser test proves shared values override localStorage, reconnect refreshes, Save shared plus reload changes limits, and unavailable API restores defaults. |
| #9 | Chat command and bot operations runbooks describe custom outbound replies and current reward workflows. Event-history runbook now documents protected reads; overlay and README examples carry keys. |
| #10–12 | ADRs 0003–0008 cover tenancy/RLS/migration, adapters/capabilities/idempotency, engine/traces/replay, variable grammar/sandbox budgets, River/Postgres fan-out/revisit thresholds and supported self-host bootstrap. All are Proposed and linked from the ADR index. |

## Verification

- `make verify`: passes backend tests/vet, TypeScript checks, ESLint, 116 frontend unit tests, both backend binaries, production frontend build, Compose validation and Open Pilot template preservation.
- `make smoke-browser`: all 26 Chromium browser tests pass, including all ten demo overlays and the new access/settings/moderation workflows.
- Backend `go test -race ./... -count=1` with `OVERLAID_TEST_DATABASE_URL` against a fresh owned PostgreSQL database: passes all packages. The migration test also passes the race-tested bot suite; the final audit metadata change passes the HTTP race suite.
- Owned integration database cleanup checks: zero public application tables, zero owned test schemas, `pgcrypto` retained in public. The disposable database is removed after verification.
- `make smoke API_PORT=8081`: passes after restarting the local API with the new binary. Overlay display/token endpoints return their expected shapes; readiness storage/process checks are healthy while missing Twitch credentials and unavailable OBS remain degraded.
- `git diff --check`: passes.

## Remaining gate

A human accepts ADRs through PR review before Phase 1 implementation. No platform credentials are required for the completed code or drafts. Live bot-account identity, production exposure, OBS and platform grants remain later deployment/integration checks; these tests do not claim them. Configuration plumbing/example defaults changed, but no actual secret or production deployment was edited. No remote issue was closed, branch pushed, PR merged or deployment performed.

The T3 shared preview had shown a different app after its restart; interactive native-preview verification is not claimed. Browser evidence here comes from the repository's existing smoke-test command against its Vite application.
