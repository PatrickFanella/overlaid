# Atomic retained reply batches

Tracking: [#119](https://git.subcult.tv/subculture-collective/overlaid/issues/119), the first P2.3 integration prerequisite under [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24).

`platformoutbound.Store.AdmitBatch` admits one to sixteen chat follow-ups from the same retained event and connection. It returns ordered action IDs and creation flags only after a successful commit. The existing `Admit` method delegates a one-item batch, preserving its identities and behavior. No registry callback, HTTP route, sender pool or worker is activated by this change.

Every request keeps the existing text/reply/key validation and stable tenant/connection/event/key identity. Mixed sources/connections and duplicate keys within a batch are rejected before writes. A prior action is a no-op only when its immutable payload fingerprint matches. Existing entries consume no new allowance. All-no-op batches remain available after a plan reduction or connection revocation, without granting any execution permission.

One tenant plan lock covers the batch. The existing combined chat/moderation plan and sixteen-action source bounds include every new entry before insertion. New actions and their payload-free River jobs commit together. A conflicting payload, missing/revoked source, quota denial or any enqueue failure returns no partial result and rolls back every new action/job; prior committed entries remain intact.

The source deadline remains occurrence time plus two minutes. Admission locks the active source connection, then checks the deadline against a fresh database clock after both tenant and connection locks. A transaction-start timestamp cannot qualify a source that expired while waiting. Execution still rechecks its original ledger/job deadlines and authorizes the selected sender around the durable claim; batch admission is not a provider permit.

No migration, role grant or production operation is required. Rollback can retain the ledger/jobs because batch writes have the same shape as single admission. Do not delete admitted actions or reset their claims on rollback. The [outbound ledger](platform-outbound.md), [worker](outbound-worker.md) and [registry cutover plan](../superpowers/plans/2026-10-16-outbound-registry-cutover.md) retain the remaining boundaries.

Run `GOTOOLCHAIN=go1.26.0 bash scripts/ci-postgres.sh`. PostgreSQL fixtures cover concurrent duplicate batches, mixed existing/new entries, cross-tenant rejection, invalid shapes/cancellation, exact source and plan capacity, failure on the second job with no partial actions, and no-op replay after revocation. A synthetic source-aging fixture holds the tenant lock, places the original source deadline between transaction start and lock release, and requires rejection without a send job. Existing shared chat/moderation admission tests exercise the unchanged combined limit through the one-item wrapper. Fixtures send no provider messages.
