# Managed tenant runtime capacity

Tracking: [#143](https://git.subcult.tv/subculture-collective/overlaid/issues/143), parent [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24).

`TENANT_RUNTIME_LIMIT` bounds loaded tenant runtimes in a managed PostgreSQL application. The default remains **100**. An explicit deployment value from **1 through 1000** selects a different bound after restart. Invalid explicit environment values, including zero, non-integers, negative values and values above 1000, fail application startup before pools or workers are constructed. The setting is in `.env.example` and Compose; tenant/browser requests cannot change it.

The bound controls loaded runtime resources, not provider permissions or delivery capacity. It does not change sender mode, queue concurrency, account pacing, claims or original deadlines. There is no idle eviction. Normal application resumption attempts to load the known tenant list; this fixture constructs its selected runtimes directly and does not qualify global resumption or retention sweeping at scale.

Managed `/readyz` reports `activeRuntimes`, `runtimeLimit` and the fixed `tenant_runtimes` check. Observed capacity denial degrades that check; releasing a slot or successfully preparing the waiting runtime clears it. Existing runtimes continue working. Retained commands prepare and bind their runtime before the durable handler-start fence, as described in the [retained sender runbook](retained-chat-sender.md). Retry exhaustion does not automatically resurrect discarded work.

## Actual runtime fixture

`TestRetainedRealRuntimeLimitsAndRestart` runs with both the default 100 and explicit 1000 limits. Each case creates distinct fixture owners, tenants and channel bindings, constructs that many actual `Server` runtimes, and starts their normal Newsroom and timer background workers. EventSub and other external transports remain disabled; a mock rejects every provider request. Documents and timer queues start empty.

The fixture submits one retained overlay-only counter command and duplicate input per tenant through the existing ingestion boundary and real River workers. It checks isolated counter values, exact completed receipt/job counts and zero chat actions/provider requests. The next tenant reaches bounded preparation retry without a handler fence. After invalidating one real runtime, the fixture explicitly promotes the same retry, then verifies one execution and cleared readiness pressure. This promotion tests recovery separately from natural maintenance-scheduler timing.

Shutdown cancels tenant contexts, stops clients, removes runtime/send-gate references and permits a fresh application to reacquire controller ownership. Persisted counters, receipts and completed jobs remain intact. This fresh startup does not reconstruct all populated runtimes a second time.

## Finite local observations

One uncached Go 1.26.0 race run on Kvant, October 16, 2026, using the owned disposable PostgreSQL tmpfs fixture recorded:

| Loaded runtimes | Construction/startup | Processing first commands | Shutdown | Sampled populated Go heap | Sampled goroutines | Sampled product-role DB sessions |
| --- | --- | --- | --- | --- | --- | --- |
| 100 | 0.428 s | 3.142 s | 0.0026 s | 10.5 MiB | 341 | 4 |
| 1000 | 3.726 s | 25.567 s | 0.0114 s | 83.5 MiB | 3053 | 5 |

The sampled goroutine count returned to its baseline of five after each close/reacquisition sequence. These are observations, not machine-independent performance thresholds or peak resource measurements. Go heap excludes process RSS, race-detector overhead and PostgreSQL memory. The session sample covers the product-role pools, not all queue/pacing/admin connections. Startup includes application construction and the selected runtime loop; command timing includes ingestion and dispatch completion. The overflow/retry phase is separate from the timing columns.

Run `GOTOOLCHAIN=go1.26.0 bash scripts/ci-postgres.sh` for the full disposable verification gate. A focused uncached race run with `-v -run TestRetainedRealRuntimeLimitsAndRestart` records fresh timing/resource samples for its own environment.

Full thousand-channel delivery acceptance remains open: this fixture does not exercise 1000 live EventSub sockets, provider sends/shared-account pacing, moderation priority, global retention sweeps, populated editorial histories, continuous hostile arrivals or a production deployment. Raising the configured bound alone establishes none of those results.
