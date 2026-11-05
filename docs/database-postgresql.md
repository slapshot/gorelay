# PostgreSQL schema and transaction design

> **Status:** proposal for GoRelay v0.1. The SQL below has not yet been applied or integration-tested. The migration implementation will be reviewed separately.

PostgreSQL 18 is the initial primary engine. All persisted event, claim and status transitions must be atomic and short-lived. In particular, **no network request occurs while holding a transaction**.

## Roles and deployment boundaries

Create a dedicated database owned by a migration role. Use a separate least-privilege runtime role: CONNECT, USAGE on its schema, and specifically granted DML privileges. The runtime role must never own tables or manage unrelated databases. Authenticate callers at the HTTP layer; `client_id` is server-derived and need not be stored in a public client management table in v0.1.

Use PostgreSQL server time (`now()`) for scheduling and lease expiry, and UTC/RFC3339 on the wire. PostgreSQL `timestamptz` stores an absolute instant; it does not retain the original time-zone name.

## Logical schema

```sql
CREATE TABLE events (
  id                  uuid PRIMARY KEY,
  client_id           text NOT NULL,
  event_type          text NOT NULL,
  destination_id      text NOT NULL,
  payload             jsonb NOT NULL,
  idempotency_key     text NOT NULL,
  request_fingerprint bytea NOT NULL,
  status              text NOT NULL DEFAULT 'PENDING',
  attempts            integer NOT NULL DEFAULT 0,
  max_attempts        integer NOT NULL DEFAULT 8,
  available_at        timestamptz NOT NULL DEFAULT now(),
  lease_until         timestamptz,
  lease_token         uuid,
  last_http_status    integer,
  last_error_code     text,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  delivered_at        timestamptz,
  CONSTRAINT unique_admission_key UNIQUE (client_id, idempotency_key),
  CONSTRAINT fingerprint_sha256 CHECK (octet_length(request_fingerprint) = 32),
  CONSTRAINT attempt_bounds CHECK
    (attempts >= 0 AND max_attempts > 0 AND attempts <= max_attempts),
  CONSTRAINT valid_state CHECK (status IN
    ('PENDING','PROCESSING','RETRY_WAIT','DELIVERED','DEAD_LETTER')),
  CONSTRAINT processing_has_lease CHECK
    ((status = 'PROCESSING') =
      (lease_token IS NOT NULL AND lease_until IS NOT NULL)),
  CONSTRAINT delivered_has_timestamp CHECK
    (status <> 'DELIVERED' OR delivered_at IS NOT NULL),
  CONSTRAINT json_object CHECK (jsonb_typeof(payload) = 'object')
);

CREATE INDEX events_due_idx
  ON events (available_at, created_at, id)
  WHERE status IN ('PENDING', 'RETRY_WAIT');

CREATE INDEX events_expired_idx
  ON events (lease_until, id)
  WHERE status = 'PROCESSING';

CREATE INDEX events_owner_time_idx
  ON events (client_id, created_at DESC);

CREATE TABLE delivery_attempts (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  event_id uuid NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  attempt_number integer NOT NULL CHECK (attempt_number > 0),
  lease_token uuid NOT NULL,
  started_at timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz,
  outcome_code text,
  http_status integer,
  duration_ms bigint CHECK (duration_ms >= 0),
  UNIQUE (event_id, attempt_number)
);

CREATE INDEX delivery_attempts_event_idx
  ON delivery_attempts (event_id, attempt_number DESC);
```

The Go service generates the UUID securely, not the client. `delivery_attempts` stores bounded diagnostic metadata; `events` remains the source of truth for scheduling.

The above is a candidate schema: migrations must be made robust against interrupted deployments, index creation strategy, privilege boundaries and future data retention. In particular, validate generated DDL, PostgreSQL version compatibility, query plans, deadlock behavior and every invariant against a real instance.

## Durable admission

1. Authenticate bearer token, derive client ID and validate destination authorization before DB work.
2. Validate the JSON and deterministically fingerprint its normalized content.
3. Insert event with unique (client_id, idempotency_key).
4. If conflict, read committed existing record; compare fingerprint; return original ID for match, 409 otherwise.
5. Respond 202 only when insert is durably committed.

A typical design uses `INSERT ... ON CONFLICT DO NOTHING RETURNING id` followed by a separate SELECT on conflict in READ COMMITTED. Understand concurrent transaction visibility; do not use a naïve check-then-insert or assume a single SQL statement will always see a row previously blocked by an uncommitted insert.

## Claiming events

Conceptual PostgreSQL SQL (must be tested):

```sql
WITH due AS (
  SELECT id
  FROM events
  WHERE (
    status IN ('PENDING', 'RETRY_WAIT')
    AND available_at <= now()
    AND attempts < max_attempts
  ) OR (
    status = 'PROCESSING'
    AND lease_until <= now()
    AND attempts < max_attempts
  )
  ORDER BY available_at, created_at, id
  FOR UPDATE SKIP LOCKED
  LIMIT $1
)
UPDATE events AS e
SET status = 'PROCESSING',
    attempts = e.attempts + 1,
    lease_until = now() + $2::interval,
    lease_token = gen_random_uuid(),
    updated_at = now()
FROM due
WHERE due.id = e.id
RETURNING e.id, e.client_id, e.event_type, e.destination_id,
          e.payload, e.attempts, e.lease_token, e.lease_until;
```

The claim transaction must record `delivery_attempts` before COMMIT. Keep the transaction bounded and return work to a bounded in-memory worker pool; the durable queue remains PostgreSQL, not a Go channel. `SKIP LOCKED` avoids waiting on other consumers but intentionally does not provide strict FIFO.

## Completion with fencing

The completion transaction updates the event only under:

```sql
WHERE id = $1 AND status = 'PROCESSING' AND lease_token = $2
```

A successful 2xx sets DELIVERED and delivered_at; a retryable error with attempts remaining sets RETRY_WAIT with available_at; permanent errors set DEAD_LETTER. Clear lease fields in the same transaction, and finish the matching attempt record transactionally. A zero-row update indicates stale ownership; never overwrite the newer owner's outcome.

A bounded sweeper moves expired PROCESSING rows at max_attempts to DEAD_LETTER. It must coordinate with claims and completion through row locking or guarded writes. The system must never leave expired exhausted events stranded indefinitely.

## Isolation, performance and operations

- Default to READ COMMITTED and short transactions; change isolation only if a real failure case requires it.
- Bound pgxpool size and timeouts independently of HTTP delivery concurrency.
- Monitor index usage, dead tuples, transaction duration and autovacuum; leave autovacuum enabled.
- Consider `available_at` for expired PROCESSING ordering carefully, and measure real query plans with `EXPLAIN (ANALYZE, BUFFERS)`.
- Do not keep full HTTP response bodies in tables or logs.
- No automatic retention/deletion in v0.1: deleting historical rows also destroys ingestion-key deduplication unless a separate tombstone scheme is introduced.
- Backup events and attempt records, and test restoring into an isolated database.

Official reference: https://www.postgresql.org/docs/18/sql-select.html
