# ADR 0006: Parsed variables and isolated evaluation

## Status

Accepted — 2026-10-13. The repository owner confirmed acceptance of this decision under [issue #12](https://git.subcult.tv/subculture-collective/overlaid/issues/12); the issue is closed. Acceptance defines the implementation contract and does not claim the feature is implemented.

## Context

Current templates substitute a small set of brace variables. Imported bot templates need nested expressions and argument ranges. Network lookups and JavaScript introduce risks that a string replacement function cannot control.

## Decision

Store source, syntax_version and parsed AST validation result on each automation version. Version 2 recognizes literal text and `$(name args)` calls, nested calls, quoted strings, backslash escapes and positional selectors. Named calls resolve through a typed allowlist; variable output is text/value data and is never reparsed as executable template source.

| Input | Contract |
| --- | --- |
| `$(user)` | Sender display name |
| `$(query)` | Original command argument tail |
| `$(1)` | First whitespace-delimited argument |
| `$(2:)` | Arguments two through the end, joined by one space |
| `$(1:3 fallback)` | Inclusive range one through three; fallback when range is empty |
| `$(weather $(query))` | Evaluate inner query as an argument value, then weather |
| `$(urlfetch "https://example.test/path")` | Bounded lookup through the guarded HTTP service |

The parser contract is:

```ebnf
template   = { literal | call } ;
call       = "$(", (named | positional), ")" ;
named      = identifier, { whitespace, argument } ;
positional = positiveInt, [ ":", [ positiveInt ] ], [ whitespace, fallback ] ;
argument   = bareText | quoted | call ;
fallback   = argument, { whitespace, argument } ;
quoted     = '"', { escaped | quotedLiteral | call }, '"'
           | "'", { escaped | singleQuotedLiteral | call }, "'" ;
identifier = letter, { letter | digit | "_" | "." | "-" | ":" } ;
positiveInt = nonzeroDigit, { digit } ;
escaped    = "\\", character ;
```

`literal` excludes unescaped `$(`; bare text excludes unescaped whitespace and parentheses. Quoted literal excludes its matching quote and unescaped `$(`. Escapes represent the following character literally. The first token selects the numeric or named branch; `api:name` is a named token, while `2:` is positional. Fallback is a lazy expression sequence joined by one space, not another reparse of its rendered text. An unmatched opener/quote is a save-time syntax error with a source span.

One-based indexes; reversed/zero/negative ranges are parse errors. Fallback is the remaining argument expression, evaluated only if needed. Unknown names produce a visible diagnostic and configured fallback, never silent executable code. Nesting and positional range support match the compatibility goal; importers translate provider aliases and report differences rather than claiming identical whitespace/eval behavior. [Nightbot arguments](https://docs.nightbot.tv/variables/arguments) describe positional ranges; [nested query examples](https://docs.nightbot.tv/variables/query) support nested calls. The [research catalogue](../research/2026-10-05-bot-parity-research.md) is the importer coverage checklist.

Parse once per version; cache by tenant, syntax version and source hash with bounded entries. Initial budgets: 4 KiB template source, AST depth 16, 256 nodes, 100 resolution steps, output 8 KiB (then platform-specific chat truncation), and one-second ordinary render deadline. Pure variables are synchronous; external work uses explicit context cancellation and per-tenant reservations. Phase 3 must test and tune these figures rather than inheriting them as performance claims.

## Eval boundary

Use goja only in a disposable child worker process, one request at a time. No host objects, filesystem/network bindings, dynamic modules or shared runtime across tenants. Inputs are bounded JSON values; outputs are bounded strings/scalars. Disable nondeterministic host state for replay and provide a captured clock/seed. The parent enforces a 100 ms evaluation deadline plus a hard 250 ms process deadline; goja Interrupt is cooperative for JavaScript and cannot interrupt arbitrary Go host calls, so no blocking host callbacks are exposed. This limitation is documented in [goja Interrupt](https://pkg.go.dev/github.com/dop251/goja#Runtime.Interrupt).

Goja has no documented per-runtime hard heap quota. A Go memory target is not a containment boundary. Require an OS/container-enforced 64 MiB worker limit, capped stack depth, process count and input/output pipes, with no-network namespace and read-only/minimal filesystem before enabling hosted eval. Kill/reap on budget or protocol failure; do not retry runaway code automatically. Self-host also defaults eval off unless the launcher proves these controls. The single-binary deployment can spawn its own evaluator role; an in-process goroutine is not an acceptable fallback. Limits are initial proposals and must cover actual Go baseline memory in stress tests before enablement.

## HTTP variables

Keep the current HTTPS-only public-destination and credential/SSRF guards. Resolve and validate all DNS answers, pin allowed IPs for connection, revalidate every redirect, reject private/loopback/link-local/reserved addresses and embedded credentials, and restrict ports. Never share a tenant's authenticated response cache with another tenant. Proposed per-render budget: two requests, 2 s each within a 3 s external-resolution deadline, 64 KiB body, three validated redirects, and plan-based daily budgets. Log selectors/status and redact auth headers/query secrets; failed fetches use configured fallback.

Signed headers resolve vaulted tenant secrets through allowlisted header names at dispatch. No user-supplied Host, Cookie, forwarding or hop-by-hop headers. Delayed response URLs are short-lived, single-use random capabilities, stored hashed and bound to tenant/run/effect; accept a capped response through a constrained endpoint, never a caller-provided arbitrary callback destination. Do not introduce this callback mode until replay, expiry, duplicate and deletion tests pass. Eval cannot call urlfetch directly.

## Migration, alternatives, consequences

A syntax-aware migration rewrites only known legacy `{user}`, `{login}`, `{args}`, channel/API and now-playing variables to version-2 equivalents; escaped braces and unknown text remain literal with a review warning. Keep the original source and syntax-1 renderer for rollback. Import preview shows converted output on fixtures before enabling a version. Never rewrite JavaScript or arbitrary text with global regex replacement.

Keeping brace-only syntax limits compatibility; embedding full JavaScript for every variable makes ordinary rendering unsafe and opaque. A bounded AST is more implementation work but offers predictable error spans, lazy fallback and accounting. Isolated eval costs process startup and memory; cache compiled programs only inside bounded workers with no cross-tenant state. Acceptance includes parser fuzzing, nesting/range fixtures, legacy conversion, SSRF DNS/redirect rebinding tests, and process-kill tests for infinite loops and memory exhaustion.
