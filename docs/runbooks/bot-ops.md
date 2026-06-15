# Bot Ops Runbook

Use this runbook when checking over-laid's FossaBot-parity bot runtime: audit storage, commands, timers, counters, quotes, future channel-point workflows, and moderation.

## Current scope

Implemented now:

- Bot audit storage in Postgres when `DATABASE_URL` is set.
- Operator-protected bot status at `GET /api/bot/status`.
- Operator-protected recent audit events at `GET /api/bot/audit`.
- Operator-protected custom command list/create/toggle/delete at `/api/bot/commands`.
- Operator-protected timer list/create/toggle/delete at `/api/bot/timers`.
- Operator-protected counter list/create/delete at `/api/bot/counters`.
- Operator-protected quote list/create/delete at `/api/bot/quotes`.
- Operator-protected custom API variable list/create/delete at `/api/bot/api-variables`.
- Operator-protected moderation rule list/create/delete and dry-run matching at `/api/bot/moderation/*`.
- Enabled timers publish safe ticker or alert overlay events when due.
- `/dashboard/bot` overview, command manager, counters/quotes manager, moderation dry-run panel, timer manager, and audit panel.
- Existing Twitch chat command diagnostic events are mapped into bot audit events.
- Custom Twitch commands can publish safe ticker/alert overlays, guarded outbound Twitch chat replies, or both, with bounded template variables.
- Templates can use operator-configured `{api:name}` variables for read-only HTTPS GET data.

Not implemented yet:

- Nuke-style multi-message workflows.
- Twitch reward creation/fulfillment through Helix management APIs.

## Fast checks

```bash
curl -fsS -H 'X-Overlay-Operator-Secret: <secret-if-configured>' http://localhost:8080/api/bot/status | jq .
curl -fsS -H 'X-Overlay-Operator-Secret: <secret-if-configured>' 'http://localhost:8080/api/bot/audit?limit=10' | jq .
curl -fsS -H 'X-Overlay-Operator-Secret: <secret-if-configured>' http://localhost:8080/api/bot/commands | jq .
curl -fsS -H 'X-Overlay-Operator-Secret: <secret-if-configured>' http://localhost:8080/api/bot/timers | jq .
curl -fsS -H 'X-Overlay-Operator-Secret: <secret-if-configured>' http://localhost:8080/api/bot/counters | jq .
curl -fsS -H 'X-Overlay-Operator-Secret: <secret-if-configured>' http://localhost:8080/api/bot/quotes | jq .
curl -fsS -H 'X-Overlay-Operator-Secret: <secret-if-configured>' http://localhost:8080/api/bot/api-variables | jq .
curl -fsS -H 'X-Overlay-Operator-Secret: <secret-if-configured>' http://localhost:8080/api/bot/moderation/rules | jq .
```

Expected status:

- `available: true` when Postgres is configured.
- `bot_runtime` readiness is `ok`.
- `commandCount` reflects configured custom commands, including disabled commands.
- `timerCount` reflects configured bot timers, including disabled timers.
- `counterCount` and `quoteCount` reflect configured community tools.

## Dashboard check

1. Open `/dashboard/bot`.
2. Confirm Bot runtime shows storage availability.
3. Confirm Bot commands shows existing commands or the empty state.
4. Create a safe test command with name `discord`, action `Ticker`, role `Viewer`, cooldown `10`, and a short response.
5. Toggle the command paused/enabled and delete it if it was only for verification.
6. Confirm Bot timers shows existing timers or the empty state.
7. Create a safe test timer with name `stream_hype`, interval `300`, action `Ticker`, and a short response.
8. Toggle the timer paused/enabled and delete it if it was only for verification.
9. Confirm Bot counters and quotes shows existing entries or the empty state.
10. Create a safe `hype` counter and a short quote, then delete them if verification-only.
11. Confirm Bot moderation dry-run shows existing rules or the empty state.
12. Create a safe `spam_link` rule matching message text `buy now`; dry-run a sample message; delete it if verification-only.
13. Confirm Bot audit shows either recent events or the empty state.
14. If the page reports `bot status failed: 401`, enter the operator secret in `/dashboard/config`.

## Custom command behavior

Custom commands are intentionally safer and smaller than FossaBot's full command language in this milestone:

- Names use lowercase letters, numbers, dash, or underscore. Built-ins `alert`, `clear`, `brb`, and `back` are reserved.
- Responses are required and capped at 500 characters.
- Actions are `ticker` or `alert` only.
- Required roles are `viewer`, `moderator`, or `broadcaster`.
- Cooldowns are per custom command and live in memory until API restart.
- Response mode is `overlay`, `chat`, or `both`. `chat` uses Twitch Helix `POST /helix/chat/messages` server-side with the stored OAuth token.
- Chat replies require `user:write:chat`; if Twitch status reports that scope missing, re-run Twitch login and re-consent.
- Commands do not execute scripts, call arbitrary APIs, or run OBS actions.
- Command saves refresh the in-memory Twitch command processor immediately.

Supported template variables:

