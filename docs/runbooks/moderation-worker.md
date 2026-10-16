# Restricted moderation worker

Tracking: [#105](https://git.subcult.tv/subculture-collective/overlaid/issues/105), part of P2.2 under [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24), following merged executor PR #104. `platformmoderation.NewClient` connects the [fenced executor](moderation-executor.md) to River through an explicitly supplied restricted leasing pool. Construction opens no pool and does not start the client. The application constructs no moderation client or worker at startup.

## Explicit construction

Supply a fully constructed executor and a separate `*sql.DB` using only the `overlaid_moderation_worker` role established by migration 0020. Existing product, queue, pacing and vault credentials retain their separate owners and lifetimes. The worker unit adds no migration, environment variable or production login. The current product pool additionally requires [priority migration 0022](moderation-priority.md). Operators remain responsible for provisioning restricted logins before an eventual reviewed activation.

`NewClient` uses the [tenant-fair driver](tenant-fair-leasing.md), which calls the shared queue validator before registering its worker. Explicit construction also requires migration 0023 and its restricted monotonic cursor. It requires the exact forced-RLS moderation policy, required River grants and matching product database/schema/server identity. It rejects owner/privileged roles, private-table access, combined reachable worker/runtime memberships including NOINHERIT, broad/PUBLIC job policies and mismatched schemas. Setting tenant context must not expose chat-dispatch or chat-send jobs. The moderation service role cannot read the ledger, events, connections, credentials or pacing tables; the executor scopes its separate product pool to each job tenant.

The client polls only `moderation_outbound` with four workers and registers only `platform.ModerationArgs`. Jobs retain their existing three-attempt limit and use priority one after [migration 0022](moderation-priority.md). Client execution has a forty-five-second timeout covering the executor's thirty seconds plus detached ledger/pacing completion. Original ledger deadlines and conservative pacing permits impose tighter bounds inside that interval. The [mixed-load qualification](moderation-priority.md) demonstrates lane isolation; it does not establish tenant fairness or provider capacity.

## Controlled leasing

[Issue #161](https://git.subcult.tv/subculture-collective/overlaid/issues/161) adds `NewControlledClient(ctx, service, executor, active)` for explicit owners that must start consumers before publishing readiness. It requires a non-nil, concurrency-safe callback and uses the same shared leasing gate as retained chat. The original `NewClient` behavior is unchanged. Both constructors validate the restricted role, exact policies and product identity before returning; a closed gate cannot hide invalid configuration.

While `active` returns false, gated fetches return no jobs. Pending jobs keep their original deadlines and zero attempts; they cannot reach executor authorization, ledger claims or pacing. Opening the gate permits normal fair leasing and the executor's existing checks. Closing it prevents subsequent gated fetches from leasing, but cannot retract a fetch already admitted or cancel an already leased action. Owners must separately bind execution cancellation through the [lifecycle gate](moderation-executor.md#explicit-lifecycle-gate), recheck authorization, and stop the client before closing pools. The readiness callback grants no effect permission and must be cheap, safe for concurrent calls and free of panics.

The real-River race fixture observes closed-gate fetches before checking untouched pending state, opens readiness for a synthetic attempt, closes it while that claimed attempt is held, and admits another job. The claimed attempt completes; the second stays pending through a fresh closed-gate client and later delivers once after activation. Original deadlines, exact authorization/attempt counts, durable receipts and released permits are checked. Closed-gate construction also rejects the same wrong roles, schemas, reachable memberships and policy drift as the original constructor. No application startup path selects this constructor yet.

## Preparation retries and terminal outcomes

Before a known claim, unavailable preparation returns the fixed `moderation_prepare_unavailable` error. River retries at one second or the original job deadline, whichever is earlier, up to the job's existing attempt bound. Exhaustion discards the River job and leaves an unclaimed ledger row pending for its existing expiry/lifecycle handling; it does not automatically re-enqueue it.

Busy pacing returns a snooze without consuming an attempt or claiming the action. Snoozes use the executor's retry time, enforce a minimum one-hundred-millisecond delay to avoid spinning, and cap scheduling at the original job deadline. A restarted worker rechecks the actual ledger, authorization and pacing state rather than trusting an earlier preparation result. Expired or malformed jobs cancel without provider authorization.

`Execution.Done` always takes precedence over an accompanying error. Delivered results complete. Rejected/uncertain results, abandoned running claims and completion errors cancel with fixed metadata; they never enter the preparation retry path. A quota or retryable provider rejection remains terminal for that claimed action. A lost claim or completion acknowledgment cannot permit another effect: the retained one-use fence is authoritative across clients and process restart.

River errors contain only fixed codes: `moderation_invalid_job`, `moderation_deadline_elapsed`, `moderation_terminal`, `moderation_terminal_error`, `moderation_prepare_unavailable` or `moderation_invalid_result`. The ledger retains its typed moderation receipt separately. Provider bodies, reasons, target IDs, credentials and panic values never become River failure details.

## Shutdown, rollback and verification

Callers must stop an explicitly started client before closing its supplied pools. Preserve pending jobs, original deadlines, action claims, receipts, pacing leases and cooldowns across restart or rollback. Never clear claims or return uncertain actions to pending to repair delivery. Stop consumers before reverting an activated execution path; this unit itself changes no startup path.

Run `bash scripts/ci-postgres.sh` for make verify and both uncached PostgreSQL race suites. Actual River fixtures cover kind/private-table isolation even with tenant context, single-attempt delivery and client restart, two tenants with separate private actions, preparation retry/redaction/exhaustion, deadline mismatch and expiry, pacing snooze without claim/attempt consumption, terminal quota/retryable/uncertain outcomes, denied/abandoned claims, adapter panic, lost completion, unsafe construction and deadline-capped retry scheduling. The provider effects are synthetic.

Deterministic mixed-priority arbitration and opt-in registry integration remain next. Existing manual/rule/Newsroom paths retain their established execution behavior. Live Twitch capacity, bot identity, restart/revocation and OBS qualification remain separate under #39/#40.
