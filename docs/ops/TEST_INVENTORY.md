# Test Inventory

> **Generated on**: 2026-01-06
> **Source**: `tools/audit_tests.sh` execution on repository.

## 1. Backend (Go)

**Summary**: Strong coverage in Config, API v3, and Middleware. Moderate coverage in Core Logic (EPG, Health).

| Package | Count | Criticality | Purpose |
|---------|-------|-------------|---------|
| `internal/config` | 76 | **P0** | Configuration loading, environment overrides, validation logic. |
| `internal/api/v3` | 53 | **P0** | API Handlers, *Auth Invariants (5 new)*. |
| `internal/api/middleware` | 26 | **P0** | Auth, CORS, Logging, Recovery. |
| `internal/jobs` | 24 | P1 | Background tasks (EPG refresh, etc). |
| `internal/pipeline/worker` | 21 | P1 | Pipeline orchestration. |
| `internal/openwebif` | 21 | P1 | Upstream receiver client logic. |
| `internal/validate` | 20 | **P0** | Core validation primitives. |
| `internal/health` | 20 | **P0** | Healthcheck logic. |
| `internal/epg` | 20 | P2 | EPG parsing and processing. |
| `internal/cache` | 17 | P1 | Caching layer. |
| `internal/recordings` | 15 | P1 | DVR listing and management. |
| `internal/telemetry` | 15 | P2 | OpenTelemetry traces/metrics. |
| `internal/metrics` | 14 | P2 | Prometheus metrics. |
| `test/integration` | 11 | **P1** | End-to-End integration scenarios. |
| `internal/auth` | 4 | **P0** | Core authentication logic (Low count warranting review). |

*(Full listing available in audit logs)*

## 2. Frontend (WebUI)

**Summary**: **Minimal Smoke Coverage Established (Vitest)**

* **Test Files**: 1 found (`webui/tests/smoke.test.tsx`)
* **Test Framework**: Vitest + React Testing Library + JSDOM
* **Coverage**:
  1. `Settings Load`: Verify Universal Policy display (P0).
  2. `No Profile Dropdown`: Verify Thin Client contract (P0).
* **Quality Gates**:
    1. `npm test` (Vitest) - **NEW**
    2. `tsc` (Static Type Checking)
    3. `npm run build` (Production Build)

## 3. Scripts & Verification

| Script | Purpose | CI Enforced? |
|--------|---------|--------------|
| `scripts/check-go-toolchain.sh` | Enforces exact Go version. | Yes |
| `scripts/verify-web-startup.sh` | Checks if UI assets load. | Manual/Optional |
| `make smoke-test` | Runs binary and checks startup. | Yes (Integration Job) |
| `make generate` | Checks for Drift in Generated Code. | Yes |
