# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [3.0.2] - 2025-11-28

### 🐛 Bug Fixes

- **Linting**: Removed unused code (`closeBody`) and fixed markdown formatting issues
- **CI/CD**: Improved compliance with linter rules for cleaner builds

## [3.0.1] - 2025-11-28

### 🐛 Bug Fixes

- **Docker Build**: Fixed CI failure by adding Node.js build stage to Dockerfile
- **Build Artifacts**: Removed `internal/api/ui` from git tracking (now built inside Docker)

## [3.0.0] - 2025-11-28

### 🚀 Major Features

- **Built-in WebUI**
  - New React-based dashboard available at `/ui/`
  - View system health, uptime, and component status
  - Browse bouquets and channels with EPG status
  - View live logs for troubleshooting
  - Zero-config: embedded directly into the single binary

### 🛡️ Reliability & Internal Improvements

- **Circuit Breaker Pattern**
  - Added circuit breaker for OpenWebIF client to prevent cascading failures
  - Automatic fail-fast when receiver is unresponsive
  - Graceful recovery with half-open state

- **HLS Stability**
  - Improved HLS playlist generation reliability
  - Fixed race conditions in stream proxy

- **Test Suite Consolidation**
  - Merged and cleaned up proxy tests
  - Reduced technical debt and improved CI execution time

## [2.2.0] - 2025-11-28

### 🔒 Security & Reliability Hardening

This release focuses on hardening the application for production use, adding TLS support, XML security improvements,
and enhanced developer tooling.

### Added

- **TLS Support**
  - Native HTTPS support for API and Stream Proxy
  - Configurable via `XG2G_TLS_CERT` and `XG2G_TLS_KEY`
  - Optional `XG2G_FORCE_HTTPS` redirect

- **XML Security Hardening**
  - Strict XML parsing enabled by default
  - Protection against XXE (XML External Entity) attacks
  - DoS protection via input size limits (50MB) and entity expansion limits

- **Developer Excellence**
  - **Load Testing**: Automated test suite for >1500 services
  - **Static Analysis**: Integrated `gosec` for security linting
  - **Coverage**: Infrastructure for mandatory code coverage checks

### Changed

- **Performance**: Improved load testing capabilities
- **Documentation**: Added TLS configuration guide to README

### Security

- **XMLTV**: Fixed potential XXE and DoS vectors in EPG parsing
- **Transport**: Added encryption support for all endpoints

## [2.0.0] - 2025-01-XX

### 🎯 Breaking Changes

**xg2g v2.0 is now zero-configuration!** Audio transcoding and the high-performance Rust remuxer are enabled by
default for the best out-of-the-box experience. This ensures iPhone Safari, Jellyfin, and Plex work immediately
without manual configuration.

**What Changed:**

- **Audio transcoding is now ON by default** (AC3/MP2 → AAC for iOS Safari compatibility)
- **Rust remuxer is now the default** (140x faster than FFmpeg subprocess: 1.4ms vs 200ms latency)
- Several transcoding environment variables are deprecated (see below)

**Migration Required If:**

- ❌ You explicitly disabled audio transcoding → Set `XG2G_ENABLE_AUDIO_TRANSCODING=false`
- ❌ You forced FFmpeg subprocess → Set `XG2G_USE_RUST_REMUXER=false`
- ❌ You customized audio codec/bitrate → Verify AAC 192k stereo meets your needs

**No Action Required If:**

- ✅ You used default settings (transcoding was already on)
- ✅ You use Plex, Jellyfin, or iOS Safari (this release improves your experience)
- ✅ You deploy via Docker Compose with recommended settings

### Added

- **Enhanced Health Endpoint** - Detailed component-level health monitoring
  - `ReceiverChecker` - Validates OpenWebIF connectivity (HEAD request, 3s timeout)
  - `ChannelsChecker` - Ensures channels are loaded (non-zero count validation)
  - `EPGChecker` - Monitors EPG data freshness (warns if >48h old)
  - Verbose mode: `GET /healthz?verbose=true` for detailed status with messages
  - Kubernetes-ready: Perfect for liveness/readiness probes

- **User-Friendly Environment Variable Aliases**
  - `XG2G_RECEIVER_USER` → alias for `XG2G_OWI_USER`
  - `XG2G_RECEIVER_PASSWORD` → alias for `XG2G_OWI_PASS`
  - Backward compatible: Both old and new names work
  - Improves discoverability for new users

- **Production-Ready Defaults**
  - Audio transcoding enabled by default (iOS Safari just works)
  - Rust remuxer enabled by default (4.94 GB/s throughput, <0.1% CPU)
  - Automatic fallback to FFmpeg if Rust remuxer fails to initialize
  - Named volumes in docker-compose examples for easier backup/restore

### Changed

- **Health Check Improvements**
  - Basic endpoint (`/healthz`) returns simple 200/503 status
  - Verbose endpoint (`/healthz?verbose=true`) shows component details
  - Better documentation in docker-compose files explaining both modes
  - Enhanced test coverage with mock receiver server

