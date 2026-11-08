# Missing-input fallback approval

**Scenes → Missing-input fallback** saves one versioned rule per managed workspace. It observes one named input in the configured OBS scene collection, explains a fresh absence, and offers an explicit scene-change approval. Saving or observing a rule does not switch scenes.

Detection means the input is absent from a complete OBS input list. An input that still exists with black video, silence or a missing media file is outside this condition. Muted inputs, inputs outside program output, connection loss, changed/incomplete evidence and stale proposals have separate explanations.

## Installation and account access

Use a PostgreSQL-backed `DEPLOYMENT_MODE=selfhost` installation with migration `0049_obs_fallback.sql` and its predecessors. Configure `OBS_WEBSOCKET_URL` and `OBS_WEBSOCKET_PASSWORD` through the existing private environment. Hosted mode keeps OBS transport disabled; file mode does not expose this retained rule. Follow [migration and recovery](tenancy-and-recovery.md) for existing databases and [getting started](getting-started.md) for OBS and account OAuth setup.

The rule needs a real signed-in account with owner, manager or editor membership in the selected workspace. An operator header alone cannot create an approval identity. For the self-host bootstrap workspace, sign in once to establish the account, then grant it configure membership through the existing member controls in a separate operator-only browser session. Return to the signed-in session and select that workspace. `/api/account` reports the genuine browser session when an operator header is also present.

An OBS URL/password change changes the connection binding. Save a new rule version after reconnecting; an old proposal cannot authorize the new connection. The managed application’s existing database controller lease owns observation and execution. A second managed controller is refused, including installations with EventSub disabled.

## Configure and operate

1. In **Scenes**, enter the exact current collection, watched input and fallback scene names. The fallback scene must exist in the current collection. Names are limited to 255 UTF-8 bytes. Choose **Observe after saving**, then **Save new rule version**. Disabling and re-enabling require separate revision/version checks; an old save retry cannot re-enable a disabled rule.
2. Inspect the saved rule version and latest condition. An enabled rule checks the configured OBS connection about every two seconds. Read failures do not establish absence.
3. A fresh missing-input proposal shows its original observation time, ten-second deadline, rule version and proposed scene. A later observation never extends that proposal’s deadline. New proposals are spaced by at least thirty seconds.
4. Use **Replay original observation** to inspect the original reason and intended scene, including after expiry. **Rehearse supplied evidence** evaluates an explicitly synthetic condition. Both use saved/supplied inputs and perform no OBS read or mutation.
5. For an actionable proposal, optionally enter an exact override scene, then choose **Approve scene change** and confirm it. Approval rechecks the current input, collection, connection, selected scene, browser/workspace permission, rule, plan, original deadline and active controller. A valid override can replace an original fallback scene that has since disappeared.
6. Use **Dismiss proposal** to acknowledge the original pending episode without a scene request. A claimed or dismissed episode stays latched until input presence/mute/hidden evidence confirms the condition cleared. Unknown or disconnected evidence cannot clear it.

**Disable rule** is independent of an in-flight browser approval. It revokes pending proposals and blocks new claims without needing a live OBS connection or observer. It cannot recall a request already sent to OBS. New local/provider checks still run after the one-use claim and immediately before transport.

The rule uses the engine’s immutable version/graph storage and existing version bounds. Retained proposals are bounded by the workspace’s `storedEvents` allowance, capped at 1000. Rule versions retain the existing 32-version automation bound. A limit refusal creates no partial version or scene attempt.

## Receipts and uncertain outcomes

One approval can receive one durable start claim and make at most one bounded scene-change attempt. The scene method rechecks through the same connection used for its single request. Existing direct OBS mutations share the application’s mutation gate. No provider error is retried automatically.

| Receipt | Meaning |
| --- | --- |
| Awaiting approval | No attempt has started; the original deadline still applies. |
| Attempt started; outcome unconfirmed | A durable claim exists. Restart or repeated approval cannot send it again. |
| OBS acknowledged the scene change | OBS returned a correlated successful response for the selected scene. Later external OBS changes remain possible. |
| Refused | A pending source was invalidated, or a final guard refused transport. |
| Outcome unknown | Cancellation, connection failure or completion uncertainty followed the claim. Inspect OBS before another deliberate action. |
| Dismissed / expired | The original pending proposal is settled without approval. |

