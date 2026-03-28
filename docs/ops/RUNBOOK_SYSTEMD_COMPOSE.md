# Systemd + Compose Runbook (v3.1.5+)

Canonical guide for managing the hardened `xg2g` daemon via systemd.

### Installation
Deploy the hardened unit file and enable the service:
```bash
cp docs/ops/xg2g.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now xg2g
```

Canonical install layout: `docs/ops/INSTALLATION_CONTRACT.md`.

**Path invariants (fail-closed):**
- Base compose file path is frozen to `/srv/xg2g/docker-compose.yml`.
- Optional GPU overlay path is `/srv/xg2g/docker-compose.gpu.yml`.
- Working directory must be `/srv/xg2g`.
- Data directory must exist and be writable at `/var/lib/xg2g`.
- Compose service name must remain `xg2g` (health gate relies on it).

Production compose is deterministic. For local development, apply the override:
`docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d`.
Add `-f docker-compose.gpu.yml` on GPU hosts.

Drift guard: `backend/scripts/verify-systemd-unit.sh` and `backend/scripts/verify-systemd-runtime-contract.sh` must pass before release.
Canonical unit is `docs/ops/xg2g.service`; no repo-root `xg2g.service` is permitted.

Host verification (deploy-time, fail-closed):
```bash
/srv/xg2g/scripts/verify-installed-unit.sh /srv/xg2g/docs/ops/xg2g.service
/srv/xg2g/scripts/verify-installation-contract.sh --verify-install-root /
/srv/xg2g/scripts/verify-systemd-runtime-contract.sh
/srv/xg2g/scripts/verify-compose-contract.sh
/srv/xg2g/scripts/run-service-smoke.sh
/srv/xg2g/scripts/verify-post-deploy-playback.sh
```

Post-deploy playback verification (operator-grade):
- `verify-post-deploy-playback.sh` starts real live sessions against the local host and proves three runtime paths:
  - tokenized HLS manifest works without cookie/header auth
  - live DVR manifest stays sliding-window (`EXT-X-START`, no `PLAYLIST-TYPE`)
  - forced transcode reaches `h264_vaapi` on `/dev/dri/renderD128`
- The script stops its probe sessions on exit and is intended for deploy-time verification, not for periodic timer-driven runtime truth checks.

### Lifecycle Management
The service uses `docker compose` as the underlying engine but is supervised by systemd.
Use `/srv/xg2g/scripts/compose-xg2g.sh` for manual compose operations so you inspect the same file stack that systemd resolves.

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
/srv/xg2g/scripts/compose-xg2g.sh ps
cid="$(/srv/xg2g/scripts/compose-xg2g.sh ps -q xg2g)"
docker inspect --format '{{.State.Health.Status}}' "$cid"
/srv/xg2g/scripts/compose-xg2g.sh logs -f --tail=200
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
Legacy receiver env aliases such as `XG2G_OWI_*`, `XG2G_STREAM_PORT`, and `XG2G_USE_WEBIF_STREAMS` now fail startup; keep `/etc/xg2g/xg2g.env` on the canonical `XG2G_E2_*` surface.

