# Phase 1 homelab acceptance — October 13, 2026

Phase 1 implementation merged in [PR #45](https://git.subcult.tv/subculture-collective/overlaid/pulls/45) at `6de594f`. Exact-head hosted CI passed for the PR source `97f73f3` (run 15224, job 28179) and merged main `6de594f` (run 15229, job 28184), including a fresh disposable PostgreSQL database and the full Go race suite. All 29 local Chromium fixtures passed.

The owner selected the homelab and delegated placement. Discovery found clean source checkouts but no prior runtime or route. NUC now runs the new public self-host installation; Dozor supplies monitoring/alerting and Almaz supplies ingress. This establishes Phase 1 operational basics, not a completed Twitch/OBS release or a public multi-tenant launch.

## Deployed artifact and boundaries

- Source artifact: `97f73f39cd105ecc25a2c0e24b948e973bacee20`, merged unchanged in `6de594f`.
- API image: `sha256:062a8b997be8e9e0b6cd5c313b42bdf7e0a88a0628300f7e6509a96d0c05d679`.
- Web image: `sha256:358c33d725bef1ba38a7d4d657e9b3cb9ce08128a6b8de54fb1fbfe95ad4da49`.
- NUC release: `/srv/apps/projects/overlaid/releases/phase1-97f73f3`, selected by `current`. All four service images are pinned in its private Compose selection and recorded in `release.json`.
- API listens on loopback 3084, PostgreSQL on loopback 55438, web on LAN 3083 and metrics on LAN 3085. UFW allows only Almaz ingress and Dozor probes on the new LAN listeners. The private database network uses verified-free `10.252.83.0/28`; default Docker pools were exhausted, and existing networks were preserved.
- `app.env` has mode 0600 in a 0700 service root. Its generated operator, overlay, webhook and vault credentials are not in Git or this receipt. Twitch EventSub and MPRIS are disabled until provider/desktop qualification.

At 22:38–22:40 UTC, public HTTPS `/status`, `/privacy`, `/terms`, `/version`, `/readyz`, `/api/public/status` and anonymous account metadata returned 200 through the existing wildcard ingress. Anonymous history and shared configuration returned 401. Authenticated operator reads returned 200; keyed SSE returned 200 and its connected handshake. The browser rendered the public status report with all four checks healthy and no horizontal overflow. `/version` reports `phase1-97f73f3`, commit `97f73f3`, Go 1.26.9.

Caddy and all existing Prometheus rules validated before configuration reload. Files were changed in place with inode/hash guards and rollback copies. Prometheus and Alertmanager retained their pre-existing container start times and zero restarts. Existing consumers were checked after reload; this was not a Docker or monitoring-service restart.

## Monitoring and real alert delivery

Dozor scrapes five independent role metrics: process, tenant storage, retention worker, access policy and web. The exporter parses readiness JSON rather than treating HTTP 200 as sufficient. Missing named checks fail. An Overlaid-only route uses the existing ntfy receiver; no notification credential was copied or changed.

A controlled fault stopped only `overlaid-readiness-1` at 22:39:22 UTC. API and web remained healthy. Prometheus transitioned from pending to firing, and Alertmanager selected `ntfy` with no silence or mute. The ntfy cache recorded published server-side receipts:

| State | Provider ID | UTC epoch |
|---|---|---|
| Generic critical firing | `2L2oTH7H7Dke` | 1791931271 |
| Overlaid probe warning firing | `JiOITMO28cCj` | 1791931335 |
| Generic critical resolved | `Z5AHpBM5D6ok` | 1791931361 |
| Overlaid probe warning resolved | `BkLQY0ozbMck` | 1791931365 |

The exporter restarted successfully. Prometheus returned `up=1` and Alertmanager returned no active Overlaid alerts. The drill revealed duplicate generic/specific notifications; a validated inhibition now suppresses the Overlaid probe warning while the generic `ServiceDown` critical alert for the same project/instance is active. This narrow rule does not affect other products. Provider publication and resolution are proven; a user's device displaying either notification is not claimed.

## Backup and independent restore

The nightly `overlaid-backup.timer` is enabled. Its first scheduled run is October 14 at 03:38:22 CDT, within the configured 03:35 plus ten-minute jitter window. The first backup was invoked manually and completed successfully; a natural nightly run has not yet occurred.

NUC backup `/srv/apps/projects/overlaid/backups/20261013T223824Z` contains the custom dump, matching private environment/keyring, migrations, Compose selection, release receipt and checksums. Dump size is 76869 bytes; SHA-256 is `64aca71bc43218dd7fde7ed71d302a2cba4eafa9937784effc2e87187de5fec9`.

An independent PostgreSQL container with `network=none` restored that dump. Restricted roles were recreated, migration 0003 reapplied and a non-owner reader verified the self-host owner, one tenant, zero commands, all 18 forced-RLS tables and `overlaid_credential_lookup` function ownership. The owned restore container was removed. The new installation contains no real provider tokens; live credential continuity is not claimed. Separate local fixtures proved legacy command preservation and encrypted token import/rotation/restart.

## Recovery and remaining qualification

Private, syntax-checked recovery scripts are retained:

- NUC: `/srv/apps/projects/overlaid/recovery/rollback-new-service.sh` stops only Overlaid, disables its new timer and removes its narrow firewall rules while preserving volumes, images, environment and backups. It rejects a later active release or an active backup.
- Almaz: `/home/onnwee/.local/state/overlaid-20261013/rollback-caddy.py` restores the saved ingress only when its current hash matches this change.
- Dozor: `/srv/apps/monitoring/recovery/overlaid-20261013/rollback-monitoring.py` restores saved monitoring configuration only when both current hashes match this change.

Rollback is prepared, not executed against the running service. Real Twitch OAuth/EventSub/chat acceptance remains #39; real OBS acceptance remains #40. #42's named release still requires those gates, credential/data continuity and an actual target rollback. Long-lead registrations remain #23. No tag or Gitea release was published.

See [tenant migration/recovery](../runbooks/tenancy-and-recovery.md) and [homelab deployment](../runbooks/homelab-phase-1.md). Private host receipts contain full manifests and configuration backups; public documentation and Git exclude their credentials.
