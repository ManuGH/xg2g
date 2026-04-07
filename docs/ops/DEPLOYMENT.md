# Deployment Guide

xg2g ships as a single OCI image with a bundled FFmpeg runtime. The only
supported deployment path is `deploy/sync.sh`:

```bash
deploy/sync.sh --apply --ref <tag|sha>
```

This copies repo truth into `/srv/xg2g` and `/etc/systemd/system`, reloads
systemd, and runs verification checks.

## Minimum Requirements

| Requirement | Value |
| :--- | :--- |
| **Runtime** | Docker or Podman |
| **Supervisor** | systemd (manages container lifecycle) |
| **Network** | Enigma2 receiver reachable from host |
| **HTTPS** | Required for non-loopback browser access |

## Detailed Documentation

| Topic | Document |
| :--- | :--- |
| Host layout, shipped artifacts, unit locations | [Installation Contract](INSTALLATION_CONTRACT.md) |
| FFmpeg paths, GPU passthrough, runtime invariants | [Runtime Contract](DEPLOYMENT_RUNTIME_CONTRACT.md) |
| systemd start/stop/reload, Compose, smoke checks | [Operator Runbook](RUNBOOK_SYSTEMD_COMPOSE.md) |

## Deployment Artifacts

Repo-side deploy truth lives under `deploy/`:

- `deploy/sync.sh --check --ref <tag|sha>` — dry-run comparison against host
- `deploy/xg2g.env.schema.yaml` — contract for validating `/etc/xg2g/xg2g.env`
- `deploy/docker-compose.yml` — production Compose template
- `deploy/xg2g.service` — systemd unit

Direct host edits, ad-hoc file copies, and manual `/srv/xg2g` drift are not
supported deployment workflows.
