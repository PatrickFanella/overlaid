# Custodied conduit inspection

[Issue #222](https://git.subcult.tv/subculture-collective/overlaid/issues/222) connects [service app custody](service-app-custody.md) to the existing [read-only conduit inspector](conduit-inspection.md). `appvault.OpenExecution` opens a separate restricted pool; `InspectConduit` returns the existing allowlisted observation. The native [overlaid-conduit-inspect CLI](conduit-inspection-cli.md) invokes this Go boundary with dedicated private configuration. There is no application environment setting, HTTP route, worker or startup caller for it.

## Inputs and permission boundary

Trusted service code supplies a private execution DSN, an immutable versioned keyring, a `Binding` with the exact app UUID, case-sensitive client ID and positive stored revision, and one explicit conduit ID. Obtain the binding from administrative metadata. A missing, revoked or stale binding fails closed; the method does not choose another app, revision or conduit. It exports neither a token accessor nor a generic credential callback.

Migration [0035](../../migrations/0035_service_app_execution.sql) creates `overlaid_app_execution` and adds one SELECT-only active-row policy on `service_app_credentials`. The SELECT policy filters revoked rows; `InspectConduit` enforces expiry separately. The role receives only the reviewed metadata, encrypted token and key-version columns. It cannot select the client-secret column, use `SELECT *`, read administrative receipts, write credentials, or use tenant, bot, queue or pacing data. Existing administrator grants stay separate. Both original/current identities and reachable memberships are checked; combining administrative and execution roles is refused even through a membership that does not inherit privileges.

Constructor and repeatable `Validate` checks reject administrative/owner/create privileges, unrelated private relations or callable security-definer functions, secret-column/write grants, column/table grant options, role membership admin options, policy/constraint/default/column/trigger/rule drift and missing forced RLS. Shared layout validation requires the exact administrative policies and the separate active-row execution policy. These checks are snapshots; stop clients before changing their roles, grants or schema.

## Read sequence

Each inspection has a ten-second cooperative context, shortened by the caller. It begins an explicitly READ COMMITTED read-only transaction, validates the boundary, acquires the same database/schema-scoped administrative lock used by install/revoke, then validates again. Post-lock database time checks the original stored expiry with more than thirty seconds remaining. A waiting caller gains no extra token lifetime.

Only the encrypted token is selected and decrypted. Associated data binds the service-app scope, immutable app ID, exact client ID, revision and token field purpose, together with the recorded AES-256 key version. The private document must have the canonical shape written by `Install`; duplicate/case-aliased/unknown/missing fields, changed identity/revision/times, invalid token bytes and unavailable keys refuse before any provider read. PostgreSQL timestamps have microsecond precision, so private nanosecond times may differ by less than one microsecond; the earlier stored expiry remains authoritative. No decryption or lookup of a client secret occurs.

The existing inspector then validates the exact app identity before and after its bounded GETs for the selected conduit and shard metadata. It retains its request/body/page/count limits, redirect refusal and cancellation behavior. Trusted custom transports must honor context and must not log or retain private request headers. No token issuance, refresh, revocation, conduit write or subscription request occurs.

Before returning, the method revalidates the role/layout, exact record, token ciphertext, key version, timestamps and database-clock expiry. Changed or missing state discards the report. Commit/cancellation failures also return a zero observation, never a partial result. Errors use fixed custody categories and preserve context identity; raw driver/provider errors and private data are excluded.

## Revocation and observations

Library installation, replacement and revocation serialize with active inspection through the shared administrative lock. A successful inspection releases its transaction before a waiting administrative change proceeds. Canceling an inspection releases the lock so revocation can continue. The administrator's existing five-second bound remains in place: a long read can cause a waiting administrative operation to time out, requiring metadata/receipt reconciliation and a later explicit decision. There is no automatic retry, permit refund or fallback to an administrative/bot credential.

The final checks also detect owner-level SQL changes that bypass the library lock. Explicit isolation prevents a login default of REPEATABLE READ from hiding committed changes behind an old snapshot. They do not make provider reads atomic or grant future effect permission. The observation's timestamps describe its provider-read interval, not a durable ownership lease or tenant mapping. The execution role cannot write receipts, and inspection leaves credential ciphertext, revisions and administrative receipts unchanged. It provides no send/badge, assignment, subscription, ingestion or runtime activation permission.

## Upgrade and rollback

Apply migrations 0034 and 0035 through the migration administrator. Provision a dedicated login that inherits only `overlaid_app_execution`, with schema usage and no extra capabilities. Supply key material through the service's private mechanism; this unit adds no configuration loader or production provisioning action.

Stop old administrative clients before applying 0035 and upgrade them with the migration. The older validator expects a single policy on the credential table and refuses the new policy set. Current administrative and execution constructors both require the reviewed expanded layout. Existing credentials, revisions, ciphertext, key versions and receipts are preserved; no data backfill or token rotation is performed.

To roll back use of inspection, stop/close its pool and preserve all records, keys and policies. Use an administrative binary compatible with 0035. Removing policies or downgrading to an older administrative client is a separate reviewed rollback; never broaden an active pool's grants or erase custody data to make a downgrade pass. The default application and existing Twitch transport remain unchanged.

## Qualification and source references

Synthetic HTTP and disposable PostgreSQL fixtures cover successful composition and unchanged custody, role/column/audit/write isolation, original/reachable identity checks, policy/grant/schema drift, original expiry after a lock wait, revocation serialization, cancellation, final report fencing and malformed encrypted documents. Existing administrative and provider inspector fixtures remain required. Run `make verify`, focused uncached races, changed-Go lint and the complete `scripts/ci-postgres.sh` after the shared fixture/migration change. These checks do not establish production migration, live credentials, power-loss durability, conduit ownership or provider/load acceptance.

Provider contracts were refreshed on October 20, 2026: Twitch documents token validation as a GET, identifies app tokens by null user fields, and accepts either OAuth or Bearer authorization prefixes. [Token validation](https://dev.twitch.tv/docs/authentication/validate-tokens/). Conduits require app access tokens, with creation, shard transport assignment, verification and subscription setup as distinct steps. This boundary implements read-only inspection. [Conduit handling](https://dev.twitch.tv/docs/eventsub/handling-conduit-events/).
