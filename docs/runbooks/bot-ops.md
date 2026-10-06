# Bot Ops Runbook

Use this runbook when checking over-laid's FossaBot-parity bot runtime: audit storage, commands, timers, counters, quotes, future channel-point workflows, and moderation.

## Current scope

Implemented now:

- Bot audit storage in Postgres when `DATABASE_URL` is set.
- Operator-protected bot status at `GET /api/bot/status`.
- Operator-protected recent audit events at `GET /api/bot/audit`.
- Operator-protected custom command list/create/toggle/delete at `/api/bot/commands`.
- Operator-protected timer list/create/toggle/delete at `/api/bot/timers`, plus `POST /api/bot/timers/{id}/run` for manual rehearsal.
- Operator-protected counter list/create/delete at `/api/bot/counters`.
- Operator-protected quote list/create/delete at `/api/bot/quotes`.
- Operator-protected custom API variable list/create/delete at `/api/bot/api-variables`.
- Operator-protected moderation rule list/create/delete and dry-run matching at `/api/bot/moderation/*`.
- Enabled timers publish safe ticker or alert overlay events when due.
- `/dashboard/chat` overview, command manager, counters/quotes manager, moderation dry-run panel, timer manager, and audit panel.
- Existing Twitch chat command diagnostic events are mapped into bot audit events.
- Custom Twitch commands can publish safe ticker/alert overlays, guarded outbound Twitch chat replies, or both, with bounded template variables.
- Templates can use operator-configured `{api:name}` variables for read-only HTTPS GET data.

Implemented in later FossaBot parity slices:

- Nuke-style dry-run, guarded delete/timeout execution, temporary radiation windows, and rollback.
- Twitch reward inventory, minimal creation/deletion, pause/resume, enable/disable, redemption fulfillment/canceling, and fulfillment-note visibility through the Twitch interaction rules page.

Still carried forward:

- Advanced Twitch reward fields such as cooldowns, per-stream/user limits, and other non-minimal Helix settings.
- Cloud-hosted uptime parity; see `docs/superpowers/plans/2026-06-16-fossabot-uptime-parity-options.md` before changing deployment architecture.

## Stabilization QA checklist

- [ ] `/dashboard/chat` loads with bot status and recent audit.
- [ ] A safe custom command can be previewed without sending chat.
- [ ] A timer can be enabled/paused without losing its response text.
- [ ] Counter and quote panels show empty states or configured items clearly.
- [ ] API variable cards show cache/status/error state without exposing secret-looking keys.
- [ ] Moderation dry-run can test a sample message without enforcing.
- [ ] Nuke planner confirmation resets after changing plan fields.
- [ ] Active radiation windows show expiry and rollback controls only for nuke radiation rules.

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

1. Open `/dashboard/chat`.
2. Confirm Bot runtime shows storage availability.
3. Confirm Bot commands shows existing commands or the empty state.
4. Create a safe test command with name `discord`, action `Ticker`, role `Viewer`, cooldown `10`, and a short response.
5. Toggle the command paused/enabled and delete it if it was only for verification.
6. Confirm Bot timers shows existing timers or the empty state.
7. Create a safe test timer with name `stream_hype`, interval `300`, action `Ticker`, and a short response.
8. Click Edit on the saved timer. Change its interval or response and save changes, then confirm the same timer row and `timer_update` audit. Cancel edit abandons changes without writing.
9. Pause the timer and click Run once. Check its ticker or alert output and the `timer_run_once` bot audit row. The timer stays paused and its schedule stays unchanged.
10. Toggle the timer paused/enabled and delete it if it was only for verification.
11. Confirm Bot counters and quotes shows existing entries or the empty state.
12. Create a safe `hype` counter and a short quote, then delete them if verification-only.
13. Confirm Bot moderation dry-run shows existing rules or the empty state.
14. Create a safe `spam_link` rule matching message text `buy now`; dry-run a sample message; delete it if verification-only.
15. Confirm Bot audit shows either recent events or the empty state.
16. If the page reports `bot status failed: 401`, enter the operator secret in `/dashboard/settings`.

## Custom command behavior

Custom commands are intentionally safer and smaller than FossaBot's full command language in this milestone:

- Names use lowercase letters, numbers, dash, or underscore. Built-ins `alert`, `clear`, `brb`, and `back` are reserved.
- Responses are required and capped at 500 UTF-8 bytes.
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

- Operators define named variables in `/dashboard/chat`; chat cannot provide URLs.
- Variables are GET-only and HTTPS-only.
- Local/private hosts, user-info URLs, fragments, unsafe names, long responses, and slow responses are rejected or capped.
- Cache TTL is 15-3600 seconds; timeout is 250-3000ms; at most three unique API variables resolve per template render.
- Supported responses are `text/plain` or scalar/selected `application/json` values via a simple dot selector such as `data.value`.
- Headers, request bodies, POST/PUT/DELETE, scripts, jq, HTML scraping, and arbitrary chat-provided URLs are intentionally not supported.

