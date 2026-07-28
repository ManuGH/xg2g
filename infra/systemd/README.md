# Deploy Bundle

`infra/systemd/` is the canonical repo-side source of truth for Linux host
deployment artifacts.

Current 2026 deploy bundle:

- `infra/systemd/xg2g.service` is the intended canonical systemd unit bundle file.
- `infra/systemd/docker-compose.yml` is the intended canonical base compose file.
- `infra/systemd/docker-compose.gpu.yml` is the intended canonical `/dev/dri` GPU overlay marker; `compose-xg2g.sh` expands it into render-node-only device entries at runtime.
- `infra/systemd/docker-compose.nvidia.yml` is the intended canonical NVIDIA runtime / NVENC overlay.
- `infra/systemd/xg2g.env.schema.yaml` is the initial machine-readable contract for `/etc/xg2g/xg2g.env`.
- `infra/systemd/sync.sh` is the idempotent host sync entrypoint.
- `infra/systemd/setup-linux.sh` is the guided release installer.
- `infra/systemd/xg2g-admin.sh` owns diagnosis, backup/restore,
  update/rollback, and safe removal.
- `infra/systemd/xg2g-backup.service` and `.timer` provide daily verified
  durable-state backups.
- `infra/systemd/xg2g-caddy.service` is the optional managed HTTPS edge. The
  unit is installed inertly and starts only when setup explicitly creates
  `/etc/xg2g/Caddyfile`; an existing reverse proxy is never replaced.
- `infra/systemd/REVERSE_PROXY.md` documents every reverse-proxy / HTTPS topology (direct, in-process TLS, Caddy, nginx, Traefik, Cloudflare Tunnel) and the settings each requires; `infra/systemd/reverse-proxy/` holds drop-in reference configs.

Deployment boundary:

- Docs renderers and verifier scripts consume `infra/systemd/` directly as repo truth.
- Live hosts are operationally validated via `/srv/xg2g` and `/etc/systemd/system`.
- `infra/systemd/sync.sh` applies the repo bundle onto those host targets.

Sync workflow:

- `infra/systemd/sync.sh --check --ref <tag|sha>` compares a pinned repo ref against the host install root.
- `infra/systemd/sync.sh --apply --ref <tag|sha>` copies the bundle to the host, reloads systemd, and reruns `--check`.
- `infra/systemd/sync.sh --apply --ref <tag|sha>` is the only supported deployment path. Manual file copies and direct host edits are drift by definition.
- Exit `0` means synced, `1` means drift, `2` means `/etc/xg2g/xg2g.env` violates the deploy contract.
- `--install-root <path>` is available for local dry-runs and fixture-style tests.

Why the env schema is intentionally curated:

- `/etc/xg2g/xg2g.env` mixes deploy-time keys, systemd/compose control, and app overrides.
- The deploy contract keys are curated here and validated before start.
- App override coverage is intentionally limited to common host-side
  overrides; the application config registry remains the broader runtime
  source.

Historical incident/runbook sections may describe older installed layouts.
They are evidence, not current deployment instructions. For current truth use
this bundle, [Deployment](../../docs/ops/DEPLOYMENT.md), and the
[2026 system overview](../../docs/arch/SYSTEM_OVERVIEW_2026.md).
