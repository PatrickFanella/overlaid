# Over-Laid Newsroom extension

Load this directory as an unpacked extension from the Chromium extensions page. In `/dashboard/newsroom`, open Newsroom settings and generate an add token. Open the extension options, enter your API origin and token, and save the connection. The extension requests permission for that origin only.

The toolbar button sends the current page to the approved reaction deck. The context menu sends the selected link. Refresh destinations in options to select a shelf or show segment. A shelf destination keeps the link for later. The add token cannot change provider settings, control OBS, or read private notes.

Tokens live in this browser profile’s local extension storage. Rotate or revoke the token in Newsroom settings when the profile is retired. Use HTTPS for a LAN/public API. The server returns errors for stale destinations, invalid URLs, duplicates, and the one-second quick-add rate limit.

This extension rebuilds the upstream Newsroom quick-add workflow. See [source provenance](../docs/vendor/newsroom/PROVENANCE.md) and the retained [MIT notice](../docs/vendor/newsroom/LICENSE).
