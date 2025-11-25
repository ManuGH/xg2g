# Phase 5: Code Examples for ac-ffmpeg Integration

**Purpose:** Reference implementations for AC3 decoder and AAC encoder
**Based on:** ac-ffmpeg 0.19 API + raw PES payload workflow

---

## 1. AC3 Decoder (Raw PES Payload → PCM)

### Overview
Our use case: Extract AC3 data from MPEG-TS PES packets → Decode to PCM

**Key Insight:** We work with raw PES payloads, NOT full container files.
Therefore we **don't use** `Input::open()`, but instead:
1. Initialize decoder with `CodecParameters`
2. Create `Packet` from raw AC3 bytes
3. Use `send_packet()` / `receive_frame()` loop

### Implementation

```rust
use ac_ffmpeg::codec::Decoder;
use ac_ffmpeg::codec::packet::Packet;
use ac_ffmpeg::codec::CodecParameters;
use anyhow::{Context, Result};

pub struct Ac3Decoder {
    /// FFmpeg decoder (lazy-initialized)
    decoder: Option<Decoder>,

    /// Sample rate (detected from stream)
    sample_rate: u32,

    /// Output channels (always 2 for stereo downmix)
    channels: u16,

    /// Statistics
    frames_decoded: u64,
}

impl Ac3Decoder {
    pub fn new() -> Result<Self> {
        Ok(Self {
            decoder: None,
            sample_rate: 48000,
            channels: 2,
            frames_decoded: 0,
        })
    }

    /// Initialize decoder on first call
    fn init_decoder(&mut self) -> Result<()> {
        if self.decoder.is_some() {
            return Ok(());
        }

        // Create codec parameters for AC3
        let codec_params = CodecParameters::builder()
            .codec_id(ac_ffmpeg::codec::Id::AC3)
            .sample_rate(48000)  // Default, will be updated from stream
            .channels(6)         // AC3 5.1 input
            .build();

        // Create decoder
        let decoder = Decoder::new(&codec_params)
            .context("Failed to create AC3 decoder")?;

        self.decoder = Some(decoder);

        Ok(())
    }

    /// Decode AC3 PES payload to PCM samples
    pub fn decode(&mut self, ac3_pes_data: &[u8]) -> Result<Vec<f32>> {
        // Ensure decoder is initialized
        if self.decoder.is_none() {
            self.init_decoder()?;
        }

        let decoder = self.decoder.as_mut().unwrap();

        // Step 1: Create packet from raw AC3 bytes
        // This is the key difference from file-based decoding!
        let packet = Packet::new(ac3_pes_data.len());

        // Copy AC3 data into packet
        unsafe {
            std::ptr::copy_nonoverlapping(
                ac3_pes_data.as_ptr(),
                packet.data_mut().as_mut_ptr(),
                ac3_pes_data.len(),
            );
        }

        // Step 2: Send packet to decoder
        decoder.send_packet(&packet)
            .context("Failed to send packet to AC3 decoder")?;

        let mut all_samples = Vec::new();

        // Step 3: Receive all decoded frames
        // Note: One packet can produce multiple frames!
        loop {
            match decoder.receive_frame() {
                Ok(frame) => {
                    // Update sample rate from actual stream
                    self.sample_rate = frame.sample_rate() as u32;

                    // Convert frame to PCM f32 samples
                    let pcm = self.frame_to_pcm(&frame)?;

                    // Downmix to stereo if needed
                    let stereo = self.downmix_to_stereo(pcm, frame.channels() as usize);

                    all_samples.extend(stereo);
                    self.frames_decoded += 1;
                }
                Err(ac_ffmpeg::Error::Again) => {
                    // Need more data - normal for fragmented packets
                    break;
                }
                Err(e) => {
                    return Err(e).context("Failed to receive frame from AC3 decoder");
                }
            }
        }

        Ok(all_samples)
    }

    /// Convert audio frame to f32 PCM samples (interleaved)
    fn frame_to_pcm(&self, frame: &ac_ffmpeg::codec::AudioFrame) -> Result<Vec<f32>> {
        let channels = frame.channels() as usize;
        let samples_per_channel = frame.samples() as usize;
        let total_samples = samples_per_channel * channels;

        let mut pcm = Vec::with_capacity(total_samples);

        // Get sample format
        let format = frame.format();

        match format {
            // Planar float (most common for AC3)
            ac_ffmpeg::codec::AudioSampleFormat::FloatPlanar => {
                for sample_idx in 0..samples_per_channel {
                    for ch in 0..channels {
                        let sample = frame.plane::<f32>(ch)[sample_idx];
                        pcm.push(sample);
                    }
                }
            }

            // Interleaved float
            ac_ffmpeg::codec::AudioSampleFormat::Float => {
                let data = frame.plane::<f32>(0);
                pcm.extend_from_slice(&data[..total_samples]);
            }

            // Planar s16
            ac_ffmpeg::codec::AudioSampleFormat::S16Planar => {
                for sample_idx in 0..samples_per_channel {
                    for ch in 0..channels {
                        let sample = frame.plane::<i16>(ch)[sample_idx];
                        pcm.push(sample as f32 / 32768.0);
                    }
                }
            }

            // Interleaved s16
            ac_ffmpeg::codec::AudioSampleFormat::S16 => {
                let data = frame.plane::<i16>(0);
                for &sample in &data[..total_samples] {
                    pcm.push(sample as f32 / 32768.0);
                }
            }

            _ => anyhow::bail!("Unsupported sample format: {:?}", format),
        }

        Ok(pcm)
    }

    /// Downmix multi-channel to stereo
    fn downmix_to_stereo(&self, samples: Vec<f32>, input_channels: usize) -> Vec<f32> {
        if input_channels == 2 {
            return samples; // Already stereo
        }

        if input_channels == 1 {
            // Mono to stereo: duplicate
            let mut stereo = Vec::with_capacity(samples.len() * 2);
            for sample in samples {
                stereo.push(sample);
                stereo.push(sample);
            }
            return stereo;
        }

        // 5.1 to stereo downmix
        // Layout: FL, FR, FC, LFE, BL, BR
        let frame_count = samples.len() / input_channels;
        let mut stereo = Vec::with_capacity(frame_count * 2);

        for frame_idx in 0..frame_count {
            let base = frame_idx * input_channels;

            let fl = samples[base];
            let fr = samples[base + 1];
            let fc = samples.get(base + 2).copied().unwrap_or(0.0);
            let bl = samples.get(base + 4).copied().unwrap_or(0.0);
            let br = samples.get(base + 5).copied().unwrap_or(0.0);

            // Downmix formula
            let left = fl + (fc * 0.7) + (bl * 0.5);
            let right = fr + (fc * 0.7) + (br * 0.5);

            // Prevent clipping
            stereo.push(left.clamp(-1.0, 1.0));
            stereo.push(right.clamp(-1.0, 1.0));
        }

        stereo
    }
}
```

