# Syntax-2 variable template parser

[Issue #268](https://git.subcult.tv/subculture-collective/overlaid/issues/268) implements V1a, the pure parser portion of the [Phase 3 plan](../superpowers/plans/2026-10-21-phase-3-engine-and-variables.md), in `backend/internal/variables`. It implements the syntax boundary from [ADR 0006](../adr/0006-variable-language-sandbox.md). No application consumer imports it. Existing brace templates and dispatchers retain their current behavior.

## Input and AST

`Parse(source, 2)` returns a private `Template` snapshot or a `Diagnostic`. Other syntax versions are refused. The original source is retained, including whitespace and escapes; `Parts()` returns a deep copy of its syntax tree. An explicitly parsed empty template is valid; the zero snapshot reports syntax version zero and is invalid.

Template parts are literal text or calls. Named calls preserve their spelling, such as `$(user)`, `$(api:weather)` or `$(nowPlaying.title)`. Identifiers use ASCII letters followed by letters, digits, underscores, dots, dashes or colons. The parser does not decide whether a name is supported. In particular, parsing `eval` or `urlfetch` is only recognizing text; it opens no evaluator or network path. The future renderer must use a typed allowlist and visible unknown-name diagnostics.

Calls contain whitespace-separated bare, quoted or nested-call arguments. Either quote style can contain nested calls. A backslash represents the following Unicode character literally; a trailing backslash is an error. Unicode whitespace separates arguments. A quote at the beginning of an argument opens a quoted argument; quotes within an already-started bare token remain literal characters. Unescaped parentheses cannot occur in bare text. Adjacent bare/quoted/call arguments require whitespace; concatenate literal/call parts inside one quoted argument instead. Outside calls, braces, quotes and ordinary parentheses remain literal text. An escaped `$(` opener stays literal and is never reparsed.

| Source | Stored structure |
| --- | --- |
| `$(user)` | Named call with no arguments |
| `$(weather $(query))` | Weather call with one nested-call argument |
| `$(weather "near $(query)")` | One quoted argument with a literal and nested call |
| `$(1)` | Position one |
| `$(2:)` | Range starting at two with an open end |
| `$(1:3 fallback $(user))` | Inclusive range plus two unevaluated fallback arguments |
| `\$(user) {user}` | Literal `$(user) {user}` under syntax 2 |

Positions are one-based positive decimal integers without leading zeros, bounded to signed 32-bit values for portable representation. Zero, negative, reversed, malformed and overflowing selectors are rejected. An open end is represented explicitly, never expanded during parsing. Position arguments retain the fallback expression sequence; this parser neither evaluates it nor decides when it is needed. Future rendering must evaluate it lazily and join its argument results by one space, without reparsing those results.

## Bounds, spans and privacy

Input is limited to 4 KiB of valid UTF-8 without NUL. At most 16 nested calls and 256 AST nodes are accepted. A literal or call part counts as one node, as does each argument container. Empty quoted arguments have no literal child. The outer template and selector metadata do not consume nodes. Quoted containers consume nodes but do not add another call-nesting level. These are parser bounds, not rendering, provider or performance guarantees.

Every part, call, selector head and argument retains a half-open **byte** span in the original source. Argument spans include quotes when present; literal values contain unescaped text, while their spans cover the original escaped bytes. Offsets do not count Unicode characters. Adjacent separator errors may identify a zero-width insertion point. Unmatched calls/quotes and dangling escapes identify the offending source range.

Diagnostics expose fixed codes and spans, with error strings prefixed `variable_`: syntax_version, source_limit, invalid_utf8, invalid_character, invalid_selector, unclosed_call, unclosed_quote, dangling_escape, expected_argument, unexpected_parenthesis, depth_limit and node_limit. Error messages contain no submitted source. Failure returns no partial AST. Original source and returned AST copies are private configuration, not a redacted trace or public export.

After parsing, a snapshot supports concurrent reads; changing nested slices, calls, ranges or arguments returned by `Parts()` cannot change its source or AST. The parser owns a bounded copy of the source rather than retaining a potentially much larger caller string backing store.

## Remaining gates

V1b still needs pure bounded rendering with captured values/clock/seed, typed name resolution, lazy fallback, no output reparse, 100 resolution steps, an 8 KiB output limit and the ordinary render deadline. V2 owns the bounded tenant/syntax/source cache and syntax-aware conversion preview. No existing template is upgraded automatically. SQL versions/run admission, complete preset resolution, current authorization and retained effect claims remain separate from parsing. Eval remains disabled until its process and OS isolation are qualified; no in-process fallback is added.

## Verification

Run `make verify` from the repository root. Focused checks from `backend/` are:

```bash
go test -race -count=1 -cover ./internal/variables
go vet ./internal/variables
GOMAXPROCS=2 go test ./internal/variables -run '^$' -fuzz '^FuzzParse$' -fuzztime=15s
```

Fixtures cover original multibyte/escaped spans, grammar examples, empty inputs, unknown names as data, quote and selector errors, exact source/depth/node limits, no partial success, snapshot mutation and concurrent reads. Fuzzing checks termination, bounded diagnostics, AST span/accounting and deterministic reconstruction. It cannot establish renderer, cache, migration, database or provider behavior. See the [decision trail](../superpowers/plans/2026-10-21-variable-parser-decisions.tsv).
