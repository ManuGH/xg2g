# Installation Contract

Canonical install-time contract for `xg2g` on a host. This defines which artifacts
must exist after installation, which are optional, which are operator-provided,
and which only need to exist at runtime.

## Required Host Layout

These paths are required for a start-ready installation.

| Target Path | Class | Required | Expected Mode | Source / Truth | Notes |
| --- | --- | --- | --- | --- | --- |
| `/srv/xg2g/docker-compose.yml` | Repo-deployed artifact | Yes | `0644` | Repo deploy bundle `infra/systemd/docker-compose.yml` | Base compose source of truth |
| `/srv/xg2g/INSTALL_REF` | Generated provenance | Yes | `0644` | `infra/systemd/sync.sh` | Exact tag/SHA label applied to the host |
| `/srv/xg2g/infra/systemd/setup-linux.sh` | Repo-deployed lifecycle helper | Yes | `0755` | Repo `infra/systemd/setup-linux.sh` | Repair/update entrypoint |
| `/srv/xg2g/infra/systemd/sync.sh` | Repo-deployed lifecycle helper | Yes | `0755` | Repo `infra/systemd/sync.sh` | Canonical sync implementation |
| `/srv/xg2g/scripts/xg2g-admin.sh` | Repo-deployed lifecycle helper | Yes | `0755` | Repo `infra/systemd/xg2g-admin.sh` | Doctor, backup/restore, update/rollback, uninstall |
| `/usr/local/sbin/xg2g-admin` | Operator command | Yes | `0755` | Repo `infra/systemd/xg2g-admin.sh` | Stable command on root's PATH |
| `/usr/local/share/man/man1/xg2g.1` | Operator documentation | Yes | `0644` | Repo `docs/man/xg2g.1` | Manual page available through `man xg2g` |
| `/srv/xg2g/docs/ops/xg2g.service` | Repo-deployed artifact | Yes | `0644` | Repo deploy bundle `infra/systemd/xg2g.service` | Canonical unit copy kept on host |
| `/srv/xg2g/scripts/compose-xg2g.sh` | Repo-deployed runtime helper | Yes | `0755` | Repo `backend/scripts/compose-xg2g.sh` | Compose resolver SSOT |
| `/srv/xg2g/scripts/verify-compose-contract.sh` | Repo-deployed runtime helper | Yes | `0755` | Repo `backend/scripts/verify-compose-contract.sh` | Compose contract gate |
| `/srv/xg2g/scripts/verify-installed-unit.sh` | Repo-deployed operator verifier | Yes | `0755` | Repo `backend/scripts/verify-installed-unit.sh` | Host unit drift audit |
| `/srv/xg2g/scripts/verify-systemd-runtime-contract.sh` | Repo-deployed operator verifier | Yes | `0755` | Repo `backend/scripts/verify-systemd-runtime-contract.sh` | Runtime env contract audit |
| `/srv/xg2g/scripts/verify-installation-contract.sh` | Repo-deployed operator verifier | Yes | `0755` | Repo `backend/scripts/verify-installation-contract.sh` | Installation contract audit |
| `/etc/systemd/system/xg2g.service` | Installed unit | Yes | `0644` | Copied from `/srv/xg2g/docs/ops/xg2g.service` | Installed unit must byte-match canonical host copy |
| `/etc/systemd/system/xg2g-backup.service` | Installed unit | Yes | `0644` | Repo `infra/systemd/xg2g-backup.service` | Online durable-state backup |
| `/etc/systemd/system/xg2g-backup.timer` | Installed unit | Yes | `0644` | Repo `infra/systemd/xg2g-backup.timer` | Daily backup schedule |
| `/etc/systemd/system/xg2g-caddy.service` | Installed inactive unit | Yes | `0644` | Repo `infra/systemd/xg2g-caddy.service` | Starts only when managed Caddy was selected |
| `/etc/xg2g/xg2g.env` | Operator-provided input | Yes before start | `0600` | Operator-managed | Required secrets and env surface; must be `root:root` |
| `/var/lib/xg2g` | Host runtime state | Yes before start | Writable directory | Operator / package | Data root must exist before service start |

The guided setup creates the persistent data and DVR scratch directories as
UID/GID `10001:10001`, matching the non-root user in the release image. It does
not change ownership of the recordings mount.

## Optional Host Artifacts

These paths are optional and host-specific. Absence is valid unless otherwise noted.

