# Phase 5: Real Codec Implementation Plan

**Status:** Planning
**Prerequisites:** Phase 2 Complete ✅
**Goal:** Replace passthrough with real AC3 decoder + AAC-LC encoder
**Estimated Duration:** 10-16 days

---

## Overview

Phase 2 delivered a fully functional native audio remuxing pipeline with **passthrough mode**. All infrastructure is in place:

- ✅ MPEG-TS demuxing
- ✅ Pipeline architecture
- ✅ FFI integration (Go ↔ Rust)
- ✅ MPEG-TS muxing
- ✅ Zero-latency streaming
- ✅ Real stream testing (ORF1 HD: AC3 5.1 → Passthrough)

**Phase 5 Focus:** Implement real codecs to transform `AC3 5.1 → PCM → AAC-LC Stereo`

---

## Current State Analysis

### What Works (Phase 2)
```
Input: MPEG-TS with AC3 5.1 (448kbps, 6ch, 48kHz)
  ↓
[Demuxer] ✅ Extracts AC3 PES packets
  ↓
[Decoder] ⚠️ Passthrough (returns silent PCM)
  ↓
[Encoder] ⚠️ Passthrough (generates placeholder AAC)
  ↓
[Muxer] ✅ Creates MPEG-TS with AAC
  ↓
Output: Valid MPEG-TS (plays in VLC)
```

### Performance Baseline (Passthrough)
- **Throughput:** 6.77 Mbps
- **CPU:** 0.06% average (83x better than FFmpeg)
- **Memory:** 20 MB
- **Latency:** <50ms
- **Stability:** Zero crashes in 30s test

### What Needs Implementation
1. **AC3 Decoder:** Decode AC3 5.1 → PCM stereo (with downmix)
2. **AAC-LC Encoder:** Encode PCM → AAC-LC + ADTS headers
3. **Integration Testing:** Validate end-to-end with real streams

---

## Dependencies

### Already in Cargo.toml ✅
```toml
ac-ffmpeg = "0.19"          # FFmpeg bindings for codecs
mpeg2ts-reader = "0.18"     # MPEG-TS demuxer
symphonia = { version = "0.5", features = ["mp2"] }  # MP2 decoder
dasp = "0.11"               # Sample format conversion, channel mixing
rubato = "0.15"             # Sample rate conversion
```

### ac-ffmpeg API (Version 0.19)
- **Decoder:** `ac_ffmpeg::codec::Decoder`
- **Encoder:** `ac_ffmpeg::codec::Encoder`
- **Methods:** `send_packet()`, `receive_frame()`, `send_frame()`, `receive_packet()`

---

## Implementation Plan

### Step 1: AC3 Decoder (AC3 → PCM) - 3-5 days

#### 1.1 Research & Design (1 day)
- [ ] Study ac-ffmpeg `Decoder` API in detail
- [ ] Understand `Input::open()` vs raw packet decoding
- [ ] Design decoder initialization strategy
- [ ] Plan error handling approach

#### 1.2 Core Decoder (2 days)
**File:** `transcoder/src/decoder.rs` (replace `Ac3Decoder` impl)

```rust
pub struct Ac3Decoder {
    decoder: Option<ac_ffmpeg::codec::Decoder>,
    sample_rate: u32,
    channels: u16,
    frames_decoded: u64,
}

impl AudioDecoder for Ac3Decoder {
    fn decode(&mut self, data: &[u8]) -> Result<Vec<PcmSample>> {
        // 1. Initialize decoder on first call
        if self.decoder.is_none() {
            self.init_decoder()?;
        }

        // 2. Create packet from raw AC3 PES data
        let packet = self.create_packet(data)?;

        // 3. Send to decoder
        self.decoder.send_packet(&packet)?;

        // 4. Receive decoded frames
        let mut pcm_samples = Vec::new();
        while let Ok(frame) = self.decoder.receive_frame() {
            // Convert frame to f32 PCM
            let samples = self.frame_to_pcm(&frame)?;
            pcm_samples.extend(samples);
        }

        // 5. Downmix 5.1 to stereo if needed
        let stereo = self.downmix_if_needed(pcm_samples)?;

        Ok(stereo)
    }
}
```

**Key Challenges:**
- Raw AC3 bytes → `ac_ffmpeg::Packet` conversion
- Sample format handling (FLTP, S16P, etc.)
- Planar → Interleaved conversion

