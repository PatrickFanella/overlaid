# Explicit application stream consumer

[Issue #204](https://git.subcult.tv/subculture-collective/overlaid/issues/204) adds `Application.prepareStream` around the [runtime-bound Newsroom adapter](application-stream-handler.md) and [fenced stream worker](stream-dispatch.md). It owns the separate stream queue pool and controlled River client. Default legacy startup creates neither this consumer nor a `stream_dispatch` readiness check. The [retained routing opt-in](stream-routing.md) now selects this owner; the owner itself adds no subscription or provider request.

## Construction and start

Preparation requires managed retained chat ingestion, the actual database controller lock, a nonempty separate stream worker URL and no existing stream owner. The URL must resolve to the same database/schema as product storage through the exact restricted stream leasing role. Construction and subsequent validation reject combined worker memberships, broadened policies, private-table or column grants, administrative privileges and database/schema mismatches.

Preparation refuses EventSub-enabled applications unless the coordinated retained stream mode is selected. Installation drains direct lifecycle handlers through a cancellation-aware routing lock before publishing the owner. The [cutover](stream-routing.md) replaces direct handling through the existing callback; use synthetic retained sources in qualification and do not enable another lifecycle subscriber.

Preparation leaves leasing closed. `start` serializes with shutdown, validates the current controller and queue grants, starts the worker and then opens its readiness gate. Only one owner and one successful start are allowed. The successful worker lifetime belongs to the application, independently of the caller's startup context. Canceling a completed startup context does not stop the worker. Canceling startup while construction/start is still in progress prevents activation.

## Runtime checks and readiness

An installed owner adds the named `stream_dispatch` check to `/readyz`. Prepared or stopped owners report degraded; a live worker with the existing controller reports ready. `/healthz` remains cheap process liveness. Monitoring must parse readiness JSON rather than HTTP status alone.

A one-second monitor rechecks the exact queue role, database/schema identity and actual controller ownership even while idle. Worker stop or permission/controller drift closes readiness and cancels the owner and application. These checks are snapshots; they do not authorize broader grants on an active pool.

Preparation and each saved capability/handler callback repeat the owner and queue validation. Consumer cancellation reaches the prepared job lifetime and running editorial handler. The underlying adapter still checks its exact runtime, connection revision, source/deadline, automation settings and entitlement. A ready queue grants no handler claim or provider permission. Claimed work retains terminal no-replay behavior.

## Shutdown and rollback

Shutdown first closes the readiness gate and cancels the consumer. It serializes against start, stops the River worker, waits for the monitor and drains lifecycle routing/admission before closing its owned queue pool. Product storage and controller ownership belong to the application and remain available until the worker and detached bookkeeping finish. A timed-out stop returns without closing these dependencies; retry shutdown after the blocked work ends. The application observes the same ordering before closing other pools or releasing its controller.

The stopped owner stays installed and degraded. It cannot restart or be replaced in place. Restart the application to create a new explicit owner; existing source/receipt fencing still prevents replay. This owner builds on the validated contracts through 0032; current application startup also validates the migration 0033 compatibility fence. Stop any explicit worker before rolling back and preserve retained sources, jobs and receipts.

## Verification and remaining work

Run the focused fixtures against disposable PostgreSQL from `backend/`:

```bash
go test -race -count=1 ./internal/http -run '^TestOwnedStream'
```

Fixtures exercise native River processing, closed preparation leasing, startup-context separation, restart no replay, invalid role/database/configuration inputs, idle drift and saved callbacks. A deliberately blocked synthetic handler verifies timed-out stop, retained pools and application shutdown ordering. These fixtures make no real provider calls. Repository qualification also requires `make verify` and `bash scripts/ci-postgres.sh`.

The [routing cutover](stream-routing.md) coordinates direct lifecycle draining, retained ingestion, the existing subscriptions and persisted writer/downgrade guards. Existing direct Newsroom lifecycle routing remains selected by default. Channel-update editorial handling, conduits, onboarding, broad load and live provider acceptance remain on the [roadmap](../superpowers/plans/2026-10-15-remaining-work-roadmap.md).
