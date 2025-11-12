# Daemon Refactoring Plan

This document provides a step-by-step plan for consolidating `cmd/daemon` and `internal/daemon` bootstrap logic.

## Goal

Reduce `cmd/daemon/main.go` from **535 lines** to **~100 lines** by moving all daemon logic to `internal/daemon`.

## Current State

```
cmd/daemon/main.go:        535 lines (✗ untestable, ✗ duplicated)
internal/daemon/bootstrap.go: 197 lines (✗ unused, ✗ incomplete)
```

## Target State

```
cmd/daemon/main.go:        ~100 lines (✓ thin CLI layer)
internal/daemon/daemon.go:  ~300 lines (✓ testable lifecycle)
internal/daemon/config.go:  ~150 lines (✓ type-safe config)
internal/daemon/servers.go: ~200 lines (✓ modular servers)
```

---

## Phase 1: Extract Configuration

**Goal**: Move ENV parsing to `internal/config`, reduce duplication.

**Estimated Time**: 1 hour

**Risk Level**: 🟢 Low (no behavior change)

### Steps

#### 1.1 Create `internal/config/env.go`

Move these functions from `cmd/daemon/main.go`:
- `env(key, default) string`
- `envWithLogger(logger, key, default) string`
- `envDuration(key, def) time.Duration`
- `envIntDefault(key, def) int`
- `atoi(s) int` (deprecated, mark for removal)

**New file**: `internal/config/env.go`

```go
package config

// ParseString reads a string from ENV
func ParseString(key, defaultValue string) string { ... }

// ParseDuration reads a duration from ENV
func ParseDuration(key string, def time.Duration) time.Duration { ... }

// ParseInt reads an integer from ENV
func ParseInt(key string, def int) int { ... }

// ParseBool reads a boolean from ENV
func ParseBool(key string, def bool) bool { ... }
```

#### 1.2 Create `internal/config/server.go`

Extract server configuration parsing:

```go
package config

type ServerConfig struct {
    ListenAddr      string
    ReadTimeout     time.Duration
    WriteTimeout    time.Duration
    IdleTimeout     time.Duration
    MaxHeaderBytes  int
    ShutdownTimeout time.Duration
}

func ParseServerConfig() ServerConfig {
    return ServerConfig{
        ListenAddr:      ParseString("XG2G_LISTEN", ":8080"),
        ReadTimeout:     ParseDuration("XG2G_SERVER_READ_TIMEOUT", 5*time.Second),
        WriteTimeout:    ParseDuration("XG2G_SERVER_WRITE_TIMEOUT", 10*time.Second"),
        IdleTimeout:     ParseDuration("XG2G_SERVER_IDLE_TIMEOUT", 120*time.Second"),
        MaxHeaderBytes:  ParseInt("XG2G_SERVER_MAX_HEADER_BYTES", 1<<20),
        ShutdownTimeout: ParseDuration("XG2G_SERVER_SHUTDOWN_TIMEOUT", 15*time.Second),
    }
}
```

#### 1.3 Update `cmd/daemon/main.go`

Replace direct ENV parsing with new config package:

```go
import "github.com/ManuGH/xg2g/internal/config"

func main() {
    // ...
    srvCfg := config.ParseServerConfig()
    srv := &http.Server{
        Addr:           srvCfg.ListenAddr,
        ReadTimeout:    srvCfg.ReadTimeout,
        // ...
    }
}
```

#### 1.4 Testing

- [ ] Unit test `config.ParseString()` with ENV set
- [ ] Unit test `config.ParseDuration()` with invalid values
- [ ] Unit test `config.ParseInt()` with empty ENV
- [ ] Integration test: Server starts with ENV vars
- [ ] Integration test: Server uses defaults when ENV unset

**Acceptance Criteria**:
- ✅ All tests pass
- ✅ No behavior change
- ✅ `cmd/daemon/main.go` reduced by ~100 lines

---

## Phase 2: Extract Server Management

**Goal**: Move proxy, metrics, API server setup to `internal/daemon`.

**Estimated Time**: 1.5 hours

