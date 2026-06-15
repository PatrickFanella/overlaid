# Bot Ops Runbook

Use this runbook when checking over-laid's FossaBot-parity bot runtime: audit storage, future commands, timers, channel-point workflows, and moderation.

## Current scope

Implemented now:

- Bot audit storage in Postgres when `DATABASE_URL` is set.
- Operator-protected bot status at `GET /api/bot/status`.
- Operator-protected recent audit events at `GET /api/bot/audit`.
- `/dashboard/bot` overview and audit panel.
- Existing Twitch chat command diagnostic events are mapped into bot audit events.

Not implemented yet:

- Timer CRUD/runtime.
- General custom response commands.
- Counters, quotes, and community utilities.
- Twitch moderation actions and nuke-style workflows.

## Fast checks

```bash
curl -fsS -H 'X-Overlay-Operator-Secret: <secret-if-configured>' http://localhost:8080/api/bot/status | jq .
curl -fsS -H 'X-Overlay-Operator-Secret: <secret-if-configured>' 'http://localhost:8080/api/bot/audit?limit=10' | jq .
```

Expected status:

- `available: true` when Postgres is configured.
- `bot_runtime` readiness is `ok`.
- `commandCount` and `timerCount` can be `0` until later milestones.

## Dashboard check

1. Open `/dashboard/bot`.
2. Confirm Bot runtime shows storage availability.
3. Confirm Bot audit shows either recent events or the empty state.
4. If the page reports `bot status failed: 401`, enter the operator secret in `/dashboard/config`.

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
