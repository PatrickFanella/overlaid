# Managed tenant runtime capacity

Tracking: [#145](https://git.subcult.tv/subculture-collective/overlaid/issues/145), [#143](https://git.subcult.tv/subculture-collective/overlaid/issues/143), parent [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24).

`TENANT_RUNTIME_LIMIT` bounds loaded tenant runtimes in a managed PostgreSQL application. The default remains **100**. An explicit deployment value from **1 through 1000** selects a different bound after restart. Invalid explicit environment values, including zero, non-integers, negative values and values above 1000, fail application startup before pools or workers are constructed. The setting is in `.env.example` and Compose; tenant/browser requests cannot change it.

The bound controls loaded runtime resources, not provider permissions or delivery capacity. It does not change sender mode, queue concurrency, account pacing, claims or original deadlines. There is no idle eviction. Normal application resumption attempts to load the known tenant list. The direct-construction fixture below covers admission and overflow; the separate global-resumption fixture covers startup and retention for exactly 1000 known tenants.

Managed `/readyz` reports `activeRuntimes`, `runtimeLimit` and the fixed `tenant_runtimes` check. Observed capacity denial degrades that check; releasing a slot or successfully preparing the waiting runtime clears it. Existing runtimes continue working. Retained commands prepare and bind their runtime before the durable handler-start fence, as described in the [retained sender runbook](retained-chat-sender.md). Retry exhaustion does not automatically resurrect discarded work.

## Actual runtime fixture

`TestRetainedRealRuntimeLimitsAndRestart` runs with both the default 100 and explicit 1000 limits. Each case creates distinct fixture owners, tenants and channel bindings, constructs that many actual `Server` runtimes, and starts their normal Newsroom and timer background workers. EventSub and other external transports remain disabled; a mock rejects every provider request. Documents and timer queues start empty.

The fixture submits one retained overlay-only counter command and duplicate input per tenant through the existing ingestion boundary and real River workers. It checks isolated counter values, exact completed receipt/job counts and zero chat actions/provider requests. The next tenant reaches bounded preparation retry without a handler fence. After invalidating one real runtime, the fixture explicitly promotes the same retry, then verifies one execution and cleared readiness pressure. This promotion tests recovery separately from natural maintenance-scheduler timing.

Shutdown cancels tenant contexts, stops clients, removes runtime/send-gate references and permits a fresh application to reacquire controller ownership. Persisted counters, receipts and completed jobs remain intact. This fresh startup does not reconstruct all populated runtimes a second time.

## Global resumption, populated restart and retention

`TestRetainedGlobalRuntimeResumeAndRetention` calls normal `Application.StartBackground` for exactly 1000 fixture tenants, then repeats that path after closing the populated application. Each startup must restore all 1000 runtimes, complete its initial application-wide retention sweep and report healthy readiness. Both generations process a retained overlay-only counter command plus duplicate input per tenant through real River workers. Per-tenant counters must be one before restart and two after the second command; surviving completed jobs must stay completed.

The tenants alternate one-day and 365-day retention policies. With the first application closed, the fixture ages its normalized inputs by 30 days through the disposable administrator connection. The restarted application's normal retention sweep must remove exactly the 500 expired events and cascade their native payloads, dispatch receipts and jobs, preserving the other 500. New commands then complete for every tenant, including those whose old input expired. Shutdown must cancel every runtime and release runtime/send-gate references. Every provider request is forbidden and no outbound action may be created.

This qualifies the initial global sweep and restart path for retained chat input at this tenant count. Newsroom documents and timer queues start empty. It does not qualify a naturally triggered minute-ticker sweep, large editorial histories, every retention table, provider-side behavior or a continuous workload. The fixture does not re-submit an expired source and makes no claim that deduplication survives removal of its retained identity.

## Finite local observations

One uncached Go 1.26.0 race run on Kvant, October 16, 2026, using the owned disposable PostgreSQL tmpfs fixture recorded:

| Loaded runtimes | Construction/startup | Processing first commands | Shutdown | Sampled populated Go heap | Sampled goroutines | Sampled product-role DB sessions |
| --- | --- | --- | --- | --- | --- | --- |
| 100 | 0.428 s | 3.142 s | 0.0026 s | 10.5 MiB | 341 | 4 |
| 1000 | 3.726 s | 25.567 s | 0.0114 s | 83.5 MiB | 3053 | 5 |

The sampled goroutine count returned to its baseline of five after each close/reacquisition sequence. These are observations, not machine-independent performance thresholds or peak resource measurements. Go heap excludes process RSS, race-detector overhead and PostgreSQL memory. The session sample covers the product-role pools, not all queue/pacing/admin connections. Startup includes application construction and the selected runtime loop; command timing includes ingestion and dispatch completion. The overflow/retry phase is separate from the timing columns.

Run `GOTOOLCHAIN=go1.26.0 bash scripts/ci-postgres.sh` for the full disposable verification gate. A focused uncached race run with `-v -run TestRetainedRealRuntimeLimitsAndRestart` records fresh timing/resource samples for its own environment.

The separate global-resumption race run on the same host and date restored 1000 runtimes in 4.02 s initially and 3.69 s after populated restart. In each generation, the initial sweep had completed by the end of runtime resumption. The 1000 retained commands and their terminal jobs settled in 25.35 s and 25.69 s respectively; shutdown took 0.011 s and 0.014 s. These timings are finite observations with no portable performance threshold. Run the focused race test with `-v -run TestRetainedGlobalRuntimeResumeAndRetention` to record another sample.

Full thousand-channel delivery acceptance remains open: these fixtures do not exercise 1000 live EventSub sockets, provider sends/shared-account pacing, moderation priority, all retention data types or natural periodic sweeps, populated editorial histories, continuous hostile arrivals or a production deployment. Raising the configured bound alone establishes none of those results.

## Accepted audit shutdown and failure diagnostics

[Issue #250](https://git.subcult.tv/subculture-collective/overlaid/issues/250) follows two failures of merged-main run16510. The first reported a failed resumption command alongside database connection timeouts; retry job30255 failed while shutting down the 1000-runtime fixture. Those observations do not establish a common cause. PR #249 passed its exact-head hosted check and complete local gate.

Managed application shutdown cancels new work, stops retained workers, then drains accepted bot audits within the remaining original fifty-second shutdown budget. Each audit write keeps its existing five-second bound. An unfinished worker stop or audit drain returns an error before releasing the application controller/product pools or clearing runtime ownership. Retain the canceled application and retry cleanup; do not start a replacement owner after a timeout. Successful cleanup releases those dependencies after drain. The existing per-worker queue capacity and audit-error semantics remain unchanged; a completed drain is not proof that a failed audit write persisted.

The deterministic slow-audit fixture first reproduced the old two-second drain failure. It now proves that an accepted write finishing within its original bound persists before shutdown. A separate bounded-drain fixture verifies canceled effect readiness, retained product/controller/runtime ownership, exactly one audit after retry and ownership reacquisition. Runtime-scale failures also report fixed counts for pending audits, standard audit error categories, database sessions/waits and terminal receipt states. They exclude role names, connection strings, tenant/viewer input, raw errors and query text. The original counter, isolation, capacity and no-replay assertions still decide acceptance.

The [decision trail](../superpowers/plans/2026-10-21-application-audit-shutdown-decisions.tsv) records verification. This repair does not establish a cause or fix for the earlier connection timeouts; provider, live/full-load, deployment and power-loss acceptance remain separate.
