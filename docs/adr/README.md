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
- [0003: Tenant and account model](0003-tenant-account-model.md) — Proposed, #10
- [0004: Platform adapter boundary](0004-platform-adapter-boundary.md) — Proposed, #11
- [0005: Automation engine](0005-automation-engine.md) — Proposed, #12
- [0006: Variable language and evaluation sandbox](0006-variable-language-sandbox.md) — Proposed, #12
- [0007: PostgreSQL jobs and fan-out](0007-postgres-jobs-fanout.md) — Proposed, #11
- [0008: Supported self-host mode](0008-supported-self-host-mode.md) — Proposed, #10

The six Phase 0 proposals are ready for review together. Acceptance must be recorded by a human reviewer; their presence does not mean Phase 1 architecture is accepted or implemented.
