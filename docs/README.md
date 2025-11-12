# xg2g Documentation

Welcome to the xg2g documentation. This directory contains comprehensive documentation for the xg2g transcoding proxy.

## Quick Links

- [Architecture Overview](ARCHITECTURE.md) - System design and component architecture
- [Changelog](CHANGELOG.md) - Version history and release notes

## Documentation Structure

### 📋 Operations
Production deployment, monitoring, and incident management.
- [Production Deployment](operations/PRODUCTION.md)
- [Docker Deployment](operations/DOCKER_DEPLOYMENT.md)
- [Health Checks](operations/HEALTH_CHECKS.md)
- [Backup & Restore](operations/BACKUP_RESTORE.md)
- [Incident Runbook](operations/RUNBOOK_INCIDENT_ROLLBACK.md)
- [Production Test Results](operations/PRODUCTION_TEST_RESULTS.md)
- [Real Stream Tests](operations/REAL_STREAM_TEST_RESULTS.md)
- [CI Load Testing](operations/CI_LOAD_TESTING.md)

### 🔧 Development
Contributing guidelines, testing strategies, and code quality.
- [Contributing Guide](development/CONTRIBUTING.md)
- [Testing Strategy](development/TESTING_STRATEGY.md)
- [Quality Checks](development/QUALITY_CHECKS.md)
- [Coverage Setup](development/COVERAGE_SETUP.md)
- [Coverage Operations](development/COVERAGE_OPERATIONS.md)
- [Code Review](development/CODE_REVIEW_2025-10-22.md)
- [CI/CD Pipeline](development/ci-cd.md)
- [CI/CD Audit](development/CI_CD_AUDIT_REPORT.md)

### 🏗️ Architecture
System architecture, design decisions, and technical specifications.
- [Architecture Decisions (ADR)](adr/) - Architectural Decision Records
- [Go + Rust Hybrid](architecture/GO_RUST_HYBRID.md)
- [Single Binary FFI](architecture/SINGLE_BINARY_FFI.md)
- [Audio Codec Research](architecture/AUDIO_CODEC_RESEARCH.md)
- [FFI Integration](architecture/FFI_INTEGRATION.md)
- [FFI Testing](architecture/FFI_TESTING.md)
- [Rust Remuxer Integration](architecture/RUST_REMUXER_INTEGRATION.md)

### 🚀 Features
Feature documentation and implementation guides.
- [GPU Transcoding](features/GPU_TRANSCODING.md)
- [Audio Transcoding](features/AUDIO_TRANSCODING.md)
- [EPG Optimization](features/EPG_OPTIMIZATION.md)
- [CPU Optimizations](features/CPU_OPTIMIZATIONS.md)
- [Audio Delay Fix](features/AUDIO_DELAY_FIX.md)

### 📚 Guides
User guides and configuration references.
- [Configuration Guide](guides/CONFIGURATION.md)
- [Default Configuration](guides/DEFAULT_CONFIGURATION.md)
- [Configuration Schema](guides/config.schema.json)
- [Config Reference](guides/config.md)
- [Migrations](guides/MIGRATIONS.md)
- [Stream Ports](guides/STREAM_PORTS.md)
- [Stream Proxy Routing](guides/STREAM_PROXY_ROUTING.md)
- [Threadfin Integration](guides/THREADFIN.md)
- [Advanced Usage](guides/ADVANCED.md)
- [OIDC Integration](guides/OIDC_INTEGRATION.md)
- [Telemetry](guides/telemetry.md)
- [Telemetry Quickstart](guides/telemetry-quickstart.md)

### 🔐 Security
Security documentation, audits, and hardening guides.
- [Security Policy](security/SECURITY.md)
- [Security Hardening](security/SECURITY_HARDENING.md)
- [Security Improvements](security/SECURITY_IMPROVEMENTS.md)
- [Security Audit Checklist](security/SECURITY_AUDIT_CHECKLIST.md)
- [GitHub Actions Security](security/GITHUB_ACTIONS_SECURITY.md)
- [Branch Protection](security/BRANCH_PROTECTION.md)
- [Renovate Setup](security/RENOVATE_SETUP.md)
- [Scorecard Improvements](security/SCORECARD_IMPROVEMENTS.md)
- [Supply Chain Tools](security/SUPPLY_CHAIN_TOOLS.md)
- [Token Rotation](security/token-rotation-2025-10.md)

