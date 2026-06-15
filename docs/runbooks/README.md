# Runbooks

Runbooks describe repeatable procedures that an operator or agent can follow safely.

Good runbooks include:

- Purpose
- Preconditions
- Commands
- Expected output
- Rollback or cleanup steps
- Verification

Add stack-specific runbooks as this template is specialized.

## over-laid operator runbooks

- `live-qa.md` — canonical live/rehearsal QA checklist.
- `live-stream-preflight.md` — quick pre-stream readiness procedure.
- `visual-qa.md` — route-by-route visual pass.
- `stream-sessions.md` — session audit reports and readiness snapshots.
- `event-history.md` — persisted event history and event pipeline diagnostics.
- `obs-automation-sequences.md` — OBS quick actions and sequences.
- `twitch-chat-commands.md` / `twitch-interaction-rules.md` — Twitch command and channel-point behavior.
- `production-packaging.md` / `production-recovery.md` — deploy, backup, restore, and recovery flow.
