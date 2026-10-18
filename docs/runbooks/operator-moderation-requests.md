# Single operator moderation requests

Tracking: [#175](https://git.subcult.tv/subculture-collective/overlaid/issues/175), under [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24). This connects the Chat page's single-action form to the [durable operator-source store](manual-moderation-sources.md) when an explicit [moderation consumer](moderation-consumer.md) is installed. No environment switch or default moderation startup is introduced. Nuke batch planning/radiation and persisted activation/downgrade guards remain unfinished.

## HTTP and identity

`GET /api/bot/moderation/action` returns sending mode, account, workspace and connected channel metadata under the existing operator/session permission boundary. `POST` binds those exact fields plus a stable request UUID to the selected typed operation. The trusted actor comes from request authorization; a body account ID cannot grant access. Hosted calls require the existing account session, workspace membership, origin and CSRF checks. The ledger rechecks moderate membership for both admission and receipt lookup.

New retained work requires the exact live tenant runtime, a ready explicit consumer, controller ownership and validated queue/pacing/product boundaries. Consumer and tenant cancellation bound admission to five seconds. Source, action and job still commit together; deletes require retained targets. Repeating identical input returns the original metadata; changed payload conflicts. Queue acceptance uses HTTP 202 for pending/running, while delivered/rejected/uncertain outcomes use HTTP 200 and typed metadata receipts. Fresh admission records `moderation_queued` audit evidence, never provider delivery.

A POST with `retry:true` only calls the read-only lookup. Missing or lifecycle-purged receipts return a fixed unresolved error and cannot reconstruct a source or enqueue work. Receipt lookup remains available after the consumer stops, without reopening pools or authorizing transport. An account/workspace/channel or mode mismatch refuses the request. Existing direct callers keep their synchronous path when no consumer is installed; an installed consumer never falls back to direct transport. Direct retries cannot be checked or replayed automatically.

## Browser handling

Before submitting, the Chat page saves the exact action draft, mode, connection and request UUID in session storage scoped to API origin, workspace and account. Failure to save prevents submission. Concurrent clicks share one request. Lost responses, pending/running status, malformed receipts and uncertainty retain the saved request through reload; subsequent clicks check only its receipt and ignore any new form draft. Confirmed delivery/rejection clears the request. Errors cannot create a fresh UUID automatically.

The form distinguishes queued acceptance from delivery and exposes an explicit acknowledgment to clear an unresolved request after checking Twitch. Clearing does not cancel queued work. Account/workspace changes fence stale responses and use separate saved requests. Browser storage contains the private moderation draft, including target/reason, until resolved or explicitly cleared; it contains no provider credential or operator secret.

## Verification and remaining cutover

`TestOperatorModerationHTTP*` uses actual account/session/CSRF routing, PostgreSQL and River with synthetic broadcaster/shared transport. It checks old retained targets, prepared refusal, running/terminal receipts, stopped lookup, payload conflicts, purged records, forged context, editor/CSRF refusal, lost controller and stale runtime with no refused effects. Existing automatic-rule and routing tests remain in the focused gate.

`moderationIntent.test.ts` covers saved-before-submit behavior, response loss/reload, receipt-only retries, malformed/uncertain results, concurrent clicks, browser storage failures, scope isolation and direct ambiguity. The existing Chromium moderation regression checks draft preservation, receipt-only resolution and visible rejected status. An isolated rendered-page check also covers pending status through reload and terminal delivery. These browser fixtures intercept API responses; they do not establish live provider behavior.

Run `GOTOOLCHAIN=go1.26.0 bash scripts/ci-postgres.sh` for the full local gate. Nuke previews must next bind their exact target set and radiation mutation to a stable batch occurrence before batch cutover. Default startup still installs no moderation consumer; broader fairness/scale and controlled live-provider acceptance remain under #24.
