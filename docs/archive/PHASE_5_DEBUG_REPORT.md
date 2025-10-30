# Phase 5 Debug Report - FFI Initialization Failure

**Date:** 2025-10-30
**Environment:** LXC Container (10.10.55.14)
**Status:** 🔴 **FFI INITIALIZATION FAILURE**

## Executive Summary

The Rust library (`libxg2g_transcoder.so`, 594 KB) successfully compiled with real AC3 decoder and AAC-LC encoder implementations using ac-ffmpeg 0.19. However, the FFI initialization fails when called from the Go daemon, preventing actual audio transcoding.

## What Works ✅

### 1. Compilation Success
- **Library:** `libxg2g_transcoder.so` (594 KB)
- **Build:** Release mode, optimized
- **API:** ac-ffmpeg 0.19 with correct patterns
- **Warnings:** 10 unused imports (non-blocking)

### 2. Library Loading
- ✅ Library found and loaded by Go daemon
- ✅ No `LD_LIBRARY_PATH` errors
- ✅ CGO bindings functional

### 3. Go Daemon Integration
- ✅ Daemon starts successfully
- ✅ Config loaded from environment variables
- ✅ Proxy server listening on port 18001
- ✅ "audio transcoding enabled (audio-only)" message
- ✅ "using native rust remuxer" - correct code path

### 4. FFmpeg Codec Availability
```bash
$ ffmpeg -codecs | grep -E 'ac3|aac'
DEAIL. ac3    ATSC A/52A (AC-3) (decoders: ac3 ac3_fixed) (encoders: ac3 ac3_fixed)
DEA.L. aac    AAC (Advanced Audio Coding) (decoders: aac aac_fixed)
```
✅ Both AC3 and AAC codecs available in system FFmpeg

## Current Blocker 🔴

### Error Message
```json
{"level":"error","service":"xg2g","version":"","component":"proxy",
 "error":"failed to initialize Rust audio remuxer",
 "time":"2025-10-30T07:11:44Z",
 "message":"failed to initialize rust remuxer"}
```

### Timeline of Call
1. ✅ Go: `GET /1:0:19:132F:3EF:1:C00000:0:0:0:`
2. ✅ Go: "using native rust remuxer"
3. ❌ FFI: `NewRustAudioRemuxer(48000, 2, 192000)` → **FAILS**
4. ❌ Go: "failed to initialize rust remuxer"
5. ❌ Client: Receives 0 bytes

## Implementation Details

### AC3 Decoder (decoder.rs:283-547)
```rust
pub struct Ac3Decoder {
    decoder: Option<ac_ffmpeg::codec::audio::AudioDecoder>,
    sample_rate: u32,
    channels: u16,
    frames_decoded: u64,
    initialized: bool,
}

fn init_decoder(&mut self) -> Result<()> {
    let codec_params = ac_ffmpeg::codec::AudioCodecParameters::builder("ac3")
        .context("Failed to create AC3 codec parameters")?
        .sample_rate(48000)
        .build();

    let decoder = ac_ffmpeg::codec::audio::AudioDecoder::from_codec_parameters(&codec_params)
        .context("Failed to create AC3 decoder builder")?
        .build()
        .context("Failed to build AC3 decoder")?;

    self.decoder = Some(decoder);
    Ok(())
}
```

### AAC-LC Encoder (encoder.rs:257-407)
```rust
pub struct FfmpegAacEncoder {
    encoder: ac_ffmpeg::codec::audio::AudioEncoder,
    config: AacEncoderConfig,
    sample_buffer: Vec<f32>,
    frames_encoded: u64,
    pts_counter: i64,
}

pub fn new(config: AacEncoderConfig) -> Result<Self> {
    let channel_layout = ac_ffmpeg::codec::audio::ChannelLayout::from_channels(config.channels as u32)
        .context("Failed to create channel layout")?;

    let codec_params = ac_ffmpeg::codec::AudioCodecParameters::builder("aac")
        .context("Failed to create AAC codec parameters")?
        .bit_rate(config.bitrate as u64)
        .sample_rate(config.sample_rate)
        .channel_layout(&channel_layout)
        .build();

    let encoder = ac_ffmpeg::codec::audio::AudioEncoder::from_codec_parameters(&codec_params)
        .context("Failed to create AAC encoder builder")?
        .set_option("profile", config.profile.ffmpeg_name())
        .build()
        .context("Failed to build AAC encoder")?;

    Ok(Self { encoder, config, ... })
}
```

