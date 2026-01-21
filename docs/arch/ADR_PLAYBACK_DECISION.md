# ADR: Playback Decision Engine

**Status**: APPROVED  
**Date**: 2026-01-18  
**Context**: Phase 4 - Backend-Driven Playback Path Selection

> [!NOTE]
> **Post-P6 Contract Hardening**: This ADR predates the camelCase v3 API hardening.
> JSON examples may use `request_id`. Normative external contract as of Phase 6+ uses `requestId`.

---

## Problem

Playback path selection (DirectPlay/DirectStream/Transcode) is currently implicit, UI-driven, and non-deterministic. This causes:

- Unpredictable behavior across clients/browsers
- Impossible root-cause analysis when playback fails
- UI logic duplication of backend concerns
- No fail-closed policy when capabilities are missing

---

## Decision

**Backend becomes sole decision authority for playback paths**.

Clients provide capabilities (versioned contract). Backend returns complete decision (no UI guessing).

---

## Input Contract

### Capabilities (Required, Versioned)

```json
{
  "capabilities_version": 1,
  "container": ["mp4", "webm", "hls"],
  "video_codecs": ["h264", "hevc"],
  "audio_codecs": ["aac", "ac3"],
  "max_video": {
    "width": 1920,
    "height": 1080,
    "fps": 60
  },
  "supports_hls": true,
  "supports_range": true,
  "device_type": "desktop"
}
```

**Required fields**:

- `capabilities_version` (int): Contract version for evolution
- `container` (array): Supported containers
- `video_codecs` (array): Supported video codecs
- `audio_codecs` (array): Supported audio codecs

**Optional fields**:

- `max_video`: Resolution/FPS constraints
- `supports_hls`: HLS playlist support
- `supports_range`: HTTP range request support
- `device_type`: Client category for policy

---

## Output Contract

### Decision (Complete, Required Fields)

```json
{
  "mode": "direct_play",
  "selected": {
    "container": "mp4",
    "video_codec": "h264",
    "audio_codec": "aac"
  },
  "outputs": [
    {
      "kind": "file",
      "url": "http://127.0.0.1:8088/api/v3/recordings/rec:xyz/stream.mp4"
    }
  ],
  "constraints": [],
  "reasons": ["source_compatible_with_client"],
  "trace": {
    "request_id": "550e8400-e29b-41d4-a716-446655440000",
    "session_id": "rec:xyz:1234"
  }
}
```

**Required fields**:

- `mode` (enum): `direct_play | direct_stream | transcode | deny`
- `selected` (object): Container + codecs (canonical output format)
- `outputs` (array): URLs/playlists for client consumption
- `constraints` (array): Applied limitations (e.g., `["downscale_required"]`)
- `reasons` (array): Machine-readable decision codes
- `trace` (object): `request_id` + optional `session_id`

**UI must not add/guess any fields**. Missing field = contract violation.

---

## Decision Modes

| Mode | Description | Example Use Case |
|------|-------------|------------------|
| `direct_play` | No transcoding, direct file serve | H.264/MP4 to modern browser |
| `direct_stream` | Container remux only (codecs unchanged) | TS→HLS with H.264 passthrough |
| `transcode` | Video/audio re-encoding required | HEVC→H.264 for older devices |
| `deny` | No compatible path available | Policy blocks transcode, client can't play native |

---

## Reason Codes (Machine-Readable)

**Success reasons**:

- `source_compatible_with_client`
- `container_incompatible_but_codecs_compatible`
- `video_codec_not_supported_by_client`
- `audio_codec_not_supported_by_client`
- `client_max_resolution_requires_transcode`
- `policy_forced_transcode`

**Failure reasons**:

- `capabilities_missing`
- `capabilities_invalid`
- `no_compatible_path`
- `policy_denies_transcode`
- `source_probe_failed`

---

## Precedence Rules (Strict Ordering)

Decision logic follows this priority:

1. **Policy Override** (highest)
   - Explicit admin/tenant policy (e.g., `FORCE_TRANSCODE_MOBILE=true`)

2. **Client Capabilities**
   - Declared codecs/containers (trusted input)

3. **Media Truth**
   - Source format/bitrate/resolution (probed)

4. **Default Fallback** (lowest)
   - HLS transcode (always works for modern clients)

---

## Fail-Closed Rules

**No silent fallbacks. No heuristics. No "best guess".**

### Missing Capabilities (v3.1+)

**Input**: No `capabilities` provided  
**Response**: `412 Precondition Failed`

```json
{
  "type": "about:blank",
  "title": "Precondition Failed",
  "status": 412,
  "code": "capabilities_missing",
  "detail": "Client must provide capabilities (capabilities_version required)",
  "request_id": "..."
}
```

### Invalid Capabilities

**Input**: Unknown `capabilities_version` or malformed  
**Response**: `400 Bad Request`

```json
{
  "type": "about:blank",
  "title": "Bad Request",
  "status": 400,
  "code": "capabilities_invalid",
  "detail": "capabilities_version 999 not supported (current: 1)",
  "request_id": "..."
}
```

### Ambiguous Decision

**Input**: No compatible path (client can't play, transcode not allowed)  
**Response**: `422 Unprocessable Entity`

```json
{
  "type": "about:blank",
  "title": "Unprocessable Entity",
  "status": 422,
  "code": "decision_ambiguous",
  "detail": "No compatible playback path (client codecs: [av1], source: hevc, transcode: disabled)",
  "request_id": "..."
}
```

---

## RFC7807 Error Codes (Stable)

| Code | HTTP Status | Meaning |
|------|-------------|---------|
| `capabilities_missing` | 412 | Client didn't provide capabilities |
| `capabilities_invalid` | 400 | Capabilities malformed or unsupported version |
| `decision_ambiguous` | 422 | No compatible path available |
| `policy_conflict` | 409 | Policy contradicts client request |

All errors include `request_id` for traceability.

---

## Migration Path

### v3.0 (Current - Grace Period)

- Capabilities **optional**
- If missing: return `deny` with reason `capabilities_required_v3_1`
- Log warning, track adoption rate

### v3.1 (January 2026 - Enforcement)

- Capabilities **required** (412 if missing)
- No silent fallbacks
- Full fail-closed enforcement

---

## Contract Verification

**Golden Snapshot Matrix**: Minimum 8 scenarios

- DirectPlay possible
- DirectStream (container mismatch)
- Transcode (codec mismatch)
- Policy deny
- Capabilities missing (fail-closed)
- Capabilities invalid (fail-closed)
- Decision ambiguous (fail-closed)
- Idempotent (same input → same output)

**CI Gate**: `make contract-matrix` must pass before merge.

---

## Consequences

**Positive**:

- Deterministic playback (same input → same decision)
- Traceable failures (request_id + reasons)
- Thin-client UI (no decision logic)
- Evolvable contract (capabilities_version)

**Negative**:

- Clients must send capabilities (breaking change mitigated by v3.0 grace period)
- Requires comprehensive contract test coverage

---

## References

- CTO Review: Stop-the-Line Requirements (2026-01-18)
- OpenAPI: `api/openapi.yaml` (decision block)
- Contract Tests: `testdata/contract/p4_1/*.json`
