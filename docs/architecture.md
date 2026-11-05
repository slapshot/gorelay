# Architecture and boundaries

Status: **proposed for v0.1**. No behavior in this document is implemented yet.

## Purpose and non-goals

GoRelay is a generic **durable HTTP webhook dispatcher**. Producers submit events; GoRelay validates, authenticates and durably records each request, then a bounded worker pool attempts delivery to a pre-authorized destination. The system does not interpret business semantics.

Goals: short, atomic persistence operations; at-least-once delivery; transparent retry/dead-letter behavior; traceability; least privilege; single small binary; self-hosted operation; storage portability where it is realistic.

Non-goals for v0.1: Kafka/RabbitMQ replacement; distributed ordering or exactly-once processing; arbitrary end-user-supplied URLs; event fan-out; guaranteed end-to-end transactions with producers or consumers; general workflow orchestration; a dashboard; bulk import; public-facing deployment by default.

## Context and trust boundaries

    Producer process
      | authenticated request (loopback / trusted network)
      v
    HTTP ingress -> authentication -> authorization -> validation
      | DB transaction (commit before 202)
      v
    Durable repository
      | atomic claim; limited worker concurrency
      v
    Delivery coordinator -> signature -> destination policy -> HTTP client
      |                            |                     |
      |                        bounded retry          trusted endpoint
      v
    state transitions + attempt history + metrics

There are three distinct trust boundaries: untrusted producer input, persistent storage, and outbound network delivery. A trusted producer may still send an invalid or dangerous payload. Destination addresses never come directly from the submitted JSON.

## Runtime components

- **API adapter**: bounded request decoding, bearer authentication, client-scoped access, JSON response and errors.
- **Application layer**: accept event, query status, claim work, record outcome; owns the transition rules and checks.
- **Domain**: event identity, state, destinations, retry classification, signature envelope; no SQL or transport dependence.
- **Repository port**: explicit atomic operations with contract tests, not an ORM-like generic CRUD interface.
- **PostgreSQL adapter**: first durable implementation, pgx/v5, small connection pool and SQL migrations.
- **SQLite adapter**: planned separately after baseline tests; isolated implementation of the same semantic port.
- **Worker coordinator**: leases, concurrent processing, backoff, timeout, reclaims, graceful shutdown.
- **Delivery adapter**: hardened HTTP transport; static destinations, DNS/IP validation, no redirects, HMAC.
- **Observability**: structured logs, correlation identifiers, health/readiness, counters.

Dependency direction: adapters depend on application/domain; domain must not import adapters. Keep interfaces small and defined at the consumer boundary. Avoid copying enterprise-pattern abstractions from Java/TypeScript without a demonstrated need.

## Suggested Go package layout

    cmd/gorelay/main.go
    internal/api/
    internal/app/
    internal/domain/
    internal/store/
    internal/store/postgres/
    internal/delivery/
    internal/worker/
    internal/security/
    internal/config/
    internal/observability/
    migrations/postgres/
    docs/
    .github/workflows/

Future optional backend: internal/store/sqlite/. Tests should live next to Go packages, plus tests/integration/ and tests/acceptance/ when needed.

## Single-destination event

In v0.1, **one event targets one destination**. Therefore event status is also its delivery status. Fan-out later needs a delivery table keyed by (event_id, destination_id), with independent attempts and delivery states; do not simulate it by duplicating business identifiers.

The producer supplies a symbolic destination_id. A trusted operator configures the HTTPS URL, destination secret, client authorizations and private-network exceptions outside the repository. The producer cannot change the endpoint URL using event input.

## API request versus delivery envelope

Inbound request: type, destination_id, payload, idempotency key; client identity comes from authentication. The server generates event_id and creation timestamp.

Outbound event envelope: stable event_id, event_type, source_client_id, payload, created_at. Delivery-attempt timestamp and signature may differ between retries. Consumer must deduplicate using event_id, not signature or attempt number.

## Deploy topology

For the first release run one instance bound to 127.0.0.1 by default, backed by a dedicated PostgreSQL database and least-privilege account. A later multiserver deployment requires shared PostgreSQL, a properly bounded connection pool, instance identity, secure routing and additional integration tests.

Do not introduce Docker/Kubernetes as a runtime prerequisite. Docker Compose may be a test fixture, not the production dependency.

## Consistency and responsibility

- Successful admission response implies durable committed event, not remote delivery.
- No database transaction is held during outbound HTTP.
- Stale workers cannot commit results after losing their lease.
- Leases cannot stop a remote side effect already in flight; duplicates remain possible.
- Consumer owns deduplication and transactional processing of its own side effects.
- Producer should use its own transactional outbox when its business commit and event publication must not diverge.

See [API](api.md), [delivery](delivery-semantics.md), [worker design](workers.md), [security](security.md), and [integration](integration.md).

## Open questions / explicit design review

1. Default HTTP timeout and lease duration should be validated against DNS/TLS/network conditions.
2. Event retention and key expiration policy must be explicit before implementing deletion.
3. Client and destination provisioning may start as validated static configuration; avoid unauthenticated admin routes.
4. PostgreSQL and SQLite must each pass equivalent end-to-end behavior tests before claiming support.
