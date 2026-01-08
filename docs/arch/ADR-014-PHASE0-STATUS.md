# ADR-014 Phase 0: Phantom Domains - COMPLETE ✅

**Date**: 2026-01-08  
**Status**: ✅ COMPLETE  
**Related**: [ADR-014-app-structure-ownership.md](./ADR-014-app-structure-ownership.md)

---

## Investigation Results

### Phantom Domains Identified

All 7 phantom domains confirmed **empty shells** (0 .go files, 0 imports):

1. `internal/domain/config/`
2. `internal/domain/control/`
3. `internal/domain/lease/`
4. `internal/domain/metadata/`
5. `internal/domain/observability/`
6. `internal/domain/playback/`
7. `internal/domain/platform/` ← **Collision investigated**

### Platform Collision Investigation

**Diagnostics run** (per CTO directive):

```bash
# 1) Go files?
find internal/domain/platform -type f -name '*.go' -print
→ No output (0 files)

# 2) Who imports it?
rg -n 'internal/domain/platform' . --type go
→ No matches (0 imports)

# 3) Packages exist?
go list ./internal/domain/platform/...
→ "matched no packages"

# 4) In dependency tree?
go list -deps ./... | rg 'internal/domain/platform'
→ No matches
```

**Structure found**:

```
internal/domain/platform/
├── fs/       (empty)
├── net/      (empty)
└── process/  (empty)
```

**Decision**: DELETE (verified phantom shell, no behavioral loss risk)

---

## Actions Taken

✅ **DELETED** all 7 phantom domains:

- Removed empty directory shells
- No imports to rewrite (0 references)
- No behavior lost (0 code files)

**Verified safe** via:

- `make build` PASS
- `go test ./...` (no missing packages)
- `scripts/verify_deps.sh` PASS

---

## Rationale

Per ADR-014 Phase 0 criteria:
> Empty shells create false confidence and structural ambiguity.  
> Prefer deletion unless migration starts within 7 days.

No migration planned for these phantoms → **DELETE** confirmed correct.

---

## Phase 0 Status

**✅ COMPLETE**

**Next**: Phase 1 - `internal/api` → `internal/control` (with Gate A: Control cannot write stores directly)

---

## Learnings

1. **Always investigate before delete**: CTO directive to run diagnostics prevented blind deletion
2. **Collision != content**: `internal/domain/platform` name collision with `infrastructure/platform` was misleading - both existed but domain version was empty
3. **Verify zero impact**: 0 files + 0 imports + not in deps = safe delete

**Phase 0 prevented structural debt accumulation and unblocks Phase 1 with clean slate.**
