# Pacing role validation

Tracking: [#111](https://git.subcult.tv/subculture-collective/overlaid/issues/111), a prerequisite under [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24).

Both explicit pacing constructors validate the current database identity, the original session identity and memberships reachable from either identity. This includes NOINHERIT memberships that can be selected through SET ROLE. A connection URL that selects a safe role cannot hide a more privileged original session login.

Reject superuser, RLS bypass, role/database administration, replication, schema creation and application-table ownership. Reject access to other application tables in the configured schema. On the pacer's own tables, require SELECT/INSERT/UPDATE/DELETE and reject TRUNCATE, REFERENCES and TRIGGER, including grants through reachable roles. These extra privileges are unnecessary for pacing and can bypass row security or change state behavior.

Every pacing table must have forced RLS and exactly one permissive all-command `pacer_only` policy, scoped to one non-public role reachable by the current identity, with literal true USING and WITH CHECK expressions. A restrictive-only policy or changed expression fails construction rather than leaving a pool that cannot perform its work.

Chat owns `chat_pacing_accounts` and `chat_pacing_channels`; moderation owns `moderation_pacing_buckets`. Use the existing [chat](chat-pacing.md) and [moderation](moderation-pacing.md) migration and login provisioning contracts. This fix needs no migration or grant expansion. No pacing pool is registered at application startup.

Validation is a construction snapshot over the configured schema. Keep the session login restricted throughout its lifetime and do not broaden grants while a pool is active. Operators must separately prevent access to unrelated schemas. This check neither authorizes provider effects nor establishes fair permit acquisition.

`TestPacingPools` in `internal/platformqueue/pacing_roles_integration_test.go` uses disposable roles and tables. It proves an unsafe original session can reset/select another role and read synthetic runtime data, then requires rejection by both constructors. Other cases replace policy expressions/permissiveness and grant administrative, schema or extra table privileges, asserting rejection and successful construction after restoration. The full gate is `GOTOOLCHAIN=go1.26.0 bash scripts/ci-postgres.sh`, including make verify and uncached application/spike race tests. These fixtures do not audit production grants or make provider calls.
