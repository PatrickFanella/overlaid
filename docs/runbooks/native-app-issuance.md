# Native app issuance

`overlaid-app-issue` explicitly prepares or executes one [durable app issuance](service-app-issuance.md). It is built by `make build` and requires a database compatible with migration 0036. No application startup, route, controller or API image includes this command. [Issue #230](https://git.subcult.tv/subculture-collective/overlaid/issues/230) records its acceptance criteria.

## Private configuration

Supply these values through the operator's private environment mechanism. Do not put credential values in arguments, recordings, shell history or source control.

| Variable | Used by | Value |
| --- | --- | --- |
| `OVERLAID_APP_ISSUANCE_DATABASE_URL` | Both steps | Dedicated restricted `overlaid_app_admin` login |
| `OVERLAID_APP_ISSUANCE_CLIENT_SECRET` | Both steps | Exact secret for the selected case-sensitive client ID |
| `OVERLAID_APP_ISSUANCE_VAULT_ACTIVE_KEY` | Execute | Active sealing version present in the keyring |
| `OVERLAID_APP_ISSUANCE_VAULT_KEYS` | Execute | Comma-separated `version:base64-key` entries, each decoding to 32 bytes |

Preparation reads no keyring. Execution uses the existing versioned AES-256 parser. The command has no private-input or provider URL flags, generic configuration fallback, prompt or automatic identity selection. The store validates the original/current/reachable database identities and exact private layout before work. Keep keys immutable while the operation runs and preserve retained versions for recovery.

## Prepare and save the original request

Read the current custody metadata with the separately configured `overlaid-app-admin list`. Use revision zero for a new client, or the exact observed revision to replace/reactivate an existing record. Revision values must be between zero and 9223372036854775806.

```bash
bin/overlaid-app-issue prepare --client '<exact-client-id>' --revision 0
```

Preparation calls `PrepareIssuance` once. It makes no provider request. Save the returned original request UUID before executing; it carries the original five-minute deadline. Repeated preparation of the same active binding returns that original metadata and cannot extend the deadline. Started/uncertain ownership blocks fresh preparation and returns the original metadata with nonzero status. Preparation may perform the store's documented expiry/terminal-retention maintenance; it is not a read-only status lookup.

## Execute the exact request

```bash
bin/overlaid-app-issue execute \
  --request '<original-lowercase-request-uuid>' \
  --client '<same-exact-client-id>' \
  --revision 0
```

Keep the client, revision and private secret bound to the original preparation. All required flags are explicit; missing, duplicate, unknown, mixed or positional arguments fail before private configuration/pool access. App/client grammar and lowercase request UUID checks also precede configuration. `--help`, command help and `--version` work offline.

Execution calls `Issue` once. Its existing committed one-use claim precedes the bounded acquisition adapter; no provider request runs under an administrative transaction/lock. Successful encrypted installation, custody audit and issued result commit together. Duplicate issued requests return original metadata without another token POST. Missing, expired, stale, revoked-by-revision or unresolved selections never gain replacement request IDs. This command adds no refresh, provider revocation, ownership clearing or replay mechanism.

## Output, interruption and reconciliation

The pool closes before stdout. When the store returns known original metadata and closure succeeds, stdout contains one JSON object with an `issuance` field, including on a nonzero operation status. Fixed diagnostics go to stderr and contain no private input or raw database/provider error.

| Exit | Meaning |
| --- | --- |
| `0` | A prepared result or issued result was returned for the exact successful selection |
| `1` | Refused, unavailable, unresolved, malformed success, close failure or output failure |
| `2` | Invalid arguments/private configuration; no pool operation |
| `130` | Caller interruption observed; known original metadata may still be returned |

Nonzero status never permits a retry. Preserve any returned original ID and inspect it with `overlaid-app-admin issuance --request UUID`, using that command's separate private administrator configuration. A metadata result with `started` or `uncertain` remains unresolved. Do not prepare a new request to replace it. If output fails or writes only partial bytes, discard those bytes and reconcile the original request; a pool-close failure suppresses metadata output. Missing metadata is not evidence that no token was minted.

Preparation has one five-second cooperative open/operation context. Execution has one twenty-second open/operation context, preserving `Issue`'s internal fifteen-second bound and at most five seconds of detached failure bookkeeping after interruption. The caller can shorten these contexts; SIGINT/SIGTERM cancels the operation. A blocked output sink is outside those bounds. Custom transports used in fixtures must honor cancellation and protect request secrets; the native command uses the existing fixed provider endpoints.

## Qualification and rollback

Run `make verify`, focused disposable PostgreSQL uncached races, changed-Go lint and native offline/input smoke checks. Fixtures compose the real restricted store with synthetic HTTP, cover duplicate/uncertain no-replay behavior, exact selection/configuration, original expiry, private fields, cancellation of an actual local HTTP request and retained failure metadata. No real provider credential or production operation is used. Hosted PR-head CI, merged-main CI, deployment and live acceptance remain separate.

Stop using the command to roll back its use. Preserve every request ID, claim, unresolved state, ciphertext and key; use binaries compatible with the installed migration. The existing metadata/revocation administrator command keeps its separate boundary. Explicit unresolved-ownership resolution, conduit ownership/mutation/reconciliation, runtime cutover and live/load/onboarding qualification remain under [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24).
