# Legacy variable conversion preview

Phase 3 V2b is tracked by [#274](https://git.subcult.tv/subculture-collective/overlaid/issues/274). The explicit `templatepreview` package compares original syntax-1 text with proposed syntax-2 text. The account preview route and dashboard panel below are its only callers; no editor, importer, save path or dispatcher calls it. It does not publish or select a version.

`Convert(source)` preserves original text and syntax version 1 alongside proposed text and version 2. It scans brace tokens using the actual legacy character grammar, preserving half-open original byte spans for warnings. It maps recognized names case-insensitively:

| Legacy token | Proposed call / supplied value |
| --- | --- |
| `{user}`, `{displayname}` | `$(user)`, trimmed display name or trimmed login fallback |
| `{args}` | `$(query)`, original supplied argument tail |
| `{channel}`, `{command}` | Corresponding builtin |
| `{nowplaying.title}`, `{nowplaying.artist}` | `$(nowPlaying.title)`, `$(nowPlaying.artist)` |
| `{count:name}` | Exact captured `$(count:name)` result; missing supplied counter is zero |
| `{api:name}` | Exact captured `$(api:name)` result; missing supplied value is empty |
| `{quote:random}` | Captured supplied quote text; no random quote lookup |

`{login}` is unknown in the actual legacy renderer. Unknown tokens remain literal in proposed source and produce `unknown_variable` warnings. A backslash immediately before a brace token preserves that whole sequence literally and produces `escaped_brace`. Legacy rendering does substitute recognized escaped brace tokens and removes unknown ones; preview shows that difference. Existing dollar openers are escaped as literal text with `literal_call` warnings. Known brace substitutions inside that text still follow the original legacy token behavior; no dollar expression or JavaScript is interpreted. Literal backslashes are escaped for syntax 2, including malformed-looking legacy text. Values returned by either renderer are never reparsed.

`Preview(ctx, source, bot.TemplateContext)` computes old output through `bot.RenderTemplate`, not the dependency-assembling `TemplateRenderer`. It constructs a bounded snapshot from only referenced supplied values, then calls the pure syntax-2 renderer. No community reads, counter mutations, API/now-playing requests, random selection, clocks, effects or provider callbacks are available. Supplied missing API/quote text is available empty and missing counters are zero, matching legacy fixture behavior. This does not establish provider availability, input freshness or authenticated retained capture. Future capture/replay integration must preserve those distinctions.

The result contains both outputs, a `Different` flag, conversion warnings and fixed render diagnostics. Every conversion includes an `output_policy` warning: legacy output trims whitespace and caps at 400 runes, while proposed variable output preserves whitespace and has an 8 KiB byte limit. Preview deliberately shows both outputs before a future platform policy or explicit version selection. Equal fixture outputs do not prove catalogue-wide parity or authorize cutover.

Original and proposed sources each have a 4 KiB UTF-8, NUL-free limit. Conversion permits at most 100 recognized brace-token occurrences and 100 warnings including the output-policy warning; proposed AST limits remain 256 nodes and depth 16. Captured definitions are limited to 64, values to 8 KiB each and snapshot text to 64 KiB. Referenced escaped tokens are bounded too because the old renderer still resolves them. Unreferenced context values are not copied. The caller must not mutate supplied maps during preview. Rendering retains its 100-resolution, 8 KiB output and cooperative one-second limit. Caller cancellation is checked before and between stages and retains context error identity.

Invalid input, conversion expansion/AST limits, invalid snapshots and terminal rendering failures return no partial preview. Fixed errors are `template_preview_input`, `template_preview_limit`, `template_preview_snapshot`, or the renderer/context error. Sources, supplied values and both outputs are private content; the result is not suitable for general logs or public exports. No automatic truncation of converted source, partial migration, replacement request key or fallback execution occurs. Future lifecycle and authorization boundaries must be established before exposing or saving previews.

Fixtures compare actual legacy outputs for all mappings, absent values, case folding, display-name fallback, escaped/unknown braces, nested-looking and mixed dollar syntax, Unicode spans, whitespace and 400-rune differences. Budget failures return no partial output. Focused races and `FuzzConversionLiteralSafety` check source/warning/output bounds, inert literal calls and legacy-policy equivalence when no deliberate literal-preservation warning applies. Run `make verify` for repository validation. No schema, concurrency owner, cache caller, worker or startup registration changes in this pure unit; no live provider qualification is claimed.

## Account route and dashboard panel

`POST /api/tenants/{id}/automations/preview-template-conversion` runs the preview for a signed-in member with configure permission. It uses the same session, Origin/CSRF and bootstrap refusal as the other automation operations. It does not open the automation store, check the metadata writer owner or read tenant rows.

The body is exactly `{"source": "...", "values": {...}}`. `values` accepts `user`, `displayName`, `channel`, `args`, `command`, `nowPlayingTitle`, `nowPlayingArtist`, `quoteRandom`, `counts` (name → integer) and `apiVariables` (name → text). Unknown fields, a missing `source` or `values`, more than 64 counter or API entries, or names over 128 bytes return HTTP 400. Package errors map to `template_preview_input` (400), `template_preview_limit` (422) and `template_preview_snapshot` (422); timeouts and cancellation use the shared automation codes.

The no-store response is `state: "preview_only"`, both syntax versions, original and proposed sources, `warnings` and `diagnostics` as `{code, start, end}` byte spans, both outputs and `different`. Warning spans refer to the original source; diagnostic spans refer to the proposed source.

The Automations page **Template conversion preview** panel sends typed values only. It requires the response to echo the exact submitted source, syntax 1→2, an `output_policy` warning, known codes, in-range spans and a `different` flag that matches the two outputs. Editing any input aborts an in-flight request and clears the shown result. Source, values and outputs stay in component memory; there is no save, select or migration control.
