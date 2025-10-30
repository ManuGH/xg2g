# Phase 4: Native Audio Remuxing - Implementation Plan

## Overview

Phase 4 implements the core native audio remuxing pipeline in Rust, replacing ffmpeg-based audio transcoding with a high-performance, low-latency native solution.

**Goal:** Native MP2/AC3 → AAC remuxing with <50ms latency and <5% CPU usage.

**Timeline:** 3-4 weeks

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    Native Audio Remuxing Pipeline                │
├─────────────────────────────────────────────────────────────────┤
│                                                                   │
│  Input MPEG-TS Stream (MP2/AC3 Audio)                           │
│         ↓                                                        │
│  ┌──────────────────┐                                           │
│  │  MPEG-TS Demuxer │  ← mpeg2ts-reader                        │
│  │  Extract PES     │                                           │
│  └────────┬─────────┘                                           │
│           ↓                                                      │
│  ┌──────────────────┐                                           │
│  │  Audio Decoder   │  ← Symphonia (MP2) / FFmpeg bindings     │
│  │  MP2/AC3 → PCM   │     (AC3) / dasp                         │
│  └────────┬─────────┘                                           │
│           ↓                                                      │
│  ┌──────────────────┐                                           │
│  │  PCM Buffer      │  ← Ring buffer for smooth streaming      │
│  │  (Intermediate)  │                                           │
│  └────────┬─────────┘                                           │
│           ↓                                                      │
│  ┌──────────────────┐                                           │
│  │  AAC Encoder     │  ← opus (fallback) / FFmpeg AAC          │
│  │  PCM → AAC       │     / External fdk-aac                   │
│  └────────┬─────────┘                                           │
│           ↓                                                      │
│  ┌──────────────────┐                                           │
│  │  MPEG-TS Muxer   │  ← mpeg2ts-writer / Custom               │
│  │  Package AAC     │                                           │
│  └────────┬─────────┘                                           │
│           ↓                                                      │
│  Output MPEG-TS Stream (AAC Audio)                              │
│                                                                   │
└─────────────────────────────────────────────────────────────────┘
```

## Implementation Stages

### Stage 1: Audio Codec Research & Dependencies (Days 1-2)

**Goal:** Evaluate and select the best Rust audio libraries for our use case.

**Tasks:**
1. Research MP2 decoder options:
   - ✅ Symphonia (pure Rust, good MP2 support)
   - FFmpeg bindings (ac-ffmpeg, ffmpeg-next)
   - Custom MP2 decoder

2. Research AC3 decoder options:
   - Symphonia (experimental AC3 support)
   - ✅ FFmpeg bindings (best AC3 support)
   - External libavcodec bindings

3. Research AAC encoder options:
   - opus crate (Opus codec, iOS compatible fallback)
   - ✅ FFmpeg AAC encoder (libavcodec)
   - fdk-aac-rs (high quality, system lib required)
   - External fdk-aac via C FFI

4. Select optimal combination based on:
   - Quality (AAC-LC compatibility with iOS)
   - Performance (CPU usage, latency)
   - Ease of integration (pure Rust vs FFI)
   - License compatibility
   - Binary size

**Deliverable:** Updated `Cargo.toml` with selected dependencies

### Stage 2: MPEG-TS Demuxer Implementation (Days 3-5)

**Goal:** Parse incoming MPEG-TS stream and extract audio PES packets.

**Files:**
- `transcoder/src/demux.rs` (NEW)
- `transcoder/src/audio_remux.rs` (UPDATE)

**Implementation:**

```rust
// transcoder/src/demux.rs

use mpeg2ts_reader::prelude::*;
use anyhow::Result;

/// MPEG-TS Demuxer
/// Extracts audio Packetized Elementary Stream (PES) from Transport Stream
pub struct TsDemuxer {
    demuxer: mpeg2ts_reader::demuxer::Demuxer<DemuxerContext>,
    audio_pid: Option<u16>,
}

impl TsDemuxer {
    pub fn new() -> Self {
        // Initialize mpeg2ts-reader demuxer
        // Auto-detect audio PID from PMT
    }

