# Runtime Configuration Hot-Reload (Operator Guide)

This document describes the absolute hardening of the hot-reloading mechanism for xg2g.

## Design Policy

1. **Safety First**: Only explicitly approved fields (initially `LogLevel`) can be hot-reloaded.
2. **IO-Lock Serialization**: Configuration updates are protected by `configMu`. This mutex acts as a **Single-Flight + IO-Lock**; while a save/sync to disk is in progress, other configuration updates are blocked.
3. **Race Safety**: Read access to the current configuration MUST go through `s.GetConfig()`, which returns a race-safe snapshot under `RLock`. Direct access to `s.cfg` is a governance violation.
4. **Governed Audit Trail**: All runtime tuning is recorded via a dedicated `log.AuditInfo` API. This API bypasses global level filtering.
5. **Request-ID Continuity**: xg2g respects existing `request_id` values in the context if present (providing continuity for internal routers). If absent, a new server-side UUID is generated (enforcing truth). Client headers (`X-Request-ID`) are preserved as `client_request_id`.

## Tunable Configurations

Field | Path | Description | Safety Gate
--- | --- | --- | ---
`LogLevel` | `logLevel` | Changes global verbosity | Hard-coded Map Allowlist & Registry

## Audit Logs (Operator Truth)

Audit logs are recorded with `audit_severity: info` and are **never silenced** by the global log level.

### Structured Log Buffer

The "Recent Logs" API (`/api/v3/system/logs`) captures full structured logs, including all audit fields (`who`, `event`, `from`, `to`, `request_id`).

**Memory Safety (Anti-OOM)**:
The capture buffer is bounded to prevent memory exhaustion:

- **Max Accumulation**: 1 MiB (un-terminated lines are dropped).
- **Max Line size**: 64 KiB (giant entries are ignored).
- **Max Count**: 100 recent entries.

**Relevance Contract (Anti-Noise)**:
To preserve performance when `LogLevel` is set to `DEBUG`, the buffer only captures relevant events.

- **Allowed**: `component="audit"` or `event="request.handled"`
- **Dropped**: All other debug noise is filtered before parsing.

This ensures that the audit trail is preserved even if the central logging system is unreachable, without risking system stability.

### Example Audit Entry (Normalized)

```json
{
  "time": "2026-01-31T22:47:11Z",
  "audit_severity": "info",
  "component": "audit",
  "event": "log.level_changed",
  "who": "admin",
  "from": "info",
  "to": "debug",
  "request_id": "req-12345",
  "message": "runtime log level updated"
}
```

## Governance Gates

1. **AST Gate**: `scripts/verify-hot-reload-governance.go` verifies the `registry.go` annotations.
2. **Semantic Normalization**: `config.Diff` canonicalizes slices (`nil` is treated as `[]string{}`) and CSV strings, preventing spurious restarts for empty fields.
3. **Status Precision**:
    - `400 Bad Request`: Input validation or invalid level string.
    - `500 Internal Server Error`: Disk IO failure, registry drift, or system application failure.
