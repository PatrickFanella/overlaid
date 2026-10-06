# Local backup/restore drill — October 6, 2026

Tested source: `13b4886` (later commits only add QA documentation). The native binary embeds build commit `37a05c0` because it was built before the source commit. Source API: owned loopback port 8081, disposable Postgres 17 database `app` at port 55439. Restore API: loopback port 8082, newly created database `overlaid_restore_9734`, and a separate private retained-state directory.

The drill stopped the source API before capturing a custom-format `pg_dump` and a tar archive of its owned api-data files. Rootless Podman supplied Postgres tooling where the Docker socket was unavailable. `pg_restore --clean --if-exists --exit-on-error` restored into the separate database, and the archive was unpacked into a new directory before starting the restore API. The original database was not overwritten.

| Restore gate | Observed evidence |
| --- | --- |
| Retained overlay data | All three BRB/goals/ticker SSE events exactly matched the source replay, including IDs, timestamps, data, and retained-state keys. |
| Event history | The entire bounded history response matched, containing eight events. |
| Session/readiness report | The entire report for session `3097a5e4122a557b63b2b94dfc364155` matched, including three events, start/end snapshots, and its audit response. |
| Shared product configuration | Dashboard config and Twitch interaction-rule responses exactly matched. |
| Bot audit storage | Both APIs reported 51 total audit rows; their 25 most recent audit records exactly matched. |
| Restored readiness | Process, Postgres, retained state, history, sessions, dashboard config, interaction storage, and bot runtime reported `ok`. |
| Smoke | `make smoke API_PORT=8082` passed against the restored API and existing local Vite shell. |
| Cleanup | Restore API stopped, owned restore database dropped, original API resumed, retained fixture cleared, and no active session remained. |

Twitch configuration/auth/EventSub and OBS reachability/auth remained degraded without their external setup. No OAuth token existed in the fixture, so this drill does not verify restoration of real credentials. It also does not verify Docker named-volume ownership, production deployment, external monitoring alerts, or production-host restore.

Private transient artifacts are under `/tmp/overlaid-9734-backup-drill`, with a private directory and restricted archive/dump permissions. They remain outside git. `expected.json` captures source responses and `verified.json` records the comparison results and cleanup. The packaging runbook now stops on the first Postgres restore error and rejects a missing api-data volume before running the extraction container.
