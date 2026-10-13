# Phase 1 homelab deployment

The owner selected the homelab and delegated host placement on October 13, 2026. Read-only discovery found no existing Overlaid service on NUC, Almaz, Dozor or Kvant, no Kuma monitor and no active Caddy route. Almaz and Dozor contain clean source checkouts at `55327be`; these are not deployments.

## Placement and release

Use NUC for the API, web and PostgreSQL; Dozor owns Prometheus, Alertmanager and Uptime Kuma; Almaz owns public ingress. The working hostname is `overlaid.subcult.tv`. This remains the working product name rather than a completed final-brand decision under #23.

NUC preflight at October 13 22:12 UTC found roughly 13 GiB of available memory and 79 GiB free on `/srv`. Ports 3083, 3084 and 55438 were unused. Source `deploy/compose.nuc.yaml` bounds PostgreSQL to 1 GiB, API to 512 MiB, web to 128 MiB and the read-only probe adapter to 64 MiB. Use exact source commit and immutable API/web image IDs in a private release directory under `/srv/apps/projects/overlaid/releases/`; make `current` point to the selected release. Do not modify the synchronized source checkouts.

Copy `compose.nuc.yaml`, `nginx.nuc.conf`, migrations and scripts into the release root. Store `app.env` with mode 0600, distinct administrator/runtime database passwords, operator/overlay/webhook secrets, vault keyring and image IDs. Record revision, image IDs and timestamps in `release.json`, without secrets. Build the web image with `VITE_API_BASE_URL=https://overlaid.subcult.tv`. Initial self-host deployment disables Twitch EventSub and MPRIS until their provider/desktop acceptance is complete; a homelab container cannot prove OBS access on the streaming desktop.

The API listens only on NUC loopback 3084, PostgreSQL on loopback 55438. Web listens on NUC LAN 3083, and the readiness exporter on 3085. Add narrow UFW access for Almaz to web and Dozor to web/probe; host networking preserves UFW enforcement. Do not publish database or API admin listeners on the LAN. NUC default Docker pools are exhausted; Overlaid uses the separately verified `10.252.83.0/28` database subnet. Set `OVERLAID_DATABASE_SUBNET` only after checking for overlap on another host.

Validate the Caddy candidate from `deploy/overlaid.Caddyfile` in a disposable container. Preserve the live Caddyfile and site backups, update bind-mounted files in place, verify host/container hash parity, then reload. The existing wildcard ingress should require no DNS or tunnel changes; verify HTTPS from outside the upstream route before claiming a public service. Check `/status`, `/privacy`, `/terms`, static assets, anonymous API rejection and authenticated/keyed requests.

## Monitoring and alert acceptance

Dozor scrapes `10.0.0.56:3085/metrics` as `job: overlaid-readiness`. `scripts/readiness-exporter.py` parses each required JSON check and independently fetches the frontend shell. Missing checks or request/parse failures report zero. `deploy/overlaid-alerts.yml` alerts after two minutes for degraded roles or unavailable probes. Validate the candidate Prometheus configuration/rules before reload and preserve rollback copies.

The existing Alertmanager routing discards legacy `project=over-laid` alerts. Use `project=overlaid` for this release and verify its route to the existing homelab ntfy receiver. Apply the Overlaid-only routing/inhibition fragment in `deploy/alertmanager-overlaid.yml`; it preserves existing receivers and prevents a duplicate probe warning when the generic critical `ServiceDown` alert is active for the same instance. Do not reuse a different product's synthetic alert name. A real test must record a unique Overlaid correlation ID, firing alert, receiver/provider acceptance and resolution. Existing notification credentials stay in their current stores; never put them in Git or copied receipts. Provider acceptance is separate from the user's device visibly displaying the alert.

## Backups and independent restore

Install `deploy/overlaid-backup.service` and `.timer` after the release path is established. The timer runs nightly at 03:35 host time with up to ten minutes of jitter and persistence after missed runs. `scripts/postgres-backup.sh` creates an atomic private directory with a custom dump, environment/keyring, migration files, Compose selection and a non-secret release receipt. Do not make this directory public. Retention/off-host backup must use the homelab's existing backup owner; this script does not silently delete archives.

Test a restore into a separately named PostgreSQL 17 container with no application ingress. Follow the [tenant recovery procedure](tenancy-and-recovery.md), recreate restricted roles, reapply 0003, initialize runtime credentials and verify tenant/product counts plus a representative owner/command read. Retain hashes, version and counts in a private dated receipt; do not copy token values. A successful fresh-database migration is separate from restoring a backup.

## Rollback

For this new service, rollback stops only the `overlaid` Compose project, disables only its new backup timer and removes only its new ingress/monitor entries after restoring saved configuration in place. Keep database volumes, private environments, backup directories and immutable images. Validate/reload Caddy and Prometheus and verify existing consumers afterward. Do not run `down -v` on a real release. Migration rollback for existing data requires the pre-migration restore procedure; there is no destructive down migration.

## Evidence boundary

The October 13 deployment and real alert/restore receipts are recorded in [Phase 1 homelab acceptance](../qa/2026-10-13-phase-1-homelab.md). The timer is enabled and its manual backup restored; a natural nightly run remains a separate observation. Public routing and provider publication are verified, while device notification visibility and actual target rollback are not claimed. Keep #23's provider registrations/reviews open until their actual external gates pass.
