# Security policy

## Supported versions

**No released or supported version yet.** GoRelay is pre-alpha documentation-first; do not deploy it for sensitive or production traffic.

## Reporting vulnerabilities

Please do not open a public issue containing a live exploit, secret, internal address, or personal data. Prefer GitHub's **Report a vulnerability** / private vulnerability reporting if it has been enabled for this repository. A verified private reporting channel and response policy must be established before the first supported release.

## Design requirements

See [threat model and security controls](docs/security.md). In particular: distinct producer credentials, allowlisted destinations, connection-time SSRF protection, fixed security boundaries, HMAC-SHA256 outbound signatures, receiver deduplication, bounded bodies and timeouts, and safe logs.

## Disclosure hygiene

Use synthetic payloads for proof-of-concept reproduction. Never expose actual keys, production traffic or customer data. A fix and tests should be developed and validated before public disclosure when practical.