---

## 2. AAC-LC Encoder (PCM → AAC + ADTS)

### Overview
Convert PCM stereo to AAC-LC with ADTS headers for iOS Safari compatibility

### Implementation

```rust
use ac_ffmpeg::codec::Encoder;
use ac_ffmpeg::codec::packet::Packet;
use ac_ffmpeg::codec::CodecParameters;
use anyhow::{Context, Result};

pub struct AacLcEncoder {
    /// FFmpeg encoder
    encoder: Encoder,

    /// Configuration
    sample_rate: u32,
    channels: u16,
    bitrate: u32,

    /// Sample buffer (accumulate to 1024 samples/channel)
    sample_buffer: Vec<f32>,

    /// Statistics
    frames_encoded: u64,
}

impl AacLcEncoder {
    pub fn new(sample_rate: u32, channels: u16, bitrate: u32) -> Result<Self> {
        // Create codec parameters for AAC-LC
        let codec_params = CodecParameters::builder()
            .codec_id(ac_ffmpeg::codec::Id::AAC)
            .sample_rate(sample_rate as i32)
            .channels(channels as i32)
            .bit_rate(bitrate as i64)
            .build();

        // Create encoder
        let mut encoder = Encoder::new(&codec_params)
            .context("Failed to create AAC encoder")?;

        // Set AAC-LC profile explicitly
        encoder.set_option("profile", "aac_low")
            .context("Failed to set AAC-LC profile")?;

        // Open encoder
        encoder.open(None)
            .context("Failed to open AAC encoder")?;

        Ok(Self {
            encoder,
            sample_rate,
            channels,
            bitrate,
            sample_buffer: Vec::with_capacity(2048),
            frames_encoded: 0,
        })
    }

    /// Encode PCM samples to AAC frames with ADTS headers
    pub fn encode(&mut self, pcm_samples: &[f32]) -> Result<Vec<u8>> {
        // Buffer incoming samples
        self.sample_buffer.extend_from_slice(pcm_samples);

        // AAC frame size: 1024 samples per channel
        let frame_size = 1024 * self.channels as usize;
        let mut output = Vec::new();

        // Process complete frames
        while self.sample_buffer.len() >= frame_size {
            // Extract one frame worth of samples
            let frame_samples: Vec<f32> = self.sample_buffer.drain(..frame_size).collect();

            // Create audio frame
            let frame = self.create_audio_frame(&frame_samples)?;

            // Send to encoder
            self.encoder.send_frame(&frame)
                .context("Failed to send frame to AAC encoder")?;

            // Receive encoded packets
            loop {
                match self.encoder.receive_packet() {
                    Ok(packet) => {
                        // Add ADTS header (required for iOS Safari)
                        let aac_with_adts = self.add_adts_header(&packet)?;
                        output.extend(aac_with_adts);

                        self.frames_encoded += 1;
                    }
                    Err(ac_ffmpeg::Error::Again) => {
                        // No more packets available
                        break;
                    }
                    Err(e) => {
                        return Err(e).context("Failed to receive packet from AAC encoder");
                    }
                }
            }
        }

        Ok(output)
    }

    /// Create audio frame from PCM samples
    fn create_audio_frame(&self, samples: &[f32]) -> Result<ac_ffmpeg::codec::AudioFrame> {
        let samples_per_channel = samples.len() / self.channels as usize;

        let mut frame = ac_ffmpeg::codec::AudioFrame::new(
            ac_ffmpeg::codec::AudioSampleFormat::Float,
            self.channels as i32,
            samples_per_channel as i32,
        )?;

        // Set sample rate and PTS
        frame.set_sample_rate(self.sample_rate as i32);
        frame.set_pts(self.frames_encoded as i64 * 1024); // PTS based on samples

        // Copy PCM data into frame
        let frame_data = frame.plane_mut::<f32>(0);
        frame_data[..samples.len()].copy_from_slice(samples);

        Ok(frame)
    }

    /// Add ADTS header to AAC packet (7 bytes)
    fn add_adts_header(&self, packet: &Packet) -> Result<Vec<u8>> {
        let aac_data = packet.data();
        let aac_len = aac_data.len();

        // Total frame length = ADTS header (7) + AAC data
        let frame_len = 7 + aac_len;

        // Get sample rate index for ADTS
        let sample_rate_index = match self.sample_rate {
            96000 => 0, 88200 => 1, 64000 => 2, 48000 => 3,
            44100 => 4, 32000 => 5, 24000 => 6, 22050 => 7,
            16000 => 8, 12000 => 9, 11025 => 10, 8000 => 11,
            _ => anyhow::bail!("Unsupported sample rate: {}", self.sample_rate),
        };

        let mut header = [0u8; 7];

        // ADTS fixed header
        header[0] = 0xFF; // Sync word (12 bits)
        header[1] = 0xF1; // Sync word (4 bits) + MPEG-4 (1) + Layer (00) + No CRC (1)

        // Profile (2 bits) + Sample rate index (4 bits) + Private (1) + Channel MSB (1)
        header[2] = (0 << 6)  // AAC-LC profile (1-1=0)
                  | (sample_rate_index << 2)
                  | ((self.channels >> 2) & 0x01);

        // Channel LSB (2 bits) + Frame length MSB (2 bits)
        header[3] = ((self.channels & 0x03) << 6)
                  | ((frame_len >> 11) as u8);

        // Frame length middle (8 bits)
        header[4] = ((frame_len >> 3) & 0xFF) as u8;

        // Frame length LSB (3 bits) + Buffer fullness MSB (5 bits)
        header[5] = (((frame_len & 0x07) << 5) | 0x1F) as u8;

        // Buffer fullness LSB (6 bits) + Number of frames (2 bits)
        header[6] = 0xFC; // 0b11111100

        // Combine header + AAC data
        let mut output = Vec::with_capacity(frame_len);
        output.extend_from_slice(&header);
        output.extend_from_slice(aac_data);

        Ok(output)
    }

    /// Flush remaining samples at end of stream
    pub fn flush(&mut self) -> Result<Vec<u8>> {
        // Pad remaining samples with zeros if needed
        let frame_size = 1024 * self.channels as usize;
        if !self.sample_buffer.is_empty() {
            let padding = frame_size - self.sample_buffer.len();
            self.sample_buffer.resize(frame_size, 0.0);

            // Encode final frame
            return self.encode(&[]);
        }

        Ok(Vec::new())
    }
}
```

