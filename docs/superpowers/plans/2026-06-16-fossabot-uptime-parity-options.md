# FossaBot Uptime Parity Options

**Question:** What does “cloud-hosted uptime parity” mean for over-laid?

## Option A: Keep self-hosted, add stronger readiness and alerting

- Keep current self-hosted Docker/Postgres model.
- Add documented health checks, external uptime probes, and ntfy/monitoring alerts.
- Best when OBS/local streaming integration matters more than SaaS-style availability.

## Option B: Split control plane from local stream worker

- Host dashboard/config/audit storage on a reliable server.
- Keep OBS/websocket/media actions on the local stream machine.
- Requires a secure worker connection model and careful secret boundaries.

## Option C: Full hosted bot service

- Host bot command/timer/moderation/reward workflows in the cloud.
- Local overlay/OBS features become optional clients.
- Highest uptime parity with FossaBot, but highest architecture and security cost.

## Recommendation

Start with Option A unless a real production requirement demands Option B or C. It improves uptime confidence without weakening over-laid's local OBS/operator safety model.


## October 6 Option A readiness probe

Implemented a standard-library `over-laid-probe` tool in `backend/cmd/probe`, built by `make build` and usable through `make probe`. Full mode fails for HTTP-200 degraded reports; selected mode requires every named check to exist and be healthy, permitting local bot monitoring before Twitch/OBS setup. The probe bounds request time and response size, rejects redirects/malformed reports, omits raw hints/bodies, handles interrupts, and provides version/help and bash/zsh/fish completions. [The monitoring runbook](../../runbooks/uptime-monitoring.md) documents external probe placement and notification/recovery policy while keeping Compose liveness independent.

Verification passed: `make verify`, all 17 browser smoke workflows, probe race tests, and `make smoke API_PORT=8081`. Native Linux checks verified help/version/completions, full-report degradation, selected-check success, missing-check failure, SIGINT/SIGTERM exit 130, and version startup median 4.0 ms/max 4.4 ms. macOS arm64 and Windows amd64 cross-builds passed; native runtime checks on those platforms remain unverified.

Still required at deployment: choose the off-host probe machine and production endpoint, install its scheduler/monitor, configure the existing ntfy/notification destination, rehearse an actual host outage and recovery, and verify live Twitch/OBS checks. No scheduler or external alert destination has been configured during this local work.
