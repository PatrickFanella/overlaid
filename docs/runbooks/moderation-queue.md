# Moderation scheduling and lease isolation

Tracking: [#95](https://git.subcult.tv/subculture-collective/overlaid/issues/95), part of P2.2 under [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24). This extends the [retained moderation ledger](platform-moderation.md) with atomic River scheduling and a dedicated leasing role. No moderation handler, worker/client startup, provider transport or production pool is added. Existing manual/rule moderation keeps its current workflow.

## Migration and jobs

Apply administrator migration `0020_moderation_queue.sql` after `0019_moderation_actions.sql` and before starting the new managed binary. It adds generated tenant/action references, a cascading composite foreign key to the moderation ledger, one-job-per-action uniqueness, exact job constraints, the runtime tenant policy and a separate restricted moderation role/policy. The product pool refuses startup without the validated moderation job foreign key. No new application environment variable or production login is created.

`Store.Admit` inserts the action and calls `tenantdb.Tx.EnqueueModeration` before committing. If enqueue fails, the action rolls back. The bridge reads the pending, unexpired ledger deadline under a row lock and gives River only `ModerationArgs{TenantID, ActionID, Deadline}`. Targets, message IDs, reasons, credentials, receipts and claim tokens stay outside job arguments. Callers cannot choose another tenant, queue, deadline, priority or attempt limit through this bridge.

Jobs have kind `overlaid_moderation`, queue `moderation_outbound`, at most three attempts and neutral priority four. Deadline is the original ledger deadline truncated to Unix seconds, without renewal on insertion. A future executor must compare that value with the retained ledger before a claim; job constraints validate shape and binding, not provider authorization. Direct SQL is not an authorization API.

Repeated admission returns the retained action without creating another job, even after its job was removed. The bridge refuses running, terminal, expired and foreign actions; uniqueness prevents duplicate pending jobs. An uncertain action cannot become eligible for insertion. Jobs cascade when their action, source, retained delete-target or tenant is removed. This preserves the ledger's target-aware viewer deletion and retention.

## Upgrade and rollback

The transactional migration catches up only unexpired actions still in `pending`. Started and terminal actions receive no jobs. Its conflict clause preserves an existing job unchanged. The upgrade regression applies the actual migration around pending/running/uncertain/expired ledger rows, verifies rollback of both schema and catch-up, then checks that repeating only catch-up does not change the job ID, schedule or deadline.

Stop older admission writers during the migration and binary transition: the older ledger implementation can insert actions without jobs. The migration's catch-up is not a recurring repair service. Once scheduling begins, use the new writer for all moderation admission. There is no startup executor to pause in this unit, but any explicitly constructed test caller must be stopped during rollback.

Retain the additive schema, original claims, receipts and private backups. Do not remove a running fence or reset an uncertain action to recover delivery. Older lifecycle/admission code does not understand this job graph; rollback requires coordinated writers and deletion handling, not dropping the table or role while callers run. This source change performs no production migration.

## Restricted leasing boundary

Migration 0020 creates the non-login group `overlaid_moderation_worker`. An eventual isolated service login may inherit only that leasing role. It receives River metadata DML, migration read and the two River sequence grants, with forced-RLS access to `overlaid_moderation` jobs only. It receives no ledger, event, vault, product or pacing-table privileges. The existing `overlaid_worker` and `overlaid_outbound_worker` policies remain chat-dispatch-only and chat-send-only, respectively. Setting tenant context does not activate the runtime policy for any worker role.

`platformqueue.Validate(..., "overlaid_moderation")` checks the exact moderation policy, forced RLS, current/session reachable privileges and the product database/schema/server identity. Combined memberships, including non-inheriting roles reachable through `SET ROLE`, fail closed. Broad kind/PUBLIC policies or private-table grants are rejected. Existing chat/outbound validators reject reachable moderation memberships too. Validation opens no pool, constructs no client and grants no effect permission.

The integration proof uses River v0.47.0's pinned `JobGetAvailable` SQL inside rolled-back fixture transactions. It demonstrates kind isolation across all three queues and tenant-context forgery, without registering a handler or using a provider. A worker implementation must still validate and fence every action before invoking a trusted provider adapter.

## Verification and remaining work

Run `bash scripts/ci-postgres.sh` for the full disposable PostgreSQL gate, `make verify` and both uncached race suites. Moderation tests cover enqueue rollback, concurrent duplicate admission, exact payload-free arguments, constraints, fixed deadlines, terminal refusal, pending-only migration catch-up and rollback, source/target lifecycle cascades, actual River lease isolation, private-table denial and unsafe reachable-role/policy rejection. The existing ledger, chat and outbound regression suites remain part of that gate.

The moderation handler/executor, fresh provider authorization/transport, endpoint pacing, deterministic mixed-priority arbitration and opt-in registry integration remain unfinished. Priority four is not a moderation priority guarantee. No queued action should be consumed by an improvised callback or replayed after its durable claim. Live provider qualification remains separate.
