# overlaid notes

---

## Theme/style

- use semantic tokens, and create tailwind utility classes to be reused across the project
- implement the neubrutal-hud skill theme
- custom css for the stream elements would be cool using our utility classes taking advantage of the themeability of tailwind

## Dashboard

- break this into multiple screens/routes
- the setup workflow should disappear if it is complete and green.
- move the operator secret to a config route, if it is entered, keep the input filled or indicate it somehow
- make the system status more compact, maybe in a sticky element
- put the overlay url cards in their own route with the stream presets
  - make the stream presets editable
- there should be a stream control route this should include:
  - fire events, brb screen should be one component
  - scene controls
  - an editable element with quick actions such as secene buttons that will activate the scene, refresh element, fire event, turn off alerts, ect, anything useful as defaults and other custom things

---

## Implementation status

- Theme/style: implemented with semantic HUD utility classes in `web/src/styles.css`, the Neubrutal HUD design direction, Dracula colors, and terminal-style overlay/dashboard surfaces.
- Dashboard split: implemented with `/dashboard`, `/dashboard/live`, `/dashboard/setup`, `/dashboard/sources`, `/dashboard/control`, and `/dashboard/config`.
- Setup workflow: implemented on `/dashboard/setup`; checklist state persists locally and completed items can be hidden.
- Operator secret: moved to `/dashboard/config` and stored in browser session storage.
- Overlay URLs/presets: moved to `/dashboard/setup` and `/dashboard/sources` with copyable source URLs and editable stream presets.
- Stream control route: implemented on `/dashboard/control` with sample events, rehearsal runner, BRB controls, OBS controls, and editable/confirmable quick actions.
- Live readiness: implemented on `/dashboard/live` with Twitch auth, EventSub, OBS, operator-secret, and setup preflight checks.
- Runtime diagnostics: Twitch EventSub diagnostics are exposed through backend status and dashboard UI.
- Overlay behavior: alerts/chat/ticker have configurable noise controls; goals/ticker/BRB state is retained and replayed to reconnecting browser sources.
- Deployment: production guardrails, Compose health checks/restart policy, and live preflight runbook are documented.
- Live QA: user completed a live QA pass; status is OK so far.
