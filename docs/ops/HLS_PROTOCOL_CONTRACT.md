# HLS Protocol Contract

This document defines the normative HTTP behavior and contract truth for
HLS serving in xg2g.

## 1. Content-Type Truth

All HLS artifacts MUST be served with the following exact `Content-Type`
headers for maximum interoperability:

| Artifact | Content-Type |
| :--- | :--- |
| M3U8 Playlist | `application/vnd.apple.mpegurl` |
| MPEG-TS Segment | `video/mp2t` |
| fMP4 Segment | `video/mp4` |

> [!IMPORTANT]
> Since P9-2, we use lowercase MIME types (e.g., `video/mp2t`) to ensure
> compatibility with case-sensitive proxies and clients.

## 2. Segment Duration Truth (Best Practice 2026)

To ensure predictable buffering and seek accuracy, xg2g enforces specific
HLS segment durations:

- **Standard Policy**: 6 seconds (Default). Balanced for stability and DVR performance.
- **Low-Latency Policy**: 1 second. Optimized for real-time channel switching.

| Profile | `HLSSegmentSeconds` | Target Segment Duration |
| :--- | :--- | :--- |
| `standard` | 6 | 6.0s (Nominal) |
| `low_latency` | 1 | 1.0s (Nominal) |

> [!NOTE]
> Segment durations are nominal and may drift slightly based on upstream GOP
> boundaries (ServiceRef keyframes). xg2g uses `-force_key_frames` to align
> FFmpeg cuts to these exact targets.

## 3. Cache-Control Strategy

To balance playback startup latency, seek performance, and DVR freshness,
the following caching rules are enforced:

### 3.1 Playlists (`index.m3u8`, `timeshift.m3u8`)

- **Policy**: `no-store`
- **Rationale**: Playlists are dynamic and must never be cached by proxies
  or browsers. Stale playlists cause "Source Buffer Full" errors or infinite
  loops if the segment lists drift.

### 3.2 Fragments / Initialization Segments (`init.mp4`, `seg_X.m4s`)

- **Policy**: `public, max-age=3600`
- **Rationale**: Initialization segments are immutable for the duration of
  a session.

### 3.3 Media Segments (`seg_X.ts`, `seg_X.m4s`)

- **Policy**: `public, max-age=60`
- **Rationale**: Media segments are immutable once written. A 1-minute cache
  is safe even for Live/DVR as segment URLs are unique.
- **DVR Risk**: Long cache times on segments can be problematic if a
  recording is deleted and restarted with same segment names. 1 minute is
  the chosen safety/performance compromise.

## 4. Range Support (206/416)

HLS Segments MUST support `Range` requests to allow clients to probe headers
or resume partial downloads.

- **Policy**: [Policy A] Single-range only.
- **Multi-range**: Strictly rejected with `416 Range Not Satisfiable`.
- **Pre-ready**: Requests for segments of a "PREPARING" recording return
  `503 Service Unavailable` with a RFC 7807 problem body.

## 5. Error Semantics (RFC 7807)

All V3 HLS errors MUST return an `application/problem+json` body.

- **503 Preparing**: Used when the playlist or segment is being generated.
  Includes `Retry-After` header and RFC 7807 problem body.
- **404 Not Found**: Used when the recording or segment does not exist.
  Includes RFC 7807 problem body.
- **416 Range Not Satisfiable**: [Protocol-Level Exception] Used for invalid
  range requests. Includes `Content-Range: bytes */size` header but
  NO problem body (naked 416) for standard HTTP compliance.
