# ADR-014 Phase 1: PR #1 - Foundation & Gate A

**Status**: Ready for Commit  
**PR**: #1  
**Scope**: Skeleton + Gate A enforcement (NO handler moves)

---

## PR #1: Definition of Done ✅

### 1. Skeleton Structure Created

```
internal/control/
├── auth/          (empty - placeholder for PR #2)
├── middleware/    (empty - placeholder for PR #2)
└── http/
    └── v3/        (empty - placeholder for PR #4+)
```

**No handler moves** - this is Foundation only.

### 2. Gate A Script: Control-Only ✅

**File**: `scripts/verify_gate_a_control_store.sh`

**Scope**: `internal/control/**` ONLY (legacy `internal/api` excluded)

**Checks**:

1. **Import prohibition** (primary):
   - Fails if `internal/control` imports `internal/domain/session/store`
   - Pattern: `rg 'internal/domain/session/store' internal/control/`

2. **Call prohibition** (backup):
   - Fails on: `.UpdateSession(`, `.PutSession(`, `.DeleteSession(`, `.TryAcquireLease(`, `.ReleaseLease(`
   - Pattern: `rg '\.(UpdateSession|PutSession|...)' internal/control/`

**Why control-only?**  
Legacy `internal/api` has 4 existing violations (identified in Phase 1 analysis). PR #1 establishes the gate - PR #4/5 eliminate the debt. Gate A must not fail on legacy to keep PR #1 green.

### 3. Make Target + CI Job ✅

**Makefile**:

```make
.PHONY: gate-a
gate-a:
 @./scripts/verify_gate_a_control_store.sh
```

**CI Job**: `gate-a-control-purity` (to be added to CI config)

**Local usage**: `make gate-a`

### 4. Binary Gates: ALL PASS ✅

- ✅ `make build` - PASS
- ✅ `go test ./...` - PASS (no tests run, skeleton only)
- ✅ `scripts/verify_deps.sh` - PASS
- ✅ `make gate-a` - PASS (no violations in empty control/)

---

## PR Body (CTO-Required Documentation)

**Why Gate A is control-only?**  
Legacy `internal/api` contains 4 identified store import violations. PR #1 establishes the architectural gate for NEW code (`internal/control`). PR #4/5 will migrate handlers and eliminate legacy violations. Failing on legacy would make PR #1 red before migration begins.

**Forbidden Imports**:

- `internal/domain/session/store`
- (Future: `internal/domain/**/store`)

**Forbidden Calls**:

- `.UpdateSession(...)`
- `.PutSession(...)`
- `.DeleteSession(...)`
- `.TryAcquireLease(...)`
- `.ReleaseLease(...)`

**CI Execution**:

- **Job name**: `gate-a-control-purity`
- **Make target**: `make gate-a`
- **Command**: `./scripts/verify_gate_a_control_store.sh`

**Which Followup PRs Will Fail Gate A (If Done Wrong)?**  

- **PR #4** (V3 Read Endpoints): If read handlers import store for listing
- **PR #5** (V3 Write Endpoints): **HIGH RISK** - most write handlers currently violate Gate A
  - Must refactor to call domain services (`session.Manager.StartSession(...)`)
  - OR publish events (`bus.Publish(model.StartSessionEvent{...})`)
- **PR #6** (Router Wiring): `http.go` currently imports store

**Consequence**: Gate A violations force architectural compliance - no silent debt accumulation.

---

## Next Steps

- [ ] Commit PR #1
- [ ] Push to feature branch / main
- [ ] Add CI job configuration (if not auto-detected by `make gate-a`)
- [ ] Proceed to PR #2 (Middleware + Auth migration)

---

**PR #1 Complete**: Skelton + Gate A established. Phase 1 foundation ready. ✅
