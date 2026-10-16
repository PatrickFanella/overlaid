# Retained moderation ledger

Tracking: [#93](https://git.subcult.tv/subculture-collective/overlaid/issues/93), part of P2.2 under [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24). `platformmoderation` persists the [typed moderation contract](moderation-contract.md) for delete, timeout, ban and unban. It uses the existing immutable tenant scope and account/Twitch connection boundaries. It atomically schedules [payload-free moderation jobs](moderation-queue.md), with no command parser, API route, provider transport or worker registration. Existing dashboard and rule-enforcement moderation continues through its current workflow.

## Migration and compatibility

Apply administrator migrations `0019_moderation_actions.sql` and `0020_moderation_queue.sql` after the existing migrations and before starting the new managed binary. It creates `platform_moderation_actions`, forced RLS, the tenant policy, runtime DML grants and lifecycle/lookup indexes. Runtime, worker, pacing and vault pools remain separate. Chat/outbound workers, both service-vault roles and the pacing role receive no access to this ledger. The managed product pool now requires forced RLS on the new table and fails closed before migration or if forced RLS is disabled.

The migration is transactional and additive. The source and optional delete-target references include tenant, event and connection, with cascading deletion. The new message lookup index on retained chat supports bounded target lookup. Index creation uses the administrator migration transaction; plan a migration window for existing retained event volume. No production migration is performed by adding this source file.

Before admitting moderation actions, all chat/moderation admission writers must use the new shared-count policy. An older binary counts only chat actions, so mixed old/new writers do not preserve the combined allowance. For rollback before any moderation use, stop the new caller and retain the additive schema. After admission starts, stop all admission/execution callers and retain claims, receipts, keyring and source data under the existing private backup/restore procedure. Do not drop the table, reset a claim or return an uncertain action to pending to repair delivery. A coordinated rollback must also preserve viewer-deletion handling for moderation targets; an older lifecycle implementation does not know this table's target identities.

## Admission

Construct a `platform.ModerationIntent`, then call `Store.Admit`. The store locks the tenant row, reads the current plan and checks existing identity/fingerprint before creating anything. Identical repeated input returns the original ID with `created=false`, including after completion. Changed input under the same source/key returns `ErrConflict`; it never replaces the original payload, deadline or state.

For a new action, the store loads the retained source envelope through its scoped database, verifies its tenant/event/connection/channel and reconstructs the intent. Caller-supplied occurrence times cannot extend a retained source deadline. A current, unrevoked Twitch connection must match the channel and is locked through commit. Retained events remain read-only to the runtime: source/target foreign-key checks protect deletion races without granting event-table update privileges.

A delete action must resolve its message ID to a retained chat event in the same tenant, connection and channel. An unknown or foreign target is rejected. The ledger retains that target event reference and its actor ID for privacy deletion. User operations retain their explicit target user ID as the deletion identity. These inputs remain private and are not exported as receipts.

Both chat and moderation admission hold the same tenant lock and count both action tables. They share the plan's `storedEvents` follow-up allowance, capped at 10,000 even for an unlimited plan, and a combined maximum of sixteen actions per source event. The plan/event bound is not doubled by switching action kinds. The insertion checks the database clock again before creating a pending action. A deadline is fixed at two minutes after the canonical source occurrence; PostgreSQL retains microsecond precision. Expiry never renews from receipt, insertion or retry time.

New actions and exactly one payload-free moderation job commit in the same transaction. Duplicate admission does not re-enqueue a missing job or renew a deadline. Migration catch-up covers only pending, unexpired rows; see the [queue migration and leasing boundary](moderation-queue.md). No job count, retry hint, retained row or successful admission grants permission to execute an effect.

## Claims, completion and recovery

`Start` locks the tenant and action, verifies the current connection and validates the typed payload. Expired or unavailable-connection actions become terminal rejections without a claim. It checks the database clock at the final running-state update too, so waiting for a connection lock cannot expose a claim after its deadline. It commits a SHA-256 hash of a random one-use claim token before returning private action input. Only the successful caller receives the token. Concurrent starts and fresh store instances cannot obtain a second claim after the action starts or becomes terminal.

The [explicit executor](moderation-executor.md) checks fresh provider support, identity, scopes, role, consent, entitlement, binding and deadlines before and after the pre-effect fence. The store's connection check is not that authorization. Moderation uses the separate [sender/channel pacer](moderation-pacing.md); the existing chat pacing budget does not establish moderation endpoint capacity.

`Finish` requires the exact tenant, action and claim token plus a valid `ModerationReceipt`. It uses a detached context bounded to five seconds so an attempt's cancellation need not discard a known result. Confirmed success needs no provider message ID. A forged, foreign, repeated or recovered claim cannot complete the action. Malformed receipts are rejected and leave the durable running fence intact; the future executor must normalize adapter results with `ResolveModerationAttempt` before completion. A failed bookkeeping write is never permission to resend. Retrying a completion write with the same claim does not perform another provider attempt.

`RecoverExpired` terminalizes at most 100 expired rows per call, ordered by deadline/ID and using locked-row skipping. Pending actions become rejected/expired; started actions become uncertain/deadline-elapsed. Recovery clears claim hashes and cannot return any action to pending. Retry hints remain metadata for future independent actions; they do not schedule or replay the claimed action.

## Receipts and lifecycle

`Receipts` returns at most 100 rows of fixed metadata: action/source/connection IDs, capability, state/code, failure/retry hint and lifecycle timestamps. Tenant export uses the same field set. Keys, target IDs/messages, reasons, durations, payload fingerprints and claim hashes/tokens are excluded. These are private account metadata, not a public overlay projection.

Deleting a source or retained delete-target event cascades to its actions and jobs, including pending and running rows. Retention therefore removes derived private input too. Viewer deletion removes actions targeting that viewer even when another viewer authored the source; source-author deletion follows the existing retained-event cascade. Tenant deletion cascades through retained sources. A deleted claim cannot later complete. No privileged exception or additional service-role grant is used for lifecycle cleanup.

## Verification and next work

Run `bash scripts/ci-postgres.sh` for the full disposable PostgreSQL gate, `make verify` and both uncached race suites. The moderation integration tests cover concurrent duplicate admission and starts, conflicts, retained deadline reconstruction, shared allowances, hashed claims, canceled-context completion, failed writes, receipt/recovery fencing, connection revocation, lock-delayed expiry, target/source/tenant deletion, service-role denial, metadata exports, migration rollback and startup/RLS refusal.

The [fenced executor](moderation-executor.md) composes current selected-identity authorization, typed transport and endpoint pacing. The dedicated worker, deterministic mixed-priority arbitration and opt-in registry integration remain unfinished. They must preserve this ledger's original deadlines and one-use claims. The existing outbound [failure-diagnosis issue #90](https://git.subcult.tv/subculture-collective/overlaid/issues/90) remains separate from this implementation and from production activation.
