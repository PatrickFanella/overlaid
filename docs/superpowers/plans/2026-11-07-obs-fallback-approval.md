# Missing-input fallback approval (#47)

## Existing boundaries

The engine validates immutable schema-2 graphs and produces reproducible supplied-input traces. Retained execution currently admits chat text only. OBS routes in `backend/internal/http/obs_handlers.go` call the configured `obsws.Client` directly. Managed runtimes clear OBS configuration in hosted mode. Automation control requires account sessions, configure permission, CSRF, tenant scope and the actual application controller for writers. These boundaries remain in place.

This feature targets managed self-host installations with an explicitly configured OBS connection and an authenticated workspace. Hosted installations retain disabled OBS transport. File-mode engine activation needs its own persistence/ownership design under the accepted Phase 3 plan; it is not inferred from a global operator secret.

## Implementation sequence

1. Add the minimal engine OBS scene proposal contract consumed by #47. Bind exact graph/version, collection, configured connection, observation identity/time and target. Model missing, present, muted, hidden, disconnected, stale and unknown separately. Rehearsal returns the same explained proposal from saved or synthetic input and has no provider callback.
2. Add retained tenant-scoped rule versions, original observations/proposals and one-use effect receipts. Reuse immutable engine version/graph ownership rather than a second scripting language. Persist one configured rule per workspace and bound proposal/receipt history. Define exact forced-RLS grants, tenant/automation/member cascades, metadata exports, schema compatibility and migration rerun behavior before adding migration 0049. Fresh tenant configure checks and controller ownership remain outside pure conditions.
3. Add the owned OBS observation/approval adapter through a shared server-side engine effect boundary. An enabled rule observes the configured collection/input and creates one explained proposal for a fresh missing-input transition. Read failure or ambiguous evidence creates no actionable proposal. Approval rechecks rule/version, collection, connection, target, observation freshness and current configure permission. Commit a hashed one-use claim before one bounded scene-change attempt. A post-claim timeout or unknown outcome is terminal and cannot replay. Persist cooldown/deduplication and refuse a second owner; stop observation/attempts before releasing product/controller dependencies.
4. Add account-scoped configure/read/rehearse/approve/dismiss/override transport and dashboard controls. Freeze the original proposal/version and override before submission. Unknown approval results recover through read-only original identity; no automatic fresh request key or direct OBS fallback. Disabling the rule invalidates pending proposals and blocks new claims. Display observation, timestamp, version, proposed scene and fixed action receipt separately.
5. Verify condition classification, deterministic rehearsal, stale/refused approvals with zero scene changes, claim races, duplicate/restart/cooldown behavior, tenant/role isolation, override, disable and cancellation. Use fake OBS WebSockets and disposable PostgreSQL only. Run `make verify`, the full PostgreSQL/race gate and browser fixtures at desktop/narrow sizes, then exact-head hosted CI before merge. Document safe test-collection qualification linked to #40; real streaming/OBS mutation is excluded from fixtures.

## Completion criteria

Every checkbox in #47 must have a passing named test or explicit live-qualification limitation. A pure proposal, saved rule or successful provider read does not authorize an effect. The feature PR remains incomplete until the configured rule, explanation, approval controls, durable at-most-once scene attempt and recovery receipt work together. #25 and #40 remain open for their wider engine and live-device acceptance.

## Checkpoints

- Pure rule/observation rehearsal is committed as `01b0dea`; race tests pass with 92.1% package coverage and lint reports zero issues.
- The storage draft uses migration 0049, immutable engine publication in the same tenant transaction, independent disable revision, an original ten-second observation deadline, thirty-second proposal spacing, and min(storedEvents,1000) retained proposals. A claimed/dismissed episode will remain latched until confirmed present/muted/hidden input; disconnected/unknown evidence cannot clear it. Claims and transport are not implemented yet.
- Focused PostgreSQL races pass for the migration contract, atomic publication/rollback, disable/re-enable conflict, original-source lookup, bounded proposal episodes, role/tenant isolation, metadata exports, storedEvents refusal and repeated schema/grant checks. The existing publication race/audit regression also passes. Full application verification remains required after integration.
- Full verification of the separate #350 tree encountered an unrelated app-vault test timeout on disk-backed storage. Its earlier browser run passed 70 tests with #351 included. No local full-gate success is claimed.
