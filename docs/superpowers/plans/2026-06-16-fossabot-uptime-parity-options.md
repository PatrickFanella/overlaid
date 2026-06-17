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
