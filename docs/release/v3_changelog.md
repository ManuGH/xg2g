# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Universal Delivery Policy**: Single tier-1 compliant stream format (H.264/AAC/fMP4/HLS) for all clients.
- **Fail-Start Protection**: Application actively refuses to start if legacy configuration (`XG2G_STREAM_PROFILE`) is detected.
- **Docs**: Comprehensive "Hard Reset" of documentation (`README.md`, `ARCHITECTURE.md`).

### Changed

- **WebUI**: Removed all profile selection logic, auto-switching, and profile persistence. Player is now a true Thin Client.
- **Streaming**: FFmpeg pipeline now rigidly enforces H.264/AAC/fMP4. Hardware acceleration is auto-detected but codec parameters are fixed.
- **Config**: `streaming.delivery_policy` replaces profile lists.

### Removed

- **Streaming Profiles**: `auto`, `safari`, `safari_hevc_hw`, `native` profiles are gone.
- **Legacy API**: `profileID` and `profile` fields removed from OpenAPI spec and internal Go types.
- **Environment**: `XG2G_STREAM_PROFILE` env var support.

### Fixed

- **Authentication**: Strict auth enforcement on all endpoints (fail-closed).
- **Concurrency**: Fixed race conditions in session creation (removed profile-based retry loops).
