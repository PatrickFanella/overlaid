# Twitch Chat Commands Runbook

Plan 9 keeps Twitch chat commands server-side and limited to safe built-in overlay actions.

## Requirements

- Twitch auth is configured and connected.
- EventSub diagnostics show connected subscriptions.
- `channel.chat.message` events are arriving.
- The sender is the broadcaster or has a Twitch moderator badge in the EventSub payload.
- Chat aliases are configured in the dashboard `Config` manager and saved to `/data/twitch-interactions.json` on the `api-data` volume.

## Supported commands

| Command | Effect |
| --- | --- |
| `!alert <message>` | Sends a short Twitch-sourced alert. |
| `!clear` | Clears transient overlays. |
| `!brb [message]` | Shows the BRB overlay with an optional message. |
| `!back` | Hides the BRB overlay. |

`!alert` requires a message. Empty `!alert` attempts emit a warning diagnostic and do not show an alert.

The parser only accepts `!`-prefixed commands, lowercases the command name, and strips the leading bang before alias lookup. Aliases are stored as bare names, so `!panic` and `panic` both normalize to the same alias entry.

## Safety model

- The trust model is broadcaster OR moderator; viewers without either badge cannot trigger overlay-changing actions.
- Command attempts emit `custom_command` diagnostic events visible in `/overlay/debug`.
- Cooldowns are global per command, apply to all trusted users, last 10 seconds, live in memory, and reset when the API process restarts.
- `!clear`, `!brb`, and `!back` update retained overlay state so OBS browser-source reconnects do not replay stale BRB/overlay state.
- Commands do not execute arbitrary OBS actions or dashboard quick actions.
- Commands do not send outbound Twitch chat replies.

If you add chat aliases, they can only point to `alert`, `clear`, `brb`, or `back`. The alias layer is intentionally small and cannot trigger custom commands.

## Pre-stream check

1. Open `/overlay/debug` in a browser tab.
2. From a broadcaster or moderator account, send `!alert command smoke`.
3. Confirm `/overlay/debug` shows a `custom_command` event and `/overlay/alerts` shows the alert.
4. Send `!clear` and confirm transient overlays clear.
5. Send `!brb short break`, then `!back`, and confirm BRB appears and hides.

## Troubleshooting

- If chat appears but commands do nothing, confirm the sender is broadcaster/moderator.
- If commands work once and then stop briefly, wait for the cooldown window.
- If `!alert` does nothing, make sure a non-empty message was supplied.
- If no chat appears, check Twitch auth, scopes, EventSub diagnostics, and API logs.
- If alias behavior looks wrong, open `/dashboard/config`, inspect the Twitch interaction rules manager, and re-save valid JSON.
