# Development state and roadmap reconciliation

Observed October 13, 2026 in worktree `t3-3769f907`, at main `55327be92482eb7ed3edca4b4e823129057ebd03`. The local branch is `chore/assess-development-state`. Remote main matched this SHA during the assessment.

## Current evidence

- `make verify` passed: Go vet/tests, TypeScript checks, ESLint, 116 frontend unit tests, API/probe/frontend builds, Compose validation and Open Pilot template preservation.
- `E2E_BASE_URL=http://127.0.0.1:45173 make smoke-browser` passed all 26 Chromium workflows against an isolated Vite server. The initial default-URL attempt had no frontend listening. The suite uses API fixtures; it does not prove live Twitch/OBS or backend database integration.
- Gitea exact-head `template-check / verify (push)` succeeded in 43 seconds at the assessed SHA. [Job evidence](https://git.subcult.tv/subculture-collective/overlaid/actions/runs/14738/jobs/27114).
- An isolated native API returned `/healthz` 200 and `/version` with the assessed commit. `/readyz` returned HTTP 200 with degraded JSON for missing Twitch configuration, unreachable OBS and missing nonlocal webhook secret.
- With `EXPOSURE_MODE=public` and no `WEBHOOK_SECRET`, that isolated API accepted a synthetic unauthenticated webhook POST with HTTP 202. No production route or data was used. This is tracked in #38.
- The working tree remained clean after the assessment; temporary API/frontend processes were stopped.

`OVERLAID_TEST_DATABASE_URL` was unset, so persistence integration tests were skipped. No fresh race suite, container/restore drill, live Twitch grant, actual OBS installation or deployed production target was qualified here. October 6 evidence remains linked from the earlier acceptance records and retains its date.

## Issue and milestone decisions

Issues #3–9 have completed implementation criteria on main. #4–9 are closed with code/test evidence; #3 was already closed. The now-playing pipeline test covers title/artist updates, paused/stopped/empty state and restart.

Issues #10–12 have completed ADR drafting criteria and remain open for human review of six Proposed decisions. Remove automatic-pickup readiness from those review issues. Phase 0 remains open for architecture acceptance and #38. Phase 1 has not started; phases 2–11 remain planned scope.

New issues cover gaps absent from the existing tracker:

- #38: fail-closed webhook ingestion in LAN/public modes, in Phase 0.
- #39: actual Twitch OAuth/EventSub/chat/moderation/reward acceptance.
- #40: actual OBS rendering, keyed source delivery and control acceptance.
- #41: fresh disposable-Postgres persistence/race checks in hosted CI.
- #42: first self-host release with exact-revision target-host recovery/monitoring/rollback proof.

Issues #39–42 belong to [Single-channel release qualification](https://git.subcult.tv/subculture-collective/overlaid/milestone/154). Existing #21 continues to cover the future hosted service's process-role operations and public status page. Existing tenancy, platform, engine and external-registration issues retain their scope rather than receiving duplicates.

## Development organization

The [development project](https://git.subcult.tv/subculture-collective/overlaid/projects/4) groups all roadmap issues into Backlog, Ready, In progress, Review, External acceptance and Done. Ready holds #38 and #41; Review holds #10–12; External acceptance holds #39, #40 and #42; Done holds #3–9. The remaining open program issues are Backlog. No work is claimed in progress merely because it appears on the board.

The [roadmap issue](https://git.subcult.tv/subculture-collective/overlaid/issues/34) and canonical [multi-platform roadmap](../superpowers/plans/2026-10-05-multiplatform-parity-roadmap.md) retain the phase order and distinguish implementation, architecture review, fixture acceptance and real-service/release acceptance. Issue planning does not authorize deployment, tagging, release publication or live moderation.
