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

## Implementation status, October 6

The original notes above are preserved as design history. Current source uses these surfaces:

- Theme/style: overlays use SUBCULT `sc-*` primitives and `web/src/styles/overlay.css`; the operator desk uses `desk-*` primitives and `web/src/styles/desk.css`. Both stylesheets enter Tailwind's components layer. The earlier shared HUD/Dracula dashboard direction has been superseded.
- Dashboard split: the primary routes are On air (`/dashboard`), Scenes, Chat, Overlays, Health, Settings, and Sessions. Earlier `/dashboard/live`, `/dashboard/control`, `/dashboard/setup`, `/dashboard/sources`, `/dashboard/bot`, and `/dashboard/config` paths resolve through the legacy-route map.
- Setup workflow: the checklist lives on `/dashboard/overlays`; state persists locally, completed items hide by default, and a completion notice remains visible. Operators can show completed items to revise them.
- Operator secret: Settings edits the secret, fills the password field from browser session storage, and indicates whether it is loaded. Shared dashboard preferences exclude secrets.
- Overlay URLs/presets: Overlays contains copyable source URLs, demo previews, editable presets, and custom CSS snippets.
- Stream controls: Scenes contains OBS controls, sample events, rehearsal, and configurable/confirmable quick actions. On air contains the live session summary, BRB, and quick-action workflow.
- Readiness/recovery: Health contains backend readiness, Twitch/EventSub diagnostics, build identity, and the fixed recovery console. Sessions contains reports, readiness snapshots, operator action logs, and persisted history.
- Bot operations: Chat contains commands, timers, API variables, counters/quotes, moderation/nuke, interaction rules, and reward queue operations.
- Overlay behavior: alerts/chat/ticker have configurable noise controls; goals/ticker/BRB state is retained and replayed after reconnect. All overlays have useful `?demo=1` rendering.
- Packaging/recovery: Compose dependencies, packaged non-root runtime/volume repair, local process recovery, backup targets, and isolated restore have recorded evidence in `docs/qa/`.
- Live QA: an earlier user-reported pass was OK. The overnight changes still require the actual live Twitch/OBS and production environment checks in `docs/runbooks/live-qa.md`; local fixtures do not replace those checks.

See [the current roadmap acceptance map](qa/2026-10-06-roadmap-acceptance.md) for local verification and remaining external gates.
