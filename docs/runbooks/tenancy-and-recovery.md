# Tenancy migration and recovery

Phase 1 uses one PostgreSQL database with forced row-level security and immutable application scopes. `DATABASE_URL` must authenticate as a non-owner, non-superuser role without RLS bypass. Application startup rejects unsafe roles and missing migrations. `overlaid_app` inherits the runtime permissions; administrator credentials are reserved for schema changes and private operator actions.

## New installation

1. Copy `.env.example` to a private `.env`. Set distinct, URL-safe administrator and runtime passwords. Use `DEPLOYMENT_MODE=selfhost` for a private operator installation or `hosted` for account-owned workspaces.
2. Generate a private 32-byte base64 vault key. Set `VAULT_ACTIVE_KEY=v1` and `VAULT_KEYS=v1:<key>` before connecting Twitch. Hosted mode always requires a keyring. Retain a private, recoverable copy.
3. Set `PUBLIC_WEB_URL` and the Twitch registered callback. Account cookies require HTTPS outside explicit loopback local development. A production frontend must be built with its public API origin; the homelab deployment uses the same HTTPS origin for both.
4. Start Compose. A fresh PostgreSQL volume runs every checked-in `migrations/*.sql` file in filename order, followed by the runtime-role initializer, before the API starts. Existing volumes require the explicit migration procedure below; initialization does not rerun on rebuild. The seeded self-host workspace is `00000000-0000-4000-8000-000000000001`; its plan remains unlimited.
5. Probe `/readyz` by parsing JSON. Required managed checks are `process`, `tenant_storage`, `retention_worker` and `access_policy`. `/healthz` proves liveness only. Verify operator authentication or account sign-in before adding providers.

## Existing installation cutover

1. Record the running revision, images, mounts, database version, row counts and credential-file permissions without printing secrets. Freeze traffic and stop old API/background writers; preserve the old release and environment.
2. Take a private custom-format database backup, the complete `api-data` volume, environment/keyring and exact source migration files. Verify hashes and independently restore the backup before cutover. A successful dump is insufficient.
3. Apply pending SQL with `make db-migrate`, using the Compose PostgreSQL administrator connection. The migration ledger prevents pre-tenancy DDL from being replayed after tenant-scoped names exist. Directly reapplying 0003 is supported for recovery; do not replay old migrations against a shared tenant database.
4. Switch `DATABASE_URL` to `overlaid_app` with its separate password. Start the new API. Existing product IDs/timestamps are assigned to the stable self-host tenant. Legacy retained-state, history, interaction-rule and Twitch token files are imported once; failures stop startup. Original files remain for rollback.
5. Verify row counts, representative command/config/session reads and keyed SSE. Then verify two tenants with identical command names, guessed-ID rejection and timer execution using disposable synthetic accounts. Do not copy production tokens into test fixtures.

## Roles and sessions

Owners control membership, ownership, retention, export and deletion. Managers can manage editor/moderator membership, connections and credentials but cannot edit owners or other managers. Editors configure product data; moderators read permitted moderation evidence and perform moderation. Ownership transfer is explicit, atomic and audited. Members must first sign in and provide their immutable application account ID; an email match never grants access.

Sessions are hashed, expire within 24 hours and can be revoked individually or for an account. CSRF and origin checks apply to browser mutations. Sign-out cancels active session-bound streams. Overlay credentials keep OBS streams independent of browser sign-out; rotation cancels credential-bound streams and invalidates old URLs. Multi-process immediate stream cancellation is not qualified by this single-API release; every new request rechecks persistent session/membership state.

## Token custody and plan changes

AES-GCM authenticated data binds tokens to tenant, provider and key version. Refresh locks the connection row to serialize API processes. Reads re-encrypt under the active key. Keep every old key until all live connection rows use the new version and their backups can still be decrypted. Removing a required key causes connection access to fail closed. Disconnecting deletes local token custody and stops the tenant runtime; users can also revoke the application grant in Twitch.

Twitch tokens are validated for immutable subject and application binding, and at least hourly while connected. Provider/browser consent and real EventSub delivery still require live credentials and separate acceptance evidence. Connecting Twitch uses broader scopes than account sign-in.

Assign a plan with `bin/overlaid-admin --tenant <UUID> --plan free|paid|unlimited`, providing `OVERLAID_ADMIN_DATABASE_URL` privately. This operator-only transaction records before/after metadata. Downgrades preserve saved commands/timers/variables; execution above the current cap stops immediately. Hourly outbound calls are counted atomically, including failed requests. Event storage trims to the plan cap, with a 250000-row safety bound for unlimited plans. Media storage and billing have no enabled upload/purchase path in Phase 1.

## Data lifecycle and restore

Owners choose 1–365 days of event/audit retention; the default is 30. The worker expires events, bot receipts, ended sessions, snapshots and usage. Tenant exports exclude tokens, credentials and sessions and stop above 32 MiB or 250000 rows per table, requiring an operator-assisted backup. Viewer deletion removes matching stable IDs/logins and quotes and clears derived retained overlay state. Tenant deletion revokes active-tenant sessions and all tenant credentials before cascading content; a non-secret deletion receipt remains up to 90 days. The self-host bootstrap cannot be deleted through the workspace API.

Before restoring a backup, isolate the target database and stop all application writers. Preserve the complete migration set and restricted-role definitions matching that backup. The original Phase 1 checkpoint used non-login `overlaid_runtime` and `overlaid_credential_lookup` roles before `pg_restore --no-owner --no-acl`, followed by migration 0003 to restore its RLS, ownership and grants. Later schemas also contain queue, pacing, bot-vault and execution roles/policies; the Phase 1 two-role recipe alone is insufficient for a current backup. Reconstruct and validate the full matching role/policy graph in an isolated restore drill, rather than granting the API administrator privileges or assuming migration 0003 restores later contracts. Run the runtime initializer with the intended separate app password. Restore the matching keyring privately, verify counts and representative reads, then reapply deletion requests that happened after the backup. Never expose the restored service until those checks pass.

For rollback of an existing installation, stop the new release, restore the pre-migration database and `api-data` backup into an isolated target, restore the previous environment/images, verify representative reads, then restore ingress. An old global binary must not run against the migrated shared database. Preserve post-cutover writes separately before any restore; they are not silently discarded.
