# Delivery semantics, idempotency and state machine

Status: proposed; this document is the normative source for state transitions.

## Guarantee statement

GoRelay aims for **durable admission and at-least-once attempted delivery** within configured retry and retention policy. Once admission is committed, an event remains in durable storage until it is delivered or placed in a persistent dead-letter state (unless an operator explicitly removes it). A retryable event is eventually eligible for retry while the service and destination are operational.

It does **not** guarantee:
- exactly-once execution at the consumer;
- strict processing order among events;
- delivery by a fixed deadline;
- successful remote handling after a 2xx;
- continued service during complete host or storage loss;
- preservation of a producer's business commit unless it uses an outbox.

## States

| State | Meaning | Eligible to claim |
| --- | --- | --- |
| PENDING | committed, not yet attempted | yes, when available_at <= DB time |
| PROCESSING | leased to one worker | only after expired lease and under max attempts |
| RETRY_WAIT | previous attempt needs delay | yes, when available_at <= DB time |
| DELIVERED | 2xx confirmed and result committed | no |
| DEAD_LETTER | permanent failure or exhausted attempts | no |

Allowed transitions:
- PENDING -> PROCESSING
- RETRY_WAIT -> PROCESSING
- PROCESSING with expired lease -> PROCESSING (new token and attempt) if attempts remain
- PROCESSING -> DELIVERED on 2xx
- PROCESSING -> RETRY_WAIT on classified transient error with attempts remaining
- PROCESSING -> DEAD_LETTER on permanent failure or retry budget exhaustion
- expired PROCESSING with no remaining attempts -> DEAD_LETTER by sweeper
- no automatic transition out of DELIVERED or DEAD_LETTER in v0.1

The worker **increments attempts on each claim**, including recovery claims. An attempt recorded as started but interrupted still consumes the budget. This avoids an infinite retry loop after repeated crashes.

## Lease fencing

Each claim sets a new unpredictable lease_token and lease_until based on database time. A delivery result is accepted only if event ID, status=PROCESSING, and lease_token still match. Updating by event ID alone is unsafe.

If lease ownership is lost, a worker must stop writing outcomes. Lease fencing prevents stale database transitions but **cannot retract** an HTTP request already sent; consumer idempotency handles duplicate effects.

For the baseline, HTTP total timeout must be shorter than lease duration with an adequate margin. Test high latency and DB pauses; add lease renewal only if longer workloads justify it. The expiry sweeper and claimer must never exceed the configured attempt budget.

## Ambiguous outcomes and delivery duplicates

Example:
1. Worker claims E and sends HTTP POST.
2. Consumer processes E and returns 200.
3. Process crashes before storing DELIVERED.
4. Lease expires, another worker retries E.

The second POST is legitimate under at-least-once; it is not proof of a bug. Consumer must atomically deduplicate event ID with its side effects, normally using a unique event-ID table in the same consumer transaction.

200/202/204 and other 2xx indicate HTTP-level acceptance, not necessarily completion of remote business work.

## Idempotent ingestion

Producer repeats a request with the same authenticated client and key after an ambiguous timeout. Durable uniqueness on (client_id, idempotency_key) ensures a single event. A matching canonical request fingerprint returns the existing ID and current status; a mismatching fingerprint returns 409.

Do not silently drop the idempotency record while advertising indefinite deduplication. For v0.1, no automatic retention pruning is performed. Later retention must specify key scope and expiration in the API and migration policy.

## Dead-letter handling

DEAD_LETTER is persistent, queryable, and counted in metrics. Store bounded diagnostic metadata (error category, status, timestamps) but not raw personal/sensitive response bodies. There is no public redrive endpoint in v0.1. Future replay requires a guarded operator action with an explicit audit trail, decision about new vs original event ID, and test coverage.

## Ordering

Events are selected in a stable priority order (available_at, created_at, id) when feasible, but concurrency, retries, and SKIP LOCKED make delivery order **non-deterministic**. If a consumer needs order, it must impose its own aggregate sequence or GoRelay must add a documented per-key serialization feature later.

## Availability and pressure

If the DB is unavailable, the service must not return 202. If worker capacity is exhausted but admission is healthy, events can remain PENDING; bound queue length and reject new events before exhausting disk. A growing queue, oldest queued age, failures and lease reclaim rate must be observable.

See [worker algorithm](workers.md), [testing](testing.md), and [database design](database-postgresql.md).
