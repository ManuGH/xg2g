# Package Layout Policy

**Status:** Active (2026-01-11)
**Scope:** All code in `internal/`

This document defines the **non-negotiable rules** for where code lives in the xg2g codebase.

## Goals

1. **Prevent drift:** No ambiguity about where new code belongs
2. **Enforce layering:** HTTP layer cannot directly import infra layer
3. **Feature ownership:** Each feature has a clear home
4. **No "utils hell":** Reject catch-all packages like `common/`, `helpers/`, `utils/`

---

## Top-Level Structure

```
internal/
├── api/              # HTTP server lifecycle (legacy, being refactored)
├── app/              # Application bootstrap & wiring (DI container)
├── config/           # Configuration parsing, validation, hot-reload
├── control/          # Control plane (API handlers, middleware, auth)
├── domain/           # Business domain models (sessions, recordings)
├── pipeline/         # Media processing pipeline (transcoding, HLS)
├── infra/            # Infrastructure adapters (ffmpeg, platform APIs)
├── platform/         # OS/Runtime abstractions (fs, net, paths)
├── library/          # VOD library & metadata store
└── [feature]/        # Feature-specific packages (dvr, epg, jobs, etc.)
```

---

## Layering Rules

### Layer 1: Platform & Infra (Bottom)

**`internal/platform/`** - OS/Runtime abstractions
- `fs/` - Filesystem operations
- `net/` - Network utilities
- `paths/` - Path resolution

**`internal/infra/`** - External system adapters
- `ffmpeg/` - FFmpeg command building & execution
- `bus/` - Event bus adapter
- `media/` - Media probing & transcoding adapters

**Rule:** Platform and infra packages **MUST NOT** import domain or control packages.

---

### Layer 2: Domain & Config (Middle)

**`internal/domain/`** - Business domain models
- `session/` - Live streaming session lifecycle
- Records, events, state machines

**`internal/config/`** - Configuration management
- Parsing, validation, hot-reload, snapshots

**Rule:** Domain packages **MAY** import platform/infra, but **MUST NOT** import control/http.

---

### Layer 3: Control Plane (Top)

**`internal/control/`** - API control plane

```
control/
├── auth/             # Authentication & authorization
├── middleware/       # HTTP middleware (CORS, CSRF, logging, tracing)
├── http/             # HTTP utilities & file serving
│   └── v3/           # API v3 handlers & resolvers
├── playback/         # Playback decision engine
├── read/             # Read-only operations (EPG, services, DVR)
└── vod/              # VOD management (probing, policy, lifecycle)
```

**Rule:** Control packages **MAY** import domain/config/platform/infra, but **MUST** stay thin.

**HTTP Handler Policy:**
- Handlers (`internal/control/http/v3/`) are **thin adapters**
- Business logic belongs in domain packages (e.g., `internal/control/vod/manager.go`)
- Resolvers (`resolver_vod.go`) handle coordination but **MUST NOT** become god objects

---

### Layer 4: Application Bootstrap (Top)

**`internal/app/bootstrap/`** - Dependency injection & wiring
- Constructs the service graph
- Wires dependencies for testing
- **Side-effect free constructors:** `WireServices()` builds, `Start()` launches

**Rule:** Bootstrap imports everything, nothing imports bootstrap (except `cmd/`).

---

## Feature Packages

Feature-specific packages live at `internal/[feature]/`:

| Feature | Package | Purpose |
|---------|---------|---------|
| DVR | `internal/dvr/` | DVR scheduling & recording |
| EPG | `internal/epg/` | Electronic Program Guide |
| Jobs | `internal/jobs/` | Background job queue |
| Library | `internal/library/` | VOD metadata & duration store |
| Pipeline | `internal/pipeline/` | Media processing pipeline |
| Recordings | `internal/recordings/` | Recording path mapping |

**Rule:** Feature packages **MAY** import domain/config/platform/infra. They **SHOULD NOT** import each other (use domain models for inter-feature communication).

---

## Import Rules (Enforced by Tests)

### ✅ ALLOWED

- `control/http/v3/` → `control/vod/` (handler → domain)
- `control/vod/` → `infra/ffmpeg/` (domain → infra)
- `domain/session/` → `platform/fs/` (domain → platform)
- `app/bootstrap/` → anything (DI container)

