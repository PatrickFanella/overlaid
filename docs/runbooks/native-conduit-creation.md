# Native conduit creation workflow

[Issue #238](https://git.subcult.tv/subculture-collective/overlaid/issues/238) adds `overlaid-conduit-create`, a separate native caller of the [ownership ledger](conduit-ownership.md) and [custody-bound creation API](custodied-conduit-creation.md). Its explicit steps are `prepare`, `status` and `execute`. No application startup, HTTP/UI route or API-image command is added.

## Prerequisites and private configuration

Apply migrations 0034 through 0037 through the migration administrator and provision separate restricted logins for `overlaid_conduit_admin` and `overlaid_app_execution`. Use the existing [service app issuance](native-app-issuance.md) workflow to install an app credential, then observe its exact app UUID/client/revision through administrative metadata. This command never acquires, refreshes or revokes credentials and never reads a client secret.

Deliver connection strings and the retained AES-256 keyring through the operator's private configuration mechanism. Do not put credentials in shell arguments, shared examples, logs or committed files. The command accepts no credential, key, arbitrary provider URL or fallback flags and does not prompt.

| Environment | Required by | Purpose |
| --- | --- | --- |
| `OVERLAID_CONDUIT_DATABASE_URL` | All commands | Dedicated metadata-only ledger login |
| `OVERLAID_CONDUIT_EXECUTION_DATABASE_URL` | `execute` | Separate token-column-only custody login |
| `OVERLAID_CONDUIT_VAULT_ACTIVE_KEY` | `execute` | Valid retained keyring version |
| `OVERLAID_CONDUIT_VAULT_KEYS` | `execute` | Comma-separated `version:base64` AES-256 keys, including the stored token version |

Preparation/status read only the ledger DSN. They do not read keyring or custody configuration, open a custody pool or contact a provider. Execution validates both separate pools through the existing APIs; their database/schema/server identity must match. A configured keyring cannot replace a revoked, expired or mismatched credential revision.

## Explicit original-request steps

Use exact observed identities in place of these example UUIDs/client. Help and version require no configuration and open no pool.

```bash
bin/overlaid-conduit-create --help
bin/overlaid-conduit-create --version

bin/overlaid-conduit-create prepare \
  --app 11111111-1111-4111-8111-111111111111 \
  --client app-client --revision 1 --shards 1

bin/overlaid-conduit-create status \
  --request 22222222-2222-4222-8222-222222222222

bin/overlaid-conduit-create execute \
  --request 22222222-2222-4222-8222-222222222222 \
  --app 11111111-1111-4111-8111-111111111111 \
  --client app-client --revision 1 --shards 1
```

`prepare` saves a trusted app/client/revision/shard snapshot under a database-generated request UUID and fixed deadline. It does not establish current credential authorization. Save its returned original request ID before execution. Exact duplicates preserve the original metadata; an already-created result returns its existing identity. Conflicts fail. An unresolved owner for the same exact client may return its original metadata with nonzero status even if local custody selection changed. Never turn that response into a replacement request.

`status` reads only the selected original request. Missing/purged status cannot create work. Exit zero means the lookup succeeded; `started` or `uncertain` still means unresolved ownership. Reading status grants no creation, retry or effect permission.

`execute` requires the original request UUID and exact full binding/count. It calls `CreateConduit` once, preserving its committed claim, custody lock/rechecks, original database deadline, one provider attempt and detached failure completion. Only a created result is success. Duplicate/restarted execution reads retained results or refuses unresolved work without replay. No automatic request ID, replacement intent, retry, refresh or ownership clearing exists.

Unknown, mixed, duplicate, missing or positional arguments fail before environment/pool access. Request/app UUIDs must be lowercase, revision positive and shards in 1..20,000. Preparation accepts no request flag; status accepts no app/client/revision/shard flags.

## Output, exit status and recovery

stdout contains one JSON object with an `intent` property when validated original metadata is known. It includes the original binding/count/state, fixed result fields, database principal and original timestamps. Claims, tokens, ciphertext, secret/key configuration and raw provider/database errors are excluded. Returned metadata is checked for required fields, fixed state/failure vocabulary, exact original deadline and command-specific scope before output. Both pools close before JSON. A close failure suppresses output; output failure requires discarding partial bytes.

| Exit | Meaning |
| --- | --- |
| `0` | Prepared/existing-created result, successful status lookup, or created execution |
| `1` | Refused, unavailable, unresolved, malformed result, close or output failure |
| `2` | Invalid arguments or missing/invalid dedicated private configuration |
| `130` | Caller interruption |

Known original metadata may accompany a nonzero operation status. Inspect its state and retain its original ID; metadata never grants replay. Missing output, interrupted output or a close error does not prove that creation did not occur. Use `status` with the saved original ID and preserve unresolved ownership. Do not prepare a new request automatically after errors, reloads or timeouts.

Preparation/status have a five-second cooperative combined open/operation bound. Execution has twenty seconds for combined opens/operation, plus the API's at-most-five-second detached failure bookkeeping. The provider adapter and original ledger deadline may shorten that time. A blocked output sink lies outside those operation bounds. Signal handling forwards interruption through the same context.

## Qualification and remaining work

`make build` includes the native binary; `make verify` builds/tests it with the application. Issue #238 additionally requires uncached disposable-PostgreSQL races for the new CLI and existing custody/ownership/transport packages, changed-Go lint, documentation/whitespace checks and actual binary offline help/version/invalid-input smoke checks. Fixtures cover private configuration isolation, argument/result validation, close/output order, real prepare/status/execute, uncertainty, restart/no replay, role/binding/key/deadline refusal, unchanged custody/audit and actual HTTP cancellation. No real provider request is used.

This command-only unit changes no schema, grants or shared store behavior. PR #237's complete PostgreSQL gate remains separate evidence; #238 requires no new complete local gate. Hosted exact-head CI still must pass before merge. Rollback stops native callers while preserving ownership records, credentials and all retained key versions. Provider provisioning/recovery, shard/session/subscription reconciliation, coordinated runtime cutover and broad live/load/onboarding acceptance remain under [the roadmap](../superpowers/plans/2026-10-15-remaining-work-roadmap.md). The [CLI decision trail](../superpowers/plans/2026-10-20-conduit-operator-decisions.tsv) records qualification.
