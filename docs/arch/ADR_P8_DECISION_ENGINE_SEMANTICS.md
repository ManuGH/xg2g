# ADR-P8: Decision Engine Semantics

## Context

Phase 7 successfully established:

- Structural Media Truth (`GetMediaTruth`)
- Identity-Bound Capabilities (`ResolveCapabilities`)
- Pure Adapter pattern (`handlePlaybackInfo` -> `Decide`)

However, the internal logic of `Decide()` and its emitted Reason Codes and Modes still allows for entropy and "implicit defaults". To guarantee SDK stability and deterministic debugging, these semantics must be frozen.

## Decision

We enforce a strict, table-driven semantic for the Decision Engine.

### 1. Decision Order (Immutable)

The engine MUST evaluate conditions in exactly this order. The first match determines the result.

| Step | Check | Result Mode |
| :--- | :--- | :--- |
| 1 | **Mandatory Truth Missing** (Container/Video/Audio) | `422 decision_ambiguous` |
| 2 | **Container Not Supported** | `deny` (Reason: `container_not_supported_by_client`) |
| 3 | **Video Codec Not Supported** | `transcode` (Reason: `video_codec_not_supported_by_client`) |
| 4 | **Audio Codec Not Supported** | `transcode` (Reason: `audio_codec_not_supported_by_client`) |
| 5 | **Transcode Required BUT Policy `allow_transcode=false`** | `deny` (Reason: `policy_denies_transcode`) |
| 6 | **MP4 Fast-Path** (Container=mp4/mov AND Codecs Supported AND Range Supported) | `direct_play` |
| 7 | **HLS Direct** (Codecs Supported AND `supports_hls=true`) | `direct_stream` |
| 8 | **Fallback** (Otherwise) | `deny` (Reason: `no_compatible_playback_path`) |

**Stop-the-Line**: Any deviation requires an ADR update and a Golden Matrix update.

### 2. Reason Code Freeze (Normative)

The application MUST emit strictly these reason codes. No implicit strings.

| Code | Meaning | Priority |
| :--- | :--- | :--- |
| `decision_ambiguous` | Missing mandatory truth (Fail-closed) | 1 (Guard) |
| `container_not_supported_by_client` | Container violation | 2 |
| `video_codec_not_supported_by_client` | Video violation | 3 |
| `audio_codec_not_supported_by_client` | Audio violation | 4 |
| `policy_denies_transcode` | Transcode required but forbidden by policy | 5 |
| `no_compatible_playback_path` | General Fallback (No other matched) | 6 |

**Note**: `direct_play` and `direct_stream` modes MAY emit codes like `directplay_match` or `directstream_match` for observability, but deny/transcode MUST be limited to the above.

### 3. Mode Semantics (Protocol Binding)

| Mode | Semantics | Protocol |
| :--- | :--- | :--- |
| `direct_play` | Static file access. Zero-copy. | `mp4` (stream.mp4) |
| `direct_stream` | Remux/Passthrough via HLS. | `hls` (playlist.m3u8) |
| `transcode` | Full re-encode via FFmpeg. | `hls` (playlist.m3u8) |
| `deny` | No playback possible. | `none` |

**Invariant**: `transcode` MUST NEVER result in `mp4` protocol.

## Consequences

- **Determinism**: The decision output is a pure function of (Truth, Caps, Policy).
- **Debuggability**: Reason codes point directly to the failing check in the ordered list.
- **SDK Stability**: Clients can rely on the presence of specific reason codes for error handling.

## Compliance

Enforced by:

1. **Golden Decision Matrix** (`fixtures/decision/*.json`)
2. **Invariant Tests** (Order & Output checks)
3. **Observability Contract** (Trace completeness)
