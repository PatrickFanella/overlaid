# Restricted outbound worker

`platformoutbound.NewClient` connects the fenced executor to River through a separate restricted service pool. It leases only `overlaid_chat_send` jobs in `chat_outbound`. Application startup does not construct this client or open its pool. Current broadcaster replies keep their existing path; the [tenant Twitch authorization adapter](tenant-outbound-auth.md) supports broadcaster/own-bot construction. The explicit [shared adapter](shared-bot-execution.md) adds restricted service credential resolution. Moderation priority and an opt-in cutover remain later work.

Apply `0016_outbound_worker.sql` through the administrator migration workflow after the ledger, outbound queue and pacing migrations. It creates the non-login `overlaid_outbound_worker` group, grants the required River metadata/sequence access, and adds a send-kind policy to the already forced-RLS `river_job`. It neither creates a production login nor grants access to product, credential or pacing tables. Retain the existing migration and private backup/restore procedure.

A future deployment must provision an isolated non-owner login inheriting only `overlaid_outbound_worker`. Keep it separate from `overlaid_worker`, `overlaid_runtime`, `overlaid_pacer` and the bot administration role. The executor still needs its independently validated product and pacing pools and a trusted authorization adapter. The caller owns all pool lifetimes. This unit adds no application environment variable or activation switch.

Both outbound and chat clients use `platformqueue.Validate` before construction. It rejects table owners, superusers, RLS bypass and role-administration privileges reachable from the current or session user, including memberships without inheritance. Reachable roles must have no private-table access in the product schema, no dangerous River table privileges, and no write access to River migration history. The client requires River metadata DML and exactly one applicable job policy with the expected name, role scope, command and kind predicates under forced RLS. PUBLIC policies and combined chat/outbound memberships fail closed. Database, schema, server address and port must match the immutable product pool. These are construction-time checks; operators must preserve the role/policy boundaries while clients run.

The worker registers only the outbound kind, with four workers and polling transport. Its forty-five-second job timeout allows the executor's thirty-second context plus detached echo, ledger and pacing writes. There is no provider callback in the leasing pool; the executor performs scoped preparation and checks authorization twice around the committed claim.

Worker outcomes follow the retained ledger:

- A delivered action or deleted source acknowledges the job. A redelivered completed job obtains no new claim.
- A running, rejected or uncertain action cancels the job. A bookkeeping failure after a claim also cancels with a fixed error; the ledger's durable fence remains authoritative even if River's acknowledgement is lost.
- An infrastructure failure before a claim returns a fixed preparation error. River allows at most three attempts. Retry scheduling is capped by the original job deadline, which the executor checks against the ledger. Retry exhaustion can leave a pending action until bounded expiry recovery; it grants no send permission.
- Pacing denial snoozes only while the action is pending. Snoozes preserve the attempt count and the original deadline. A minimum delay avoids a tight loop on stale timing; every delay is capped by that deadline. On wake-up, the executor rechecks state, authorization, deadline and pacing rather than reserving a future slot.
- Invalid arguments or an elapsed deadline cancel the job. No typed provider retry hint, quota rejection or uncertainty automatically reschedules an attempted action.

Only fixed diagnostics cross this boundary. Provider errors, credentials, message text and panic values do not enter the worker's error strings. The existing receipt and export allowlists remain unchanged. Source/viewer/tenant deletion continues to cascade through the action and job foreign keys.

Run `bash scripts/ci-postgres.sh` for `make verify` and both uncached race suites against disposable PostgreSQL. Worker fixtures exercise real River leasing, preparation retry/exhaustion, pacing snooze/restart, expired jobs, completed redelivery, abandoned claims and terminal/bookkeeping failures. They also exercise forged tenant context, direct private-table denial, combined memberships with and without inheritance, broad PUBLIC/kind policies, missing forced RLS, dangerous grants and mismatched schemas. Synthetic send callbacks produce fixture receipts; no provider messages are sent. Existing chat leasing and lifecycle tests run alongside them.

For rollback before activation, stop any explicitly constructed fixture/client and retain the additive role, policy and all ledger/pacing state. Do not delete claims or requeue actions to repair uncertain delivery. No production sending path changes in this unit. Local fixture results do not qualify live OAuth grants, provider roles, production throughput or cutover.

## Failure diagnosis in fixtures

[Issue #90](https://git.subcult.tv/subculture-collective/overlaid/issues/90) records a hosted restart/snooze failure on merged main `de6a449`: River's job fetch reached its ten-second timeout, then the test did not observe completion. A later green run does not establish that failure's cause. The merged pacing revision `f71131c` passed [hosted verification](https://git.subcult.tv/subculture-collective/overlaid/actions/runs/15556/jobs/28701).

The fixture's completion waiter retains its fifteen-second observation budget and now applies that context to each database query. A blocked query can no longer wait beyond that budget indefinitely. On completion or snooze-observation failure, the test logs an independent diagnostic snapshot bounded to one second. It reads activity first so a locked job table cannot hide the activity result.

The snapshot contains only pool connection/wait counts; counts of active sessions, lock waits, I/O waits and blocked sessions for the worker's own database role; and job state, attempts, error count and timing relative to the database clock. Separate success flags distinguish unavailable activity/job observations from measured zeros. No job arguments, error bodies, query text, role names, credentials, claims or provider payloads are logged. Diagnostics use the existing restricted worker pool and add no grants. The regression fixture holds a disposable job-table lock, verifies that the state query honors cancellation, and confirms that a blocked job read still reports lock counts. A three-second fixture-only unlock watchdog prevents a broken waiter from hanging that regression.

October 15 local investigation on `f71131c`: eight consecutive uncached race runs of `TestOutboundRiverPacingSnoozeAndRestart` passed. Three combined uncached race runs of `platformoutbound`, `twitchoutbound` and `tenancy` also passed with the disposable PostgreSQL container limited to one CPU and 512 MiB. These are finite synthetic observations. No River fetch/worker timeout, action deadline, pacing rule, claim or startup behavior changed. The original hosted fetch-timeout cause remains unresolved; capture the failure snapshot if it recurs before choosing a runtime fix.

With `OVERLAID_TEST_DATABASE_URL` already pointing to a disposable database, focused checks from `backend/` are:

```bash
go test -race -count=8 -run '^TestOutboundRiverPacingSnoozeAndRestart$' ./internal/platformoutbound
go test -race -count=1 -run '^TestOutboundDiagnostics' ./internal/platformoutbound
```

Run `bash scripts/ci-postgres.sh` from the repository root for the full disposable verification gate. Test diagnostics do not establish production health or authorize replay of a claimed action.
