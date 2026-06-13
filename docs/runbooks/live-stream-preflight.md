# Live Stream Preflight Runbook

## Start

```bash
make docker-build
make smoke
```

## Dashboard

Start at:

```text
http://localhost:5173/dashboard/live
```

Expected ready checks:

- Twitch authenticated
- EventSub connected
- OBS reachable
- Operator secret loaded if configured
- Setup checklist complete

## Rehearsal

Open `/dashboard/control` and run `Run rehearsal`.

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

Open `/dashboard/config` and enter `OPERATOR_SECRET`.

## Browser sources during stream

Do not rely on OBS browser sources until `/dashboard/live` is clear of readiness incidents. Fix any degraded checks first, then confirm the browser sources are stable before going live.


## Live QA status

Latest manual live QA pass: OK so far. Keep using this runbook before each real stream and update this section if a recurring failure appears.
