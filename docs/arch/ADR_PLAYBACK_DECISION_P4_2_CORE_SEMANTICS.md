# ADR Delta (P4-2) — Playback Decision Engine Core Semantics and Reason-Code Freeze

**Status**: Proposed (P4-2 entry artifact)  
**Date**: 2026-01-18  
**Scope**: Engine-core semantics only (pure in-process `Decide()`), no handler/UI wiring.

> [!NOTE]
> **Post-P6 Contract Hardening**: This ADR predates the camelCase v3 API hardening.
> JSON examples may use `request_id`. Normative external contract as of Phase 6+ uses `requestId`.

## 0. Context / Prior ADR Link

This delta extends the P4-1 contract foundation ([ADR_PLAYBACK_DECISION.md](./ADR_PLAYBACK_DECISION.md) + OpenAPI + golden contract matrix). P4-2 implements the decision algorithm without changing the external contract.

## 1. Decision

Implement a typed, deterministic, fail-closed playback decision engine:

- **Entry point**: `Decide(ctx, Input) -> (HTTPStatus, Decision|Problem)`
- **Engine must be pure** (no I/O, no network, no filesystem, no FFmpeg probing).
- **Engine output must match P4-1 contract** (decision block or RFC7807 problem).
- **Reason-code vocabulary is frozen** in this delta. No ad-hoc strings.

## 2. Definitions

**Media truth (Source)**: `{ container, video_codec, audio_codec, bitrate_kbps?, width?, height?, fps? }`

**Capabilities**: `{ capabilities_version, container[], video_codecs[], audio_codecs[], supports_hls, supports_range?, max_video?, device_type? }`

**Policy**: `{ allow_transcode: bool }` (+ future fields gated by ADR+OpenAPI+goldens)

### Modes

- **`direct_play`**: Client can play source container+codecs directly.
- **`direct_stream`**: No re-encode; packaging/transport/container adaptation only (e.g., remux + HLS packaging), subject to capabilities (notably `supports_hls`).
- **`transcode`**: Any re-encode of audio and/or video.
- **`deny`**: Deterministic "cannot play" outcome.

### Indeterminate vs Deterministic

- **Deterministic deny** (200 + `mode=deny`): Given known truth and policy/caps, engine can conclude "no playable path," or "policy blocks required transcode."
- **Indeterminate** (422 problem `decision_ambiguous`): Input truth is unknown/contradictory such that the engine cannot decide.

## 3. Output Shape and Sentinel Rules (Hard Contract)

For all `status=200` responses:

- Always return a decision object with required fields: `mode`, `selected`, `outputs`, `constraints`, `reasons`, `trace`.
- `constraints` and `reasons` must be present (may be empty arrays).

### Selected Sentinel Rule (Non-Negotiable)

- For `mode=deny`, `selected.container`, `selected.video_codec`, `selected.audio_codec` MUST be the sentinel string `"none"` (not null, not omitted).
- For `mode != deny`, selected fields MUST be concrete non-empty strings.

### Outputs Rule

- For `mode=deny`, `outputs` MUST be `[]`.
- For `direct_play`, `outputs` MUST include a non-empty playable URL of kind `file` or `mp4` (exact kind naming per OpenAPI schema).
- For `direct_stream`, `outputs` MUST include an HLS output (playlist) and `supports_hls` MUST be `true`.
- For `transcode`, `outputs` MUST include the transcode target URL/playlist as applicable.

### Trace Rule

- `trace.request_id` is mandatory for all decision responses.

## 4. RFC7807 Problem Mapping (Frozen)

Return RFC7807 problem with stable code and status in these cases:

| Condition | Status | code | type |
|-----------|--------|------|------|
| Missing capabilities in API v3.1+ | 412 | `capabilities_missing` | `recordings/capabilities-missing` |
| Invalid `capabilities_version` | 400 | `capabilities_invalid` | `recordings/capabilities-invalid` |
| Indeterminate decision due to unknown/contradictory media truth | 422 | `decision_ambiguous` | `recordings/decision-ambiguous` |

**No new problem codes in P4-2.**

## 5. Reason-Code Vocabulary Freeze (P4-2 Baseline)

The engine MUST only emit reason codes from this list (additions require ADR+OpenAPI+goldens update):

### Policy

- `policy_denies_transcode`

### Capability incompatibility

- `video_codec_not_supported_by_client`
- `audio_codec_not_supported_by_client`
- `container_not_supported_by_client`
- `hls_not_supported_by_client`

### Decision classification

- `source_compatible_with_client`
- `container_remux_required`
- `transcode_required`

### Indeterminate

- `media_truth_unknown` (used only if returning 422; not allowed in 200 decision)

**Notes:**

- Reasons are machine-readable; no free-text reasons.
- Multiple reasons allowed; order is deterministic (Policy → Capability → Media/Packaging).

## 6. Decision Algorithm (Deterministic Precedence)

**Precedence remains**: Policy > Capabilities > Media truth > Default.

### 6.1 Input Validations (Fail-closed)

**V-1 Capabilities presence**

- If `api_version >= v3.1` and capabilities missing: return 412 `capabilities_missing`.

