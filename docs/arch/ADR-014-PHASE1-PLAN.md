# ADR-014 Phase 1: Migration Plan - internal/api → internal/control

**Date**: 2026-01-08  
**Status**: PLANNING  
**Related**: [ADR-014-app-structure-ownership.md](./ADR-014-app-structure-ownership.md)

---

## Current State Analysis

### Structure of `internal/api`

```
internal/api/ (79 .go files total)
├── http.go                    # Main router wiring
├── server_gen.go              # OpenAPI generated server
├── server_impl.go             # Server implementation
├── middleware.go              # Middleware registration
├── auth.go, config_reload.go, deps.go, fileserver.go, metrics.go
├── middleware/                # 15 files (cors, csrf, ratelimit, security headers, etc.)
├── v3/                        # 50+ files (handlers, auth, sessions, recordings, DVR, library)
│   ├── auth.go, handlers_core.go, http.go
│   ├── sessions*.go (heartbeat, wrapper)
│   ├── recordings*.go (classifier, contract, probe, remux, resume, vod)
│   ├── dvr.go, library.go
│   └── middleware/
├── dist/                      # WebUI assets (embedded)
└── testdata/                  # Contract test fixtures
```

### **Gate A Violations (EXISTS NOW)** ⚠️

**4 files currently import `internal/domain/session/store`**:

1. `internal/api/http.go` - Router setup
2. `internal/api/v3/contract_v3_test.go` - Contract tests
3. `internal/api/v3/v3.go` - V3 server factory
4. `internal/api/v3/auth_strict_test.go` - Auth tests

**This proves Gate A is needed** - Control layer is directly accessing stores NOW.

---

## Canonical Layout Decision (CTO Approved)

**Target**: `internal/control/http/v3/...`

**Rationale**:

- Makes "Ingress/HTTP" explicit
- Avoids false semantic sog ("api" implies domain)
- Clear separation: `control/http` (ingress) vs `domain` (business logic)

**Structure**:

```
internal/control/
├── auth/                     # Auth middleware & handlers (from internal/auth + api/auth.go)
├── middleware/               # HTTP middleware (from api/middleware/)
└── http/
    ├── server.go             # Router wiring (from api/http.go)
    ├── fileserver.go         # Static serving
    └── v3/                   # V3 API endpoints
        ├── sessions.go       # Session handlers
        ├── recordings.go     # Recording handlers
        ├── dvr.go           # DVR handlers
        ├── library.go       # Library handlers
        └── middleware/      # V3-specific middleware
```

---

## Gate A: Hard Enforcement

### Definition (Operational)

**FORBIDDEN** in `internal/control/**`:

- Importing `internal/domain/session/store`
- Importing `internal/domain/session/manager` (if it exposes Store)
- Direct calls to `Store.UpdateSession`, `PutSession`, `DeleteSession`, `TryAcquireLease`, `ReleaseLease`

**ALLOWED**:

1. Call domain use-cases (e.g., `session.Service.Start/Stop`)
2. Publish domain events via `Bus`

### Enforcement Strategy

**Two-layer check**:

1. **Import Check** (primary, catches 90%):

   ```bash
   # Fail if control imports store packages
   rg -type go 'internal/domain/session/store' internal/control/
   ```

2. **Call Check** (backup, catches creative bypasses):

   ```bash
   # Fail on direct store mutation methods
   rg -type go '\.(UpdateSession|PutSession|DeleteSession|TryAcquireLease|ReleaseLease)\(' internal/control/
   ```

**CI Gate**: Both checks must return 0 matches.

---

## Phase 1 Move Order (CTO-Optimized)

### PR #1: Skeleton + Gate A Script

**Scope**: Foundation only, no handler moves yet

- [ ] Create `internal/control/http/` directory structure
- [ ] Add `scripts/gate_a_check.sh` (import + call checks)
- [ ] Add CI job: `make gate-a` target  
- [ ] **Gate**: `make build && go test ./... && make gate-a` all PASS

**Files touched**: 0 moves, new dirs + script only

### PR #2: Middleware Layer

**Scope**: Pure cross-cutting, no domain writes

- [ ] Move `internal/api/middleware/* → internal/control/middleware/`
- [ ] Move `internal/auth/* → internal/control/auth/`
- [ ] Update imports in dependents
- [ ] **Gate**: `make build && go test ./... && make gate-a` all PASS

**Risk**: Low (middleware rarely writes stores)

### PR #3: WebUI + Static Serving (Read-Only)

**Scope**: Fileserver, embedded UI, health/status

