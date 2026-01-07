# HWAccel Determinism - Production Ready

**Date**: 2026-01-05
**Status**: ✅ **Production Ready**
**Version**: v3.1+

---

## 🎯 Implementation Summary

### **Problem Solved**: Non-Deterministic GPU/CPU Switching

Before v3.1, the server automatically chose GPU vs CPU encoding based on hardware availability (`/dev/dri/renderD128`). This made Safari bugs **non-reproducible** across environments.

### **Solution**: Explicit `hwaccel` Parameter

```yaml
hwaccel:
  type: string
  enum: [auto, force, off]
  default: auto
```

---

## ✅ Production Readiness Criteria

### 1. **Hard-Fail Semantics** (`force` mode)

```go
// handlers_v3.go:218-224
if hwaccelMode == profiles.HWAccelForce && !hasGPU {
    RespondError(w, r, http.StatusBadRequest, ErrInvalidInput,
        "hwaccel=force requested but GPU not available (no /dev/dri/renderD128)")
    return
}
```

**Behavior**:
- `force` + no GPU → **400 Bad Request** (fail fast)
- `force` + GPU → VAAPI encoding
- **No silent degradation** to CPU

---

### 2. **Strict Input Validation**

```go
// handlers_v3.go:210-215
default:
    RespondError(w, r, http.StatusBadRequest, ErrInvalidInput,
        fmt.Sprintf("invalid hwaccel value: %q (must be auto, force, or off)", hwaccel))
    return
```

**Behavior**:
- Unknown `hwaccel` values → **400 Bad Request**
- No silent fallback to `auto`

---

### 3. **Deterministic Logging**

```go
// handlers_v3.go:266-279
logger.Info().
    Bool("gpu_available", hasGPU).
    Str("hwaccel_requested", string(hwaccelMode)).
    Str("hwaccel_effective", hwaccelEffective).  // ← NEW
    Str("hwaccel_reason", hwaccelReason).        // ← NEW
    Str("encoder_backend", encoderBackend).      // ← NEW
    Str("video_codec", profileSpec.VideoCodec).
    Str("container", profileSpec.Container).
    Bool("llhls", profileSpec.LLHLS).
    Msg("intent profile resolved")
```

**Log Fields**:
- `hwaccel_requested`: User intent (`auto|force|off`)
- `hwaccel_effective`: Actual outcome (`gpu|cpu|off`)
- `hwaccel_reason`: Why decision was made (see table below)
- `encoder_backend`: Specific encoder (`vaapi|libx264|hevc|none`)

#### `hwaccel_reason` Values

| Reason | Meaning |
|--------|---------|
| `forced` | User requested `hwaccel=force` |
| `auto_has_gpu` | Auto mode, GPU available |
| `user_disabled` | User requested `hwaccel=off` |
| `no_gpu_available` | Auto mode, no GPU |
| `profile_cpu_only` | Profile doesn't support GPU |
| `passthrough` | No transcoding (copy mode) |

---

### 4. **Test Coverage** (Automated)

```bash
$ go test -v ./internal/pipeline/profiles -run TestHWAccel
=== RUN   TestHWAccelForceWithoutGPU
--- PASS: TestHWAccelForceWithoutGPU (0.00s)
=== RUN   TestHWAccelOffAlwaysCPU
--- PASS: TestHWAccelOffAlwaysCPU (0.00s)
=== RUN   TestHWAccelAutoRespectGPU
--- PASS: TestHWAccelAutoRespectGPU (0.00s)
=== RUN   TestHWAccelHEVCProfiles
--- PASS: TestHWAccelHEVCProfiles (0.00s)
=== RUN   TestHWAccelPassthroughIgnored
--- PASS: TestHWAccelPassthroughIgnored (0.00s)
=== RUN   TestHWAccelDeterminism
--- PASS: TestHWAccelDeterminism (0.00s)
PASS
ok      github.com/ManuGH/xg2g/internal/pipeline/profiles    0.004s
```

**Critical Tests**:
1. `force` + no GPU → Hard-fail (handler validates)
2. `off` + GPU → Always CPU
3. `auto` + GPU → GPU / `auto` + no GPU → CPU

---

## 📋 API Examples

### Force GPU (Fail if Unavailable)

```bash
curl -X POST /api/v3/intents \
  -H "Content-Type: application/json" \
  -d '{
    "type": "stream.start",
    "profileID": "safari",
    "serviceRef": "1:0:1:445D:453:1:C00000:0:0:0:",
    "hwaccel": "force"
  }'
```

**Response** (if no GPU):
```json
{
  "code": "INVALID_INPUT",
  "message": "hwaccel=force requested but GPU not available (no /dev/dri/renderD128)",
  "request_id": "req_abc123"
}
```

