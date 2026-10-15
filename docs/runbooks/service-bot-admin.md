# Shared service bot administration

[Issue #68](https://git.subcult.tv/subculture-collective/overlaid/issues/68) adds `overlaid-bot-admin`, a dedicated operator CLI for the [service vault](service-bot-vault.md). `make build` produces `bin/overlaid-bot-admin`. It is not installed in the application container or mounted on tenant HTTP routes. The plan-assignment `overlaid-admin` command and its administrator DSN remain separate.

Use the restricted bot administrator login described in the vault runbook and apply migrations through 0012. This change provisions no production login or secrets. The CLI reads only `OVERLAID_BOT_ADMIN_DATABASE_URL` for its database; it never falls back to `DATABASE_URL`, worker credentials or `OVERLAID_ADMIN_DATABASE_URL`. Role/schema validation runs before OAuth starts. Do not give this login tenant-table or migration-owner privileges.

Metadata operations write JSON to stdout; instructions and fixed errors go to stderr. Secrets must be supplied through the existing private environment/secret delivery, never command arguments or shell history. Read operations need only the restricted DSN. OAuth also needs `TWITCH_CLIENT_ID`, `TWITCH_CLIENT_SECRET`, `VAULT_ACTIVE_KEY` and `VAULT_KEYS`. Use the same client and keyring intended for service execution.

```bash
bin/overlaid-bot-admin list
bin/overlaid-bot-admin catalog
bin/overlaid-bot-admin receipts --limit 20
```

To authorize a service bot, first identify its immutable Twitch user ID. Register the exact loopback callback URI on the Twitch application as an operator/provider setup action. This CLI does not modify application registrations. The flow follows Twitch's [authorization-code requirements](https://dev.twitch.tv/docs/authentication/getting-tokens-oauth/#authorization-code-grant-flow) and [token validation contract](https://dev.twitch.tv/docs/authentication/validate-tokens/).

```bash
bin/overlaid-bot-admin authorize --expect-user-id BOT_USER_ID --revision 0 \
  --redirect-url http://127.0.0.1:41925/oauth/callback
```

Use revision 0 only for a new subject. For reauthorization, read the current account revision and supply it explicitly. The command binds an IPv4 loopback listener and prints its `/oauth/start` URL. Open that URL in the browser which will authorize the intended bot account. The start route sets an HttpOnly SameSite=Lax cookie and redirects to Twitch with random state, fresh consent and `user:read:chat`, `user:write:chat`, `user:bot` scopes. The callback must return to the same browser. The expected subject check prevents accidentally saving a different signed-in Twitch identity.

Only `http://127.0.0.1:PORT/oauth/callback` with an explicit nonzero port is accepted. Host header, loopback peer, path, method, state, browser cookie, duplicate code/state parameters and bounded URI/body sizes are checked. The state lives only in the command process and is consumed once. Default timeout is ten minutes; `--timeout` can shorten it. Ctrl-C/SIGTERM stops the command, closes the listener and cancels pending requests. For a remote operator, forward the same registered loopback port through an authenticated SSH tunnel and use the browser-side loopback URL; no public listener or new hosted callback is required.

Provider calls have fixed HTTPS destinations, ten-second request timeouts, a thirty-second callback budget and no redirect following. Responses are limited to 64 KiB. Before saving, the flow validates the configured client, exact expected subject, required validation-endpoint scopes, token type, refresh token and positive bounded expiry. The shorter exchange/validation lifetime is retained. Tokens, client secrets, authorization codes, DSNs and raw provider/driver errors are excluded from CLI and callback output. No pending state or plaintext credential file is written.

A successful command prints the committed service account metadata and grant revision. It does not publish the bot, assign it to a tenant or activate sending. Reauthorization makes previous publication unavailable through the existing store contract. Publication is a separate operator action:

```bash
bin/overlaid-bot-admin publish --bot BOT_UUID --revision GRANT_REVISION --catalog-revision 0
bin/overlaid-bot-admin unpublish --bot BOT_UUID --revision GRANT_REVISION --catalog-revision CATALOG_REVISION
bin/overlaid-bot-admin revoke --bot BOT_UUID --revision GRANT_REVISION
```

Catalogue revision 0 means first publication only. Reload catalogue metadata for later publish/unpublish actions. Stale revisions fail. Each successful mutation uses the vault's atomic fixed-field receipt. Tenant owners subsequently approve consent in Chat; service administrators do not read or write tenant delegations.

Exit 0 means metadata was emitted successfully, 2 means invalid arguments/configuration before an operation, 1 means an operational/output failure, and 130 means cancellation. If interruption or stdout failure occurs after persistence, the credential may already be committed: inspect account metadata and receipts before retrying. If provider authorization succeeds but validation or database persistence fails, no credential receipt is reported; the command does not claim to revoke an already-issued provider grant. Correct the issue and inspect provider/account state before starting another authorization.

Tests use mocked provider HTTP and disposable PostgreSQL role/schema fixtures. They cover callback binding/replay, provider failures, expiry/cancellation, secret-free output, restricted-role rejection, persistence and explicit publication. They do not perform real OAuth, send provider messages, register callbacks or provision credentials. Live authorization and sender/release qualification remain separate operator work. For rollback, stop the CLI and retain the database/keyring backups; no schema change is added here.
