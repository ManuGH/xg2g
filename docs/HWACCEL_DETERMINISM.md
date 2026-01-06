# HWAccel Determinism & Reproducibility

**Date**: 2026-01-05
**Status**: ✅ Implemented
**Version**: v3.1+

---

## Problem: Non-Deterministic GPU/CPU Switching

### Before (v3.0)

```go
// Server decides GPU/CPU based on hardware availability
hasGPU := hardware.HasVAAPI()  // Checks /dev/dri/renderD128

if hasGPU {
    spec.HWAccel = "vaapi"  // GPU encoding
} else {
    spec.VideoCodec = "libx264"  // CPU fallback
}
```

**Issue**: Same `profileID=safari` request → different encoder depending on server hardware
- **Production server** (with GPU): VAAPI HEVC encoding
- **Dev laptop** (no GPU): libx264 CPU encoding
- **Safari bugs not reproducible** across environments

---

## Solution: Explicit `hwaccel` Parameter

### API Extension

```yaml
# api/openapi.yaml
IntentRequest:
  properties:
    hwaccel:
      type: string
      enum: [auto, force, off]
      default: auto
      description: |
        Hardware acceleration override (v3.1+).
        - auto: Server decides based on GPU availability
        - force: Force GPU encoding (fails if no GPU)
        - off: Force CPU encoding
```

### Backend Implementation

```go
// internal/pipeline/profiles/resolve.go

type HWAccelMode string

const (
    HWAccelAuto  HWAccelMode = "auto"  // Server decides
    HWAccelForce HWAccelMode = "force" // Force GPU
    HWAccelOff   HWAccelMode = "off"   // Force CPU
)

func shouldUseGPU(hasGPU bool, mode HWAccelMode) bool {
    switch mode {
    case HWAccelForce:
        return true  // Force GPU (FFmpeg fails if unavailable)
    case HWAccelOff:
        return false  // Force CPU
    case HWAccelAuto:
        return hasGPU  // Auto-detect
    }
}
```

### Enhanced Logging

```go
// internal/api/handlers_v3.go

logger.Info().
    Str("profile", profileSpec.Name).
    Bool("gpu_available", hasGPU).
    Str("hwaccel_requested", string(hwaccelMode)).
    Bool("hwaccel_active", actualGPU).
    Str("video_codec", profileSpec.VideoCodec).
    Str("container", profileSpec.Container).
    Bool("llhls", profileSpec.LLHLS).
    Msg("intent profile resolved")
```

**Output Example**:
```json
{
  "profile": "safari",
  "gpu_available": true,
  "hwaccel_requested": "force",
  "hwaccel_active": true,
  "video_codec": "h264",
  "container": "fmp4",
  "llhls": false
}
```

---

## Usage

### API Requests

```bash
# Default: Auto-detect GPU
curl -X POST /api/v3/intents \
  -d '{"type":"stream.start","profileID":"safari","serviceRef":"..."}'

# Force CPU (for debugging Safari issues)
curl -X POST /api/v3/intents \
  -d '{"type":"stream.start","profileID":"safari","serviceRef":"...","hwaccel":"off"}'

# Force GPU (fails if no GPU available)
curl -X POST /api/v3/intents \
  -d '{"type":"stream.start","profileID":"safari","serviceRef":"...","hwaccel":"force"}'
```

### Frontend (Future)

```typescript
// webui/src/components/V3Player.tsx

const startStream = async () => {
  await fetch('/api/v3/intents', {
    method: 'POST',
    body: JSON.stringify({
      type: 'stream.start',
      profileID: selectedProfile,
      serviceRef: sRef,
      hwaccel: debugMode ? 'off' : 'auto'  // Force CPU for debugging
    })
  });
};
```

---

## Debugging Safari Issues

### Scenario: "Player disappears on Safari, but only sometimes"

**Step 1**: Reproduce with forced CPU encoding
```bash
curl -X POST /api/v3/intents \
  -d '{"hwaccel":"off","profileID":"safari","serviceRef":"..."}'
```

**Step 2**: Check logs
```
INFO intent profile resolved
  profile=safari
  hwaccel_requested=off
  hwaccel_active=false
  video_codec=libx264
  container=fmp4
```

**Step 3**: If issue persists with CPU → Safari/Profile bug, not GPU-specific
**Step 4**: If issue disappears with CPU → VAAPI bitstream incompatibility

---

## Profile Behavior Matrix

| Profile | hwaccel=auto | hwaccel=force | hwaccel=off |
|---------|--------------|---------------|-------------|
| `safari` | GPU if available | GPU (or fail) | CPU |
| `safari_hevc_hw` | GPU if available | GPU (or fail) | CPU (x265) |
| `safari_hevc_hw_ll` | GPU if available | GPU (or fail) | CPU (x265 + LL-HLS) |
| `high` | Passthrough/CPU | N/A | N/A |
| `low` | Always CPU | N/A | N/A |

---

## Test Coverage

### Unit Test

```go
func TestHWAccelOverride(t *testing.T) {
    tests := []struct {
        name           string
        hwaccel        profiles.HWAccelMode
        hasGPU         bool
        expectedHWAccel string
    }{
        {"auto+gpu → vaapi", profiles.HWAccelAuto, true, "vaapi"},
        {"auto+no-gpu → cpu", profiles.HWAccelAuto, false, ""},
        {"force+gpu → vaapi", profiles.HWAccelForce, true, "vaapi"},
        {"force+no-gpu → vaapi (fail later)", profiles.HWAccelForce, false, "vaapi"},
        {"off+gpu → cpu", profiles.HWAccelOff, true, ""},
        {"off+no-gpu → cpu", profiles.HWAccelOff, false, ""},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            spec := profiles.Resolve("safari", "Safari/17.0", 10800, nil, tt.hasGPU, tt.hwaccel)
            assert.Equal(t, tt.expectedHWAccel, spec.HWAccel)
        })
    }
}
```

---

## Migration Notes

### v3.0 → v3.1

**No breaking changes**:
- Default `hwaccel=auto` preserves v3.0 behavior
- Existing clients continue to work
- `hwaccel` parameter is **optional**

**Recommended Updates**:
1. Add `hwaccel` parameter to WebUI (debug toggle)
2. Update tests to explicitly set `hwaccel` for reproducibility
3. Document `hwaccel` in API reference

---

## Future Improvements

### Phase 1 (Current): Explicit Override
- ✅ API parameter `hwaccel=auto|force|off`
- ✅ Logging for debugging

### Phase 2 (Future): Frontend UI
- [ ] Debug panel in WebUI
- [ ] "Force CPU" toggle for Safari testing
- [ ] "Force GPU" toggle for performance testing

### Phase 3 (Future): Auto-Fallback
- [ ] Detect FFmpeg GPU init failures
- [ ] Auto-retry with CPU if GPU fails
- [ ] Metrics: `hwaccel_fallback_count`

---

## Related Documentation

- [Safari Inline Playback](SAFARI_INLINE_PLAYBACK_IMPLEMENTATION.md) - Safari-specific fixes
- [Profile Resolution](../internal/pipeline/profiles/resolve.go) - Profile logic
- [Hardware Detection](../internal/pipeline/hardware/gpu.go) - VAAPI detection

---

## Summary

✅ **GPU/CPU decision now deterministic**
✅ **Reproducible Safari debugging**
✅ **Backward compatible (auto=default)**
✅ **Enhanced logging for troubleshooting**

**Key Benefit**: "Player disappears on Safari" bugs are now reproducible by forcing CPU encoding (`hwaccel=off`), eliminating "works on my machine" issues.
