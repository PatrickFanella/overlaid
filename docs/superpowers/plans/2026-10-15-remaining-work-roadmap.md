# Remaining-work roadmap

Tracking: [#34](https://git.subcult.tv/subculture-collective/overlaid/issues/34). Baseline: merged main `a2f9841efa83582998a676eec1f12213a7652490`, after PR #83, on October 15, 2026.

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
| P2.3 | Opt-in outbound integration through the existing shared registry. Persist actions/jobs once and select one active sender/controller. Add readiness, explicit configuration and a documented rollback boundary. | P2.1–P2.2 and compatibility fixtures for every existing reply-producing path | Legacy and retained senders cannot both execute; controller loss/role failure degrades without fallback. Restart, duplicates, known echo IDs, manual broadcaster commands, Newsroom requests, pause/cooldown and cancellation retain behavior. |
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

1. P2.1 shared-vault execution is merged in [PR #86](https://git.subcult.tv/subculture-collective/overlaid/pulls/86), closing #85; see the [shared execution runbook](../../runbooks/shared-bot-execution.md). P2.2 is in progress: visibility (#87 / PR #88) and the moderation contract (#89 / PR #91) are merged. The retained moderation ledger (#93 / PR #94) is merged. Atomic scheduling and restricted leasing (#95 / PR #96), typed transport (#97 / PR #98), selected identity authorization (#99 / PR #100), moderation pacing (#101 / PR #102) and the fenced executor (#103 / PR #104) are merged. The explicit moderation worker (#105 / PR #106) is merged. Priority metadata and mixed-load lane isolation (#107 / PR #108) are merged. Tenant-fair leasing (#109 / PR #110) is merged. Pacing role validation (#111 / PR #112) is merged. Chat permit fairness (#113 / PR #114) is merged. Moderation permit fairness (#115 / PR #116) is merged. Active-caller eligibility (#117 / PR #118) is merged. Source-bound reply batches (#119 / PR #120) are merged. Saved registry snapshot metadata is tracked under #121; broader admission qualification and opt-in registry integration remain unfinished.
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
