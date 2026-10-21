# Explicit conduit inspection

[Issue #212](https://git.subcult.tv/subculture-collective/overlaid/issues/212) starts P2.5 with `twitchconduit.NewInspector` and `Inspector.Inspect`. This is an explicit read-only library, with no application startup caller, CLI, HTTP route, credential store or provider-write method. Existing tenant user-token WebSockets and retained dispatch owners remain the runtime transport.

## Trusted inputs and app identity

Construction requires an exact expected Twitch client ID, an explicitly selected conduit ID and a trusted token callback. IDs use one to 128 ASCII letters, digits, hyphens or underscores. The callback owns token acquisition, custody, refresh and revocation; the separate [explicit acquisition adapter](app-token-acquisition.md) can supply a freshly validated ephemeral credential. The inspector stores the callback rather than an access token and invokes it afresh once per inspection. It opens no tenant, bot-administrator or bot-execution vault. Do not wire it to a tenant/browser-supplied token or reuse a selected broadcaster/bot identity as app authority.

Each inspection validates its one token against Twitch before inventory access and again before returning. The validation client ID must match exactly. App-token login/user_id fields must be omitted or null; an explicitly empty string is refused. A thirty-second minimum remaining lifetime is required at both validations. Wrong clients, user-shaped tokens, insufficient lifetime and failed validation return a fixed failure with no report. Validation uses exact identity field names and rejects duplicate/case-aliased keys, malformed data and unknown identity response fields. No scopes, tenant consent or effect permission are inferred.

Twitch's [validation reference](https://dev.twitch.tv/docs/authentication/validate-tokens/) documents app-token identity fields; [conduit handling](https://dev.twitch.tv/docs/eventsub/handling-conduit-events/) and [Get Conduits/Get Conduit Shards](https://dev.twitch.tv/docs/api/reference/#get-conduits) require app access tokens. These primary contracts were refreshed October 20, 2026. This library's per-call validation does not implement a future long-lived credential owner's hourly validation or refresh lifecycle.

## Read sequence and limits

Only GET requests to the fixed official validation, conduit-list and conduit-shard endpoints are implemented. The inspector:

1. Validates the token for the configured client.
2. Finds the selected conduit in that app's list.
3. Pages through its shards using the exact selected conduit ID and opaque cursor.
4. Rechecks the selected conduit and its shard count.
5. Revalidates the same token and returns the allowlisted report if the context remains active.

A missing selected conduit stops before shard access. Duplicate conduit/shard IDs, noncanonical/out-of-range shard indices, missing shard coverage, repeated/empty advancing pages, unknown or contradictory transport/status, malformed JSON or changing counts refuse the entire report. No partial data is returned. The reviewed status catalogue follows Twitch's [shard reference](https://dev.twitch.tv/docs/api/reference/#get-conduit-shards). Unassigned/missing/unknown shard shapes do not establish readiness; they require reviewed handling before reconciliation.

Local bounds are ten seconds for the complete cooperative operation, thirty seconds of validated token lifetime, one MiB per response, eight MiB of response bytes in total, 201 shard pages, 100 listed conduits and 20,000 shards. Cursor/field sizes and JSON nesting are bounded. These are local admission limits, not a full-scale provider guarantee. The caller's shorter deadline and shorter configured HTTP timeout still apply. Trusted token callbacks and custom transports must honor context cancellation; the inspector starts no helper goroutine to outlive them.

The HTTP client borrows only the configured transport and a shorter timeout. Cookies, caller redirect hooks and arbitrary endpoint overrides are not borrowed. Redirects are refused, tokens are header-only, and there is no application retry loop, cooldown, mutation or socket creation. Fixed errors exclude callback, transport and provider-body details; caller cancellation/deadline identity is preserved. A throttled or failed report grants no scheduling or retry authority.

## Report meaning and privacy

A successful report contains the configured client/conduit IDs, local start/completion times, selected shard count and numerically sorted shard IDs with reviewed status and transport method. It excludes access tokens, raw WebSocket session IDs, webhook callback URLs/secrets, native provider extras and other conduit IDs. Returned slices are fresh per call; no inventory or authorization cache exists.

The reads are a sequence of observations, not an atomic provider snapshot. An unchanged count does not prove that sessions, grants or bindings stayed unchanged; changes away and back may be invisible. Final token validation cannot prevent later revocation. This report establishes neither local controller ownership, tenant subscription mapping, a lease nor permission to assign a shard, subscribe, ingest or send. Future mutations must freshly authorize and compare their own exact private bindings and durable ownership fences.

## Verification and remaining work

Run `make verify`, uncached `go test -race -count=1 -cover ./internal/twitchconduit` from `backend/`, and changed-Go lint. Synthetic HTTP fixtures cover identity checks before/after reads, exact selected-conduit access, pagination/count drift, completeness and bounds, private-field omission, no retries/writes, redirect refusal, actual request cancellation, callback/transport failures and concurrent independent reads. No real provider credentials or requests are used. This isolated package changes no application persistence, queue, controller or startup path; its issue specifies these checks without a new PostgreSQL qualification run.

Next add app-token custody and durable conduit ownership, then one-use mutation/uncertainty receipts, shard sessions and tenant subscription reconciliation. Preserve the current transport until a coordinated single-owner cutover is qualified. Operator grants, live migration/recovery and broad load/onboarding acceptance remain under the [roadmap](../superpowers/plans/2026-10-15-remaining-work-roadmap.md) and #24.

The explicit [custodied inspection boundary](custodied-conduit-inspection.md) composes this inspector with a separate token-column-only role, exact stored app binding and read-only administrative lock. It adds no startup caller or conduit mutation permission.

The separate [creation transport](conduit-creation.md) validates app identity around one explicit non-replayable POST. It has no application caller or durable ownership; the inspector remains read-only.
