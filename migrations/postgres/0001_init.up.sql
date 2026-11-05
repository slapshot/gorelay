-- GoRelay PostgreSQL v0.1 schema.
-- Apply explicitly as the migration role, inside a transaction.
-- This migration is forward-only until a safe rollback policy is defined.

BEGIN;

CREATE TABLE schema_migrations (
  version integer PRIMARY KEY,
  applied_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE events (
  id uuid PRIMARY KEY,
  client_id text NOT NULL,
  event_type text NOT NULL,
  destination_id text NOT NULL,
  payload jsonb NOT NULL,
  idempotency_key text NOT NULL,
  request_fingerprint bytea NOT NULL,
  status text NOT NULL DEFAULT 'PENDING',
  attempts integer NOT NULL DEFAULT 0,
  max_attempts integer NOT NULL DEFAULT 8,
  available_at timestamptz NOT NULL DEFAULT now(),
  lease_until timestamptz,
  lease_token uuid,
  last_http_status integer,
  last_error_code text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  delivered_at timestamptz,
  CONSTRAINT unique_admission_key UNIQUE (client_id, idempotency_key),
  CONSTRAINT fingerprint_sha256 CHECK (octet_length(request_fingerprint) = 32),
  CONSTRAINT attempt_bounds CHECK (attempts >= 0 AND max_attempts > 0 AND attempts <= max_attempts),
  CONSTRAINT valid_state CHECK (status IN
    ('PENDING','PROCESSING','RETRY_WAIT','DELIVERED','DEAD_LETTER')),
  CONSTRAINT processing_has_lease CHECK (
    (status = 'PROCESSING') =
    (lease_token IS NOT NULL AND lease_until IS NOT NULL)
  ),
  CONSTRAINT delivered_has_timestamp CHECK (status <> 'DELIVERED' OR delivered_at IS NOT NULL),
  CONSTRAINT json_object CHECK (jsonb_typeof(payload) = 'object')
);

CREATE INDEX events_due_idx ON events (available_at, created_at, id)
  WHERE status IN ('PENDING','RETRY_WAIT');
CREATE INDEX events_expired_idx ON events (lease_until, id)
  WHERE status = 'PROCESSING';
CREATE INDEX events_owner_time_idx ON events (client_id, created_at DESC);

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

CREATE INDEX delivery_attempts_event_idx ON delivery_attempts (event_id, attempt_number DESC);

INSERT INTO schema_migrations (version) VALUES (1);

COMMIT;