---

## 3. Integration into AudioRemuxer

### Modified decode/encode flow

```rust
// In audio_remux.rs, process_ts_packet() method

match self.demuxer.process_packet(ts_packet)? {
    Some(pes_data) => {
        // Step 1: Decode AC3 PES payload → PCM
        let pcm_samples = self.ac3_decoder.decode(&pes_data)?;

        // Step 2: Encode PCM → AAC with ADTS
        let aac_data = self.aac_encoder.encode(&pcm_samples)?;

        // Step 3: Mux AAC → MPEG-TS packets
        if !aac_data.is_empty() {
            let ts_packets = self.muxer.mux_audio(&aac_data, pts, dts)?;
            output_packets.extend(ts_packets);
        }
    }
    None => {
        // Non-audio or incomplete PES
    }
}
```

---

## 4. Testing Strategy

### Unit Tests
```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_ac3_decoder_init() {
        let decoder = Ac3Decoder::new().unwrap();
        assert_eq!(decoder.sample_rate, 48000);
        assert_eq!(decoder.channels, 2);
    }

    #[test]
    fn test_aac_encoder_init() {
        let encoder = AacLcEncoder::new(48000, 2, 192000).unwrap();
        assert!(encoder.encoder.is_open());
    }

    #[test]
    fn test_downmix_5_1_to_stereo() {
        let decoder = Ac3Decoder::new().unwrap();
        // FL, FR, FC, LFE, BL, BR
        let surround = vec![0.5, -0.5, 0.2, 0.0, 0.1, -0.1];
        let stereo = decoder.downmix_to_stereo(surround, 6);

        assert_eq!(stereo.len(), 2);
        // Left: 0.5 + 0.7*0.2 + 0.5*0.1 = 0.69
        // Right: -0.5 + 0.7*0.2 + 0.5*(-0.1) = -0.41
        assert!((stereo[0] - 0.69).abs() < 0.01);
        assert!((stereo[1] - (-0.41)).abs() < 0.01);
    }

    #[test]
    fn test_adts_header_48khz_stereo() {
        let encoder = AacLcEncoder::new(48000, 2, 192000).unwrap();
        let fake_aac = vec![0u8; 256];
        let fake_packet = Packet::new(&fake_aac);

        let with_adts = encoder.add_adts_header(&fake_packet).unwrap();

        // Check ADTS header
        assert_eq!(with_adts[0], 0xFF); // Sync word
        assert_eq!(with_adts[1], 0xF1); // MPEG-4, no CRC
        assert_eq!(with_adts.len(), 7 + 256); // Header + data
    }
}
```

