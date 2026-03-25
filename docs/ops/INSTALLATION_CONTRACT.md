# Installation Contract

Canonical install-time contract for `xg2g` on a host. This defines which artifacts
must exist after installation, which are optional, which are operator-provided,
and which only need to exist at runtime.

## Required Host Layout

These paths are required for a start-ready installation.

| Target Path | Class | Required | Expected Mode | Source / Truth | Notes |
| --- | --- | --- | --- | --- | --- |
| `/srv/xg2g/docker-compose.yml` | Repo-deployed artifact | Yes | `0644` | Repo deploy bundle `deploy/docker-compose.yml` | Base compose source of truth |
| `/srv/xg2g/docs/ops/xg2g.service` | Repo-deployed artifact | Yes | `0644` | Repo deploy bundle `deploy/xg2g.service` | Canonical unit copy kept on host |
| `/srv/xg2g/scripts/compose-xg2g.sh` | Repo-deployed runtime helper | Yes | `0755` | Repo `backend/scripts/compose-xg2g.sh` | Compose resolver SSOT |
| `/srv/xg2g/scripts/verify-compose-contract.sh` | Repo-deployed runtime helper | Yes | `0755` | Repo `backend/scripts/verify-compose-contract.sh` | Compose contract gate |
| `/srv/xg2g/scripts/verify-installed-unit.sh` | Repo-deployed operator verifier | Yes | `0755` | Repo `backend/scripts/verify-installed-unit.sh` | Host unit drift audit |
| `/srv/xg2g/scripts/verify-systemd-runtime-contract.sh` | Repo-deployed operator verifier | Yes | `0755` | Repo `backend/scripts/verify-systemd-runtime-contract.sh` | Runtime env contract audit |
| `/srv/xg2g/scripts/verify-installation-contract.sh` | Repo-deployed operator verifier | Yes | `0755` | Repo `backend/scripts/verify-installation-contract.sh` | Installation contract audit |
| `/etc/systemd/system/xg2g.service` | Installed unit | Yes | `0644` | Copied from `/srv/xg2g/docs/ops/xg2g.service` | Installed unit must byte-match canonical host copy |
| `/etc/xg2g/xg2g.env` | Operator-provided input | Yes before start | `0600` | Operator-managed | Required secrets and env surface |
| `/var/lib/xg2g` | Host runtime state | Yes before start | Writable directory | Operator / package | Data root must exist before service start |

## Optional Host Artifacts

These paths are optional and host-specific. Absence is valid unless otherwise noted.

| Target Path | Class | Required | Expected Mode | Source / Truth | Notes |
| --- | --- | --- | --- | --- | --- |
| `/srv/xg2g/docker-compose.gpu.yml` | Repo-deployed optional overlay | GPU hosts only | `0644` | Repo deploy bundle `deploy/docker-compose.gpu.yml` | Install only when the host should auto-load that GPU overlay |
| `/etc/xg2g/config.yaml` | Operator-provided input | Optional | Operator-defined | Operator-managed | Optional explicit config file |

Repo-side canonical deploy truth now lives under `deploy/`. Files under `/srv/xg2g/`
remain installation targets and must not be treated as an editable source of truth.
Supported installs must be applied via `deploy/sync.sh --apply --ref <tag|sha>`, not via manual file copies.

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

1. Core daemon installation must not depend on the optional GPU overlay or the optional periodic verifier bundle.
2. `/etc/systemd/system/xg2g.service` must byte-match `/srv/xg2g/docs/ops/xg2g.service`.
3. If the periodic verifier bundle is installed, all bundle members above must be present.
4. `/etc/xg2g/xg2g.env` is mandatory before `systemctl start xg2g` and must remain mode `0600`.
5. The installation verifier for this contract is `backend/scripts/verify-installation-contract.sh`.
6. Live-host validation uses `/srv/xg2g/scripts/verify-installation-contract.sh --verify-install-root /`.
