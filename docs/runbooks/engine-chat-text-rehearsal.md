# Captured chat-text rehearsal

[Issue #311](https://git.subcult.tv/subculture-collective/overlaid/issues/311) adds explicit `engine.RehearseChatText` as a pure composition of pinned-graph flow rehearsal and captured syntax-2 variable rendering. It prepares private diagnostic text for future bridge work. The separate [source-bound operator API](automation-control-api.md#rehearse-chat-text-from-the-original-operator-clock) now consumes it with the original database clock only. No provider adapter, queue or effect executor selects it.

## Supplied input and identity

Callers supply a validated `PinnedGraph` and `ChatTextInput`: the original `RehearsalInput`, an immutable `variables.Snapshot` and explicit `SyntaxVersion: 2`. The helper regenerates flow through `RehearseFlow`; it does not accept a caller-authored flow report. Execute mode remains refused. These are supplied facts and values, with no claim of authenticated runtime provenance.

`Snapshot.Fingerprint` returns a version-one SHA-256 over the snapshot's owned values, definitions, captured calls, fallback text and explicit clock/seed presence. Map ordering is deterministic; clocks normalize to UTC. Available-empty, absent and unavailable values remain distinct. A zero snapshot has no fingerprint. The fingerprint is private identifying metadata, not anonymization, authentication or permission. Do not publish it as a delivery receipt.

Explicit snapshot clocks/seeds must agree with the supplied flow clock/seed. Missing clock or seed remains unavailable; neither is copied from the flow input or invented. In particular, the existing retained operator capture's seed zero is a format placeholder and does not establish historical random input. This work does not change that source format or add a trusted variable capture producer.

The chat-text report hash binds the fresh flow report, variable fingerprint and syntax version. Each proposal hash additionally binds its traversal sequence and automation/version/node identity. Repeated loop or preset visits remain distinct, and reconstruction from exact pinned documents and captures is deterministic. These hashes are rehearsal identities; they are not retained effect keys, deduplication guarantees, ledger claims, scheduling or replay permission. Current-config mode retains the existing original/current graph distinction.

## Text preparation and refusal

Only completed supported flow (`proposed` or an explicit `stopped` traversal) can produce text. Blocked/unsupported reports refuse as a whole, including any earlier diagnostic proposals. Each proposal must be `chat.send` with exactly one argument named `text`. Threaded replies and all other capabilities require separate source/effect contracts; extra or missing arguments cannot be silently dropped into a send.

Templates are parsed under explicitly selected syntax 2 and rendered only against the supplied immutable snapshot. No cache, provider lookup, callback, eval, live fallback or automatic template migration is introduced. Rendered values remain literal and are never reparsed. A nonterminal unknown/unavailable/arity diagnostic refuses the whole chat-text report, even if the general renderer produced fallback text. Valid positional fallback with available query input retains the renderer's existing behavior.

Every message must be valid UTF-8, contain no NUL, have non-whitespace text and contain at most 500 Unicode runes. Authored whitespace is preserved; messages are not trimmed or truncated. The complete report permits at most 100 variable resolutions and 64 KiB of rendered UTF-8 text, in addition to existing graph/flow/source bounds. A shared one-second context bounds traversal and all renders. Parse, unsupported, unavailable and bound failures use fixed errors with no submitted text. Any error or cancellation discards all private output; context cancellation/deadline identity is preserved.

## Qualification and remaining bridge work

Unit and race fixtures cover deterministic reconstruction, capture fingerprint/availability/offsets, explicit time/random, literal variable results, repeated loop/preset identities, stop and unsupported prefixes, argument refusal, exact/overflow rune and resolution bounds, aggregate UTF-8 output and canceled/expired contexts. `FuzzChatTextCapture` exercises bounded arbitrary templates/captured values for deterministic results and no private partial output on failure. Run `make verify` and `go test -race ./internal/engine ./internal/variables` from `backend` for the latter command.

The pure API changes no schema, role, pool, worker or runtime concurrency path. Its source-bound store/HTTP consumer separately runs the disposable PostgreSQL gate. No real messages or provider requests are sent. Retained source capture, permission/entitlement checks, atomic ledger/job admission, one-use claims, continuations, dispatcher migration and live acceptance remain unfinished under [Phase #25](https://git.subcult.tv/subculture-collective/overlaid/issues/25). Existing dashboard/operator rehearsal and saved trace codecs remain unchanged and opaque.
