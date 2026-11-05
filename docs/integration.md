# Generic producer and consumer integration

> **Status:** generic examples. No SDKs or application adapters are shipped yet.

GoRelay's core understands event metadata and JSON, not domain-specific data. It must be reusable by a billing system, observability pipeline, ecommerce application, publishing service or internal automation without code changes in GoRelay.

## Producer responsibilities

Use authenticated POST /v1/events with a stable Idempotency-Key per business event. Do not use a random new key on every HTTP retry. Interpret 202 as durable acceptance, 200 as an accepted duplicate and 409 as a client bug/key collision. On 5xx, connection reset or timeout, retry with the **same** key; do not assume the event was rejected.

Example abstract event:

```json
{
  "type": "inventory.updated",
  "destination_id": "analytics-hook",
  "payload": {
    "item_id": "item-42",
    "quantity": 7
  }
}
```

These names are illustrative only, not built-in plugins or preset destination definitions.

## Producer transactional outbox

If business state and event publication must be atomic, write the business mutation and an outbox row in the **same local application transaction**. An independent publisher reads unsent outbox rows, POSTs them with stable keys, and records acknowledgement. This bridges a non-atomic boundary between the producer's database and GoRelay.

```text
Producer DB transaction:
  modify domain record
  INSERT outbox(event_id, event_type, payload, delivery_status)
COMMIT

Outbox publisher:
  read pending outbox
  POST /v1/events with stable Idempotency-Key
  on confirmed 200/202, mark outbox published
```

If the publisher dies after GoRelay commit but before marking its outbox row, the next send resolves idempotently. The producer database can be SQLite or PostgreSQL independent of GoRelay's backend; each uses its own transaction mechanisms.

GoRelay does not make the producer's business transaction atomic by itself.

## Consumer responsibilities

Verify signature over **exact raw bytes** and timestamp, ensure event-ID deduplication, and send 2xx only after the desired acceptance semantics are durable. A typical consumer has an inbox table with UNIQUE(event_id), inserted in the same DB transaction as the effect. If event ID is already present, return 2xx without repeating the effect.

Do not deduplicate by signature (changes per retry), transport connection, body hash alone or attempt number. Receiver should tolerate out-of-order arrival.

## Versioning and contracts

Namespaced event types such as `inventory.updated` are producer-controlled. The payload schema and compatibility policy belong to the producer/consumer pair, not GoRelay. A future optional schema registry must be opt-in and keep the generic transport core unchanged.

A versioned outbound envelope and HMAC canonicalization should be tested across multiple languages using shared fixtures.

## Deployment models

- Same-host via loopback.
- Trusted private network/VPN with TLS and explicit authentication.
- Public HTTPS destination for outbound webhooks subject to strict allowlist/SSRF controls.
- No requirement for SaaS, a cloud queue, or container orchestration.

GoRelay may serve several client applications with separate credentials and authorizations; each remains independently deployable.

## Future adapters

Language-specific producer SDKs, integration examples and a CLI can live in separate packages/repositories. They must not be dependencies of the core relay server.
