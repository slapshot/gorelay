# Verification plan and acceptance criteria

> **Status:** design-only acceptance suite. No tests have been executed for an implementation yet.

## Test strategy

Use Go `testing`, `httptest`, and `go test -race ./...`. The race detector checks Go memory races, **not** PostgreSQL transaction correctness. Therefore, use a real PostgreSQL instance for concurrency and recovery verification; use mocked clocks only where deterministic time control is intended.

Layers: pure domain/unit tests, HTTP handler tests, PostgreSQL adapter integration tests, backend-agnostic store contracts, subprocess crash tests, security fault injection, load/soak tests and isolated backup-restore rehearsals.

## Acceptance matrix

| ID | Scenario | Required result |
| --- | --- | --- |
| ADM-01 | Submit valid event | 202 only after committed insert |
| ADM-02 | 50 concurrent identical idempotency keys | Exactly one event ID |
| ADM-03 | Same key different body | 409, original unchanged |
| ADM-04 | DB unavailable during admission | Never return 202 |
| ADM-05 | Client timeout after DB commit | Retry returns same ID |
| AUTH-01 | Wrong/missing bearer | 401, no persistence |
| AUTH-02 | Client not authorized to destination | 403 |
| AUTH-03 | Cross-client status lookup | 404, no leak |
| API-01 | Invalid/oversized/duplicate-key JSON | Rejected without queue entry |
| CLM-01 | Many competing claimers | At most one unexpired lease in DB per event |
| CLM-02 | Reclaimed expired lease | New token, attempt increment |
| CLM-03 | Old worker completes late | Guarded update affects zero rows |
| DEL-01 | 200/202/204 | DELIVERED persisted |
| DEL-02 | 408/425/429/500/timeout | RETRY_WAIT with bounded delay |
| DEL-03 | 301/302/400/403 | Permanent failure, no redirect |
| DEL-04 | Exhaust max attempts | DEAD_LETTER without further automatic claims |
| REC-01 | SIGKILL after admission commit | Event survives restart |
| REC-02 | SIGKILL during HTTP | Expired lease recovers without state corruption |
| REC-03 | Crash after consumer 200 before DB result | Duplicate possible, stable event ID |
| REC-04 | SIGTERM | Admission stops; in-flight operations drain or recover |
| SEC-01 | Forbidden DNS/IP destination | No outbound connection |
| SEC-02 | Redirect to metadata IP | Not followed |
| SEC-03 | Invalid signature / replay | Receiver rejects |
| SEC-04 | Log inspection | No secrets or payload |
| OPS-01 | PostgreSQL unavailable | readyz 503, healthz liveness |
| OPS-02 | Restore from backup | Rows, indexes, keys and constraints valid |
| PERF-01 | Sustained load | Measure admission latency, claim throughput, backlog age |

## Failure injection points

Crash before insert, after insert but before commit, after commit before response, after claim commit, during outbound request, after consumer 2xx before outcome commit, during DB completion, during shutdown, during lease expiry. Each crash test should run as a subprocess terminated with SIGKILL, restarted against the same DB.

Distinguish confirmed delivery, ambiguous remote outcome, and confirmed permanent failure; verify attempt numbering.

## Persistence adapter tests

Every supported adapter must pass the same externally observable state machine tests. PostgreSQL has a multiclient/multi-claimer suite; SQLite, when introduced, must separately pass WAL, busy timeout, checkpoint, lock contention, crash recovery, local filesystem backup and **single-instance enforcement** tests. Never claim a backend supported until tests pass in CI.

## Quality gates

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `gofmt` verification
- Dependency audit and pinned third-party CI actions.
- Migration up/down or documented forward-only behavior in isolated database.
- Integration suite on real PostgreSQL.
- Security SSRF matrix, HMAC vectors, cancellation and shutdown.
- Load results documented as environment-specific measurements, not broad benchmark guarantees.

Use test fixtures with synthetic client IDs and generic order/inventory examples only. Do not commit any production data or credentials.
