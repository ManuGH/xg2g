# xg2g System Overview 2026

**Status:** Active current-state reference

**Snapshot date:** 2026-07-28

**Scope:** Repository layout, supported host runtime, network edge, storage,
application boundaries, and operator lifecycle

This document is the shortest authoritative description of the system that
exists now. Detailed behavior remains normative in the linked contracts.
Historical ADRs, release notes, audit reports, and dated runbook observations
record earlier states and must not be used as current deployment instructions.

## Supported System

| Area | Current truth |
| :--- | :--- |
| Server host | Linux `amd64` or `arm64` with systemd |
| Container runtime | Docker Engine with the Compose v2 plugin |
| Backend | Go 1.26.8, one OCI image with pinned FFmpeg |
| WebUI | React/Vite under `apps/webui`, built and embedded into the backend image |
| API | Versioned `/api/v3`, generated from `backend/api/openapi.yaml` |
| Production supervisor | `xg2g.service` → canonical Compose resolver |
| Backend listener | `127.0.0.1:8088` by default |
| Remote browser access | HTTPS through an existing same-host proxy or explicitly managed Caddy |
| Durable state | `/var/lib/xg2g` |
| DVR/HLS scratch | Configurable `XG2G_HLS_ROOT`; may use a persistent mounted HDD/SSD/NVMe |
| Backups | Daily verified durable-state/config archives under `/var/backups/xg2g` |

macOS is a supported development and test workstation. The guided persistent
host installer targets Linux because it installs systemd units and a Docker
Compose runtime.

## Installation And Release Truth

Official release archives contain the binary plus every deployment input
consumed by `infra/systemd/sync.sh`. The beginner path is:

```bash
sudo ./infra/systemd/setup-linux.sh
```

GitHub-generated branch ZIPs are not immutable release artifacts and are
rejected. The canonical host layout is `/srv/xg2g` plus systemd units under
`/etc/systemd/system`; direct edits there are drift. See the
[Installation Contract](../ops/INSTALLATION_CONTRACT.md) and
[Deployment Guide](../ops/DEPLOYMENT.md).

## HTTPS And Reverse Proxies

Caddy is not mandatory and is never silently adopted as the operator's edge.
The guided installer exposes four explicit topologies:

1. Existing same-host reverse proxy — the default HTTPS choice.
2. Managed public Caddy — opt-in for DNS/ACME.
3. Managed internal Caddy — opt-in for LAN/VPN with a private CA.
4. Local-only loopback HTTP — same-host evaluation only.

With an existing proxy, setup does not edit or reload it, creates no Caddyfile,
pulls no Caddy image, and does not enable or start `xg2g-caddy.service`. It
configures xg2g's allowed origin/trusted proxy and validates the real HTTPS
path. The Caddy unit file is installed inertly for deterministic future use and
is guarded by `ConditionPathExists=/etc/xg2g/Caddyfile`.

The standard backend remains loopback-only. Therefore the beginner existing
proxy path expects nginx, Traefik, Caddy, HAProxy, or another proxy on the same
Linux host. A proxy on another host requires a deliberately exposed,
firewalled/VPN-restricted backend listener. See
[Reverse Proxy & HTTPS](../../infra/systemd/REVERSE_PROXY.md).

## Runtime And Route Policy

`internal/api` owns top-level server lifecycle, the outer chi router,
compatibility routes, and production route-policy binding. API v3 handlers,
generated operation routing, authentication, scopes, exposure rules, and
feature subpackages live under `internal/control/http/v3`.

Production router construction registers outer and v3 routes through one policy
binding registry. Deadline/streaming policy is attached at registration time;
v3 routes use the same authentication, scope, household, and exposure stack
whether constructed directly or through the production registrar. Governance
tests compare the mounted production inventory against an independent policy
baseline.

The WebUI is a thin client. Server/OpenAPI contracts own playback and security
decisions; generated TypeScript access stays behind the WebUI client wrapper.
The production bundle is generated into
`backend/internal/control/http/dist`.

## Storage Model

- `/var/lib/xg2g` stores durable databases and JSON state.
- `XG2G_HLS_ROOT` stores temporary live DVR/HLS segments.
- Receiver recordings remain receiver/external-storage content.
- `/var/backups/xg2g` stores mode-`0600` online backups of durable state and
  protected configuration.

The setup wizard estimates DVR space from rewind duration and simultaneous
streams. A dedicated path must already be mounted and persistent; setup never
partitions, formats, mounts, or deletes a disk. Default uninstall preserves
configuration, durable data, backups, recordings, and external DVR storage.
See [Storage Layout](../ops/STORAGE_LAYOUT.md).

## Operator Lifecycle

The installed `xg2g-admin` command provides:

```bash
sudo xg2g-admin doctor
sudo xg2g-admin backup
sudo xg2g-admin restore ARCHIVE --yes
sudo xg2g-admin update --ref vX.Y.Z
sudo xg2g-admin rollback --yes
sudo xg2g-admin uninstall
```

Restore accepts only regular files from the governed backup inventory, rejects
links/unsafe paths, verifies checksums and sizes, and takes a safety backup.
Updates back up first and automatically reinstall the prior ref after a failed
health check. See [Backup & Restore](../ops/BACKUP_RESTORE.md) and the
[Operational Lifecycle Contract](../ops/OPERATIONAL_LIFECYCLE_CONTRACT.md).

## Repository And Toolchain

| Path | Ownership |
| :--- | :--- |
| `backend/` | Go backend, OpenAPI source, config registry, scripts, tests |
| `apps/webui/` | React/Vite UI, client wrapper, UI contracts and tests |
| `infra/systemd/` | Canonical Linux deployment and HTTPS reference bundle |
| `docs/` | Active contracts plus clearly dated historical evidence |
| `mk/` | Root Make workflow implementation |

Go is pinned to 1.26.8 and Node.js to 24. Use `mise install` or the
devcontainer, then `make doctor`. `make ci-pr` is the authoritative local PR
bundle and `make pre-push` is the required cheap guard before every push.

## Source-Of-Truth Order

When documents disagree, use this order:

1. Machine-enforced schemas, generated contracts, and tests.
2. Active installation/runtime/security contracts.
3. This current-state overview.
4. Guides and runbooks.
5. Historical ADRs, audits, release notes, and dated incident observations.

Do not rewrite historical evidence to look current. Update this overview and
the matching active contract when the supported system changes.
