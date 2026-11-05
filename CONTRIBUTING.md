# Contributing to GoRelay

Thanks for helping build a small, reliable, generic event-delivery service.

> Current project status: documentation and design / pre-alpha. No implementation exists yet.

## Before proposing work

- Read [architecture](docs/architecture.md), [delivery guarantees](docs/delivery-semantics.md), [security](docs/security.md), and [roadmap](docs/roadmap.md).
- Prefer issues describing a reproducible failure, contract ambiguity, threat model gap, or measurable improvement.
- Keep domain examples generic; GoRelay should not depend on any specific company, application or hospitality platform.
- Never include real credentials, private deployment addresses, personal event payloads or proprietary source.

## Workflow

1. Work from the `dev` branch when available; create a short-lived feature branch.
2. For behavior-changing work, update specs/ADRs and acceptance tests before or alongside code.
3. Use idiomatic Go and small packages. Add interfaces at real dependency boundaries, not reflexively.
4. Add tests using the real DB for transaction claims; mocks alone do not prove atomicity.
5. Run formatting, vet, unit tests, race detector and integration tests relevant to the change.
6. Open a PR against `dev` with rationale, migration risk, test evidence and security implications.
7. Merge to `main` only after review and passing gates.

## Code review checklist

- Stable API behavior and error statuses.
- Atomic DB writes and no HTTP calls inside DB transactions.
- Lease ownership/fencing and bounded retries.
- No SSRF, secret logging or unvalidated redirects.
- Cancellation and graceful shutdown.
- Backward-compatible migrations, or a documented breaking change.
- Documentation matches implemented behavior.

## License

This repository uses the MIT license. Unless a different agreement is explicitly made, contributions should be compatible with that license.

## Security reports

Do not disclose exploit details or real credentials in public issues. Until a formal security-contact process is established, avoid publishing sensitive vulnerability reproductions publicly. A private disclosure path must be documented before the first supported production release.
