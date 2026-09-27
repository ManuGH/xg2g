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

## Documentation Structure (Diátaxis Framework)

The **xg2g** documentation is systematically organized according to the [Diátaxis framework](https://diataxis.fr/) across four distinct operational quadrants:

```
                  PRACTICAL
                     ▲
                     │
    [How-To Guides]  │  [Tutorials]
    Problem-oriented │  Learning-oriented
    docs/how-to/     │  docs/tutorials/
                     │
◄────────────────────┼────────────────────►
WORK                 │                 STUDY
                     │
    [Reference]      │  [Explanation]
    Information-     │  Understanding-
    oriented         │  oriented
    docs/reference/  │  docs/explanation/
                     │
                     ▼
                THEORETICAL
```

### 1. 🎓 [Tutorials](tutorials/README.md) (Learning-Oriented)

Step-by-step guidance for beginners to achieve their first success:
- [**Getting Started with xg2g**](guides/GETTING_STARTED.md): Setup, linking receiver via OpenWebIf, and first stream playback.
- [**Local Development Environment**](guides/DEVELOPMENT.md): Toolchain setup, building backend, running WebUI, mock tuner twin.

### 2. 🛠️ [How-To Guides](how-to/README.md) (Problem-Oriented)

Practical recipes to solve concrete operational and administrative tasks:
- [**Linux Host Installation**](guides/INSTALLATION.md): Automated installer (`setup-linux.sh`), systemd unit setup, and reverse proxying.
- [**Production Deployment**](ops/DEPLOYMENT.md): Docker Compose orchestration, container limits, storage mounts, and auto-restart policies.
- [**Maintainer Incident Triage**](ops/RUNBOOK_SYSTEMD_COMPOSE.md): Step-by-step triage sequence for receiver stalls, tuner starvation, and playback lockups.
- [**Troubleshooting & Diagnostics**](guides/TROUBLESHOOTING.md): Diagnostics, `xg2g-admin doctor`, and common error states.
- [**Security Hardening**](ops/SECURITY.md): Auth tokens, session secret, TLS proxy setup, and security model.

### 3. 📖 [Reference](reference/README.md) (Information-Oriented)

Authoritative, complete descriptions of configuration surfaces, schemas, and reason codes:
- [**Configuration Guide**](guides/CONFIGURATION.md): Environment variables (`xg2g.env`) and configuration parameters.
- [**Config Surfaces Inventory**](guides/CONFIG_SURFACES.md): Generated inventory mapping every code reference to configuration keys.
- [**Configuration Schema**](guides/config.schema.json): Machine-readable JSON Schema for runtime configuration validation.
- [**Lease Reason Code Matrix**](ADR/lease_reason_matrix.md): Formally audited tuner lease reason codes and preemption states.
- [**Client Profiles Catalog**](ops/CLIENT_PROFILES.md): Client capabilities, browser probes, and codec fallback rules.

### 4. 🧠 [Explanation](explanation/README.md) (Understanding-Oriented)

Architectural principles, system models, design rationale, and technical background:
- [**2026 System Overview**](arch/SYSTEM_OVERVIEW_2026.md): Architecture, network layout, storage model, and system invariants.
- [**Engineering Charter**](ENGINEERING_CHARTER.md): Governing system principles, invariants, and review gate.
- [**Architecture Invariants (ADR-005)**](ADR/005-Architecture-Invariants.md): Hard non-negotiables for package layering, failure isolation, and hermetic execution.
- [**Concurrency Manifest (ADR-006)**](ADR/006-Concurrency-Manifest.md): Concurrency hierarchy, mutex ordering, and goroutine leak prevention across media pipelines.
- [**Composite Lease Model (ADR-029)**](ADR/029-resource-arbitration-composite-lease-model.md): Multi-resource composite tuner lease model, state machine, and conflict resolution.
- [**Lease Reconciliation (ADR-030)**](ADR/030-lease-reconciliation.md): 3-phase reconciliation engine (Snapshot, Deterministic Analysis, Bounded Remediation) and startup gates.
- [**Codec & Container Matrix**](arch/CODEC_MATRIX.md): FFmpeg remux & transcode logic, hardware acceleration (VAAPI/NVENC).
- [**ADR Catalog**](ADR/README.md): Architecture Decision Records and technical rationale.
- [**WebUI Architecture**](webui/README.md): React frontend layout, state management, and player telemetry.
- [**Agentic Tooling & Xcode Runbook**](../AGENTS.md#ios-client--agentic-xcode-tooling-truth): Xcode MCP Server, stdio bridge, and Device Hub automation.
- [**Scanner Governance**](SCANNER_GOVERNANCE.md): Static checks, Gitleaks, CodeQL, and CI rules.

---

## Maintenance & Drift Policy

- Category `README.md` files serve strictly as navigation hubs. Put normative rules in dedicated architecture or ops docs.
- The root [`README.md`](../README.md) is compiled from [`backend/templates/README.md.tmpl`](../backend/templates/README.md.tmpl) via `./backend/scripts/render-docs.sh`.
- Run `./backend/scripts/render-docs.sh` and `git diff --check` before committing doc updates.
