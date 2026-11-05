# Persistence strategy: PostgreSQL now, SQLite later

Status: architectural decision; **only PostgreSQL is targeted by v0.1 implementation**. SQLite support remains a proposed extension.

## Why PostgreSQL first

PostgreSQL offers mature transactions, row-level locks, MVCC, robust concurrency for multiple worker processes, partial indexes, and FOR UPDATE SKIP LOCKED. It is a deliberate learning objective: isolation, lock behavior, WAL, backup/PITR, query plans, autovacuum, and operational monitoring should become first-class engineering topics.

A PostgreSQL server is additional operational cost (service, credentials, backups, updates, monitoring); GoRelay must not require other brokers or caches just to schedule deliveries.

## Why SQLite is plausible

For a single-host, single-process relay with moderate event rates, SQLite can be entirely adequate. WAL mode permits many readers concurrently with one writer. It is embedded and can remove a full DB service dependency. The application still needs durable, atomic claim and retry semantics; Go goroutines are not themselves a persistent queue.

SQLite WAL **allows only one writer at a time**, and WAL databases cannot be safely shared across machines over a network filesystem. Driver behavior, busy handling, checkpointing, fsync mode, and filesystem guarantees matter.

Official references: https://www.sqlite.org/wal.html and https://www.sqlite.org/lang_transaction.html.

## Port boundary

Use a narrow application-owned interface, conceptually:

    type EventStore interface {
        Admit(ctx, clientID, key, fingerprint, event) (AdmissionResult, error)
        GetOwned(ctx, clientID, eventID) (Event, error)
        ClaimDue(ctx, now, limit, lease) ([]ClaimedEvent, error)
        Complete(ctx, eventID, leaseToken, result) (bool, error)
        ExpireExhausted(ctx, now, batchSize) (int, error)
        Ready(ctx) error
    }

This is illustrative rather than compile-ready Go: actual types and DB-time behavior are defined during implementation. Do not implement a generic SQL query builder, hide backend-specific transactions behind leaky abstractions, or assume adapters can share SQL text.

All write operations must express **atomic domain operations**. A two-step Select-then-Update without a transaction or compare-and-swap check is unacceptable.

## PostgreSQL claim

Use one transaction for claim: SELECT ordered due events FOR UPDATE SKIP LOCKED, UPDATE status/lease/attempts and RETURNING claimed rows, then COMMIT. Do not hold the transaction across HTTP. Claimer/sweeper must coordinate attempt exhaustion.

Multiple instances may claim disjoint rows through the DB, subject to actual integration tests; lack of contention is not the same as globally ordered processing.

Reference: https://www.postgresql.org/docs/18/sql-select.html.

## SQLite claim (future implementation)

- Limit official SQLite deployment to **one GoRelay instance per database** initially.
- Enable WAL deliberately, check reported journal mode, set an explicit busy timeout and appropriate synchronous durability policy; do not rely on undocumented defaults.
- In an explicit **BEGIN IMMEDIATE** transaction acquire write intent, select due rows, update them with token/lease/attempts and commit promptly. An atomic UPDATE ... RETURNING strategy is also possible; SQLite RETURNING has been supported since 3.35.
- Test SQLITE_BUSY, timeouts, aborts, high read pressure, WAL checkpoints, and locked-file behavior.
- Never keep a write transaction open across network delivery.
- Do not run this database over NFS/SMB; require local filesystem and documented backup/checkpoint handling.
- Do not reuse PostgreSQL SKIP LOCKED queries; SQLite does not provide that feature.
- Multiple goroutines can deliver concurrently after short serialized claims; write concurrency is still constrained by SQLite itself.

References: https://www.sqlite.org/lang_returning.html and https://www.sqlite.org/wal.html.

## Contract tests for both engines

Use the same table-driven acceptance suite with store factories:
1. simultaneous same-client same-key admission -> one ID, other 200 or conflict as appropriate;
2. client isolation;
3. competing claims -> no overlapping active lease for the same event;
4. expired lease can be reclaimed;
5. old token cannot complete;
6. attempt budget enforced;
7. crash after commit, process restart and recovery;
8. durable terminal states;
9. time and index consistency;
10. queue pressure and backup/restore behavior.

Backend-specific performance tests must be labeled and must not claim equivalent multi-instance scalability.

## Migration and data portability

PostgreSQL uses UUID, JSONB, TIMESTAMPTZ, bytea and partial indexes. SQLite may use TEXT UUID, TEXT JSON, INTEGER timestamps and BLOB digest. Domain model remains stable, but schema migrations are separate per backend.

**Backend migration PostgreSQL <-> SQLite is not v0.1 scope**. A future migration tool needs snapshot consistency, integrity verification, and a deliberate cutover protocol so no live event is silently duplicated or dropped.

## Decision criteria

Default to PostgreSQL when running multiple instances, requiring simultaneous claimers, needing advanced backup/PITR, or aiming to learn database operations. Consider SQLite for embedded single-host deployments when measured write contention, checkpoint latency, backup procedures, and failover limits are acceptable.