| Target Path | Class | Required | Expected Mode | Source / Truth | Notes |
| --- | --- | --- | --- | --- | --- |
| `/srv/xg2g/docker-compose.gpu.yml` | Repo-deployed optional overlay | GPU hosts only | `0644` | Repo deploy bundle `infra/systemd/docker-compose.gpu.yml` | Install only when the host should auto-load that marker overlay for render-node-only `/dev/dri` bindings |
| `/srv/xg2g/docker-compose.nvidia.yml` | Repo-deployed optional overlay | NVIDIA GPU hosts only | `0644` | Repo deploy bundle `infra/systemd/docker-compose.nvidia.yml` | Install only when the host should auto-load the NVIDIA runtime overlay |
| `/etc/xg2g/config.yaml` | Operator-provided input | Optional | Operator-defined | Operator-managed | Optional explicit config file |
| `/etc/xg2g/Caddyfile` | Setup-generated input | Managed HTTPS only | `0640` | `infra/systemd/setup-linux.sh` | Caddy public ACME or internal-CA edge |
| `/etc/xg2g/https-ca.crt` | Setup-copied trust input | Existing private CA only | `0644` | Operator-selected CA | Used by post-install and doctor HTTPS verification |
| `/var/lib/xg2g-caddy` | Managed runtime state | Managed HTTPS only | Private directory | Caddy | Certificates and Caddy runtime configuration |

Repo-side canonical deploy truth now lives under `infra/systemd/`. Files under `/srv/xg2g/`
remain installation targets and must not be treated as an editable source of truth.
Supported installs must be applied via `infra/systemd/sync.sh --apply --ref <tag|sha>`, not via manual file copies.
For a first installation, `infra/systemd/setup-linux.sh` gathers and validates
the operator inputs, creates the env/storage inputs, and then calls that same
pinned sync path. It is a front end to the contract, not a second deployment
mechanism.

Official release archives ship every source consumed by `sync.sh` and use
`--source-dir` so installation does not depend on Git or a second download.
GitHub branch source ZIPs are rejected because they are mutable and cannot
prove an OCI-image/ref pairing.

## Optional Periodic Verifier Bundle

The periodic verifier is optional, but all-or-nothing. If any path in this bundle is installed,
all of them must be present in the same installation.

| Target Path | Class | Required | Expected Mode | Source / Truth | Notes |
| --- | --- | --- | --- | --- | --- |
| `/srv/xg2g/docs/ops/xg2g-verifier.service` | Repo-deployed artifact | Optional bundle | `0644` | Repo `docs/ops/xg2g-verifier.service` | Canonical verifier unit copy |
| `/srv/xg2g/docs/ops/xg2g-verifier.timer` | Repo-deployed artifact | Optional bundle | `0644` | Repo `docs/ops/xg2g-verifier.timer` | Canonical verifier timer copy |
| `/srv/xg2g/scripts/verify-runtime.sh` | Repo-deployed operator verifier | Optional bundle | `0755` | Repo `backend/scripts/verify-runtime.sh` | Runtime truth audit |
| `/srv/xg2g/VERSION` | Repo-deployed metadata | Optional bundle | `0644` | Repo `backend/VERSION` | Verifier input |
| `/srv/xg2g/DIGESTS.lock` | Repo-deployed metadata | Optional bundle | `0644` | Repo `DIGESTS.lock` | Verifier input |
| `/etc/systemd/system/xg2g-verifier.service` | Installed unit | Optional bundle | `0644` | Copied from `/srv/xg2g/docs/ops/xg2g-verifier.service` | Installed verifier unit must match canonical host copy |
| `/etc/systemd/system/xg2g-verifier.timer` | Installed unit | Optional bundle | `0644` | Copied from `/srv/xg2g/docs/ops/xg2g-verifier.timer` | Installed verifier timer must match canonical host copy |

## Rules

1. Core daemon installation must not depend on optional hardware overlays. The
   guided setup installs and enables the verifier bundle; direct `sync.sh`
   operators may still select the governed all-or-nothing optional mode.
2. `/etc/systemd/system/xg2g.service` must byte-match `/srv/xg2g/docs/ops/xg2g.service`.
3. If the periodic verifier bundle is installed, all bundle members above must be present.
4. `/etc/xg2g/xg2g.env` is mandatory before `systemctl start xg2g` and must remain `root:root` with mode `0600`.
5. The installation verifier for this contract is `backend/scripts/verify-installation-contract.sh`.
6. Live-host validation uses `/srv/xg2g/scripts/verify-installation-contract.sh --verify-install-root /`.
7. Default uninstall preserves `/etc/xg2g`, `/var/lib/xg2g`,
   `/var/backups/xg2g`, receiver recordings, and external DVR scratch.
8. Selecting an existing reverse proxy must not create
   `/etc/xg2g/Caddyfile`, pull a Caddy image, or enable/start
   `xg2g-caddy.service`. The installed unit remains an inert deploy artifact.
