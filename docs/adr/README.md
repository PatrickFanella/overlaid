# Architecture Decision Records

Use ADRs for decisions that affect structure, dependencies, operations, security, or long-term maintenance.

Suggested format:

```markdown
# ADR NNNN: Title

## Status

Proposed | Accepted | Superseded

## Context

What forces or constraints led to this decision?

## Decision

What did we choose?

## Consequences

What trade-offs follow from this choice?
```

## Records

- [0001: Generic base template](0001-generic-base-template.md)
- [0002: PostgreSQL product storage](0002-postgres-product-storage.md)
- [0003: Tenant and account model](0003-tenant-account-model.md) — Accepted 2026-10-13, #10
- [0004: Platform adapter boundary](0004-platform-adapter-boundary.md) — Accepted 2026-10-13, #11
- [0005: Automation engine](0005-automation-engine.md) — Accepted 2026-10-13, #12
- [0006: Variable language and evaluation sandbox](0006-variable-language-sandbox.md) — Accepted 2026-10-13, #12
- [0007: PostgreSQL jobs and fan-out](0007-postgres-jobs-fanout.md) — Accepted 2026-10-13, #11
- [0008: Supported self-host mode](0008-supported-self-host-mode.md) — Accepted 2026-10-13, #10

The repository owner accepted all six Phase 0 decisions under issues #10–12 on October 13, 2026. Phase 1 must implement and verify these contracts; architecture acceptance alone does not establish implementation or runtime readiness.
