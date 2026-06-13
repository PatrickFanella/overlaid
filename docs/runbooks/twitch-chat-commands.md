# Twitch Chat Commands Runbook

Plan 3 adds a small Twitch chat command layer on top of EventSub chat messages.

## Requirements

- Twitch auth is configured and connected.
- EventSub diagnostics show connected subscriptions.
- `channel.chat.message` events are arriving.
- The sender is the broadcaster or has a Twitch moderator badge in the EventSub payload.

## Supported commands

| Command | Effect |
| --- | --- |
| `!alert <message>` | Sends a short Twitch-sourced alert. |
| `!clear` | Clears transient overlays. |
| `!brb [message]` | Shows the BRB overlay with an optional message. |
| `!back` | Hides the BRB overlay. |

`!alert` requires a message. Empty `!alert` attempts emit a warning diagnostic and do not show an alert.

## Safety model

- The trust model is broadcaster OR moderator; viewers without either badge cannot trigger overlay-changing actions.
- Command attempts emit `custom_command` diagnostic events visible in `/overlay/debug`.
- Cooldowns are global per command, apply to all trusted users, last 10 seconds, live in memory, and reset when the API process restarts.
- `!clear`, `!brb`, and `!back` update retained overlay state so OBS browser-source reconnects do not replay stale BRB/overlay state.
- Commands do not execute arbitrary dashboard quick actions.
- Commands do not send outbound Twitch chat replies.

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