    /// Process MPEG-TS packet (188 bytes)
    /// Returns audio PES data if packet contains audio
    pub fn process_packet(&mut self, ts_packet: &[u8; 188]) -> Result<Option<Vec<u8>>> {
        // 1. Parse TS packet header
        // 2. Check if PID matches audio PID
        // 3. Extract payload
        // 4. Reassemble PES packets
        // 5. Return complete audio frames
    }

    /// Get detected audio codec type
    pub fn audio_codec(&self) -> Option<AudioCodec> {
        // Return detected codec (MP2, AC3, AAC, etc.)
    }
}

#[derive(Debug, Clone, Copy)]
pub enum AudioCodec {
    Mp2,
    Ac3,
    Aac,
}
```

**Key Challenges:**
- PES packet reassembly (spanning multiple TS packets)
- Audio PID auto-detection from PMT
- Handling scrambled streams (skip or error)
- Timestamp extraction (PTS/DTS)

**Testing:**
- Unit tests with sample TS packets
- Integration test with real DVB stream capture

### Stage 3: Audio Decoder Implementation (Days 6-10)

**Goal:** Decode MP2/AC3 audio to PCM samples.

**Files:**
- `transcoder/src/decoder.rs` (NEW)
- `transcoder/src/audio_remux.rs` (UPDATE)

**Implementation:**

```rust
// transcoder/src/decoder.rs

use anyhow::Result;

/// Audio decoder trait
/// Decodes compressed audio to PCM samples
pub trait AudioDecoder: Send {
    /// Decode audio frame to PCM
    /// Returns interleaved PCM samples (f32, -1.0 to 1.0)
    fn decode(&mut self, data: &[u8]) -> Result<Vec<f32>>;

    /// Get sample rate
    fn sample_rate(&self) -> u32;

    /// Get channel count
    fn channels(&self) -> u8;

    /// Reset decoder state
    fn reset(&mut self);
}

/// MP2 Decoder using Symphonia
pub struct Mp2Decoder {
    // Symphonia codec instance
    // Format reader
    // Decoder state
}

impl AudioDecoder for Mp2Decoder {
    fn decode(&mut self, data: &[u8]) -> Result<Vec<f32>> {
        // 1. Feed data to Symphonia
        // 2. Decode to PCM samples
        // 3. Convert to f32 range [-1.0, 1.0]
        // 4. Return interleaved samples
    }
}

/// AC3 Decoder using FFmpeg
pub struct Ac3Decoder {
    // FFmpeg codec context
    // Decoder state
}

impl AudioDecoder for Ac3Decoder {
    fn decode(&mut self, data: &[u8]) -> Result<Vec<f32>> {
        // 1. Feed data to FFmpeg decoder
        // 2. Decode to PCM samples
        // 3. Convert to f32 range
        // 4. Handle multi-channel downmix if needed
    }
}

/// Auto-detecting decoder wrapper
pub struct AutoDecoder {
    decoder: Box<dyn AudioDecoder>,
}

impl AutoDecoder {
    pub fn new(codec: AudioCodec) -> Result<Self> {
        let decoder: Box<dyn AudioDecoder> = match codec {
            AudioCodec::Mp2 => Box::new(Mp2Decoder::new()?),
            AudioCodec::Ac3 => Box::new(Ac3Decoder::new()?),
            _ => anyhow::bail!("Unsupported codec: {:?}", codec),
        };
        Ok(Self { decoder })
    }
}
```

**Key Challenges:**
- Sample format conversion (i16/i32/f32)
- Channel mapping and downmixing (5.1 → stereo)
- Sample rate conversion (if needed)
- Buffer management for partial frames
- Error recovery (corrupted frames)

**Testing:**
- Unit tests with known audio samples
- Decode test files (MP2, AC3)
- Verify output quality (SNR, frequency response)

### Stage 4: AAC Encoder Implementation (Days 11-15)

**Goal:** Encode PCM samples to AAC-LC format (iOS compatible).

**Files:**
- `transcoder/src/encoder.rs` (NEW)
- `transcoder/src/audio_remux.rs` (UPDATE)

**Implementation:**

```rust
// transcoder/src/encoder.rs

use anyhow::Result;

/// AAC encoder configuration
#[derive(Debug, Clone)]
pub struct AacEncoderConfig {
    pub sample_rate: u32,   // 8000-96000 Hz
    pub channels: u8,        // 1-8 channels
    pub bitrate: u32,        // bits per second
    pub profile: AacProfile, // AAC-LC, HE-AAC, etc.
}

