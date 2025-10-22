# ADR-002: Configuration Precedence Model

**Status**: Accepted
**Date**: 2025-01-21
**Deciders**: Development Team
**Technical Story**: Priority 2 Implementation

## Context and Problem Statement

xg2g requires configuration from multiple sources: environment variables (for containers), YAML files (for complex setups), and default values (for simplicity). Without a clear precedence model, configuration becomes unpredictable and difficult to debug.

Users need to:
- Override production defaults with environment variables in Docker/Kubernetes
- Use YAML files for complex multi-instance deployments
- Have sensible defaults for quick starts

## Decision Drivers

- **Docker/Kubernetes Compatibility**: ENV vars are standard in containerized environments
- **Developer Experience**: YAML files are easier for complex configurations
- **12-Factor App Compliance**: Configuration should come from environment
- **Predictability**: Clear precedence rules prevent configuration conflicts
- **Debuggability**: Users should know which config source "wins"

## Considered Options

1. **ENV > File > Defaults** (Highest to Lowest)
2. **File > ENV > Defaults**
3. **Defaults > File > ENV**
4. **Single Source Only** (ENV only or File only)

## Decision Outcome

**Chosen option**: "ENV > File > Defaults"

### Rationale

This precedence model follows the **12-Factor App** methodology and Docker/Kubernetes best practices:

1. **Environment Variables (Highest Priority)**: Allow runtime overrides without rebuilding images
2. **Configuration Files (Medium Priority)**: Enable complex setups with structured data
3. **Default Values (Lowest Priority)**: Provide sensible fallbacks for common cases

### Positive Consequences

- **Container-Friendly**: Docker `--env` and Kubernetes ConfigMaps work seamlessly
- **Flexible Deployments**: Dev uses defaults, staging uses files, prod uses ENV overrides
- **No Surprises**: Explicit precedence prevents "config mystery bugs"
- **Easy Testing**: Tests can override config with ENV without touching files

### Negative Consequences

- **Documentation Burden**: Users must understand precedence to debug config issues
- **Potential Confusion**: ENV overriding file may surprise users expecting file to win

## Pros and Cons of the Options

### ENV > File > Defaults

- **Good**, because aligns with 12-Factor App and Docker best practices
- **Good**, because allows runtime overrides without file changes
- **Good**, because Kubernetes ConfigMaps naturally become ENV vars
- **Bad**, because ENV vars can't represent complex nested structures well

### File > ENV > Defaults

- **Good**, because files are more explicit and version-controlled
- **Bad**, because contradicts Docker/Kubernetes conventions
- **Bad**, because requires file changes to override config (not runtime-friendly)

### Defaults > File > ENV

- **Bad**, because defaults would override user configuration (nonsensical)

### Single Source Only

- **Good**, because simplest to understand
- **Bad**, because inflexible (can't mix ENV and file config)
- **Bad**, because forces all-or-nothing approach

## Implementation

### Precedence Model

```
┌─────────────────────────────────────┐
│   1. Environment Variables          │  ← Highest Priority
│      XG2G_OWI_BASE=http://...       │
└─────────────────────────────────────┘
           ↓ (if not set)
┌─────────────────────────────────────┐
│   2. Configuration File (YAML)      │  ← Medium Priority
│      owi_base: http://...           │
└─────────────────────────────────────┘
           ↓ (if not set)
┌─────────────────────────────────────┐
│   3. Default Values (Code)          │  ← Lowest Priority
│      DefaultOWIBase = "..."         │
└─────────────────────────────────────┘
```

### Code Example

```go
// internal/config/config.go
func Load() (jobs.Config, error) {
    cfg := jobs.Config{
        // 1. Start with defaults
        StreamPort: 8001,
        DataDir:    "/tmp",
    }

    // 2. Load from file (overrides defaults)
    if fileExists(configPath) {
        fileCfg, err := loadYAML(configPath)
        if err != nil {
            return cfg, err
        }
        cfg = mergeConfig(cfg, fileCfg)
    }

    // 3. Load from ENV (overrides file and defaults)
    if owiBase := os.Getenv("XG2G_OWI_BASE"); owiBase != "" {
        cfg.OWIBase = owiBase
    }

    return cfg, nil
}
```

### Environment Variable Expansion in YAML

```yaml
# config.yaml
owi_base: ${OWI_BASE:-http://192.168.1.100}  # Uses ENV or default
stream_port: ${STREAM_PORT:-8001}
```

### Configuration Debugging

```bash
# Show effective configuration with precedence indicators
$ xg2g config show
owi_base: http://192.168.1.200     [ENV]
stream_port: 8001                  [DEFAULT]
data_dir: /data                    [FILE]
bouquet: Favourites                [FILE]
```

### Migration Path

**Phase 1 (Current)**: Implement precedence model
- ENV > File > Defaults
- No breaking changes (ENV-only users unaffected)

**Phase 2 (Future)**: Enhance debugging
- Add `--config-source` flag to show config origins
- Log warnings for conflicting config sources

## Testing

```go
func TestConfigPrecedence(t *testing.T) {
    // Setup: File has port 9000, ENV has port 7000, default is 8001
    os.Setenv("XG2G_STREAM_PORT", "7000")
    defer os.Unsetenv("XG2G_STREAM_PORT")

    cfg, err := config.Load()
    if err != nil {
        t.Fatal(err)
    }

    // ENV should win
    if cfg.StreamPort != 7000 {
        t.Errorf("Expected ENV to override file: got %d, want 7000", cfg.StreamPort)
    }
}
```

## Links

- [The Twelve-Factor App - Config](https://12factor.net/config)
- [Kubernetes ConfigMaps](https://kubernetes.io/docs/concepts/configuration/configmap/)
- [Docker Environment Variables](https://docs.docker.com/compose/environment-variables/)
- Implementation: `internal/config/config.go`
- Related: ADR-003 (Validation Package) - validates merged config

## Notes

### Lessons Learned

1. **ENV Expansion in YAML**: `${VAR:-default}` syntax provides flexibility
2. **Validation After Merge**: Always validate the final merged config, not individual sources
3. **Clear Documentation**: Precedence must be documented prominently in README

### Future Considerations

1. **Config Reload**: Hot-reload YAML config without restart (watch file changes)
2. **Remote Config**: Support etcd/Consul for distributed config (would be highest priority)
3. **Secret Management**: Integrate with Vault/K8s Secrets for sensitive values
4. **Schema Validation**: JSON Schema or similar for YAML validation before merge

### Common Pitfalls

**Pitfall 1**: User sets value in YAML, expects it to be used, but ENV override exists
- **Solution**: Log warnings when ENV overrides file values

**Pitfall 2**: Complex nested config in ENV (hard to express)
- **Solution**: Use YAML for complex structures, ENV only for simple overrides

**Pitfall 3**: Config changes not taking effect
- **Solution**: Provide `config validate` and `config show` commands for debugging
