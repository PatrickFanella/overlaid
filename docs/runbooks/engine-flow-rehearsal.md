# Captured automation flow rehearsal

[Issue #286](https://git.subcult.tv/subculture-collective/overlaid/issues/286) adds pure `engine.RehearseFlow(ctx, pinnedGraph, input)` as the first E3 flow rehearsal boundary. The explicit [retained operator rehearsal store method](retained-operator-rehearsal.md) now consumes it; the [workspace metadata API](automation-control-api.md) now selects that method. No editor, effect dispatcher, worker or provider caller is selected. It accepts immutable supplied graphs and facts and returns a trace plus private action proposals. It does not acquire permission, reserve cooldown/stream limits, render variables, persist a run or execute an effect.

## Capture and mode identity

Input supplies a canonical occurrence hash, the original graph digest, a captured clock and seed, immutable tenant-bound facts, a trace-entry cap and one of two modes:

- `reproduce` requires the supplied original digest to equal the complete current pinned graph digest.
- `current_config` labels a test of the supplied graph against captured input, retaining both original and current graph digests even if they differ.

`execute` is rejected. The capture digest binds UTC clock, seed and the complete typed fact map. Rehearsal identity also binds original/current graph, occurrence, mode and trace cap. Map insertion order and equivalent clock zones do not change identity. Changing captured values, seed, clock, mode, occurrence, graph or trace cap does. Reconstructing exact canonical versions reproduces the same trace and proposals.

All input remains supplied evidence. A future authenticated caller must load the actual original graph/context and establish source, tenant, availability and freshness; agreement between supplied digests does not prove historical retrieval or current permission. Neither the captured clock nor seed is read from runtime services. Random selection is still unsupported; its seed is bound now for explicit capture identity.

## Supported traversal

Root filters and selected admission groups use the existing three-valued captured predicate evaluator. A missing schema-2 policy or a nonmatching/unavailable root gate returns a blocked report before any node proposals. Admission matching simulates captured predicates only; current roles, plans, cooldown, stream identity and provider permission are not checked or reserved.

Supported nodes are sequential, serial and parallel `all`, condition, switch, finite loop, stop, delay, set output, action declarations and exact pinned presets without invocation arguments. A condition's true branch is first and false branch second. A switch selects the first true group or its final default if all prior groups are false. An unknown group halts traversal with `condition_unavailable`; it never selects a false/default branch or skips to a later true case. Only selected branches are traversed.

Each preset invocation enters its exact retained effect-tree root and local condition groups. It does not select a current version or reapply that preset's independent trigger-admission policy. `stop` halts the entire rehearsal, including callers of a preset. Loops and repeated presets receive separate sequence numbers, preserving their qualified automation/version/node identity.

Action proposals contain capability declarations and opaque authored arguments. For example, `hello $(user)` stays exactly that string. It is not a rendered message or validated transport payload. Variable rendering and capability-specific effect contracts require separate integration. Callers must handle proposals as private workspace input and never send them directly to an adapter.

[Issue #335](https://git.subcult.tv/subculture-collective/overlaid/issues/335) simulates three further kinds without effects:

- `delay` records `visited`/`simulated_delay`, walks its child immediately and sets `Deferred` on the result. Nothing sleeps. Each proposal carries `DelayMS`, its fixed offset from the occurrence clock: sequential children start where the previous child ended, a delay adds its `delayMs` to its own subtree, parallel `all` branches share a start and finish at the latest branch, and loop iterations run one after another. A total offset above 24 hours (`MaxDelayMS`) fails the whole rehearsal with `engine_rehearsal_limit`. Offsets are not in the saved trace.
- Chat-text rendering keeps each proposal's offset and still renders every reply from the occurrence clock, never a later time. Chat-plan preparation stores each reply's offset as `delayMs` in the canonical plan body, omitted when zero so older plans keep their digests. Admission gives every reply its own fixed window: `not_before` is the occurrence clock plus the offset, and the deadline is the run deadline plus the offset. Both are set once and never renewed. The queue job is scheduled for `not_before`, and the executor defers before pacing, authorization or any claim while database time is earlier. A delay that shifts no reply leaves the plan immediate.
- Parallel `all` records `visited`/`parallel_branches` and walks children in authored order. Rehearsal says nothing about real concurrency or ordering.
- `set_output` records `visited`/`output_declared`. The authored value is not rendered, stored or made available to later steps.

Weighted random, no-repeat and argument-bearing presets still stop with fixed `unsupported_node` metadata. Random needs a captured seed, and the operator capture format records seed 0 only as a placeholder. No-repeat needs retained per-automation state. Preset arguments have no binding contract yet. Saved traces from before #335 that recorded delay, parallel `all` or set output as unsupported remain valid. Older binaries refuse to load traces containing the new reasons; stored rows are unchanged, so returning to a current binary restores them. The report may retain the trace and proposals from before a blocked/unsupported boundary; that prefix is diagnostic, not a complete executable plan. These cases are explicit remaining work rather than successful rehearsals of those behaviors.

## Bounds, trace and cancellation

The complete graph retains its existing version, canonical-byte, depth and expanded-step validation bounds. Traversal independently caps depth at 16 and evaluated nodes at 100. Private proposals are bounded to 64 KiB of authored argument name/value bytes; this is not a serialized JSON or heap bound. Exceeding a runtime bound returns fixed `engine_rehearsal_limit` with no partial result.

The caller chooses 1–100 retained trace entries. Sequence numbers count all observed steps, including omitted ones, and `DroppedSteps` reports truncation explicitly. Truncating trace retention does not remove proposals or skip traversal. Entries contain qualified authored IDs, parent/sequence, kind and fixed outcomes/reasons. They contain no predicate values, argument values, native payloads or delivery identities. Authored identifiers and capture digests remain private metadata; this is not a public export format.

Caller cancellation is checked throughout predicate/traversal work and before returning. Cancellation and other errors return no partial trace/proposals and preserve context identity. The API exposes no provider, persistence, variable-resolver or effect callback, and imports no transport/OBS adapter. There are no provider or product mutation effects to replay. Trace persistence, captured variable integration, elapsed-time reporting, durable continuations and actual delivery receipts remain separate.

## Verification and remaining work

Engine race tests cover canonical reconstruction, capture/mode/current-graph binding, tenant mismatch, mutation isolation, root refusal, lazy branch selection, unknown switch ordering, loop/preset/stop traversal, simulated delay/parallel/output steps, sequential/parallel/loop delay offsets and the 24-hour offset cap, remaining unsupported cases, trace truncation, proposal exhaustion and mid-evaluation cancellation. `FuzzRehearsalCapture` checks bounded captured inputs and deterministic output. Run `make verify`, engine race tests, bounded fuzzing and new-diff lint before merge. This unit changes no SQL, durable queue or startup caller.

E3 context/trace persistence and authenticated retrieval are unfinished. E4 effect admission/continuations, E5 remaining semantics and the command/Newsroom/timer/editor migrations remain under [Phase #25](https://git.subcult.tv/subculture-collective/overlaid/issues/25). A pure proposed trace is not run completion, delivery, live acceptance or full Newsroom parity.
