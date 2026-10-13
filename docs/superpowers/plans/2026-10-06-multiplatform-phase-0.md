# Multi-platform Phase 0 implementation plan

Scope: October 5 parity roadmap, Gitea issues #3–7, #9–12. Existing June work is a separate baseline. No live platform credentials are needed for these changes.

## Current closeout — October 13, 2026

The original implementation scope merged in PR #35. Issues #3–9 are complete, including the now-playing resolver tracked separately by #8. ADR drafts under #10–12 remain Proposed and await human acceptance; their completed drafting criteria do not close that review gate.

The development assessment adds [#38](https://git.subcult.tv/subculture-collective/overlaid/issues/38), fail-closed webhook credentials in LAN/public modes, to Phase 0. Until that fix and ADR acceptance are complete, this phase stays open. No architecture acceptance or Phase 1 implementation is implied by the merged code.

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

ADRs require human review before Phase 1 implementation. Remote publication was outside the original local implementation scope; the code and drafts subsequently merged through PR #35. Deployment and release acceptance remain separate.

## Acceptance and review

Code acceptance evidence is recorded in [Phase 0 QA](../../qa/2026-10-06-multiplatform-phase-0.md). The six ADRs are linked from [the ADR index](../../adr/README.md), all with Proposed status. #10–12's drafting criteria are complete; their accepted-status goal remains a human PR-review gate. Phase 1 implementation has not started.

The October roadmap and research were restored byte-for-byte from Gitea main for this older worktree: Git blob hashes `002bafe1091584ecd26c891f3d16993ae6720fa5` and `708165dcb9e8812f7a195e1d28b4e1e19c23e67b`. The June acceptance record now identifies its narrower historical scope.
