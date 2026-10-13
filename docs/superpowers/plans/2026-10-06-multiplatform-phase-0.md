# Multi-platform Phase 0 implementation plan

Scope: October 5 parity roadmap, Gitea issues #3–7, #9–12. Existing June work is a separate baseline. No live platform credentials are needed for these changes.

## Current closeout — October 13, 2026

The original implementation scope merged in PR #35. Issues #3–9 are complete, including the now-playing resolver tracked separately by #8. The repository owner accepted all six ADRs under #10–12 and closed those issues. The accepted statuses are now recorded in the files and ADR index.

The development assessment adds [#38](https://git.subcult.tv/subculture-collective/overlaid/issues/38), fail-closed webhook credentials in LAN/public modes, to Phase 0. The closeout change enforces that boundary and adds route-level tests for credential transports, exposure normalization, rejection before payload parsing, and absence of rejected-event state/history/publication. PR #44 merged this closeout at `a64ac84`; #38 and the Phase 0 milestone are closed. Phase 1 implementation is tracked separately.

Real Twitch/OBS, fresh persistence CI evidence, and first self-host release qualification are tracked separately in [milestone 154](https://git.subcult.tv/subculture-collective/overlaid/milestone/154), issues #39–42. See the [updated roadmap](2026-10-05-multiplatform-parity-roadmap.md) and [development project](https://git.subcult.tv/subculture-collective/overlaid/projects/4).

## Original implementation checklist

- [x] #3: overlay key configuration, authenticated streams/history, sanitized history copies, source URLs and access tests.
- [x] #7: display-only shared config endpoint, overlay load/reconnect fetch, demo/default fallback and tests.
- [x] #4: bounded TTL cache of successful outbound message IDs; drop echoes before command parsing and moderation; manual broadcaster commands remain usable.
- [x] #5: moderation role exemptions and timeout duration, schema migration, editor and dry-run evidence.
- [x] #6: matching scope defaults and re-consent instructions.
- [x] #9: correct chat reply and channel point runbooks.
- [x] #10–12: six Proposed ADRs covering tenancy, platform boundary, engine, variables, Postgres jobs/fan-out and self-host mode, with migration and alternatives.
- [x] Run `make verify`, browser smoke and fresh Postgres integration checks as applicable; review the diff and record acceptance evidence.

The owner has completed the required architecture review. The original code and drafts merged through PR #35; the webhook closeout is a separate reviewed change. Deployment and release acceptance remain separate.

## Acceptance and review

Code acceptance evidence is recorded in [Phase 0 QA](../../qa/2026-10-06-multiplatform-phase-0.md). The six ADRs are linked from [the ADR index](../../adr/README.md), all now Accepted. #10–12 are closed after owner acceptance. The Phase 1 implementation plan will apply these contracts.

The October roadmap and research were restored byte-for-byte from Gitea main for this older worktree: Git blob hashes `002bafe1091584ecd26c891f3d16993ae6720fa5` and `708165dcb9e8812f7a195e1d28b4e1e19c23e67b`. The June acceptance record now identifies its narrower historical scope.

## Webhook closeout verification

The route regression matrix first reproduced HTTP 202 for unauthorized nonlocal requests and HTTP 400 for malformed unauthorized input. After the shared-helper fix, those paths return 401 before decoding, retaining state, appending history or publishing events. Local empty credentials and configured custom-header/Bearer credentials remain supported, with existing header precedence.

`make verify` passes with 116 frontend tests. The affected HTTP webhook/overlay contracts also pass with the race detector. Persistence tests are not claimed because no disposable database URL was configured for this Phase 0 change. The independent security-boundary investigation and candidate review cover this narrow change; external Twitch/OBS acceptance remains #39–40.
