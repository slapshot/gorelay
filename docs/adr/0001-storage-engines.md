# ADR-0001: PostgreSQL primary; SQLite as future option

Status: Proposed
Date: 2026-11-05

## Context
GoRelay needs atomic durable admission, concurrent claims, retry scheduling and crash recovery. PostgreSQL and SQLite can both serve a persisted queue, but have different concurrency and operating profiles.

## Decision
Implement v0.1 against a dedicated PostgreSQL database using pgx. Define an application-level store port with **atomic domain methods**, not a fake lowest-common-denominator SQL abstraction. Evaluate a separate SQLite/WAL implementation later; first support single-instance SQLite only.

## Alternatives
SQLite-first: simpler to deploy and adequate for many small single-host workloads, but weaker multi-writer scalability and different claim semantics. External brokers: more moving parts than this MVP needs. PostgreSQL and SQLite simultaneously from day one: testing and design scope increase before core behavior is proven.

## Consequences
Run PostgreSQL service, maintain migrations/backups; benefit from MVCC and SKIP LOCKED. SQLite is **not** supported until separately built and tested. No claims of equivalent throughput or high availability.

## Verification / reversal
Revisit when storage operations dominate deployment effort or representative benchmarks show the PostgreSQL requirement is unnecessary for target installations. Require contract and recovery tests for both adapters.
