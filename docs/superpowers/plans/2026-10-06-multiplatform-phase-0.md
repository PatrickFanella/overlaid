# Multi-platform Phase 0 implementation plan

Scope: October 5 parity roadmap, Gitea issues #3–7, #9–12. Existing June work is a separate baseline. No live platform credentials are needed for these changes.

- [x] #3: overlay key configuration, authenticated streams/history, sanitized history copies, source URLs and access tests.
- [x] #7: display-only shared config endpoint, overlay load/reconnect fetch, demo/default fallback and tests.
- [x] #4: bounded TTL cache of successful outbound message IDs; drop echoes before command parsing and moderation; manual broadcaster commands remain usable.
- [x] #5: moderation role exemptions and timeout duration, schema migration, editor and dry-run evidence.
- [x] #6: matching scope defaults and re-consent instructions.
- [x] #9: correct chat reply and channel point runbooks.
- [x] #10–12: six Proposed ADRs covering tenancy, platform boundary, engine, variables, Postgres jobs/fan-out and self-host mode, with migration and alternatives.
- [x] Run `make verify`, browser smoke and fresh Postgres integration checks as applicable; review the diff and record acceptance evidence.

ADRs require human review before Phase 1 implementation. Drafting and Phase 0 code continue independently. Remote publishing and deployment are outside this local implementation scope.

## Acceptance and review

Code acceptance evidence is recorded in [Phase 0 QA](../../qa/2026-10-06-multiplatform-phase-0.md). The six ADRs are linked from [the ADR index](../../adr/README.md), all with Proposed status. #10–12's drafting criteria are complete; their accepted-status goal remains a human PR-review gate. Phase 1 implementation has not started.

The October roadmap and research were restored byte-for-byte from Gitea main for this older worktree: Git blob hashes `002bafe1091584ecd26c891f3d16993ae6720fa5` and `708165dcb9e8812f7a195e1d28b4e1e19c23e67b`. The June acceptance record now identifies its narrower historical scope.
