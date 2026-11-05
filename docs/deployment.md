# Linux / systemd deployment and rollback design

> **Status:** deployment guide blueprint. GoRelay binary and service unit are not yet available.

Target: a generic self-hosted Linux server, not tied to any proprietary application. Debian is the initial test distribution. No requirement for Docker, Kubernetes or a managed cloud service.

## Directory layout

```text
/opt/gorelay/releases/<version>/gorelay     # immutable release binary
/opt/gorelay/current -> releases/<version> # controlled symlink
/etc/gorelay/                             # protected config
/var/lib/gorelay/                         # optional persistent local state
```

Service account `gorelay`: non-login, no root privileges, no application-specific groups. Restrictive file modes. The database is operated under its own Linux and PostgreSQL identities.

## Prerequisites and preflight

- Supported Linux architecture, available CPU/RAM/disk, correct time synchronization.
- Compatible PostgreSQL instance and dedicated DB/roles.
- A free loopback port (proposed 8092; configurable), no collision with other services.
- Network access **only** to approved destinations.
- Working backup and restore procedure, available rollback binary, validated config.
- Secrets stored in root-managed protected paths or systemd credentials.

## Example systemd unit sketch (NOT production-ready)

```ini
[Unit]
Description=GoRelay durable webhook delivery service
After=network-online.target postgresql.service
Wants=network-online.target

[Service]
Type=simple
User=gorelay
Group=gorelay
WorkingDirectory=/opt/gorelay/current
ExecStart=/opt/gorelay/current/gorelay
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
LockPersonality=true
CapabilityBoundingSet=
ReadWritePaths=/var/lib/gorelay
# LoadCredential=... or an appropriately protected EnvironmentFile=...
# MemoryMax=..., CPUQuota=..., LimitNOFILE=...

[Install]
WantedBy=multi-user.target
```

Validate unit restrictions on the actual host and when using a remote DB. The above `After=postgresql.service` matters only if PostgreSQL is local; it should be adapted rather than blindly reused. Do not expose GoRelay publicly by changing bind address without a full network/auth/TLS review.

## Release procedure

1. Build reproducibly from tagged source; record Go version, dependency checksums, Git commit and artifact checksum.
2. Verify release artifact signature/checksum and migration compatibility.
3. Take a verified backup before changes to stateful components.
4. Stop/drain service for migrations requiring exclusive access.
5. Run explicit forward migrations using the migration role, checking schema version.
6. Install versioned binary and atomically update symlink.
7. Start under systemd, check journal and /healthz and /readyz.
8. Run synthetic authorized admission/delivery smoke test to an isolated destination.
9. Retain previous compatible binary; monitor failures/backlog.

No database schema rollback should be assumed safe without reviewing backward compatibility and data migration. Binary rollback may require a compatibility window; always specify which versions may operate on the same schema.

## Restore

Restore PostgreSQL to an isolated database, validate counts, constraints, schema, sample event states and secrets configuration, then document cutover. Replaying pending events after restore can produce duplicates; consumers must deduplicate. Backup validation must include offline recovery and not merely successful backup creation.

## First deployment safety gates

No changes to existing applications or background services; no public port; unique service identity; separate DB and roles; resource limits; backups; security and failure tests pass; all integration endpoints are synthetic until explicitly enabled by an operator.