To test a custom command:

1. Create `!discord` in `/dashboard/chat` with response `Join {user}: https://example.test {args}`.
2. Send `!discord today` in Twitch chat.
3. Confirm `/overlay/ticker` or `/overlay/alerts` shows the configured response for `overlay`/`both` modes, or Twitch chat receives the rendered response for `chat`/`both` modes.
4. Confirm `/dashboard/chat` audit records the command with result `allowed`.

Use Edit on a saved command to change its response, response mode, overlay action, required role, cooldown, or enabled state. Save changes keeps the existing command ID; Cancel edit abandons the draft. Failed writes keep the draft for retry, and pending operations disable editing and row controls. Pause/enable updates the editor if that command is selected; deleting it returns to the new-command form. A successful write remains visible if the later status/audit refresh fails.

If one bot operation fails to load, its panel reports the failure and the other panels keep their successful results. Check the affected panel before assuming all bot storage is unavailable. Counters and quotes report load errors separately.

## Timer behavior

Timers are persisted so operator configuration survives browser/session changes. When bot storage is available, the API starts a timer worker that polls due enabled timers and publishes the configured safe `ticker` or `alert` response into the existing event pipeline. Timer executions write bot audit rows with result `allowed` and reason `timer_due`.

Use Edit on a saved timer to change its name, interval, response, action, or enabled state. Save changes updates its existing ID and preserves the last scheduled run timestamp. Cancel edit returns to the new-timer form without writing. Failed saves keep the edited draft for retry. Timer controls stay disabled while an operation is pending. A successful operation stays visible even if the subsequent status/audit refresh fails.

Run once sends one saved timer response, including a paused timer. It requires operator access and bot storage, but no Twitch credentials. It does not enable the timer or update `lastRunAt`. The dashboard labels that timestamp Last scheduled run.

Create, update, pause, enable, and delete operations also record bot audit rows with action `timer_create`, `timer_update`, `timer_pause`, `timer_enable`, or `timer_delete`. They include scheduling metadata and active session ID, without response templates. Failed configuration changes use result `failed`. The dashboard refreshes audit after configuration changes.

Manual executions appear in bot audit with source `dashboard`, actor role `operator`, reason `timer_run_once`, event ID, and active stream session ID. Empty rendered responses return a validation error and write a failed audit row.

Timer validation:

- Names use lowercase letters, numbers, dash, or underscore.
- Intervals must be between 30 seconds and 24 hours.
- Actions are currently `ticker` or `alert`.
- Responses are required and capped at 500 UTF-8 bytes.
- Template variables use the same renderer as custom commands. `{channel}` and `{command}` are useful for timers; user/args variables render empty for timer-triggered messages.

## Editing API variables

Use Edit on a saved API variable to change its name, URL, selector, fallback, TTL, timeout, or enabled state. Save changes keeps its ID; Cancel edit abandons the draft. Pending writes disable the editor and row controls. Failed writes keep the draft for retry, and successful writes remain visible if the later status/audit refresh fails. Name, TTL, and timeout limits are checked in the form; backend URL validation still rejects private hosts and embedded credentials.

Each resolution loads the current saved configuration before using a cached HTTP value. Disabling returns the configured fallback; deleting makes the variable missing. Edits invalidate values cached for the old configuration and reset diagnostics to untested. A fetch that finishes after an edit cannot replace the new configuration's diagnostics or supply a future cache hit for that configuration. Diagnostic updates do not invalidate an otherwise valid cache entry.

The optional Postgres cache workflow test performs no outbound HTTP requests. From `backend`, run `OVERLAID_TEST_DATABASE_URL='<disposable-postgres-url>' go test ./internal/bot -run TestAPIVariableCacheConfigurationPostgres -count=1`. It checks disable/edit/delete/recreate behavior and late fetch results using seeded cache values.

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

Moderation rules are dry-run by default. They only call Twitch delete, timeout, or ban APIs when `enforce` is explicitly enabled on the matching rule.

- Rule names use lowercase letters, numbers, dash, or underscore.
- Patterns match message text, username, or both.
- Patterns can be plain contains matches or Go/RE2 regular expressions.
- Matching is case-insensitive unless `case sensitive` is enabled.
- Actions `flag`, `delete`, `timeout`, and `ban` can be tested in dry-run first. `delete`/`timeout`/`ban` become live only when `enforce` is checked.
- Dry-run requests return the rules that would match a sample username/message.
- Keep new rules in dry-run until a live sample confirms the pattern is correct; regex mistakes can otherwise delete, timeout, or ban real users.

