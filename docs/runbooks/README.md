# Operator and developer runbooks

Use [getting started](getting-started.md) for a fresh installation. Read [current capabilities and limits](../reference/project-status.md) before enabling an experimental execution path. Dated receipts and finite fixtures do not replace acceptance on your deployment.

## Run a show

- [Live preflight](live-stream-preflight.md), [rehearsal/live QA](live-qa.md), and [visual QA](visual-qa.md).
- [Overlays](overlays.md): routes, OBS sizes, parameters and brand rules.
- [Newsroom](newsroom.md): editorial workflow, provider setup, public episodes and exports.
- [Bot operations](bot-ops.md), [Twitch commands](twitch-chat-commands.md), and [interaction/reward rules](twitch-interaction-rules.md).
- [OBS sequences](obs-automation-sequences.md), [stream sessions](stream-sessions.md), and [event history](event-history.md).

## Install, secure and recover

- [Getting started](getting-started.md): container/native evaluation and exposure settings.
- [Tenancy and recovery](tenancy-and-recovery.md): migrations, runtime roles, keyrings, membership and data lifecycle.
- [Production packaging](production-packaging.md), [production recovery](production-recovery.md), and [uptime monitoring](uptime-monitoring.md).
- [Homelab deployment](homelab-phase-1.md): environment-specific deployment procedure and evidence boundaries.

## Twitch identity and retained chat

- [Bot onboarding](bot-onboarding.md), [tenant-owned bots](bot-accounts.md), [service bot administration](service-bot-admin.md), and [shared delegation](shared-bot-delegation.md).
- [Retained Twitch dispatch](retained-twitch-chat.md) and [retained chat sender](retained-chat-sender.md): explicit activation, roles, readiness and rollback.
- [Normalized stream-event contracts](normalized-twitch-stream-events.md) and [retained stream admission](retained-stream-events.md): typed online/offline and channel updates, atomic private storage/jobs and tenant lifecycle. The [fenced stream consumer](stream-dispatch.md) provides explicit controlled leasing and handler receipts; the [application-owned Newsroom adapter](application-stream-handler.md) binds runtime ownership and fresh local read gates. The [explicit application consumer](application-stream-consumer.md) owns queue readiness and shutdown. The [retained routing cutover](stream-routing.md) adds an explicit legacy-default startup mode through the existing lifecycle controls, with persisted writer guards and a [retained channel-update subscription set](channel-update-subscriptions.md). The [private observation read API](stream-observations.md) exposes eligible retained lifecycle/channel metadata without fetching providers or changing editorial fields. Live provider/load acceptance remains separate.
- [Platform ingestion](platform-ingest.md), [dispatch](platform-dispatch.md), [outbound ledger](platform-outbound.md), and [outbound worker](outbound-worker.md).
- [Manual announcements](manual-announcement-intents.md), [registry reply collection](registry-reply-collection.md), and [saved-command admission](registry-admission-binding.md).
- [Runtime capacity](runtime-capacity.md), [chat pacing](chat-pacing.md), [read-only pacing inspection](pacing-inspection.md), and [tenant-fair leasing](tenant-fair-leasing.md).
- [Tenant sender authorization](tenant-outbound-auth.md) and [shared execution custody](shared-bot-execution.md).

## Explicit retained moderation

- [Moderation consumer](moderation-consumer.md), [source admission](moderation-admission.md), [operator occurrences](manual-moderation-sources.md), [single-action requests/status](operator-moderation-requests.md), [nuke preview/radiation store](nuke-preview-binding.md), and [automatic-rule routing/handoff](moderation-routing.md).
- [Contract](moderation-contract.md), [ledger](platform-moderation.md), [queue](moderation-queue.md), [executor](moderation-executor.md), and [worker](moderation-worker.md).
- [Provider authorization](twitch-moderation-authorization.md), [typed transport](twitch-moderation.md), [pacing](moderation-pacing.md), and [priority qualification](moderation-priority.md).

Retained moderation has an explicit `TWITCH_MODERATION_MODE=retained` opt-in with separate worker/pacing credentials, retained chat prerequisites, a closed startup lease barrier and persisted downgrade guards. Saved single-action and nuke HTTP/UI workflows are implemented. Legacy remains the default; the linked procedures distinguish finite fixtures from live provider and broader load acceptance.

- [Retained channel-update subscriptions](channel-update-subscriptions.md): selected stream-set repair, reconnect, historical metadata and editorial isolation.

- [Explicit conduit inspection](conduit-inspection.md): validated app-token reads of one selected conduit; no credential custody, provider writes or startup activation.
- [Explicit conduit creation transport](conduit-creation.md): one app-validated POST with fixed uncertainty and metadata projection; durable ownership and application invocation remain separate.
- [Durable conduit creation ownership](conduit-ownership.md): a separate metadata-only role, original intent/deadline, one-use claims and retained uncertainty; no provider or startup caller.
- [Custody-bound conduit creation](custodied-conduit-creation.md): explicit composition of separate token-reader and ledger pools, fresh custody checks and one claimed provider attempt; no startup caller.
- [Native conduit creation](native-conduit-creation.md): explicit prepare/status/execute, dedicated private configuration, original request IDs and no automatic replay.
- [Read-only app conduit inventory](conduit-inventory.md): bounded app-wide ID/count observations through restricted custody and native `--inventory`; no ownership clearing or adoption.

- [Explicit app-token acquisition](app-token-acquisition.md): one bounded issuance attempt and app validation, with redacted ephemeral credentials and explicit uncertainty before custody.

- [Service app custody](service-app-custody.md): separate administrative encryption, revision checks, local revocation and metadata-only receipts; no execution resolver or startup caller.

- [Custodied conduit inspection](custodied-conduit-inspection.md): separate token-column-only role, exact app revision, read-only transaction and revocation fencing.
- [Native conduit inspection CLI](conduit-inspection-cli.md): exact app/client/revision/conduit selection, private execution configuration and one bounded metadata report.
- [Durable service-app issuance](service-app-issuance.md): database-generated intents, one-use claims, atomic custody installation and retained uncertainty.
- [Native app issuance](native-app-issuance.md): explicit prepare/execute steps, dedicated private configuration, original request IDs and no automatic retry.

- [One-attempt WebSocket conduit shard assignment](conduit-shard-assignment.md)

- [Durable conduit shard assignment ledger](conduit-assignment-ledger.md): original session fingerprints, one-use claims and retained uncertainty under a separate metadata-only role.

- [Custody-bound conduit shard assignment](custodied-conduit-assignment.md): separate credential/creation/assignment owners, required live-session guard and one committed provider attempt.

- [Owned initial conduit session](owned-conduit-session.md): private welcome binding, original deadline, scoped lease, bounded notification delivery and explicit shutdown.

- [Retained conduit stream ingress](conduit-stream-ingress.md): copied subscription/tenant scopes, required local owner lease and existing atomic stream event/payload/job admission.

- [Selected conduit subscription inspection](conduit-subscription-inspection.md): bounded read-only provider/custody observation and native --subscriptions mode, with private-condition exclusion.
