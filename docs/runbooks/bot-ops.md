# Bot Ops Runbook

Use this runbook when checking over-laid's FossaBot-parity bot runtime: audit storage, future commands, timers, channel-point workflows, and moderation.

## Current scope

Implemented now:

- Bot audit storage in Postgres when `DATABASE_URL` is set.
- Operator-protected bot status at `GET /api/bot/status`.
- Operator-protected recent audit events at `GET /api/bot/audit`.
- Operator-protected timer list/create/toggle/delete at `/api/bot/timers`.
- `/dashboard/bot` overview, timer manager, and audit panel.
- Existing Twitch chat command diagnostic events are mapped into bot audit events.

Not implemented yet:

- Timer firing/runtime dispatch.
- General custom response commands.
- Counters, quotes, and community utilities.
- Twitch moderation actions and nuke-style workflows.

## Fast checks

```bash
curl -fsS -H 'X-Overlay-Operator-Secret: <secret-if-configured>' http://localhost:8080/api/bot/status | jq .
curl -fsS -H 'X-Overlay-Operator-Secret: <secret-if-configured>' 'http://localhost:8080/api/bot/audit?limit=10' | jq .
curl -fsS -H 'X-Overlay-Operator-Secret: <secret-if-configured>' http://localhost:8080/api/bot/timers | jq .
```

Expected status:

- `available: true` when Postgres is configured.
- `bot_runtime` readiness is `ok`.
- `commandCount` can be `0` until custom commands land.
- `timerCount` reflects configured bot timers, including disabled timers.

## Dashboard check

1. Open `/dashboard/bot`.
2. Confirm Bot runtime shows storage availability.
3. Confirm Bot timers shows existing timers or the empty state.
4. Create a safe test timer with name `stream_hype`, interval `300`, action `Ticker`, and a short response.
5. Toggle the timer paused/enabled and delete it if it was only for verification.
6. Confirm Bot audit shows either recent events or the empty state.
7. If the page reports `bot status failed: 401`, enter the operator secret in `/dashboard/config`.

## Timer behavior

Timers are persisted now so operator configuration survives browser/session changes. Runtime dispatch is intentionally held until custom response commands/templates land; otherwise timers would need a separate response language that would be replaced in the next milestone.

Timer validation:

- Names use lowercase letters, numbers, dash, or underscore.
- Intervals must be between 30 seconds and 24 hours.
- Actions are currently `ticker` or `alert`.
- Responses are required and capped at 500 characters.

## Command audit check

1. Start a rehearsal session from `/dashboard/live` if you want session tagging.
2. From a broadcaster/moderator account, send `!alert audit smoke`.
3. Open `/dashboard/bot`.
4. Confirm a bot audit row appears with result `allowed`.
5. From a viewer account, send `!clear`.
6. Confirm a bot audit row appears with result `denied` and reason `permission_denied`.

## Troubleshooting

- If `bot_runtime` is degraded, inspect `DATABASE_URL`, Postgres health, and API logs.
- If audit is empty after commands, confirm EventSub chat is connected and `/overlay/debug` sees chat command diagnostics.
- Bot audit data intentionally rejects secret-looking keys. Do not store tokens, passwords, cookies, authorization headers, or raw webhook secrets.
