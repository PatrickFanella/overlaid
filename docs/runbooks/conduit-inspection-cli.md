# Native custodied conduit inspection

`overlaid-conduit-inspect` makes one explicit [custodied conduit observation](custodied-conduit-inspection.md). It is a native operator command built by `make build`, with offline `--help` and `--version`. It is not included in the API image or selected by application startup. [Issue #224](https://git.subcult.tv/subculture-collective/overlaid/issues/224) records this command's acceptance criteria.

## Prepare private configuration

The target database must already have migrations 0034 and 0035, a dedicated login inheriting only `overlaid_app_execution`, and a currently validated encrypted app credential. The custody constructors verify the role and schema; they refuse administrator, tenant and combined identities. This command does not provision roles, install credentials or acquire/refresh a token. See [service app custody](service-app-custody.md) for the administrative boundary and upgrade requirements.

Supply these values through the operator's private environment mechanism:

| Variable | Value |
| --- | --- |
| `OVERLAID_APP_EXECUTION_DATABASE_URL` | Dedicated restricted execution DSN |
| `OVERLAID_APP_EXECUTION_VAULT_ACTIVE_KEY` | Active version present in the keyring |
| `OVERLAID_APP_EXECUTION_VAULT_KEYS` | Comma-separated `version:base64-key` entries; each key decodes to 32 bytes |

Retain the version that sealed the selected credential. The keyring uses the existing `tenancy.ParseKeyring` format; duplicate versions, absent active versions, malformed base64 and shorter keys are refused. Keys must match the private custody key material. There is no fallback to generic application, tenant or app-admin configuration, and no credential or provider URL flag. Keep DSNs and keys out of arguments, terminal recordings, shared reports and source control. Help and version read none of these values.

## Select the exact stored revision

Use `overlaid-app-admin list` with its separate administrative configuration to obtain the app UUID, case-sensitive client ID and current stored revision. Select one explicit app-owned conduit ID. Replace the placeholders and revision in this example with that selection:

```bash
bin/overlaid-conduit-inspect \
  --app '<lowercase-app-uuid>' \
  --client '<exact-client-id>' \
  --revision 1 \
  --conduit '<selected-conduit-id>'
```

All four flags are required. Unknown, duplicate, missing or positional arguments are refused before reading private configuration or opening a pool. Client/conduit IDs use the existing inspector's bounded identifier grammar. App UUIDs must be lowercase; revisions must be positive signed 64-bit integers. There is no automatic app, revision or conduit selection.

The command opens one restricted pool and invokes `InspectConduit` once. Its ten-second cooperative context includes opening and inspection; a shorter caller deadline or SIGINT/SIGTERM cancels database/provider work. The existing inspector performs bounded validation/inventory GETs, refuses redirects, validates the app identity before and after provider reads, and fences the final credential revision, expiry and role state. The pool closes before success output. No retry, acquisition, mutation, socket or subscription operation is performed.

## Read the result

Success writes one JSON object containing `binding` (`appId`, `clientId`, `revision`) and `observation` (client/conduit IDs, start/completion times, shard count and the existing allowlisted shard metadata). Credentials, key versions, raw provider fields, session IDs, callback URLs and administrative receipts are excluded. The report is a non-atomic observation of the selected conduit, not a readiness verdict, ownership lease, tenant mapping, send/badge grant or future effect permission.

| Exit | Meaning |
| --- | --- |
| `0` | Complete observation written after pool closure |
| `1` | Refused/unavailable operation, close failure or output failure |
| `2` | Invalid arguments or private configuration |
| `130` | Caller interruption observed during the operation |

Diagnostics use fixed text on stderr. Database/provider errors and private input are not printed. Failures before output discard the observation. If stdout reports an error or short write, discard any partial bytes and use the nonzero exit status. A blocked output sink is outside the cooperative operation bound; the command has already closed the pool before writing. Do not treat a lost/partial report as permission for a provider mutation. A later explicit inspection makes a new observation; the command never retries automatically.

## Verification and rollback

CLI fixtures cover input/configuration refusal, exact selection, interruption, close-before-output and output failures. Disposable PostgreSQL fixtures compose real restricted custody with synthetic HTTP and verify successful projection, unchanged credentials/receipts, revoked/stale/wrong-role/invalid-key refusal and cancellation of real SQL waits and local HTTP requests. No real provider credentials or production operations are used in these fixtures. Run `make verify`, focused uncached PostgreSQL races for the CLI/appvault/inspector and changed-Go lint. PR-head CI, merged-main CI, deployment and live-provider acceptance remain separate evidence.

To stop using the command, stop its process and retain existing records, keys and policies. It adds no migration or shared store behavior. Migration 0035's existing stop/upgrade requirement for older app-admin clients still applies. Durable issuance ownership, conduit mutation/reconciliation, runtime activation and live qualification remain open under [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24).
