# Release Notes v3.9.0

## One-line highlight

`xg2g v3.9.0 introduces guided Linux installation automation, 2026 monorepo layout purity, route-policy deadline enforcement, and portable dedicated DVR scratch storage.`

## Why this release matters

This release delivers a beginner-friendly, guided Linux setup wizard (`setup-linux.sh`) and a comprehensive installation guide (`INSTALLATION.md`), making it simple to deploy xg2g with systemd, Docker, VAAPI/NVENC hardware acceleration, and reverse proxies (Caddy/Nginx). It also modernizes the repository topology into canonical `apps/` and `infra/` structures with root purity enforcement.

## What changed

### Guided Linux Setup & Installation
- **Setup Wizard:** Added interactive `infra/systemd/setup-linux.sh` for guided receiver credentials, storage mounts, GPU selection, and HTTPS reverse proxy setup.
- **Complete Guide:** Added `docs/guides/INSTALLATION.md` covering pre-flight requirements, Docker/systemd setup, SHA-256 release validation, backup/restore, and uninstallation.

### Streaming and Hardening
- **Response Deadlines:** Enforced route-aware response deadlines with full preservation of v3 auth/scope parity.
- **DVR Scratch Storage:** Supported portable dedicated DVR scratch mounts with fail-closed capacity reporting.
- **Security & Preflight:** Hardened proxy-aware rate limiting, CORS preflight handling, and dependency vulnerability checks.

### Operations and Monorepo Structure
- **Topology Migration:** Restructured repository layout into `apps/webui` and `infra/` (`infra/docker`, `infra/systemd`, `infra/monitoring`).
- **Root Purity Gate:** Enforced strict zero-clutter root purity in CI via `ci_gate_root_purity.sh`.

## Upgrade notes

- Existing systemd and Docker Compose deployments remain fully compatible and upgrade seamlessly via `sync.sh` or `compose.dev.yaml`.
- The new `setup-linux.sh` wizard is optional for existing installations and can be run to reconfigure settings at any time.

## Breaking changes

- `None`. All API contracts, environment variables, and config surfaces remain 100% backwards-compatible.

## Quick links

- Docker image: `ghcr.io/manugh/xg2g:v3.9.0`
- Installation Guide: <https://github.com/ManuGH/xg2g/blob/main/docs/guides/INSTALLATION.md>
- Configuration Guide: <https://github.com/ManuGH/xg2g/blob/main/docs/guides/CONFIGURATION.md>
- API Reference: <https://manugh.github.io/xg2g/>