### ❌ FORBIDDEN

- `control/http/v3/` → `infra/ffmpeg/` (HTTP bypasses domain)
- `domain/session/` → `control/http/` (domain imports HTTP)
- `infra/ffmpeg/` → `domain/session/` (infra imports domain)
- `platform/fs/` → `config/` (platform imports config)

---

## Ownership Rules

### VOD Feature Ownership

VOD spans multiple layers:

- **Domain logic:** `internal/control/vod/` (manager, prober, policy)
- **HTTP handlers:** `internal/control/http/v3/handlers_vod.go`
- **HTTP resolvers:** `internal/control/http/v3/resolver_vod.go`
- **DTOs:** `internal/control/http/v3/types/vod.go`
- **Storage:** `internal/library/` (duration cache, metadata)

**Rule:** HTTP resolvers **MUST** delegate to `control/vod/Manager` for business decisions. Resolvers handle HTTP concerns only (negative caching, singleflight, interest tracking).

### Playback Feature Ownership

- **Decision engine:** `internal/control/playback/engine.go` (Live/DVR/VOD routing)
- **HTTP integration:** `internal/control/http/v3/` (calls engine)

**Rule:** Playback logic lives in `control/playback/`, HTTP layer just invokes it.

---

## Deprecated Packages

### `internal/core/` - DEPRECATED

**DO NOT add new code here.**

See [internal/core/README.md](../../internal/core/README.md) for migration plan.

### `internal/api/` - LEGACY

The old monolithic API server. Being refactored into:
- `app/bootstrap/` (wiring)
- `control/http/v3/` (handlers)
- Feature-specific packages

**Rule:** New code goes to the new structure, not `internal/api/`.

---

## Adding New Code

### Q: Where do I add a new HTTP endpoint?

1. **Handler:** `internal/control/http/v3/handlers_[feature].go`
2. **Resolver:** `internal/control/http/v3/resolver_[feature].go` (if coordination is needed)
3. **DTO:** `internal/control/http/v3/types/[feature].go`
4. **Business logic:** `internal/control/[feature]/` or `internal/domain/[feature]/`

### Q: Where do I add a new utility function?

**STOP.** Ask yourself:

1. Is it OS/runtime related? → `internal/platform/`
2. Is it an external system adapter? → `internal/infra/`
3. Is it domain-specific? → `internal/domain/[feature]/`
4. Is it HTTP-specific? → `internal/control/http/`

**DO NOT create `internal/util/` or `internal/common/`.**

### Q: Where do I add a new configuration field?

1. **Schema:** `internal/config/config.go`
2. **Validation:** `internal/config/validation.go`
3. **Snapshot logic:** `internal/config/snapshot.go`

---

## Enforcement

### Automated Guardrails

Import rules are enforced by:
- `internal/validate/imports_test.go` (layering checks)
- Go module boundaries (`internal/` is private)

### Code Review Checklist

- [ ] Does this code belong in the right layer?
- [ ] Does this introduce a forbidden import?
- [ ] Is this creating a new "utils" package? (REJECT)
- [ ] Is this duplicating existing functionality in `core/` or `platform/`?

---

## Rationale

### Why no `internal/utils/`?

Utils packages become dumping grounds. Instead:
- **Be specific:** `internal/platform/fs/security/` beats `internal/utils/pathutil.go`
- **Collocate:** Put helpers next to their usage (e.g., `control/vod/policy.go`)

### Why strict layering?

- **Testability:** Domain logic can be tested without HTTP
- **Clarity:** HTTP layer is thin and obviously correct
- **Velocity:** Changes don't ripple across unrelated layers

### Why deprecate `core/`?

The name "core" provides no semantic information. It became a second `utils/` package.

---

## References

- [Hexagonal Architecture](https://alistair.cockburn.us/hexagonal-architecture/)
- [Go Project Layout](https://github.com/golang-standards/project-layout)
- [Screaming Architecture](https://blog.cleancoder.com/uncle-bob/2011/09/30/Screaming-Architecture.html)

---

**Last updated:** 2026-01-11
**Owner:** Engineering