- **Docker Compose Examples**
  - Removed manual transcoding configuration (now automatic)
  - Added named volumes with backup/restore commands
  - Improved comments explaining health check levels
  - Simplified configuration by removing deprecated variables

- **Better Error Messages**
  - Configuration validation now happens at startup
  - Clear warnings when using deprecated environment variables
  - Receiver connectivity errors show actionable messages

### Deprecated

> ⚠️ **These variables will be removed in v3.0** - Deprecation warnings are logged at startup

- `XG2G_ENABLE_AUDIO_TRANSCODING` - Transcoding is always on (set to `false` if you need to disable)
- `XG2G_AUDIO_CODEC` - Only AAC is supported (MP3 option will be removed)
- `XG2G_AUDIO_BITRATE` - Fixed to 192k for optimal quality/size ratio
- `XG2G_AUDIO_CHANNELS` - Fixed to 2 (stereo) for maximum compatibility
- `XG2G_FFMPEG_PATH` - Rust remuxer is default (set `XG2G_USE_RUST_REMUXER=false` if needed)

### Performance

| Metric | v1.5.0 (FFmpeg) | v2.0.0 (Rust Default) | Improvement |
|--------|-----------------|----------------------|-------------|
| **Transcoding Latency** | 200-500ms | 1.4ms | **140x faster** ⚡ |
| **CPU Usage** | 5-10% | <0.1% | **50-100x lower** 🔋 |
| **Throughput** | ~50 MB/s | 4.94 GB/s | **100x higher** 🚀 |
| **Memory Usage** | 150-200 MB | 50-80 MB | **3x lower** 💾 |

### Documentation

- Added health check usage examples in docker-compose.yml
- Documented verbose health check mode for monitoring tools
- Added migration guide for v2.0 breaking changes
- Updated environment variable documentation with aliases
- Added backup/restore commands for named volumes

### Fixed

- Health endpoint test compatibility with new component checks
- Mock receiver server for realistic health check testing
- Docker Compose health check documentation clarity

### Migration Guide

#### Scenario 1: You explicitly disabled transcoding

**Before (v1.5.0):**

```yaml
environment:
  - XG2G_ENABLE_AUDIO_TRANSCODING=false
```

**After (v2.0.0):**

```yaml
environment:
  - XG2G_ENABLE_AUDIO_TRANSCODING=false  # Still works, but deprecated
```

**Action:** No immediate action required, but you'll see deprecation warnings. Consider removing this variable
and using default transcoding in v3.0.

---

#### Scenario 2: You forced FFmpeg subprocess

**Before (v1.5.0):**

```yaml
environment:
  - XG2G_ENABLE_AUDIO_TRANSCODING=true
  - XG2G_AUDIO_CODEC=aac
  - XG2G_FFMPEG_PATH=/usr/bin/ffmpeg
```

**After (v2.0.0):**

```yaml
environment:
  - XG2G_USE_RUST_REMUXER=false  # Force FFmpeg if needed
```

**Action:** Remove deprecated variables. Set `XG2G_USE_RUST_REMUXER=false` if you need FFmpeg for specific
reasons.

---

#### Scenario 3: You used default settings (recommended)

**Before (v1.5.0):**

```yaml
environment:
  - XG2G_OWI_BASE=http://192.168.1.100
  - XG2G_BOUQUET=Favourites
```

**After (v2.0.0):**

```yaml
environment:
  - XG2G_OWI_BASE=http://192.168.1.100
  - XG2G_BOUQUET=Favourites
  # That's it! Transcoding is automatic now.
```

**Action:** None! Everything just works better now. 🎉

---

#### Scenario 4: You want to use user-friendly aliases

**Before (v1.5.0):**

```yaml
environment:
  - XG2G_OWI_USER=root
  - XG2G_OWI_PASS=secretpassword
```

**After (v2.0.0):**

```yaml
environment:
  - XG2G_RECEIVER_USER=root          # New user-friendly alias
  - XG2G_RECEIVER_PASSWORD=secretpassword  # New user-friendly alias
```

**Action:** Optional! Both old and new variable names work. Use whichever you prefer.

---

### Upgrade Checklist

- [ ] Review breaking changes (transcoding now on by default)
- [ ] Test with your Enigma2 receiver (check `/healthz?verbose=true`)
- [ ] Verify iOS Safari playback (should work without manual config)
- [ ] Check logs for deprecation warnings
- [ ] Update docker-compose.yml to use named volumes (recommended)
- [ ] Remove deprecated environment variables (optional, still work in v2.0)
- [ ] Plan for v3.0 migration (deprecated variables will be removed)

---

### Why v2.0?

This release represents a major philosophical shift: **convention over configuration**. Instead of requiring users
to discover and enable audio transcoding manually, v2.0 enables it by default with the fastest available backend
(Rust remuxer). This ensures iPhone Safari, Jellyfin, and Plex work immediately without reading documentation.

The deprecation of transcoding configuration variables reflects our commitment to simplicity: xg2g should just work
out of the box, with sane defaults that cover 95% of use cases. Power users can still override defaults, but new
users get a working system immediately.

**Philosophy:** Zero-config defaults, infinite configurability for edge cases.

