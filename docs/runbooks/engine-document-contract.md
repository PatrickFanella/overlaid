# Versioned engine document contract

[Issue #266](https://git.subcult.tv/subculture-collective/overlaid/issues/266) implements E1 of the [Phase 3 plan](../superpowers/plans/2026-10-21-phase-3-engine-and-variables.md) in `backend/internal/engine`. This is a pure structural contract. The application does not import or activate it. Existing commands, Newsroom actions, timers, rewards and quick actions retain their current dispatchers.

## Validate and retain a snapshot

`Validate(Document)` checks typed input and returns `Validated`, which owns private canonical JSON bytes. `Parse([]byte)` also rejects unknown or duplicate JSON object fields (including Go JSON case-fold aliases), trailing input, invalid raw UTF-8, excessive JSON nesting and oversized input. Errors are the fixed `engine_document_invalid`; they contain no submitted text. The zero `Validated` value is invalid.

Schema version 1 contains canonical lowercase nonzero UUIDs for tenant, automation and version; a stable trigger ID/kind; capability declarations from the existing platform registry; typed field predicates, restriction groups and a node graph with one root. Field names are bounded symbols and operators are `eq`, `neq`, `exists`, `gte` or `lte`. The contract does not resolve those symbols against a live event or evaluate predicates. Empty all/any/none groups are representable; their evaluation semantics remain those of ADR 0005. Permission and entitlement checks will be mandatory outside these groups.

Node IDs share a document-wide namespace with the trigger, filter, group and predicate IDs. IDs use one to 64 lowercase ASCII letters, digits, underscores, dots, colons or dashes, starting with a letter. Dangling references, cycles, unreachable nodes and duplicate IDs are refused. Node-array order and capability declaration order are normalized; ordered children, restrictions, predicates and arguments retain their order.

Supported structural node kinds are sequential, all, weighted random, condition, switch, loop, delay, stop, set output, pinned preset and action. Nonzero configuration belonging to another kind is rejected. Conditions refer to restriction groups; switch children correspond to the listed conditions followed by a required default. An action must name a declared capability. A declaration, including a request for parallel work, grants no permission and selects no provider adapter. Actual predicate, control-flow and action evaluation is future work.

## Bounds and unresolved presets

- Source JSON and canonical snapshot: at most 64 KiB each. JSON nesting is capped at 16 before typed decoding.
- Local graph: at most 100 nodes, depth 16 and 100 worst-case evaluated steps, including control nodes and repeated DAG edges. Random/conditional branches use the most expensive branch; sequential/all branches sum their cost. Loops expand their child cost, with at most 20 iterations. These checks avoid unchecked multiplication.
- Filters and restriction predicates: at most 100 combined, with at most 100 groups. Arguments: at most 32 unique names per node; each text value is valid UTF-8, at most 4 KiB and contains no NUL.
- Weighted choices: positive integer weights with total at most 1,000,000. Parallel all requests declare a concurrency bound from one to four; sequential all uses zero.
- Delay declarations: one millisecond through 24 hours. This is a structural bound, not a scheduling or retention allowance. A future continuation must still honor its original source deadline; active computation remains separately bounded by ADR 0005.

A preset reference must specify the same tenant and a nonzero automation/version UUID. Direct self-automation references are conservatively refused, including references to another version of that automation. External preset bodies are **unresolved**: this validator counts the local call as one step and cannot prove the referenced body's cost or absence of cross-version cycles. Before admission or evaluation, the future tenant-scoped boundary must load exact pinned versions, validate the complete reference graph and expanded budgets, and refuse missing, deleted, foreign or cyclic input. Structural validation alone is insufficient for activation.

`JSON()` returns a fresh byte slice and `Document()` returns a fresh deep copy. Changing the original typed input, returned bytes or nested collections cannot change the validated snapshot. Callers must not mutate input concurrently with `Validate`; afterward, a validated snapshot supports concurrent reads. These accessors return private configuration, not a redacted trace or public export.

## Digest and run identity

`Digest()` is a SHA-256 of the canonical schema-1 document under the `overlaid.engine.document.v1` domain. `RunID` hashes a fixed-order JSON tuple under `overlaid.engine.run.v1`, binding tenant, canonical occurrence ID, automation ID, version ID, document digest and mode. Occurrence IDs are 64 lowercase hexadecimal characters supplied by a future trusted source adapter; hashing untrusted input does not establish that it is an admitted occurrence.

The modes are `execute`, `reproduce` and `current_config`. Missing/invalid snapshots, occurrences or modes return `engine_identity_invalid`. Reconstructing the same snapshot and occurrence yields the same identity. Changing the tenant, occurrence, automation/version, content or mode changes it. Reproduce and current-configuration rehearsal remain distinct from execution.

The ID is not a durable uniqueness constraint, reservation, permission check, pacing permit or effect claim. E2 must atomically bind run admission to current tenant permissions, entitlement, immutable saved versions and reservations. Later effect identities must include their original run-step/iteration occurrence and use existing retained no-replay claims; a node ID or run ID alone cannot deduplicate provider effects. No clock, random source, captured-context loader, renderer, database, queue, network or OBS executor is provided here.

## Verification

From the repository root, run `make verify`. From `backend/`, focused qualification is:

```bash
go test -race -count=1 -cover ./internal/engine
go vet ./internal/engine
GOMAXPROCS=2 go test ./internal/engine -run '^$' -fuzz '^FuzzParse$' -fuzztime=15s
```

Fixtures exercise graph/refusal boundaries, expanded loops, repeated DAG cost, depth and step edges, control shapes, pinned/foreign/self presets, strict JSON, shared predicate bounds, mutation isolation, concurrent reads, reconstruction and identity bindings. Fuzzing checks bounded parse/refusal and canonical reconstruction; it does not establish evaluator, runtime, database or provider behavior. The [decision trail](../superpowers/plans/2026-10-21-engine-contract-decisions.tsv) records qualification and limitations.

The separate [complete pinned graph resolver](engine-pinned-graphs.md) can now validate supplied exact preset bodies across versions. Local validation and `Validated.RunID` remain unchanged; future admission must load those immutable bodies within its tenant boundary and use the complete graph identity. No persistence or effect permission follows from either structural check.
