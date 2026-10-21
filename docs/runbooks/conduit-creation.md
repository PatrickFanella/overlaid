# Explicit conduit creation transport

[Issue #232](https://git.subcult.tv/subculture-collective/overlaid/issues/232) adds `twitchconduit.NewCreator` and `Creator.Create` as an unregistered provider adapter. A trusted caller supplies an exact expected app client, a context-aware token source and a shard count. The adapter validates the app, makes at most one creation POST and validates the same app token again before returning metadata.

There is no application caller, CLI, HTTP route, credential acquisition, refresh, persistence or startup registration. A future durable owner must record the original intent and commit its one-use claim before invoking this adapter. Repeated calls are independent provider attempts; the adapter provides no idempotency or deduplication. Never automatically repeat an uncertain attempt.

## Inputs, identity and bounds

Client IDs contain one to 128 ASCII letters, digits, underscores or hyphens. The required token source is trusted service configuration and must protect its plaintext and honor cancellation. It is called once per operation. Invalid shard counts outside 1..20,000, missing configuration, nil contexts and canceled inputs refuse before provider work. Tokens must be bounded visible ASCII. Tenant/browser tokens and selected broadcaster/bot credentials cannot establish app authority.

The existing [validation contract](conduit-inspection.md#trusted-inputs-and-app-identity) requires an exact client match and omitted/null user identity fields. Both validations must succeed, and the original validation's expiry must still leave more than thirty seconds at completion. A later validation cannot extend that original lifetime. These observations cannot prevent later revocation or prove durable local ownership.

One cooperative ten-second context bounds token access, both validation reads and creation. A shorter caller deadline or configured HTTP timeout applies. The adapter borrows only the supplied HTTP client's transport and a shorter timeout; it does not borrow cookies or redirect hooks. Trusted callbacks/transports must honor cancellation. There are no helper goroutines, retries, credential cache or automatic refresh.

The creation body is exactly `{"shard_count":N}` at the fixed `https://api.twitch.tv/helix/eventsub/conduits` endpoint, with header-only Bearer authorization and the expected Client-Id. The request has no replay body or idempotency key. The creation response is bounded to 64 KiB, validation responses retain the inspector's one-MiB limit, and all response bytes share an eight-MiB budget. JSON depth is bounded to 32, duplicate/case-aliased keys and trailing JSON are refused, and exact required field names are mandatory. Exactly one returned conduit must have a canonical bounded ID and the requested shard count. Bounded unknown creation fields are discarded; identity fields remain strictly allowlisted.

## Outcomes and reconciliation

| Outcome | Meaning | Returned conduit |
| --- | --- | --- |
| `not_started` | Input, token access or initial identity validation failed before the creation attempt. | None |
| `refused` | The creation endpoint returned a documented 400, 401 or 429 refusal. | None |
| `uncertain` | The request may have started and complete acknowledgement/identity validation could not be established. | None |
| `created` | One valid response and final app validation completed within the original bounds. | Metadata only |

400 maps to `ErrInvalid`, 401 to `ErrUnauthorized`, and 429 to `ErrCreationLimited`. The conduit-limit response conveys no timed retry permission. Other statuses, redirects, private transport/read errors, malformed or mismatched acknowledgements, final identity failure, cancellation and panics after the attempt produce `ErrCreationUncertain` with a fixed cause. Caller cancellation/deadline identity is retained. Raw callback/provider errors and bodies are excluded. Even an apparently successful response is uncertain if final validation fails or response cleanup panics.

Successful metadata contains only the exact client ID, returned conduit ID, requested shard count and local attempt start/completion times. It excludes the token, raw response and private provider extras. This is provider acknowledgement, with no database receipt, owned conduit, assigned transport, subscription, ingestion or send permission. Failure metadata cannot be used to infer that an unacknowledged conduit does not exist. Retain the original request for reconciliation rather than creating a replacement key.

## Provider sources and qualification

Primary contracts were refreshed October 20, 2026. Twitch's [Create Conduits reference](https://dev.twitch.tv/docs/api/reference/#create-conduits) specifies app-token authorization, the POST body/result and documented response codes. [Conduit setup and limits](https://dev.twitch.tv/docs/eventsub/handling-conduit-events/) supplies the current 20,000-shard ceiling and separates creation, transport assignment, shard verification and subscription installation. [Token validation](https://dev.twitch.tv/docs/authentication/validate-tokens/) supplies the identity endpoint. This adapter's bounds and conservative uncertainty rules are local implementation contracts.

Run `make verify`, uncached `go test -race -count=1 -cover ./internal/twitchconduit` from `backend/`, changed-Go lint and documentation link/whitespace checks. Synthetic fixtures cover exact requests, boundary counts, pre/post identity and original lifetime, malformed/oversized acknowledgements, fixed refusals, private failures, panic cleanup, actual HTTP cancellation and redirect refusal. They use no real credentials or provider effects. This transport-only unit changes no schema, database role, worker or runtime path; its issue requires no new complete PostgreSQL qualification run.

The separate [durable ownership ledger](conduit-ownership.md) retains original creation intent, one-use claims and unresolved ownership with no credential/provider access. Fresh custody-bound execution must compose these boundaries before application invocation. Reconciliation, shard sessions, tenant subscriptions and a coordinated single-owner transport cutover remain under [the roadmap](../superpowers/plans/2026-10-15-remaining-work-roadmap.md). Existing [app issuance uncertainty](service-app-issuance.md) remains protected; this adapter neither resolves nor clears it. The [decision trail](../superpowers/plans/2026-10-20-conduit-creation-decisions.tsv) records this unit's scope and qualification.
