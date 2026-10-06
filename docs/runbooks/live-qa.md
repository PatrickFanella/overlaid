# Live QA Runbook

Canonical checklist for off-stream rehearsal, live-start validation, and post-stream evidence. Use this before real streams and after any deployment that changes dashboard, overlay, Twitch, OBS, or event-history behavior.

## Preconditions

- [ ] Latest code is deployed with `make docker-build`.
- [ ] `make smoke` passes against the running stack.
- [ ] `make smoke-browser` passes from the operator machine.
- [ ] `/readyz` is `ok`, or every degraded check has an accepted reason.
- [ ] OBS is open with the intended scene collection.
- [ ] Dashboard operator secret is loaded if `OPERATOR_SECRET` is configured.

## Phase 1: static dashboard and overlay pass

Follow `docs/runbooks/visual-qa.md` after route, CSS, or layout changes.

Minimum live-day route pass:

- [ ] `/dashboard/health` loads without console-visible failures.
- [ ] `/dashboard/scenes` shows expected quick actions and disabled-state reasons.
- [ ] `/dashboard/settings` can validate OBS quick action assignments.
- [ ] `/overlay/alerts`, `/overlay/ticker`, `/overlay/goals`, `/overlay/brb`, and `/overlay/now-playing` fit the OBS canvas.

## Phase 2: one-click preflight rehearsal

1. Open `/dashboard/sessions` (or `/dashboard`, which has the same session panel).
2. Click **Run preflight rehearsal**.
3. Confirm the session starts and ends automatically.
4. Open the generated report.

Expected evidence:

- [ ] Report contains readiness snapshots for `start` and `end`.
- [ ] Report contains tagged alert, ticker/goal, and BRB events.
- [ ] Readiness snapshots have no unexpected degraded checks.
- [ ] Export Markdown and JSON if this is a release/rehearsal record.

## Phase 3: OBS control validation

1. Open `/dashboard/settings`.
2. Click **Load from OBS**.
3. Confirm assigned main scene, standby/BRB scene, and alerts source match the real OBS collection.
4. Click **Validate quick actions**.
5. Fix missing scene/input/source warnings before continuing.
6. Open `/dashboard/scenes` and run the safe rehearsal quick actions.

Expected evidence:

- [ ] Main scene switch succeeds.
- [ ] Standby/BRB scene switch succeeds.
- [ ] Alerts browser source refresh succeeds.
- [ ] Session log records action durations and failures clearly.

## Phase 4: Twitch/EventSub validation

1. Confirm `/dashboard/health` shows Twitch authenticated and EventSub connected.
2. Confirm EventSub diagnostics show expected subscription count.
3. If `missing scopes` appears, re-run Twitch login and re-consent.
4. From a broadcaster/moderator account, send a safe chat command such as `!alert command smoke`.
5. Open `/dashboard/chat` and confirm bot status, recent audit, commands, timers, counters/quotes, API variables, moderation dry-run, and nuke planner panels render.
6. Open `/dashboard/chat`, load Twitch rewards, and verify the reward queue shows manageable rewards without requiring deletion.
7. Trigger a configured channel-point test if available off-stream.

Expected evidence:

- [ ] `/overlay/debug` shows incoming Twitch diagnostics.
- [ ] Chat command from broadcaster/moderator produces the expected safe overlay or chat event.
- [ ] Viewer/non-moderator commands are denied and recorded as diagnostics/audit events.
- [ ] `/dashboard/chat` shows recent audit entries for command/moderation checks without exposing tokens or raw EventSub payloads.
- [ ] Reward queue can load manageable rewards and shows lifecycle state, fulfillment notes, and reversible pause/disable controls before delete.
- [ ] EventSub reconnect attempts and last disconnect remain stable during the pass.

## Phase 5: recovery drill

Run at least one recovery action off-stream:

- [ ] Refresh OBS browser source or reload the overlay route.
- [ ] Restart API and confirm retained BRB/overlay state and recent history behave as expected.
- [ ] Confirm `/readyz` returns to `ok` after the restart window.
- [ ] Confirm `event_pipeline` has no unexpected dropped deliveries.

## Phase 6: post-stream notes

After rehearsal or live stream:

- [ ] End any active stream session.
- [ ] Open the latest session report. Check Session bot actions against the actions performed during that session, including loaded/total counts and any audit-unavailable warning.
- [ ] Export Markdown/JSON if the pass found issues or confirms release readiness.
- [ ] Record recurring failures in the relevant runbook or issue tracker.
- [ ] Back up Postgres and `api-data` if the session created important operational history.

Controlled workspace customization evidence:

- [ ] Capture the default `/dashboard/health` layout before rehearsal.
- [ ] If layout customization is used, reset to default before handoff or document the operator-specific order in evidence notes.
- [ ] Confirm the recovery console stays fixed above the arrangeable panels on `/dashboard/health`, and session controls stay fixed on `/dashboard` and `/dashboard/sessions`.

## Latest QA status

Current status: automated checks and one-click rehearsal coverage are in place. Real-stream QA should continue to use this runbook, because Twitch chat, channel-point redemptions, OBS scene collections, and browser-source behavior depend on the live operator environment.

When updating this status, include date, deployed commit, rehearsal/session report ID, OBS collection used, and any accepted degraded readiness checks. Do not paste secrets, OAuth tokens, or webhook payloads.


### October 6 local process recovery evidence

The disposable native API/Postgres drill passed against source commit `13b4886`. See [the recorded drill](../qa/2026-10-06-local-recovery-drill.md) for the session ID, replay/history checks, probe transitions, cleanup, and accepted degraded checks. This verifies local process recovery; OBS browser-source reload, live Twitch reconnect, Docker deployment, and production outage recovery still require their actual environments.
