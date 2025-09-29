# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Comprehensive retry and timeout configuration for OpenWebIF requests
- Exponential backoff with configurable limits
- Prometheus metrics for request latencies, retry counts, and error classification
- Context cancellation support with proper cleanup
- English-only policy for all project communication
- GitHub issue and pull request templates
- Comprehensive test suite for configuration validation and retry scenarios

### Changed

- **BEHAVIOR CHANGE**: Request retry logic now uses exponential backoff (previously immediate retry)
- **BEHAVIOR CHANGE**: Default timeout increased from 5s to 10s per request attempt
- **BEHAVIOR CHANGE**: Maximum retry attempts reduced from unlimited to 3 by default
- Improved structured logging with consistent field names (`attempt`, `duration_ms`, `error_class`)
- Enhanced CI pipeline with markdownlint and comprehensive quality checks

### Fixed

- Context cancellation now properly prevents goroutine leaks
- Race conditions in concurrent request handling
- Code quality issues (go vet lostcancel warnings, formatting)

### Configuration

New environment variables for stability tuning:

- `XG2G_OWI_TIMEOUT_MS` (default: 10000) - Timeout per request attempt
- `XG2G_OWI_RETRIES` (default: 3) - Maximum retry attempts  
- `XG2G_OWI_BACKOFF_MS` (default: 500) - Base backoff delay
- `XG2G_OWI_MAX_BACKOFF_MS` (default: 2000) - Maximum backoff delay

### Migration Guide

If you're upgrading from a previous version:

1. **Timeout Changes**: The new default timeout is 10s per attempt (previously 5s total). If your receiver is very fast, consider setting `XG2G_OWI_TIMEOUT_MS=5000`

2. **Retry Behavior**: Retries now use exponential backoff. If you have a very slow receiver, you may need to increase `XG2G_OWI_MAX_BACKOFF_MS=5000`

3. **Monitoring**: New Prometheus metrics are available. Update your dashboards to include:
   - `xg2g_openwebif_request_duration_seconds` for latency percentiles
   - `xg2g_openwebif_request_retries_total` for retry rates
   - `xg2g_openwebif_request_failures_total` for error monitoring

## Previous Releases

See git history for changes prior to this changelog.
