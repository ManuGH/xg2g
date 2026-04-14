# Deployment Guide

xg2g ships as a single OCI image with a bundled FFmpeg runtime. The only supported deployment path is `deploy/sync.sh`:

```bash
deploy/sync.sh --check --ref <tag|sha>
deploy/sync.sh --apply --ref <tag|sha>
```

This copies repo truth into `/srv/xg2g` and `/etc/systemd/system`, reloads
systemd, and runs verification checks.

Canonical install layout: `docs/ops/INSTALLATION_CONTRACT.md`.
Use `deploy/sync.sh --check --ref <tag|sha>` for drift checks and
`deploy/sync.sh --apply --ref <tag|sha>` for the actual deployment.

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
| Lifecycle preflight, shared operator/startup gates | [Operational Lifecycle Contract](OPERATIONAL_LIFECYCLE_CONTRACT.md) |
| FFmpeg paths, GPU passthrough, runtime invariants | [Runtime Contract](DEPLOYMENT_RUNTIME_CONTRACT.md) |
| systemd start/stop/reload, Compose, smoke checks | [Operator Runbook](RUNBOOK_SYSTEMD_COMPOSE.md) |

## Deployment Artifacts

Repo-side deploy truth lives under `deploy/`.

Deployment artifacts:

- `deploy/sync.sh --check --ref <tag|sha>` — dry-run comparison against host
- `deploy/xg2g.env.schema.yaml` — contract for validating `/etc/xg2g/xg2g.env`
- `deploy/docker-compose.yml` — production Compose template
- `deploy/docker-compose.gpu.yml` — optional `/dev/dri` marker overlay (expanded into render-node-only binds by `compose-xg2g.sh`)
- `deploy/docker-compose.nvidia.yml` — optional NVIDIA runtime overlay
- `deploy/xg2g.service` — systemd unit

Direct host edits, ad-hoc file copies, and manual `/srv/xg2g` drift are not
supported deployment workflows.

## Local Runtime Workspace Sync

If a host keeps a separate runtime workspace beside the writable source checkout
(for example source in `/root/xg2g` and runtime in `/opt/xg2g`), keep the
workflow one-way:

```bash
make runtime-sync-check
make runtime-sync-apply
```

Rules:

- Keep exactly one writable git checkout.
- Treat the runtime workspace as deploy-only, even if it still contains `.git/`
  during migration.
- `deploy/runtime-sync.sh` compares or applies a pinned source ref (defaults to
  clean `HEAD`) into the runtime workspace.
- Overwritten or removed runtime files are backed up under
  `.runtime-bin/runtime-sync/backups/`.
- Use `deploy/runtime-sync.sh --check` as the drift signal for the runtime
  workspace. `git status` inside the runtime workspace is transitional noise,
  not the source of truth.
