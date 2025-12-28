# v3.1.0 Release Notes

## Recording Playback Enhancement

### Added

- **Local-first recording playback** with automatic receiver fallback
  - Configure receiver→local path mappings for zero-network-overhead playback
  - File stability gate prevents streaming recordings being written
  - Graceful fallback to Receiver HTTP when local file unavailable or unstable
  - **Enigma2 ServiceRef recording references are now supported via automatic
    path extraction** (enables local playback with real Enigma2 recordings)

- **New configuration options**:
  - `XG2G_RECORDINGS_POLICY`: Playback policy (`auto`, `local_only`,
    `receiver_only`)
  - `XG2G_RECORDINGS_STABLE_WINDOW`: File stability check duration
    (default: `2s`)
  - `XG2G_RECORDINGS_MAP`: Receiver→local path mappings (semicolon-separated)

- **Observability**: All playback decisions logged with source type
  - `source_type=local`: Using local filesystem
  - `source_type=receiver`: Using Receiver HTTP stream

- **Documentation**:
  - Comprehensive configuration guide with NAS, multi-location, and Docker
    examples
  - Path mapping rules and security validation details
  - Stability window tuning guidance for different storage types
  - Troubleshooting guide for common deployment scenarios
  - Smoke test scenarios with expected log outputs

### Fixed

- Enigma2 service reference format (`1:0:0:0:0:0:0:0:0:0:/path`) is now
  correctly parsed to extract filesystem paths for local playback mapping
  - Path extraction is automatic and transparent
  - No manual transformation required
  - Defensive: handles edge cases (no colon, relative paths, empty strings)

### Security

- Path mapping validation with traversal blocking and normalization
- Absolute path requirements for receiver and local roots
- Longest-prefix-first matching to prevent path collision attacks

### Performance

- Zero network overhead for local playback (direct file access)
- Lower latency compared to Receiver HTTP streaming
- Configurable stability window balances safety vs. playback start speed

### Backward Compatibility

- Default behavior unchanged: recordings use Receiver HTTP when feature
  unconfigured
- No breaking changes to existing deployments
- Optional feature: enable only when path mappings configured

### Test Coverage

- 43 unit tests across PathMapper, Stability, and ServiceRef extraction
  - PathMapper: 22 tests (security, edge cases, normalization)
  - Stability: 7 tests (stable files, writing files, edge cases)
  - ServiceRef: 14 tests (Enigma2 formats, special characters, Unicode)
- 6 smoke test scenarios documented with expected outputs
- Production-verified with real Enigma2 recordings

### Migration Guide

No migration required. The feature is opt-in via configuration:

**Enable local-first playback** by adding path mappings to your `.env` or
`config.yaml`:

```bash
# Environment variables
XG2G_RECORDINGS_MAP=/media/hdd/movie=/mnt/recordings
XG2G_RECORDINGS_STABLE_WINDOW=2s
```

Or in YAML:

```yaml
recording_playback:
  playback_policy: auto
  stable_window: 2s
  mappings:
    - receiver_root: /media/hdd/movie
      local_root: /mnt/recordings
```

See [Configuration Guide](docs/guides/CONFIGURATION.md#recording-playback) for
detailed setup instructions.

---

**Release Date**: TBD
**Contributors**: Claude Sonnet 4.5, ManuGH
