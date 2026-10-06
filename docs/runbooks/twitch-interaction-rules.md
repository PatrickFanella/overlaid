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

1. Open `/dashboard/chat` (Twitch rules section).
2. In the Twitch interaction rules manager, add a test alias like `!panic -> clear`.
3. Save with the operator secret.
4. Send `!panic` from a moderator or broadcaster account.

## Channel point reward rules

- Enabled reward-ID rules take precedence; rules without an ID use exact case-insensitive title matching.
- Supported actions are `alert`, `brb`, `clear`, and `ignore`.
- Optional cooldown seconds suppress repeated matching reward actions and emit a diagnostic event while cooling down. Reservation is atomic and belongs to the EventSub client, scoped by broadcaster and Twitch reward ID (normalized title when an ID is absent). Reconnects preserve the client state; separate clients and channels do not share cooldowns. Reward renames retain cooldown when the reward ID stays the same. Expired entries are reclaimed as notifications arrive.
- Optional fulfillment notes are stored with the rule and included in emitted overlay event metadata for operator review.
- Title/message overrides support `{user}`, `{reward}`, and `{input}` variables.
- `ignore` suppresses the normal redemption alert and emits a `custom_command` diagnostic event. Diagnostic events can still appear in broad diagnostic surfaces such as `/overlay/debug`, event history, or ticker-style feeds that intentionally show generic event summaries.

Examples:

- `Change Color` matches `change color` and `CHANGE COLOR`.
- `ignore` is useful when you want to suppress the normal redemption alert while still leaving an operator-visible diagnostic trail.
- A cooldown is useful for expensive or disruptive rewards such as scene changes, long BRB prompts, or visual spam.

## Reward queue

The config page can load manageable Twitch rewards, create custom rewards with guarded optional limits, pause/resume or enable/disable a selected reward, delete a selected manageable reward after confirmation, then load unfulfilled redemptions for a selected reward. Reward lifecycle, create/delete, and fulfill/cancel controls call Twitch Helix with `channel:manage:redemptions`.

When a loaded reward or redemption title matches an enabled channel point rule, the reward queue shows the saved fulfillment note next to the selected reward context and each matching redemption. Use this for operator-only instructions such as “refund if no input”, “read aloud after current segment”, or “check OBS scene before fulfilling”.

Stabilization QA evidence:

- [ ] Loading rewards defaults to manageable rewards.
- [ ] Selected reward context shows enabled/disabled, paused/not paused, and out-of-stock state when Twitch returns it.
- [ ] Pause/resume and enable/disable controls preserve the selected reward after refresh.
- [ ] Delete remains separated behind an operator confirmation checkbox.
- [ ] Fulfillment notes appear for matched selected rewards and matching redemptions, but do not trigger fulfillment automatically.
- [ ] Advanced reward fields use `0` as disabled in the dashboard, backend update calls send disabled paired flags, and pause/disable remains the preferred reversible control before delete.

Safety boundaries:

- Reward creation supports title/cost/prompt/enabled/user-input-required plus guarded cooldown and max redemption limits: global cooldown seconds, max per stream, and max per user per stream. Reward updates support enabled/paused/prompt/user-input-required plus those guarded limits; title and cost changes stay in Twitch for now. Use `0` to disable optional limit fields. High-risk fields such as `should_redemptions_skip_request_queue`, background color/image styling, and reward image upload stay in Twitch for now.
- Pause/resume and enable/disable are the preferred reversible lifecycle controls for temporary changes.
- Reward deletion requires an operator confirmation checkbox. Twitch can mark pending unfulfilled redemptions as fulfilled when a reward is deleted, so prefer pause/disable before delete.
- Reward listing defaults to manageable rewards only.
- Fulfill/cancel only applies to explicit operator-selected unfulfilled redemptions.
- Fulfillment notes are display-only guidance; they do not automatically fulfill, cancel, refund, or create rewards.
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

Then repopulate valid rules from `/dashboard/chat`.

## Live QA

- Open `/dashboard/health` and confirm `twitch_interactions` is `ok`.
- Open `/overlay/debug` and verify alias attempts emit `custom_command` diagnostics.
- Test one broadcaster/mod alias, one channel point `alert`, one `clear`, and one `ignore` rule.
- Confirm `ignore` suppresses the redemption alert and produces only the diagnostic event in debug/history surfaces.

## Stable reward ID rules

Set **Reward ID (optional)** to the Twitch reward ID to keep a rule attached when its title changes. Its reward title becomes a display label and may be empty. Leave the ID blank for the existing case-insensitive exact-title match. Enabled ID rules take precedence over title rules; an ID rule never matches a different reward by its label. Duplicate IDs keep the first configured rule.

IDs persist in backend rules JSON and export/import. The reward queue uses the same precedence for selected reward and redemption fulfillment notes. Rule IDs and redemption/reward IDs are identifiers, not credentials. Load rewards and copy the displayed Reward ID from Selected reward, or use an existing rules export.

## Reward operation receipts

The reward queue allows one write at a time. While creating, editing, pausing/enabling, deleting, fulfilling, or canceling, queue controls and reload actions are disabled. Delete consumes its confirmation when the request starts. A failed write retains its draft for retry.

Successful writes update the visible returned reward or remove the completed redemption immediately, and show a success receipt. If the following reload fails, the receipt stays visible beside the reload error. Reload failures do not mean the write failed. Older list responses cannot overwrite newer write results. Changing API or operator credentials invalidates pending UI responses and clears the old queue; it cannot undo an already-sent request.
