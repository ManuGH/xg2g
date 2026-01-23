# ADR-009: Playback Decision Engine Specification

**Status:** Accepted
**Date:** 2026-01-23

## Context

The Playback Decision Engine determines whether media should be played via `DirectPlay`, `DirectStream`, `Transcode`, or `Deny`. This core logic requires absolute determinism ("CTO-Grade Proof") to prevent regression and ensure operator trust.

## Normative Specification

### 1. Inputs (Fail-Closed)

The decision is a pure function of `DecisionInput`. Any "unknown" or zero-value in critical fields results in `ModeDeny`.

| Field | Type | Constraint | Unknown Semantics |
|---|---|---|---|
| `Source.Container` | string | Normalized (lowercase) | Deny (ReasonContainerNotSupported) |
| `Source.VideoCodec` | string | Normalized (lowercase) | Deny (ReasonVideoCodecNotSupported) |
| `Source.AudioCodec` | string | Normalized (lowercase) | Deny (ReasonAudioCodecNotSupported) |
| `Capabilities.Containers` | []string | Set of allowed | Implicit Deny if not present |
| `Capabilities.VideoCodecs` | []string | Set of allowed | Implicit Deny if not present |
| `Capabilities.AudioCodecs` | []string | Set of allowed | Implicit Deny if not present |
| `Policy.AllowTranscode` | bool | Boolean | If false, Transcode -> Deny |

### 2. Output Mode Lattice (Experience Order)

Modes are strictly ordered by "Experience Quality". Monotonicity Property: Improved capabilities must never produce a lower mode mode (unless Policy constraints intervene).

`ModeDeny` < `ModeTranscode` < `ModeDirectStream` < `ModeDirectPlay`

- **Deny**: Playback impossible.
- **Transcode**: Heavy resource usage, latency.
- **DirectStream**: Remux only (CPU light), requires compatible codecs.
- **DirectPlay**: Zero processing, requires compatible container & codecs.

### 3. Capability Partial Order

Capabilities $A$ are "better or equal" to $B$ ($A \succeq B$) if:

- $Set(A.Containers) \supseteq Set(B.Containers)$
- $Set(A.VideoCodecs) \supseteq Set(B.VideoCodecs)$
- $Set(A.AudioCodecs) \supseteq Set(B.AudioCodecs)$
- $A.SupportsHLS \geq B.SupportsHLS$ (True > False)

**Property:** If $Caps_New \succeq Caps_Old$, then $Mode(Caps_New) \geq Mode(Caps_Old)$.
*Exception:* If Policy prohibits transcode, a capability change that moves a decision from Deny to Transcode is still valid (it stays Deny due to Policy).

### 4. Decision Rules (Precedence Order)

Evaluation stops at the first match.

1. **Rule-Container**: If `Source.Container` not in `Capabilities.Containers` -> **Fail checks** (May fallback to Transcode).
2. **Rule-Video**: If `Source.VideoCodec` not in `Capabilities.VideoCodecs` -> **Transcode Needed**.
3. **Rule-Audio**: If `Source.AudioCodec` not in `Capabilities.AudioCodecs` -> **Transcode Needed**.
4. **Rule-DirectPlay**: If `Container` matches AND `DirectPlayPossible` (MP4/MOV + Range) -> **DirectPlay**.
5. **Rule-DirectStream**: If `SupportsHLS` AND Codecs Compatible -> **DirectStream**.
6. **Rule-Transcode**: If `AllowTranscode` -> **Transcode**.
7. **Default**: **Deny** (`ReasonNoCompatiblePlaybackPath`).

**Policy Exception Logic:**
If logic dictates `Transcode`, but `Policy.AllowTranscode == false`, result is `Deny` with reason `ReasonPolicyDeniesTranscode`. This is a specific "Policy Override".

### 5. Determinism & Observability

To guarantee $f(Input) \to Output$ is bit-identical:

- **Reason Sorting**: `Decision.Reasons` must be sorted alphabetically by `ReasonCode`.
- **Trace**: Every decision must carry a `Trace` with:
  - `InputHash`: SHA-256 of canonical JSON input (excluding RequestID).
  - `RuleHits`: Ordered list of rules evaluated/hit (Bounded enum).
  - `Why`: Structured explanation (e.g., `[{"code": "container_mismatched", "want": "mp4", "got": "mkv"}]`).

## Invariants

1. **Input Freeze**: Use `scripts/gate_decision_drift.sh` to prevent `DecisionInput` changes.
2. **Reason Boundedness**: All `ReasonCode` values must be in `reasons.go` whitelist.
