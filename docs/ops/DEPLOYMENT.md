# Deployment (Index)

This document is now split into two canonical sources:

- Installation contract (host layout / shipped artifacts): `docs/ops/INSTALLATION_CONTRACT.md`
- Runtime contract (GPU/FFmpeg/paths): `docs/ops/DEPLOYMENT_RUNTIME_CONTRACT.md`
- Operator runbook (systemd + Compose): `docs/ops/RUNBOOK_SYSTEMD_COMPOSE.md`

If you are looking for systemd, start/stop/reload, or smoke checks, use the runbook.
If you are looking for required host paths, shipped scripts, unit locations, or install-time artifacts, use the installation contract.
If you are looking for FFmpeg, GPU, or runtime invariants, use the runtime contract.

Migration note: the repo-side deploy bundle is being staged under `deploy/`.
Until the verifier and rendering paths are rewired, the existing installation contract
and runbook remain the operational source of truth for live hosts.
