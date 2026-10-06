# Local API process recovery drill — October 6, 2026

Tested source: `13b4886` on `feat/implement-roadmap`. The native binary was built from that source before the commit, so its embedded build commit is `37a05c0`. The test used the owned loopback API at port 8081, Vite at 5173, disposable Postgres 17 at 55439, and retained-state files under `/tmp`. No production service or live Twitch/OBS operation was involved.

Session: `3097a5e4122a557b63b2b94dfc364155` (`Local recovery drill 2026-10-06`, rehearsal).

| Gate | Observed evidence |
| --- | --- |
| Before restart | Open session contained two tagged events and one start readiness snapshot. SSE replay contained retained `goals`, `ticker`, and active `brb` events. |
| Actual outage | The owned API process stopped. The readiness probe exited 1 with endpoint reachability failure before a replacement process started. |
| Retained replay | After restart, all three replayed events exactly matched their pre-restart JSON, including IDs, data, timestamps, and retained-state keys. |
| Persistent history/report | The entire report matched its pre-restart JSON. The open session kept the same ID. |
| Active session restoration | A newly published post-restart alert carried the original session ID. The ended report contained three events and both `start` and `end` readiness snapshots. |
| Readiness recovery | Process, event pipeline, bot runtime, retained state, history, Postgres, dashboard config, sessions, and interaction storage reported `ok`. The pipeline reported `dropped=0`. |
| Probe recovery | The probe selecting process/Postgres/retained-state/history/sessions exited 0 after restart. This selection verifies those local dependencies; it does not claim full integration readiness. |
| Cleanup | Retained fixture state was cleared through the API; the rehearsal ended and no current session remained. Historical rehearsal evidence remains in the disposable database. |

Accepted degraded checks: Twitch configuration/auth/EventSub were unavailable without credentials; OBS reachability/auth were unavailable without the local OBS WebSocket service. Full readiness therefore remained `degraded`, as it did before restart. Those integrations require live validation.

Transient evidence was captured outside git in `/tmp/overlaid-9734-recovery-before.json`, `/tmp/overlaid-9734-recovery-replay.json`, and `/tmp/overlaid-9734-recovery-after.json`. Assertions compared the complete retained replay and report, checked the post-restart event's session tag, checked snapshot phases and dependency statuses, and confirmed cleanup. The Health dashboard was reloaded against the restarted API.

This closes the credential-independent API restart/replay/history portion of the recovery drill. It does not close OBS browser-source reload, Twitch reconnect/re-consent, production backup/restore, deployed external alerts, or real outage recovery.