- [ ] Move `internal/api/fileserver.go → internal/control/http/fileserver.go`
- [ ] Move `internal/api/dist/ → internal/control/http/dist/` (or keep in api/, TBD)
- [ ] Move health/version endpoints (if separate)
- [ ] **Gate**: `make build && go test ./... && make gate-a` all PASS

**Test**: GET endpoints only, no writes

### PR #4: V3 Read-Only Endpoints

**Scope**: GET handlers (sessions list, recordings list, library)

- [ ] Move read-only v3 handlers to `internal/control/http/v3/`
- [ ] **Expected Gate A violations**: If any read handler imports store → **MUST REFACTOR** to use domain service
- [ ] **Gate**: `make build && go test ./... && make gate-a` all PASS

**Critical**: This PR will expose existing architectural debt. Do NOT waive Gate A.

### PR #5: V3 Write Endpoints (High Risk)

**Scope**: POST/DELETE handlers (sessions start/stop, recordings delete)

- [ ] Move write handlers to `internal/control/http/v3/`
- [ ] **Expected Gate A violations**: HIGH - many will fail
- [ ] **Required refactor**: Convert direct store writes to:
  - Domain service calls (`session.Manager.StartSession(...)`)
  - OR Event publish (`bus.Publish(model.StartSessionEvent{...})`)
- [ ] **Gate**: `make build && go test ./... && make gate-a` all PASS

**Hard requirement**: Zero Gate A violations. If "too much work", that's the point.

### PR #6: Router Wiring + Server

**Scope**: Main router setup

- [ ] Move `internal/api/http.go → internal/control/http/server.go`
- [ ] Move `internal/api/server_impl.go → internal/control/http/v3/server.go`
- [ ] Fix Gate A violation in `http.go` (currently imports store)
- [ ] **Gate**: `make build && go test ./... && make gate-a` all PASS

### PR #7: Delete Legacy `internal/api`

**Scope**: Final cleanup

- [ ] Verify `internal/api/` is empty (except maybe `dist/` if kept for build reasons)
- [ ] Delete `internal/api/` directory
- [ ] Update all remaining imports
- [ ] **Gate**: `make build && go test ./... && scripts/verify_deps.sh && make gate-a` all PASS

---

## Hard Requirements (Non-Negotiable)

1. **Each PR must be green**:
   - `make build` PASS
   - `go test ./...` PASS
   - `scripts/verify_deps.sh` PASS
   - `make lint` PASS (if exists)
   - **`make gate-a` PASS** ← NEW

2. **No `internal/domain/session/store` imports in `internal/control/**`**
   - If tests need store: anti-pattern, refactor to use domain fixtures

3. **If handler writes store directly → MUST REFACTOR**:
   - Option A: Call domain use-case
   - Option B: Publish event → Session Manager handles
   - Option C is "skip for now" → REJECTED

---

## Success Criteria

Phase 1 is complete when:

- ✅ `internal/api` deleted (or only contains build artifacts with clear README)
- ✅ `internal/control/http/v3/` contains all HTTP handlers
- ✅ `make gate-a` enforced in CI and green
- ✅ Zero store imports in control layer

---

## Gate A Script (Minimum Viable)

**Location**: `scripts/gate_a_check.sh`

```bash
#!/bin/bash
set -e

echo "=== Gate A: Control Layer Purity Check ==="

# Check 1: Import prohibition
echo "Checking for forbidden store imports in control..."
STORE_IMPORTS=$(rg -type go --files-with-matches 'internal/domain/session/store' internal/control/ 2>/dev/null || true)
if [ -n "$STORE_IMPORTS" ]; then
    echo "❌ GATE A FAIL: internal/control cannot import session/store"
    echo "$STORE_IMPORTS"
    exit 1
fi

# Check 2: Direct mutation calls (backup check)
echo "Checking for direct store mutations..."
MUTATIONS=$(rg -type go --line-number '\.(UpdateSession|PutSession|DeleteSession|TryAcquireLease|ReleaseLease)\(' internal/control/ 2>/dev/null || true)
if [ -n "$MUTATIONS" ]; then
    echo "❌ GATE A FAIL: internal/control cannot directly mutate stores"
    echo "$MUTATIONS"
    exit 1
fi

echo "✅ Gate A: PASS - Control layer is pure"
```

**Makefile target**:

```makefile
.PHONY: gate-a
gate-a:
 @./scripts/gate_a_check.sh
```

---

## Next: Phase 1 Execution Starts with PR #1

Phase 0 complete ✅  
Phase 1 ready to start ✅  
Gate A script defined ✅
