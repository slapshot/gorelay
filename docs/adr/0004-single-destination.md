# ADR-0004: Single destination per event in MVP

Status: Proposed
Date: 2026-11-05

## Context
Fan-out creates separate delivery states and retry schedules for every subscriber, making a single event status ambiguous.

## Decision
Every admitted event targets exactly one authorized destination in v0.1. The event's delivery state is unambiguous. Later fan-out will require a first-class deliveries table and independent per-target leases/attempts.

## Alternatives
Multiple destinations from v0.1, or duplicating source event into many opaque queue records. Both increase complexity before a baseline has been tested.

## Consequences
Producers that need multiple destinations initially publish separate events with separately scoped idempotency keys. Ordering across these events is not promised.

## Verification / reversal
Add fan-out only with a reviewed schema, new API semantics and failure-mode suite.