Manual guarded Twitch moderation actions are available from `/dashboard/chat` and `POST /api/bot/moderation/action`:

- `delete` requires `messageId` and Twitch scope `moderator:manage:chat_messages`.
- `timeout` requires `targetUserId`, `durationSeconds` from `1` to `1209600`, and Twitch scope `moderator:manage:banned_users`.
- `ban` requires `targetUserId` and Twitch scope `moderator:manage:banned_users`.
- `unban` requires `targetUserId` and Twitch scope `moderator:manage:banned_users`.
- The current implementation is broadcaster-self mode: the OAuth token user is used as both broadcaster and moderator id.
- Every attempted action writes a bot audit row with result `allowed` or `failed`.

If the dashboard reports missing moderation scopes, update `TWITCH_SCOPES`, re-run Twitch login, and re-consent before retrying. Enforced rules fail closed when the required Twitch scope is missing and write a failed audit row.

Nuke planning and guarded execution are available from `/dashboard/chat`:

- It scans recent persisted `chat_message` history only.
- It supports plain contains matching or RE2 regular expressions.
- It supports case-insensitive matching by default and an optional case-sensitive mode.
- It accepts a bounded lookback window and result limit.
- It returns matching users/messages/message IDs when available.
- Execution uses `POST /api/bot/moderation/nuke/execute`, accepts the same plan fields, and requires `confirm: true`.
- Execution supports `delete` and `timeout` only. Timeout defaults to 60 seconds when no duration is supplied.
- Execution is capped to 50 matching chat events per request and writes bot audit rows for successful and failed Twitch moderation attempts.
- The dashboard clears confirmation whenever the plan inputs change; run the dry-run, review the listed matches, check `I reviewed this dry-run plan`, then execute.
- Optional radiation minutes create a temporary enforced moderation rule from the nuke pattern for future matching chat messages. Radiation windows are capped at 120 minutes and appear under active radiation windows. Future timeout radiation uses the standard 60-second timeout.
- Roll back a radiation window from `/dashboard/chat` or `POST /api/bot/moderation/nuke/radiation/rollback` with `ruleId` and `confirm: true`; rollback disables enforcement and writes a bot audit row.
- It does not ban users or undo already-deleted chat messages. Timeout rollback is limited to disabling the future-enforcement window; manual unban/untimeout remains available through the guarded moderation action controls.

## Session bot audit review

Counter and quote panels load independently and keep separate drafts, pending controls, errors, and results. A failed write keeps its draft for retry. Successful writes update the visible rows immediately; a later status/audit refresh failure keeps the success result and reports the refresh error. A failed initial list request remains visible because one successful write cannot prove the rest of the list is complete.

Dashboard counter and quote create, update, and delete actions record success or failure in bot audit. Records use source `dashboard`, role `operator`, and the active stream session ID. Counter records include the saved name and value; quote records include only the record ID, without quote text or author content.

1. Start a rehearsal session, then create, update, and delete a verification counter and quote.
2. Check the `counter_create`, `counter_update`, `counter_delete`, `quote_create`, `quote_update`, and `quote_delete` rows in Bot audit.
3. End the rehearsal and open its report in `/dashboard/sessions`.
4. Check Session bot actions and its loaded/total count. Reports query that session before applying the row limit, so newer activity from another session cannot displace its rows.
5. Export JSON and Markdown. Exports contain the loaded rows and their count; a bounded report is not a full audit archive. An unavailable audit warning means the report could not load audit data, not that no actions occurred.

The optional Postgres audit integration test needs a disposable database. It creates and removes its own ended-session fixtures. From `backend`, run `OVERLAID_TEST_DATABASE_URL='<disposable-postgres-url>' go test ./internal/bot -run TestAuditForSessionPostgres -count=1`. The normal suite skips this test when the variable is unset.

## Command audit check

1. Start a rehearsal session from `/dashboard` or `/dashboard/sessions` if you want session tagging.
2. From a broadcaster/moderator account, send `!alert audit smoke`.
3. Open `/dashboard/chat`.
4. Confirm a bot audit row appears with result `allowed`.
5. From a viewer account, send `!clear`.
6. Confirm a bot audit row appears with result `denied` and reason `permission_denied`.

## Troubleshooting

- If `bot_runtime` is degraded, inspect `DATABASE_URL`, Postgres health, and API logs.
- If audit is empty after commands, confirm EventSub chat is connected and `/overlay/debug` sees chat command diagnostics.
- Bot audit data intentionally rejects secret-looking keys. Do not store tokens, passwords, cookies, authorization headers, or raw webhook secrets.
