# xg2g v3.0.0 Release Notes

**Release Date:** 2025-12-24
**Status:** Production Ready / Feature Complete

---

## Overview

xg2g v3.0.0 marks the completion of a comprehensive architectural modernization, delivering a production-ready streaming middleware with enterprise-grade security, observability, and a fully TypeScript-based frontend. This release is **feature complete** and **stable** for production deployment.

### Key Achievements

- ✅ **Event-Driven V3 Streaming Architecture** with finite state machine and persistent sessions
- ✅ **100% TypeScript Frontend Migration** (Phase 1-6 complete)
- ✅ **RBAC Security Model** with scoped Bearer tokens
- ✅ **OpenTelemetry Integration** for distributed tracing
- ✅ **Zero Legacy Code** - all compatibility layers removed

---

## Breaking Changes

### 1. Authentication (Security Enhancement)

**Removed:** Query parameter authentication (`?token=...`)

```diff
- http://localhost:8080/stream.m3u8?token=abc123
+ Authorization: Bearer abc123
```

**Rationale:** Prevents token leakage in proxy logs, browser history, and referrer headers.

**Migration:**
- Use `Authorization: Bearer <token>` header, or
- Use `xg2g_session` cookie (WebUI only)

**Impact:** All API clients must update authentication method.

---

### 2. Configuration (Simplified)

**Removed:** Legacy environment variable aliases

```diff
- RECEIVER_IP → Use XG2G_OPENWEBIF_BASEURL
- XG2G_API_ADDR → Use XG2G_API_BIND
```

**Changed:** Default config schema version is `3.0.0` with **strict validation enabled**

**Migration:**
1. Update environment variables to canonical names (see `config.example.yaml`)
2. Run `xg2g config validate` to check for errors
3. (Optional) Use `xg2g config migrate` for automated upgrade scaffolding

---

### 3. API Version (Stable)

**Version:** `/api/v3` is now the **stable production API**

- V2 endpoints are deprecated (will be removed in v4.0.0)
- V3 routes always registered with semantic status codes
- RFC 7807 Problem Details for all errors

---

## New Features

### Security

- **RBAC (Role-Based Access Control):**
  - Scoped tokens: `read`, `write`, `admin`
  - Deny-by-default enforcement for sensitive endpoints
  - Centralized route registration with scope mapping
  - See `docs/guides/rbac.md` for endpoint→scope reference

- **Security Hardening:**
  - Non-root container execution (uid 65532)
  - SBOM (Software Bill of Materials) generation
  - Automated vulnerability scanning (govulncheck, Trivy)

### Observability

- **OpenTelemetry Integration:**
  - Distributed tracing (Jaeger/Tempo support)
  - Structured logging (zerolog, JSON output)
  - Prometheus metrics (`/metrics`)
  - Health checks (`/api/v3/system/health`)

### Frontend (TypeScript Migration)

**Phase 3:** Component Migration
- All 40 frontend files migrated to `.tsx`/`.ts` (0 `.jsx` remaining)
- Strict TypeScript mode enabled

**Phase 4:** Performance Optimization
- Lazy loading and code splitting
- HLS.js, React, API client in separate bundles
- Final bundle size: **868KB** (optimized)

**Phase 5:** Feature Architecture
- EPG module with data layer, state machine, UI components
- Clean separation of concerns (Data / Model / UI)
- Type-safe API client auto-generated from OpenAPI spec

**Phase 6:** Advanced Optimization
- Lazy-loaded Player component (heavy HLS.js dependency isolated)

### Configuration

- **Config Version Management:**
  - `configVersion: "3.0.0"` in YAML
  - Strict validation by default (fail-fast on errors)
  - Migration scaffolding: `xg2g config migrate`

- **Validation:**
  - `xg2g config validate` - Check YAML syntax and schema
  - `xg2g config dump --effective` - Show merged config (ENV + file + defaults)

### Architecture

- **Unified Circuit Breaker:**
  - Consolidated into `internal/resilience` package
  - Replaces scattered implementations in `api` and `openwebif`

- **V3 Store Validation:**
  - Startup validation for HLS paths (fail-fast if unwritable)
  - Prevents runtime errors from misconfiguration

---

## Upgrade Guide

### Step 1: Review Breaking Changes

**Action Items:**
1. Update API authentication from query params to Bearer tokens
2. Replace legacy environment variables with canonical names
3. Update client code to use `/api/v3` endpoints

### Step 2: Backup Configuration

```bash
cp config.yaml config.yaml.bak
```

### Step 3: Validate Configuration

```bash
# Check for errors
xg2g config validate --config config.yaml

# Preview effective config (with secrets redacted)
xg2g config dump --effective
```

### Step 4: Update Environment Variables

**Example `.env` migration:**

