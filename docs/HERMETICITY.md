# Provably Hermetic Go Build + Codegen Pipeline

## Overview

xg2g provides **provably hermetic Go builds and code generation** - the build system works offline, without caches, without toolchain downloads, and produces deterministic output.

**Scope**: This proof covers the **Go build pipeline** and **code generation from OpenAPI specs**. Runtime dependencies (FFmpeg, certificates) and test execution are outside this guarantee's scope.

## Provable Guarantees

These guarantees are **mechanically enforced** in CI, not conventions:

### 1. Offline Build

Works with all network escape hatches blocked:

- `GOPROXY=off` - No module proxy
- `GOSUMDB=off` - No checksum database
- `GOVCS=*:off` - No VCS network fetches (even if deps are missing)
- `GOTOOLCHAIN=local` - No Go toolchain auto-download

### 2. Cache Independence

Succeeds with cold, per-step unique caches:

- `GOMODCACHE=$(mktemp -d)` - Fresh module cache
- `GOCACHE=$(mktemp -d)` - Fresh build cache

No warm cache masking missing vendored dependencies.

### 3. Deterministic Code Generation

`make generate` produces identical output every time:

- Verified via `git diff --exit-code -- internal/api internal/control/http/v3`
- Directory-based check (future-proof against new generated files)

### 4. Vendored Build Tools

All build-time tools hermetically vendored:

- Tools declared in [`tools.go`](../../tools.go)
- Vendored in `vendor/github.com/oapi-codegen/`
- Verified via `vendor/modules.txt` (canonical source)

### 5. No External Tool Installation

CI never calls:

- `go install oapi-codegen`
- `curl`/`wget` to download generators
- Any external binary downloads

## Architecture

```
tools.go (build-time dependency declaration)
    ↓
go mod vendor (hermetic vendoring)
    ↓
vendor/modules.txt (canonical module list)
    ↓
Makefile: go run -mod=vendor (hermetic execution)
    ↓
Generated code (deterministic output)
```

## Enforcement Mechanisms

### CTO Gate: Contract Enforcement

**Script**: [`scripts/ci/ctogate_hermetic_codegen.sh`](../../scripts/ci/ctogate_hermetic_codegen.sh)

- **Allow-list**: Codegen steps must use `make generate` only
- **Global forbid**: Blocks direct `oapi-codegen`, `go install`, `curl`/`wget` invocations
- **No self-reference**: External script avoids false positives

### Adversarial Offline Proof

**CI Step**: "Hermetic Build Proof (Adversarial-Grade)"

Environment:

```bash
GOPROXY=off
GOSUMDB=off
GOVCS=*:off
GOTOOLCHAIN=local
GOFLAGS=-mod=vendor
GIT_TERMINAL_PROMPT=0
GOMODCACHE=$(mktemp -d)
GOCACHE=$(mktemp -d)
```

Sequence:

1. Clean build (preserves `vendor/`)
2. Generate code with vendored tools
3. Verify determinism (`git diff --exit-code`)
4. Build binary offline

**Result**: Proof that no escape hatches exist.

### Makefile Invariants

**Target**: `make verify-hermetic-codegen`

Checks (behavior-based, not string-based):

1. `make -n generate` uses `go run -mod=vendor`
2. `vendor/modules.txt` lists `oapi-codegen`
3. `vendor/github.com/oapi-codegen/` directory exists
4. `tools.go` imports the generator

**Integrated**: Runs as part of `make quality-gates`

## Verification Commands

### Local Verification (Full Offline Test)

```bash
# Full adversarial test
GOPROXY=off GOSUMDB=off GOVCS=*:off GOTOOLCHAIN=local \
  GOMODCACHE=$(mktemp -d) GOCACHE=$(mktemp -d) \
  make clean && make generate && make build

# Verify determinism
make generate
git diff --exit-code -- internal/api internal/control/http/v3

# Verify hermetic invariants
make verify-hermetic-codegen
```

### CI Proof

See [`.github/workflows/ci.yml`](../../.github/workflows/ci.yml) - step
"Hermetic Build Proof (Adversarial-Grade)"

Every push proves the guarantee under adversarial conditions.

## Explicit Boundaries (Out of Scope)

### ❌ Runtime Hermeticity

- **FFmpeg**: External runtime dependency (not part of Go build)
- **Certificates**: System trust store (OS-level)
- **Network services**: Enigma2 receiver, etc.

**Rationale**: These are runtime concerns, distinct from build hermeticity.

### ❌ Test Hermeticity

- Unit tests may depend on external services (mock receivers, etc.)
- Integration tests may require network access

**Rationale**: Test hermeticity requires separate proof with test-specific constraints. Current proof is **build + codegen only**.

### ❌ OS Package Dependencies

- System packages outside Go ecosystem (e.g., `apt install`)
- Use Docker base image pinning for full isolation

**Rationale**: OS package hermeticity requires container-level guarantees.

## Why This Matters

### Reproducible Builds

Same input → Same output, always. Critical for:

- Build artifact verification
- Supply chain auditing
- Debugging production builds

### Supply Chain Security

No surprise downloads during build:

- No MITM attack surface
- No proxy compromise risk
- No "Tuesday's build is different" issues

### Offline Development

Works in air-gapped environments:

- Government/defense contractors
- High-security environments
- Disconnected development

### CI Reliability

No flakes from network issues:

- Proxy timeouts don't break builds
- Module mirror outages don't block CI
- Deterministic CI runs

## Audit Trail

Every CI run includes:

- `go env` printout (shows exactly what env vars were set)
- `git diff` output (proves determinism or shows violations)
- Offline build success/failure (proves network independence)

**Evidence**: CI run logs provide auditable, reproducible proof of hermetic guarantees.

## Maintenance

This guarantee is **mechanically enforced**, not manually maintained:

- **CTO Gate** prevents contract violations in CI
- **Adversarial proof** runs on every push
- **Quality gates** enforce invariants before merge

**Future-proof**: Adding new code generators requires updating `tools.go` + vendoring. The gates will catch violations.

---

### Trust Boundaries

The hermetic build process is the foundation for our security trust model.
By proving that our tools and code-generation paths are immutable and
deterministic, we can safely optimize scanner signal for handwritten code.
See [SECURITY.md](./SECURITY.md) for details on signal
governance and generated code trust.

---

**This is not a convention - it's a proven, mechanically enforced guarantee.**

Verified as of: January 2026  
CI proof: See latest `main` branch workflow runs