#[derive(Debug, Clone, Copy)]
pub enum AacProfile {
    AacLc,    // AAC Low Complexity (iOS compatible)
    HeAac,    // High Efficiency AAC
    HeAacV2,  // HE-AAC v2
}

/// AAC encoder trait
pub trait AacEncoder: Send {
    /// Encode PCM samples to AAC
    /// Input: interleaved f32 PCM samples [-1.0, 1.0]
    /// Output: AAC frames (ADTS or raw)
    fn encode(&mut self, pcm: &[f32]) -> Result<Vec<u8>>;

    /// Get frame size (samples per channel)
    fn frame_size(&self) -> usize;

    /// Flush encoder (encode remaining samples)
    fn flush(&mut self) -> Result<Vec<u8>>;
}

/// FFmpeg AAC encoder
pub struct FfmpegAacEncoder {
    // FFmpeg codec context
    // Encoder state
    config: AacEncoderConfig,
}

impl AacEncoder for FfmpegAacEncoder {
    fn encode(&mut self, pcm: &[f32]) -> Result<Vec<u8>> {
        // 1. Convert f32 to encoder input format
        // 2. Feed to FFmpeg encoder
        // 3. Retrieve encoded AAC frames
        // 4. Add ADTS headers (for MPEG-TS)
    }

    fn frame_size(&self) -> usize {
        1024 // AAC frame size
    }
}

/// Opus encoder (fallback)
pub struct OpusEncoder {
    // Opus encoder instance
    config: AacEncoderConfig,
}

impl AacEncoder for OpusEncoder {
    fn encode(&mut self, pcm: &[f32]) -> Result<Vec<u8>> {
        // Opus encoding (iOS compatible via WebRTC)
        // Lower quality fallback option
    }
}
```

**Key Challenges:**
- AAC-LC profile configuration (iOS compatibility)
- ADTS header generation
- Bitrate control (CBR/VBR)
- Latency optimization (frame size)
- Quality vs. performance tradeoff

**Testing:**
- Encode test PCM files
- Verify iOS Safari playback
- Measure encoding latency
- Quality assessment (PESQ, POLQA)

### Stage 5: MPEG-TS Muxer Implementation (Days 16-18)

**Goal:** Package AAC frames into MPEG-TS packets.

**Files:**
- `transcoder/src/muxer.rs` (NEW)
- `transcoder/src/audio_remux.rs` (UPDATE)

**Implementation:**

```rust
// transcoder/src/muxer.rs

use anyhow::Result;

/// MPEG-TS Muxer configuration
#[derive(Debug, Clone)]
pub struct TsMuxerConfig {
    pub audio_pid: u16,    // Audio PID (default: 256)
    pub pcr_pid: u16,      // PCR PID (default: 256)
    pub pmt_pid: u16,      // PMT PID (default: 4096)
    pub video_pid: u16,    // Video PID (passthrough)
}

/// MPEG-TS Muxer
/// Packages AAC audio into Transport Stream packets
pub struct TsMuxer {
    config: TsMuxerConfig,
    continuity_counter: u8,
    pts_offset: u64,
}

impl TsMuxer {
    pub fn new(config: TsMuxerConfig) -> Self {
        Self {
            config,
            continuity_counter: 0,
            pts_offset: 0,
        }
    }

    /// Mux AAC frame into TS packets
    /// Returns one or more 188-byte TS packets
    pub fn mux_audio(&mut self, aac_data: &[u8], pts: u64, dts: u64) -> Result<Vec<[u8; 188]>> {
        // 1. Create PES packet from AAC data
        // 2. Add PTS/DTS timestamps
        // 3. Fragment PES into TS packets (188 bytes)
        // 4. Add TS headers (sync, PID, continuity)
        // 5. Return vector of TS packets
    }

    /// Mux video packet (passthrough)
    pub fn mux_video_passthrough(&mut self, ts_packet: &[u8; 188]) -> Result<[u8; 188]> {
        // Pass through video packets unchanged
        // Update continuity counter for video PID
    }

