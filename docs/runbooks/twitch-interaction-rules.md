# Twitch Interaction Rules Runbook

Use this when editing backend Twitch chat aliases or channel point reward rules.

## What lives where

- Rules are stored on `api-data` at `/data/twitch-interactions.json`.
- `TWITCH_INTERACTIONS_FILE` defaults to `/data/twitch-interactions.json` in Docker.
- The dashboard `Config` manager is the normal editing surface.
- `GET /api/twitch/interactions` and `PUT /api/twitch/interactions` are operator-protected.

## Readiness behavior

- Missing `/data/twitch-interactions.json` is normal on first boot.
- If the file is malformed or unreadable, `/readyz` reports `twitch_interactions: degraded`.
- When that happens, custom Twitch interaction rules are disabled until you save valid rules or repair the file and restart/reload.

## Chat aliases

- Chat commands must come from broadcaster or moderator accounts.
- The parser only accepts `!`-prefixed commands and lowercases the command name before alias resolution.
- Aliases can only resolve to the safe built-in commands `alert`, `clear`, `brb`, and `back`.
- Commands never jump into arbitrary OBS actions or dashboard quick actions.

Recommended smoke check:

1. Open `/dashboard/config`.
2. In the Twitch interaction rules manager, add a test alias like `!panic -> clear`.
3. Save with the operator secret.
4. Send `!panic` from a moderator or broadcaster account.

## Channel point reward rules

- Reward-title matching is exact after case-insensitive normalization.
- Supported actions are `alert`, `brb`, `clear`, and `ignore`.
- `ignore` suppresses the normal redemption alert and emits a `custom_command` diagnostic event. Diagnostic events can still appear in broad diagnostic surfaces such as `/overlay/debug`, event history, or ticker-style feeds that intentionally show generic event summaries.

Examples:

- `Change Color` matches `change color` and `CHANGE COLOR`.
- `ignore` is useful when you want to suppress the normal redemption alert while still leaving an operator-visible diagnostic trail.

## Backup and recovery

1. Back up the file before making large edits.
2. If the file is corrupt, save a copy and replace it with valid JSON.
3. Re-save the rules from the dashboard Config manager to clear the degraded state.
4. Include `/data/twitch-interactions.json` in `api-data` backups if you want the rules to survive restores.

Example recovery flow:

```bash
docker compose exec api sh -c 'cp /data/twitch-interactions.json /data/twitch-interactions.json.bak'
docker compose exec api sh -c 'rm /data/twitch-interactions.json'
docker compose restart api
```

Then repopulate valid rules from `/dashboard/config`.

## Live QA

- Open `/dashboard/live` and confirm `twitch_interactions` is `ok`.
- Open `/overlay/debug` and verify alias attempts emit `custom_command` diagnostics.
- Test one broadcaster/mod alias, one channel point `alert`, one `clear`, and one `ignore` rule.
- Confirm `ignore` suppresses the redemption alert and produces only the diagnostic event in debug/history surfaces.