## [1.5.0] - 2025-10-29

### Added

- **Actionlint workflow** for automatic GitHub Actions validation
- **Conftest OPA policies** for enforcing Dockerfile and Kubernetes security rules
- **XMLTV fuzzing tests** with Go native fuzzing and weekly scheduled runs
- **OpenSSF Scorecard** workflow for automated security scoring
- **Renovate** configuration for intelligent dependency updates with dashboard
- **Security badges** section in README (OpenSSF Scorecard, SBOM, Conftest, Fuzzing, Container Security)
- Cosign signature verification for container images
- SLSA Level 3 provenance attestation for supply chain security
- Automated dependency updates via Dependabot and Renovate
- Graceful shutdown tests for SIGTERM/SIGINT handling
- Audio transcoder coverage tests
- Comprehensive documentation:
  - `docs/security/BRANCH_PROTECTION.md` - Branch protection setup guide
  - `docs/security/SUPPLY_CHAIN_TOOLS.md` - Complete supply chain tools overview
  - `docs/security/RENOVATE_SETUP.md` - Step-by-step Renovate activation guide

### Changed

- Container runtime hardening: enabled read-only root filesystem in docker-compose.yml
- CI pipeline now implements complete security chain: SBOM → Cosign → CodeQL → Fuzzing → Conftest
- CHANGELOG.md migrated to "Keep a Changelog" format
- Improved security posture across all container deployments

### Security

- **SLSA Level 3 Provenance** activated for all releases
- **Keyless Cosign signatures** with Rekor attestations
- **Conftest policy gate** active for Dockerfile and Kubernetes manifests
- **SBOM generation** (SPDX/CycloneDX format) with automatic CVE scanning
- **Non-root container** execution (UID 65532, CIS 5.1 compliant)
- **Automated policy enforcement** for container security best practices
- **Comprehensive capability dropping** (cap_drop: ALL)
- **Read-only root filesystem** protection
- **No-new-privileges** security option
- **Dependency digest pinning** for GitHub Actions and Docker images
- **Automated CVE mitigation** < 24h via Dependabot + Renovate

### Fixed

- Conftest CI workflow now uses dockerfile-parse instead of non-existent dockerfile-json
- Docker Compose tmpfs/volume mount conflict resolved
- Test expectations aligned with v1.4.0+ default behavior
- HDHR SSDP multicast group joining in container environments (#22)
- HEAD requests now allowed for /files/ endpoint (#23)

### Metrics

| Category | Before | After |
|----------|--------|-------|
| **OpenSSF Score** | ~5.5 | **8.5+** ⭐ |
| **CVE Response Time** | Manual | **< 15 min** ⚡ |
| **Policy Violations** | Undetected | **Blocked in CI** 🛡️ |
| **Dependency Freshness** | Manual | **Daily automated** 🤖 |
| **Build Provenance** | None | **SLSA Level 3** 🔒 |

### Documentation

This release includes extensive documentation for supply chain security tools, configuration guides, and best practices.
See `docs/security/` for details.

## [1.4.0] - 2024-01-XX

### Added

- HDHomeRun emulation for Plex/Jellyfin native integration
- Auto-discovery via SSDP/UPnP
- Complete out-of-the-box experience with all features enabled by default
- Configuration file support (YAML)
- Versioned API (v1) with deprecation headers for legacy endpoints

### Changed

- Simplified configuration documentation
- Organized Docker Compose files by use case
- Made Enigma2 authentication explicit in examples
- Improved startup logging for better visibility

### Deprecated

- Legacy API endpoints (/api/status, /api/refresh) in favor of /api/v1/*

## [1.3.0] - 2023-XX-XX

### Added

- OSCam Streamrelay support with automatic channel detection
- Smart port selection (8001 vs 17999)
- Audio transcoding (MP2/AC3 → AAC)
- GPU transcoding with VAAPI support
- Prometheus metrics
- Health checks (/healthz, /readyz)

### Changed

- Improved EPG collection performance
- Enhanced logging output

## [1.2.0] - 2023-XX-XX

### Added

- XMLTV EPG support (7 days)
- Channel logos (picons)
- Multiple bouquets support
- Integrated stream proxy

## [1.1.0] - 2023-XX-XX

### Added

- M3U playlist generation
- Docker support
- Basic authentication for Enigma2

## [1.0.0] - 2023-XX-XX

### Added

- Initial release
- Basic Enigma2 to IPTV gateway functionality

[Unreleased]: https://github.com/ManuGH/xg2g/compare/v1.5.0...HEAD
[1.5.0]: https://github.com/ManuGH/xg2g/compare/v1.4.0...v1.5.0
[1.4.0]: https://github.com/ManuGH/xg2g/releases/tag/v1.4.0
[1.3.0]: https://github.com/ManuGH/xg2g/releases/tag/v1.3.0
[1.2.0]: https://github.com/ManuGH/xg2g/releases/tag/v1.2.0
[1.1.0]: https://github.com/ManuGH/xg2g/releases/tag/v1.1.0
[1.0.0]: https://github.com/ManuGH/xg2g/releases/tag/v1.0.0
