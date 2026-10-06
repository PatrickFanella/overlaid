# ADR 0008: Supported single-tenant self-hosting

## Status

Proposed — 2026-10-06. Completes the self-host decision required by #10 and the Phase 0 program.

## Context

The hosted program must preserve the current minimal, environment-driven installation. Maintaining a separate self-host code fork would diverge behavior and security fixes.

## Decision

Use one codebase and one PostgreSQL schema. Deployment mode is explicit: selfhost or hosted; exposure mode local/lan/public is independent. Selfhost bootstraps one stable tenant with the unlimited plan inside a transaction protected by a unique bootstrap key. Repeated boots reconcile non-secret environment defaults without overwriting operator-edited configuration. Never create a fresh tenant because a channel name changed. Ownership adoption and changing deployment mode are explicit administrative migrations, not an inferred startup action.

Environment OAuth configuration and saved legacy connection tokens seed the tenant vault once. Keep configured deployment secrets out of product config/audit. OPERATOR_SECRET may authenticate the selfhost operator into that tenant; hosted mode uses account sessions and refuses a global operator secret as tenant authority. OVERLAY_TOKEN becomes a tenant-scoped read-only token; store its hash and allow later rotation from the desk. Local development can remain credential-free on loopback. LAN/public deployments reject missing credentials and report degraded readiness.

Unlimited removes paid-tier ceilings but retains hard resource budgets, provider rate/quota limits, permission checks and bounded event retention. Stripe is not loaded in selfhost mode. Product name, public domain and bot username remain configuration, so renaming does not require a code fork or remap immutable platform IDs.

Start API, ingest, engine, outbound and overlay gateway roles in one binary. The same role selectors can split containers when measurements justify it. PostgreSQL remains required for product data/jobs; local file fallbacks remain development/recovery facilities, not a second multi-tenant persistence implementation. Hosted OBS cannot reach a creator's localhost; the later browser bridge must carry tenant/purpose-scoped effects. Selfhost can keep direct OBS WebSocket control with existing operator guards.

## Migration and support contract

ADR 0003 moves existing global data into the bootstrap tenant, preserving IDs and timestamps. Document env mappings, token rotation and restore before release. Changing selfhost to hosted requires an operator-run ownership/account adoption tool, a backup and isolation checks before new signups. Reverse migration is an export/import of one chosen tenant into an empty selfhost installation; never drop other tenants automatically. Neither migration is performed by this Phase 0 draft.

CI must run tenant-scoped store/engine tests in both modes, plus bootstrap idempotence, environment seeding, restart with user edits, unlimited plan enforcement, missing-public-secret denial and restore fixtures. Compose smoke tests must show one binary plus PostgreSQL booting without Stripe or other hosted-only credentials. `/healthz` stays cheap liveness; monitoring parses `/readyz` checks via the existing probe. Publish each phase's selfhost setup and migration instructions.

## Alternatives and consequences

A separate single-tenant fork is initially smaller but doubles migrations and security maintenance. Requiring public account signup breaks private installs. An env-backed tenant in the shared model adds bootstrap and mode tests but gives selfhost the same editor, queues, adapters and fixes as hosted deployments. Hosted-only external reviews and billing credentials cannot gate private local development. Eval still needs the containment controls in ADR 0006 and defaults off until they are available.
