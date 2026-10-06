# Live Stream Preflight Runbook

## Start

```bash
make docker-build
make smoke
```

## Dashboard

Start at `/dashboard/health`; it has readiness, preflight, and the recovery console. `/dashboard` (On air) repeats the preflight next to the session controls. For the full rehearsal/live checklist, use `docs/runbooks/live-qa.md`.

```text
http://localhost:5173/dashboard/health
```

Expected ready checks:

- Twitch authenticated
- EventSub connected
- OBS reachable
- Operator secret loaded if configured
- Setup checklist complete

## Rehearsal

Open `/dashboard/scenes` and run `Run rehearsal`.

After the sequence completes, check the Event history panel on `/dashboard/sessions` to confirm the recent sample event summaries were recorded.

Expected sequence:

1. Alert appears.
2. Ticker updates.
3. Goal rail updates.
4. BRB appears.
5. BRB hides.

## Common failures

### Twitch authenticated but EventSub not connected

Re-run Twitch login from the dashboard header. If scopes changed, re-consent.

### OBS unreachable

Check OBS WebSocket settings, host networking override, and `OBS_WEBSOCKET_PASSWORD`.

### Operator controls return 401

Open `/dashboard/settings` and enter `OPERATOR_SECRET`.

## Browser sources during stream

Do not rely on OBS browser sources until `/dashboard/health` is clear of readiness incidents. Fix any degraded checks first, then confirm the browser sources are stable before going live.

Use the Event history panel as a quick post-rehearsal audit trail; it should show the latest sanitized stream events newest-first.


## Live QA status

Latest status is tracked in `docs/runbooks/live-qa.md`. Keep using that runbook before each real stream and update it if a recurring failure appears.
