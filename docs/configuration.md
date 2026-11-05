# Configuration reference (design)

> The variables below are **proposed names**. No parser or executable currently consumes them.

The initial deployment model uses environment configuration plus a protected destination/credential configuration file. Avoid putting bearer tokens, HMAC secrets and raw database passwords into world-readable service unit files, public logs or shell history. Prefer a protected environment file or systemd credential loading, then move to more advanced secret management only if needed.

## Proposed environment variables

| Variable | Default | Purpose |
| --- | --- | --- |
| `GORELAY_LISTEN_ADDR` | `127.0.0.1:8092` | Local HTTP bind |
| `GORELAY_STORE` | `postgres` | Persistence adapter; SQLite not shipped in v0.1 |
| `GORELAY_DATABASE_URL` | no default | Protected PostgreSQL DSN |
| `GORELAY_WORKERS` | `4` | Concurrent outbound workers |
| `GORELAY_CLAIM_BATCH` | `16` | Work to claim per transaction |
| `GORELAY_HTTP_TIMEOUT` | `10s` | Per attempt total timeout |
| `GORELAY_LEASE_DURATION` | `30s` | DB lease, must exceed HTTP timeout |
| `GORELAY_MAX_ATTEMPTS` | `8` | Started attempts including interrupted claims |
| `GORELAY_BACKOFF_BASE` | `2s` | Retry backoff initial interval |
| `GORELAY_BACKOFF_MAX` | `300s` | Retry delay ceiling before Retry-After |
| `GORELAY_RETRY_AFTER_MAX` | `1h` | Cap on external Retry-After |
| `GORELAY_MAX_BODY_BYTES` | `262144` | Max incoming request bytes |
| `GORELAY_MAX_OUTSTANDING` | TBD | Backpressure queue budget |
| `GORELAY_LOG_LEVEL` | `info` | slog level |
| `GORELAY_LOG_FORMAT` | `json` | Structured stdout/journald |
| `GORELAY_DESTINATIONS_FILE` | no default | Protected config containing endpoint allowlists and secret references |

All duration formats, unknown variable handling, and file schema require parser tests before they are considered stable. Fail at startup on invalid config, ambiguous destinations, missing required secrets and unsafe public binding unless explicitly authorized.

## Destination configuration (illustrative, not implemented)

```yaml
destinations:
  - id: accounting-service
    url: https://webhooks.example.net/ingest
    secret_env: GORELAY_SIGNING_KEY_ACCOUNTING
    allowed_client_ids:
      - demo-producer
    allowed_cidrs: []
```

A config file containing a URL is **trusted operator input** but still undergoes URL/TLS/DNS/IP validation. `secret_env` is the name of a secret variable, not the secret itself. Be explicit about deployment-specific private destinations and never ship broad private-network exceptions.

## Validation and precedence

Prefer one documented order: CLI explicit flags (where supported) > environment > default. Reject contradicting flags rather than guessing. Avoid hot reload of credentials/destinations until atomic validation and rotation are specified.

## SQLite planned configuration

A future SQLite adapter may use a local absolute file path (e.g. `GORELAY_SQLITE_PATH=/var/lib/gorelay/events.db`), WAL, busy timeout, synchronous FULL/NORMAL chosen by durability policy, checkpoint limits, one supported instance per DB, and backup using SQLite online backup API. **Do not set `GORELAY_STORE=sqlite` in v0.1 expecting it to work.**

## Operations

Document a sample config with only redacted fake secrets in the repository. Operator-provided secrets belong under restricted permissions. Validate effective configuration without printing secret values.
