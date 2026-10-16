# Read-only pacing inspection

`chatpacing.Store.Inspect` and `bin/overlaid-pacing-probe` explain one sender/channel's current pacing state without acquiring a permit or sending a provider request. This is the visibility slice of P2.2 under [#87](https://git.subcult.tv/subculture-collective/overlaid/issues/87). Moderation action identity/receipt contracts, priority scheduling, continuous monitoring/notification delivery and application activation remain separate work.

Build the probe with `make build`. Supply the restricted pacing DSN privately as `OVERLAID_PACING_DATABASE_URL`, then run:

```bash
bin/overlaid-pacing-probe --sender 123 --channel 456
bin/overlaid-pacing-probe --sender 123 --channel 456 --deadline 2026-10-15T12:00:00Z --require-ready
```

The timestamp is an example absolute original action deadline; use the retained action's actual deadline rather than renewing it from inspection time. This command never changes an action deadline. The DSN is environment-only, not a command-line flag or JSON field. Use a separate login with only the existing restricted `overlaid_pacer` membership, not runtime, worker, owner or vault credentials. The probe's operations are read-only; it uses the existing pacing store's validated role, which has the DML privileges needed by admission. It does not provision that login or open any pool at application startup. Do not expose this global service inspection as a tenant/public endpoint.

Output is one JSON object on stdout. A valid report exits 0 by default, including when pacing is blocked or warnings exist. `--require-ready` exits 1 when an attempt could not be admitted now under the snapshot's pacing/deadline checks. Missing/invalid configuration or arguments exit 2. Unavailable storage, malformed retained state or output failure exit 1; no valid JSON readiness report is claimed. Interruption exits 130. Diagnostics contain fixed text and never print database/provider error strings. `--help` works without a database. `--timeout` is positive and at most five seconds, including connection validation and inspection.

The report is fixed metadata:

| Fields | Meaning |
| --- | --- |
| `observed_at`, `retry_at`, `ready` | Database-clock observation, earliest time allowed by the current pacing policy and whether it permits an attempt now. An optional expired/risky deadline makes `ready` false. |
| `account_limit`, `account_window_seconds`, `used`, `remaining` | The configured conservative 20-attempt rolling 30-second budget and the live window's usage/headroom. Expired timestamps are ignored in memory, not deleted. |
| `channel_wait_ms`, `permit_wait_ms` | Current channel-spacing and in-flight permit delays, rounded up to milliseconds. An active provider attempt is not preempted. |
| `cooldown_wait_ms`, `budget_wait_ms`, `clock_wait_ms` | Provider cooldown, exhausted account-window wait and backwards-clock protection. Blocking reasons can overlap; `retry_at` uses their maximum under the admission policy. |
| `deadline`, `deadline_at_risk` | Optional original absolute deadline and whether earliest availability is at or beyond it. This comparison does not forecast other queued work or reserve capacity. |
| `blocking`, `warnings` | Bounded fixed reason codes. Low headroom warns at four or fewer remaining attempts; provider cooldown, clock anomaly and deadline risk have separate warning codes. A low-headroom warning alone does not deny an otherwise permitted attempt. |

Accounts using [fair admission](chat-permit-fairness.md) also report `fair_admission_required` and `tenant_turn_unqualified`, with ready=false. A sender/channel-only probe cannot qualify a particular action turn; retry/deadline metrics describe budget timing and do not include its unknown queue wait.

The report excludes sender IDs, channel IDs, their hashes, attempt timestamp lists, permit secrets, provider credentials and message/action data. Unknown senders/channels yield an empty-budget snapshot without creating database rows. Inspection uses a single statement snapshot inside an explicitly read-only transaction, takes no admission/advisory lock, does not book future slots, refund attempts, clear cooldowns or mutate/prune retained state. Errors fail closed rather than returning an empty ready report.

A clear snapshot is neither effect authorization nor a reservation. Another worker may consume the budget immediately after it. The executor must still recheck capability, identity/consent, plan and original deadline, then call the real pacing admission method and commit its durable action claim. The warning threshold is an operator policy signal; it does not claim to predict a Twitch silence penalty or prove live provider capacity. See [durable pacing](chat-pacing.md), [outbound execution](outbound-executor.md) and the [remaining-work roadmap](../superpowers/plans/2026-10-15-remaining-work-roadmap.md).

Run `bash scripts/ci-postgres.sh`. Pure policy tests cover exact window/deadline boundaries, warning threshold, clock protection, fixed fields and invalid data. PostgreSQL tests compare complete stored rows before/after repeated inspection, prove no row creation/pruning or permit invalidation, exercise restart/cross-channel cooldown and run the actual CLI handler with a real restricted pool. CLI tests cover JSON, help/argument/failure/exit behavior and secret-free diagnostics. No provider messages or production data are used.

The current pacing constructor requires additive migration 0024 for fair-state inspection. No application startup change is included. The new read helper shares attempt validation with admission but leaves valid-state policy unchanged. Rollback can remove the probe/helper without changing retained state. The next priority unit needs a typed moderation action/receipt contract and deterministic mixed-priority arbitration; this read-only tool does not close that acceptance criterion.
