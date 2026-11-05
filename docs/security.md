# Security model and threat analysis

> **Status:** required controls, not shipped features. **Do not expose an unimplemented server publicly.**

## Threat boundaries

The threat model includes unauthorized callers, compromised or buggy producers, malicious payloads, compromised destinations, DNS rebinding, internal metadata endpoints, credential leaks, dependency vulnerabilities, and resource exhaustion.

Assets: API bearer secrets, destination signing keys, event bodies, event status integrity, database data, and host network reachability. Running in a private network reduces exposure but does not make input trusted.

## Inbound authentication and authorization

- Each producer gets a distinct opaque high-entropy bearer credential.
- Authenticate before processing or admitting events; derive `client_id` from server-side credentials, not request JSON.
- Map each client to permitted destination IDs. Reject unauthorized destinations.
- Consider only hashed credential representations at rest; secret provisioning and rotation are explicit deployment tasks.
- Validate request body size, headers, content-type, UTF-8, JSON envelope, duplicate keys and nesting depth.
- Scope event status queries to the authenticated client and use 404 for unowned IDs.
- Rate-limit ingestion and bound total pending queue/storage.
- Keep admin facilities off public API. Do not treat permissive CORS as a security control.
- Use TLS for non-loopback ingress, and configure reverse proxies to avoid leaking Authorization headers into access logs.

## SSRF and destination policy

**Never accept an arbitrary webhook URL from an event**. Producer supplies only a symbolic destination identifier; operators configure its URL and allowlist.

For every outbound connection:

1. Accept only explicit permitted schemes, hosts and ports; public targets require HTTPS.
2. Reject userinfo, malformed URLs, unexpected fragments and ambiguous encodings.
3. Resolve DNS and check **every candidate IP**, IPv4 and IPv6, against policy.
4. Dial the verified IP address rather than re-resolving an unvalidated hostname; preserve the correct TLS SNI and certificate hostname validation.
5. Disable automatic redirects, including 3xx to metadata services.
6. Reject loopback, link-local, private, multicast, unspecified and metadata destinations by default; allow private ranges only by a specific operator exception.
7. Disable environment proxy auto-discovery unless an explicit vetted proxy is configured.
8. Cap connection time, total deadline, concurrency, response bytes and idle connections.

Apply checks on every connection, including re-connections when DNS records change. Pinning a DNS answer forever is also incorrect; apply secure re-resolution on the next connection. Tests must include rebinding, IPv4-mapped IPv6, mixed permitted/forbidden DNS answers and redirect attempts.

## Outbound HMAC signature protocol v1

The exact outbound HTTP request body is a UTF-8 JSON envelope containing stable event ID, source, type, created_at and payload. For each delivery attempt:

```text
signing_input = UTF8(timestamp) + "." + exact_HTTP_body_bytes
signature = lowercase_hex(HMAC_SHA256(destination_secret, signing_input))
X-GoRelay-Signature: v1=<signature>
X-GoRelay-Timestamp: <RFC3339-UTC>
X-GoRelay-Event-ID: <stable event UUID>
X-GoRelay-Attempt: <integer>
```

The receiver validates a bounded time window (proposed ±5 minutes), verifies HMAC in constant time using raw body bytes, and atomically deduplicates the event ID with its side effect. **Timestamp alone is not replay protection**. The stable event ID is covered by the signed body; retry attempt and timestamp may change.

Provide destination-specific HMAC keys, separate from producer bearer tokens. Before a production release, specify key IDs and an overlap period for safe rotation, including exhaustive tests. Do not log signatures, raw secrets or potentially sensitive bodies.

## Data minimization and privacy

Log event_id, client_id (if non-sensitive), destination alias, status class, durations, attempts and request ID. Do not log payloads, authorization headers, full URLs with tokens, response bodies or private data. Keep diagnostics bounded and sanitized. Define a retention policy before storing personal/regulated data; no auto-deletion is planned for v0.1 because it can compromise idempotency.

## Host hardening

Unprivileged systemd identity, restrictive file modes, no capabilities by default, filesystem read-only where possible, temporary/private dirs, bounded memory/CPU/files, restart policy. PostgreSQL runtime role with least privilege, protected database backups and separate migration permissions. Default listen address: 127.0.0.1.

## Security test gates

Credential mismatch, client isolation, token rotation, signature tampering, time-skew, SSRF to private/metadata, mixed DNS answers, rebind, IP normalization, redirect, TLS validation, environment-proxy bypass, resource limits, JSON duplication, bounded diagnostics, graceful shutdown on hostile traffic.

Security review must record any missing feature explicitly, not label planned controls as already implemented.
