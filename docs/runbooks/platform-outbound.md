# Retained outbound chat ledger

`internal/platformoutbound` stores immutable follow-up actions for retained Twitch chat events. This is a storage foundation: the application does not yet admit these actions or run an outbound worker. Current broadcaster replies continue through the existing send path. No deployment flag or provider request is introduced.

Apply `migrations/0013_platform_outbound.sql` with the existing administrator migration workflow before starting the new managed binary. The runtime checks the new table's forced RLS at startup. The table grants access only to the existing tenant runtime role; River workers and service-bot administrators receive no additional access. File mode is unsupported.

`Admit` derives a SHA-256 action ID from the tenant, connection, retained source event and producer-owned action key. A repeated identity with matching text and reply parent returns the original action without mutation; a different payload returns `ErrConflict`. The source event and connection must belong to the same tenant and active Twitch channel. Composite foreign keys preserve that relationship. The deadline is the source event's occurrence time plus two minutes, independent of admission or restart time.

Admission locks the tenant with plan changes. The retained action count is bounded by `storedEvents` and a hard ceiling of 10,000 per tenant, including terminal actions; each source event admits at most 16 actions. Unlimited plans still obey those ceilings. Lowering a plan does not invalidate duplicate identity lookup or erase receipts. Expired or deleted sources cannot admit new work.

| Operation | Result |
| --- | --- |
| `Start` on a valid pending action | Commit `running` with a hashed random claim fence before returning the private message and deadline. |
| `Start` on an expired or disconnected pending action | Commit a rejected receipt; return no claim. |
| `Start` on running or terminal work | Return no claim. Restart and concurrent calls cannot replay it. |
| `Finish` with the current claim | Commit delivered, rejected or uncertain status and bounded metadata; clear the fence. |
| `Finish` with a forged, foreign, repeated or recovered claim | Return `ErrFence`. |
| `RecoverExpired` | Terminalize up to 100 actions: pending becomes rejected; running becomes uncertain. Never return work to pending. |

`Finish` detaches from a canceled provider context for a bounded five-second persistence attempt. If persistence fails, leave the durable running fence and recover it as uncertain after its deadline. A late provider result cannot overwrite a recovered receipt. Quota rejections retain a retry hint in nanoseconds, but this store never retries or reschedules them.

Claims and retained message text are internal execution data. Receipt inspection accepts limits from 1 to 100 and exposes only action/event/connection IDs, fixed state/code, provider message ID, failure class, retry hint and timestamps. Owner exports use the same metadata allowlist and omit message text, reply parents, producer keys, payload hashes and claim hashes. Source retention, viewer deletion and tenant deletion cascade to these actions. Deduplication lasts while the source and action are retained; deletion is not a replay mechanism.

The future executor must bind its attempt context to the claim deadline, recheck current tenant capability/consent, resolve the permitted identity through the vault boundary, apply account/channel pacing and use the [single-attempt Twitch transport](twitch-delivery.md). A stored claim does not grant platform permission. Atomic job scheduling, queue priority, explicit retry policy, echo-marker integration and bot identity activation remain later units.

Run `bash scripts/ci-postgres.sh` for `make verify`, disposable PostgreSQL integration and the parent/River race suites. Fixtures cover concurrent duplicate admission and claims, immutable payload conflicts, restart, failed-start rollback, cancellation during completion, recovery fencing, quota receipts, plan changes, forced RLS and lifecycle deletion. They do not send provider messages.

The migration is additive and has no backfill. Before activation in a later release, application rollback can leave this unused table in place. Preserve it for inspection rather than deleting retained evidence during rollback. Once an executor is enabled, its rollback must stop execution before changing binaries or ledger state.
