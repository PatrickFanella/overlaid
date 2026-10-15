# Bot onboarding in Chat

[Issue #66](https://git.subcult.tv/subculture-collective/overlaid/issues/66) connects the retained identity and delegation stores to the Chat page. Managed workspaces can connect tenant-owned Twitch bot accounts, inspect metadata, explicitly select an own/shared bot and revoke authorization or shared consent. Existing commands, moderation and Newsroom remain in the general command registry. The current sender still uses broadcaster credentials; this unit does not activate bot sending, check live moderator status or expand EventSub subscriptions.

Apply migrations through `0012_shared_bot_delegations.sql` before starting this managed revision. Configure the existing Twitch client/secret, registered callback URL and vault keyring. There are no new environment variables or database roles. File-only installations keep broadcaster sending and show the managed-workspace requirement in the identity panel.

The owner selects **Connect or reauthorize a bot** in `/dashboard/chat`. Sign into the intended bot account on Twitch. The authorization-code flow requests `user:read:chat`, `user:write:chat` and `user:bot` and forces fresh authorization, following [Twitch's OAuth parameters](https://dev.twitch.tv/docs/authentication/getting-tokens-oauth/) and [chatbot guide](https://dev.twitch.tv/docs/chat/chatbot-guide/). The provider authorization page determines the signed-in identity; inspect the returned login and subject before selecting it in Over-Laid.

OAuth state is random, stored hashed, browser-bound, valid for ten minutes and consumed once. Its retained purpose binds the owner and initiating session hash to the tenant. Cookie-based callbacks require the same live session and active workspace; a different session for the same account is rejected. Current ownership is checked before token exchange and again before persistence, including the store's transactional owner check. Bound session requests are canceled when the application revokes the session or its expiry timer fires. Self-host bootstrap header initiation uses the existing browser-bound callback mechanism and is limited to the bootstrap tenant.

The callback validates client ID, provider subject/login, positive bounded expiry, refresh token and the required chat scopes returned by Twitch's validation endpoint. It retains only validated scopes, bounds expiry by the shorter exchange/validation lifetime, and saves the bot through the encrypted tenant vault. Authorization does not replace the account session, alter broadcaster credentials or automatically select a bot. Reauthorizing the same bot clears its own-bot binding as documented in the [identity store](bot-accounts.md).

Authenticated control routes are under `/api/tenants/{tenant}/bots`:

- `GET` returns bot metadata, published shared identities, broadcaster connection metadata, retained shared consent, whether that consent is current, owner/configuration flags and the current sender mode. Owners and managers can inspect it; no token or ciphertext is returned.
- `POST /oauth` starts owner-only authorization and returns a provider URL containing only OAuth parameters and opaque state.
- `POST /bind` selects a tenant-owned bot using `botId` and the expected `channelUserId`.
- `DELETE /{botId}` revokes that tenant-owned authorization and removes retained tokens.
- `POST /shared` accepts `botId`, `channelUserId`, `grantRevision`, `catalogRevision`, `revision` and `capabilities` for explicit owner consent.
- `DELETE /shared` uses the expected consent `revision` to revoke only this tenant's delegation.

All mutations use the existing session/CSRF or self-host bootstrap authorization boundary. Shared revision conflicts return 409 and require reload. Capabilities remain allowlisted by the [delegation store](shared-bot-delegation.md). The panel requires confirmation for selection/revocation, clears confirmation on reload, displays stale consent and disables mutations for managers. Provider roles, scopes and entitlements must still be checked by the future sender; saved consent is a snapshot.

Service-administrator OAuth is not exposed by these routes. Service publication still uses the separate restricted administrative vault; the [operator CLI](service-bot-admin.md) supplies its separate OAuth workflow. Production provisioning remains operator work. No service credential is copied to the browser or tenant tables.

Verification separates three proof states: disposable PostgreSQL HTTP/store tests with mocked OAuth; full local build/lint/frontend/race checks; and browser interaction checks using a synthetic local API fixture. Browser fixtures demonstrate panel behavior, not live provider or deployed-runtime acceptance. No real OAuth authorization, provider message, credential provisioning or deployment is performed by the tests.

Rollback uses the existing broadcaster sender and preserves database/keyring backups. The feature adds no schema migration. Earlier revisions ignore the onboarding routes; keep the retained identity/delegation tables and encryption keys. Do not run old connection writers alongside future shared-bot consumers because older writers cannot maintain delegation invalidation.

The unregistered [tenant outbound adapter](tenant-outbound-auth.md) can resolve broadcaster and tenant-owned bot credentials for retained sends. A separate own bot must pass a fresh moderator check; the broadcaster needs an explicit `moderation:read` (or `channel:manage:moderators`) grant. Default broadcaster scopes are unchanged, and the existing sender is not switched by this adapter. Shared service-bot execution remains separate.
