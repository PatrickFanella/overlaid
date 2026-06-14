# Visual QA Runbook

Manual route-by-route checklist for confirming overlay and dashboard rendering after rebuilds, CSS changes, or route work.

## Before you start

- [ ] `make docker-build` completed successfully.
- [ ] `make smoke` passed against the running stack.
- [ ] OBS is running if you want to verify browser sources live.
- [ ] Browser cache is cleared or a private window is open.
- [ ] Dashboard operator secret is available if protected routes are enabled.

Open routes at the deployed web origin, for example `http://<host>:5173`. Check dashboard routes at a standard desktop/laptop viewport and overlay routes at a 16:9 OBS canvas such as 1920×1080.

## Dashboard routes

### `/dashboard`

- [ ] Page loads without a blank screen or console overlay.
- [ ] Header, status cards, build-info card, and ops route guide are readable.
- [ ] Route guide points operators to live recovery, config/rules, stream control, sources, and packaging/backup guidance.
- [ ] No text overflows or clipped panels at the default desktop width.

### `/dashboard/live`

- [ ] Recovery console, build info, backend readiness, live preflight, EventSub diagnostics, setup checklist, session log, and event history panels are visible.
- [ ] Recovery-console runbook links are clearly external and do not look like destructive browser actions.
- [ ] Twitch, EventSub, OBS, operator-secret, event-history, and interaction-rule indicators are easy to scan.
- [ ] Degraded states are obvious without horizontal scrolling.

### `/dashboard/setup`

- [ ] Setup checklist is readable from top to bottom.
- [ ] Completed/open setup decisions are visually distinct.
- [ ] Checklist hiding or completion states do not leave empty-looking panels.

### `/dashboard/sources`

- [ ] Source URL helpers and any alias content are legible.
- [ ] Layout still works with long hostnames and URLs.
- [ ] Browser-source URLs render as copyable text blocks.
- [ ] Stream preset cards and guidance sections do not overlap.

### `/dashboard/control`

- [ ] Control groups are separated clearly.
- [ ] Quick actions, rehearsal controls, and OBS controls remain visible.
- [ ] Buttons have obvious hover/active states and no wrapping glitches.
- [ ] Session log is compact, readable, and does not crowd the primary controls.

### `/dashboard/config`

- [ ] Operator secret inputs, automation profiles, Twitch interaction rules, MPRIS decision, and overlay noise controls are aligned.
- [ ] Import/export textareas stay readable and do not expose operator secret values.
- [ ] Configuration panels stay readable on a standard laptop viewport.
- [ ] No form controls are clipped by cards or columns.

### Control/config interaction checks

- [ ] On `/dashboard/control`, confirm destructive quick actions show the `Confirm ...` armed state before running.
- [ ] Confirm quick-action sequences only show when at least one runnable child action is available in the full catalog.
- [ ] Run a rehearsal off-stream and confirm the session log records success/failure summaries.
- [ ] On `/dashboard/config`, confirm operator secret text remains session-only and is not present in dashboard config export JSON.
- [ ] Import a Twitch interaction rules JSON sample and confirm invalid actions are normalized before save.

## Overlay routes

### `/overlay/alerts`

- [ ] Alert container is centered and balanced in the frame.
- [ ] Title, message, and severity styling remain readable at a glance.
- [ ] Empty state or idle state does not crowd the screen.

### `/overlay/chat`

- [ ] Chat rows are aligned and easy to scan.
- [ ] Long usernames/messages wrap cleanly.
- [ ] Timestamp and metadata treatment matches the rest of the HUD.

### `/overlay/goals`

- [ ] Goal labels, counts, and progress cues stay legible.
- [ ] Progress bars or rails do not clip at the edges.
- [ ] The route still feels like a minimal broadcast overlay.

### `/overlay/now-playing`

- [ ] Track metadata is readable and not overly dense.
- [ ] Artist/title truncation behaves cleanly.
- [ ] Loading or no-track states remain visually calm.

### `/overlay/ticker`

- [ ] Ticker height is stable and text baseline looks aligned.
- [ ] Long messages scroll or wrap without breaking the lower third.
- [ ] Recent event styling stays consistent across severities.

### `/overlay/brb`

- [ ] BRB title, message, ETA, and item list are easy to read.
- [ ] The intermission screen fills the frame cleanly.
- [ ] Empty/hidden state does not leave stray artifacts.

### `/overlay/debug`

- [ ] Debug content is clearly secondary to production overlays.
- [ ] Raw state, diagnostics, or test data remain contained.
- [ ] Nothing spills outside the viewport.

Overlay routes should remain sparse and broadcast-safe. Generic diagnostic or command events may appear in ticker/debug routes; production overlays should not expose raw secrets or token material.

## Finish

- [ ] Recheck the routes you changed most recently.
- [ ] Confirm typography, spacing, and contrast are still consistent.
- [ ] If a route regressed, capture the exact route and viewport before changing code.
