# Workers, retries and crash recovery

> **Status:** design target; not implemented.

## Lifecycle

Startup: validate configuration, secrets, destinations, schema version and database reachability. When ready, accept HTTP admissions and periodically claim due work. Use a fixed-size worker pool with cancellation-aware goroutines; database is the persistent queue, while an in-memory channel may only buffer already claimed work for a short bounded interval.

Claim and attempt recording happen in one DB transaction. After commit, the worker sends HTTP outside the transaction. The result is recorded conditionally using the lease token. Retry scheduling never blocks a worker with a long `time.Sleep`; it persists RETRY_WAIT and returns the worker to the pool.

Shutdown: first reject new admissions and signal `/readyz = 503`; stop claiming; drain in-flight work until deadline; cancel remaining work. Unfinished claims are recovered after lease expiry.

## Initial tuning parameters

| Setting | Proposed initial value |
| --- | --- |
| Worker concurrency | 4 |
| Claim batch | 16 |
| Polling | 500–2000 ms with jitter |
| Outbound request total timeout | 10 seconds |
| Lease duration | 30 seconds |
| Maximum started attempts | 8 |
| Backoff base | 2 seconds |
| Backoff cap | 300 seconds |
| Retry-After cap | 3600 seconds |
| Graceful shutdown deadline | 20 seconds |

These are starting defaults, not benchmark claims. Require lease duration to exceed total outbound request timeout with room for DB completion, scheduling and host pauses; measure actual p99 latencies.

## Failure classification

| Result | Action |
| --- | --- |
| Any 2xx HTTP response | DELIVERED |
| 408 / 425 / 429 / 5xx | RETRY_WAIT while budget remains |
| Network timeout or transient transport failure | RETRY_WAIT while budget remains |
| Other 4xx | DEAD_LETTER by default |
| Redirect 3xx | Never follow; permanent failure by default |
| Invalid TLS identity / forbidden target IP | Fail closed; do not treat as transient |
| Attempts exhausted | DEAD_LETTER |

Separate DNS temporary failure from a forbidden resolved address. Never retry unsafe destinations automatically in a tight loop.

## Retry scheduler

For failed attempt number n (starting at 1):

```text
ceiling = min(max_backoff, base_backoff * 2^(n-1))
random_delay = uniform(0, ceiling)
next_attempt_at = DB_time + max(random_delay, valid_Retry_After)
```

Use overflow-safe arithmetic and deterministic injected random source in tests. Parse `Retry-After` seconds or HTTP-date, cap per policy; reject malformed/negative values. A minimum nonzero retry floor can prevent bursts.

`attempts` is incremented **when claimed**, even if a process dies before sending HTTP. A consistent finite budget is more important than pretending a failed process completed an attempt. Store `available_at` as the next eligible instant; do not use process-local timers as source of truth.

## Leases and stale workers

Every claim assigns a fresh unpredictable token and lease_until computed using DB time. The result update requires matching current token. After expiry another claimer can reclaim with a new token. The old worker's DB completion must fail.

Lease fencing cannot cancel remote HTTP side effects already emitted. Consumers must deduplicate by stable event ID. Timeout/lease tuning can reduce unexpected overlaps but not eliminate duplicate delivery.

The sweeper handles expired PROCESSING records with no remaining attempts; a separate short transaction moves them to DEAD_LETTER.

## Backpressure

Bound admission rate per client, maximum pending queue, outstanding bytes, claim batch and HTTP connections. If saturation is detected, return a documented 429 or 503 **before** claiming durable admission. A healthy API with an unhealthy worker pool can accumulate PENDING items: track backlog count and age.

Strict FIFO is not guaranteed. Later optional features may add destination-specific concurrency limits, fair scheduling, ordering keys and pause/resume, each with contract tests.

## Outbound HTTP transport

Use `net/http` with explicit total timeout, connection limits, TLS hostname verification, redirect prohibition, DNS/IP checks tied to the actual dial target, and ambient proxy disabled unless explicitly approved. Do not read or retain unbounded response bodies. Build the signed body deterministically for each attempt and sign the actual transmitted bytes.

## Fault injection

Test slow HTTP response, shutdown at every state transition, database fail after 2xx but before commit, request timeouts, lease races and repeated crash cycles. Distinguish at-least-once delivery from exactly-once remote effects in every result.

See [delivery semantics](delivery-semantics.md), [database design](database-postgresql.md), [testing](testing.md).