    /// Generate PAT (Program Association Table)
    pub fn generate_pat(&mut self) -> [u8; 188] {
        // Create PAT packet
    }

    /// Generate PMT (Program Map Table)
    pub fn generate_pmt(&mut self) -> [u8; 188] {
        // Create PMT packet
        // List video and AAC audio streams
    }
}
```

**Key Challenges:**
- PES packet creation
- Timestamp management (PTS/DTS wrapping)
- TS packet fragmentation
- PAT/PMT table generation
- Continuity counter management
- PCR generation (for timing)

**Testing:**
- Validate TS packet structure
- ffprobe validation
- iOS Safari playback test
- VLC playback test

### Stage 6: Complete Pipeline Integration (Days 19-21)

**Goal:** Connect all components into a complete remuxing pipeline.

**Files:**
- `transcoder/src/audio_remux.rs` (MAJOR UPDATE)
- `transcoder/src/ffi.rs` (UPDATE)

**Implementation:**

```rust
// transcoder/src/audio_remux.rs

use crate::{demux::*, decoder::*, encoder::*, muxer::*};
use anyhow::Result;

/// Complete audio remuxing pipeline
pub struct AudioRemuxer {
    config: AudioRemuxConfig,
    demuxer: TsDemuxer,
    decoder: Option<AutoDecoder>,
    encoder: Box<dyn AacEncoder>,
    muxer: TsMuxer,

    // Buffer management
    pcm_buffer: Vec<f32>,

    // Statistics
    stats: RemuxStats,
}

impl AudioRemuxer {
    pub fn new(config: AudioRemuxConfig) -> Self {
        // Initialize all components
    }

    /// Remux MPEG-TS stream (main entry point)
    /// Input: MPEG-TS with MP2/AC3 audio
    /// Output: MPEG-TS with AAC audio
    pub async fn remux<R, W>(&self, mut input: R, mut output: W) -> Result<()>
    where
        R: Read + Send,
        W: std::io::Write + Send,
    {
        let mut ts_packet = [0u8; 188];
        let mut output_packets = Vec::new();

        loop {
            // 1. Read TS packet from input
            if input.read_exact(&mut ts_packet).is_err() {
                break; // End of stream
            }

            // 2. Check packet type
            let pid = Self::extract_pid(&ts_packet);

            if pid == self.demuxer.audio_pid {
                // AUDIO PACKET - Process through pipeline

                // 2a. Demux: Extract PES data
                if let Some(pes_data) = self.demuxer.process_packet(&ts_packet)? {

                    // 2b. Decode: MP2/AC3 → PCM
                    let decoder = self.decoder.as_mut().ok_or_else(|| anyhow::anyhow!("No decoder"))?;
                    let pcm = decoder.decode(&pes_data)?;

                    // 2c. Buffer PCM (accumulate to encoder frame size)
                    self.pcm_buffer.extend(pcm);

                    // 2d. Encode: PCM → AAC (when buffer full)
                    while self.pcm_buffer.len() >= self.encoder.frame_size() * self.config.channels as usize {
                        let frame = self.pcm_buffer.drain(..frame_size).collect::<Vec<_>>();
                        let aac = self.encoder.encode(&frame)?;

                        // 2e. Mux: AAC → TS packets
                        let pts = self.calculate_pts();
                        let dts = pts;
                        let ts_packets = self.muxer.mux_audio(&aac, pts, dts)?;
                        output_packets.extend(ts_packets);
                    }
                }
            } else {
                // VIDEO/OTHER PACKET - Passthrough
                output_packets.push(ts_packet);
            }

            // 3. Write output packets
            for packet in output_packets.drain(..) {
                output.write_all(&packet)?;
            }

            // 4. Update stats
            self.stats.packets_processed += 1;
        }

        // Flush remaining data
        let remaining_aac = self.encoder.flush()?;
        // ... mux and write remaining packets

        Ok(())
    }

    fn extract_pid(ts_packet: &[u8; 188]) -> u16 {
        (((ts_packet[1] & 0x1F) as u16) << 8) | (ts_packet[2] as u16)
    }

    fn calculate_pts(&self) -> u64 {
        // Calculate PTS based on processed samples
        // Ensure monotonic increase
    }
}

