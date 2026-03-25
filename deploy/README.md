# Deploy Bundle

`deploy/` is the staged repo-side source of truth for host deployment artifacts.

This is the step-1 migration slice:

- `deploy/xg2g.service` is the intended canonical systemd unit bundle file.
- `deploy/docker-compose.yml` is the intended canonical base compose file.
- `deploy/docker-compose.gpu.yml` is the intended canonical GPU overlay.
- `deploy/xg2g.env.schema.yaml` is the initial machine-readable contract for `/etc/xg2g/xg2g.env`.

Current migration boundary:

- Existing docs, renderers, and verifier scripts are still wired to legacy compatibility paths.
- Live-host deployment is not switched to `deploy/` yet.
- Until the next migration slice lands, changes in `deploy/` must keep these compatibility mirrors aligned:
  - `deploy/xg2g.service` -> `docs/ops/xg2g.service`
  - `deploy/docker-compose.yml` -> `docker-compose.yml`
  - `deploy/docker-compose.gpu.yml` -> `docker-compose.gpu.yml`

Why the env schema is intentionally narrow in step 1:

- `/etc/xg2g/xg2g.env` mixes deploy-time keys, systemd/compose control, and app overrides.
- The deploy contract keys are curated here first.
- App override coverage is intentionally limited to common host-side overrides until the schema can be generated from the config registry and runtime env readers.

Not migrated yet:

- `backend/scripts/render-docs.sh`
- `backend/scripts/verify-installation-contract.sh`
- `backend/scripts/verify-systemd-runtime-contract.sh`
- `mk/verify.mk`
- public ops docs that still point at `docs/ops/xg2g.service` and repo-root compose files
