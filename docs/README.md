# xg2g Documentation

Central index for **xg2g** documentation and architecture specs.

For system architecture, network topology, storage contracts, and API guarantees, read the [2026 System Overview](arch/SYSTEM_OVERVIEW_2026.md).

---

## Quickstart & Local Dev

```bash
# Start local dev environment
make dev

# Fast-track deploy staging to LXC 110
./scripts/fast_deploy.sh

# Run PR verification gates
make ci-pr
```

---

## Documentation Index

### 1. User & Operations Guides

| Document | Description |
| :--- | :--- |
| [**Getting Started**](guides/GETTING_STARTED.md) | Setup, linking receiver via OpenWebIf, and first stream playback. |
| [**Linux Installation**](guides/INSTALLATION.md) | Automated installer (`setup-linux.sh`), systemd unit setup, and reverse proxying. |
| [**Configuration Guide**](guides/CONFIGURATION.md) | Environment variables (`xg2g.env`) and configuration parameters. |
| [**Troubleshooting**](guides/TROUBLESHOOTING.md) | Diagnostics, `xg2g-admin doctor`, and common error states. |

---

### 2. Architecture & Operations

| Document | Description |
| :--- | :--- |
| [**2026 System Overview**](arch/SYSTEM_OVERVIEW_2026.md) | Architecture, network layout, storage model, and system invariants. |
| [**Deployment Guide**](ops/DEPLOYMENT.md) | Docker Compose, systemd unit supervision, and updates. |
| [**Security Operations**](ops/SECURITY.md) | Auth tokens, session secret, TLS proxy setup, and security model. |
| [**Client Profiles**](ops/CLIENT_PROFILES.md) | Client capabilities, browser probes, and codec fallback rules. |

---

### 3. Maintainer Reference

| Document | Description |
| :--- | :--- |
| [**Repository Map**](dev/REPO_MAP.md) | Codebase layout, modules, and file responsibilities. |
| [**Codec & Container Matrix**](arch/CODEC_MATRIX.md) | FFmpeg remux & transcode logic, hardware acceleration (VAAPI/NVENC). |
| [**WebUI Architecture**](webui/README.md) | React frontend layout, state management, and player telemetry. |
| [**ADR Index**](ADR/README.md) | Architecture Decision Records and technical rationale. |
| [**Scanner Governance**](SCANNER_GOVERNANCE.md) | Static checks, Gitleaks, CodeQL, and CI rules. |

---

## Maintenance & Drift Policy

- Category `README.md` files serve strictly as navigation hubs. Put normative rules in dedicated architecture or ops docs.
- The root [`README.md`](../README.md) is compiled from [`backend/templates/README.md.tmpl`](../backend/templates/README.md.tmpl) via `./backend/scripts/render-docs.sh`.
- Run `./backend/scripts/render-docs.sh` and `git diff --check` before committing doc updates.
