# Design: Universal Transcoder Interface (UTI) v1

## 1. Objective

To decouple the `xg2g` daemon from the lower-level transcoding mechanics by establishing a strict, versioned, and hermetic interface for media processing.

## 2. Input Contract (Interface Definition)

The UTI is invoked via an opaque configuration object (JSON or Proto) passed through `stdin`. No ambient environment variables (except `UTI_INTERFACE_VER=1`) or host paths are permitted.

**Invariant**: UTI processes MUST be treated as disposable. The daemon MUST assume zero internal state persistence between invocations.

### Input JSON Structure

```json
{
  "source": {
    "type": "mpegts_stream",
    "uri": "http://receiver:8001/...",
    "timeout_ms": 5000
  },
  "sink": {
    "type": "hls_segmenter",
    "output_path": "/tmp/xg2g/sessions/abc...",
    "segment_duration_s": 1
  },
  "strategy": {
    "profile": "vaapi_lowlatency",
    "hw_device": "/dev/dri/renderD128",
    "max_cpu_percent": 15,
    "degradation_policy": "drop_fps"
  }
}
```

## 3. Capability Declaration

The transcoder MUST provide a discovery mechanism to the daemon on startup (`uti --info`):

- **Encoders**: `h264_vaapi`, `hevc_vaapi`, `libx264` (fallback)
- **Decoders**: Hardware-aware list based on `DRI` probing.
- **Limits**: Max concurrent hardware contexts, memory overhead per session.
- **ABI Version**: Current contract version (e.g., `1.0.2`).

## 4. Failure Taxonomy (Return Codes)

The UTI MUST use the following exit codes to signal failure categories:

| Code | Type | Description |
| :--- | :--- | :--- |
| **0** | Success | Process exited normally (graceful stop). |
| **101** | Terminal (Input) | Invalid Strategy or Malformed JSON. DO NOT RETRY. |
| **102** | Terminal (HW) | Hardware Device Busy or Missing. Switch to Software Fallback. |
| **103** | Recoverable (Source)| Source Connection Lost. Retry with exponential backoff. |
| **104** | Terminal (IO) | Disk Full or Permission Denied in `/var/lib/xg2g`. |
| **127** | Terminal (ABI) | Library/Symbol mismatch. SYSTEM ERROR. |

## 5. Metadata Propagation

The UTI must periodically emit "Sync Marks" through a side-channel (Unix Socket) during transcoding to report:

- Real-time bitrates.
- Frame-drop count.
- Progress (PTS/DTS offset).
- Hardware health (Temperature/Load if available).