#### 1.3 Channel Downmixing (1 day)
- [ ] Implement 5.1 → Stereo downmix algorithm
- [ ] Standard coefficients: `L = FL + 0.7*FC + 0.5*BL`
- [ ] Handle mono, stereo, 5.1, 7.1 layouts
- [ ] Normalization to prevent clipping

#### 1.4 Testing (1 day)
- [ ] Unit tests with synthetic AC3 data
- [ ] Integration test with real AC3 PES packets
- [ ] Validate PCM output correctness
- [ ] Performance benchmarking

---

### Step 2: AAC-LC Encoder (PCM → AAC) - 3-5 days

#### 2.1 Research & Design (1 day)
- [ ] Study ac-ffmpeg `Encoder` API
- [ ] Understand AAC-LC profile configuration
- [ ] Research ADTS header generation
- [ ] Plan frame buffering strategy

#### 2.2 Core Encoder (2 days)
**File:** `transcoder/src/encoder.rs` (replace `FfmpegAacEncoder` impl)

```rust
pub struct FfmpegAacEncoder {
    encoder: ac_ffmpeg::codec::Encoder,
    config: AacEncoderConfig,
    sample_buffer: Vec<f32>,
    frames_encoded: u64,
}

impl AacEncoder for FfmpegAacEncoder {
    fn encode(&mut self, pcm: &[f32]) -> Result<Vec<u8>> {
        // 1. Buffer PCM samples (need 1024 samples/channel)
        self.sample_buffer.extend_from_slice(pcm);

        let frame_size = 1024 * self.config.channels as usize;
        let mut output = Vec::new();

        // 2. Process complete frames
        while self.sample_buffer.len() >= frame_size {
            let frame_data = self.sample_buffer.drain(..frame_size).collect();
            let pcm_frame = self.create_audio_frame(frame_data)?;

            // 3. Send to encoder
            self.encoder.send_frame(&pcm_frame)?;

            // 4. Receive encoded packets
            while let Ok(packet) = self.encoder.receive_packet() {
                // 5. Add ADTS header
                let aac_with_adts = self.add_adts_header(&packet)?;
                output.extend(aac_with_adts);
            }
        }

        Ok(output)
    }
}
```

**Key Challenges:**
- PCM f32 → `ac_ffmpeg::AudioFrame` conversion
- AAC-LC profile configuration
- ADTS header generation (7 bytes)

#### 2.3 ADTS Header Generation (1 day)
- [ ] Implement ADTS header builder (already exists in `encoder.rs`)
- [ ] Validate header structure
- [ ] Test with iOS Safari (ADTS requirement)

#### 2.4 Testing (1 day)
- [ ] Unit tests with synthetic PCM data
- [ ] Validate AAC output format
- [ ] Test ADTS header correctness
- [ ] iOS Safari compatibility check

---

### Step 3: Integration & End-to-End Testing - 2-3 days

#### 3.1 Pipeline Integration (1 day)
- [ ] Update `AudioRemuxer` to use new codecs
- [ ] Remove passthrough warnings
- [ ] Verify FFI still works correctly
- [ ] Test with Go integration

#### 3.2 Real Stream Testing (1 day)
**Test Environment:** LXC Container + Enigma2 Uno4K

```bash
# Test with ORF1 HD (AC3 5.1 → AAC Stereo)
curl http://10.10.55.14:18001/1:0:19:132F:3EF:1:C00000:0:0:0: -o /tmp/aac_output.ts

# Validate output
ffprobe /tmp/aac_output.ts  # Should show AAC-LC audio
ffplay /tmp/aac_output.ts   # Should play with audio
```

**Test Cases:**
- [ ] ORF1 HD (AC3 5.1 @ 448kbps)
- [ ] SD channel (MP2 @ 192kbps) - if available
- [ ] Different bitrates and sample rates
- [ ] Long-duration stream (5+ minutes)

#### 3.3 iOS Safari Testing (1 day)
- [ ] Generate M3U8 playlist
- [ ] Test on iOS 16, 17, 18
- [ ] Verify audio playback
- [ ] Check audio/video sync
- [ ] Monitor for glitches or dropouts

---

### Step 4: Performance Optimization - 2-3 days

#### 4.1 Benchmarking (1 day)
Compare Phase 5 (real codecs) vs Phase 2 (passthrough):

