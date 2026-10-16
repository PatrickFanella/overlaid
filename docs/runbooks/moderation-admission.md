# Explicit retained-source moderation admission

Tracking: [#167](https://git.subcult.tv/subculture-collective/overlaid/issues/167), under [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24). This adds an explicit writer to the [application-owned consumer](moderation-consumer.md). No route, environment setting, default startup registration or existing workflow selects it yet.

`moderationConsumer.admit` requires a ready consumer and the exact current tenant runtime. Its caller supplies a retained source ID, an exact action key and the existing private moderation request. It reads the source envelope through an immutable tenant scope; it accepts no caller-supplied source envelope, broadcaster, sender credential, action ID or deadline. Application, consumer, tenant and caller cancellation bound the operation, with a five-second admission budget. Replaced runtimes, hosted bootstrap access and unready owners fail closed. Preparation alone cannot admit work and a refused call does not activate or cancel a prepared consumer.

Request mapping selects delete, timeout, ban or unban. It preserves existing draft semantics: irrelevant message/user/duration/reason fields do not change another operation. Timeout seconds are range-checked before conversion to prevent overflow. The typed intent validates the selected effect fields and derives its identity, fingerprint and original two-minute deadline from the canonical retained source. No provider role lookup, HTTP allowance reservation, pacing permit or effect claim occurs during admission.

The existing [moderation ledger](platform-moderation.md) rechecks the source, active connection, retained delete target, shared chat/moderation plan and per-source bounds inside its transaction. Action and payload-free job commit together. Repeating a key preserves the original action/job/deadline, including terminal outcomes; changing effect input conflicts. Admission returns an action ID and whether it was created, never a delivery receipt. The worker remains responsible for fresh selected-identity authorization, pacing, one-use claim and typed completion. Database/provider details are excluded from admission failures.

## Existing effect paths and remaining cutover

The October 16 source inventory follows every non-test call to `executeTwitchModerationAction`, `deleteTwitchChatMessage`, `banOrTimeoutTwitchUser` and `unbanTwitchUser`:

| Entry | Current effect boundary | Required retained binding |
| --- | --- | --- |
| Dashboard `/api/bot/moderation/action` | `twitch_moderation.go` calls the central workflow directly | A durable authorized operator occurrence and stable retry identity; the request currently has no canonical chat source. |
| Dashboard nuke execution | `moderationWorkflow.executeNuke` calls the same boundary for up to 50 history matches | A durable batch occurrence and per-target keys, bounded admission and queued-status reporting. Existing history lookback can exceed the chat source's two-minute deadline. |
| Automatic enforced rules | `moderationWorkflow.enforceModerationForEvent` calls the central boundary for its first non-exempt match | The actual retained dispatch source and a stable rule/occurrence key. Flag-only matches remain audit-only. |
| Nuke radiation enforcement | The saved temporary rule uses the same automatic enforcement path | The actual retained source plus the saved radiation rule identity; disable/expiry must retain their existing behavior. |

The three low-level Helix helpers are called only by the central workflow in this inventory. Rule configuration, dry-run matching and radiation rollback do not send moderation effects. Recheck these call sites when cutover starts rather than treating this inventory as a permanent guarantee.

The next units must define durable operator/batch source admission without fabricating a chat event or renewing an old source deadline; bind automatic rules to the existing dispatcher; expose pending/terminal moderation status separately from synchronous acceptance; and then compose opt-in startup with persisted compatibility/downgrade guards. All effect paths must select the retained boundary together when enabled. Admission or execution failures must never fall back to direct broadcaster transport. Default legacy behavior remains unchanged until that coordinated cutover is implemented and qualified.

## Verification

`TestModerationAdmissionRequestMapping` checks all four operation mappings, irrelevant draft fields, unsupported clear-chat and timeout overflow/range refusal. Real-River `TestModerationAdmissionDeliveryAndTerminalDuplicate` verifies broadcaster/shared delivery, prepared refusal without activation, terminal duplicate/no replay, changed-payload conflicts and original job/deadline preservation. `TestModerationAdmissionRefusesUnboundWork` checks missing/foreign/expired/deleted sources, revoked connections, canceled callers, replaced runtimes, stopped consumers, role-policy drift, invalid input and unretained delete targets with zero moderation rows/jobs/provider requests.

Run `GOTOOLCHAIN=go1.26.0 bash scripts/ci-postgres.sh` for `make verify` and both uncached application/River race suites against disposable PostgreSQL. Fixtures use synthetic provider responses. This establishes neither live effects nor default startup/cutover, unlimited-load fairness or full thousand-channel acceptance.