```diff
- RECEIVER_IP=192.168.1.50
+ XG2G_OPENWEBIF_BASEURL=http://192.168.1.50

- XG2G_API_ADDR=:8080
+ XG2G_API_BIND=:8080
```

See `config.example.yaml` for complete reference.

### Step 5: Update Client Authentication

**Before (v2):**
```bash
curl "http://localhost:8080/api/v2/status?token=abc123"
```

**After (v3):**
```bash
curl -H "Authorization: Bearer abc123" \
     http://localhost:8080/api/v3/system/health
```

### Step 6: Deploy

**Docker Compose:**
```bash
docker compose pull
docker compose up -d
```

**Bare Metal:**
```bash
systemctl restart xg2g
journalctl -fu xg2g
```

### Step 7: Verify

```bash
# Health check
curl http://localhost:8080/api/v3/system/health

# Check logs for warnings
journalctl -u xg2g --since "5 minutes ago" | grep WARN
```

---

## Deprecation Timeline

| Component | Deprecated In | Removed In | Notes |
|-----------|---------------|------------|-------|
| Query token auth (`?token=...`) | v2.0.1 | **v3.0.0** | ✅ Removed |
| Legacy env vars (`RECEIVER_IP`, etc.) | v2.1.0 | **v3.0.0** | ✅ Removed |
| `/api/v2/*` endpoints | v3.0.0 | v4.0.0 | Use `/api/v3/*` |
| Compatibility layer (frontend) | v3.0.0 | **v3.0.0** | ✅ Removed |

---

## Technical Details

### Frontend Architecture

**Migration Phases (All Complete):**

| Phase | Scope | Status |
|-------|-------|--------|
| Phase 1 | TypeScript setup, basic types | ✅ Complete |
| Phase 2 | Context API for state management | ✅ Complete |
| Phase 3 | Component migration to `.tsx` | ✅ Complete |
| Phase 4 | Lazy loading + bundle optimization | ✅ Complete |
| Phase 5 | EPG feature architecture | ✅ Complete |
| Phase 6 | Performance optimization (lazy Player) | ✅ Complete |

**Metrics:**
- **LOC:** 4,132 lines of TypeScript
- **Bundle Size:** 868KB (optimized with code splitting)
- **Coverage:** 55%+ (CI-enforced)

### Backend Architecture

**V3 Event-Driven Design:**
- **Intent-Based API:** Clients request Start/Stop intents, receive SessionID
- **Event Bus:** Decouples API from Worker (in-memory pub/sub)
- **Finite State Machine:** New → Tuning → Transcoding → Ready → Stopped
- **State Persistence:** BadgerDB/BoltDB for session lifecycle
- **HLS Delivery:** 45-minute timeshift buffer, browser-native playback

**Code Organization:**
- **Packages:** 28 internal Go packages
- **Tests:** Unit, integration, contract, benchmark, smoke, load
- **Binary Size:** 26MB (statically linked, embedded WebUI)

---

## Known Issues

None. This release is **production stable**.

---

## Rollback Plan

If you encounter issues during upgrade:

### Rollback to v2.1.0

**Docker:**
```bash
docker compose down
docker compose pull xg2g:v2.1.0
docker compose up -d
```

**Bare Metal:**
```bash
systemctl stop xg2g
mv /usr/local/bin/xg2g /usr/local/bin/xg2g.v3.bak
mv /usr/local/bin/xg2g.v2 /usr/local/bin/xg2g
systemctl start xg2g
```

### Restore Configuration

```bash
cp config.yaml.bak config.yaml
systemctl restart xg2g
```

---

## Support and Documentation

- **User Guide:** `docs/guides/user-guide.md`
- **RBAC Guide:** `docs/guides/rbac.md`
- **API Reference:** `api/openapi.yaml` (OpenAPI 3.0 spec)
- **Architecture Decisions:** `docs/adr/`
- **Issue Tracker:** https://github.com/anthropics/xg2g/issues (placeholder)

---

## Acknowledgments

This release represents a **complete architectural modernization** with focus on:
- Security (RBAC, token-based auth)
- Observability (tracing, structured logging, metrics)
- Maintainability (TypeScript, ADRs, comprehensive testing)
- Production readiness (12-Factor compliance, container-native)

**Status:** Feature complete and ready for production deployment.

---

## Checksums

**Binary Verification:**

```bash
# SHA256 checksums (example - replace with actual)
sha256sum xg2g_3.0.0_linux_amd64.tar.gz
# <checksum>  xg2g_3.0.0_linux_amd64.tar.gz
```

**Docker Image:**
```bash
docker pull xg2g:v3.0.0
# Digest: sha256:<digest>
```

---

**Questions?** See `docs/guides/` or open an issue.
