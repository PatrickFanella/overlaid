# Newsroom feature catch-up

Reviewed cutoff: PatrickFanella/newsroom PR #17 at `d7f66d201d5bf51c246c341f15d62bc7d37dafe7`, integrating PRs #1–#16. The user confirmed full feature catch-up on October 14. Base: Over-Laid main after PR #48.

1. Add independent shelf preparation settings/copies and segment targets. Verify fresh IDs, retained metadata/notes, empty segments, shared projection exclusion, and copying to the live deck without fetching.
2. Add owner usage estimates and transactional admission before AI/search calls, retaining failed attempts and explicit skipped jobs. Verify concurrent allowance checks, month resets, pause controls, and fail-closed provider dispatch.
3. Add transactional moderator activity and literal paginated archived-show search. Verify committed attribution without private text/tokens, conflict exclusion, detached history, and filter-bound cursors.
4. Add previewed per-show Discord exports with stable stored chunks/receipts, unused-story carry-forward, and explicitly published public episode projections. Verify fingerprints, fencing, idempotency, cleanup-safe copies, public allowlists, revocation, and tenant isolation.
5. Add opt-in Twitch boundaries through the existing authenticated EventSub WebSocket and owner correction controls. Verify stable event/stream IDs, timestamp watermarks, manual-show preservation, overlap rejection, and recalculated play offsets. Keep transport/session ownership in the existing Twitch client.
6. Complete dashboard/public flows and browser verification, run `bash scripts/ci-postgres.sh` (required make verify and full Go race suite), update runbooks/provenance, then publish a follow-up PR.

Retain single-writer file mode, existing forced-RLS tenant storage, vault-only provider credentials, and narrow independently revocable public capabilities. Do not register subscriptions, send messages, or call paid providers during fixtures. Public episode capability lookup requires an additive migration before the matching runtime; retain additive schema and a matching document checkpoint for rollback.
