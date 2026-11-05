# GoRelay

**GoRelay** is an application-agnostic, self-hosted, durable HTTP event and webhook delivery service written in Go.

> **Status: design / pre-alpha.** The repository currently documents a proposed architecture and API. The dev branch contains a first, incomplete Go admission service; event delivery workers and production hardening are not implemented. No production-ready relay exists yet.

GoRelay accepts authenticated events, persists them **before acknowledging acceptance**, and asynchronously delivers signed HTTP requests to allowlisted destinations. It is intended for independent applications that need a small, observable event-delivery component without adopting a full message broker.

## Design goals

- Durable admission and **at-least-once** delivery (never claim exactly-once execution).
- Explicit consumer-side deduplication using a stable event identifier.
- Short atomic database transactions; network I/O occurs outside transactions.
- Retry with bounded exponential backoff, jitter, dead-letter states, and crash recovery.
- Lease tokens / fencing to prevent stale workers from committing results.
- Secure client authentication, destination authorization, HMAC signatures, SSRF defenses.
- Small dependency footprint; idiomatic Go, standard `net/http`, and `log/slog`.
- Self-hosted Linux deployment via `systemd`; no container orchestrator required.
- Portable delivery semantics across supported persistence backends.

## Persistence strategy

**PostgreSQL is the first implementation target.** It supports row-level locking and `FOR UPDATE SKIP LOCKED` for queue consumers. This choice also enables serious hands-on learning of PostgreSQL transactions, MVCC, indexing, operational recovery, and tuning.

**SQLite is a planned alternative, not an implemented feature.** In WAL mode SQLite is suitable for many single-host, moderate-volume workloads. It supports concurrent readers but only one writer at a time. A SQLite adapter will require different atomic-claim logic, explicit contention handling, and a shared backend contract test suite. No cross-backend concurrency equivalence is promised.

See [Persistence and backend trade-offs](docs/persistence.md).

## Intended API (proposal)

| Method | Route | Purpose |
| --- | --- | --- |
| `POST` | `/v1/events` | Authenticated durable ingestion |
| `GET` | `/v1/events/{id}` | Event status (client-scoped) |
| `GET` | `/healthz` | Liveness |
| `GET` | `/readyz` | Readiness |

This API is a **design contract**, not an available endpoint. Details: [API specification](docs/api.md).

## Delivery model

```text
Producer -> Authenticated HTTP API -> Durable event storage
                                          |
                                    Worker claims event
                                          |
                              Signed HTTP request to target
                                          |
                    2xx -> delivered | retry -> backoff | terminal -> dead letter
```

One event has one destination in v0.1. Multiple subscriptions, fan-out, management UI, and dynamic routing are explicitly out of scope.

## Documentation

- [Architecture and boundaries](docs/architecture.md)
- [API contracts and errors](docs/api.md)
- [State model and delivery semantics](docs/delivery-semantics.md)
- [PostgreSQL schema proposal](docs/database-postgresql.md)
- [PostgreSQL vs. SQLite](docs/persistence.md)
- [Security and threat model](docs/security.md)
- [Retry, scheduling, and worker algorithm](docs/workers.md)
- [Failure modes and acceptance tests](docs/testing.md)
- [Configuration](docs/configuration.md)
- [Observability and operations](docs/operations.md)
- [Linux/systemd deployment and recovery](docs/deployment.md)
- [Producer transactional outbox](docs/integration.md)
- [Implementation roadmap](docs/roadmap.md)
- [Contributing](CONTRIBUTING.md) and [security reporting](SECURITY.md)
- [Architecture decision records](docs/adr/README.md)

The dev branch already contains a candidate PostgreSQL migration and admission prototype. OpenAPI and delivery workers are not yet implemented. Read [implementation status](docs/implementation-status.md) on dev.

## Status and contributing

The main branch currently documents the design; development work is ongoing on **dev**. Please do not depend on its design as on a shipped binary or API. See [roadmap](docs/roadmap.md) for implementation milestones and required acceptance gates.

No application-specific proprietary source, real deployment addresses, credentials, guest data, or internal configurations are intended to be part of this repository.

## License

Licensed under the **MIT License**. See [LICENSE](LICENSE).

## Sources

- [Go release policy](https://go.dev/doc/devel/release)
- [PostgreSQL locking and SKIP LOCKED](https://www.postgresql.org/docs/18/sql-select.html)
- [SQLite WAL](https://www.sqlite.org/wal.html)