Before the approval POST, the desk stores the original proposal/version, request UUID and selected scene in tab storage scoped to backend, account and workspace. A lost response, reload, malformed receipt or failed lookup keeps this intent. **Check approval outcome** reads only the original proposal; it cannot send, repair or create work. A missing/purged result remains unresolved.

**Acknowledge and clear approval** changes tab bookkeeping only. It neither cancels transport nor proves an unknown outcome. An uncertain or previously started source never becomes replayable. Inspect OBS before choosing another action. Changing workspace/account aborts old UI requests and keeps their original scoped handles separate.

Receipts expose fixed status/failure fields and original identities, with no claim token, provider comment, password or connection URL. Tenant exports contain metadata only. Tenant/automation deletion cascades remove rule and proposal input. Removing the configuring member deletes its rule and proposals; removing an approving member deletes that member’s receipt. Immutable engine configuration versions remain tenant-owned under their existing lifecycle. Retention removes old proposals without resetting the durable episode latch. Shutdown drains observation, attempts and detached completion writes before releasing dependencies. A timed-out shutdown retains ownership and pools for cleanup retry.

## Controlled OBS qualification for #40

This procedure supports [live OBS acceptance #40](https://git.subcult.tv/subculture-collective/overlaid/issues/40). Automated tests use fake WebSockets and disposable PostgreSQL; they do not establish real OBS behavior or atomicity against another OBS client changing state between a read and a request.

1. Use a separate test OBS instance with no active stream, recording or replay output. Export the existing collection and record its name before changing anything. Create a disposable collection named **Overlaid fallback qualification** with scenes **Overlaid test program** and **Overlaid safe scene**. Add an expendable source named **Overlaid test input** to the program scene. Use an expendable local media source with audio when qualifying mute behavior. Do not use an active show, production source or third-party macro as the fixture.
2. Point only the test Overlaid installation at this OBS instance. Select the test collection in OBS. Sign in with a configure member and save a rule watching **Overlaid test input** with target **Overlaid safe scene**. Confirm that saving and the present-input observation leave the program scene unchanged.
3. Hide the test input, then restore it. Confirm a hidden/outside-program explanation and zero fallback attempts. Mute and unmute the test audio source; confirm mute is distinct from absence. Stop only the test OBS WebSocket service, then restore it; confirm connection loss/unknown evidence produces no actionable absence.
4. Rename only the disposable input to **Overlaid renamed test input**. Inspect the proposal’s original time/version/scene. Replay it and rehearse synthetic cases; neither operation should switch OBS. Let one proposal become stale and confirm approval is unavailable/refused without a scene change. Wait for a later eligible proposal rather than replacing an unresolved request key.
5. Approve one fresh proposal and confirm a single change to **Overlaid safe scene** with a successful receipt. Reload the desk and check that original approval; it must use lookup and make no second scene request. Restore the input name and program scene deliberately, then use a later fresh proposal to qualify a valid override scene. Record its original proposal/request identities and selected scene.
6. On a later fresh pending proposal, disable the rule and confirm approval is refused with no scene change. Verify duplicate requests, cooldown and restart recovery using the same original identity. If a network interruption makes an attempt uncertain, inspect OBS and preserve its original receipt; do not retry the effect or reset its database claim.
7. Disable the test rule, inspect any unresolved attempts, then restore the original collection using OBS. Preserve the exported collection and a dated metadata receipt for #40. Record OBS/WebSocket versions, application commit/migration, account role, each observed condition, selected scene, exact scene-request count if instrumented, restart result and remaining unqualified cases. Keep URLs/passwords, raw protocol captures and private input names out of shared evidence.

Stop observation/execution before rolling back a runtime. Preserve migration-0049 rows and original claims; do not delete receipts, reset the latch or rewrite a running/uncertain source to make an older build retry it. General automation dispatch, other OBS conditions and live-provider acceptance remain separate work.

## Developer checks

Run `make verify`, then the owned disposable PostgreSQL/race gate in [the README](../../README.md#development). Focused tests are `TestOBSFallback*` in tenancy/HTTP and `TestFallback*` in the OBS adapter. `web/e2e/obs-fallback.e2e.ts` covers the desk’s normal, lost, malformed, stale, scoped and narrow-layout workflows using intercepted APIs. None of these fixtures sends to a real OBS/Twitch instance.
