# ADR-014 Phase 0: Phantom Domains Decision

**Date**: 2026-01-08  
**Status**: Investigation  
**Related**: [ADR-014-app-structure-ownership.md](./ADR-014-app-structure-ownership.md)

## Objective

Clarify status of "phantom domains" in `internal/domain/*` that appear to be empty shells or placeholders.

## Investigation Results

**Phantom domain candidates** (created during SOA planning but not yet populated):

- `internal/domain/config/`
- `internal/domain/control/`
- `internal/domain/lease/`
- `internal/domain/metadata/`
- `internal/domain/observability/`
- `internal/domain/playback/`
- `internal/domain/platform/` (collision with `infrastructure/platform`)

**Analysis pending**: Check if these contain any `.go` files or are truly empty.

## Decision Criteria (per ADR-014)

If empty shells (0 `.go` files):

- **Option A (preferred)**: Delete immediately to avoid false confidence
- **Option B**: Add `README.md` stub stating "reserved, not yet migrated; no code allowed here until migration ticket exists"

**Rule**: Prefer deletion unless a migration starts within 7 days.

## Next Actions

1. Run: `find internal/domain/{config,control,lease,metadata,observability,playback,platform} -name "*.go" | wc -l`
2. If count = 0: **DELETE** directories
3. If count > 0: Review files, determine if they should be:
   - Kept (actively used)
   - Moved (misplaced during earlier SOA attempt)
   - Deleted (abandoned experiment)

## Status

- [ ] Investigation complete
- [ ] Decision made (DELETE or STUB)
- [ ] Action executed
- [ ] Verified in `git status`

---

**This is Phase 0 of ADR-014 migration. Do not proceed to Phase 1 until Phase 0 is complete.**
