# Over-Laid documentation

Start with the [project README](../README.md) for the product overview and [current capabilities and limits](reference/project-status.md) for activation status. These pages describe the current application; dated plans and QA receipts retain their historical context.

## First visit

- [Getting started](runbooks/getting-started.md): fresh Compose setup, demos, native development, and sharing a running instance.
- [Newsroom](runbooks/newsroom.md): submission/triage, reaction deck, shows, enrichment, sharing and notes.
- [Overlay reference](runbooks/overlays.md): browser-source routes, sizes and query parameters.
- [Bot operations](runbooks/bot-ops.md): general Chat commands, timers, moderation and rewards.
- [Stack and configuration](reference/stack.md): toolchain, storage modes, environment groups and entry points.

## Operate and recover

- [Runbook index](runbooks/README.md): grouped links for setup, stream operation, accounts and retained execution.
- [Preflight](runbooks/live-stream-preflight.md) and [live QA](runbooks/live-qa.md): prepare and rehearse before a stream.
- [Service app custody](runbooks/service-app-custody.md): separate administrative app credentials, role isolation, keys and rollback boundaries.
- [Tenancy and recovery](runbooks/tenancy-and-recovery.md): database roles, migrations, vault keys, sessions and lifecycle.
- [Packaging and backups](runbooks/production-packaging.md), [recovery](runbooks/production-recovery.md), and [monitoring](runbooks/uptime-monitoring.md).

## Understand and contribute

- [Contributing](../CONTRIBUTING.md) and [security reporting](../SECURITY.md).
- [Remaining-work roadmap](superpowers/plans/2026-10-15-remaining-work-roadmap.md): current delivery order and completion gates.
- [Phase 3 engine and variable plan](superpowers/plans/2026-10-21-phase-3-engine-and-variables.md): bounded units, compatibility and verification gates; current delivery is tracked in the roadmap.
- [Architecture decisions](adr/README.md): accepted contracts and boundaries.
- [Original multi-platform scope](superpowers/plans/2026-10-05-multiplatform-parity-roadmap.md): feature targets and research context.
- [Gitea project](https://git.subcult.tv/subculture-collective/overlaid/projects/4): hosted work tracking; repository permissions apply.
- [October 13 operational receipt](qa/2026-10-13-phase-1-homelab.md): historical deployment/restore evidence, separate from current implementation and live acceptance.
- [Newsroom provenance](vendor/newsroom/PROVENANCE.md), [River provenance](vendor/river/), and [overlay brand provenance](../web/public/brand/subcult/PROVENANCE.md).

`reference/` holds technical reference material; `runbooks/` holds procedures; `adr/` holds decisions; `research/` holds exploratory material; `superpowers/plans/` and `qa/` retain dated plans and evidence. Template-origin guidance such as `template-maintenance.md` and `stacks.md` describes repository ancestry rather than first-run application setup.
