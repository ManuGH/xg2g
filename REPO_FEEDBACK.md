# Repo Feedback

## Highlights
- Clear positioning: README quickly conveys that xg2g bridges Enigma2 receivers with modern IPTV workflows and emphasizes plug-and-play setup. The feature comparison table and quick-start steps lower the barrier for new users.
- Strong developer onboarding: Docker-first workflow plus pinned Go 1.25 toolchain instructions make local development approachable, and the `.env.example` centralizes configuration knobs.
- Modern WebUI focus: Screenshots and narrative in the README underline the dashboard, health checks, and stream inspector, which help validate the system is operational without digging into logs.
- Production readiness signals: CI badge, Docker pulls badge, and mentions of Kubernetes probes, Prometheus metrics, and OpenTelemetry tracing suggest attention to operability.

## Opportunities
- Testing visibility: Surface how to run and interpret the Go test suite (e.g., `make test`, required services) in the README to encourage contributions.
- Architecture pointers: The README links to `docs/ARCHITECTURE.md`; highlighting the hybrid Go/Rust boundary (transcoding path vs. API/server) with a short diagram would help contributors navigate the code faster.
- Configuration presets: Consider adding example `.env` presets for common scenarios (single-box local test, GPU-enabled host) to make the extensive configuration options less daunting.

## Questions
- Is there a recommended workflow for validating hardware-accelerated Mode 3 in CI or locally without access to target GPUs?
- Are there guidelines for contributing to the Rust remuxer versus Go components to keep cross-language interfaces stable?
