# Implementation status (pre-alpha)

This page distinguishes implemented pre-alpha capabilities from production-ready guarantees. Do not deploy this version for production traffic.

## Available in the dev branch

- Standalone Go HTTP process with loopback-only bind.
- Strict JSON parsing with nested duplicate-key rejection, payload size cap and input validation.
- Per-client bearer authentication and destination authorization from protected local configuration.
- `POST /v1/events` with PostgreSQL atomic insertion and client-scoped idempotency key.
- `GET /v1/events/{id}` with client isolation.
- Liveness and database/schema readiness.
- PostgreSQL schema migration SQL (must be applied explicitly by an operator).
- Unit tests and PostgreSQL integration tests in GitHub Actions.
- PostgreSQL atomic claims, fencing, delivery attempts, retry/dead-letter states and lease-expiration sweeper.
- Opt-in worker pool and signed outbound HTTP delivery to operator-configured destinations.
- Explicitly permitted local-loopback test sink with HMAC verification; strict default outbound IP policy.

## NOT yet available

- Comprehensive fault injection, sustained-load evaluation, and end-to-end production hardening.
- Production signing key rotation, audited key management and administrative DLQ replay.
- SQLite adapter, Prometheus metrics, alerting, load testing and production deployment.
- Security review of complete outbound SSRF controls.
- End-to-end producer outbox/consumer inbox examples.

**Important:** Admission is disabled by default. Outbound delivery additionally requires `GORELAY_ENABLE_DELIVERY_PREVIEW=true` and a private destination configuration. Both flags are intended solely for an isolated development lab. This build is not approved for production traffic.

See [the isolated Debian lab guide](development-lab.md) for a complete, reproducible end-to-end exercise.

## Running for local development

1. Create a disposable PostgreSQL database and migration-only role.
2. Apply `migrations/postgres/0001_init.up.sql` using a migration role. Grant DML (events, delivery_attempts) and schema-read privileges to the separate runtime role.
3. Create a JSON config analogous to `examples/clients.example.json` and chmod 0600. Export the referenced high-entropy token as an environment variable.
4. Set `GORELAY_DATABASE_URL` to the runtime PostgreSQL DSN and `GORELAY_CLIENTS_FILE` to the absolute path.
5. For admission-only experiments set `GORELAY_ENABLE_ADMISSION_PREVIEW=true`; to deliver to a signed test sink, additionally configure `GORELAY_DESTINATIONS_FILE` and `GORELAY_ENABLE_DELIVERY_PREVIEW=true`. Run `go run ./cmd/gorelay` with a supported Go toolchain.
6. Check `curl -fsS http://127.0.0.1:8092/healthz` and `/readyz`.

Do not expose the pre-alpha service on public interfaces. PostgreSQL driver and checksums are pinned in go.mod and go.sum.

## Compatibility note

The implementation starts on Go >=1.25 because pgx/v5 v5.12.0 requires it. The CI targets a recent stable Go release. The domain and API use standard-library facilities available since Go 1.23, but the full binary requires the newer dependency toolchain.

## Potential protocol adjustment

The canonical event fingerprint uses `encoding/json` serialization with stable sorted object keys and preserved lexical JSON number spellings. `1` and `1.0` therefore produce different fingerprints and cannot reuse an idempotency key. This must be documented as a deliberate v0.1 API rule or replaced with a rigorously tested numeric canonicalization before an initial stable release.
