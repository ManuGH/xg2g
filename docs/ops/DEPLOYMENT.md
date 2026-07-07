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
supported deployment workflows for tagged releases, and are never acceptable
on any host other than the maintainer's own.

## Fast Iteration Path (maintainer's own host only)

The tag-and-image path above is the only supported way to run xg2g anywhere
outside the maintainer's own infrastructure, and the only path that produces
an artifact anyone else can pull or audit. On the maintainer's own LXC/VM,
day-to-day iteration instead uses a sanctioned fast path that skips the
container image build (CI + FFmpeg image builds are too slow for tight
edit/verify loops):

1. Build on the host that has the real working copy (not a laptop clone):
   `make build-with-ui` produces `bin/xg2g`.
2. Push the binary into the running container's bind mount (e.g.
   `pct push <ctid> bin/xg2g /srv/xg2g/xg2g-dev-binary.new && mv` into place —
   never overwrite the in-use file directly, some container runtimes return
   success on a busy-file write while leaving the old binary running).
3. Restart the service (`systemctl restart xg2g`, or
   `docker compose up -d --force-recreate` for containers that only read env
   at recreate time, not at `docker restart`).
4. Verify the deployed commit before considering this done: compare
   `curl <host>/healthz` (`version` field, a `git describe` string) against
   `git log origin/<branch>..HEAD` on the host that built it.

**Non-negotiable rule:** every commit reachable from a binary running on this
path must already be pushed to `origin` before it is deployed. A clean
`git status` is not sufficient evidence of this — a fully committed branch
can still be several commits ahead of `origin/<branch>` and thus invisible to
anyone but the person who built it. Deploying unpushed commits means the
running system's actual code has no record anywhere reviewable.

This path is never used for the OCI image or GHCR tags — those are produced
exclusively by `.github/workflows/release.yml` from a pushed, tagged commit,
per the [Release Output Contract](RELEASE_OUTPUT_CONTRACT.md).
