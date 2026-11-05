# ADR-0005: Separate domain contract from storage engines

Status: Proposed
Date: 2026-11-05

## Context
PostgreSQL and SQLite expose different claim, transaction, locking, data-type and backup mechanisms. A reusable relay should not make business and delivery logic depend on SQL dialect.

## Decision
Define small application-facing operations for admit, client-scoped lookup, atomic claim, fenced completion, expiry and readiness. Implement adapters independently. Keep the domain state machine and external API backend-neutral. Do not introduce a generic ORM or high-level framework merely for portability.

## Alternatives
Embed SQL in HTTP handlers, generic SQL builder, or duplicate entire application per backend. Each increases coupling or hides engine-specific semantics.

## Consequences
Adapter-specific schema migrations and engine-specific tests remain necessary. The shared contract guarantees observable semantics, not equivalent throughput or deployment topology.

## Verification / reversal
Test both adapters with the same ingestion, locking, retry, completion and crash-recovery suite. Revise interface only when a concrete use-case exposes poor boundaries.
