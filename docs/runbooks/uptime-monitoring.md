# Uptime monitoring

Use the self-hosted Option A from the [uptime plan](../superpowers/plans/2026-06-16-fossabot-uptime-parity-options.md). The API remains next to local OBS; an external probe adds visibility when that host or its network fails.

## Liveness and readiness

Compose uses `/healthz` for cheap liveness. `/readyz` returns dependency diagnostics as JSON and can return HTTP 200 while degraded. A monitor that checks only HTTP status misses those failures.

Build the read-only probe with `make build-probe` (also included in `make build`):

```bash
bin/over-laid-probe --url http://127.0.0.1:8080/readyz
bin/over-laid-probe --help
bin/over-laid-probe --version
```

The default requires overall `status: ok` and every reported check to be `ok`. The probe follows no redirects, bounds response size to 64 KiB, and gives the complete request/response five seconds by default. It rejects invalid JSON, empty/duplicate checks, unknown statuses, and a missing required check. Diagnostics omit raw response bodies and readiness hints.

| Exit | Meaning |
| --- | --- |
| 0 | Required checks ready |
| 1 | Readiness degraded, missing check, transport/HTTP failure, invalid report, or timeout |
| 2 | Invalid flags, endpoint, or selected check names |
| 130 | Interrupted by SIGINT/SIGTERM |

Probe diagnostics go to stderr; help uses stderr, while version and completion scripts use stdout. This works from an interactive terminal or a non-interactive scheduler. Override the deadline with `--timeout 10s`.

## Select deployment requirements

For local bot development without Twitch credentials or reachable OBS, require storage and event/config services explicitly:

```bash
bin/over-laid-probe --url http://127.0.0.1:8081/readyz \
  --checks process,exposure,event_pipeline,postgres,bot_runtime,retained_state,event_history,dashboard_config,stream_sessions,twitch_interactions
```

The equivalent repository command is `make probe API_PORT=8081 PROBE_CHECKS=process,postgres,bot_runtime`. Selection requires every named check to exist and be `ok`, while unrelated degradation remains visible in `/readyz`. Use full-report mode for a live stream that requires Twitch and OBS. If Postgres is intentionally absent, select file-backed checks explicitly; requiring `postgres` should fail when the database check is missing or degraded.

## External probes and alerts

Run a second copy of the probe from a separate host against the configured HTTPS endpoint. Set `EXPOSURE_MODE` and the existing operator/webhook protections according to the [production recovery runbook](production-recovery.md); this probe does not need an operator token. Do not change Compose liveness into full readiness, since Twitch/OBS maintenance should not cause a container restart loop.

Configure the external monitor to run every minute, alert after two consecutive failures, and send one recovery notification when checks return to ready. Keep a separate liveness check so alerts distinguish an unreachable API from dependency degradation. Route those alerts through the installation's existing ntfy or monitoring notification channel; this repository does not choose a notification destination, install a scheduler, or send messages automatically. Validate alert delivery and the off-host outage path when that deployment access is available.

For a rehearsal without remote services, the probe's tests use loopback HTTP fixtures for HTTP-200 degradation, selected checks, missing checks, malformed reports, redirects, and stalled response headers/bodies. `make probe` against local Postgres verifies the real report. Follow the [live QA checklist](live-qa.md) before relying on Twitch/OBS readiness in production.

## Shell completions and platforms

Add `bin` to PATH or install the built binary using the host's existing tooling. Emit completions with `over-laid-probe --completion bash`, `--completion zsh`, or `--completion fish`, and load the result through that shell's completion configuration. The binary uses the Go standard library. Linux runtime verification and macOS/Windows cross-compilation are recorded in the roadmap; native runtime checks on those other systems remain deployment work.