### Security Notes (Minimum)
- `/etc/xg2g/xg2g.env` must be `root:root` and `0600` (contains secrets).
- `/srv/xg2g/scripts/compose-xg2g.sh config` redacts secret env values by default before printing rendered Compose. Use `XG2G_COMPOSE_CONFIG_REDACT=0` only when you explicitly need raw output on a controlled terminal.
- Credentials embedded in URLs are a known risk; prefer separate user/pass variables or secret files when available.
- Browser-facing deployments must use HTTPS directly or a trusted HTTPS proxy.
  Plain HTTP is acceptable only for same-host loopback access. For non-loopback
  browsers, `/api/v3/auth/session` rejects plain HTTP, the `xg2g_session`
  cookie is not issued, and native HLS playback can fail with `HTTPS required`,
  media `401`, or generic `Video Error: 4` surfaces.
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
/srv/xg2g/scripts/compose-xg2g.sh logs -f --tail=200
```

**System Journal**:
```bash
journalctl -u xg2g -f
```

**Common Issues**:
* **Start Fails immediately**: Check `systemctl status xg2g`. Ensure all required env vars are set in `/etc/xg2g/xg2g.env`:
  - `XG2G_E2_HOST` — receiver base URL.
  - `XG2G_API_TOKEN` — control plane bearer token.
  - `XG2G_DECISION_SECRET` — live stream JWT signing key, min 32 bytes. See `docs/ops/SECURITY.md` for generation and rotation procedure.
  - If the journal mentions `legacy environment variable(s) detected`, remove the listed legacy receiver keys and apply the exact `XG2G_E2_*` replacement lines shown in the error.
* **Crash Loop**: If running manually via `docker compose` without the systemd safety checks, the container may restart indefinitely on missing config. Use `systemctl start` for fail-closed protection.
* **Remote browser playback fails with `HTTPS required`, media `401`, or `Video Error: 4`**: the browser is likely reaching xg2g over plain HTTP. Put `/ui/` and `/api/v3/` behind HTTPS or a trusted HTTPS proxy, then verify the browser can complete `POST /api/v3/auth/session` and receive the `xg2g_session` cookie.

### AI / Incident Triage Order
When a future AI or operator debugs a failed `systemctl start xg2g` or `systemctl restart xg2g`, use this order and do not assume repo docs match the live host:

1. Capture the exact failing phase:
   ```bash
   systemctl status xg2g.service --no-pager -l
   journalctl -xeu xg2g.service --no-pager -n 120
   ```
2. Compare the live sources of truth:
   - `/etc/systemd/system/xg2g.service`
   - `/srv/xg2g/docker-compose.yml`
   - `/srv/xg2g/docker-compose.gpu.yml` (if present)
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

Observed live-host delta on March 17, 2026:
- `/srv/xg2g/docker-compose.yml` used `image: xg2g:local`
- `/srv/xg2g/docker-compose.yml` healthcheck used `interval=10s`, `timeout=5s`, `retries=3`, `start_period=15s`
- `/etc/systemd/system/xg2g.service` still enforced health primarily through its own `ExecStartPost` polling loop (`150` iterations with `sleep 1`)
- Result: the old runbook wording that derived the startup gate from `start_period=60s` and `retries=5` was stale for this host; the practical gate remained the systemd-side 150s wait budget with `TimeoutStartSec=180`

Observed live-host delta on March 17, 2026 (image truth):
- `/etc/systemd/system/xg2g.service` derives the image to preflight from `/srv/xg2g/docker-compose.yml`; it is not a separate image source of truth.
- When `/srv/xg2g/docker-compose.yml` points at `ghcr.io/manugh/xg2g:v3.4.1`, a locally built `xg2g:local` image is insufficient for `systemctl restart xg2g.service`.
- Symptom: `ExecStartPre` fails with `No such image: ghcr.io/manugh/xg2g:v3.4.1` before Compose can start anything.
- Operational rule: for hotfix deploys, either build/tag the image under the compose-pinned reference or update the live compose file first. Do not patch the installed unit under the assumption that it owns image truth.

Observed live-host delta on March 17, 2026 (GPU contract drift):
- The old repo contract effectively assumed a device mount in the base production compose.
- That made CPU-only hosts impossible to start cleanly and left the runtime CPU fallback unreachable at infrastructure level.
- Symptom: `hwaccel=force` playback probes fail with `GPU not available (no /dev/dri/renderD128)` even though the host itself exposes the render node.
- Current rule: keep `/srv/xg2g/docker-compose.yml` device-neutral and load `/srv/xg2g/docker-compose.gpu.yml` only on GPU hosts. If VAAPI verification fails with that exact error, inspect the GPU overlay selection first; auto-discovery inside the app cannot recover from a missing container device mount.

Observed live-host delta on March 17, 2026 (playback/runtime):
- Safari-native media requests could not be trusted to carry header auth on `.m3u8` fetches; tokenized media URLs became the runtime-safe source of truth for HLS auth.
- After a service restart, Safari may keep polling an old HLS session URL from an already cancelled tab state. Symptom pattern:
  - repeated `GET /api/v3/sessions/<old-id>/hls/index.m3u8?...` with `410`
  - playback feedback like `networkError: levelLoadError`
  - no fresh `POST /api/v3/live/stream-info`, no fresh `POST /api/v3/intents`, and no new `playlist ready - transitioning to READY state`
  In that case, do not debug startup first. Hard-reload Safari (`Cmd+Option+R`) or reopen the tab, then start a new stream and inspect the fresh session instead of the stale `410` noise.

Observed live-host delta on March 23, 2026 (VAAPI runtime truth):
- Container-level GPU visibility is necessary but not sufficient. A live host with `/dev/dri/renderD128` mounted into the container still fell back to CPU for Safari transcode sessions until `ffmpeg.vaapiDevice` was explicitly configured.
- Current runtime rule: if GPU transcoding is desired, set exactly one of:
  - `/var/lib/xg2g/config.yaml`:
    ```yaml
    ffmpeg:
      vaapiDevice: /dev/dri/renderD128
    ```
  - `/etc/xg2g/xg2g.env`: `XG2G_VAAPI_DEVICE=/dev/dri/renderD128`
- Without that config key, VAAPI preflight is skipped during wiring/bootstrap and live playback decisions fail-closed to CPU (`libx264`) even when FFmpeg inside the container can encode with `h264_vaapi`.
- Verification rule: do not trust only `docker exec ... ls /dev/dri` or ad-hoc FFmpeg smoke tests. Also run a real transcode-capable live session and confirm log evidence such as:
  - `gpu_available:true`
  - `decision.summary ... "path":"transcode_hw"`
  - `pipeline video: vaapi`
- Operational shorthand: `ffmpeg.vaapiDevice` is currently not optional for GPU transcoding. A fresh deploy that omits it should be expected to fall back silently to CPU for eligible live transcode paths.

Observed live-host delta on March 24, 2026 (installation drift still present):
- `/etc/systemd/system/xg2g.service` did not byte-match either `/srv/xg2g/docs/ops/xg2g.service` or the repo template `docs/ops/xg2g.service`.
- `/srv/xg2g/docs/ops/xg2g.service` and the installed unit still used direct `/usr/bin/docker compose --project-name xg2g ...` calls even though `/srv/xg2g/scripts/compose-xg2g.sh` was present on the host.
- The installed unit carried extra `ExecStartPre` gates not present in the canonical host copy, including:
  - image preflight derived from `services.xg2g.image` in `/srv/xg2g/docker-compose.yml`
  - `docker compose run --rm --no-deps xg2g config validate -f /var/lib/xg2g/config.yaml`
- `/srv/xg2g/docker-compose.yml` was still pinned to `ghcr.io/manugh/xg2g:v3.3.0` and still mounted `/dev/dri/renderD128` in the base compose.
- `/srv/xg2g/docker-compose.gpu.yml` now existed, but it duplicated the same `/dev/dri/renderD128` device mount instead of being the sole GPU-specific overlay.
- Runtime truth matched that older deployment: `docker inspect` reported image `ghcr.io/manugh/xg2g:v3.3.0`, and recent container logs reported `version":"v3.3.0-hotfix26"`.
- Operational rule: for this host, treat the installed unit plus `/srv/xg2g/docker-compose.yml` as runtime truth and the GitHub docs as target-state documentation until `/srv/xg2g` and `/etc/systemd/system/xg2g.service` are redeployed together. The mere presence of `/srv/xg2g/scripts/compose-xg2g.sh` does not mean systemd is using the helper yet.

If you see the March 11 metrics-only health mismatch, fix the live env first by setting:

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