### 📦 Releases
Release notes and version history.
- [v1.7.0 Release](releases/RELEASE_v1.7.0.md)
- [Release Checklist](releases/RELEASE_CHECKLIST.md)
- [v0.3.0 Production Ready](releases/v0.3.0-production-ready.md)
- [v0.3.0 EPG Deployment](releases/v0.3.0-epg-deployment.md)

### 🔄 Improvements
Improvement roadmaps and implementation tracking.
- [Improvement Overview](improvements/README.md)
- [Implementation Roadmap](improvements/IMPLEMENTATION_ROADMAP.md)
- [Existing Strengths](improvements/EXISTING_STRENGTHS.md)
- [Tier 1: GPU Metrics](improvements/TIER1_GPU_METRICS.md)
- [Tier 1: Rate Limiting](improvements/TIER1_RATE_LIMITING.md)
- [Tier 2: Circuit Breaker](improvements/TIER2_CIRCUIT_BREAKER.md)
- [Tier 2: GPU Queue](improvements/TIER2_GPU_QUEUE.md)
- [Tier 2: Production Compose](improvements/TIER2_PRODUCTION_COMPOSE.md)
- [Tier 3: Features](improvements/TIER3_FEATURES.md)

### 🐛 Issues
Known issues and troubleshooting.
- [Issues Overview](issues/README.md)
- [API Refactoring](issues/API_REFACTORING.md)

### 🗄️ Archive
Historical documentation and completed projects.
- [Archive Overview](archive/README.md)
- [Phase 4 Completion](archive/PHASE_4_COMPLETION.md)
- [Phase 4 Implementation](archive/PHASE_4_IMPLEMENTATION.md)
- [Phase 5 Code Examples](archive/PHASE_5_CODE_EXAMPLES.md)
- [Phase 5 Debug Report](archive/PHASE_5_DEBUG_REPORT.md)
- [Phase 5 Implementation](archive/PHASE_5_IMPLEMENTATION_PLAN.md)
- [Phase 6/7 Baseline](archive/PHASE_6_7_BASELINE.md)
- [Action Plan](archive/ACTION_PLAN.md)
- [Daemon Refactoring](archive/DAEMON_REFACTORING_PLAN.md)
- [Sprint Summary](archive/SPRINT_SUMMARY.md)
- [Refactoring Summary](archive/refactoring-summary.md)

### 🚢 Deployment
Deployment guides for specific platforms.
- [Container HDHomeRun](deployment/CONTAINER_HDHOMERUN.md)
- [v1.3.0 Deployment](deployment/V1.3.0_DEPLOYMENT.md)

### 🔌 API
API documentation and contracts.
- [API v1 Contract](api/API_V1_CONTRACT.md)
- [API Migration Guide](api/API_MIGRATION_GUIDE.md)
- [API Health Check](api/API_HEALTH_CHECK.md)
- [API Reference](api/api.html)

## Getting Started

1. **New Users**: Start with the [Architecture Overview](ARCHITECTURE.md) and [Configuration Guide](guides/CONFIGURATION.md)
2. **Operators**: Check [Production Deployment](operations/PRODUCTION.md) and [Docker Deployment](operations/DOCKER_DEPLOYMENT.md)
3. **Developers**: Read [Contributing Guide](development/CONTRIBUTING.md) and [Testing Strategy](development/TESTING_STRATEGY.md)
4. **Security**: Review [Security Policy](security/SECURITY.md) and [Security Hardening](security/SECURITY_HARDENING.md)

## Contributing

See [CONTRIBUTING.md](development/CONTRIBUTING.md) for contribution guidelines.
