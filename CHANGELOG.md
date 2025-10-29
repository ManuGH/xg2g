# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- OpenSSF Scorecard workflow for automated security scoring
- Renovate configuration for intelligent dependency updates
- Actionlint workflow for GitHub Actions validation
- Conftest OPA policies for Dockerfile and Kubernetes security enforcement
- XMLTV fuzzing tests with weekly scheduled runs
- Cosign signature verification for container images
- SLSA provenance attestation for supply chain security
- Automated dependency updates via Dependabot and Renovate
- Graceful shutdown tests for SIGTERM/SIGINT handling
- Audio transcoder coverage tests
- Comprehensive supply chain security tools documentation
- Security badge section in README with OpenSSF Scorecard, SBOM, Conftest, Fuzzing, and Container Security badges
- Complete Renovate activation and usage guide

### Changed
- Container runtime hardening: enabled read-only root filesystem in docker-compose.yml
- Removed conflicting tmpfs mount for /data to allow persistent storage

### Security
- Automated policy enforcement for container security best practices
- Non-root container execution (UID 65532)
- Comprehensive capability dropping (cap_drop: ALL)
- Read-only root filesystem protection
- No-new-privileges security option
- Dependency digest pinning for GitHub Actions and Docker images

### Fixed
- Conftest CI workflow now uses dockerfile-parse instead of non-existent dockerfile-json
- Docker Compose tmpfs/volume mount conflict resolved
- Test expectations aligned with v1.4.0+ default behavior
- HDHR SSDP multicast group joining in container environments (#22)
- HEAD requests now allowed for /files/ endpoint (#23)

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

[Unreleased]: https://github.com/ManuGH/xg2g/compare/v1.4.0...HEAD
[1.4.0]: https://github.com/ManuGH/xg2g/releases/tag/v1.4.0
[1.3.0]: https://github.com/ManuGH/xg2g/releases/tag/v1.3.0
[1.2.0]: https://github.com/ManuGH/xg2g/releases/tag/v1.2.0
[1.1.0]: https://github.com/ManuGH/xg2g/releases/tag/v1.1.0
[1.0.0]: https://github.com/ManuGH/xg2g/releases/tag/v1.0.0
