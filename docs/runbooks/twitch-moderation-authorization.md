# Twitch moderation authorization

Tracking: [#99](https://git.subcult.tv/subculture-collective/overlaid/issues/99), part of P2.2 under [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24), following merged transport PR #98. `twitchoutbound.Adapter.AuthorizeModeration` is a trusted callback for the [explicit moderation executor](moderation-executor.md). It adds no startup client, endpoint pacer, ledger execution or existing-workflow cutover.

## Identity and consent

The callback takes private `platformmoderation.Target` input: tenant, connection, original deadline and exact moderation action. The executor derives these fields from the retained ledger and compares the job deadline with the original ledger deadline. This helper does not read or claim that ledger. Do not put the target, action reason or callback into jobs, exports or logs.

Credential resolution uses the same custody helper as chat sending. The current selection chooses the broadcaster, explicitly bound own bot, or published shared bot; an unavailable selected identity never falls back. OAuth validation checks the configured client and exact selected subject on every authorization. Refresh uses the existing tenant/account locks and vault boundaries; a refresh-only update retains selection revisions.

Delete requires `moderator:manage:chat_messages`; timeout/ban/unban require `moderator:manage:banned_users`. A chat-send grant cannot authorize moderation. Separate bot identities require a fresh moderator lookup using the broadcaster's existing moderation-read permission. Shared execution requires the explicitly supplied restricted execution store, publication and exact grant/catalogue/delegation revisions, capability-specific tenant consent, and the existing user:bot policy. The result advertises only the requested moderation capability.

## Invocation fence

Authorization returns the validated configured client ID, moderator ID, binding fingerprint, a capability report and an exact-action callback. The callback captures identity/binding metadata rather than the initially resolved token. It refuses changes to action ID, channel, operation, target, message, reason or timeout duration. The future executor must still compare client/moderator/binding and capability before and after its durable claim.

Invocation freshly resolves and validates credentials, required scope, role, entitlement and shared consent again, compares identity/binding, reserves effect usage, and checks the current local selection immediately before [one bounded transport attempt](twitch-moderation.md). No database lock is held across the effect. Provider authorization and local snapshots can change afterward; this does not create an atomic transaction spanning Twitch and the database.

Authorization and invocation each use a ten-second bound shortened by the original deadline and initiating context. Errors contain fixed strings only. Confirmed denials produce rejected/unauthorized receipts; preparation unavailability before a provider attempt produces rejected/retryable metadata. Neither classification permits replay after a durable claim. Transport uncertainty retains the typed transport result.

This callback is not one-use storage. Calling it does not acquire endpoint capacity or establish a durable claim; the future executor must provide both and retain every post-claim outcome without automatic replay. Existing chat authorization still supports only chat.send/reply and continues to use its original scope, error and send behavior.

## Verification and remaining work

Disposable PostgreSQL and synthetic HTTP fixtures cover all four operations through broadcaster, own-bot and shared-bot identities; exact scopes and payloads; fresh role/grant checks; independent consent/publication withdrawal; binding cycles; broadcaster reauthorization; plan denial; bounded provider failures; cancellation/expiry; and refresh across an already authorized callback. Existing chat tests run alongside the moderation cases. No real Twitch effects or credentials are used.

Run `make verify` and `bash scripts/ci-postgres.sh`, including both uncached race suites. No migration or persistent-data rollback is introduced. The [moderation pacer](moderation-pacing.md) and [fenced executor](moderation-executor.md) are implemented without startup registration. The [dedicated worker](moderation-worker.md) is explicitly constructed. The separate [configured consumer](moderation-consumer.md#configured-opt-in) registers the dedicated fair queue/pacing path only under explicit opt-in; construction alone does not activate it. Live acceptance remains under #39.