| Metric | Phase 2 (Passthrough) | Phase 5 (Real Codecs) | Target |
|--------|----------------------|----------------------|--------|
| CPU | 0.06% | ? | <5% |
| Memory | 20 MB | ? | <30 MB |
| Latency | <50ms | ? | <200ms |
| Throughput | 6.77 Mbps | ? | >5 Mbps |

#### 4.2 Profiling (1 day)
- [ ] Profile with `perf` on Linux
- [ ] Identify hot paths
- [ ] Measure codec overhead
- [ ] Check for memory allocations

#### 4.3 Optimization (1 day)
**Potential optimizations:**
- Zero-copy buffer management
- SIMD for downmixing
- Buffer pool for frames
- Reduce allocations in hot path

---

### Step 5: Documentation & Deployment - 1-2 days

#### 5.1 Documentation (1 day)
- [ ] Update `ARCHITECTURE.md` with codec details
- [ ] Document AC3 decoder implementation
- [ ] Document AAC encoder + ADTS headers
- [ ] Update performance benchmarks
- [ ] iOS Safari compatibility notes

#### 5.2 Deployment (1 day)
- [ ] Build release binary on LXC
- [ ] Update systemd service (if applicable)
- [ ] Update environment variables
- [ ] Test in production environment
- [ ] Monitor for issues

---

## Success Criteria

### Functional Requirements
- [ ] AC3 5.1 decodes to PCM stereo without errors
- [ ] AAC-LC encodes with valid ADTS headers
- [ ] Output plays correctly in VLC, FFplay
- [ ] **iOS Safari plays audio without glitches** ⭐
- [ ] No crashes or memory leaks in 1-hour test

### Performance Requirements
- [ ] CPU usage <5% (currently 0.06% passthrough)
- [ ] Memory usage <30 MB (currently 20 MB)
- [ ] Latency <200ms (currently <50ms)
- [ ] Throughput >5 Mbps (currently 6.77 Mbps)

### Quality Requirements
- [ ] Audio quality: No artifacts, clear speech
- [ ] A/V sync: No drift or desync
- [ ] Channel downmix: Proper stereo image
- [ ] Bitrate: Target 192kbps AAC achieved

---

## Risk Assessment

### High Risk
**Issue:** ac-ffmpeg API complexity - may require multiple iterations to get right
**Mitigation:** Start with simple test cases, iterate on LXC with fast compile times

**Issue:** Sample format conversion edge cases (planar vs interleaved, different bit depths)
**Mitigation:** Study FFmpeg docs, test with multiple input formats

### Medium Risk
**Issue:** Performance degradation vs passthrough
**Mitigation:** Profile early, optimize hot paths, consider SIMD

**Issue:** iOS Safari ADTS header compatibility
**Mitigation:** Follow ADTS spec exactly, test early with iOS devices

### Low Risk
**Issue:** Integration with existing pipeline
**Mitigation:** Well-defined interfaces already exist, minimal changes needed

---

## Alternatives Considered

### Option A: Continue with Passthrough (REJECTED)
**Pros:** Works now, zero risk
**Cons:** No iOS Safari support, no real transcoding

### Option B: Use FFmpeg subprocess (REJECTED)
**Pros:** Proven, well-tested
**Cons:** High latency (200-500ms), high CPU (15-20%), defeats purpose of Phase 2

### Option C: ac-ffmpeg integration (SELECTED ✅)
**Pros:** Native performance, low latency, iOS compatible
**Cons:** API complexity, requires careful implementation

---

## Next Steps

1. **Schedule kickoff meeting** to align on timeline
2. **Set up development environment** on LXC container
3. **Start with Step 1.1** (AC3 decoder research)
4. **Daily progress updates** in docs/PHASE_5_PROGRESS.md
5. **Weekly demos** of working components

---

## References

- [ac-ffmpeg crate](https://crates.io/crates/ac-ffmpeg)
- [ac-ffmpeg docs](https://docs.rs/ac-ffmpeg/0.19.0)
- [FFmpeg AAC encoder](https://trac.ffmpeg.org/wiki/Encode/AAC)
- [ADTS header spec](https://wiki.multimedia.cx/index.php/ADTS)
- [Phase 2 results](./REAL_STREAM_TEST_RESULTS.md)

---

**Document Version:** 1.0
**Created:** 2025-10-29
**Author:** Claude Code (AI Assistant)
**Status:** Ready for Review
