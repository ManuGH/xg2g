<!-- GENERATED FILE - DO NOT EDIT. Source: templates/docs/ops/OPERATIONS_MODEL.md.tmpl -->
# xg2g Operations Model (Operator-Grade 2026)

This document defines the immutable operational contract for the `xg2g` service. Adherence to these invariants is mandatory for system stability.

## 1. What Must Always Be True (Hard Invariants)

- **Pre-flight Integrity**: The `xg2g.service` MUST NOT start if the configuration fails the containerized validation gate (`ExecStartPre`).
- **Health Gating**: The service is only considered "Active" once the integrated healthcheck reports `ready` and `metrics=true`.
- **Repo Truth**: The installed systemd unit MUST strictly match `/srv/xg2g/docs/ops/xg2g.service` (canonical ops path).
- **Identity Isolation**: The service MUST run as UID `10001` (xg2g) within the container.
- **Fail-Closed Auth**: No endpoint beyond `/healthz` and `/readyz` is accessible without a valid `XG2G_API_TOKEN`.
- **Resource Pinning**: The container MUST use the pinned image `ghcr.io/manugh/xg2g:3.1.7`.

## 2. What Is Allowed To Fail (Controlled Transients)

- **Background Data Refresh**: A failure to refresh EPG or Channel lists due to receiver downtime is allowed; the service remains "Healthy" but "Degraded" until the next successful ingest.
- **Metrics Scraping Latency**: Short-term metrics scraping timeouts are tolerable as long as the internal `/metrics` endpoint eventually responds within the healthcheck window. Metrics unavailability beyond the healthcheck window MUST flip the service to NotReady.
- **VAAPI Hardware Unavailability**: If the GPU is locked, the system may fall back to software transcoding (if configured), though this is monitored for performance loss.

## 3. What Is Intentionally Impossible (Security/Safety Boundaries)

- **Schema Drift**: Starting the service with unknown or malformed YAML keys in `config.yaml` is physically prevented by the `KnownFields(true)` loader.
- **Root FS Mutation**: Writing to any directory other than `/var/lib/xg2g` or `/tmp` within the container is blocked by the hardened read-only filesystem policy.
- **Drop-in Overrides**: Manual overrides in `/etc/systemd/system/xg2g.service.d/` are neutralized by the `xg2g-verify.service` audit.
- **Anonymous Operations**: Any state-changing operation (POST/PUT/DELETE) without a verified origin and token is impossible by design.

---

> [!NOTE]
> This model represents the "Operational Truth" as of Phase 4. Any modification to these boundaries requires a Governance Change Request (GCR).
