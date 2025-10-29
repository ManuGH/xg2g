# ADR 005: Daemon Bootstrap Consolidation

## Status

Proposed

## Context

Currently, xg2g has two separate bootstrapping paths:

1. **`cmd/daemon/main.go`** (535 lines):
   - Primary entry point
   - Environment variable parsing
   - Server setup (main API, metrics, proxy)
   - SSDP announcer
   - Signal handling
   - Graceful shutdown

2. **`internal/daemon/bootstrap.go`** (197 lines):
   - Structured daemon lifecycle
   - Telemetry initialization
   - Config loading
   - HTTP server setup
   - Graceful shutdown

This duplication creates several issues:

### Problems

- **Duplication**: Server configuration, shutdown logic, and signal handling exist in both places
- **Inconsistency**: Telemetry only in `internal/daemon`, proxy only in `cmd/daemon`
- **Testing**: Harder to test bootstrap logic when it's in `main()`
- **Maintainability**: Changes need to be synchronized between two files
- **Technical Debt**: New features (like proxy, metrics) added only to `cmd/daemon`

### Current Architecture

```
cmd/daemon/main.go (ACTIVE)
├─ Config loading
├─ Logger setup
├─ Proxy server
├─ Initial refresh
├─ Metrics server
├─ Main API server
├─ SSDP announcer
└─ Graceful shutdown

internal/daemon/bootstrap.go (UNUSED)
├─ Config loading
├─ Logger setup
├─ Telemetry
├─ HTTP server
└─ Graceful shutdown
```

## Decision

We will **consolidate** bootstrapping into `internal/daemon`, making it the **single source of truth** for daemon lifecycle management.

### Approach

**Incremental refactoring** over 3 phases:

#### Phase 1: Extract Configuration (Low Risk)
- Move ENV parsing helpers to `internal/config`
- Create `config.ServerConfig` struct
- Keep `cmd/daemon` as thin CLI layer

#### Phase 2: Extract Server Management (Medium Risk)
- Move server setup to `internal/daemon.Daemon`
- Add proxy, metrics, SSDP to `Daemon`
- Wire everything through `daemon.Config`

#### Phase 3: Simplify CMD Layer (High Impact)
- Reduce `cmd/daemon/main.go` to ~100 lines
- Just CLI flags → `daemon.New()` → `daemon.Start()`
- All logic in testable packages

### Target Architecture

```
cmd/daemon/main.go (~100 lines)
└─ CLI flags + daemon.New() + daemon.Start()

internal/daemon/
├─ daemon.go          (Daemon struct + lifecycle)
├─ config.go          (Config struct + ENV parsing)
├─ servers.go         (API, metrics, proxy setup)
└─ shutdown.go        (Graceful shutdown logic)

Responsibilities:
├─ Config loading
├─ Logger setup
├─ Telemetry init
├─ Proxy server
├─ Metrics server
├─ Main API server
├─ SSDP announcer
├─ Initial refresh
└─ Graceful shutdown
```

## Consequences

### Positive

- ✅ **Single Source of Truth**: All daemon logic in one package
- ✅ **Testable**: Can test daemon lifecycle without running `main()`
- ✅ **Reusable**: Other binaries (e.g., one-shot tools) can use `internal/daemon`
- ✅ **Maintainable**: Changes in one place
- ✅ **Consistent**: Telemetry, proxy, metrics all in one structure
- ✅ **Type-Safe**: Config structs instead of scattered ENV parsing

### Negative

- ⚠️ **Migration Effort**: ~2-3 hours of careful refactoring
- ⚠️ **Risk**: Regression if not tested thoroughly
- ⚠️ **Breaking**: Integration tests may need updates

### Mitigation

1. **Incremental approach**: Three small PRs, not one big-bang
2. **Test coverage**: Integration tests before each phase
3. **Backward compatibility**: Keep ENV vars working
4. **Rollback plan**: Git tags at each phase

## Implementation Plan

### Phase 1: Extract Configuration (~1 hour)

**Files changed:**
- `internal/config/env.go` (new)
- `internal/config/server.go` (new)
- `cmd/daemon/main.go` (use new config)

**Changes:**
```go
// internal/config/env.go
func ParseEnv() *EnvConfig { ... }
func ParseDuration(key string, def time.Duration) time.Duration { ... }

// internal/config/server.go
type ServerConfig struct {
    ReadTimeout, WriteTimeout, IdleTimeout time.Duration
    MaxHeaderBytes, ShutdownTimeout int
}

// cmd/daemon/main.go (simplified)
func main() {
    envCfg := config.ParseEnv()
    srvCfg := config.ParseServerConfig()
    // ... rest stays same for now
}
```

**Testing:**
- Unit tests for `config.ParseEnv()`
- Integration test: ENV vars still work

### Phase 2: Extract Server Management (~1.5 hours)

**Files changed:**
- `internal/daemon/daemon.go` (expand)
- `internal/daemon/servers.go` (new)
- `cmd/daemon/main.go` (delegate to daemon)

**Changes:**
```go
// internal/daemon/daemon.go
type Daemon struct {
    config      Config
    apiServer   *http.Server
    metricsServer *http.Server
    proxyServer   *proxy.Server
    hdhrServer    *hdhr.Server
    logger      zerolog.Logger
    telemetry   *telemetry.Provider
}

func (d *Daemon) Start(ctx context.Context) error {
    d.startProxyServer(ctx)
    d.startMetricsServer(ctx)
    d.startAPIServer(ctx)
    d.startSSDPAnnouncer(ctx)
    return d.waitForShutdown(ctx)
}

// cmd/daemon/main.go (much simpler)
func main() {
    cfg := daemon.ParseConfig(...)
    d := daemon.New(cfg)
    if err := d.Start(ctx); err != nil {
        log.Fatal(err)
    }
}
```

**Testing:**
- Integration test: All servers start
- Integration test: Graceful shutdown works
- Integration test: Proxy, metrics, SSDP all work

### Phase 3: Final Cleanup (~30 min)

**Files changed:**
- `cmd/daemon/main.go` (minimal)
- Remove deprecated helpers

**Target `cmd/daemon/main.go` (~100 lines):**
```go
func main() {
    // CLI flags
    showVersion := flag.Bool("version", false, "print version")
    configPath := flag.String("config", "", "config file")
    flag.Parse()

    if *showVersion {
        fmt.Println(Version)
        return
    }

    // Parse config
    cfg := daemon.ParseConfig(*configPath, Version)

    // Create daemon
    d, err := daemon.New(cfg)
    if err != nil {
        log.Fatal(err)
    }

    // Run
    ctx := daemon.WaitForShutdown()
    if err := d.Start(ctx); err != nil {
        log.Fatal(err)
    }
}
```

**Testing:**
- Full integration test suite
- Smoke test all features

## Alternatives Considered

### 1. Keep Current Structure
**Rejected**: Technical debt keeps growing, harder to test

### 2. Big-Bang Refactor
**Rejected**: Too risky, hard to review, difficult to rollback

### 3. Create New `cmd/daemon-v2`
**Rejected**: Confusing for users, migration burden

### 4. Move Everything to `cmd/daemon`
**Rejected**: Violates Go project layout, makes code untestable

## References

- [Standard Go Project Layout](https://github.com/golang-standards/project-layout)
- [Effective Go: Initialization](https://go.dev/doc/effective_go#init)
- [The Twelve-Factor App: Config](https://12factor.net/config)
- Previous ADRs: 001-api-versioning, 002-config-precedence

## Decision Date

2025-10-29

## Decision Makers

- @ManuGH (owner)
- Claude Code (co-author)
