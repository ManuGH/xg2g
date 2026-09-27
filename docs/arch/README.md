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
| Enigma2 FBC Tuner Resource Model | [Enigma2 FBC Resource Model](ENIGMA2_FBC_RESOURCE_MODEL.md) |
| Codec/container truth | [Codec Matrix](CODEC_MATRIX.md) |

## Playback Decision System

| Need | Document |
| :--- | :--- |
| Decision-engine normative index | [Playback Decision Spec Index](PLAYBACK_DECISION_SPEC_INDEX.md) |
| Decision-engine semantics | [Playback Decision Spec](../ADR/009-playback-decision-spec.md) |
| Core playback decision rules | [Playback Confidence Policy](../ADR/025-playback-confidence-policy.md) |
| Capability resolution | [Capability Resolution](../ADR/028-playback-capability-claims.md) |
| Live playback attestation & tokens | [Live Playback Attestation](LIVE_PLAYBACK_ATTESTATION.md) |
| Session lifecycle & cancel semantics | [Session Lifecycle](SESSION_LIFECYCLE.md) |
| HLS protocol & MIME truth | [HLS Protocol Contract](HLS_PROTOCOL_CONTRACT.md) |
| Finite duration truth | [Duration Truth](DURATION_TRUTH.md) |
| Resource contention degradation | [Degradation Contract](DEGRADATION_CONTRACT.md) |
| Universal transcoder interface | [Universal Transcoder Design](UNIVERSAL_TRANSCODER_DESIGN.md) |
| Hermetic execution boundary | [Hermetic Boundary Design](HERMETIC_BOUNDARY_DESIGN.md) |

## Platform-Specific Architecture

| Need | Document |
| :--- | :--- |
| Android device/session behavior | [Android Device Session State Machine](ANDROID_DEVICE_SESSION_STATE_MACHINE.md) |
| File config curated surface | [File Config Curated Surface](ADR_014_FILECONFIG_CURATED_SURFACE.md) |

## Maintenance Rule

Architecture docs should state invariants and ownership. Do not put deploy
commands, incident workarounds, or host-specific observations here; link to
`docs/ops/` for those.