### Integration Test
```bash
# Build on LXC container
cd /root/xg2g/transcoder
cargo build --release

# Test with real AC3 stream
cd /root/xg2g
./xg2g-daemon &

# Fetch stream
curl http://10.10.55.14:18001/1:0:19:132F:3EF:1:C00000:0:0:0: -o /tmp/aac_test.ts

# Validate output
ffprobe /tmp/aac_test.ts  # Should show: AAC (LC), 48000 Hz, stereo, 192 kb/s
ffplay /tmp/aac_test.ts   # Should play with clear audio
```

---

## 5. Common Pitfalls & Solutions

### Problem 1: "Error::Again" never stops
**Cause:** Decoder needs more data, but we're only sending one packet at a time
**Solution:** This is normal! Just break the loop and wait for next PES packet

### Problem 2: Planar vs Interleaved samples
**Cause:** AC3 outputs planar (separate channels), we need interleaved
**Solution:** Manually interleave in `frame_to_pcm()` (see code above)

### Problem 3: ADTS header causes iOS Safari to fail
**Cause:** Incorrect header structure or sample rate index
**Solution:** Validate header with hex dump, compare to working AAC file

### Problem 4: Audio/Video desync
**Cause:** Incorrect PTS calculation
**Solution:** Maintain consistent PTS: increment by 1024 samples per frame

### Problem 5: Memory leak in frame handling
**Cause:** Not properly releasing FFmpeg frames
**Solution:** ac-ffmpeg handles this automatically with RAII

---

**Document Version:** 1.0
**Created:** 2025-10-29
**Author:** Claude Code (AI Assistant)
**Status:** Ready for Implementation
