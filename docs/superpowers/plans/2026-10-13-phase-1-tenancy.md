# Phase 1: Tenancy and accounts implementation plan

Scope: issues #13–22. The owner accepted ADRs 0003–0008 under #10–12. Phase 0's webhook closeout is the prerequisite change. External application registration and provider reviews remain #23; later platform login adapters remain in their platform phases, as #13 specifies Twitch first.

## Ordered implementation

1. Add an explicit, transactional migration for control-plane tables and tenant ownership of existing product rows. Preserve legacy IDs/timestamps, use one stable bootstrap tenant, replace global singleton/name constraints with tenant-scoped constraints, and add composite references plus forced RLS. Runtime credentials must be non-owner and cannot bypass RLS; migration uses separate administrator credentials.
2. Give every product store an immutable tenant scope. Use transaction-local context on pooled connections, explicit scope predicates in queries, separate event buses and retained state per tenant, and isolated timer/API-variable runtime state. Reject hosted startup if migration/runtime invariants are absent.
3. Add Twitch account login, opaque expiring/revocable sessions and CSRF/origin checks. Keep self-host operator-header access restricted to the bootstrap tenant. Bind OAuth state to a browser session; identity is provider plus immutable user ID, never an email match.
4. Add tenant creation/switching, memberships, explicit owner/manager/editor/moderator permissions and transactional change audits. Preserve one owner and safe ownership transfer.
5. Move connection tokens to an AES-GCM vault with a versioned environment keyring, tenant/provider authenticated data, serialized refresh and revocation. Import the legacy self-host token once without removing rollback files. Hash rotatable overlay/webhook credentials and revoke active credential-bound streams.
6. Add data-driven plan limits enforced at configuration and execution boundaries. Preserve configuration on downgrade and explain overages. Self-host gets unlimited commercial limits, while hard safety/provider budgets remain.
7. Add retention, expiry, secret-free tenant export, viewer deletion and tenant deletion with token/session revocation and audit receipts. Add privacy/terms and a public status surface with no private data.
8. Verify self-host and hosted modes with fresh disposable Postgres, migration/restart/restore fixtures, pooled alternating-tenant and guessed-ID tests across stores/REST/SSE/jobs, OAuth/session/role tests, and browser workflows. Keep `make verify` required. Prepare monitoring/backup integration and distinguish an isolated drill from real homelab alert delivery and public rollout.

## Verification and rollout

Run `make verify`, full disposable-Postgres `go test -race ./... -count=1`, repository browser smoke, and isolated native/Compose checks. Do not substitute a global operator secret for hosted tenant identity. Re-read tenant membership, plan and revocation state at request/execution boundaries.

Migration is explicit, with traffic paused, verified backup/restore, a separate administrator connection and non-secret row-count receipts. Existing production data and provider credentials are not modified by implementation tests. Rollback restores the pre-migration backup before hosted traffic; an old global binary must not operate the migrated shared database. Actual deployment, public provider registration and externally observed alerts need their target environment and are reported separately from code acceptance.

## Acceptance map

- #13: Twitch sign-in/out, session expiry/revocation, CSRF and preserved self-host headers.
- #14: tenant selection, membership CRUD, role policy and audit diffs.
- #15: all product tables/stores scoped; two-tenant REST/SSE/job and migration evidence.
- #16: encrypted token custody, one refresh/revoke path and legacy token import.
- #17: tenant-scoped overlay credentials, rotation and stream isolation.
- #18: plan rows, runtime/config limits, clear errors and immediate tier changes.
- #19: retention/expiry, export and audited tenant/viewer deletion.
- #20: idempotent env bootstrap, both-mode CI and setup/migration docs.
- #21: role readiness, alert integration, backup/restore evidence and public status.
- #22: privacy/terms routes linked from login and dashboard.

## Current verification

The implementation covers #13–20 and #22, with homelab deployment preparation for #21. Local `make verify` includes disposable PostgreSQL tests; the full Go race suite and all 29 Chromium fixtures pass. A fresh Compose database initializes with a restricted runtime role, the migration ledger safely skips pre-tenancy DDL, and an independent custom-dump restore preserves a representative command with all 18 product tables under forced RLS. Hosted CI, reviewed merge and live homelab/provider acceptance remain separate gates. See [tenant recovery](../../runbooks/tenancy-and-recovery.md) and [homelab release](../../runbooks/homelab-phase-1.md).
