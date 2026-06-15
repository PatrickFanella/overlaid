# Bot Ops Runbook

Use this runbook when checking over-laid's FossaBot-parity bot runtime: audit storage, future commands, timers, channel-point workflows, and moderation.

## Current scope

Implemented now:

- Bot audit storage in Postgres when `DATABASE_URL` is set.
- Operator-protected bot status at `GET /api/bot/status`.
- Operator-protected recent audit events at `GET /api/bot/audit`.
- Operator-protected custom command list/create/toggle/delete at `/api/bot/commands`.
- Operator-protected timer list/create/toggle/delete at `/api/bot/timers`.
- Enabled timers publish safe ticker or alert overlay events when due.
- `/dashboard/bot` overview, command manager, timer manager, and audit panel.
- Existing Twitch chat command diagnostic events are mapped into bot audit events.
- Custom Twitch commands can publish safe ticker or alert overlay responses with bounded template variables.

Not implemented yet:

- Counters, quotes, and community utilities.
- Twitch moderation actions and nuke-style workflows.

## Fast checks

```bash
curl -fsS -H 'X-Overlay-Operator-Secret: <secret-if-configured>' http://localhost:8080/api/bot/status | jq .
curl -fsS -H 'X-Overlay-Operator-Secret: <secret-if-configured>' 'http://localhost:8080/api/bot/audit?limit=10' | jq .
curl -fsS -H 'X-Overlay-Operator-Secret: <secret-if-configured>' http://localhost:8080/api/bot/commands | jq .
curl -fsS -H 'X-Overlay-Operator-Secret: <secret-if-configured>' http://localhost:8080/api/bot/timers | jq .
```

Expected status:

- `available: true` when Postgres is configured.
- `bot_runtime` readiness is `ok`.
- `commandCount` reflects configured custom commands, including disabled commands.
- `timerCount` reflects configured bot timers, including disabled timers.

## Dashboard check

1. Open `/dashboard/bot`.
2. Confirm Bot runtime shows storage availability.
3. Confirm Bot commands shows existing commands or the empty state.
4. Create a safe test command with name `discord`, action `Ticker`, role `Viewer`, cooldown `10`, and a short response.
5. Toggle the command paused/enabled and delete it if it was only for verification.
6. Confirm Bot timers shows existing timers or the empty state.
7. Create a safe test timer with name `stream_hype`, interval `300`, action `Ticker`, and a short response.
8. Toggle the timer paused/enabled and delete it if it was only for verification.
9. Confirm Bot audit shows either recent events or the empty state.
10. If the page reports `bot status failed: 401`, enter the operator secret in `/dashboard/config`.

## Custom command behavior

Custom commands are intentionally safer and smaller than FossaBot's full command language in this milestone:

- Names use lowercase letters, numbers, dash, or underscore. Built-ins `alert`, `clear`, `brb`, and `back` are reserved.
- Responses are required and capped at 500 characters.
- Actions are `ticker` or `alert` only.
- Required roles are `viewer`, `moderator`, or `broadcaster`.
- Cooldowns are per custom command and live in memory until API restart.
- Commands do not execute scripts, send outbound chat, call external APIs, or run OBS actions.
- Command saves refresh the in-memory Twitch command processor immediately.

Supported template variables:

- `{user}` / `{displayName}` — Twitch display name when available, otherwise login.
- `{channel}` — configured Twitch channel.
- `{args}` — text after the command name.
- `{command}` — resolved command name without `!`.
- `{nowPlaying.title}` / `{nowPlaying.artist}` — reserved now-playing variables; currently render empty until now-playing context is wired into the bot runtime.

Unknown variables render empty and are included in command audit data. Rendered responses are capped at 400 characters.

To test a custom command:

1. Create `!discord` in `/dashboard/bot` with response `Join {user}: https://example.test {args}`.
2. Send `!discord today` in Twitch chat.
3. Confirm `/overlay/ticker` or `/overlay/alerts` shows the configured response.
4. Confirm `/dashboard/bot` audit records the command with result `allowed`.

## Timer behavior

Timers are persisted so operator configuration survives browser/session changes. When bot storage is available, the API starts a timer worker that polls due enabled timers and publishes the configured safe `ticker` or `alert` response into the existing event pipeline. Timer executions write bot audit rows with result `allowed` and reason `timer_due`.

Timer validation:

- Names use lowercase letters, numbers, dash, or underscore.
- Intervals must be between 30 seconds and 24 hours.
- Actions are currently `ticker` or `alert`.
- Responses are required and capped at 500 characters.
- Template variables use the same renderer as custom commands. `{channel}` and `{command}` are useful for timers; user/args variables render empty for timer-triggered messages.

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
