# Rendering variables from captured values

[Issue #270](https://git.subcult.tv/subculture-collective/overlaid/issues/270) implements V1b of the [Phase 3 plan](../superpowers/plans/2026-10-21-phase-3-engine-and-variables.md) beside the [syntax-2 parser](variable-template-parser.md). `variables.Render` consumes a parsed template and an immutable snapshot. No application imports the package; existing syntax-1 templates and dispatchers are unchanged.

## Supply a snapshot

`NewSnapshot(SnapshotInput)` copies and validates input. Values distinguish **available empty text** from **unavailable input**. The zero snapshot is invalid; constructing an explicit empty snapshot is allowed. A snapshot contains:

- Optional available/unavailable values for `user`, `query`, `login`, `channel`, `command`, `nowPlaying.title` and `nowPlaying.artist`.
- Data-only definitions naming captured calls, minimum/maximum argument counts and literal fallback text. Definitions contain no Go callbacks. Names are case-sensitive, use the parser's identifier syntax and have at most 64 bytes. Built-ins cannot be overridden, and eval definitions are refused regardless of case.
- Captured invocation results keyed by the exact name and ordered rendered argument tuple. Length-prefixed keys distinguish one argument containing spaces from several arguments. Missing or explicitly unavailable captures remain unavailable; they never trigger a live lookup.
- An optional copied clock and seed, plus literal unknown/unavailable fallbacks.

These are caller-supplied records. Construction does not prove that a provider was queried, that input is fresh or that it belongs to an authenticated run. Future capture/admission boundaries must establish source, tenant, version and permissions before relying on them. Snapshot data is private configuration; no public export or persisted capture format is introduced.

Input has at most seven fixed built-in fields, 64 definitions, 128 captures and 16 arguments per captured call. Value/fallback/argument strings are valid UTF-8 without NUL and individually at most 8 KiB. The aggregate 64 KiB text budget counts values, fallback strings, definition names, captured arguments and results. Fixed built-in names and repeated references to definition names have separate fixed count/name bounds. Duplicate definitions/capture tuples, unknown fields, invalid arity and unavailable values carrying nonempty text are refused with `variable_snapshot_invalid`. No partial snapshot is returned.

After construction, caller mutation of input maps, slices, clock or seed cannot alter a snapshot. Concurrent renders share immutable data but own their counters, diagnostics and random-draw sequence. Callers must not mutate input concurrently with snapshot construction.

## Render without live resolution

`Render(ctx, template, snapshot)` returns text, fixed-code source-span diagnostics and a resolution count. The parser and snapshot must both be valid. Nil context and invalid snapshots/templates are refused explicitly.

| Expression | Behavior |
| --- | --- |
| `$(user)` and the other fixed text built-ins | Return the supplied available text, including an empty string. Missing/unavailable input uses the configured unavailable fallback with a diagnostic. |
| `$(query)` | Preserve the original argument tail's spacing. |
| `$(1)`, `$(2:)`, `$(1:3 fallback)` | Select whitespace-delimited query words and join them with one space. Closed ranges clamp to the available tail. Evaluate fallback arguments only when the selected range is empty. |
| `$(weather $(query))`, with a declared definition | Evaluate arguments as data and find that exact captured tuple. A missing tuple uses the definition's literal fallback. No provider is contacted. |
| `$(time)` | Format the supplied clock in UTC RFC3339Nano. No supplied clock means unavailable; there is no live-clock value fallback. |
| `$(random a b c)` | Choose supplied argument text using the captured seed and a renderer-local draw ordinal. Missing seed means unavailable. No global random state is read. |

The random contract hashes the `overlaid.variables.random.v1` domain followed by big-endian uint64 seed/draw values, then selects by the first uint64 modulo the choice count. The same template and captured snapshot reconstruct the same results. This variable does not implement the engine's future weighted/no-repeat state.

Named calls validate allowlist membership and arity before evaluating arguments. Unknown names produce `unknown_variable` and the literal unknown fallback; bad arity produces `argument_count`. Registered missing captures and unavailable built-ins produce `unavailable_variable`. Unknown/invalid calls and missing-clock/seed calls do not evaluate unnecessary arguments. Available query text with an empty range is a normal fallback case; unavailable query input also emits an unavailable diagnostic. Explicit positional fallback arguments override the general unavailable fallback.

Values, captures and fallback text are appended as data and are **never reparsed** as templates. A captured result containing `$(eval ...)` remains that literal text. Parsing or rendering cannot install an evaluator; there is no function callback, HTTP resolver, network client, filesystem access or effect executor in this API.

## Budgets and cancellation

Each evaluated call, including a positional selector or unknown name, consumes one of 100 resolution steps. Unevaluated positional fallback consumes none. Final output, each rendered argument and joined fallback text are individually capped at 8 KiB; overrun is refused rather than truncated. Internal captured-call tuple keys are bounded by 16 arguments of at most 8 KiB each. Platform chat truncation remains a separate future integration decision.

Rendering uses a one-second context deadline, respecting an earlier caller deadline and checking cancellation through traversal and before returning success. This is cooperative Go cancellation for bounded pure work, not an evaluator process-isolation boundary. Deadline/cancellation errors preserve `context.DeadlineExceeded`/`context.Canceled` identity. Resolution/output exhaustion returns `variable_render_limit` and a fixed `resolution_limit`/`output_limit` diagnostic. Diagnostics are bounded by 100 resolution outcomes plus one terminal failure and contain only codes and original-source byte spans.

Any terminal budget or cancellation failure returns **no partial text**, even when a prefix was already assembled. Normal unknown/unavailable outcomes can return fallback text with diagnostics. A successful render grants no effect permission, run uniqueness, pacing reservation, provider claim or retry.

## Verification and remaining work

Run `make verify`. Focused checks from `backend/`:

```bash
go test -race -count=1 -cover ./internal/variables
go vet ./internal/variables
GOMAXPROCS=2 go test ./internal/variables -run '^$' -fuzz '^FuzzRender$' -fuzztime=15s
```

Fixtures cover lazy fallback, exact captured tuples, available-empty/unavailable distinction, no output reparse, configured literal fallbacks, captured clock/seed reconstruction, immutable inputs/concurrent renders, aggregate/individual/output/resolution boundaries and pre-canceled/expired caller contexts. Fuzzing checks deterministic captured rendering and bounded outputs/diagnostics. These checks do not demonstrate a live provider, persisted run, cache, migration or application activation.

V2 still needs the bounded tenant/syntax/source cache and syntax-aware conversion preview. E2 owns stored version/run admission; E3 owns authenticated retained context and replay evidence. Live capture adapters, guarded HTTP variables, isolated optional eval, effect execution and dispatcher migration remain separate reviewed work. See the [decision trail](../superpowers/plans/2026-10-21-variable-renderer-decisions.tsv).