- `{user}` / `{displayName}` — Twitch display name when available, otherwise login.
- `{channel}` — configured Twitch channel.
- `{args}` — text after the command name.
- `{command}` — resolved command name without `!`.
- `{nowPlaying.title}` / `{nowPlaying.artist}` — reserved now-playing variables; currently render empty until now-playing context is wired into the bot runtime.
- `{count:name}` — current value for a stored counter, or `0` when missing.
- `{quote:random}` — random stored quote, including author when present.
- `{api:name}` — operator-configured read-only HTTPS API variable output or fallback.

Unknown variables render empty and are included in command audit data. Rendered responses are capped at 400 characters.

## API variable behavior

API variables are safer than FossaBot-style inline custom API URLs:

- Operators define named variables in `/dashboard/bot`; chat cannot provide URLs.
- Variables are GET-only and HTTPS-only.
- Local/private hosts, user-info URLs, fragments, unsafe names, long responses, and slow responses are rejected or capped.
- Cache TTL is 15-3600 seconds; timeout is 250-3000ms; at most three unique API variables resolve per template render.
- Supported responses are `text/plain` or scalar/selected `application/json` values via a simple dot selector such as `data.value`.
- Headers, request bodies, POST/PUT/DELETE, scripts, jq, HTML scraping, and arbitrary chat-provided URLs are intentionally not supported.

To test a custom command:

1. Create `!discord` in `/dashboard/bot` with response `Join {user}: https://example.test {args}`.
2. Send `!discord today` in Twitch chat.
3. Confirm `/overlay/ticker` or `/overlay/alerts` shows the configured response for `overlay`/`both` modes, or Twitch chat receives the rendered response for `chat`/`both` modes.
4. Confirm `/dashboard/bot` audit records the command with result `allowed`.

## Timer behavior

Timers are persisted so operator configuration survives browser/session changes. When bot storage is available, the API starts a timer worker that polls due enabled timers and publishes the configured safe `ticker` or `alert` response into the existing event pipeline. Timer executions write bot audit rows with result `allowed` and reason `timer_due`.

Timer validation:

- Names use lowercase letters, numbers, dash, or underscore.
- Intervals must be between 30 seconds and 24 hours.
- Actions are currently `ticker` or `alert`.
- Responses are required and capped at 500 characters.
- Template variables use the same renderer as custom commands. `{channel}` and `{command}` are useful for timers; user/args variables render empty for timer-triggered messages.

## Counters and quotes behavior

Counters and quotes are intentionally safe community tools:

- `!count name` increments a named counter by one. Broadcaster/moderator is required because this mutates persistent state.
- Counter names use lowercase letters, numbers, dash, or underscore.
- `!quote` displays a random stored quote as a ticker-style command event.
- `!quote add text` or `!addquote text` stores a quote with the broadcaster/moderator display name as author.
- `!delquote <id>` deletes a quote by id for broadcaster/moderator users.
- Quote text is required and capped at 500 characters; author is capped at 80 characters.
- Counter and quote commands do not send outbound chat, call external APIs, run OBS actions, or moderate users.

## Moderation dry-run behavior

Moderation rules are a safe foundation for future Twitch moderation tools. They currently do not call Twitch delete, timeout, or ban APIs.

- Rule names use lowercase letters, numbers, dash, or underscore.
- Patterns match message text, username, or both.
- Patterns can be plain contains matches or Go/RE2 regular expressions.
- Matching is case-insensitive unless `case sensitive` is enabled.
- Actions `flag`, `delete`, `timeout`, and `ban` are labels only in this milestone.
- Dry-run requests return the rules that would match a sample username/message.

Manual guarded Twitch moderation actions are available from `/dashboard/bot` and `POST /api/bot/moderation/action`:

- `delete` requires `messageId` and Twitch scope `moderator:manage:chat_messages`.
- `timeout` requires `targetUserId`, `durationSeconds` from `1` to `1209600`, and Twitch scope `moderator:manage:banned_users`.
- `ban` requires `targetUserId` and Twitch scope `moderator:manage:banned_users`.
- `unban` requires `targetUserId` and Twitch scope `moderator:manage:banned_users`.
- The current implementation is broadcaster-self mode: the OAuth token user is used as both broadcaster and moderator id.
- Every attempted action writes a bot audit row with result `allowed` or `failed`.

If the dashboard reports missing moderation scopes, update `TWITCH_SCOPES`, re-run Twitch login, and re-consent before retrying. Do not use action labels from dry-run rules as automatic enforcement yet; rules are still dry-run/planning inputs.

Nuke dry-run planning is available from `/dashboard/bot` and `POST /api/bot/moderation/nuke/dry-run`:

- It scans recent persisted `chat_message` history only.
- It supports plain contains matching or RE2 regular expressions.
- It supports case-insensitive matching by default and an optional case-sensitive mode.
- It accepts a bounded lookback window and result limit.
- It returns matching users/messages/message IDs when available.
- It does not delete, timeout, ban, or create a radiation/future-enforcement window yet.

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
