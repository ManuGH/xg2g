# ADR-P7: Capability Resolution & Profile Selection

## Context

Capability drift (specifically AC3 support) and layering violations in truthful
playback resolution required a normative "Best-of" alignment.

## Decision

We enforce strict identity-bound capability resolution.

### 1. Profile Selection Rule

- **Explicit-Only**: `requested_profile` must be explicit (Body, Param, or Setting).
- **Default**: `web_conservative` (H264, AAC, MP3).
- **UA-Mapping**: Prohibited except via a versioned, hashed allowlist mapping to
  identity-bound profiles.

### 2. Single Source of Truth (SSOT)

- `ResolveCapabilities` is the sole provider of playback capabilities.
- **v3.1 Restriction**: Server NEVER extends client-provided codec/container lists.
  - **Allowed Constraints**:
    - `allow_transcode`: **Policy Constraint** (not a capability). Logic is
      `Server_Config && Client_Constraint`.
    - `max_video`: Server may clamp resolution (MIN apply).
  - **Immutable**: Lists (`containers`, `video_codecs`, `audio_codecs`) and
    `supports_hls` are strictly client-defined.
- **Legacy/Anon**: Returns exact bit-for-bit fixture content.
  - **Metric**: Verified via Go struct-equality and Canonical JSON comparison.

### 3. Structural Media Truth

- `GetMediaTruth` provides only raw facts (`container`, `video_codec`,
  `audio_codec`, `duration`, etc.).
- **PFLICHTFELDER (Mandatory)**: `container`, `video_codec`, `audio_codec`.
- **Precedence**: `Metadata Cache > Probe > Store`.
- **Fail-Closed**: ANY empty mandatory field MUST result in
  `422 decision_ambiguous`. No defaulting to "mp4" or "h264".

### 4. Mechanical Enforcement

- **Fixtures**: Every profile has a JSON fixture in `fixtures/capabilities/`.
- **Hash Gate**: `make verify-capabilities` enforces consistency via
  `GOVERNANCE_CAPABILITIES_BASELINE.json`.

---

## Consequences

- Deterministic behavior across all layers.
- Incompatibility between `web_conservative` and AC3 is truthfully reported (Transition to Transcode).
- Architectural purity: Handlers no longer "re-derive" truth from DTOs.
