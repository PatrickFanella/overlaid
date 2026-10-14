# River transaction compatibility spike

This module implements the prerequisite in ADR 0007 under [issue #52](https://git.subcult.tv/subculture-collective/overlaid/issues/52). It is test code, separate from the application module. River and `riverdatabasesql` are pinned to **v0.47.0**, whose dependency graph retains the application's **pgx v5.10.0**. Newer versions are not required for this compatibility gate.

Run `bash scripts/ci-postgres.sh` from the repository root for a disposable database, `make verify`, and uncached race checks in both modules. To use an existing disposable database:

```bash
cd backend/spikes/river
OVERLAID_TEST_DATABASE_URL=postgres://... go test -race -count=1 -v ./...
```

Without that variable the database tests skip. A compilation-only pass is not transaction evidence.

Local result on October 14, 2026: `bash scripts/ci-postgres.sh` passed on PostgreSQL 17, including `make verify`, 133 frontend tests, the full application race suite and the spike's uncached race suite. Transactional insertion with the pinned database/sql driver passed the cases below. Hosted checks remain a separate result.

The fixture creates an application schema through the shared PostgreSQL helper, runs River's pinned migrations as the administrator, and uses the same pgx `database/sql` runtime role and transaction-local tenant setting as `tenantdb`. The role is validated through `tenantdb.Open`: it owns no tables and cannot bypass RLS. A fixture-only domain event table and forced policy on `river_job` restrict both event rows and job arguments to the current tenant.

The tests exercise:

- Event plus River `InsertTx` commit, pre-commit invisibility from another connection, explicit rollback, and rollback after a rejected enqueue.
- Twenty-four concurrent deliveries with the same event key producing one retained event and one job, followed by fresh-pool/client readback and redelivery.
- Two tenants using the same native delivery identity, predicate-free reads under RLS, missing tenant context, missing tenant arguments, and forged cross-tenant job arguments.
- Job arguments limited to tenant, connection and event IDs. Chat text stays in the fixture's event table. No worker, provider effect, listener or external notification consumer starts.

River documents [transactional enqueueing](https://riverqueue.com/docs/transactional-enqueueing); this spike tests the pinned implementation against Over-Laid's driver and role constraints. The fixture owns and drops its schema and roles. PR #53 kept application migrations and dependencies unchanged. The subsequent production ingestion unit adopts the same pins and migrations while retaining this isolated regression spike.

The compatibility result applies to transactional insertion. This spike uses a raw `sql.Tx` with the same transaction-local setting. The production ingestion unit under #54 adds the encapsulated `tenantdb.Tx` bridge, domain event storage and lifecycle cleanup; its tests exercise those production boundaries. A shared worker still needs an explicit service role and must re-enter the immutable tenant scope before product reads. Tenant-scoped insertion alone does not qualify River's cross-tenant worker and maintenance queries. The spike replaces the production job policy only inside its disposable fixture to exercise its separate prototype job kind.

There is no custom queue implementation here. Worker leases, retry policy, fairness, transport recovery and real provider delivery remain later implementation/acceptance work. Removing this directory and its verification hooks reverts the spike without any production database rollback.
