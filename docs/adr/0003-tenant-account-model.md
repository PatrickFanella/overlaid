# ADR 0003: Accounts, tenants, and scoped product data

## Status

Proposed — 2026-10-06. Addresses #10. Human review must accept this decision before Phase 1 tenancy implementation.

## Context

The current single channel stores global configuration and a broadcaster token. Hosted creators must not share authorization or data. The same code must support environment-bootstrapped self-hosting. This follows the [October roadmap](../superpowers/plans/2026-10-05-multiplatform-parity-roadmap.md), rather than extending the earlier Twitch-only plan.

## Decision

Use one PostgreSQL database and shared tables. An account is a person; a tenant is a creator workspace. Platform identities are separate from workspace membership. Proposed tables:

| Table | Keys and responsibilities |
| --- | --- |
| accounts | UUID, display name, lifecycle timestamps |
| account_identities | account_id, provider, immutable provider user ID; unique(provider, user ID) |
| tenants | UUID, unique slug, owner_account_id, plan_id, retention policy, lifecycle |
| tenant_memberships | tenant_id + account_id unique, role, inviter, timestamps |
| platform_connections | tenant_id, platform, channel ID, bot_account_id, encrypted credential reference, granted scopes, granted_at, revoked_at |
| bot_accounts | platform identity, encrypted credential reference, service-owned or tenant-owned scope |
| plans | free, paid, unlimited; versioned entitlement document |
| tenant_plan_assignments | current assignment and billing provenance; audited changes |
| overlay_tokens | tenant_id, hashed random token, purpose, expiry/revocation, last-used timestamp |

Exactly one owner controls billing, deletion and ownership transfer. Manager administers connections, memberships below manager, and configuration. Editor changes commands, timers, overlays and automation, but cannot grant access or read tokens. Moderator can read the moderation workspace and moderate; it cannot edit automation or billing. Authorization uses explicit operation permissions, not an assumption that any numeric role can do everything below it. Only the owner can appoint managers or transfer ownership; transfers update owner and memberships atomically and cannot leave a tenant ownerless.

Every product row, audit receipt, event, run, job and cached value carries tenant_id. Tenant-scoped stores require a validated TenantContext; public HTTP input alone cannot create one. Every lookup and mutation includes tenant_id, including updates/deletes by ID. Composite foreign keys (tenant_id, parent_id) prevent cross-tenant references. Service-owned plan/bot identity rows use separate administrative access; credentials never enter generic config JSON.

Enable PostgreSQL row-level security as defense in depth on product tables in Phase 1. Runtime connections use a non-owner role without BYPASSRLS, and FORCE ROW LEVEL SECURITY covers table ownership accidents. Transactions set a tenant UUID using SET LOCAL; policies reject missing context. Never set a tenant on a pooled connection outside a transaction. Migration and service-wide retention workers use separate narrowly privileged roles. Store predicates remain mandatory. PostgreSQL documents owner/bypass exceptions and default-deny behavior in [row security policies](https://www.postgresql.org/docs/current/ddl-rowsecurity.html).

Login sessions use opaque server-side session IDs in Secure, HttpOnly, SameSite cookies. Browser mutation requests require origin/CSRF checks. Hosted mode rejects OPERATOR_SECRET as a cross-tenant login. OAuth state binds initiation to the browser session. Token encryption uses a deployment key outside the database, with key version and authenticated tenant/provider identity; refresh serialization and revocation live in the connection store. The Phase 1 plan must choose encryption/session implementation and cover key rotation before production rollout.

Entitlements are plan data with typed validation and versioning. Limits apply at create time and execution time. A downgrade disables excess scheduled execution predictably without deleting configuration; the desk explains the overage. Unlimited removes commercial ceilings, not security budgets, retention obligations or platform limits.

## Migration

Back up and test restore first. In one controlled migration, create a legacy tenant/account and unlimited self-host plan, add nullable tenant_id, backfill all existing product rows, verify counts and foreign keys, then enforce NOT NULL and composite references. Upgrade stores and streams together before accepting hosted requests. Import the current token and interaction/config documents into that tenant through the vault; do not copy secrets into audit diffs. Overlay credentials migrate to a tenant token and old URLs are explicitly rotated. Record a migration receipt without secret material. Rollback restores the pre-migration backup while traffic is paused; no multi-tenant traffic is allowed before the migration passes isolation tests.

## Alternatives and consequences

Database per tenant gives strong boundaries but raises migration and connection costs on the initial homelab footprint. Schema per tenant creates similar operational growth. Store predicates alone are simpler but one missed predicate leaks data. Shared tables plus predicates and RLS add transaction discipline and schema work, but allow common migrations and measured capacity growth.

Acceptance requires tests with two tenants across every API, stream, queue and store; guessed IDs and forged references must fail. Exercise pooled connections with alternating tenants and absent context. Self-host bootstrap must still work. Export/deletion and retention jobs must be tenant-scoped and revoke tokens before removing data. Proposal acceptance is a review decision, not evidence that these features are implemented.
