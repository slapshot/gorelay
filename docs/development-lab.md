# Isolated Debian development lab (pre-alpha)

**Scope:** a disposable Debian 13 guest (VM or dedicated LXC on Proxmox), not the Proxmox host or any existing production container. Do not run the relay with sensitive production events.

## Why a separate guest?

GoRelay is experimental. A small dedicated Debian guest permits snapshots, deliberate PostgreSQL migrations, testing crashes, and full removal without affecting other applications. LXC is sufficient for a single-host Go + PostgreSQL lab; a full VM offers stronger kernel isolation and makes future network/security experiments easier. No Docker/Kubernetes requirement.

Suggested **lab-only** starting resources: 2 vCPU, 2 GiB RAM, 20 GiB disk. Prefer a guest with its own backup/snapshot. Restrict inbound ports; GoRelay and test sink bind to loopback.

## Toolchain prerequisites

Debian 13's default `postgresql` metapackage currently selects PostgreSQL 17. Install **PostgreSQL 18** from the project's maintained Debian Apt repository, not the default metapackage:

https://www.postgresql.org/download/linux/debian/

```bash
sudo apt update
sudo apt install -y git curl ca-certificates openssl postgresql-common
sudo /usr/share/postgresql-common/pgdg/apt.postgresql.org.sh
sudo apt update
sudo apt install -y postgresql-18 postgresql-client-18
psql --version
```

Install Go 1.27.3 using the official binary archive with SHA256 verification (example is **linux/amd64** only; change artifact/sha if architecture differs):

```bash
curl -fsSLO https://go.dev/dl/go1.27.3.linux-amd64.tar.gz
echo '65efbc80fdc1fa2cd2631cad5b481777a68e8725f34e89f50fc6ac714e379b40  go1.27.3.linux-amd64.tar.gz' | sha256sum -c -
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.27.3.linux-amd64.tar.gz
sudo ln -s /usr/local/go/bin/go /usr/local/bin/go
sudo ln -s /usr/local/go/bin/gofmt /usr/local/bin/gofmt
go version
```

Do not override an existing installation on a shared container; these instructions are intended only for a **new isolated guest**.

## Database and roles

For quick disposable lab testing, use a private database and a single local PostgreSQL login. Production installations must separate a migrator and a least-privileged runtime role.

```bash
sudo -u postgres createuser --pwprompt gorelay_lab
sudo -u postgres createdb --owner=gorelay_lab gorelay_lab
```

PostgreSQL normally listens on loopback; keep it that way. Test with `pg_isready`.

## Source and migration

```bash
git clone https://github.com/slapshot/gorelay.git
cd gorelay
git switch dev
go mod download
export GORELAY_DATABASE_URL='postgres://gorelay_lab:REPLACE_WITH_LAB_PASSWORD@127.0.0.1:5432/gorelay_lab?sslmode=disable'
psql "$GORELAY_DATABASE_URL" -v ON_ERROR_STOP=1 -f migrations/postgres/0001_init.up.sql
export GORELAY_TEST_DATABASE_URL="$GORELAY_DATABASE_URL"
go test ./...
go test -race ./...
go vet ./...
go build -o /tmp/gorelay ./cmd/gorelay
```

If you have already applied migration 0001, **do not run it twice**. The v0.1 prototype uses explicitly applied migrations. Quote URL-special characters in the PostgreSQL password or use a more secure .pgpass/credential setup instead of history-visible exports.

## Local receiver and signed delivery

Keep the receiver and the relay on the same guest. In two separate shells, set the **same** high-entropy signing key; the receiver validates signatures, but deliberately does **not** perform persistent deduplication.

```bash
# Terminal A, from the repository
umask 077
export GORELAY_DEV_SINK_SECRET="$(openssl rand -hex 32)"
echo "$GORELAY_DEV_SINK_SECRET" > /tmp/gorelay-dev-sink-secret
chmod 600 /tmp/gorelay-dev-sink-secret
go run ./examples/dev-sink
```

```bash
# Terminal B, from the repository
umask 077
export GORELAY_DEV_SINK_SECRET="$(cat /tmp/gorelay-dev-sink-secret)"
export GORELAY_TOKEN_DEMO="$(openssl rand -hex 32)"
printf '%s' "$GORELAY_TOKEN_DEMO" > /tmp/gorelay-token
chmod 600 /tmp/gorelay-token
install -m 600 examples/clients.example.json /tmp/gorelay-clients.json
install -m 600 examples/destinations.example.json /tmp/gorelay-destinations.json

export GORELAY_DATABASE_URL='postgres://gorelay_lab:REPLACE_WITH_LAB_PASSWORD@127.0.0.1:5432/gorelay_lab?sslmode=disable'
export GORELAY_CLIENTS_FILE=/tmp/gorelay-clients.json
export GORELAY_DESTINATIONS_FILE=/tmp/gorelay-destinations.json
export GORELAY_ENABLE_ADMISSION_PREVIEW=true
export GORELAY_ENABLE_DELIVERY_PREVIEW=true
export GORELAY_LISTEN_ADDR=127.0.0.1:8092
# GORELAY_DATABASE_URL must also be set as above.
go run ./cmd/gorelay
```

```bash
# Terminal C
TOKEN="$(cat /tmp/gorelay-token)"
curl -fsS -i http://127.0.0.1:8092/readyz
curl -sS -i -X POST http://127.0.0.1:8092/v1/events \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: demo-item-001' \
  --data '{"type":"inventory.updated","destination_id":"example-sink","payload":{"sku":"demo-1","quantity":7}}'
```

Expected: POST returns HTTP 202; the terminal A sink logs the event ID; querying `GET /v1/events/<ID>` with the same token returns status DELIVERED after a short interval. Resending the **same** key and content returns 200 with the original ID. The event remains durable in PostgreSQL across process restarts.

Inspect PostgreSQL directly:

```sql
SELECT status, attempts, created_at, delivered_at
  FROM events ORDER BY created_at DESC LIMIT 10;
SELECT event_id, attempt_number, outcome_code, http_status
  FROM delivery_attempts ORDER BY id DESC LIMIT 10;
```

To test retry/dead-letter, use the integration suite and a disposable database. Do not manually edit persisted state in a deployed service.

## Important limits

- This is a **pre-alpha lab**; there is no public endpoint or production deployment supported.
- Outbound preview is opt-in, and private destinations other than loopback are blocked by default.
- The developer receiver has no durable inbox; it demonstrates HMAC but **not** exactly-once consumer effects.
- No SQLite adapter yet, and no full security audit, migration rollback, performance/load testing, or backup/restore evidence.
- Never run these experiments in an existing application database or shared service account.
