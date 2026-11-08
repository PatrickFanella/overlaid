# Captured no-repeat memory (#347)

1. Add pure bounded memory validation/capture and weighted exclusion to flow rehearsal. Check deterministic replay, loops, single-child behavior, input isolation and legacy refusal with engine race tests.
2. Add migration 0048, exact schema/grant validation and disposable fixture support. Persist original memory and final picks atomically under the existing tenant/budget locks. Check duplicates, later-admission replay, legacy captures, plans and traces with PostgreSQL integration races.
3. Accept capture 3 and the fixed trace reason in Go/browser validators. Keep private memory out of transport/exports and preserve capture-1/2 digests. Run browser codec tests and lint.
4. Run `make verify` and `scripts/ci-postgres.sh`, inspect the diff, open a PR closing #347, and merge only after exact-head CI succeeds and mergeability is clean.

Migration requires stopped older clients. Rollback retains all occurrence/budget rows and schema; do not clear retained memory or replay effects. This work adds no provider caller or execution dispatcher.
