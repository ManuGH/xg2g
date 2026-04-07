# Codec & Container Matrix

This document maps the codec and container vocabulary that `xg2g` actually
reasons about at runtime. Every entry is derived from the source code, not from
theoretical muxing tables.

Canonical source files:

- `backend/internal/media/codec/codec.go` — normalized codec IDs
- `backend/internal/media/container/format.go` — container carry matrix
- `backend/internal/control/recordings/decision/media_adapter.go` — alias normalization
- `backend/internal/infra/ffmpeg/builder.go` — transcode target codecs
- `docs/ops/CLIENT_PROFILES.md` — browser-family playback policy

## Video Codecs

| Codec ID | Recognized Aliases | Typical Enigma2 Source | Notes |
| :--- | :--- | :--- | :--- |
| `h264` | `avc`, `avc1`, `libx264`, `video/avc` | SD/HD DVB-S/S2, DVB-T2 | Universal baseline. Every browser family supports this. |
| `hevc` | `h265`, `h.265`, `hev1`, `hvc1`, `libx265`, `video/hevc` | UHD DVB-S2 | DirectPlay/DirectStream for Safari families only. Transcode target when source is HEVC-capable. |
| `av1` | `av01`, `av1_vaapi`, `libsvtav1`, `libaom-av1`, `video/av01` | — | DirectPlay when client advertises support. Not an Enigma2 source codec today. |
| `mpeg2` | `mpeg-2`, `mpeg2video`, `video/mpeg2` | Legacy SD DVB | Always requires transcode for web delivery. |
| `vp9` | `vp09`, `libvpx-vp9`, `video/x-vnd.on2.vp9` | — | DirectPlay in MKV containers. Not an Enigma2 source codec today. |

## Audio Codecs

| Codec ID | Typical Enigma2 Source | Notes |
| :--- | :--- | :--- |
| `aac` | Common in re-encoded recordings | Universal web-safe audio. Default transcode target. |
| `ac3` | Standard DVB broadcast audio | Safari families can play natively. Others require transcode to AAC. |
| `eac3` | HD/UHD DVB broadcasts | Same browser support profile as AC3. |
| `mp2` | Legacy SD DVB | Requires transcode. Carried by MPEGTS and MKV only. |
| `mp3` | Rare in DVB context | Broad browser support. Transcode target option. |

## Container Carry Matrix

Which codecs can each container carry, as enforced by
`container.Format.CanCarry()`:

| | MPEGTS | fMP4 / MP4 | MKV |
| :--- | :---: | :---: | :---: |
| **h264** | ✅ | ✅ | ✅ |
| **hevc** | ✅ | ✅ | ✅ |
| **av1** | — | ✅ | ✅ |
| **mpeg2** | ✅ | — | ✅ |
| **vp9** | — | — | ✅ |
| **aac** | ✅ | ✅ | ✅ |
| **ac3** | ✅ | ✅ | ✅ |
| **eac3** | ✅ | ✅ | ✅ |
| **mp2** | ✅ | — | ✅ |
| **mp3** | ✅ | ✅ | ✅ |

## Browser Family Support

Summarized from [CLIENT_PROFILES.md](../ops/CLIENT_PROFILES.md) (binding
policy):

| Family | Video | Audio | Containers | Engine |
| :--- | :--- | :--- | :--- | :--- |
| `safari_native` | h264, hevc | aac, mp3, ac3 | mp4, ts | Native HLS |
| `ios_safari_native` | h264, hevc | aac, mp3, ac3 | mp4, ts | Native HLS |
| `firefox_hlsjs` | h264 | aac, mp3 | mp4, ts, fmp4 | hls.js |
| `chromium_hlsjs` | h264 | aac, mp3 | mp4, ts, fmp4 | hls.js |

## Transcode Targets

What FFmpeg can encode to when a transcode path is selected (from
`builder.go`):

| Kind | Supported Targets | Default |
| :--- | :--- | :--- |
| **Video** | `h264` (libx264), `hevc` (libx265) | h264 |
| **Audio** | `aac`, `ac3`, `mp3` (libmp3lame) | aac |
| **HLS Segments** | mpegts (`.ts`), fmp4 (`.m4s`) | mpegts |

## Decision Compatibility Checks

The typed compatibility evaluation (`EvaluateVideoCompatibility`) checks these
dimensions in order. Any failure triggers a reason code:

| Check | Reason Code | Effect |
| :--- | :--- | :--- |
| Codec ID mismatch | `codec_mismatch` | Incompatible (short-circuit) |
| Source is interlaced | `interlaced_source` | Requires video repair |
| Bit depth exceeds client | `bit_depth_exceeded` | Incompatible |
| Resolution exceeds client max | `resolution_exceeded` | Incompatible |
| Frame rate exceeds client max | `frame_rate_exceeded` | Incompatible |

Unknown dimensions (`bit_depth_unknown`, `resolution_unknown`,
`frame_rate_unknown`) are tracked but treated conservatively per the current
fail-closed policy.

## Recognized Frame Rates

The decision engine normalizes floating-point FPS to rational values:

| Float | Rational | Common Source |
| :--- | :--- | :--- |
| 23.976 | 24000/1001 | Film content |
| 25.0 | 25/1 | PAL DVB (most Enigma2 sources) |
| 29.97 | 30000/1001 | NTSC |
| 50.0 | 50/1 | PAL interlaced → progressive |
| 59.94 | 60000/1001 | NTSC high frame rate |

## How to Read This

Given a source stream and a client:

1. Look up the source video/audio codecs in the **Video/Audio Codecs** tables.
2. Check if the client's browser family (from **Browser Family Support**)
   claims those codecs.
3. Check the **Container Carry Matrix** for the delivery container.
4. If all match → **DirectPlay** or **DirectStream** is possible.
5. If any mismatch → the decision engine evaluates a **Transcode** path using
   the **Transcode Targets**.
6. If transcode is policy-denied or the client lacks HLS → **Deny**.

For the full decision algorithm, see
[ADR: Playback Decision Engine Semantics](ADR_P8_DECISION_ENGINE_SEMANTICS.md).
