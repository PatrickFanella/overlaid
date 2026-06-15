# Twitch Interaction Rules Runbook

Use this when editing backend Twitch chat aliases or channel point reward rules.

## What lives where

- Rules are stored in Postgres table `twitch_interaction_rules` when `DATABASE_URL` is set.
- If `DATABASE_URL` is unset, rules fall back to `api-data` at `/data/twitch-interactions.json`.
- `TWITCH_INTERACTIONS_FILE` controls the JSON fallback path and defaults to `/data/twitch-interactions.json` in Docker.
- The dashboard `Config` manager is the normal editing surface.
- `GET /api/twitch/interactions` and `PUT /api/twitch/interactions` are operator-protected.

## Readiness behavior

- With Postgres enabled, a missing row is normal on first boot; saving rules creates/updates the singleton `default` row.
- With JSON fallback, missing `/data/twitch-interactions.json` is normal on first boot.
- If Postgres is unreachable or the fallback file is malformed/unreadable, `/readyz` reports `twitch_interactions: degraded`.
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
- Optional cooldown seconds suppress repeated matching reward actions and emit a diagnostic event while cooling down.
- Optional fulfillment notes are stored with the rule and included in emitted overlay event metadata for operator review.
- Title/message overrides support `{user}`, `{reward}`, and `{input}` variables.
- `ignore` suppresses the normal redemption alert and emits a `custom_command` diagnostic event. Diagnostic events can still appear in broad diagnostic surfaces such as `/overlay/debug`, event history, or ticker-style feeds that intentionally show generic event summaries.

Examples:

- `Change Color` matches `change color` and `CHANGE COLOR`.
- `ignore` is useful when you want to suppress the normal redemption alert while still leaving an operator-visible diagnostic trail.
- A cooldown is useful for expensive or disruptive rewards such as scene changes, long BRB prompts, or visual spam.

## Reward queue

The config page can load manageable Twitch rewards, then load unfulfilled redemptions for a selected reward. Fulfill/cancel controls call Twitch Helix redemption status updates with `channel:manage:redemptions`.

Safety boundaries:

- Reward creation/deletion is not exposed in this pass.
- Reward listing defaults to manageable rewards only.
- Fulfill/cancel only applies to explicit operator-selected unfulfilled redemptions.
- If Twitch status shows `missingScopes` for `channel:manage:redemptions`, re-run Twitch login and re-consent before fulfillment controls can work.

## Backup and recovery

1. Back up Postgres with `make postgres-backup` before making large edits when `DATABASE_URL` is set.
2. Back up the JSON file before making large edits when using fallback storage.
3. If Postgres-backed rules are unavailable, check `docker compose ps postgres`, run `make db-migrate`, and restart the API after Postgres is healthy.
4. If the fallback file is corrupt, save a copy and replace it with valid JSON.
5. Re-save the rules from the dashboard Config manager to clear the degraded state.
6. Include `/data/twitch-interactions.json` in `api-data` backups only when using JSON fallback storage.

Postgres recovery checks:

```bash
docker compose ps postgres
make db-migrate
make db-tables
docker compose restart api
```

JSON fallback recovery flow:

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
