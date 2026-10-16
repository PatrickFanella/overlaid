# Overlays

The on-air overlays use the SUBCULT "poster press" pack: purple fields, ink type, acid-green blocks and registration marks, with Oswald, Space Grotesk and JetBrains Mono. The editing master for the brand is `subcult-tv/studio/brands/subcult`. `web/public/brand/subcult/PROVENANCE.md` records which files were copied and from which commit.

The dashboard is deliberately a separate, neutral design. Nothing in `src/styles/desk.css` should appear on air.

## Routes and OBS sizes

Add each route as an OBS Browser Source at the listed size. `/dashboard/overlays` shows a live preview of each one and copies the URL.

| Route | Size | What it shows |
| --- | --- | --- |
| `/overlay/alerts` | 1000×720 | Newest alert large, older alerts stepped down. Severity sets the field: info purple, success green, warning bone, error ink. |
| `/overlay/chat` | 600×900 | Chat column filling from the bottom. Each new line flashes purple, then settles to ink. |
| `/overlay/goals` | 780×640 | One primary goal with quarter ticks, up to three secondary meters. |
| `/overlay/now-playing` | 920×260 | MPRIS track strip. Falls back to the fisheye circle mark when there is no album art. |
| `/overlay/ticker` | 1920×72 | Full-width lower rail. Scrolls once there are two or more items. |
| `/overlay/newsroom` | 1920×1080 | Current approved story or selected audience question. See the [Newsroom guide](newsroom.md) for privacy and playback behavior. |
| `/overlay/camera` | 1920×1080 | Purple frame, corner registration marks and a nameplate. Size it to the camera source and stack it on top. |
| `/overlay/starting` | 1920×1080 | Full-frame countdown card. |
| `/overlay/brb` | 1920×1080 | Full-frame intermission card driven by `brb` events from the dashboard. |
| `/overlay/ending` | 1920×1080 | Full-frame end card on ink. |
| `/overlay/debug` | 1000×900 | Event inspector for setup. Not an on-air asset. |

## Query parameters

Every overlay:

- `?demo=1` plays sample events locally and never connects to the API. Use it to check layout in OBS without Twitch or a running stream.
- `?status=1` keeps the connection pip visible. It normally hides while the stream is connected, so it never shows up on air.
- `?apiBaseUrl=http://host:8080` overrides the API origin.
- `?key=<OVERLAY_TOKEN>` authorizes live overlay reads. Copy the URL from the Overlays page; never use the operator secret. Demo mode needs no credential.

Empty states such as "Waiting for Twitch chat" show only while connecting or reconnecting. Once the event stream is live they disappear, so a quiet chat at the start of a stream stays blank.

`/overlay/starting`:

- `?minutes=5` counts down from when the source loads. The default is 5.
- `?at=20:00` counts down to a local wall-clock time and overrides `minutes`. A time already past by more than a minute rolls to the next day.
- `?title=`, `?message=` replace the headline and supporting line. The last word of the title sits on the green slab.
- `?items=Archive bugs|Clip queue|Q&A` adds up to four agenda lines. URL-encode `&` as `%26`.

`/overlay/ending`:

- `?title=`, `?message=` as above.
- `?next=Thursday 8PM` adds a "next stream" block.
- `?link=subcult.tv` sets the destination line. The default is `subcult.tv`.

`/overlay/camera`:

- `?name=` sets the nameplate. The default is `patrick__eff`.
- `?role=` adds a second line. Omit it to hide the line.
- `?frame=14` sets the frame thickness in pixels.
- `?mark=0` hides the corner mark.

The BRB, starting and ending cards also show the current now-playing track in their footer when MPRIS events arrive.

## Brand rules that apply here

- Green on purple is decorative. Never set small text in green on purple.
- Use the `ink` mark on purple, green or bone fields and the `white` mark on ink. In code these are the `tone` props of `Mark` and `Lockup` in `web/src/overlay/Brand.tsx`. The pack's own file names describe the field, so `mark-dark.svg` has white letters.
- Keep the mark at least 32px wide and the lockup at least 220px wide. Do not redraw, recolor or add effects to the marks.
- Motion is limited to wipes, the slab reveal and the chat flash. Everything stops under `prefers-reduced-motion`.

## Updating the brand files

1. Pull the pack in `subcult-tv` and check `studio/brands/subcult/GUIDE.md` for changed rules.
2. Copy the changed SVGs into `web/public/brand/subcult/`. The circle PNG is in Git LFS. If the checkout only has a pointer file, extract it with `git show HEAD:studio/brands/subcult/logos/mark-fisheye-circle.png | git lfs smudge > mark-fisheye-circle.png`.
3. Update the commit and hashes in `PROVENANCE.md`.
4. Run the visual QA overlay pass in `visual-qa.md`.

## Stream access and shared display settings

Set `OVERLAY_TOKEN` separately from `OPERATOR_SECRET`. Authenticate the desk with the operator secret, then copy Browser Source URLs from Overlays; they include `?key=<OVERLAY_TOKEN>` and any API base override. Direct overlay URLs must carry the same key. Keep keyed URLs private and rotate the environment value plus OBS sources if a URL leaks. Reverse proxies must redact query credentials from logs. `?demo=1` previews stay offline and require no key.

The stream and display-settings API validate the key. History uses the operator header instead. Empty credentials are permitted for local development; LAN/public requests fail closed when the matching credential is missing, and `/readyz` reports degraded configuration. The read-only display endpoint returns no quick actions, integrations, or operator configuration. Save shared display settings in the desk and reload an OBS source to apply them; reconnect also refreshes settings. API failures use default display values rather than the OBS browser's localStorage.
