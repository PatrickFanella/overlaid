# Fenced chat dispatch worker

Issue [#56](https://git.subcult.tv/subculture-collective/overlaid/issues/56) adds the `platformdispatch` consumer boundary. It does not register a live Twitch effect handler or start a worker from the API. The current dispatcher remains active until the separate consumer compatibility migration.

`NewClient` accepts a dedicated River service pool, an immutable product database and a handler. The service role must not own tables or bypass RLS, must belong to the chat worker policy and must have no product-table privileges. Database, schema and server identity must match the product pool. It leases only `overlaid_chat_dispatch` jobs on `ingest_followup`, using polling, four workers and a 30-second handler timeout. The caller owns client shutdown and both pools. The service role cannot directly read events, native payloads, connection credentials or receipts.

The worker enters the job's immutable tenant scope before loading its retained event. It verifies canonical identity, connection binding, the unchanged occurrence-based deadline, an active Twitch connection and the chat-read grant. Expired, invalid or unavailable events get a bounded skipped receipt and no handler call. A future effect handler must also recheck the capability and authorization for each requested effect; a chat-read grant does not authorize sending or moderation.

## Execution and recovery contract

Before invoking the handler, the worker commits a `running` receipt with a private claim token. An advisory transaction lock coordinates concurrent admission; the retained receipt supplies the crash fence. A handler starts at most once for a retained event. Completed receipts acknowledge redelivery. Failed, skipped or uncertain receipts cancel redelivery. A repeated attempt that finds a prior `running` receipt changes it to `uncertain/prior_start` and does not call the handler.

Completion updates require the original claim token and `running` state. A late result cannot overwrite uncertainty recorded by another attempt. Cancellation records `uncertain/handler_cancelled` through a bounded detached finalization context. Handler errors and panics become `failed/handler_failed`; their text and panic values are never stored in receipts or returned to River. Database failures before admission may retry. A finalization failure after admission may cause another River attempt, but the durable start fence prevents another handler invocation.

This is an at-most-once admission contract, not proof of exactly-once external delivery. A crash after committing the start fence can leave uncertainty even if no effect happened. A failed handler may have applied part of its work. This unit provides no automatic effect retry or manual requeue control. Any future owner-reviewed recovery must account for provider receipts and ambiguous delivery before replacing that fence.

`Receipts` returns at most 100 tenant-scoped records containing event/connection IDs, state, fixed code and timestamps. It excludes claim tokens, chat text, native payloads and handler errors. Owner exports use the same field allowlist. Retention, viewer deletion and tenant deletion remove receipts through the retained event FK. No new public receipt route is installed.

## Migration and operation

Apply `0008_platform_dispatch.sql` through the administrator migration path before starting the updated managed API. Startup requires forced RLS on the receipt table. The migration creates the non-login `overlaid_worker` group role and a chat-kind policy on `river_job`, grants River metadata access, and keeps product-table access separate. It records completion atomically in the application migration ledger.

This migration does not create or change a production login password. The future live worker integration must supply an isolated non-owner login inheriting only `overlaid_worker`, plus the normal validated product pool. Do not give the River pool `overlaid_runtime` membership or product grants. Disposable tests create and remove their own login roles.

Rollback uses the previous application revision with the additive receipt table and role retained. Do not delete receipts to force retry. The current API starts no new consumer, so this unit does not require a production worker shutdown or transport switch.

## Verification

Run `bash scripts/ci-postgres.sh`. Fixtures perform actual River leasing across two tenants with synthetic handlers, verify that another job kind remains untouched, and reject direct product reads, owner/runtime service roles and mismatched product schemas. They cover concurrent starts, abandoned-start restart fencing, late completion, errors, panics, cancellation, completed redelivery, expiration, changed deadlines, revoked/missing grants, bounded redacted receipts, exports and lifecycle deletion. No provider effects are sent.
