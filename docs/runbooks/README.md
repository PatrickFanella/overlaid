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
- [Platform ingestion](platform-ingest.md), [dispatch](platform-dispatch.md), [outbound ledger](platform-outbound.md), and [outbound worker](outbound-worker.md).
- [Manual announcements](manual-announcement-intents.md), [registry reply collection](registry-reply-collection.md), and [saved-command admission](registry-admission-binding.md).
- [Runtime capacity](runtime-capacity.md), [chat pacing](chat-pacing.md), [read-only pacing inspection](pacing-inspection.md), and [tenant-fair leasing](tenant-fair-leasing.md).
- [Tenant sender authorization](tenant-outbound-auth.md) and [shared execution custody](shared-bot-execution.md).

## Explicit retained moderation

- [Moderation consumer](moderation-consumer.md), [source admission](moderation-admission.md), [operator occurrences](manual-moderation-sources.md), and [automatic-rule routing/handoff](moderation-routing.md).
- [Contract](moderation-contract.md), [ledger](platform-moderation.md), [queue](moderation-queue.md), [executor](moderation-executor.md), and [worker](moderation-worker.md).
- [Provider authorization](twitch-moderation-authorization.md), [typed transport](twitch-moderation.md), [pacing](moderation-pacing.md), and [priority qualification](moderation-priority.md).

The retained moderation consumer still has no environment switch or default startup registration. Operator/batch store admission is implemented; HTTP/UI wiring, asynchronous owner status and guarded configured activation remain unfinished. The linked procedures distinguish explicit fixtures from deployable startup paths.
