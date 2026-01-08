# ADR-014: App Structure & Ownership Boundaries

**Status**: Proposed  
**Date**: 2026-01-08  
**Decision Owner**: CTO / Core Backend  
**Context**: SOA migration ongoing; verify_deps now at 0 waivers. Next risk is structural drift: mixed-layer legacy roots (internal/pipeline, internal/api) and duplicate platform abstractions.

---

## 1. Decision

We standardize the repository into a strict layered SOA structure:

**Layering (hard rule)**:

```
cmd → control → domain ← infrastructure
```

### 1.1 Canonical locations

#### `cmd/<app>`: Composition root / wiring only

- **Allowed**: config loading, dependency construction, lifecycle start/stop
- **Forbidden**: business logic, state transitions, policy decisions

#### `internal/control/**`: Ingress, API, auth, adapters to domain use-cases

- Contains HTTP handlers, middleware, request validation, OpenAPI binding
- **Forbidden**: direct store writes (except via domain APIs/ports)

#### `internal/domain/<domain>/**`: Product logic (ports + invariants + state machine)

- Must be testable without OS/FFmpeg/network
- **Forbidden**: importing `internal/infrastructure/*` or legacy roots

#### `internal/infrastructure/**`: Concrete implementations of ports (OS/FS, exec/media, bus, persistence)

- May import domain packages and implement domain ports
- **Forbidden**: implementing ingress or business policies

### 1.2 Naming rules

- `internal/api` is renamed/migrated to `internal/control`
- `internal/pipeline` is a legacy root and must be dissolved
- Domain names are product domains, not technologies:
  - **Allowed**: `session`, `playback`, `metadata`, `lease`, `recording`, `observability`, `platform` (domain concept), `config`
  - **Avoid**: `exec`, `ffmpeg`, `worker` as top-level domain names

### 1.3 Ownership (who can change what)

Each domain has an owner for architectural decisions and boundary enforcement:

- `domain/session`: Core Backend (Owner: Session Lead)
- `domain/playback`: Playback & Clients (Owner: Playback Lead)
- `domain/metadata`: Metadata & EPG (Owner: Data/Metadata Lead)
- `domain/lease`: Core Backend (Owner: Resource/Lease Lead)
- `control/*`: API Team (Owner: API Lead)
- `infrastructure/*`: Platform/SRE (Owner: Infra Lead)
- `config`: Core Backend + Product (Owner: Config/Contracts Lead)

**Ownership means**: Approves new ports, new public APIs, boundary exceptions (exceptions require ADR).

---

## 2. Rationale

**Current IST risks**:

- `internal/pipeline` mixes control + domain + infrastructure → boundary erosion and regression risk
- Duplicate platform utilities (`internal/platform/*` vs `infrastructure/platform` vs domain ports) → inconsistent semantics and security gaps
- "Phantom domains" (`internal/domain/*` empty shells) → misleading structure, false confidence
- `internal/api` name implies "domain" but contains ingress/HTTP → encourages wrong dependency direction

**We prioritize**:

- **Reliability**: deterministic behavior and enforceable invariants
- **Velocity**: clear import rules prevents refactor churn
- **Security**: platform-sensitive operations live behind ports/adapters

---

## 3. Non-negotiables (hard gates)

### Gate A: Store write prohibition in Control

- `internal/control/**` must not directly mutate domain stores (no `Store.UpdateSession`, etc.)
- Control must call domain use-cases (service layer) or publish domain events
- **Enforcement**: CI architecture test (grep/go list / custom script)

### Gate B: Domain must not import Infrastructure

- No `internal/domain/**` import path may include `internal/infrastructure/**`
- **Enforcement**: `scripts/verify_deps.sh` (already exists; extend rules as needed)

### Gate C: Single Composition Root

- `cmd/daemon` (or the chosen main) is the only place where concrete adapters are assembled
- Infrastructure packages should not "self-wire" domain services

---

## 4. Consequences

### Positive

- Removes ambiguity and prevents mixed-layer drift
- Enables safer modular refactors (ports/adapters)
- Improves testability and deterministic CI

### Negative / Costs

- Requires phased migration of `internal/pipeline` and `internal/api`
- Import rewrites and package renames will be recurring until legacy is removed
- Some "utility" code will need re-homing into either `domain/platform` (concept) or `infrastructure/platform` (OS)

---

## 5. Migration plan (no Big Bang)

**Rule**: Every migration step must satisfy:

- `make build` PASS
- `go test ./...` PASS
- `scripts/verify_deps.sh` PASS
- Scope is "move + imports + minimal wiring", unless a port extraction is the explicit goal

### Phase 0: Clarify "phantom domains"

If `internal/domain/{config,control,lease,metadata,observability,playback,platform}` are empty shells:

- Either **delete them immediately** or
- Add `README.md` stubs that state "reserved, not yet migrated; no code allowed here until migration ticket exists"
- **Decision**: prefer deletion unless a migration starts within 7 days

### Phase 1: Rename/Migrate Ingress

**Target**: `internal/api/**` → `internal/control/http/**` (or `internal/control/api/**` if versioned)

- Move `internal/auth/**` under `internal/control/auth/**`
- Ensure Control does not write stores directly (Gate A enforced)

### Phase 2: Dissolve internal/pipeline by category (Strangler)

Treat each sub-tree by true nature:

- `pipeline/bus` → `infrastructure/bus` (already exists; delete legacy wrapper when unused)
- `pipeline/exec` → `infrastructure/media/*` (already underway via ports)
- `pipeline/api` → `control/session/*` (ingress concerns)
- `pipeline/profiles` → `domain/playback/profiles` (pure domain data/config)
- `pipeline/lease` → `domain/lease` (ports + invariants; adapters in infra if needed)

**Each move requires**:

- Replace imports in dependents
- Remove old directory when it becomes empty (no "dead legacy copies")

### Phase 3: Re-home remaining root-level scattered packages

**Mapping rules**:

- `internal/vod` → `domain/playback/vod`
- `internal/dvr` and `internal/recordings` → `domain/recording` (or split: `recording` + `playback/vod`)
- `internal/epg`, `internal/library` → `domain/metadata/*`

---

## 6. Explicit "do not do"

- Do not move folders "because it looks nicer" without a boundary outcome
- Do not introduce new legacy roots
- Do not create new cross-domain imports; use ports

---

## 7. Success criteria

ADR-014 is complete when:

- `internal/pipeline` is empty (deleted) or contains only temporary shims with a dated deprecation plan
- `internal/api` no longer exists; replaced by `internal/control`
- All non-domain OS/FS/process operations are behind `ports.Platform` (or infra equivalents)
- CI gates for dependency direction and control-store prohibition are enforced and green

---

## CTO Decision

**Proceed**: ADR-014 is approved as written.

**Next execution step**: Phase 0 (phantom domains decision) + Phase 1 (`internal/api` → `internal/control`) with binary gates on every PR.

**Hard constraint**: no "random moves" outside these phases.
