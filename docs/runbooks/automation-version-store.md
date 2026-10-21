# Immutable automation version storage

[Issue #278](https://git.subcult.tv/subculture-collective/overlaid/issues/278) implements Phase 3 E2a: durable version publication and current-pointer selection. `tenancy.NewAutomationStore(ctx, scope)` explicitly borrows an immutable managed `tenantdb.DB` scope. No application, HTTP, editor, file-mode or dispatcher caller constructs this store. [Explicit operator run admission](operator-automation-admission.md) now supplies the first E2b writer with atomic cooldown reservation. Trusted stream/per-stream reservation and provider/source integration remain separate work.

## Migration and role boundary

Migration `0039_automation_versions.sql` adds three forced-RLS tables:

| Table | Ownership and content | Runtime grants |
| --- | --- | --- |
| `engine_automations` | Tenant/automation key, current version pointer and database update time | SELECT/INSERT; UPDATE only on pointer and update time |
| `engine_automation_versions` | Composite tenant/automation/version key, canonical private document, digest, author reference and database creation time | SELECT/INSERT only |
| `engine_automation_presets` | Composite source and exact target version references | SELECT/INSERT only |

The current pointer uses a deferred composite foreign key, allowing the initial automation and body to commit together. Source versions cascade from their automation, and preset references cascade from their source version. Exact target references are deferred, non-cascading foreign keys: deleting a target alone cannot destroy a dependent pinned graph. Tenant deletion cascades all three tables in one transaction. Author account deletion clears the version's account reference; configuration bodies and digests remain unchanged. Removed members retain no store access, while their authored configuration remains workspace-owned.

Database checks bind document headers to tenant/automation/version IDs, cap canonical text at 64 KiB per body, and verify the domain-separated SHA-256 digest using the existing public `pgcrypto` extension. Runtime roles cannot update/delete versions or references, truncate tables, change their identity columns, or obtain reference/trigger privileges through this migration. No queue, pacing, vault or administrative role receives access. Constructor and operation checks reject unsafe current/original/reachable role privileges, including column-only and non-inherited role grants, table ownership and schema creation. They also require exact forced-RLS tenant policies and reviewed validated constraints. These checks are snapshots; operators must not alter grants or schema during active transactions.

New managed binaries require all three forced-RLS tables when opening a tenant pool, even though no engine caller is activated. Apply 0039 with administrator credentials before starting that binary. The migration is additive and reapplicable on populated fixtures without rewriting bodies, pointers or timestamps. Retain a database backup and stop incompatible writers before migration as required by the existing [managed tenancy procedure](tenancy-and-recovery.md). No production migration is performed by this development work.

## Publication, reads and pointer selection

`Publish(ctx, actor, validated, expectedCurrent)` requires a fresh configure-capable membership. The body must already pass the pure validator, and its tenant header must equal the trusted scope. A transaction locks the tenant before reading membership, current pointer, exact dependencies and storage counts. This uses the same tenant-first order as membership changes and deletion.

An empty expected pointer means create-only. Updating an existing automation requires its exact current version. A new version body, direct preset references, pointer update and fixed metadata audit entry commit together. Audit failure or commit failure returns no publication result. Database row/constraint detail is reduced to fixed store errors so private document text cannot escape through an error. Caller cancellation preserves context error identity; an uncertain commit must be resolved by reading the same immutable version identity, without inventing another version automatically.

Repeating the same current version and body returns `Unchanged` with its original creation time and no new audit record. Reusing a version ID with different content conflicts. Publishing an old retained version through this API also conflicts if it is no longer current; use explicit pointer selection for rollback. A duplicate result grants no effect permission.

`SelectCurrent(ctx, actor, automation, version, expectedCurrent)` requires configure permission and the same tenant fence. It selects a retained exact version and atomically records a fixed metadata audit. A repeated already-current selection is a no-op. Selection changes no body, preset reference or existing pinned graph. It does not select a dispatcher or replay an effect.

`Load(ctx, actor, automation, version)` requires fresh read permission, including moderator read access. Empty version means read the current root pointer. Dependencies always load by exact saved version, never by their current pointer. Each loaded body is reparsed and checked against its stored canonical bytes, digest and composite identity, then the [complete pinned graph validator](engine-pinned-graphs.md) checks cycles and expanded budgets. Reads hold the tenant share fence through graph construction. Returned graphs contain private configuration and require the caller's lifecycle handling; they are not public export objects.

## Bounds, privacy and lifecycle

Publication enforces hard storage safety limits under the tenant lock: at most 100 automations, 32 immutable versions per automation and 4 MiB canonical document bytes per tenant. Existing rows and new bytes count together. There is no automatic pruning or limit increase. Selecting an already retained version remains possible at the version cap. These bounds do not establish plan entitlement, execution capacity or permission to run an automation. Future admission must independently check current plan, membership, capability and provider authorization.

Every graph retains the existing 16-version/256 KiB aggregate, depth-16 and expanded-step-100 bounds. Configuration is retained until tenant lifecycle deletion; ordinary event-retention pruning does not remove immutable configuration versions. This unit adds no event/viewer capture, credential fields, native provider payloads, replay context or runs. Authored arguments remain private text and must not be used for secret custody. Future runtime input and trace storage need their own event/viewer/member deletion cascades.

Owner exports include automation/preset identifiers and version digest/creation metadata. Version documents and author-account references are excluded from that projection. Audit diffs contain only automation ID, previous/current version IDs and document/graph digests, under the existing audit retention policy. Private documents remain in managed database backups; neither logs nor generic exports should include loaded graph bodies. This metadata projection is not a complete configuration importer or restore format.

Fixed operation errors are `automation_version_invalid`, `automation_version_conflict`, `automation_version_schema`, `automation_version_store`, existing permission/limit errors, missing-row identity, or caller context errors. Failures return no partial publication/graph. The store does not close its borrowed pool.

## Rollback and verification

Rollback to a prior compatible binary leaves the additive schema and saved versions intact. Older binaries ignore these unused tables; they cannot operate on the new versions. Do not drop the tables or rewrite version bodies as rollback. The migration ledger prevents normal reapplication of old bootstrap SQL. If an operator deliberately reruns the broad 0003 grants, the new constructor refuses the widened grants; reapply reviewed 0039 before using the store. Populated disposable fixtures test this refusal and restoration. Broader administrative changes still require inspection and a reviewed repair.

Disposable fixtures cover exact presets through a changed target current pointer, tenant isolation, missing context, member revocation, immutable grants, competing expected-pointer publications, audit rollback, no-op retries, canonical conflict checks, author deletion, owner tenant deletion, metadata exports, all three storage caps, populated reapplication, missing migration and policy/constraint/reachable-role drift. Focused SQL race tests and new-diff lint run before the full `scripts/ci-postgres.sh` gate, which includes `make verify` and the full application/River race suites. The gate is required before merge; its result is recorded on the PR. These checks do not prove power-loss durability, deployment, run uniqueness, effect delivery or Phase #25 completion.

The current explicit store additionally requires [migration 0040 and the schema-2 admission policy codec](engine-admission-policy.md). Schema-1 bodies remain canonical and loadable; current schema/header guards reject pre-upgrade or broadened contracts. Older explicit-store builds fail closed against the upgraded header. Preserve both codecs and data on rollback; the explicit operator run writer remains unregistered in the application.

New managed binaries also require the additive forced-RLS tables from [migration 0041](operator-automation-admission.md) at pool opening. Version operations retain their existing schema checks; explicit run operations have additional repeated contract checks.