**V-2 Capabilities version**

- If `capabilities.capabilities_version != 1`: return 400 `capabilities_invalid`.

**V-L1 Legacy Compatibility (v3.0 Shim)**

> [!NOTE]
> V-L1 is a handler-level policy for backward compatibility; the Decision Engine itself remains pure and strictly requires explicit capabilities as input.

- If `api_version == v3.0` and capabilities missing:
  - The handler MUST provide "Anonymous Legacy Capabilities":
    - Video: `h264, hevc, mpeg2`
    - Audio: `aac, ac3, mp2, mp3`
    - Containers: `mp4, ts, mkv`
    - `supports_hls: true`
  - This shim exists only for backward compatibility with pure GET clients; v3.1+ clients MUST provide explicit capabilities or face 412.

**V-3 Media truth completeness**

- If any of `source.container`, `source.video_codec`, `source.audio_codec` is missing/empty/unknown:
  - return 422 `decision_ambiguous` with reason `media_truth_unknown`.
- If source fields are present but contradictory (e.g., invalid types): return 422 `decision_ambiguous`.

### 6.2 Compatibility Predicates

Define booleans (pure functions):

- `can_container = source.container ∈ caps.container[]`
- `can_video = source.video_codec ∈ caps.video_codecs[]`
- `can_audio = source.audio_codec ∈ caps.audio_codecs[]`
- `direct_play_possible = can_container && can_video && can_audio`
- `direct_stream_possible = caps.supports_hls == true && can_video && can_audio`  
  (container may differ; direct_stream is allowed to remux/package)
- `transcode_needed = !can_video || !can_audio || (!can_container && !direct_stream_possible)`
- `transcode_possible = policy.allow_transcode == true`  
  (P4-2 does not model resource constraints; feasibility is purely policy-gated)

### 6.3 Decision Table (Normative)

Evaluate in this order; first match wins:

| Step | Condition | Output |
|------|-----------|--------|
| **D-1** | `direct_play_possible` | 200 `decision.mode=direct_play`, reasons include `source_compatible_with_client` |
| **D-2** | `direct_stream_possible && !direct_play_possible` | 200 `decision.mode=direct_stream`, reasons include `container_remux_required` |
| **D-3** | `transcode_needed && policy.allow_transcode == true` | 200 `decision.mode=transcode`, reasons include `transcode_required` plus specific incompatibility reasons (video/audio/container/hls) |
| **D-4** | `transcode_needed && policy.allow_transcode == false` | 200 `decision.mode=deny`, reasons include `policy_denies_transcode` plus specific incompatibility reasons (video/audio/container/hls) |
| **D-5** | otherwise (should be unreachable if predicates correct) | 422 `decision_ambiguous` |

**Important**: D-4 is deterministic deny, not 422.  
422 is reserved for unknown/indeterminate truth, not policy/capability conflicts.

## 7. Mode Semantics Constraints (Prevent Mode Confusion)

**`direct_stream` MUST NOT re-encode video or audio.** It may only:

- remux container and/or
- package into HLS segments/playlist (if `supports_hls=true`).

If any re-encode would be required, the decision MUST be `transcode` (or `deny` if policy forbids).

## 8. Golden Contract Enforcement

- Existing P4-1 golden snapshots are binding acceptance criteria.
- P4-2 MUST keep `make contract-matrix` green for every commit that touches the engine or contract test harness.
- Any change to reason codes, decision schema, sentinel, or problem mapping is **Stop-the-line** and requires:
  1. ADR update
  2. OpenAPI update
  3. Explicit golden update with rationale

## 9. Non-Goals (Explicit)

- No handler wiring, no WebUI changes, no HTTP integration in P4-2.
- No FFmpeg probe calls, no media inspection, no runtime performance tuning.
- No capability inference or defaulting ("best guess") when capabilities or truth are missing.

## 10. Implementation Notes (Binding Guidance)

- Implement a typed internal model (structs) and pure helper predicates.
- Normalize/validate inputs once; do not scatter "if missing then…" logic.
- Emit reasons in deterministic order: Policy → Capability → Packaging/Transcode → Classification.

---

## Appendix A — Example Outputs (Normative Patterns)

### Deterministic deny

```json
{
  "status": 200,
  "decision": {
    "mode": "deny",
    "selected": { 
      "container": "none", 
      "video_codec": "none", 
      "audio_codec": "none" 
    },
    "outputs": [],
    "constraints": [],
    "reasons": [
      "policy_denies_transcode", 
      "video_codec_not_supported_by_client"
    ],
    "trace": { "request_id": "..." }
  }
}
```

### Indeterminate

```json
{
  "status": 422,
  "problem": {
    "type": "recordings/decision-ambiguous",
    "title": "Decision Ambiguous",
    "status": 422,
    "code": "decision_ambiguous",
    "detail": "Media truth unavailable or unknown (cannot make deterministic decision)"
  }
}
```

---

**Note**: If you want this to be even more "mechanical," I can also provide a one-page checklist version that reviewers can run line-by-line against the implementation (inputs validated, predicates correct, decision table order preserved, sentinel enforced, reason codes limited to whitelist).
