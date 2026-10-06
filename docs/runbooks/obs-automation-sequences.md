# OBS Automation Sequences Runbook

Dashboard-only quick action sequences let an operator chain existing atomic OBS/dashboard actions into one confirmed button press.

## What sequences are

- Sequences live in the dashboard config stored in browser localStorage.
- A sequence references existing quick action IDs in order.
- A sequence can only run through the dashboard control UI.
- Sequences reuse the same protected API helpers as normal quick actions.
- Sequences do not accept arbitrary scripts, chat triggers, or backend workflow storage.

## Safety model

- The parent sequence confirmation is the safety gate.
- Child action `confirm` prompts are not shown again inside a sequence.
- Nested sequences are skipped rather than executed.
- Missing action IDs are skipped and reported.
- `stopOnError: true` stops at the first failed step.
- `stopOnError: false` continues through the remaining steps and reports all failed labels at the end.
- Sequence failures and warnings are written to the dashboard session log.

## Default sequences

The default dashboard config includes:

| Label | Child actions | Notes |
| --- | --- | --- |
| `Go Live Main` | `main-scene` → `refresh-alerts` → `hide-brb` | Main streaming handoff. |
| `Start BRB` | `brb-scene` → `show-brb` | Switch to intermission flow. |

If a default sequence is missing after an update, refresh the dashboard config so the stable default IDs can be merged back in.

## Automation profiles and show modes

The dashboard now supports local automation profiles in `/dashboard/settings`. Use these as show modes, not as access control:

- `Full console` — everything visible; best for setup, testing, and editing.
- `Main live` — the normal live-show profile for primary scene switching and alert refresh actions.
- `BRB ops` — the intermission profile for BRB and standby controls.

Profile filtering changes what `/dashboard/scenes` and `/dashboard/overlays` show, but it does not delete any hidden quick actions or presets. The full catalog stays in config so you can switch profiles later without rebuilding the show setup.

For sequences, the visible button list is filtered by the active profile, but sequence execution still resolves child actions from the full catalog. That means a visible sequence can still call hidden child actions when they exist in the dashboard config, while missing child IDs are warned about and skipped.

Before changing a show setup or reshaping the profile list, export the dashboard JSON from `/dashboard/settings` first so you have a local backup of the current automation profiles and other dashboard settings.

## Editing a sequence

1. Open `/dashboard/scenes`.
2. Open the quick action editor.
3. Create or edit a quick action with kind `sequence`.
4. Enter the child quick action IDs in the order you want them to run.
5. Keep only existing atomic action IDs in the list.
6. Choose whether the sequence should stop on the first error.
7. Save and check for visible warnings before using it live.

Recommended editing rules:

- Use one action ID per line if the editor shows the list as plain text.
- Keep sequence labels short and obvious.
- Build sequences from tested atomic actions only.
- Avoid including other sequence actions.

## Pre-stream check

1. Open `/dashboard/scenes` and confirm the expected default sequences are visible.
2. Run `Go Live Main` in a safe rehearsal window.
3. Confirm the steps run in order: main scene, refresh alerts, hide BRB.
4. Run `Start BRB` and confirm the BRB scene and overlay appear together.
5. Check the session log for any missing-ID, nested-sequence, or action-failure warnings.
6. If the sequence reports warnings, fix the config before going live.

## Troubleshooting

- If a sequence does nothing, confirm the operator secret is loaded and the dashboard can reach OBS/API helpers.
- If only the first step runs, check whether `stopOnError` is enabled and whether that step failed.
- If a step is skipped, confirm the child ID still exists and is not another sequence.
- If the session log shows warnings, remove missing IDs and replace nested sequence references with atomic actions.
- If OBS control calls fail, verify OBS WebSocket connectivity, password, and the current scene/input names.
- `/api/obs/status` reports a safe `errorKind` such as `not_configured`, `connection_refused`, `dns_failed`, `timeout`, or `auth_failed`. The displayed OBS URL is redacted to omit user-info and query strings.
- If defaults disappeared after an update, reload the dashboard config so the stable default actions are restored.


### Pending operations and failure receipts

The operator action runner accepts one operation at a time. While a sequence is running, atomic quick actions and other controls using that runner remain disabled. Confirmation is consumed before execution. After failure, review the session log before retrying: completed steps remain completed, and `stopOnError` prevents later steps from running. A subsequent run requires confirmation again when configured. The runner releases its pending guard on success or failure, including a callback that throws before returning its promise.
