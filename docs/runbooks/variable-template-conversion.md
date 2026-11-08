# Legacy variable conversion preview

Phase 3 V2b is tracked by [#274](https://git.subcult.tv/subculture-collective/overlaid/issues/274). The explicit `templatepreview` package compares original syntax-1 text with proposed syntax-2 text. The account preview routes and dashboard panels below call it with supplied sample values. It does not publish or select a version.

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

## Saved command migration preview

[Issue #354](https://git.subcult.tv/subculture-collective/overlaid/issues/354) connects the comparison to canonical saved commands as the first M1 inventory/preview step for managed PostgreSQL workspaces. In **Automations → Saved command migration preview**, choose **Load saved commands**, then a command. The selection shows its saved ID, name, enabled state, action, response mode, required role, cooldown and snapshot fingerprint. Separate saved aliases stay separate entries. Built-in parser commands outside the saved registry remain part of the later M1 behavior inventory.

For a ticker or alert command, enter sample values and choose **Preview conversion**. The server reads the saved response and compares its actual legacy rendering with the proposed syntax-2 text. For example, the saved response `Hello {user}` with display name `Viewer` proposes `Hello $(user)` and renders `Hello Viewer` in both formats. API values, counters, now-playing fields and quotes still come only from the supplied samples. The comparison neither calls providers nor reads their present values.

Newsroom submit, capture, current, question and raffle actions are labelled as editorial work. Their response field is not a substitute for the Newsroom document, revisions, synchronous editorial behavior or receipts, so it is not converted. A matching ticker/alert sample also leaves effect mapping, aliases, permission/cooldown enforcement and one-dispatcher source selection to later M1 work. Overlay and both response modes retain their separate effect requirements.

The two account operations share the existing configure/session/Origin/CSRF boundary and bootstrap refusal:

| Operation | Exact request | Result |
| --- | --- | --- |
| `list-command-migration` | Empty object | `state: command_catalogue_only` and the complete `commands` metadata list |
| `preview-command-migration` | `commandId`, expected `snapshot`, and `values` in the supplied-value format above | `state: command_preview_only`, the selected `command` metadata, `status: template_review` and a nested `template` comparison; editorial commands return `status: editorial_contract_required` and a null template |

The catalogue excludes response text, permits at most 1,000 entries and sorts names/IDs by PostgreSQL `C` collation. Overflow or any malformed row refuses the whole list. Preview requires the exact tenant-scoped saved ID and `bot.CommandSnapshotFingerprint`, including the original command timestamps and configuration. An edited source returns 409; a deleted or foreign saved ID returns 404. The browser cannot submit a replacement source. This fingerprint identifies a read snapshot; it is not an atomic database revision or effect permission.

These no-store reads use the existing immutable tenant scope and fresh configure checks. They require the current automation schema but no active controller, runtime, pool or sender. They perform no first-use Newsroom import and change no commands, counters, cooldowns, engine state or jobs. Saved responses retain the command manager's 500-byte bound; the existing conversion/rendering bounds still apply.

Refresh clears the selection. Sample edits cancel the current comparison and clear its result; account/workspace/API/CSRF departure unmounts the private desk and fences late responses. Source text, samples and reports remain in memory and disappear on reload. If the command changed, load the catalogue again and explicitly review its new snapshot. No publication, live selection or automatic migration follows a preview.

Disposable PostgreSQL races cover all actions, modes, roles/cooldowns, aliases, edits/deletion, tenant and membership boundaries, cancellation, whole-list limits and unchanged persisted state. Browser fixtures cover escaped text, stale/malformed responses, sample edits, workspace departure, keyboard access and narrow layout. Run `make verify`, the disposable PostgreSQL gate and browser tests before merge. These fixtures do not establish live provider acceptance or complete M1/Phase #25.
