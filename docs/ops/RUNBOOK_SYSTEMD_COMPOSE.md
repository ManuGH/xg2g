# Systemd + Compose Runbook (v3.1.5+)

Canonical guide for managing the hardened `xg2g` daemon via systemd.

### Installation
Deploy the hardened unit file and enable the service:
```bash
cp docs/ops/xg2g.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now xg2g
```

**Path invariants (fail-closed):**
- Compose file path is frozen to `/srv/xg2g/docker-compose.yml`.
- Working directory must be `/srv/xg2g`.
- Data directory must exist and be writable at `/var/lib/xg2g`.
- Compose service name must remain `xg2g` (health gate relies on it).

Production compose is deterministic. For local development, apply the override:
`docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d`.

Drift guard: `backend/scripts/verify-systemd-unit.sh` must pass before release.
Canonical unit is `docs/ops/xg2g.service`; no repo-root `xg2g.service` is permitted.

Host verification (deploy-time, fail-closed):
```bash
/srv/xg2g/scripts/verify-installed-unit.sh /srv/xg2g/docs/ops/xg2g.service
/srv/xg2g/scripts/verify-compose-contract.sh
/srv/xg2g/scripts/run-service-smoke.sh
```

### Lifecycle Management
The service uses `docker compose` as the underlying engine but is supervised by systemd.

| Action | Command | Effect |
|--------|---------|--------|
| **Start** | `systemctl start xg2g` | Upstreams configuration checks; starts containers. |
| **Stop** | `systemctl stop xg2g` | gracefully stops containers (`docker compose stop`); preserves volumes/networks. |
| **Reload** | `systemctl reload xg2g` | Re-applies configuration changes (`docker compose up -d`). |
| **Status** | `systemctl status xg2g` | Shows service health and recent logs. |

### Operational Truth Model (Explicit)
Systemd orchestrates Compose and enforces preflight + startup health gating. Runtime truth lives in Docker.
After startup, systemd can remain **active (exited)** even if containers later crash or go unhealthy.

**Runtime truth commands (authoritative):**
```bash
cd /srv/xg2g
docker compose --project-name xg2g ps
cid="$(docker compose --project-name xg2g ps -q xg2g)"
docker inspect --format '{{.State.Health.Status}}' "$cid"
docker compose --project-name xg2g logs -f --tail=200
```

**Startup health gate:**
`systemctl start xg2g` blocks until the container healthcheck is **healthy** or fails within `TimeoutStartSec`.
Gate timing is derived from the Compose healthcheck: `start_period=60s` + `retries=5` × `interval=10s`
plus headroom, yielding a 150s health wait and `TimeoutStartSec=180`.

**Reload semantics (explicit):**
`systemctl reload xg2g` is best-effort and does **not** gate on health. For a health-gated cycle,
use `systemctl restart xg2g` and verify container health via Docker.

### Configuration Updates
1. Edit the environment file:
   ```bash
   nano /etc/xg2g/xg2g.env
   ```
2. Apply changes without downtime (if possible):
   ```bash
   systemctl reload xg2g
   ```

### Security Notes (Minimum)
- `/etc/xg2g/xg2g.env` must be `root:root` and `0600` (contains secrets).
- Credentials embedded in URLs are a known risk; prefer separate user/pass variables or secret files when available.
```bash
chown root:root /etc/xg2g/xg2g.env
chmod 600 /etc/xg2g/xg2g.env
```
Host-side hygiene in the systemd unit (UMask/NoNewPrivileges) applies to the compose client only; it
does not harden the container runtime.
Container hardening (cap drops, read-only, tmpfs, user) is owned by `docker-compose.yml`.

### Log Access & Troubleshooting
Access logs via Docker (source of truth) or systemd (capture).

**Live Tail (Recommended)**:
```bash
cd /srv/xg2g
docker compose logs -f --tail=200
```

**System Journal**:
```bash
journalctl -u xg2g -f
```

**Common Issues**:
* **Start Fails immediately**: Check `systemctl status xg2g`. Ensure all required env vars are set in `/etc/xg2g/xg2g.env`:
  - `XG2G_E2_HOST` (legacy fallback: `XG2G_OWI_BASE`) — receiver base URL; both vars must match if set.
  - `XG2G_API_TOKEN` — control plane bearer token.
  - `XG2G_DECISION_SECRET` — live stream JWT signing key, min 32 bytes. See `docs/ops/SECURITY.md` for generation and rotation procedure.
* **Crash Loop**: If running manually via `docker compose` without the systemd safety checks, the container may restart indefinitely on missing config. Use `systemctl start` for fail-closed protection.

### AI / Incident Triage Order
When a future AI or operator debugs a failed `systemctl start xg2g` or `systemctl restart xg2g`, use this order and do not assume repo docs match the live host:

1. Capture the exact failing phase:
   ```bash
   systemctl status xg2g.service --no-pager -l
   journalctl -xeu xg2g.service --no-pager -n 120
   ```
2. Compare the three live sources of truth:
   - `/etc/systemd/system/xg2g.service`
   - `/srv/xg2g/docker-compose.yml`
   - `/etc/xg2g/xg2g.env`
3. Only after that, compare against the checked-in template `docs/ops/xg2g.service`.

Use these symptom mappings:

- `ExecStartPre` shows `No such image`: the installed systemd unit is probably checking a stale hardcoded image tag. Treat `services.xg2g.image` in `/srv/xg2g/docker-compose.yml` as image truth.
- Container logs show `XG2G_DECISION_SECRET is required but not set`: the live env file is missing a mandatory JWT signing secret. Fix `/etc/xg2g/xg2g.env` first; the service should fail before container startup once the live unit is in sync with the checked-in template.
- `ExecStartPost` fails with `Container is unhealthy`: inspect Docker health details directly:
  ```bash
  docker inspect -f '{{json .State.Health}}' xg2g
  docker logs --since 5m xg2g
  ```

Important nuance: `/readyz` may already be healthy while Docker health is still red. One confirmed failure mode is a metrics-only health mismatch where readiness is `200` but the Docker healthcheck still fails because `http://localhost:9091/metrics` is unreachable.

Observed live-host delta on March 11, 2026:
- `/srv/xg2g/docker-compose.yml` required `xg2g healthcheck --mode ready --require-metrics --metrics-port 9091`
- `/etc/xg2g/xg2g.env` did not define `XG2G_METRICS_LISTEN`
- Result: `xg2g healthcheck --mode ready` succeeded, but `xg2g healthcheck --mode ready --require-metrics --metrics-port 9091` failed with `connect: connection refused`

If you see that exact mismatch, fix the live env first by setting:

```bash
XG2G_METRICS_LISTEN=:9091
```

Then recreate the container and re-check:

```bash
docker inspect -f '{{json .State.Health}}' xg2g
systemctl status xg2g.service --no-pager -l
```

### Service Smoke Matrix
See `docs/ops/SERVICE_SMOKE.md` for the CTO-grade start/stop matrix (negative + positive + idempotent).
