# Compose dependency and exposure verification — October 6, 2026

Tested source: `92f2130` plus the Compose environment correction committed with this record. Docker Compose 5.5.1 used an owned rootless Podman API socket. A separate project, `overlaid-9734-qa`, used fresh named volumes and loopback ports 8084/5184/55441. An isolated environment file disabled EventSub and selected public exposure mode; inherited Compose/app variables were removed from the runner's environment. The project reused the locally verified images and overrode only image/build selection and host port bindings.

## Reproduced defect and correction

Before the correction, `EXPOSURE_MODE=public docker compose --env-file /dev/null config --format json` had no `EXPOSURE_MODE` in the API environment. The container therefore used the backend's local default despite the documented deployment setting. Compose now forwards `${EXPOSURE_MODE:-local}`. Config inspection confirmed `local`, `lan`, and `public` pass through unchanged.

## Observed gates

- `compose up -d --wait --wait-timeout 90 --no-build --pull never` passed; Postgres became healthy before the API started, and all three services became healthy.
- Default API-to-Postgres service DNS/credentials worked inside the Compose network. Product readiness checks for Postgres, retained state, event history, dashboard config, sessions, interaction storage, and bot runtime were healthy.
- Public mode with intentionally empty fixture secrets produced degraded `operator_secret` and `webhook_secret` checks naming the nonlocal exposure setting. The valid mode's separate `exposure` check remained healthy.
- A rehearsal session recorded one BRB event and both start/end readiness snapshots. Session ID: `af971cd660e969725896affe670b8337`.
- `compose restart api` preserved the complete BRB SSE replay and the entire ended-session report.
- `make smoke API_PORT=8084 WEB_PORT=5184`, `make verify`, and all 21 `make smoke-browser` workflows passed.
- The owned project was removed with its volumes/network, and its temporary Podman API service/socket were stopped/removed. Native development services remained available.

This closes local Compose service/health dependency, named-volume persistence, and exposure-setting forwarding gates. It does not verify the Docker engine, Docker build integration, production public exposure, reverse-proxy authentication, live OBS host-gateway access, or Twitch credentials. Public-mode fixture ports stayed bound to loopback. Readiness also retained expected Twitch/OBS degradation without live services.

The private runner, isolated configuration, and before/after JSON comparisons remain outside git under `/tmp/overlaid-9734-compose-drill`.
