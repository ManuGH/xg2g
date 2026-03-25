# Deployment (Index)

This document is now split into two canonical sources:

- Installation contract (host layout / shipped artifacts): `docs/ops/INSTALLATION_CONTRACT.md`
- Runtime contract (GPU/FFmpeg/paths): `docs/ops/DEPLOYMENT_RUNTIME_CONTRACT.md`
- Operator runbook (systemd + Compose): `docs/ops/RUNBOOK_SYSTEMD_COMPOSE.md`

If you are looking for systemd, start/stop/reload, or smoke checks, use the runbook.
If you are looking for required host paths, shipped scripts, unit locations, or install-time artifacts, use the installation contract.
If you are looking for FFmpeg, GPU, or runtime invariants, use the runtime contract.

Repo-side deploy truth lives under `deploy/`.

- `deploy/sync.sh --check --ref <tag|sha>` compares a pinned repo ref against the host install root.
- `deploy/sync.sh --apply --ref <tag|sha>` copies repo truth into `/srv/xg2g` and `/etc/systemd/system`, reloads systemd, and reruns the same checks.
- `deploy/xg2g.env.schema.yaml` is the contract for validating `/etc/xg2g/xg2g.env` during sync.

The installation contract and runbook remain the live-host reference for target paths and runtime debugging.