**Risk Level**: 🟡 Medium (behavior preserved, but complex)

### Steps

#### 2.1 Expand `internal/daemon/daemon.go`

Add server fields to `Daemon` struct:

```go
type Daemon struct {
    config        Config
    jobsCfg       jobs.Config

    // Servers
    apiServer     *http.Server
    metricsServer *http.Server
    proxyServer   *proxy.Server
    hdhrServer    *hdhr.Server

    // Infrastructure
    logger        zerolog.Logger
    telemetry     *telemetry.Provider
}
```

#### 2.2 Create `internal/daemon/config.go`

Consolidate all daemon configuration:

```go
type Config struct {
    // Core
    Version    string
    ConfigPath string

    // Server
    Server config.ServerConfig

    // Metrics
    MetricsAddr string

    // Proxy
    ProxyEnabled  bool
    ProxyAddr     string
    ProxyTarget   string

    // Features
    InitialRefresh bool
    HDHomeRun      bool
}

func ParseConfig(configPath, version string) (*Config, error) {
    cfg := &Config{
        Version:    version,
        ConfigPath: configPath,
        Server:     config.ParseServerConfig(),
        // ...
    }
    return cfg, nil
}
```

#### 2.3 Create `internal/daemon/servers.go`

Extract server lifecycle methods:

```go
func (d *Daemon) startAPIServer(ctx context.Context, handler http.Handler) error
func (d *Daemon) startMetricsServer(ctx context.Context) error
func (d *Daemon) startProxyServer(ctx context.Context) error
func (d *Daemon) startSSDPAnnouncer(ctx context.Context) error
func (d *Daemon) performInitialRefresh(ctx context.Context) error
```

#### 2.4 Update `daemon.Start()`

Orchestrate all servers:

```go
func (d *Daemon) Start(ctx context.Context, handler http.Handler) error {
    d.logger.Info().
        Str("version", d.config.Version).
        Str("listen", d.config.Server.ListenAddr).
        Msg("Starting xg2g daemon")

    // Initialize telemetry
    if err := d.initTelemetry(ctx); err != nil {
        d.logger.Warn().Err(err).Msg("Telemetry init failed")
    }

    // Start servers
    if d.config.ProxyEnabled {
        if err := d.startProxyServer(ctx); err != nil {
            return err
        }
    }

    if d.config.MetricsAddr != "" {
        if err := d.startMetricsServer(ctx); err != nil {
            return err
        }
    }

    if d.config.InitialRefresh {
        if err := d.performInitialRefresh(ctx); err != nil {
            d.logger.Warn().Err(err).Msg("Initial refresh failed")
        }
    }

    if err := d.startAPIServer(ctx, handler); err != nil {
        return err
    }

    if d.config.HDHomeRun {
        if err := d.startSSDPAnnouncer(ctx); err != nil {
            d.logger.Warn().Err(err).Msg("SSDP announcer failed")
        }
    }

    return d.waitForShutdown(ctx)
}
```

#### 2.5 Create `internal/daemon/shutdown.go`

Extract shutdown logic:

```go
func (d *Daemon) waitForShutdown(ctx context.Context) error {
    <-ctx.Done()
    d.logger.Info().Msg("Shutting down gracefully...")

    shutdownCtx, cancel := context.WithTimeout(
        context.Background(),
        d.config.Server.ShutdownTimeout,
    )
    defer cancel()

    return d.Shutdown(shutdownCtx)
}

func (d *Daemon) Shutdown(ctx context.Context) error {
    var errs []error

    // Shutdown API server
    if d.apiServer != nil {
        if err := d.apiServer.Shutdown(ctx); err != nil {
            errs = append(errs, fmt.Errorf("API shutdown: %w", err))
        }
    }

    // Shutdown metrics server
    if d.metricsServer != nil {
        if err := d.metricsServer.Shutdown(ctx); err != nil {
            errs = append(errs, fmt.Errorf("metrics shutdown: %w", err))
        }
    }

    // Shutdown proxy server
    if d.proxyServer != nil {
        if err := d.proxyServer.Shutdown(ctx); err != nil {
            errs = append(errs, fmt.Errorf("proxy shutdown: %w", err))
        }
    }

    // Shutdown telemetry
    if d.telemetry != nil {
        if err := d.telemetry.Shutdown(ctx); err != nil {
            errs = append(errs, fmt.Errorf("telemetry shutdown: %w", err))
        }
    }

    if len(errs) > 0 {
        return fmt.Errorf("shutdown errors: %v", errs)
    }

    d.logger.Info().Msg("Daemon stopped cleanly")
    return nil
}
```

