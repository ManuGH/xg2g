# Deploy Bundle

`deploy/` is the staged repo-side source of truth for host deployment artifacts.

This is the deploy-SSoT migration slice:

- `deploy/xg2g.service` is the intended canonical systemd unit bundle file.
- `deploy/docker-compose.yml` is the intended canonical base compose file.
- `deploy/docker-compose.gpu.yml` is the intended canonical GPU overlay.
- `deploy/xg2g.env.schema.yaml` is the initial machine-readable contract for `/etc/xg2g/xg2g.env`.
- `deploy/sync.sh` is the idempotent host sync entrypoint.

Current migration boundary:

- Docs renderers and verifier scripts now consume `deploy/` as repo truth and keep legacy mirrors in sync.
- Live hosts are still operationally validated via `/srv/xg2g` and `/etc/systemd/system`.
- Until the host pull path is fully adopted, changes in `deploy/` must keep these compatibility mirrors aligned:
  - `deploy/xg2g.service` -> `docs/ops/xg2g.service`
  - `deploy/docker-compose.yml` -> `docker-compose.yml`
  - `deploy/docker-compose.gpu.yml` -> `docker-compose.gpu.yml`

Sync workflow:

- `deploy/sync.sh --check --ref <tag|sha>` compares a pinned repo ref against the host install root.
- `deploy/sync.sh --apply --ref <tag|sha>` copies the bundle to the host, reloads systemd, and reruns `--check`.
- `deploy/sync.sh --apply --ref <tag|sha>` is the only supported deployment path. Manual file copies and direct host edits are drift by definition.
- Exit `0` means synced, `1` means drift, `2` means `/etc/xg2g/xg2g.env` violates the deploy contract.
- `--install-root <path>` is available for local dry-runs and fixture-style tests.

Why the env schema is intentionally narrow in step 1:

- `/etc/xg2g/xg2g.env` mixes deploy-time keys, systemd/compose control, and app overrides.
- The deploy contract keys are curated here first.
- App override coverage is intentionally limited to common host-side overrides until the schema can be generated from the config registry and runtime env readers.

Remaining tail after this slice:

- live hosts still need to adopt `deploy/sync.sh` as the normal deployment entrypoint everywhere
- some public ops docs still describe legacy compatibility mirrors for historical drift analysis
