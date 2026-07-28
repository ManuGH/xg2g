# Deployment Guide

xg2g ships as a single OCI image with a bundled FFmpeg runtime. The only supported deployment path is `infra/systemd/sync.sh`:

```bash
infra/systemd/sync.sh --check --ref <tag|sha>
infra/systemd/sync.sh --apply --ref <tag|sha>
```

For a new Linux host, use the guided front end
`infra/systemd/setup-linux.sh`:

```bash
sudo ./infra/systemd/setup-linux.sh
```

Start with the [complete Linux installation guide](../guides/INSTALLATION.md)
when preparing a new host. It includes exact release download and checksum
commands, Docker/systemd prerequisites, receiver and storage preflight, every
wizard decision, first login, verification, update, restore, and uninstall.

It asks for the receiver, private/public access topology, DVR window and
concurrency, an optional already-mounted scratch disk, and GPU type. It
generates the required secrets and delegates the installation to the pinned
`sync.sh` path above. It never partitions, formats, mounts, or deletes a disk.
Existing installations remain on `sync.sh`; the wizard will not overwrite
their environment file. The hardened base Compose file continues to publish
xg2g on loopback only.

## HTTPS Ownership

Caddy is not an automatic dependency. The setup prompt makes ownership
explicit:

| Mode | Proxy ownership | Installer behavior |
| :--- | :--- | :--- |
| Existing same-host proxy (default) | Operator | Does not edit or reload the proxy. It creates no Caddyfile, pulls no Caddy image, and leaves `xg2g-caddy.service` disabled/inactive. It verifies the external HTTPS endpoint and xg2g's forwarded-header contract. |
| Managed public Caddy (opt-in) | xg2g setup | Creates `/etc/xg2g/Caddyfile`, pulls the pinned Caddy image, and enables/starts `xg2g-caddy.service` for public ACME HTTPS. |
| Managed internal Caddy (opt-in) | xg2g setup | Creates an internal-CA Caddyfile, optionally binds an exact LAN/VPN IP, enables/starts the service, and exposes the CA certificate for client trust. |
| Local-only | None | Leaves xg2g on loopback HTTP for same-host use only. |

The Caddy unit is installed as an inert deploy artifact in every standard
installation. `ConditionPathExists=/etc/xg2g/Caddyfile` prevents it from
starting unless managed Caddy was explicitly selected.

The guided existing-proxy mode assumes the proxy runs on the xg2g host because
the backend remains bound to `127.0.0.1:8088`. A separate proxy host requires a
deliberate backend bind plus firewall/VPN policy and is outside the
beginner-safe wizard.

Official release archives contain the complete deploy bundle and install
without cloning Git. GitHub-generated branch source ZIPs are rejected because
they cannot prove a released image/ref pairing.

This copies repo truth into `/srv/xg2g` and `/etc/systemd/system`, reloads
systemd, and runs verification checks.

Canonical install layout: `docs/ops/INSTALLATION_CONTRACT.md`.
Current architecture/runtime snapshot:
`docs/arch/SYSTEM_OVERVIEW_2026.md`.
Use `infra/systemd/sync.sh --check --ref <tag|sha>` for drift checks and
`infra/systemd/sync.sh --apply --ref <tag|sha>` for the actual deployment.

## Minimum Requirements

| Requirement | Value |
| :--- | :--- |
| **Operating system** | Linux with systemd (`amd64` or `arm64`) |
| **Runtime** | Docker Engine with the Compose v2 plugin |
| **Supervisor** | systemd (manages container lifecycle) |
| **Network** | Enigma2 receiver reachable from host |
| **HTTPS** | Required for non-loopback browser access |

The guided path is distribution-neutral above the package layer and prints
prerequisite commands for Debian/Ubuntu, Fedora/RHEL, and Arch families.
Appliance/NAS operating systems without systemd are not covered by this host
installer; they can consume the OCI image through their native container
manager, but must provide equivalent persistence, secrets, HTTPS, health, and
backup supervision themselves.

## Detailed Documentation

| Topic | Document |
| :--- | :--- |
| Host layout, shipped artifacts, unit locations | [Installation Contract](INSTALLATION_CONTRACT.md) |
| Lifecycle preflight, shared operator/startup gates | [Operational Lifecycle Contract](OPERATIONAL_LIFECYCLE_CONTRACT.md) |
| FFmpeg paths, GPU passthrough, runtime invariants | [Runtime Contract](DEPLOYMENT_RUNTIME_CONTRACT.md) |
| systemd start/stop/reload, Compose, smoke checks | [Operator Runbook](RUNBOOK_SYSTEMD_COMPOSE.md) |

## Deployment Artifacts

Repo-side deploy truth lives under `infra/systemd/`.

Deployment artifacts:

- `infra/systemd/sync.sh --check --ref <tag|sha>` — dry-run comparison against host
- `infra/systemd/xg2g.env.schema.yaml` — contract for validating `/etc/xg2g/xg2g.env`
- `infra/systemd/docker-compose.yml` — production Compose template
- `infra/systemd/docker-compose.gpu.yml` — optional `/dev/dri` marker overlay (expanded into render-node-only binds by `compose-xg2g.sh`)
- `infra/systemd/docker-compose.nvidia.yml` — optional NVIDIA runtime overlay
- `infra/systemd/xg2g.service` — systemd unit
- `infra/systemd/xg2g-admin.sh` — doctor, backup/restore, update/rollback, and
  uninstall lifecycle
- `infra/systemd/xg2g-backup.service` / `.timer` — online daily backup
- `infra/systemd/xg2g-caddy.service` — optional managed HTTPS edge

Routine host commands:

```bash
sudo xg2g-admin doctor
sudo xg2g-admin backup
sudo xg2g-admin restore /var/backups/xg2g/ARCHIVE.tar.gz --yes
sudo xg2g-admin update --ref vX.Y.Z
sudo xg2g-admin rollback --yes
sudo xg2g-admin uninstall
```

Uninstall preserves configuration, data, local backups, recordings, and
external DVR storage unless the operator explicitly adds `--purge-data --yes`.

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
   `pct push <ctid> bin/xg2g /srv/xg2g/xg2g-dev-binary.new && pct exec <ctid> -- mv /srv/xg2g/xg2g-dev-binary.new /srv/xg2g/xg2g-dev-binary` into place —
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