/// Remuxing statistics
#[derive(Debug, Default)]
pub struct RemuxStats {
    pub packets_processed: u64,
    pub audio_frames_decoded: u64,
    pub audio_frames_encoded: u64,
    pub bytes_input: u64,
    pub bytes_output: u64,
    pub latency_ms: f64,
}
```

**Key Challenges:**
- Pipeline orchestration
- Buffer management across stages
- Timestamp synchronization
- Error propagation
- Performance optimization (minimize copies)
- Async/streaming architecture

**Testing:**
- End-to-end remuxing test
- Performance profiling
- Memory leak detection
- Stress testing (long streams)

### Stage 7: Testing & Optimization (Days 22-28)

**Goal:** Validate correctness, measure performance, optimize bottlenecks.

**Tasks:**

1. **Correctness Testing:**
   - Unit tests for each component
   - Integration tests with real DVB streams
   - iOS Safari playback validation
   - Audio quality assessment

2. **Performance Testing:**
   - CPU usage measurement
   - Memory profiling
   - Latency measurement (target: <50ms)
   - Throughput testing (target: 500+ Mbps)

3. **Optimization:**
   - Profile with `perf` / `flamegraph`
   - Optimize hot paths
   - Reduce allocations (use buffers)
   - SIMD optimization (if applicable)
   - Multi-threading (parallel streams)

4. **Error Handling:**
   - Corrupted stream recovery
   - Codec errors
   - Buffer overflow/underflow
   - Graceful degradation

5. **Documentation:**
   - API documentation
   - Architecture diagrams
   - Performance benchmarks
   - Usage examples

**Deliverables:**
- Test suite with 80%+ coverage
- Performance report
- Optimization recommendations

## Dependencies

### Required Crates

```toml
[dependencies]
# MPEG-TS Processing
mpeg2ts-reader = "0.18"  # Demuxer
mpeg2ts-writer = "0.1"   # Muxer (if available, else custom)

# Audio Decoding
symphonia = { version = "0.5", features = ["mp2"] }
ac-ffmpeg = { version = "0.19", features = ["audio"] }  # AC3 via FFmpeg

# Audio Encoding
ac-ffmpeg = { version = "0.19", features = ["audio"] }  # AAC via FFmpeg
# OR
opus = "0.3"  # Fallback encoder

# Audio Processing
dasp = "0.11"  # Sample rate conversion, mixing
rubato = "0.15"  # High-quality resampling

# Performance
rayon = "1.8"  # Parallel processing
crossbeam = "0.8"  # Concurrent data structures
```

### System Dependencies

- **FFmpeg libraries** (if using FFmpeg-based codecs):
  ```bash
  # Debian/Ubuntu
  sudo apt-get install libavcodec-dev libavformat-dev libavutil-dev libswresample-dev

  # macOS
  brew install ffmpeg
  ```

## Performance Targets

| Metric | Target | Measured |
|--------|--------|----------|
| Latency | <50ms | TBD |
| CPU Usage | <5% | TBD |
| Memory | <30MB | TBD |
| Throughput | 500+ Mbps | TBD |
| Audio Quality | >4.0 MOS | TBD |

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| AC3 decoder not available in Rust | High | Use FFmpeg bindings |
| AAC encoder quality insufficient | High | Use FFmpeg libavcodec or fdk-aac |
| Latency too high (>100ms) | Medium | Optimize buffer sizes, use smaller frames |
| iOS Safari incompatibility | High | Validate early with AAC-LC profile |
| CPU usage too high (>10%) | Medium | Profile and optimize, consider HW accel |
| Memory leaks in long-running streams | Medium | Strict testing, Valgrind/ASAN |

## Success Criteria

Phase 4 is considered complete when:

- ✅ Native remuxing pipeline is fully functional
- ✅ iOS Safari can play remuxed streams without delay
- ✅ Latency is <50ms (end-to-end)
- ✅ CPU usage is <5% (single stream)
- ✅ Audio quality is perceptually lossless (>4.0 MOS)
- ✅ All tests pass (unit, integration, e2e)
- ✅ Documentation is complete

## Next Steps After Phase 4

- **Phase 5:** Integration into xg2g proxy pipeline
- **Phase 6:** Production deployment and monitoring
- **Phase 7:** Hardware acceleration (VAAPI for video)
