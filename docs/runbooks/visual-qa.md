# Visual QA Runbook

Manual route-by-route checklist for confirming overlay and dashboard rendering after rebuilds, CSS changes, or route work.

## Before you start

- [ ] `make docker-build` completed successfully.
- [ ] `make smoke` passed against the running stack.
- [ ] OBS is running if you want to verify browser sources live.
- [ ] Browser cache is cleared or a private window is open.
- [ ] Dashboard operator secret is available if protected routes are enabled.

Open routes at the deployed web origin, for example `http://<host>:5173`. Check dashboard routes at a standard desktop/laptop viewport and overlay routes at a 16:9 OBS canvas such as 1920×1080.

## Operator desk checks

- [ ] The sidebar groups pages into Show (On air, Scenes, Chat), Prepare (Overlays, Health, Settings) and Review (Sessions). Alt+1 to Alt+7 jumps between them.
- [ ] The top bar tally reads Off air, Rehearsal or On air and matches the stream session state. OBS, Twitch, EventSub, Ready and Operator lamps are readable without hovering.
- [ ] Old paths (`/dashboard/live`, `/control`, `/setup`, `/sources`, `/bot`, `/config`) land on the new page and the address bar updates.
- [ ] Keyboard focus shows an amber outline on links, buttons, inputs and disclosures.
- [ ] Tally red appears only for On air and destructive controls.

## Dashboard routes

### `/dashboard` (On air)

- [ ] Quick-action tiles, BRB control, session controls, preflight and latest events fit a laptop viewport without horizontal scrolling.
- [ ] With no operator secret, tiles are disabled and the reason is visible.
- [ ] Confirmable quick actions show the red armed state before they run, and disarm when focus leaves them.

### `/dashboard/scenes`

- [ ] OBS controls load inventory only after **Refresh OBS**. The program scene is marked.
- [ ] **Arrange modules** opens the module editor; hidden modules disappear from the page.
- [ ] The quick-action preset editor keeps sequence warnings visible.

### `/dashboard/chat`

- [ ] Section links jump to commands, timers, counters and quotes, API variables, moderation, Twitch rules and the reward queue.
- [ ] The nuke planner sits in its own red-edged panel with plan, review and execute steps in order.
- [ ] Reward delete stays behind the confirmation checkbox and below pause and enable controls.
- [ ] Dry-run results and audit rows do not expose tokens, cookies or raw EventSub payloads.

### `/dashboard/overlays`

- [ ] Every preview renders its overlay on the checkerboard. A white preview means the iframe lost transparency.
- [ ] Copy URL copies the browser-source URL; the copied tag appears.
- [ ] Checklist, stream presets and custom CSS stay readable with long hostnames.

### `/dashboard/health`

- [ ] The recovery console stays above the arrangeable panels.
- [ ] **Customize panel layout** opens the editor with `Reset layout` and the locked-panel copy.
- [ ] Degraded readiness checks are listed as incidents with their hints.

### `/dashboard/settings`

- [ ] The operator secret and webhook secret fields sit outside the arrangeable panels.
- [ ] Export JSON does not contain the operator secret.
- [ ] OBS import shows the scene and input counts and lists validation issues.

### `/dashboard/sessions`

- [ ] Start live, start rehearsal and end session buttons match the session state.
- [ ] Opening a recent session shows its report, snapshots and tagged events; Markdown and JSON export work.

## Overlay routes

Check overlays at 1920×1080 and at the OBS sizes listed in `overlays.md`. Use `?demo=1` to fill them with sample events. Put a busy image behind the browser source; the overlays must stay readable over gameplay.

- [ ] `/overlay/alerts`: newest alert is large, older alerts are smaller. All four severity fields (purple, green, bone, ink) are readable. The slab covers the whole last word.
- [ ] `/overlay/chat`: lines fill from the bottom, long names truncate, long messages wrap, badges read as mod, sub, vip or host.
- [ ] `/overlay/goals`: numbers do not clip, quarter ticks cut through the fill, secondary meters line up.
- [ ] `/overlay/now-playing`: long titles truncate with an ellipsis; the paused state dims the text; no album art shows the circle mark.
- [ ] `/overlay/ticker`: the rail sits flush at the bottom, scrolls without a visible seam, and the brand block stays fixed.
- [ ] `/overlay/camera`: frame and corner marks align with the camera source; nameplate text is not clipped.
- [ ] `/overlay/starting`, `/overlay/brb`, `/overlay/ending`: the slab sits behind the headline without covering the line above, long `next` values wrap inside the side block, and the mark is bottom-right with clear space.
- [ ] `/overlay/debug`: shows JSON for recent events and the connection state.
- [ ] Without `?demo=1` or `?status=1`, no connection pip or "waiting" copy is visible once the stream connects.

Overlays must not expose secrets or token material. Diagnostic and command events may appear on the ticker and debug routes only.

## Finish

- [ ] Recheck the routes you changed most recently.
- [ ] Confirm typography, spacing, and contrast are still consistent.
- [ ] If a route regressed, capture the exact route and viewport before changing code.

## Style separation checks

- The dashboard uses only `desk-*` classes and IBM Plex. No SUBCULT purple, slab or Oswald appears in the dashboard.
- Overlays use only `sc-*` classes and the brand fonts. No dashboard grey panels appear on air.
- Monospace stays in use for logs, IDs, JSON and URLs.
