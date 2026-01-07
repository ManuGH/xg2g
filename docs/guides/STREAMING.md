# Streaming Guide

## Overview

xg2g uses a **single, server-defined streaming delivery policy** to ensure consistent, reliable playback across all supported clients.

**Policy**: `universal`

---

## Guarantees

The `universal` delivery policy provides the following guarantees:

| Component | Specification |
|---|---|
| **Video Codec** | H.264 (AVC) |
| **Audio Codec** | AAC |
| **Container** | fMP4 (Fragmented MP4) |
| **Delivery Protocol** | HLS (HTTP Live Streaming) |
| **Compatibility** | Safari/iOS Tier-1 compliant |

### Safari/iOS Compliance

The `universal` policy enforces **server-side constraints** to ensure Safari/iOS compatibility:

- **GOP (Group of Pictures)**: Aligned with segment boundaries
- **Segment Duration**: Optimized for Safari's HLS implementation
- **fMP4 Initialization**: Proper `#EXT-X-MAP` handling for Safari

> [!IMPORTANT]
> These constraints are **enforced by the server**. There is no client-side configuration or fallback logic.

---

## Non-Goals

The `universal` policy explicitly **does not** support:

- ❌ **HEVC/x265 by default**: H.264 is the only video codec
- ❌ **User-Agent detection**: No browser-specific logic
- ❌ **Client-side fallbacks**: No auto-switching or profile selection
- ❌ **Multiple policies**: `universal` is the only supported policy

---

## Philosophy: Bugs, Not Policies

> **If a browser doesn't play the stream, it's a bug in the pipeline—not a reason to introduce new policies or UI fallbacks.**

This approach ensures:

1. **Single Contract**: All clients receive the same stream format
2. **Server-Side Control**: Policy decisions are made by the server, not the client
3. **Predictable Behavior**: No client-side automatisms or hidden logic
4. **Easier Debugging**: One pipeline to test and fix

---

## Configuration

### Environment Variable

```bash
export XG2G_STREAMING_POLICY=universal  # Default (only supported value)
```

### YAML Configuration

```yaml
streaming:
  delivery_policy: universal  # Only supported value
```

### Validation

The application will **fail to start** if:

- `delivery_policy` is set to any value other than `universal`
- The deprecated `XG2G_STREAM_PROFILE` environment variable is still set

**Error Example**:

```
XG2G_STREAM_PROFILE removed. Use XG2G_STREAMING_POLICY=universal (ADR-00X)
```

---

## Troubleshooting

### Stream doesn't play in Safari

**Diagnosis**:

1. Check server logs for FFmpeg errors
2. Verify GOP/segment alignment in HLS playlist
3. Check `#EXT-X-MAP` initialization segment

**Resolution**:

- This is a **bug in the pipeline**, not a client issue
- Fix the server-side encoding/segmentation logic
- Do **not** introduce client-side fallbacks or new policies

### Stream doesn't play in Chrome/Firefox

**Diagnosis**:

1. Verify H.264/AAC codec support in browser
2. Check HLS.js console errors (if applicable)
3. Verify fMP4 container format

**Resolution**:

- This is a **bug in the pipeline**, not a client issue
- Fix the server-side encoding/container logic
- Do **not** introduce browser-specific policies

### Performance Issues

**Diagnosis**:

1. Check FFmpeg CPU/GPU usage
2. Verify segment size and bitrate
3. Check network bandwidth

**Resolution**:

- Optimize FFmpeg encoding parameters (server-side)
- Adjust segment duration if needed (server-side)
- Do **not** introduce client-side quality switching

---

## Migration from Profiles (v3.0 → v3.1)

### Breaking Changes

The concept of "Streaming Profiles" (`auto`, `safari`, `safari_hevc_hw`) has been **completely removed** in v3.1.

**Old Behavior** (v3.0):

- UI dropdown to select profile
- Auto-switching on error (auto → safari)
- Fullscreen profile switching (safari → safari_hevc_hw)
- `localStorage` persistence of user preference

**New Behavior** (v3.1):

- No UI dropdown
- No auto-switching
- No fullscreen profile changes
- Server-defined `universal` policy only

### Environment Variable Migration

| Old (v3.0) | New (v3.1+) | Required Action |
|---|---|---|
| `XG2G_STREAM_PROFILE=auto` | `XG2G_STREAMING_POLICY=universal` | **Update env var** |
| `XG2G_STREAM_PROFILE=safari` | `XG2G_STREAMING_POLICY=universal` | **Update env var** |
| `XG2G_STREAM_PROFILE=safari_hevc_hw` | `XG2G_STREAMING_POLICY=universal` | **Update env var** |

> [!CAUTION]
> **Fail-Start Protection**: If `XG2G_STREAM_PROFILE` is still set, the application will **fail to start**.

### Configuration File Migration

**Before** (v3.0):

```yaml
streaming:
  default_profile: auto
  allowed_profiles:
    - auto
    - safari
    - safari_hevc_hw
```

**After** (v3.1):

```yaml
streaming:
  delivery_policy: universal
```

---

## Future Policies

Any new delivery policy (e.g., HEVC, AV1) will require:

1. **New ADR**: Architectural Decision Record documenting the rationale
2. **Test Plan**: Comprehensive browser/device compatibility testing
3. **Audit**: Security and performance review
4. **Explicit Opt-In**: No automatic policy selection

This ensures that new policies are **deliberate, tested, and documented**—not reactive workarounds for client-side issues.

---

## References

- [ADR-00X: Delivery Policy](../rfc/ADR-00X-delivery-policy.md)
- [Configuration Guide](CONFIGURATION.md)
- [WebUI Thin Client Audit](../architecture/WEBUI_THIN_CLIENT_AUDIT.md)