---

### Force CPU (Reproducible Safari Debugging)

```bash
curl -X POST /api/v3/intents \
  -H "Content-Type: application/json" \
  -d '{
    "type": "stream.start",
    "profileID": "safari",
    "serviceRef": "1:0:1:445D:453:1:C00000:0:0:0:",
    "hwaccel": "off"
  }'
```

**Log Output**:
```json
{
  "profile": "safari",
  "gpu_available": true,
  "hwaccel_requested": "off",
  "hwaccel_effective": "cpu",
  "hwaccel_reason": "user_disabled",
  "encoder_backend": "libx264",
  "video_codec": "libx264",
  "container": "fmp4"
}
```

---

### Auto (Default, Backward Compatible)

```bash
curl -X POST /api/v3/intents \
  -H "Content-Type: application/json" \
  -d '{
    "type": "stream.start",
    "profileID": "safari",
    "serviceRef": "1:0:1:445D:453:1:C00000:0:0:0:"
  }'
```

**Log Output** (with GPU):
```json
{
  "profile": "safari",
  "gpu_available": true,
  "hwaccel_requested": "auto",
  "hwaccel_effective": "gpu",
  "hwaccel_reason": "auto_has_gpu",
  "encoder_backend": "vaapi",
  "video_codec": "h264",
  "container": "fmp4"
}
```

---

## 🔍 Debugging Safari Issues

### Scenario: "Player disappears on Safari Production, works on Dev"

**Step 1**: Check logs for GPU difference
```bash
# Production (GPU server)
grep "hwaccel_effective" logs.json | jq .
# → hwaccel_effective: "gpu", encoder_backend: "vaapi"

# Dev (No GPU)
grep "hwaccel_effective" logs.json | jq .
# → hwaccel_effective: "cpu", encoder_backend: "libx264"
```

**Step 2**: Force CPU on Production
```bash
curl -X POST /api/v3/intents -d '{"hwaccel":"off",...}'
```

**Step 3**: Interpret Results
- Bug **persists** with CPU → Safari/Profile issue (not GPU-specific)
- Bug **disappears** with CPU → VAAPI bitstream incompatibility

---

## 🎯 Next Hardening Items (Future)

### Phase 2: FFmpeg Init Failure Handling
```go
// Detect VAAPI init failures and auto-retry with CPU
if err := ffmpeg.Start(); err != nil && isVAAPIError(err) {
    log.Warn().Msg("VAAPI init failed, retrying with CPU")
    profileSpec.HWAccel = ""
    profileSpec.VideoCodec = "libx264"
    return ffmpeg.Start()
}
```

**Metrics**: `hwaccel_fallback_count{reason="vaapi_init_failed"}`

---

## ✅ Production Readiness Checklist

- [x] **`force` semantics defined** (hard-fail, no degradation)
- [x] **Input validation** (unknown values → 400)
- [x] **Deterministic logging** (`hwaccel_effective`, `hwaccel_reason`, `encoder_backend`)
- [x] **Test coverage** (6 automated tests)
- [x] **Backward compatible** (default `auto` preserves v3.0 behavior)
- [x] **API documentation** (OpenAPI schema updated)
- [x] **Error messages** (clear user-facing errors)

---

## 📊 Comparison Matrix

| Aspect | Before v3.1 | After v3.1 |
|--------|-------------|------------|
| **GPU Decision** | Server-side auto (non-deterministic) | User-controlled (`hwaccel` param) |
| **Reproducibility** | ❌ Same request → different encoder | ✅ Same request → same encoder |
| **Safari Debugging** | ❌ Can't force CPU on prod | ✅ `hwaccel=off` forces CPU |
| **Logging** | `gpu: true/false` | `hwaccel_effective`, `hwaccel_reason`, `encoder_backend` |
| **Validation** | Silent fallback | Hard-fail on invalid/unavailable |

---

## 📚 Related Documentation

- [SAFARI_INLINE_PLAYBACK_IMPLEMENTATION.md](SAFARI_INLINE_PLAYBACK_IMPLEMENTATION.md)
- [api/openapi.yaml](../api/openapi.yaml#L62-L71) - API Schema
- [internal/pipeline/profiles/hwaccel_test.go](../internal/pipeline/profiles/hwaccel_test.go) - Test Suite

---

## Summary

✅ **Production Ready**
✅ **`force` semantics: Hard-fail** (no silent degradation)
✅ **Strict validation** (unknown → 400)
✅ **Deterministic logging** (3 new fields)
✅ **6 automated tests** (all passing)
✅ **Backward compatible** (`auto` default)

**Key Benefit**: Safari bugs are now **100% reproducible** by forcing CPU encoding (`hwaccel=off`), eliminating "works on my machine" issues.
