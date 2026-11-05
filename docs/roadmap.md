# Delivery roadmap

> This is an engineering plan. No milestone is complete unless independently verified and tagged as such.

## Milestone 0 — specifications and public repository

- [x] Create public repository and documentation-first README.
- [ ] Review contracts and ADRs.
- [x] Choose MIT license and add contribution policy.
- [ ] Add machine-readable OpenAPI 3.1 and validate examples.
- [ ] Convert PostgreSQL SQL sketch into versioned migration(s) and run on real PostgreSQL.
- [ ] Define threat model sign-off and accepted limits.

**Exit criterion:** a contributor can implement the same external contract without guessing critical behavior.

## Milestone 1 — Go foundation

- [ ] Go module using supported stable Go (verify at implementation time).
- [ ] cmd/gorelay, config validation, structured logging, cancellation and signal handling.
- [ ] HTTP health/readiness with tests.
- [ ] PostgreSQL connection pool and schema version validation.
- [ ] GitHub Actions: format, vet, unit tests, -race, dependency checks.

**Exit:** clean CI and successful localhost startup against disposable PostgreSQL.

## Milestone 2 — ingestion and durable queue

- [ ] Per-client credentials and destination authorization.
- [ ] Strict JSON validation, limits, idempotency key and deterministic fingerprint.
- [ ] Atomic PostgreSQL admission and status lookup.
- [ ] Unit/integration test matrix for concurrent identical and conflicting requests.
- [ ] Claim implementation, attempt audit rows, leases and fencing.
- [ ] Configurable worker pool and 2xx delivery.

**Exit:** durable admission, delivery and recovery from forced shutdown; no false 202.

## Milestone 3 — retry and security hardening

- [ ] Retry scheduler with full jitter, Retry-After and bounded attempt budget.
- [ ] Persistent dead-letter states and expiry sweeper.
- [ ] SSRF protection at actual dial target, TLS verification, redirect disabled.
- [ ] HMAC signing, rotation protocol, consumer example and test vectors.
- [ ] Client and global admission rate/queue budgets, graceful draining.
- [ ] Security/race/fault-injection gates.

**Exit:** no unresolved blocker from acceptance or threat model; clear operational limits.

## Milestone 4 — self-hosted release

- [ ] Versioned binaries, checksums, release notes.
- [ ] systemd unit and least-privilege installation recipe.
- [ ] Database backup/restore validated, rollback rehearsed.
- [ ] Metrics, safe logs and operations runbooks.
- [ ] Benchmark report with environment and methodology.
- [ ] Publish first pre-release with honest feature status.

**Exit:** repeatable installation and isolated restore exercise.

## Milestone 5 — portability and ecosystem

- [ ] Re-evaluate whether a SQLite backend adds enough operational value.
- [ ] Implement SQLite adapter using WAL and atomic SQLite-specific claims.
- [ ] Run shared store semantics suite plus SQLITE_BUSY/checkpoint/crash tests.
- [ ] Document single-instance boundary, backup API and migration limitations.
- [ ] Publish generic producer/consumer examples, language SDKs if useful.
- [ ] Consider fan-out model and manual dead-letter replay in separate ADRs.

**Exit:** SQLite declared supported only after all backend-specific contract tests pass.

## Explicit scope control

- A business-specific adapter cannot introduce proprietary domain policy into GoRelay.
- No code from unrelated private repositories will be copied to the public repo.
- Do not promise exactly-once, strict ordering, multi-instance SQLite or production readiness before evidence.
- Each milestone uses a branch, CI checks, review, then merge; no unreviewed deployment into another application's runtime.
