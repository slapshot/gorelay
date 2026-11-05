# Observability and operations

> **Status:** operations design. Metrics and health endpoints are not implemented yet.

## Logging contract

Use `log/slog` JSON with stable keys: timestamp, level, component, request_id, event_id, client_id (when safe), destination_id, attempt, state_from, state_to, outcome_code, duration_ms. Exclude payloads, credentials, HMAC signatures, raw destination URLs with secret query strings, raw response bodies and stack traces containing sensitive values.

Correlate admission and delivery using a stable event ID; request IDs are per API request and cannot replace event IDs.

## Health endpoints

`GET /healthz`: 200 when the process handles requests, without checking PostgreSQL. `GET /readyz`: 200 only if schema compatibility and bounded DB connectivity checks succeed and the service is not draining; otherwise 503. Limit checks and network exposure so they cannot become DoS vectors.

## Metrics candidates

Counters:
- `gorelay_admitted_total{result}`
- `gorelay_delivery_attempts_total{result}`
- `gorelay_delivery_outcomes_total{status_class}`
- `gorelay_lease_reclaims_total`
- `gorelay_dead_letter_total`

Gauges:
- `gorelay_events_pending`
- `gorelay_events_retry_wait`
- `gorelay_events_processing`
- `gorelay_events_dead_letter`
- `gorelay_oldest_queued_age_seconds`
- `gorelay_workers_busy`

Histograms:
- `gorelay_http_ingest_duration_seconds`
- `gorelay_db_claim_duration_seconds`
- `gorelay_delivery_duration_seconds`

These names are proposals, not operational endpoints. Keep labels low-cardinality: never use event ID, request ID, arbitrary source, URL or error text as metrics labels. Histograms need measured bucket strategy before release.

## Alerting examples

- Readiness failure continuously exceeding a configured threshold.
- Growth of oldest pending/retry age beyond expected delivery SLO.
- High dead-letter event rate and retry exhaustion.
- Sudden rise of 429/5xx destination errors.
- Database connections near limit or prolonged locks.
- Lease reclaim frequency, especially if HTTP timeout < lease but processes remain live.
- Queue storage growth and disk free space low.
- Backup/restore validation overdue.

Alert thresholds are deployment-specific; do not ship misleading universal values.

## Diagnostics and runbooks

For a delivery incident, establish:
1. Is GoRelay ready and the DB reachable?
2. Is admission succeeding but backlog accumulating?
3. Is a particular destination rejecting requests?
4. Are claims leased too long, expiring, or being reclaimed?
5. Are retries and Retry-After scheduled correctly?
6. Are events in DEAD_LETTER due to permanent errors or attempt exhaustion?

Restrict diagnostic queries and protect payload confidentiality. Do not manually UPDATE queue rows in production without a documented, auditable recovery process.

## Capacity and maintenance

Benchmark under representative event-body sizes and destination latency; set disk headroom, DB connection caps and worker concurrency based on measurements. Watch dead tuples, index bloat and autovacuum in PostgreSQL. SQLite future backend requires checkpoint policy and local-disk backup tests.

## Backup and restore

Backups must include event state, keys, attempts, schema/migration version and **destination configuration outside DB**. Use a consistent PostgreSQL backup method (e.g. pg_dump for modest volume or physical/PITR for stronger requirements), encrypt and restrict backups, and perform isolated restore drills.

After restore, previously completed requests may be retried if the backup predates their delivery commit. This is inherent in restoration and requires consumer deduplication plus operational review. A restore is not transparently exactly-once.
