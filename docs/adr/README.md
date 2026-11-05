# Architecture Decision Records

An ADR captures why a design choice was made, the alternatives considered, its consequences, and what could trigger reconsideration. ADRs are **proposed** until the implementation reviews/accepts them.

- [ADR-0001: PostgreSQL primary, SQLite future adapter](0001-storage-engines.md)
- [ADR-0002: At-least-once HTTP delivery](0002-at-least-once.md)
- [ADR-0003: Symbolic allowlisted destinations](0003-destination-routing.md)
- [ADR-0004: One destination per event in v0.1](0004-single-destination.md)
- [ADR-0005: Backend-agnostic application core](0005-storage-ports.md)

Template for new decisions:

```markdown
# ADR-NNNN: Short title
Status: Proposed | Accepted | Superseded
Date: YYYY-MM-DD

## Context
## Decision
## Alternatives
## Consequences
## Verification / reversal conditions
```

Accepted decisions can still be superseded via a new ADR; do not rewrite history without explanation.