## Possible Causes

### 1. Lazy Initialization Issue
The decoder is initialized **lazily** on first packet, not during FFI initialization. But the encoder is initialized **eagerly** in `new()`.

**Hypothesis:** Encoder initialization fails during `NewRustAudioRemuxer()`.

### 2. Error Context Lost in FFI
The Rust code uses `anyhow::Result` with `.context()` for detailed errors, but the FFI boundary only returns null on failure.

**Current FFI signature:**
```rust
#[no_mangle]
pub extern "C" fn NewRustAudioRemuxer(
    sample_rate: u32,
    channels: u16,
    bitrate: u32,
) -> *mut RemuxerHandle {
    // Returns null on error - no error details exposed!
}
```

### 3. ac-ffmpeg Initialization
The `AudioEncoder::from_codec_parameters()` might fail if:
- FFmpeg library not initialized properly
- Codec not available at runtime (despite being in `ffmpeg -codecs`)
- Channel layout invalid for AAC
- Bitrate out of range

### 4. Memory/Threading Issues
- FFmpeg might not be thread-safe in initialization
- Memory allocation failure (unlikely - only 594 KB library)

## Diagnostic Steps Needed

### 1. Add Error Logging to FFI
```rust
#[no_mangle]
pub extern "C" fn NewRustAudioRemuxer(
    sample_rate: u32,
    channels: u16,
    bitrate: u32,
) -> *mut RemuxerHandle {
    match AudioRemuxer::new(sample_rate, channels, bitrate) {
        Ok(remuxer) => Box::into_raw(Box::new(RemuxerHandle { remuxer })),
        Err(e) => {
            eprintln!("FFI Error: {:#}", e); // Print full error chain!
            std::ptr::null_mut()
        }
    }
}
```

### 2. Enable Trace Logging
```bash
export RUST_LOG=trace
export RUST_BACKTRACE=1
```

### 3. Unit Test FFI Directly
Create a Rust test that calls `NewRustAudioRemuxer()` directly without Go:
```rust
#[test]
fn test_ffi_initialization() {
    let handle = unsafe { NewRustAudioRemuxer(48000, 2, 192000) };
    assert!(!handle.is_null(), "FFI initialization failed");
    unsafe { FreeRustAudioRemuxer(handle) };
}
```

### 4. Test Encoder Directly
```rust
#[test]
fn test_aac_encoder_creation() {
    let config = AacEncoderConfig {
        sample_rate: 48000,
        channels: 2,
        bitrate: 192000,
        profile: AacProfile::AacLc,
    };

    let result = FfmpegAacEncoder::new(config);
    match result {
        Ok(_) => println!("Encoder created successfully"),
        Err(e) => panic!("Encoder creation failed: {:#}", e),
    }
}
```

## Configuration Used

```bash
# Working configuration from test-rust-remuxer.sh
export LD_LIBRARY_PATH=/root/xg2g/transcoder/target/release
export XG2G_OWI_BASE=http://10.10.55.57:17999
export XG2G_ENABLE_STREAM_PROXY=true
export XG2G_PROXY_LISTEN=:18001
export XG2G_PROXY_TARGET=http://10.10.55.57:17999
export XG2G_USE_RUST_REMUXER=true
export XG2G_ENABLE_AUDIO_TRANSCODING=true
export XG2G_AUDIO_CODEC=aac
export XG2G_AUDIO_BITRATE=192k
export XG2G_AUDIO_CHANNELS=2
export XG2G_LOG_LEVEL=debug
```

## Test Environment

- **Proxy URL:** http://10.10.55.14:18001/
- **Test Stream:** ORF1 HD - `1:0:19:132F:3EF:1:C00000:0:0:0:`
- **Audio Source:** AC3 5.1 Surround, 48kHz, 448kbps
- **Video:** H.264 720p50

## Next Actions

1. **HIGH PRIORITY:** Add error logging to FFI layer (eprintln!)
2. **HIGH PRIORITY:** Write unit test for FFI initialization
3. **MEDIUM:** Test encoder creation in isolation
4. **MEDIUM:** Enable RUST_LOG=trace and retry
5. **LOW:** Check ac-ffmpeg examples for initialization patterns

## References

- **Phase 2:** Passthrough mode worked perfectly (6.77 Mbps, 0.06% CPU)
- **Compilation:** All 25 initial errors fixed, compiles cleanly
- **API Research:** User-provided ac-ffmpeg 0.19 examples used correctly
