# Missing-input fallback approval (#47)

## Existing boundaries

The engine validates immutable schema-2 graphs and produces reproducible supplied-input traces. The bounded OBS consumer now uses retained typed proposals and the configured application mutation owner. Existing OBS routes in `backend/internal/http/obs_handlers.go` share that mutation gate. Managed runtimes clear OBS configuration in hosted mode. Automation control requires account sessions, configure permission, CSRF, tenant scope and the actual application controller for writers. These boundaries remain in place.

This feature targets managed self-host installations with an explicitly configured OBS connection and an authenticated workspace. Hosted installations retain disabled OBS transport. File-mode engine activation needs its own persistence/ownership design under the accepted Phase 3 plan; it is not inferred from a global operator secret.

## Implementation sequence

1. Add the minimal engine OBS scene proposal contract consumed by #47. Bind exact graph/version, collection, configured connection, observation identity/time and target. Model missing, present, muted, hidden, disconnected, stale and unknown separately. Rehearsal returns the same explained proposal from saved or synthetic input and has no provider callback.
2. Add retained tenant-scoped rule versions, original observations/proposals and one-use effect receipts. Reuse immutable engine version/graph ownership rather than a second scripting language. Persist one configured rule per workspace and bound proposal/receipt history. Define exact forced-RLS grants, tenant/automation/member cascades, metadata exports, schema compatibility and migration rerun behavior before adding migration 0049. Fresh tenant configure checks and controller ownership remain outside pure conditions.
3. Add the owned OBS observation/approval adapter through a shared server-side engine effect boundary. An enabled rule observes the configured collection/input and creates one explained proposal for a fresh missing-input transition. Read failure or ambiguous evidence creates no actionable proposal. Approval rechecks rule/version, collection, connection, target, observation freshness and current configure permission. Commit a hashed one-use claim before one bounded scene-change attempt. A post-claim timeout or unknown outcome is terminal and cannot replay. Persist cooldown/deduplication and refuse a second owner; stop observation/attempts before releasing product/controller dependencies.
4. Add account-scoped configure/read/rehearse/approve/dismiss/override transport and dashboard controls. Freeze the original proposal/version and override before submission. Unknown approval results recover through read-only original identity; no automatic fresh request key or direct OBS fallback. Disabling the rule invalidates pending proposals and blocks new claims. Display observation, timestamp, version, proposed scene and fixed action receipt separately.
5. Verify condition classification, deterministic rehearsal, stale/refused approvals with zero scene changes, claim races, duplicate/restart/cooldown behavior, tenant/role isolation, override, disable and cancellation. Use fake OBS WebSockets and disposable PostgreSQL only. Run `make verify`, the full PostgreSQL/race gate and browser fixtures at desktop/narrow sizes, then exact-head hosted CI before merge. Document safe test-collection qualification linked to #40; real streaming/OBS mutation is excluded from fixtures.

## Completion criteria

Every checkbox in #47 must have a passing named test or explicit live-qualification limitation. A pure proposal, saved rule or successful provider read does not authorize an effect. The feature PR remains incomplete until the configured rule, explanation, approval controls, durable at-most-once scene attempt and recovery receipt work together. #25 and #40 remain open for their wider engine and live-device acceptance.

## Requirement evidence

| #47 requirement | Named evidence |
| --- | --- |
| Versioned scoped rule, disable and restart | `TestOBSFallbackAtomicRuleVersionsAndDisable`, `TestOBSFallbackControlExplainsApprovesAndRecovers` |
| Fresh missing condition; suppressed/lost/unknown distinctions | `TestRehearseConditionsAndFreshness`, `TestFallbackSnapshotClassifiesReadOnlyEvidence`, `TestFallbackSnapshotRejectsIncompleteOrChangingEvidence` |
| Approval/override and fresh refusal with no scene request | `TestOBSFallbackClaimFreshGuards`, `TestOBSFallbackPostClaimRevocation`, `TestOBSFallbackControlFreshRefusalsAndDisable`, `TestFallbackSceneAttemptRechecksAndSendsOnce` |
| One use, original deadline, cooldown and owned cleanup | `TestOBSFallbackClaimRaceAndDetachedReceipt`, `TestOBSFallbackClaimRechecksOriginalDeadlineAfterLock`, `TestOBSFallbackOwnerCloseRetainsDependencies` |
| Replay/rehearsal and fixed private receipts | `TestReplayBindingAndSyntheticIsolation`, `TestOBSFallbackObservationEpisodesRecoveryAndPrivacy`, the no-RPC replay assertion in the HTTP workflow, `obsFallback.test.ts` |
| Browser recovery, scope, stale state and narrow controls | Six tests in `web/e2e/obs-fallback.e2e.ts`, including lost/malformed response recovery without another approval POST |
| Safe real OBS qualification | [The runbook](../../runbooks/obs-fallback.md#controlled-obs-qualification-for-40), linked to #40; actual provider qualification remains separate |

## Verification

- Full `scripts/ci-postgres.sh` passed on backend checkpoint `7880327`: `make verify`, the uncached PostgreSQL application race suite and the isolated River race suite. Normal HTTP: 606.503s; race HTTP: 721.087s. Subsequent edits changed frontend/rehearsal controls and documentation, with no backend change.
- Final `make verify` passed after those UI fixes. All 244 frontend unit tests pass, including eight approval/receipt tests with precise sub-millisecond deadline boundaries. Focused Go vet and lint reported zero findings.
- The complete Chromium suite passed 76 tests; the six fallback tests were rerun after the compact layout and timestamp fixes and passed. Shared-browser inspection found and fixed stale development-remount abort notices. Desktop/narrow layouts fit without horizontal overflow; the UI scanner reports no findings on the two changed route files.
- Android 15 Chrome loaded the fixture desk, scrolled its controls and replayed original saved evidence. This is a fixture/device check, not a real OBS qualification. No test used a live Twitch or OBS transport.
- Exact PR-head hosted CI and merge remain required. A local pass alone does not close #47 or qualify a live release.

## OBS protocol evidence

The [official OBS WebSocket protocol](https://github.com/obsproject/obs-websocket/blob/master/docs/generated/protocol.md) defines the collection/input/scene reads, source activity and mute fields used by the adapter. Missing here means absent from the complete input list; it does not detect a black camera frame, silence or a media-file error. The [official GetInputMute implementation](https://github.com/obsproject/obs-websocket/blob/master/src/requesthandler/RequestHandler_Inputs.cpp) returns status 604 for inputs without audio. That response can accompany confirmed input presence, but never qualifies absence. No upstream implementation code was copied.
