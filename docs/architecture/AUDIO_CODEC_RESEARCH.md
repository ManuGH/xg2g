# Audio Codec Research - Phase 4

## Objective

Evaluate and select optimal Rust libraries for native audio remuxing (MP2/AC3 → AAC).

**Criteria:**
1. **Quality:** iOS Safari AAC-LC compatibility
2. **Performance:** Low latency (<50ms), low CPU (<5%)
3. **Integration:** Ease of use, pure Rust vs FFI
4. **License:** Compatible with our project
5. **Binary Size:** Minimal overhead

## MP2 Decoder Options

### Option 1: Symphonia ✅ **RECOMMENDED**

**Pros:**
- ✅ Pure Rust (no system dependencies)
- ✅ Excellent MP2 support
- ✅ Good performance
- ✅ MIT/Apache-2.0 license
- ✅ Active development
- ✅ Used in production (Firefox, others)

**Cons:**
- ⚠️ Slightly higher CPU than native libraries

**Verdict:** **Best choice for MP2** - Pure Rust, reliable, good performance.

```toml
symphonia = { version = "0.5", features = ["mp2"] }
```

### Option 2: FFmpeg bindings (ac-ffmpeg)

**Pros:**
- ✅ Battle-tested decoder
- ✅ Excellent performance
- ✅ Supports all formats

**Cons:**
- ❌ Requires FFmpeg system libraries
- ❌ Complex FFI
- ❌ Larger binary size

**Verdict:** Use only if Symphonia has issues.

## AC3 Decoder Options

### Option 1: FFmpeg bindings (ac-ffmpeg) ✅ **RECOMMENDED**

**Pros:**
- ✅ Best AC3 decoder available
- ✅ Dolby Digital support
- ✅ Handles all AC3 variants
- ✅ Production-proven

**Cons:**
- ⚠️ Requires FFmpeg libraries
- ⚠️ FFI overhead (minimal)

**Verdict:** **Best choice for AC3** - No pure Rust alternative with comparable quality.

```toml
ac-ffmpeg = { version = "0.19", features = ["audio"] }
```

### Option 2: Symphonia (experimental)

**Pros:**
- ✅ Pure Rust
- ✅ No system dependencies

**Cons:**
- ❌ AC3 support is experimental
- ❌ Limited features (no E-AC3)
- ❌ Quality not production-ready

**Verdict:** Not ready for production yet.

## AAC Encoder Options

### Option 1: FFmpeg AAC Encoder (libavcodec) ✅ **RECOMMENDED**

**Pros:**
- ✅ iOS Safari compatible (AAC-LC)
- ✅ Good quality
- ✅ Configurable bitrate/profile
- ✅ Production-ready
- ✅ ADTS header support

**Cons:**
- ⚠️ Requires FFmpeg libraries
- ⚠️ Moderate quality (not as good as fdk-aac)

**Verdict:** **Best choice for AAC** - Good balance of quality, compatibility, and integration.

```toml
ac-ffmpeg = { version = "0.19", features = ["audio"] }
```

### Option 2: fdk-aac (via FFI)

**Pros:**
- ✅ Highest quality AAC encoder
- ✅ iOS compatible
- ✅ Industry standard

**Cons:**
- ❌ Requires system library (libfdk-aac)
- ❌ Complex licensing (proprietary/patent)
- ❌ Complex FFI integration
- ❌ Not available on all systems

**Verdict:** Consider for future quality upgrade, not for initial implementation.

### Option 3: Opus (opus crate)

**Pros:**
- ✅ Pure Rust bindings
- ✅ Good quality
- ✅ Low latency

**Cons:**
- ❌ Not AAC (different codec)
- ❌ iOS Safari support via WebRTC only
- ❌ Not compatible with MPEG-TS ecosystem

**Verdict:** Not suitable for our use case (need AAC for iOS Safari).

## MPEG-TS Muxer Options

### Option 1: mpeg2ts-writer ⚠️ **Check availability**

```bash
# Check if crate exists
cargo search mpeg2ts-writer
```

**If available:**
- ✅ Purpose-built for TS muxing
- ✅ Pure Rust

**If not available:**
- Build custom muxer (not complex)

