# Explicit app-token acquisition

[Issue #214](https://git.subcult.tv/subculture-collective/overlaid/issues/214) adds `twitchconduit.NewAppAuthorizer` and `Acquire` as the provider adapter needed before app credential custody. It has no route, CLI, startup caller, vault, cache, automatic refresh or conduit-write operation. Existing broadcaster/own/shared-bot user-token paths remain separate.

## Inputs and provider calls

Trusted service configuration supplies one exact client ID, a cancellation-aware client-secret callback and optionally a trusted HTTP transport/shorter timeout. The authorizer stores the callback rather than the secret and calls it once per explicit acquisition. Client IDs use the inspector's bounded ASCII identifier policy; secrets/tokens must be nonempty visible ASCII without spaces or control bytes, up to 4096 bytes. Do not provide browser/tenant-owned secrets or substitute a selected bot credential.

The adapter sends one POST to the fixed `https://id.twitch.tv/oauth2/token` endpoint. Its form body contains only `client_id`, `client_secret` and `grant_type=client_credentials`; no scopes are requested and secrets never enter the URL. The request has no replayable body or idempotency key. Redirects/cookies are refused, caller redirect hooks are not borrowed and there is no application retry loop. A custom transport must honor context and must not replay the POST.

A successful bounded issuance response must contain only the reviewed access_token, expires_in and bearer token_type fields. Duplicate/case-aliased keys, user-shaped additions, malformed values, excessive sizes/depth and duration overflow fail closed. The same exact app-client validator used by the [conduit inspector](conduit-inspection.md) then checks the token against Twitch: exact expected client, null/omitted user identity and sufficient lifetime. The original issuance request time and validation request time independently bound expiry; the credential uses their earlier expiry with at least thirty seconds remaining at return. Its lifetime check keeps the local monotonic clock where available.

The complete cooperative operation has a ten-second deadline, with the caller's shorter deadline/HTTP timeout preserved. Issuance bodies are capped at 64 KiB; validation uses the inspector's one-MiB response/eight-MiB budget and bounded JSON decoder. Trusted callbacks/transports must honor cancellation; no helper goroutine outlives them. No retry, refresh, fallback or revoke request is made.

These contracts were refreshed October 20, 2026 against Twitch's [client credentials flow](https://dev.twitch.tv/docs/authentication/getting-tokens-oauth/#client-credentials-grant-flow) and [token validation reference](https://dev.twitch.tv/docs/authentication/validate-tokens/). A long-lived custody owner still needs its own rotation, revocation and periodic validation lifecycle.

## Outcomes and uncertainty

| Outcome | Meaning |
| --- | --- |
| `not_started` | Local configuration, secret acquisition or cancellation refused before a token request might start. No credential is returned. |
| `refused` | The token endpoint returned reviewed 400/401/403 authorization refusal or 429 throttling. No credential is returned. |
| `uncertain` | A POST may have reached the provider, but transport/response/redirect/validation/lifetime/panic/cancellation prevented a usable credential. `errors.Is(err, ErrAcquisitionUncertain)` is true. No partial credential is returned. |
| `issued` | One issuance response passed exact app identity and conservative lifetime checks. A private ephemeral credential is returned. |

An error after a POST may mean Twitch minted a token. It does not establish that nothing happened and must not become an automatic acquisition retry. Cancellation identity is preserved alongside uncertainty. Fixed error values exclude provider bodies, secret-source errors and transport/panic details. This unit does not record durable issuance/retry intent or reconcile unknown minted tokens; that belongs with the custody owner before activation.

## Credential handling

`AppCredential` keeps its token behind a private accessor closure and its identity/lifetime fields private. This also prevents ordinary formatting of a nested private wrapper from traversing a token string field. JSON serializes only clientId, validatedAt and expiresAt; ordinary/debug formatting is redacted. The credential rejects JSON decoding; metadata serialization cannot construct a usable credential. `Metadata` returns a copy. These are accidental-disclosure controls, not memory isolation or secure zeroization.

`Token(ctx)` is the explicit plaintext accessor for trusted custody or inspection. It rejects canceled contexts, empty/invalid credentials and less than thirty seconds of remaining lifetime. It does not make a fresh provider call or establish current consent/ownership. The existing inspector can use it as its token callback and still validates before and after inventory reads. Later revocation or grant changes remain possible; every future consumer must recheck its own current authority. A validated app token grants no tenant mapping, ingestion, send/badge, mutation or startup permission.

## Verification and remaining work

Run `make verify`, uncached `go test -race -count=1 -cover ./internal/twitchconduit` from `backend/`, and changed-Go lint. Synthetic HTTP fixtures cover exact body/header binding, one POST with no replay body, identity/type/expiry, redacted JSON/formatting, metadata-only reconstruction refusal, conservative lifetimes, refusal/uncertainty, redirect and actual cancellation, private read/transport/panic failures, token access guards and acquisition-to-inspector composition. Fixtures use no real secrets or provider requests.

This isolated provider adapter changes no persistence, worker, queue, controller or startup path; its issue specifies package races and repository verification without a new full PostgreSQL qualification run. Next add separate app credential custody with revisions, rotation/revocation and durable issuance ownership, then conduit mutation receipts, shard/subscription reconciliation and a coordinated single-owner cutover. Keep the current transport recoverable and live/load/onboarding acceptance separate under the [roadmap](../superpowers/plans/2026-10-15-remaining-work-roadmap.md).

The separate [administrative custody store](service-app-custody.md) accepts the typed credential plus its exact acquisition secret through a private binding proof. This preserves the original token lifetime and supplies no durable issuance intent, plaintext resolver or automatic retry.