#### 2.6 Simplify `cmd/daemon/main.go`

Now just wire up the daemon:

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

    // Parse daemon config
    cfg, err := daemon.ParseConfig(*configPath, Version)
    if err != nil {
        log.Fatal(err)
    }

    // Create daemon
    d, err := daemon.New(cfg)
    if err != nil {
        log.Fatal(err)
    }

    // Create API handler
    apiCfg := d.JobsConfig() // expose jobs config
    apiHandler := api.New(apiCfg).Handler()

    // Run daemon
    ctx := daemon.WaitForShutdown()
    if err := d.Start(ctx, apiHandler); err != nil {
        log.Fatal(err)
    }
}
```

#### 2.7 Testing

- [ ] Unit test `daemon.Start()` with all features enabled
- [ ] Unit test `daemon.Shutdown()` with timeout
- [ ] Integration test: All servers start and stop gracefully
- [ ] Integration test: Proxy server works
- [ ] Integration test: Metrics server works
- [ ] Integration test: SSDP announcer works
- [ ] Integration test: SIGTERM triggers graceful shutdown
- [ ] Integration test: Initial refresh runs if configured

**Acceptance Criteria**:
- ✅ All tests pass
- ✅ All features work identically
- ✅ `cmd/daemon/main.go` reduced to ~150 lines

---

## Phase 3: Final Cleanup

**Goal**: Remove deprecated code, finalize structure.

**Estimated Time**: 30 minutes

**Risk Level**: 🟢 Low (cleanup only)

### Steps

#### 3.1 Remove Deprecated Functions

- [ ] Remove `atoi()` from old code
- [ ] Remove duplicate `env()` functions
- [ ] Remove `maskURL()` from cmd (move to config if needed)
- [ ] Remove `ensureDataDir()` from cmd (move to config validation)

#### 3.2 Update Documentation

- [ ] Update `docs/ARCHITECTURE.md` with new structure
- [ ] Add `internal/daemon/README.md` explaining structure
- [ ] Update `CONTRIBUTING.md` with daemon guidelines

#### 3.3 Final Testing

- [ ] Full integration test suite (all existing tests)
- [ ] Smoke test all features manually
- [ ] Check test coverage (target: > 80%)
- [ ] Run benchmarks to ensure no performance regression

**Acceptance Criteria**:
- ✅ `cmd/daemon/main.go` is ~100 lines
- ✅ All logic in `internal/daemon`
- ✅ All tests pass
- ✅ Documentation updated

---

## Rollback Plan

If any phase fails:

1. **Git Revert**: `git revert HEAD~N` (revert N commits)
2. **Run Tests**: Ensure original behavior restored
3. **Investigate**: Root cause analysis before retry

Each phase should be a separate PR with:
- Clear commit messages
- Full test coverage
- Review before merge

---

## Success Metrics

| Metric | Before | Target |
|--------|--------|--------|
| `cmd/daemon/main.go` lines | 535 | ~100 |
| Testable daemon logic | 0% | 100% |
| Test coverage (daemon) | N/A | > 80% |
| Duplication score | High | Low |

---

## Next Steps

1. Review this plan with team
2. Create GitHub issues for each phase
3. Schedule implementation (1-2 days)
4. Execute incrementally

**Related Documents**:
- [ADR 005: Daemon Consolidation](adr/005-daemon-consolidation.md)
- [Architecture](ARCHITECTURE.md)
- [Contributing](CONTRIBUTING.md)
