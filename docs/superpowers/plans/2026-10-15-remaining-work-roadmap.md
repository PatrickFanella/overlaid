# Remaining-work roadmap

Tracking: [#34](https://git.subcult.tv/subculture-collective/overlaid/issues/34). Baseline: merged main `a2f9841efa83582998a676eec1f12213a7652490`, after PR #83, on October 15, 2026.

For a reader-facing snapshot through PR #170, see [current capabilities and limits](../../reference/project-status.md). The starting-point table below records the October 15 baseline; subsequent unit sections record later implementation. Use the linked activation runbooks for current configuration rather than inferring startup behavior from a historical unit description.

This records the October 15 delivery order for the 16 open program issues at the baseline. Bounded implementation issues are added as their units start. It supersedes the scheduling and current-state sections of the [October 5 roadmap](2026-10-05-multiplatform-parity-roadmap.md). That document and the [research catalogue](../../research/2026-10-05-bot-parity-research.md) retain the detailed parity scope; the [accepted ADRs](../../adr/README.md) retain their architectural authority. No accepted feature is removed by this roadmap. Provider documentation and competitor coverage must be refreshed when the relevant implementation unit starts; the October research is not a current API guarantee.

## Outcome and priorities

Deliver a public multi-tenant bot and overlay service for Twitch, Kick, Discord and YouTube Live, while keeping single-tenant self-hosting supported. Finish the Twitch execution boundary, then prioritize the shared automation engine and chat depth. Alerts/media, loyalty and the paid tier follow those foundations. Final branding and provider registrations run alongside implementation rather than holding up unrelated work.

Each implementation unit should leave a usable, reviewable result. A planning document does not activate providers, publish a release or authorize a live test. Existing operator authorization and the applicable issue/runbook determine those actions. Use synthetic credentials and provider transports until a controlled external acceptance session is ready.

## Starting point

| State | Evidence and limit |
| --- | --- |
| Complete: Phase 0 and Phase 1 implementation | PRs #44–46; issues #3–22, #38 and #41 are closed. Tenant scopes, forced RLS, vaults, sessions, plans and lifecycle controls are already foundations to extend. |
| Complete: Newsroom rebuild and feature catch-up | PRs #48–49. Preserve the shared Chat command manager, retained editorial workflow, enrichment fencing and existing overlay/account boundaries. Newsroom does not complete the broader Discord, media or automation phases. |
| Complete: 17 bounded Phase 2 units | Closed issues #50–82 (even numbers), merged PRs #51–83; see [Phase 2 tracking](https://git.subcult.tv/subculture-collective/overlaid/issues/24) and its [implementation plan](2026-10-14-phase-2-platform-layer.md). |
| Incomplete: Phase 2 activation and transport | Retained chat is opt-in; legacy remains default. Outbound workers/adapters are unregistered at startup. Shared service-vault execution, priority/warnings, cutover, conduits, additional normalized events and onboarding exits remain. |
| Recorded operational baseline | [October 13 homelab receipts](../../qa/2026-10-13-phase-1-homelab.md) document a prior deployment/alert publication/restore. This roadmap does not refresh production or establish real provider credentials, natural backup execution or actual rollback. |
| Verified implementation checks | PR #83 head `07f503b` and merged main `a2f9841` have successful hosted verification. The recorded local disposable PostgreSQL run includes `make verify`, 133 frontend tests and both uncached Go race suites. This is separate from deployment and live qualification. |

## Delivery sequence

| Order | Deliverable | Tracking | Start dependency | Exit |
| --- | --- | --- | --- | --- |
| 1 | Finish Twitch identity, execution and transport | #24 | Existing contracts, ledgers, pacing and bot stores | Controlled onboarding, bot identity/badge, restart/revocation and full simulated-load evidence. |
| 2 | Build the versioned engine and variable language | #25 | Stable normalized chat/effect contracts; can start while conduit or external acceptance work proceeds | Existing commands, timers, rewards and quick actions use one engine, with traces and effect-free replay. |
| 3 | Deliver chat-bot depth and import compatibility | #26 | Engine versions, restrictions and trace contract | Nightbot/Fossabot catalogue coverage with explicit gaps and a successful import rehearsal. |
| 4 | Expand automation effects and the OBS bridge | #27 | Engine/effect contract; implement in batches alongside #26 after that contract is stable | Platform/HTTP/OBS catalogue coverage and controlled real-OBS scene/text proof. |
| Early vertical slice | Explain and approve a missing-input fallback | #47 | Bounded engine version/trace contract from #25 plus the minimal OBS effect path | One configured, rehearsable, approved fallback with stale-observation and duplicate-effect protection. It need not wait for all of #25 or #27. |
| 5 | Add Kick | #28 | Reusable engine/capabilities and outbound boundary; Kick registration under #23 | Existing command/filter fixtures run on Kick; unsupported effects are explicit. |
| 6 | Add Discord | #29 | Engine events, session reports and Discord registration | Linked server, go-live post and opt-in viewer role sync pass controlled acceptance. |
| 7 | Add YouTube Live | #30 | Adapter/engine contract and Google/YouTube approvals | Two controlled one-hour tenant sessions stay within quota with visible headroom. |
| 8 | Deliver alerts, widgets and open media requests | #31 | Engine queues/priority, object-storage lifecycle and needed event adapters | Queued alerts, useful widgets and a moderated media queue have working stop/removal controls and tenant isolation. |
| 9 | Deliver viewers and loyalty | #32 | Retained event lifecycle; cross-platform identity links need their adapters | Viewer-initiated linking, bounded currency accounting, giveaways and games pass retention/isolation checks. |
| 10 | Add payments, integrations and service analytics | #33 | Stable entitlement/lifecycle contracts and Stripe registration | Signed billing events change plans safely, self-host stays independent, and the chosen public integration/analytics scope is accepted. |
| Alongside delivery | External registrations and branding | #23 | Owner decisions and provider access | Each required registration/review has a non-secret receipt for its consuming phase. |
| Alongside delivery | Existing-product Twitch and OBS acceptance | #39, #40 | Controlled accounts/targets, owned OBS collection and an exact candidate revision | Real workflows have dated results, restored fixtures and explicit unresolved failures. |
| Release gate | Qualify the first named self-hosted release | #42 | #39 and #40 plus a selected artifact and target | Exact deployed revision, real credential restore, monitoring visibility, target rollback and reviewed release publication. |

The first named single-channel release can complete before the later multi-platform phases. Phase 2 bot/badge acceptance is additional evidence; it does not silently replace #39's existing-product tests. An external provider delay does not block parser, engine, fixture or unrelated platform work. Neither #24 nor #34 closes merely because its child implementation issues are closed.

## Next Twitch implementation units — #24

Create a bounded issue for a unit when implementation starts, linked to #24. The identifiers below describe planned units; they are not new issue numbers. Do not recreate the completed #50–82 tickets.

| Unit | Result and affected boundary | Dependency | Decisive verification |
| --- | --- | --- | --- |
| P2.1 — merged #85 / PR #86 | Restricted shared service-vault execution in `tenancy` and `twitchoutbound`. Resolve only a currently published identity with exact grant/catalog/delegation revisions and tenant/channel consent. Add a dedicated restricted role/pool if needed; the administrator store must not become a worker credential. | PR #83 and current publication/consent stores | Separate pool cannot administer grants or read tenant/queue data; combined and reachable roles reject. Revocation, unpublish/republish, reauthorization, changed binding/role/plan and concurrent refresh cannot send under stale consent. Real send transport stays mocked. |
| P2.2 — #87/#89 merged; ledger #93 | Priority-aware outbound scheduling and visible pacing/limit failures. Use the existing action ledger, River jobs and global account/channel pacing; do not reserve future slots for low-priority chatter. | P2.1's authorization contract; moderation action identity/receipt contract before enabling moderation effects | Mixed-priority synthetic load proves moderation is not starved, quotas remain global across tenants, deadlines remain fixed and retry hints never replay an uncertain action. Add deterministic headroom/warning threshold checks. |
| P2.3 — controller #131, opt-in sender #133 | Opt-in outbound integration through the existing shared registry. Persist actions/jobs once and select one active sender/controller. Add readiness, explicit configuration and a documented rollback boundary. | P2.1–P2.2 and compatibility fixtures for every existing reply-producing path | Legacy and retained senders cannot both execute; controller loss/role failure degrades without fallback. Restart, duplicates, known echo IDs, manual broadcaster commands, Newsroom requests, pause/cooldown and cancellation retain behavior. |
| P2.4 | Expand normalized Twitch events and capability mappings in reviewed batches. Keep native delivery IDs, raw-payload limits, permissions and lifecycle deletion. | Current chat ingest; shared contracts needed by each batch | Native fixtures prove each event's identity, typed fields and redacted projection; duplicate delivery and tenant isolation pass before new subscriptions are enabled. |
| P2.5 | App-authorized conduit/shard ownership and subscription reconciliation. Keep the current transport recoverable until replacement compatibility is proven. | Shared authorization, event contracts from P2.4 for subscribed kinds and operator-owned grants from #23 | Synthetic shard disconnect/reassignment/restart and subscription drift cannot duplicate dispatch or cross tenant boundaries. Live migration gets a separate controlled receipt. |
| P2.6 | Complete Chat onboarding: request enabled-feature grants, verify current role, explain blocked capabilities and perform an explicit test send with a receipt. | Activated execution/transport contracts, P2.3 and P2.5 | Fresh-tenant onboarding is timed; bot subject/badge and reconnect/revocation are observed in a controlled session. A full 1,000-channel simulation includes queueing, priorities and deadlines rather than only the pacing model. |

The current pacer permits a conservative 20 attempts per rolling 30 seconds per account and one-second channel spacing. The original moderator-capacity target is not enabled. Keep that budget until current provider rules, role evidence and a reviewed policy justify a change; do not raise it as a prerequisite for finishing an unrelated unit. Headroom/warning metrics belong with P2.2, even though broader analytics remain under #33.

Every unit preserves immutable tenant scopes, forced RLS, source lifecycle cascades, one dispatcher, durable pre-effect claims, no replay after uncertainty, selection revisions and bounded redacted receipts. See [outbound worker](../../runbooks/outbound-worker.md), [tenant authorization](../../runbooks/tenant-outbound-auth.md) and [shared consent](../../runbooks/shared-bot-delegation.md).

## Engine and chat implementation order — #25 and #26

Write the bounded Phase 3 plan against [ADR 0005](../../adr/0005-automation-engine.md) and [ADR 0006](../../adr/0006-variable-language-sandbox.md) before its first code change.

1. **Version and validate automation documents.** Add immutable versions, tenant-owned references, stable trigger occurrence/run identity, mandatory permission/entitlement gates and atomic cooldown/limit reservations. Invalid trees, cycles, depth/step/loop budgets and cross-tenant references must fail before execution.
2. **Build the variable parser and migration preview.** Add syntax/version-aware ASTs, ranges, nesting, lazy fallbacks, diagnostics and bounded caches. Preserve original brace templates and unknown literal text; compare rendered fixtures before selecting a new version. Fuzz malformed input and verify captured clock/seed behavior.
3. **Run bounded effects and durable continuations.** Start with chat and the minimal approved dashboard/OBS action interfaces; add delay/cancellation, output/presets and priority/pause semantics. Link run steps to outbound receipts without turning ambiguous delivery into an engine retry.
4. **Expose traces and replay.** Record why filters/restrictions/effects passed or failed, with truncation and redaction. Reproduce pinned versions/context separately from testing current configuration. Replay uses captured external values and cannot mutate network, OBS or product state through effects.
5. **Migrate existing behavior in batches.** Commands, Newsroom bot entries, timers, reward rules and quick-action sequences need compatibility fixtures, a single selected executor per item, rollback mappings and reviewed version changes. Preserve the general Chat command manager. Remove a legacy path only after its replacement has parity evidence.
6. **Finish the editor and advanced variables.** Version history, publish/rollback controls, guarded HTTP variables and delayed-response capabilities require tenant/role/deletion tests. Hosted `$(eval)` remains disabled until ADR 0006's child-process, OS memory/time/process and network/filesystem isolation is demonstrated; there is no in-process fallback.
7. **Deliver #26 in catalogue batches.** Commands/roles/aliases/reply modes and chat management; default commands/variables; timers/keywords/filters/lookalikes; public pages and retained search; importers. Record each Nightbot/Fossabot checklist item as covered, intentionally different or still missing. Test a real export/import fixture without provider side effects.

The #47 fallback slice may start after the version/trace contract and minimal effect path exist. It must distinguish a missing input from unknown/disconnected/stale/muted/hidden state, explain one proposal, revalidate it on approval and retain a bounded receipt. Rehearsal performs no OBS effect. Qualification uses #40's owned test collection and restores its original state.

## Later-phase boundaries to preserve

| Tracking | Required scope that remains |
| --- | --- |
| #27 | Typed event filters; platform moderation/stream/reward effects; guarded HTTP, Discord and webhook effects; OBS bridge/events; mobile-usable presets/quick actions/control deck; versioned setup import/export. Existing manual controls and Newsroom Discord delivery are inputs, not full catalogue acceptance. |
| #28 | Tenant OAuth/webhook verification/reconciliation, normalized Kick events, chat/moderation execution, explicit unavailable capabilities and observed bot identity. Confirm current provider contracts before implementing them. |
| #29 | Templated go-live/clip posts, signed slash-command interactions, opt-in linked-viewer roles with fallback, and session recap. Keep this separate from Newsroom's existing confirmed webhook exports. |
| #30 | Live detection, bounded live chat, supported monetization/membership/poll mapping, per-tenant/shared quota accounting, opt-in bot replies and moderated bot identity. Public launch waits for required Google review/quota acceptance. |
| #31 | Alert variations and priority queues; tenant media storage; event-fed goal/subathon/poll/prediction/counter/chat widgets; embeddable-source policy; approve-first controls, user/queue limits, blocks, fast stop, desk/OBS players, backup playlist and public queue. Preserve the non-removable copyright-stop notice. Currency costs integrate after #32's ledger exists. |
| #32 | Watch/message/first/last-seen viewer records, viewer-initiated identity linking, capped currencies/transfers/role bonuses/ranks, giveaway modes/luck/overlay and games. Newsroom raffles are not full loyalty acceptance. |
| #33 | Donations/subscriptions/portal and signed idempotent plan updates; tenant usage/service metrics; scoped public OAuth API/event stream; sandboxed plugins only if demand justifies that conditional scope. Validate paid-tier packaging using usage data; bot custody/onboarding already exists and must not be rebuilt. |

For #31, do not block the basic media queue on loyalty: add costs only after #32 is ready. For #33, safety/operational metrics needed by an earlier unit ship with that unit; a later analytics phase is not a reason to omit them.

## External acceptance and release

| Issue | Next action | Evidence required to close |
| --- | --- | --- |
| #23 | Owner resolves final branding/bot identity and prepares the required Twitch, Kick, Discord, Google/YouTube and Stripe registrations in time for their phases. Domain/privacy/terms are already recorded as complete. | Non-secret registration/review receipts, explicit pending/rejected reviews and links to consuming phases. Request secrets through private tooling only. No account creation or publication is performed by this roadmap. |
| #39 | Prepare an exact-revision controlled Twitch session for OAuth/re-consent, EventSub/reconnect, a custom reply/echo suppression, consenting moderation targets and reversible reward/redemption changes. | Dated real-provider receipts, cleanup and unresolved failures. Mocked HTTP and fixture browser workflows do not close this issue. |
| #40 | Select an owned test collection and record OBS version, networking/auth and source revision. Check useful demos, keyed live sources, reconnect/display settings and scene/source controls. | OBS CEF screenshots/results and action receipts; restore original source/collection state. Chromium alone does not close this issue. |
| #42 | After #39/#40, select the approved target and exact artifact, restore real retained data/credentials into an isolated clone, prove readiness-aware monitoring/notification visibility and execute target rollback. | Matching deployed `/version`, immutable artifacts, restore/rollback receipts, reviewed scope/limitations/recovery notes and separately authorized tag/release publication. Prior Phase 1 deployment/restore is baseline evidence only. |

Unobserved natural backup execution and device notification display remain evidence gaps from the earlier operational receipt. Refresh scheduler/service/artifact evidence when qualifying #42; do not assume elapsed time closed them. Named release acceptance does not authorize changing active production while planning.

## Verification and issue handling

Run `make verify` for every delivered unit. Changes to persistence, worker roles, claims, queues or concurrency also use `bash scripts/ci-postgres.sh`, which owns disposable PostgreSQL and includes uncached application/River race suites. Add the smallest behavior check that can disprove the unit's acceptance; do not count skipped integration fixtures as a pass.

Separate six evidence states in each closeout: local checks, exact PR-head hosted checks, reviewed merge, merged-main checks, deployment and controlled runtime/provider acceptance. Record revision and limitations. Use fixture browser checks for changed user flows; use OBS/provider qualification for their actual environments. Never send real Twitch/Discord effects from fixtures.

Keep #34 as the umbrella and #24–33 as phase owners. Use bounded implementation issues for reviewed units, link their PRs and assign their phase milestone. Close a unit after its implementation acceptance and reviewed merge; keep its phase open until the full exit is demonstrated. External #23/#39/#40/#42 close only from their own receipts. The #47 dependency is the bounded engine contract, not the whole later catalogue.

Before starting the next unit, refresh the checkout, issue/PR state and relevant evidence. Preserve dirty work and active services. Database cutovers require additive migrations, one writer/dispatcher, retained claims, a private backup/restore path and a documented recovery boundary; an old binary must not resume against incompatible shared schema.

## First work to start

1. P2.1 shared-vault execution is merged in [PR #86](https://git.subcult.tv/subculture-collective/overlaid/pulls/86), closing #85; see the [shared execution runbook](../../runbooks/shared-bot-execution.md). P2.2 is in progress: visibility (#87 / PR #88) and the moderation contract (#89 / PR #91) are merged. The retained moderation ledger (#93 / PR #94) is merged. Atomic scheduling and restricted leasing (#95 / PR #96), typed transport (#97 / PR #98), selected identity authorization (#99 / PR #100), moderation pacing (#101 / PR #102) and the fenced executor (#103 / PR #104) are merged. The explicit moderation worker (#105 / PR #106) is merged. Priority metadata and mixed-load lane isolation (#107 / PR #108) are merged. Tenant-fair leasing (#109 / PR #110) is merged. Pacing role validation (#111 / PR #112) is merged. Chat permit fairness (#113 / PR #114) is merged. Moderation permit fairness (#115 / PR #116) is merged. Active-caller eligibility (#117 / PR #118) is merged. Source-bound reply batches (#119 / PR #120) are merged. Saved registry snapshot metadata (#121 / PR #122) is merged. Source-bound collection (#123 / PR #124) is merged. Transactional registry admission (#125 / PR #126) is merged. Manual-source admission (#127 / PR #128) is merged. The announcement request/UI contract is tracked under #129; broader admission qualification and opt-in registry integration remain unfinished.
2. Prepare the Phase 3 bounded plan under #25 using the existing accepted engine/variable decisions. Engine contract work may proceed while P2.4–P2.6 or external reviews wait.
3. Keep #23's registration checklist and #39/#40's controlled-session prerequisites visible; do not perform live effects or request secrets in public issue text.

No calendar dates or completion percentages are promised here. Estimate a unit after its implementation plan exposes the migration, provider and verification work; update this roadmap when a dependency or accepted scope changes.

## P2.2 visibility slice — #87

[Issue #87](https://git.subcult.tv/subculture-collective/overlaid/issues/87) adds a [read-only pacing report and explicit operator probe](../../runbooks/pacing-inspection.md), independently of the shared-execution PR review. It reports account headroom, current blocking, clock anomalies and original-deadline risk without acquiring a permit. The ledger currently schedules chat sends only. The typed moderation contract in #89 is merged; durable moderation admission/jobs/claims and mixed-priority arbitration remain required before priority scheduling can be accepted. This slice does not close P2.2, raise account budgets, start continuous alerting or activate a sender.

## P2.2 moderation contract — #89

[Issue #89](https://git.subcult.tv/subculture-collective/overlaid/issues/89) adds the [moderation identity and receipt contract](../../runbooks/moderation-contract.md) for the existing delete, timeout, ban and unban operations. It pins source/key identity, immutable conflict fingerprints, original deadlines and message-ID-free success receipts. It is independent of the visibility slice in #87. Durable moderation admission, atomic jobs and claims, provider authorization/transport and mixed-priority arbitration remain unfinished prerequisites for P2.2 acceptance. No moderation worker is activated by the contract.

The merged P2.1 revision `de6a449` has a hosted race-test failure in the existing outbound restart/snooze test; [#90](https://git.subcult.tv/subculture-collective/overlaid/issues/90) tracks diagnosis. Merged status alone is not a successful verification receipt. Resolve or explain that failure before activating the outbound worker.

## P2.2 retained moderation ledger — #93

[Issue #93](https://git.subcult.tv/subculture-collective/overlaid/issues/93) adds the [moderation ledger](../../runbooks/platform-moderation.md): retained-source admission, shared chat/moderation allowances, fenced one-use claims, typed completion and source/target lifecycle deletion. Pacing visibility (#87 / PR #88), the moderation contract (#89 / PR #91) and bounded failure diagnostics (PR #92) are merged. Issue #90 is now closed; its historical timeout cause was not established by finite verification; the merged diagnostics revision has successful hosted verification. Moderation River scheduling, fresh provider authorization/transport, moderation API pacing and mixed-priority arbitration remain unfinished. No scheduler or provider effect is activated by this ledger.

## P2.2 moderation jobs — #95

[Issue #95](https://git.subcult.tv/subculture-collective/overlaid/issues/95) adds [atomic moderation jobs and isolated leasing](../../runbooks/moderation-queue.md) after the merged ledger in #93 / PR #94. It preserves payload-free job arguments, original deadlines, one job per action, pending-only migration catch-up and three-way chat/outbound/moderation role isolation. Jobs retain neutral priority four; this is not mixed-priority arbitration or a registered moderation executor. Provider authorization/transport, endpoint pacing, the handler and opt-in registry execution remain unfinished.

## P2.2 typed moderation transport — #97

[Issue #97](https://git.subcult.tv/subculture-collective/overlaid/issues/97) adds an unregistered [single-attempt Twitch moderation transport](../../runbooks/twitch-moderation.md) after merged scheduling PR #96. It maps delete/timeout/ban/unban to typed moderation receipts, preserves cancellation and original request bounds, refuses redirects/replayable bodies, and treats ambiguous results as uncertain. Fresh subject/scope/role/consent authorization, endpoint pacing, ledger execution, the dedicated moderation worker, mixed-priority arbitration and opt-in registry integration remain unfinished. This transport does not activate moderation effects or complete P2.2.

## P2.2 moderation authorization — #99

[Issue #99](https://git.subcult.tv/subculture-collective/overlaid/issues/99) adds [fresh selected-identity moderation authorization](../../runbooks/twitch-moderation-authorization.md) after merged transport PR #98. Broadcaster, own and shared bots use the existing custody/refresh boundary with moderation-specific scopes and exact shared capability consent. Invocation rechecks identity, binding, role, scope, plan and consent, and binds the exact private action. This callback is not a pacing permit or one-use ledger fence. Endpoint pacing, fenced moderation execution, the dedicated worker, mixed-priority arbitration and opt-in registry integration remain unfinished.

## P2.2 durable moderation pacing — #101

[Issue #101](https://git.subcult.tv/subculture-collective/overlaid/issues/101) adds [isolated moderation sender/channel admission](../../runbooks/moderation-pacing.md) after merged authorization PR #100. Hashed client/moderator and client/broadcaster keys coordinate conservative spacing, fenced in-flight leases and durable cooldowns across tenants. The fresh authorization result exposes its validated client ID for the future executor's pacing namespace. No pool or executor is activated. Fenced moderation execution, the dedicated worker, mixed-priority arbitration and opt-in registry integration remain unfinished.


## P2.2 fenced moderation execution — #103

[Issue #103](https://git.subcult.tv/subculture-collective/overlaid/issues/103) adds the [explicit moderation executor](../../runbooks/moderation-executor.md) after merged pacing PR #102. It validates private ledger input and original deadlines, checks authorization before pacing and after a committed claim, compares exact client/moderator/binding/capability, and invokes one bounded callback. Typed moderation normalization, detached completion and durable future-action cooldowns preserve no replay after any claim, including uncertain delivery or lost bookkeeping. No worker or startup path is registered. The dedicated River client/worker, mixed-priority arbitration and opt-in registry integration remain next; live provider qualification stays separate.


## P2.2 restricted moderation worker — #105

[Issue #105](https://git.subcult.tv/subculture-collective/overlaid/issues/105) adds the [explicit moderation River worker](../../runbooks/moderation-worker.md) after merged executor PR #104. The restricted pool leases only moderation jobs and the executor scopes each action to its tenant. Preparation retries and snoozes stay within original deadlines; every claimed/terminal result cancels or completes without replay, even when persistence returns an error. No pool or worker is registered at startup. Deterministic mixed-priority arbitration and opt-in registry integration remain next; live provider qualification stays separate.


## P2.2 moderation lane priority — #107

[Issue #107](https://git.subcult.tv/subculture-collective/overlaid/issues/107) adds [priority-one moderation metadata and mixed-load lane qualification](../../runbooks/moderation-priority.md) after merged worker PR #106. Migration 0022 promotes existing jobs without renewing identity, deadlines, attempts or claims; the product pool requires its exact validated compatibility check and old priority-four writers fail closed. The actual River fixture holds a same-sender/channel chat callback while 1,000 chat actions from two tenants remain queued and proves moderation progresses using its separate capacity. Queued chatter books no future account slots. This is lane isolation, not cross-queue ordering, preemption, deterministic tenant fairness, complete provider quota accounting or the 1,000-channel phase acceptance. Tenant-fair arbitration and opt-in registry integration remain next; no startup caller is activated.


## P2.2 tenant-fair outbound leasing — #109

[Issue #109](https://git.subcult.tv/subculture-collective/overlaid/issues/109) adds [durable ready-job tenant turns](../../runbooks/tenant-fair-leasing.md) to both explicit outbound clients after merged priority PR #108. The pinned driver override retains exact-kind roles, native row decoding and atomic lease bookkeeping; a bounded non-cycling cache-one sequence and job metadata survive client restart without wall-clock ordering. Lease selection grants no claim or pacing permit. No pool or worker is registered at startup. This qualifies fairness among ready jobs, separately from fair provider-permit acquisition, cross-queue ordering, infinite hostile arrivals and live provider capacity. Those admission/activation limits remain before P2.2/P2.3 acceptance.

## P2.2 pacing role validation prerequisite — #111

[Issue #111](https://git.subcult.tv/subculture-collective/overlaid/issues/111) strengthens both [pacing constructors](../../runbooks/pacing-role-validation.md) after merged tenant-turn PR #110. Disposable PostgreSQL fixtures reproduce the original-session SET ROLE escape, administrative/schema privilege acceptance, changed policy acceptance and dangerous state-table grants. Constructors reject those configurations while restored valid roles remain usable. This changes no migration, budget, permit or startup registration. Shared provider-permit fairness and opt-in registry integration remain unfinished.

## P2.2 chat permit turns — #113

[Issue #113](https://git.subcult.tv/subculture-collective/overlaid/issues/113) extends tenant fairness from ready-job leasing into [shared chat permit admission](../../runbooks/chat-permit-fairness.md). Migration 0024 adds bounded hash-only waiting/turn state to the existing restricted account row. The explicit executor uses canonical action deadlines, preserves pre-claim retries without refunds and retains non-replayable claimed outcomes. Sender/channel inspection reports the unqualified tenant turn without writes. This covers registered channel-eligible chat work, not unlimited hostile arrivals, absent selected callers, moderation permit fairness or the full 1,000-channel acceptance. Moderation and opt-in registry cutover remain before activation.

## P2.2 moderation permit turns — #115

[Issue #115](https://git.subcult.tv/subculture-collective/overlaid/issues/115) extends the shared turn codec into [moderation permit admission](../../runbooks/moderation-permit-fairness.md). Migration 0025 retains one bounded list per client and selection checks both sender/channel resource eligibility; independent pairs retain concurrent permits. Canonical executor identity/deadlines, exact-fence pre-claim cleanup and non-replayable claimed outcomes remain. Chat retains its existing serialized format and read-only probe behavior. Broader admission/1,000-channel qualification, absent-selected-caller limits and opt-in registry cutover remain before activation.

## P2.2 active-caller eligibility — #117

[Issue #117](https://git.subcult.tv/subculture-collective/overlaid/issues/117) bounds selection stalls using [three-second caller eligibility leases](../../runbooks/fair-admission-liveness.md) in the shared codec. Canonical retries refresh eligibility without changing tickets, turns, original deadlines or quota; inactive callers are skipped and can rejoin. The version-two writer safely reads legacy waiters/markers; rollback must preserve rows and use a compatible build. Existing storage envelopes and role grants suffice. Broader admission/load qualification and opt-in registry integration remain before activation.

## P2.3 atomic retained reply admission — #119

[Issue #119](https://git.subcult.tv/subculture-collective/overlaid/issues/119) adds [atomic same-source reply batches](../../runbooks/reply-batch-admission.md) without registering a sender. The [cutover plan and caller inventory](2026-10-16-outbound-registry-cutover.md) separate source-bound registry replies, Newsroom current, manual announcements and overlay-only timers. Single-action identities, shared admission bounds, no partial enqueue and post-lock original-deadline checks remain. Source collection, non-chat source contracts, one-sender/controller guards, lifecycle/readiness and opt-in activation remain.

## P2.3 saved registry command snapshots — #121

[Issue #121](https://git.subcult.tv/subculture-collective/overlaid/issues/121) carries [saved IDs and tenant-scoped snapshot fingerprints](../../runbooks/registry-command-snapshots.md) through accepted processor events. A fresh Newsroom registry read rejects missing/stale metadata before effects while preserving the existing parser, roles, cooldowns and response modes. This supplies metadata for source-bound reply collection but does not provide an atomic admission revision fence, sender/controller integration or startup activation.

## P2.3 source-bound registry reply collection — #123

[Issue #123](https://git.subcult.tv/subculture-collective/overlaid/issues/123) adds an explicit [reply collector](../../runbooks/registry-reply-collection.md) through the existing retained dispatcher and central chat-send boundary. Actual custom and Newsroom current replies use canonical stored sources, saved snapshot/effect keys and one atomic admission batch. Unbound callbacks fail without provider fallback when explicitly selected. No startup path selects this boundary. Transactional registry binding, manual source contracts, one-sender/controller validation, readiness/lifecycle, rollback, opt-in activation and live/load acceptance remain.

## P2.3 transactional registry reply binding — #125

[Issue #125](https://git.subcult.tv/subculture-collective/overlaid/issues/125) adds [saved-command checks inside atomic reply admission](../../runbooks/registry-admission-binding.md). New reply effects require current enabled snapshots and command entitlement under the tenant/command locks through commit. Tenant locking precedes the plan read, preventing concurrent reassignment join loss. Existing keys/noops and mixed batches remain compatible. No migration or role grant is added. Manual sources, controller/readiness/lifecycle/rollback, opt-in activation and live/load acceptance remain.

## P2.3 explicit manual announcement sources — #127

[Issue #127](https://git.subcult.tv/subculture-collective/overlaid/issues/127) adds a [separate manual source and atomic outbound admission](../../runbooks/manual-outbound-sources.md) with migration 0026. Fresh configure permission, actor/request-key identity, immutable payload conflicts, bounded fixed deadlines and existing payload-free queue jobs remain behind explicit construction. Subject-viewer/retention/member/tenant lifecycle cascades remove private input; exports expose fixed metadata. Existing chat actions, deadlines/claims and job identities remain intact. HTTP/UI queued acknowledgements and content binding, one-sender/controller/readiness/rollback integration and opt-in activation remain.

## P2.3 manual announcement request/UI contract — #129

[Issue #129](https://git.subcult.tv/subculture-collective/overlaid/issues/129) adds [server-projected previews, locked Newsroom content admission and saved unresolved browser intents](../../runbooks/manual-announcement-intents.md). Retained retries only read receipts; changed content, missing/purged receipts and sender-mode/scope changes cannot trigger fallback or new admission. Queue acknowledgements stay distinct from delivery, and direct ambiguity requires explicit resolution. Newsroom writers use the same tenant-before-document lock order. No migration or startup selection is added. One-sender/controller, pool/readiness/lifecycle/rollback integration and opt-in activation remain.

## P2.3 controller ownership for managed writers — #131

[Issue #131](https://git.subcult.tv/subculture-collective/overlaid/issues/131) closes an existing writer bypass in the [chat controller](../../runbooks/retained-twitch-chat.md). EventSub-disabled managed applications still have manual chat endpoints, so they now acquire the same ownership lease and honor the retained-ingestion guard. Actual-lock checks fence central sends and new manual/registry admissions; observed loss cancels tenant effects and degrades readiness. Separate outbound/pacing/execution pool validation, opt-in sender startup and broader lifecycle/load qualification remain.

## P2.3 opt-in retained chat startup — #133

[Issue #133](https://git.subcult.tv/subculture-collective/overlaid/issues/133) composes the existing registry/manual admission, selected Twitch authorization, executor and restricted outbound client under the [same managed controller](../../runbooks/retained-chat-sender.md). Retained sending is explicit and validates separate worker/pacing/optional shared-vault pools and compatible state before gated leasing. It persists an installation guard, rejects direct fallback and binds effects to controller cancellation; readiness and shutdown retain fixed, bounded contracts. Synthetic real-River fixtures qualify registry/manual delivery, invalid startup without consumption, duplicates, queued versus delivered acknowledgement, durable echoes, preserved pacing, uncertain restart and role drift. Broader lifecycle/revocation/pause/cooldown/mixed-load and live/badge acceptance remain under #24; the phase stays open.

## P2.3 concurrent controller checks and queued lifecycle — #135

[Issue #135](https://git.subcult.tv/subculture-collective/overlaid/issues/135) fixes a pgx panic reproduced by concurrent ownership checks on the pinned controller connection. A cancellation-aware gate serializes each complete query/scan pair and shutdown unlock within the existing check budget. Real-River application fixtures hold an authorized, paced attempt before its claim: connection revocation prevents the claim, same-subject reauthorization fails the post-claim binding check, and source deletion cascades the action without transport. Charged pacing remains. The existing registry suppresses a command disabled before dispatch and a second input within cooldown. These fixtures do not prove retraction of admitted snapshots, persistent command cooldowns across runtime replacement, or cancellation of an already-started provider effect. Selected own/shared identity lifecycle and mixed-load/deadline qualification remain; the current 100-active-runtime cap also bounds application capacity. #24 remains open.

## P2.3 selected own/shared sender lifecycle — #137

[Issue #137](https://git.subcult.tv/subculture-collective/overlaid/issues/137) extends the [application lifecycle fixtures](../../runbooks/retained-chat-sender.md) to selected own/shared bot delivery. Transport checks the exact selected subject/channel, credential and running claim; both authorizations perform fresh moderator checks, and successful delivery retains an echo marker. Pre-claim own-bot revocation/reauthorization and independent shared consent, publication, service-grant and service-credential changes reject delivery after the claim without broadcaster fallback. Charged attempts remain and permits complete. This is synthetic real-River application evidence, separate from provider-side revocation timing, live role grants, already-started provider cancellation and badge qualification. Mixed-load/deadline and capacity work remain under #24.

## P2.3 cooldown isolation and original-deadline expiry — #139

[Issue #139](https://git.subcult.tv/subculture-collective/overlaid/issues/139) adds a bounded two-tenant application/River fixture mixing provider cooldown, an aged queued reply, another sender's fresh reply, duplicate input and overlay-only commands. It exposed a cancelled-job/pending-ledger gap at fractional deadlines. Pending-only expiry bookkeeping now applies the conservative whole-second job bound using database time and exact tenant/action/deadline identity, with no claim or deadline rewrite. The fixture verifies progress for the other sender, terminal jobs/ledger, unchanged original timestamps and no extra transport, pacing charge or permit. Broader load, the 100-active-runtime cap, application moderation priority and full thousand-channel acceptance remain open under #24.

## P2.3 runtime preparation before the dispatch fence — #141

[Issue #141](https://git.subcult.tv/subculture-collective/overlaid/issues/141) fixes capacity failures consuming an unexecuted command's durable handler fence. An explicit trusted preparation callback initializes and binds the tenant runtime before a new start; failure uses bounded pre-handler retries with no receipt. Locked reconstruction still validates the canonical source and refuses an unprepared new start. Terminal receipts/sources retain their no-replay path, and invalidation cancels the bound runtime. Fixed readiness exposes observed capacity pressure. A real-River fixture uses synthetic map entries and explicitly promotes a retry after releasing a slot; it proves neither natural scheduler timing nor a 100-channel load. The bound remains 100. Capacity expansion and broader load, moderation-priority, event/transport/onboarding and live acceptance remain open under #24.

## P2.3 configurable and actual runtime bounds — #143

[Issue #143](https://git.subcult.tv/subculture-collective/overlaid/issues/143) adds explicit `TENANT_RUNTIME_LIMIT` selection from 1 through 1000 while retaining the default of 100. Invalid explicit input fails startup; admission and readiness use the selected bound. The [actual-runtime fixture](../../runbooks/runtime-capacity.md) constructs 100 and 1000 real tenant runtimes with normal per-tenant background workers, retained overlay-only commands and duplicate inputs, then verifies scoped counters, exact terminal receipts/jobs, overflow preparation without a fence, recovery after explicit retry promotion, shutdown and fresh controller reacquisition. A finite local 1000-runtime race run recorded 3.73 s startup, 25.57 s command processing and 83.5 MiB sampled Go heap. This is runtime/command evidence only; live EventSub connections, provider delivery/pacing, global retention, moderation-priority and full thousand-channel acceptance remain open under #24.

## P2.3 global resumption and retention — #145

[Issue #145](https://git.subcult.tv/subculture-collective/overlaid/issues/145) exercises normal application-wide startup and populated restart for exactly 1000 configured tenant runtimes. Its [fixture contract](../../runbooks/runtime-capacity.md) requires the initial retention sweep to apply mixed one-day/365-day policies, cascade expired chat input, native payloads, receipts and jobs and preserve in-policy input. Real River commands before and after restart check duplicate suppression, isolated counter values, terminal jobs, readiness and shutdown. The fixture forbids provider requests and creates no outbound actions. This expands the earlier direct-construction evidence; live connection/delivery/shared pacing, natural periodic sweeping, populated editorial histories, moderation priority and full thousand-channel acceptance remain open under #24.


## P2.3 shared sender cooldown and restart — #147

[Issue #147](https://git.subcult.tv/subculture-collective/overlaid/issues/147) combines selected shared custody with multiple consenting tenant bindings, a mocked provider quota response, durable pacing denial, application restart and an independent broadcaster delivery. The [fixture contract](../../runbooks/retained-chat-sender.md#shared-cooldown-across-tenant-bindings-and-restart) requires the original queued action/job/deadline and account cooldown to survive restart, then expire without a claim or additional pacing charge. Exact transport bindings, no replay, tenant-specific echoes and terminal jobs remain required. This is a finite three-tenant case; natural post-cooldown recovery, saturation throughput, continuous-load fairness, moderation priority and full thousand-channel delivery acceptance remain open under #24.


## P2.3 natural shared cooldown recovery and finite saturation — #149

[Issue #149](https://git.subcult.tv/subculture-collective/overlaid/issues/149) extends the shared-sender restart fixture to drain 24 replies across two consenting tenants after an eight-second mocked provider cooldown. Its [fixture contract](../../runbooks/retained-chat-sender.md#natural-shared-cooldown-recovery-and-account-window-saturation) requires natural scheduling, preserved original queued identity/deadline, an independent broadcaster delivery, exact charge/transport observations, a full 20-attempt rolling account window and natural replenishment. Duplicate inputs, no quota-action replay, isolated echoes, terminal jobs and released permits remain checked. The saturation case uses unlimited fixture plans to isolate account pacing: a diagnostic free-plan run exhausted the separate 120 HTTP calls per hour allowance through fresh authorization retries and failed closed. Hosted-plan HTTP headroom remains to be measured. This is finite saturation evidence; continuous hostile arrivals, broader tenant/channel throughput, moderation priority and live-provider acceptance remain open under #24.


## P2.3 provider HTTP headroom during pacing waits — #151

[Issue #151](https://git.subcult.tv/subculture-collective/overlaid/issues/151) adds metadata-only, read-only pacing inspection before expensive provider authorization in explicit retained sending. The [runtime contract](../../runbooks/retained-chat-sender.md#avoiding-provider-work-during-known-pacing-waits) keeps one-second short snoozes and original deadlines, with no hint-based permit, claim, credential access or authorization. Actual sends still recheck providers before pacing and after claim. The natural shared burst now uses ordinary free plans and records bounded HTTP usage instead of masking retry costs with unlimited plans. The plan-limit receipt/UI continuation is tracked in #153; broader continuous-load headroom and moderation priority remain open under #24.

## P2.3 local HTTP allowance receipt classification — #153

[Issue #153](https://git.subcult.tv/subculture-collective/overlaid/issues/153) adds chat-only `plan_limited` receipts, preserves the classification before pacing, after claim and immediately before transport, and keeps provider quota/permission failures distinct. It adds migration 0027 and a retained-startup compatibility guard. Manual Newsroom status reads expose fixed failure metadata; the existing interface explains local allowance exhaustion without replaying the request. Adapter, actual application/River, schema and browser fixtures verify the bounded behavior. Continuous arrival load, broader tenant/channel throughput, moderation priority and live-provider acceptance remain open; no plan limit or automatic retry policy changes.

## P2.3 sustained uneven arrivals and fair-turn preparation — #155

[Issue #155](https://git.subcult.tv/subculture-collective/overlaid/issues/155) adds a [sustained shared-sender workload](../../runbooks/retained-chat-sender.md#sustained-uneven-arrivals-and-fair-turn-preparation) with 24 hot-tenant arrivals over roughly 36 seconds, six quieter-tenant arrivals, the original queued reply and independent broadcaster probes during shared backlog. The baseline exhausted the hot tenant's free HTTP allowance through authorization before unqualified fair-turn deferrals. Explicit `PrepareFair` now registers/refreshes bounded hash-only waiting turns without a permit, charge, claim or provider access; real authorization and `AcquireFair` remain authoritative. Existing read-only inspection/hint constructors, original deadlines, caller liveness and no-replay semantics remain. Qualification checks progress before arrivals stop, natural drain, global rolling bounds, allowance usage, exact receipts/echoes and cleanup. This finite workload does not close continuous hostile-load, moderation priority, thousand-channel or live-provider gates under #24.

## P2.2 selected shared moderation during retained chat load — #157

[Issue #157](https://git.subcult.tv/subculture-collective/overlaid/issues/157) joins the real shared-identity adapter, typed delete transport and restricted moderation River worker with the actual retained chat application in a [finite mixed-lane fixture](../../runbooks/moderation-priority.md#selected-shared-identity-with-retained-application-chat). Delivery and quota cases settle moderation while a chat request is held and sixteen retained chat inputs remain unfinished with only one charged slot. Chat drains after release; typed private receipts, separate cooldowns, duplicate admission, fixed job/attempt/deadline, client restart and per-tenant echoes/allowances remain checked. The fixture explicitly owns moderation construction. It adds no startup registration, admission route, provider contract, role or migration; controller/lifecycle activation, broader fairness/scale and live-provider acceptance remain open under #24.

## P2.2 moderation lifecycle binding foundations — #159

[Issue #159](https://git.subcult.tv/subculture-collective/overlaid/issues/159) adds an [optional pre-claim attempt gate and unregistered application wrappers](../../runbooks/moderation-executor.md#explicit-lifecycle-gate). Failed gates cannot claim or attempt; charged permits remain charged and terminal cleanup errors cannot replay. Wrappers require the retained controller/startup barrier, current tenant runtime and original deadline, cancel with app/tenant lifecycle, and reject saved closures after invalidation. They do not take chat's echo mutex; the composed shared transport fixture retains progress under held chat. Default constructors/startup are unchanged. Moderation-specific pool-role/readiness/controlled leasing/shutdown integration and opt-in registration remain next, with broader fairness/scale and live-provider gates still open under #24.

## P2.2 controlled moderation leasing — #161

[Issue #161](https://git.subcult.tv/subculture-collective/overlaid/issues/161) adds an explicit [controlled client](../../runbooks/moderation-worker.md#controlled-leasing) using the existing shared leasing gate. Closed readiness preserves pending jobs, attempts, original deadlines, claims and pacing; both constructors still reject unsafe roles/policies/identity. Real River verifies activation, closure during an already claimed attempt, pending work through client replacement and one delivery per action after reopening. The callback is only a fetch barrier: execution cancellation and authorization retain their own checks. Existing construction and application startup are unchanged. Moderation-specific pool/readiness/shutdown ownership and opt-in startup/admission remain before activation; broader fairness/scale and live-provider gates remain under #24.

## P2.2 moderation pacing activation preflight — #163

[Issue #163](https://git.subcult.tv/subculture-collective/overlaid/issues/163) adds [repeatable role checks and read-only retained-state validation](../../runbooks/moderation-pacing.md#read-only-activation-preflight). `Open` reuses `Validate`, which also rejects column-only private-table grants. Explicit `CheckState` bounds sender/channel/client counts, reads supported legacy/current fair formats and requires moderation resource pairs without pruning or rewriting expired state, leases, cooldowns or timestamps. PostgreSQL race fixtures cover active-pool drift, populated-state preservation, invalid formats/resources, capacity and invalid/closed/canceled pools. Neither check grants a permit or effect permission. Moderation-specific application pool/readiness/shutdown ownership and opt-in startup/admission remain next, with broader fairness/scale and live-provider gates open under #24.

## P2.2 explicit moderation consumer ownership — #165

[Issue #165](https://git.subcult.tv/subculture-collective/overlaid/issues/165) composes an [explicit application-owned consumer](../../runbooks/moderation-consumer.md). Preparation owns separate queue/pacing pools while borrowing product/shared execution dependencies, validates roles/state/identity and leaves leasing closed. Explicit activation publishes named readiness and adds fresh execution guards plus idle role/controller/worker monitoring. Cancellation retains exact tenant/action binding without chat echo locking. Shutdown stops the worker and monitor before closing pools; a timed-out stop retains dependencies and controller ownership for retry. Real-River synthetic transport fixtures cover delivery/restart, unsafe preparation/start, drift, pre-claim and saved-invocation guards, borrowed pool ownership and shutdown retry. Default startup, mode guards and moderation admission cutover remain next; no environment setting, production migration or live effect is introduced.

## P2.2 explicit source-bound moderation admission — #167

[Issue #167](https://git.subcult.tv/subculture-collective/overlaid/issues/167) adds an [explicit admission bridge and effect-path inventory](../../runbooks/moderation-admission.md). The ready consumer resolves canonical retained input through immutable tenant scopes, requires the exact live runtime and maps existing private requests into typed source/key identities. The ledger preserves original deadlines, active connection/target checks, shared bounds, immutable conflicts and atomic action/jobs. Real-River fixtures verify broadcaster/shared delivery, terminal duplicates and refusal without new work or provider requests. No existing effect path selects this bridge yet. Durable operator/batch occurrences, automatic-rule source binding, asynchronous status, coordinated all-path cutover and guarded opt-in startup remain before activation. Broader fairness/scale and live-provider gates remain open under #24.

## P2.2 automatic moderation binding and direct-owner handoff — #169

[Issue #169](https://git.subcult.tv/subculture-collective/overlaid/issues/169) adds [routing ownership and actual retained rule binding](../../runbooks/moderation-routing.md). Explicit preparation drains direct moderation before installing a consumer, under its existing bounded startup budget. Once installed, even prepared/stopped owners block direct/dashboard/batch fallback, with nuke refusal before radiation creation. Automatic/radiation rules bind the actual retained projection and saved rule key to admission; audit records queued acceptance while typed ledger completion remains separate. Admission failure reaches the durable handler receipt without replay. Real-River fixtures cover delivery/duplicates, changed-payload conflicts, effect-free rules, unbound projections, failed dispatch and actual direct-flight handoff/cancellation. Default startup still installs no owner. Durable operator/batch occurrences, asynchronous owner status and guarded all-path startup remain before activation; broader fairness/scale and live-provider gates remain under #24.

## P2.2 durable operator and batch moderation sources — #173

[Issue #173](https://git.subcult.tv/subculture-collective/overlaid/issues/173) adds the [internal operator-source store](../../runbooks/manual-moderation-sources.md), migration 0028 and runtime compatibility checks. Authenticated membership, stable request identity, sorted keyed fingerprints, fixed database-clock deadlines and bounded atomic source/actions/jobs apply to single requests and batches up to 50. Reordered duplicates retain IDs; conflicts and lifecycle deletion cannot cause replay. PostgreSQL fixtures cover permissions, isolation, whole-batch rollback, combined limits, lifecycle/export privacy, existing chat migration preservation and real River delivery through synthetic transport. Operator HTTP/nuke wiring, asynchronous owner status and guarded all-path startup remain next. No default activation or live provider effect is introduced; broader fairness/scale and live-provider acceptance remain under #24.

## P2.2 single operator moderation HTTP and status — #175

[Issue #175](https://git.subcult.tv/subculture-collective/overlaid/issues/175) connects [single-action HTTP/UI requests](../../runbooks/operator-moderation-requests.md) to the durable operator store under explicit consumer ownership. Trusted sessions, exact account/workspace/channel/mode binding and live controller/runtime guards bound new admission. Receipt-only retries cannot reconstruct missing/purged work and remain readable after consumer stop. The browser persists exact input and request UUID before submit, retains pending/uncertain/lost/malformed outcomes through reload, and distinguishes acceptance from delivery. Actual authenticated PostgreSQL/River fixtures use synthetic selected identities; browser state and Chromium fixtures cover the user flow. Nuke-batch target/radiation binding and guarded configured startup remain next; no production migration, default activation or live effect is introduced.

## P2.2 direct moderation receipt compatibility — #177

[Issue #177](https://git.subcult.tv/subculture-collective/overlaid/issues/177) repairs the single-action browser path to recognize the backend's actual `accepted`/`twitch_helix` direct receipt. It keeps failed, queued, malformed and audit-only `allowed` results unresolved. A shared fixture binds actual Go HTTP responses to browser unit and Chromium checks, including saved-intent cleanup through reload. Retained behavior, nuke-batch binding and configured startup scope are unchanged.
