# Workspace automation metadata API

[Issue #292](https://git.subcult.tv/subculture-collective/overlaid/issues/292) exposes the existing version, operator-admission, rehearsal and trace stores through managed account control. The endpoints publish configuration and retain metadata; they do not select an effect executor, command dispatcher, worker, provider adapter or graphical editor.

## Authentication and workspace scope

All operations use `POST /api/tenants/{tenantId}/automations/{operation}` with JSON bodies. They require an account session cookie, the exact configured web Origin, the session's `X-Overlaid-CSRF` header and fresh configure membership in the route's workspace. Owner, manager and editor roles may configure; moderator read access to other product surfaces does not grant this API access. Actor identity comes only from the authenticated session. Bootstrap/operator headers and overlay credentials cannot authorize these endpoints; the bootstrap tenant is refused. File-backed installations do not expose this API.

The URL selects the workspace, independently of the browser session's active-workspace selector. An account may explicitly address another workspace where it currently has configure permission. Version document tenant headers must match that route. Clients must retain the initiating account/workspace/request/version tuple when keeping an unresolved operator intent; switching the UI workspace must not retarget it. Never generate a replacement request UUID automatically after a timeout, error or missing receipt.

The control request is registered with the existing session expiry/revocation mechanism and bound to application shutdown, with a fifteen-second outer context. Store operations retain their own shorter bounds. Mutating metadata operations check actual ownership through the existing managed controller; this opens no additional pool and grants no effect permission. Losing that owner or canceling a session stops pending work through its request context. A lost write response still requires resolving the original immutable identity; cancellation does not prove that a preceding commit failed.

Bodies are limited to 65,536 bytes for the whole JSON request, with unknown fields and trailing JSON rejected. This transport bound includes the document wrapper and is independent of inner engine validation budgets. Responses use `Cache-Control: no-store`. Graph bodies and rehearsal proposals are private configure-member outputs. POST is also used for private lookups so request UUIDs remain in the body and the same CSRF boundary applies.

## Operations

| Operation | JSON request fields | Result |
| --- | --- | --- |
| `list` | `{}` | `automations` array of automation/current-version IDs, current document digest, version count, pointer update time and current version creation time. Ordered by automation ID. |
| `list-versions` | `automationId` | Automation/current-version IDs and `versions` array of version ID, document digest and original creation time. Ordered by creation time descending, then version ID descending. |
| `validate` | `document` | `validation: structure_only`, automation/version IDs, canonical digest and declared capabilities. No publication or preset lookup. |
| `publish` | `document`, `expectedCurrentVersionId` | Atomic version publication with IDs, document/graph digests, original version creation time and `unchanged`. Empty expected ID is create-only. |
| `select` | `automationId`, `versionId`, `expectedCurrentVersionId` | Explicit retained-pointer selection and fixed metadata publication result. |
| `load` | `automationId`, optional `versionId` | Complete private pinned graph: selected root IDs, graph digest and exact canonical `versions` bodies. Empty version selects the current root. |
| `admit` | `requestId`, `automationId`, `expectedVersionId` | Original run/occurrence/version/graph identity, creation/deadline and `state: admitted_metadata`. This creates the bounded run/cooldown record; it queues no effect. |
| `lookup` | `requestId` | Original initiating actor's retained run metadata. Missing/purged input creates nothing. |
| `rehearse` | `requestId`, `mode` | Capture version, selected version ID, original run and bounded private rehearsal report/proposals. |
| `save-trace` | `requestId`, `mode`, `expectedVersionId` | Canonical metadata `document`, digest, original trace save time and `unchanged`. No proposals are persisted. |
| `load-trace` | `requestId`, `traceId` | Saved canonical metadata `document`, digest and original save time, without re-evaluation. |

Modes must be explicitly `reproduce` or `current_config`; execute/missing/unknown modes refuse. Rehearsal returns camel-case transport step metadata and `opaqueArguments` on private proposals. Saved trace `document` preserves the existing canonical format-1 field names, including its versioned step fields. The returned digest identifies that stored canonical document; the outer response is not a signed execution receipt.

[Issue #294](https://git.subcult.tv/subculture-collective/overlaid/issues/294) adds bounded discovery. `list` returns at most 100 automations, with `automationId`, `currentVersionId`, `digest`, `versionCount`, `updatedAt` and `versionCreatedAt` per item. `list-versions` returns at most 32 versions, each with `versionId`, `digest` and `createdAt`. An empty workspace returns `{"automations":[]}`. Missing or foreign automation IDs return 404; malformed IDs return 400. Retained data exceeding either catalogue bound returns a fixed capacity failure without partial results or pruning. There is no cursor or search filter because these whole catalogues are bounded by the existing publication limits.

Discovery rechecks configure permission under the tenant lock and returns metadata only, without loading document bodies, arguments, actors, source requests or trace contents. Pointer and count reads share the publication/selection tenant fence. Selecting an older version changes the current pointer and its update time; it preserves immutable version creation order. Metadata digests identify documents, not complete graphs or provider authorization. Use `load` with the discovered IDs to retrieve and validate the exact pinned graph before editing or rehearsal. Catalogue results can become stale after the request; publication/selection still require the exact expected current version.

These reads use the existing account/CSRF/session/app lifetime and repeated role/schema checks, with a ten-second store bound inside the fifteen-second request bound. They remain available without writer ownership and do not mutate audit, cooldown, configuration, run or trace state. They add no pool or schema change. Run and trace history discovery and a graphical desk remain separate work.

An admitted run uses the [existing operator contract](operator-automation-admission.md): schema-2 dashboard trigger, explicit policy, empty captured facts, zero per-stream limit and fixed original database deadline. Admission may charge the declared cooldown and retained metadata capacity. Structural validation and reads do not perform those reservations. Missing fact gates stay unavailable; neither a successful admission nor a proposed rehearsal establishes current provider permission.

The [source-bound rehearsal](retained-operator-rehearsal.md) uses original database time, empty facts and format-defined seed zero. Current-config mode keeps the original input while identifying the newer graph. Arguments remain authored, unrendered text. [Trace persistence](operator-rehearsal-traces.md) saves fixed metadata only. Duplicate admission/save operations return original identities and timestamps under their existing retry rules; publication/select semantics likewise remain those of the immutable version store.

## Example workflow

Use the authenticated account API to find the intended workspace and current session/CSRF state. Call `list` with `{}` to discover saved automations, then `list-versions` with a returned automation ID and `load` with an explicitly selected version. Keep a fresh automation UUID and version UUID for a new authored document; existing versions require a new version UUID and exact expected current pointer. This structural example proposes a chat action without executing it:

```json
{
  "document": {
    "schemaVersion": 2,
    "tenantId": "11111111-1111-4111-8111-111111111111",
    "automationId": "22222222-2222-4222-8222-222222222222",
    "versionId": "33333333-3333-4333-8333-333333333333",
    "trigger": {"id": "button", "kind": "dashboard"},
    "capabilities": ["chat.send"],
    "restrictions": [],
    "admission": {"groups": [], "cooldownMs": 0, "perStreamLimit": 0},
    "root": "root",
    "nodes": [{"id": "root", "kind": "action", "capability": "chat.send", "arguments": [{"name": "text", "value": "Hello $(user)"}]}]
  },
  "expectedCurrentVersionId": ""
}
```

Replace the example tenant with the authorized route workspace and choose actual immutable IDs. Send the document to `validate`, then `publish` when intentionally saving it. Persist a separate request UUID before calling `admit` with the returned automation/version IDs. Use that exact request UUID for `lookup` and `rehearse`. Save a trace deliberately with the reported selected version, then use its trace ID for `load-trace`. The example's `$(user)` stays opaque: variable rendering and message delivery are unfinished work.

## Failures, compatibility and verification

Business failures use fixed JSON `error` codes. Invalid document/request input is 400; permission is 403; missing retained rows are 404; version/policy/cooldown/capacity refusal is 409; an admission retry after its private run was purged is 410. Schema or managed-writer unavailability is 503, operation timeout is 504, and canceled work may return 408. Unknown operation failures return fixed 500 metadata. Transport authentication/CSRF/JSON-body failures retain the account control's fixed text responses. Database/provider/document detail is never echoed through an error.

Apply the existing migrations through 0042 before starting the current managed binary. No new migration, startup mode, secret, pool or worker is added by the HTTP boundary. Stores continue their exact role/schema and transaction checks on each operation. Binary rollback can remove these routes while retaining all immutable configuration, run markers, cooldown charges and traces; never replay saved proposals through a legacy path.

Disposable HTTP/store races exercise publish/admit/rehearse/save/load, duplicate and stale-version handling, scoped private outputs, no delivery jobs/runtimes, strict bodies, Origin/CSRF/cookie/role/tenant boundaries, controller loss, and cancellation of a tenant-lock-waiting write by logout or shutdown. File-mode and fixed-error checks remain separate. Run `make verify`, new-diff lint and the full disposable PostgreSQL gate before merge. No dashboard browser, deployment, merged-main CI, provider delivery or Phase #25 completion is established by these tests.

Catalogue fixtures additionally cover empty results, deterministic timestamp ties, older current selection, load-by-discovered-ID, metadata allowlists, exact/overflow bounds, cross-tenant isolation, configure/revoked membership, grant drift, canceled lock waits and unchanged persisted state. HTTP discovery is exercised after actual controller ownership loss; it still creates no jobs or tenant runtimes.
