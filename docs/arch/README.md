# Architecture Index

Use this area for system design, package ownership, compatibility rules, and
decision-engine semantics. Operational commands belong in `docs/ops/`.

## Start Here

| Need | Document |
| :--- | :--- |
| Current repository/runtime snapshot | [2026 System Overview](SYSTEM_OVERVIEW_2026.md) |
| High-level system model | [Architecture Reference](ARCHITECTURE.md) |
| Where code belongs | [Package Layout](PACKAGE_LAYOUT.md) |
| Enigma2 streaming topology | [Enigma2 Streaming Topology](ENIGMA2_STREAMING_TOPOLOGY.md) |
| Codec/container truth | [Codec Matrix](CODEC_MATRIX.md) |
| Repository structure audit | [Repository Structure Audit](AUDIT_REPO_STRUCTURE.md) |

## Playback Decision System

| Need | Document |
| :--- | :--- |
| Decision-engine semantics | [Playback Decision Spec](../ADR/009-playback-decision-spec.md) |
| Core playback decision rules | [Playback Confidence Policy](../ADR/025-playback-confidence-policy.md) |
| Capability resolution | [Capability Resolution](../ADR/028-playback-capability-claims.md) |

## Platform-Specific Architecture

| Need | Document |
| :--- | :--- |
| Android device/session behavior | [Android Device Session State Machine](ANDROID_DEVICE_SESSION_STATE_MACHINE.md) |
| File config curated surface | [File Config Curated Surface](ADR_014_FILECONFIG_CURATED_SURFACE.md) |

## Maintenance Rule

Architecture docs should state invariants and ownership. Do not put deploy
commands, incident workarounds, or host-specific observations here; link to
`docs/ops/` for those.
