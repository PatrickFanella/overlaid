# Manual Newsroom announcement requests

Tracking: [#129](https://git.subcult.tv/subculture-collective/overlaid/issues/129), parent [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24). This connects the [manual-source foundation](manual-outbound-sources.md) to authenticated requests and the Newsroom interface. [Retained chat startup](retained-chat-sender.md) selects this contract explicitly; legacy sending remains the default.

`GET /api/newsroom/announce/{current|raffle}` prepares a read-only preview. It returns the current sending mode, tenant/actor scope and an opaque fingerprint of the server-projected message, connection, kind and subject viewer references. It does not refresh provider credentials, fetch the submitted page, create a source/job or send a message. Configure permission is required. Current-story announcements require the selected item to remain approved; raffle announcements use the selected winner or the currently enabled general-registry entry command.

The dashboard generates one UUID request key after preview, then saves the key/fingerprint/mode in session storage before its first POST. Storage is scoped by API endpoint, workspace, actor and announcement kind. It contains no message text, operator secret, provider token or session credential. A storage failure blocks POST. Overlapping clicks share one in-flight request. A preview with a different account/workspace is rejected; POST also checks the expected scope against freshly authenticated context, so an account/workspace change during a request cannot redirect that intent.

`POST /api/newsroom/announce/{kind}` accepts the saved mode, tenant/actor identifiers, request key, fingerprint and explicit `retry` flag. The identifiers are comparison fields, not authority: authentication, CSRF, route permission and fresh store permission still apply. Unknown fields and malformed bodies fail. A mode change fails without falling back to another sender. The legacy empty-body direct POST remains compatible, while the new interface uses preview binding for direct sends too.

## New retained admission

The initial request first looks for an existing matching receipt. Existing accepted work is acknowledged from its saved metadata even if the current desk has changed. A new request derives its text and viewer references on the server; callers cannot submit arbitrary announcement text. Its preview fingerprint must still match.

`AdmitNewsroomAnnouncement` holds the tenant assignment/permission, active connection, Newsroom document and any chosen raffle-entry command through the source/action/job commit. It rereads the approved selected story or raffle projection under those locks and rejects changes. Newsroom mutation transactions now take the tenant lock before their document lock, matching this order and avoiding an inverted dependency when an episode/capability insert references the tenant. File mode retains its existing single-writer mutex. This does not add a parser, command manager, migration or provider permission.

After admission, the handler rereads the actual receipt before responding. Pending/running work returns HTTP 202 with `queued: true`, `announced: false` and its saved state. Only `delivered` has `announced: true`; rejected/uncertain states are distinct. A successful commit followed by a lost status read remains unresolved in the browser. A queued acknowledgement never promises eventual delivery or extends the original deadline.

## Receipt checks and unresolved outcomes

A subsequent interface click sends the same key/fingerprint with `retry: true`. `LookupManual` checks the current configure permission and returns matching fixed metadata; it never admits, re-enqueues, reconstructs text or calls Twitch. A missing/purged source is unresolved and cannot be recreated by this path. Conflicting fingerprints or kinds fail. Changed desk content is irrelevant to the status of a previously accepted request.

A network error, invalid response, missing receipt or uncertain delivery preserves the saved intent across reloads in that tab. Direct transport has no retained receipt; an unresolved direct attempt is never retried automatically. The interface offers **Check receipt** and requires **I checked Twitch chat before clearing this request** before enabling **Clear unresolved request**. Clearing removes only local bookkeeping and does not send. A later explicit announcement click prepares a new intent. No error handler, reload or background timer generates a replacement key or sends again.

An acknowledged queued/delivered/rejected result clears the unresolved local intent. Pending work can still expire or be rejected later; subsequent delivery remains subject to the executor's authorization, pacing and claim fences. The session-storage record is a client continuation aid, not a secret capability or durable backup. A browser storage reset, deliberate user clear or lifecycle purge does not prove the prior effect failed. Real channel inspection remains necessary when resolving uncertainty.

## Verification and remaining activation work

Disposable PostgreSQL HTTP fixtures cover configure/CSRF/workspace boundaries, queue acknowledgements, changed previews, rejected current content, duplicate status reads, missing/purged sources, mode changes and zero retained provider transport calls. Storage fixtures reproduce a document change ahead of admission, an editor blocked on the admission backend until commit, and a disabled raffle command. Existing Newsroom tests/races exercise the new tenant/document lock order. A direct-mode fixture makes one synthetic transport attempt and refuses a retry.

Frontend fixtures cover lost responses, reload continuation, coherent queued/delivered/uncertain response validation, storage failure, scope mismatch and overlapping clicks. Browser verification with a synthetic saved intent in demo mode confirms reload restoration, acknowledgement-gated clearing, panel removal and no horizontal overflow at 1280 pixels. It does not qualify a live Twitch deployment. The full PostgreSQL verification/race gate includes 140 frontend tests.

The [retained sender controller](retained-chat-sender.md) supplies explicit startup selection, pool/role validation, readiness/shutdown and a persistent rollback guard. Broader lifecycle/load acceptance remains under #24. Default sending remains direct. Preserve the manual-source migration and compatible runtime when rolling back; do not discard claims or uncertain outcomes to recover an older binary.