### Option 2: Custom MPEG-TS Muxer ✅ **FALLBACK**

**Pros:**
- ✅ Full control
- ✅ Optimized for our use case
- ✅ No dependencies

**Cons:**
- ⚠️ More development time
- ⚠️ Need to handle all TS edge cases

**Verdict:** Build custom muxer if no good crate available.

## Audio Processing Utilities

### Sample Rate Conversion

**Option:** `rubato` ✅
```toml
rubato = "0.15"  # High-quality resampling
```

**Use Case:** Convert sample rates if input doesn't match encoder

### Sample Format Conversion / Mixing

**Option:** `dasp` ✅
```toml
dasp = "0.11"  # Digital audio signal processing
```

**Use Case:** Channel downmixing (5.1 → stereo), format conversion

## Recommended Dependency Set

Based on research, here's the optimal configuration:

```toml
[dependencies]
# === Core Async ===
tokio = { version = "1", features = ["full"] }

# === MPEG-TS Processing ===
mpeg2ts-reader = "0.18"  # Demuxer

# === Audio Codecs ===
# MP2 Decoder (pure Rust)
symphonia = { version = "0.5", features = ["mp2"] }

# AC3 Decoder + AAC Encoder (FFmpeg)
ac-ffmpeg = { version = "0.19", features = ["audio"] }

# === Audio Processing ===
dasp = "0.11"      # Sample processing, mixing
rubato = "0.15"    # Sample rate conversion

# === Utilities ===
anyhow = "1"
thiserror = "2"
bytes = "1"
```

## System Dependencies

Users will need FFmpeg libraries installed:

### Debian/Ubuntu
```bash
sudo apt-get install libavcodec-dev libavformat-dev libavutil-dev libswresample-dev
```

### macOS
```bash
brew install ffmpeg
```

### Docker (Alpine)
```dockerfile
RUN apk add --no-cache ffmpeg-dev
```

## Alternative: Pure Rust Approach (Future)

For a completely pure Rust solution (no FFmpeg):

```toml
[dependencies]
# MP2 Decoder
symphonia = { version = "0.5", features = ["mp2"] }

# AC3 Decoder (when stable)
# symphonia = { version = "0.5", features = ["ac3"] }

# AAC Encoder (pure Rust - when available)
# rAAC or similar (not yet production-ready)
```

**Timeline:** Wait for pure Rust AAC encoder to mature (6-12 months).

## Decision Matrix

| Component | Library | Type | Quality | Performance | Ease |
|-----------|---------|------|---------|-------------|------|
| **MP2 Decoder** | Symphonia | Pure Rust | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ |
| **AC3 Decoder** | ac-ffmpeg | FFI | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ |
| **AAC Encoder** | ac-ffmpeg | FFI | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐ |
| **TS Demux** | mpeg2ts-reader | Pure Rust | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ |
| **TS Mux** | Custom | Pure Rust | ⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐ |

## Implementation Strategy

### Phase 4a: Hybrid Approach (FFmpeg + Rust)
- **MP2:** Symphonia (pure Rust)
- **AC3:** ac-ffmpeg (FFI)
- **AAC:** ac-ffmpeg (FFI)
- **Timeline:** 3-4 weeks
- **Risk:** Low

### Phase 4b: Pure Rust Migration (Future)
- **MP2:** Symphonia ✅
- **AC3:** Wait for Symphonia AC3 stabilization
- **AAC:** Wait for pure Rust AAC encoder
- **Timeline:** 6-12 months
- **Risk:** Medium

## Conclusion

**Recommended Stack for Phase 4:**

✅ **Symphonia** for MP2 decoding (pure Rust)
✅ **ac-ffmpeg** for AC3 decoding (FFI to FFmpeg)
✅ **ac-ffmpeg** for AAC encoding (FFI to FFmpeg)
✅ **mpeg2ts-reader** for demuxing (pure Rust)
✅ **Custom muxer** for MPEG-TS output (pure Rust)

This provides the best balance of:
- Quality (iOS Safari compatible AAC)
- Performance (native speed)
- Maintainability (mostly Rust)
- Time-to-market (3-4 weeks)

We can migrate to pure Rust codecs in the future as they mature.
