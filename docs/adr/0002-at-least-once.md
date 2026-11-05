# ADR-0002: At-least-once delivery, not exactly-once

Status: Proposed
Date: 2026-11-05

## Context
An HTTP response can be lost after the remote consumer has processed an event. No independent relay can atomically commit the remote side effect and its own status without cooperation from the consumer.

## Decision
Persist before acknowledging ingestion; deliver using stable event IDs, bounded retries and lease fencing. Document that duplicated remote delivery is possible. Require consumer idempotency; recommend a receiver inbox table transactionally joined to its side effect.

## Alternatives
At-most-once would drop events on ambiguous failure. Exactly-once is not a credible generic HTTP transport claim without receiver cooperation and stronger application protocol constraints.

## Consequences
At-least-once *attempted* delivery with bounded retries; after max attempts event remains DEAD_LETTER. Consumers control effect deduplication. No strict ordering guarantee.

## Verification / reversal
Test crash after remote 2xx but before local delivery commit and verify same event ID on retry. Do not change guarantee language without distributed end-to-end evidence.
