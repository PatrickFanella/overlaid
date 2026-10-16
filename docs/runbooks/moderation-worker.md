# Restricted moderation worker

Tracking: [#105](https://git.subcult.tv/subculture-collective/overlaid/issues/105), part of P2.2 under [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24), following merged executor PR #104. `platformmoderation.NewClient` connects the [fenced executor](moderation-executor.md) to River through an explicitly supplied restricted leasing pool. Construction opens no pool and does not start the client. The application constructs no moderation client or worker at startup.

## Explicit construction

Supply a fully constructed executor and a separate `*sql.DB` using only the `overlaid_moderation_worker` role established by migration 0020. Existing product, queue, pacing and vault credentials retain their separate owners and lifetimes. No new migration, environment variable or production login is introduced by this worker. Operators remain responsible for provisioning restricted logins before an eventual reviewed activation.

`NewClient` calls the shared queue validator before registering its worker. It requires the exact forced-RLS moderation policy, required River grants and matching product database/schema/server identity. It rejects owner/privileged roles, private-table access, combined reachable worker/runtime memberships including NOINHERIT, broad/PUBLIC job policies and mismatched schemas. Setting tenant context must not expose chat-dispatch or chat-send jobs. The moderation service role cannot read the ledger, events, connections, credentials or pacing tables; the executor scopes its separate product pool to each job tenant.

The client polls only `moderation_outbound` with four workers and registers only `platform.ModerationArgs`. Jobs retain their existing three-attempt limit and neutral priority four. Client execution has a forty-five-second timeout covering the executor's thirty seconds plus detached ledger/pacing completion. Original ledger deadlines and conservative pacing permits impose tighter bounds inside that interval. No mixed-priority or provider-capacity guarantee follows from worker count or queue separation.

## Preparation retries and terminal outcomes

Before a known claim, unavailable preparation returns the fixed `moderation_prepare_unavailable` error. River retries at one second or the original job deadline, whichever is earlier, up to the job's existing attempt bound. Exhaustion discards the River job and leaves an unclaimed ledger row pending for its existing expiry/lifecycle handling; it does not automatically re-enqueue it.

Busy pacing returns a snooze without consuming an attempt or claiming the action. Snoozes use the executor's retry time, enforce a minimum one-hundred-millisecond delay to avoid spinning, and cap scheduling at the original job deadline. A restarted worker rechecks the actual ledger, authorization and pacing state rather than trusting an earlier preparation result. Expired or malformed jobs cancel without provider authorization.

`Execution.Done` always takes precedence over an accompanying error. Delivered results complete. Rejected/uncertain results, abandoned running claims and completion errors cancel with fixed metadata; they never enter the preparation retry path. A quota or retryable provider rejection remains terminal for that claimed action. A lost claim or completion acknowledgment cannot permit another effect: the retained one-use fence is authoritative across clients and process restart.

River errors contain only fixed codes: `moderation_invalid_job`, `moderation_deadline_elapsed`, `moderation_terminal`, `moderation_terminal_error`, `moderation_prepare_unavailable` or `moderation_invalid_result`. The ledger retains its typed moderation receipt separately. Provider bodies, reasons, target IDs, credentials and panic values never become River failure details.

## Shutdown, rollback and verification

Callers must stop an explicitly started client before closing its supplied pools. Preserve pending jobs, original deadlines, action claims, receipts, pacing leases and cooldowns across restart or rollback. Never clear claims or return uncertain actions to pending to repair delivery. Stop consumers before reverting an activated execution path; this unit itself changes no startup path.

Run `bash scripts/ci-postgres.sh` for make verify and both uncached PostgreSQL race suites. Actual River fixtures cover kind/private-table isolation even with tenant context, single-attempt delivery and client restart, two tenants with separate private actions, preparation retry/redaction/exhaustion, deadline mismatch and expiry, pacing snooze without claim/attempt consumption, terminal quota/retryable/uncertain outcomes, denied/abandoned claims, adapter panic, lost completion, unsafe construction and deadline-capped retry scheduling. The provider effects are synthetic.

Deterministic mixed-priority arbitration and opt-in registry integration remain next. Existing manual/rule/Newsroom paths retain their established execution behavior. Live Twitch capacity, bot identity, restart/revocation and OBS qualification remain separate under #39/#40.
