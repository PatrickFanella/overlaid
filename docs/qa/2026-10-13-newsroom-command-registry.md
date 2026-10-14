# Shared Newsroom command verification

Newsroom Twitch actions now use the general bot command registry and dispatcher. Chat → Bot commands manages link submissions, bare-link capture, current-source replies, questions, and raffle entry. Retired Newsroom parser/settings are removed; legacy fields are read only for a persisted one-time import.

## Local evidence

- `bash scripts/ci-postgres.sh` passed, including `make verify`, disposable PostgreSQL integration fixtures, and the full uncached Go race suite. All 121 frontend tests passed.
- Regression coverage verifies supporter roles, cooldowns, renamed and paused commands, capture plan admission, queued pause fencing, immutable published events, command/alias collisions in both directions, import name collisions, and deletion surviving import replay.
- The isolated browser preview listed all four seeded actions in Chat with Edit, Enable/Pause, and Delete controls. It renamed `news` to `submit`, selected Subscriber or VIP, saved without a static response, enabled and paused the entry, then created and deleted a bare-link capture action.
- Restarting the isolated API retained the renamed command, supporter role, paused state, and import marker. The deleted capture entry remained absent. Desktop layout had no horizontal overflow.
- Screenshot: `/home/onnwee/.t3/userdata/browser-artifacts/browser-screenshot-127-0-0-1-mv7e7l4z-c7a1b82b.png`.

Preview data lives under `/tmp/overlaid-newsroom-qa/`, with API port 41881 and web port 41882. Twitch and provider credentials were disabled. No real messages or external enrichment calls were sent. This receipt supplements the [Newsroom rebuild evidence](2026-10-13-newsroom-rebuild.md).

File-mode command persistence supports the same manager without PostgreSQL; durable bot audits, timers, and community storage still need PostgreSQL. Live Twitch delivery and hosted CI remain unverified. These changes are local and uncommitted; no push, merge, or deployment is claimed.
