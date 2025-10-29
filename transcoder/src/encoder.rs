//! AAC Audio Encoder
//!
//! This module provides AAC-LC encoding functionality for iOS Safari compatibility.
//! It converts PCM audio samples to AAC format with ADTS headers for MPEG-TS streaming.
//!
//! # Supported Profiles
//!
//! - **AAC-LC** (Low Complexity): Primary profile, iOS Safari compatible
//! - **HE-AAC**: High Efficiency (future support)
//!
//! # Usage
//!
//! ```rust,ignore
//! use crate::encoder::{AacEncoder, FfmpegAacEncoder, AacEncoderConfig, AacProfile};
//!
//! let config = AacEncoderConfig {
//!     sample_rate: 48000,
//!     channels: 2,
//!     bitrate: 192000,
//!     profile: AacProfile::AacLc,
//! };
//!
//! let mut encoder = FfmpegAacEncoder::new(config)?;
//! let aac_data = encoder.encode(&pcm_samples)?;
//! ```

use anyhow::{Context, Result};
use tracing::{debug, trace, warn};

// ac-ffmpeg imports for AAC encoder (TODO: Fix API compatibility)
// Temporarily disabled until ac-ffmpeg 0.19 API is properly researched
// use ac_ffmpeg::codec::audio::{AudioEncoder as FfmpegAudioEncoder, AudioFrame};
// use ac_ffmpeg::codec::Encoder;
// use ac_ffmpeg::packet::Packet;

/// AAC Profile
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AacProfile {
    /// AAC Low Complexity (most compatible, iOS Safari)
    AacLc,
    /// High Efficiency AAC
    HeAac,
    /// High Efficiency AAC v2
    HeAacV2,
}

impl AacProfile {
    /// Get FFmpeg profile name
    pub fn ffmpeg_name(&self) -> &str {
        match self {
            Self::AacLc => "aac_low",
            Self::HeAac => "aac_he",
            Self::HeAacV2 => "aac_he_v2",
        }
    }

    /// Get ADTS profile value (for header generation)
    pub fn adts_profile(&self) -> u8 {
        match self {
            Self::AacLc => 1,    // AAC-LC
            Self::HeAac => 4,    // HE-AAC (AAC SBR)
            Self::HeAacV2 => 28, // HE-AAC v2 (AAC PS)
        }
    }
}

/// AAC Encoder Configuration
#[derive(Debug, Clone)]
pub struct AacEncoderConfig {
    /// Sample rate in Hz (8000-96000)
    pub sample_rate: u32,

    /// Number of channels (1-8)
    pub channels: u16,

    /// Bitrate in bits per second
    pub bitrate: u32,

    /// AAC profile
    pub profile: AacProfile,
}

impl Default for AacEncoderConfig {
    fn default() -> Self {
        Self {
            sample_rate: 48000,
            channels: 2,
            bitrate: 192000, // 192 kbps
            profile: AacProfile::AacLc,
        }
    }
}

impl AacEncoderConfig {
    /// Validate configuration
    pub fn validate(&self) -> Result<()> {
        if self.sample_rate < 8000 || self.sample_rate > 96000 {
            anyhow::bail!(
                "Invalid sample rate: {} (must be 8000-96000 Hz)",
                self.sample_rate
            );
        }

        if self.channels < 1 || self.channels > 8 {
            anyhow::bail!("Invalid channel count: {} (must be 1-8)", self.channels);
        }

        if self.bitrate < 32000 || self.bitrate > 512000 {
            anyhow::bail!(
                "Invalid bitrate: {} (must be 32000-512000 bps)",
                self.bitrate
            );
        }

        Ok(())
    }
}

/// AAC Encoder Trait
///
/// Defines the interface for AAC audio encoding.
pub trait AacEncoder: Send {
    /// Encode PCM samples to AAC
    ///
    /// # Arguments
    ///
    /// * `pcm` - Interleaved f32 PCM samples in range [-1.0, 1.0]
    ///
    /// # Returns
    ///
    /// * AAC frames with ADTS headers (ready for MPEG-TS muxing)
    fn encode(&mut self, pcm: &[f32]) -> Result<Vec<u8>>;

    /// Get frame size (samples per channel)
    ///
    /// AAC frame size is typically 1024 samples per channel.
    fn frame_size(&self) -> usize;

    /// Flush encoder (encode remaining samples)
    ///
    /// Call this at the end of stream to encode any buffered samples.
    fn flush(&mut self) -> Result<Vec<u8>>;

    /// Get encoder configuration
    fn config(&self) -> &AacEncoderConfig;

    /// Reset encoder state
    fn reset(&mut self);

    /// Get encoder name for logging
    fn name(&self) -> &str;
}

/// ADTS Header Builder
///
/// Generates ADTS (Audio Data Transport Stream) headers for AAC frames.
/// ADTS headers are required for AAC in MPEG-TS containers.
///
/// # ADTS Header Structure (7 bytes)
///
/// ```text
/// Byte 0-1: Sync word (0xFFF)
/// Byte 1:   MPEG version, Layer, Protection
/// Byte 2:   Profile, Sample rate index, Channel config
/// Byte 3-4: Frame length (header + data)
/// Byte 5-6: Buffer fullness, Number of frames
/// ```
pub struct AdtsHeader;

impl AdtsHeader {
    /// Generate ADTS header for AAC frame
    ///
    /// # Arguments
    ///
    /// * `profile` - AAC profile (1=AAC-LC, 4=HE-AAC, etc.)
    /// * `sample_rate` - Sample rate in Hz
    /// * `channels` - Number of channels
    /// * `frame_length` - Total frame length (header + AAC data)
    ///
    /// # Returns
    ///
    /// * 7-byte ADTS header
    pub fn generate(
        profile: AacProfile,
        sample_rate: u32,
        channels: u16,
        frame_length: usize,
    ) -> Result<[u8; 7]> {
        // Get sample rate index
        let sample_rate_index = Self::sample_rate_to_index(sample_rate)?;

        // Get channel configuration
        let channel_config = channels as u8;
        if channel_config > 8 {
            anyhow::bail!("Invalid channel count for ADTS: {}", channels);
        }

        // ADTS profile (subtract 1 for ADTS encoding)
        let adts_profile = profile.adts_profile();

        // Total frame length (ADTS header + AAC data)
        let total_length = frame_length + 7;
        if total_length > 0x1FFF {
            anyhow::bail!("Frame too large for ADTS: {} bytes", total_length);
        }

        let mut header = [0u8; 7];

        // Byte 0: Sync word (0xFF)
        header[0] = 0xFF;

        // Byte 1: Sync word (0xF0) + MPEG-4 (1) + Layer (00) + Protection absent (1)
        header[1] = 0xF1; // 0xF0 | 0x01 (MPEG-4) | 0x00 (no CRC)

        // Byte 2: Profile (2 bits) + Sample rate index (4 bits) + Private (1 bit) + Channel MSB (1 bit)
        header[2] = ((adts_profile - 1) << 6) | (sample_rate_index << 2) | (channel_config >> 2);

        // Byte 3: Channel LSB (2 bits) + Original (1 bit) + Home (1 bit) + Copyrighted (1 bit) + Copyright start (1 bit) + Frame length MSB (2 bits)
        header[3] = ((channel_config & 0x03) << 6) | ((total_length >> 11) as u8);

        // Byte 4: Frame length middle (8 bits)
        header[4] = ((total_length >> 3) & 0xFF) as u8;

        // Byte 5: Frame length LSB (3 bits) + Buffer fullness MSB (5 bits)
        header[5] = (((total_length & 0x07) << 5) | 0x1F) as u8;

        // Byte 6: Buffer fullness LSB (6 bits) + Number of AAC frames (2 bits, 0 = 1 frame)
        header[6] = 0xFC; // 0b11111100 (buffer fullness = 0x7FF, 1 frame)

        trace!("Generated ADTS header: {:02X?}", header);

        Ok(header)
    }

    /// Convert sample rate to ADTS sample rate index
    fn sample_rate_to_index(sample_rate: u32) -> Result<u8> {
        let index = match sample_rate {
            96000 => 0,
            88200 => 1,
            64000 => 2,
            48000 => 3,
            44100 => 4,
            32000 => 5,
            24000 => 6,
            22050 => 7,
            16000 => 8,
            12000 => 9,
            11025 => 10,
            8000 => 11,
            7350 => 12,
            _ => anyhow::bail!("Unsupported sample rate for ADTS: {}", sample_rate),
        };
        Ok(index)
    }
}

/// FFmpeg AAC Encoder
///
/// Encodes PCM audio to AAC-LC with ADTS headers using ac-ffmpeg.
/// Implements proper AAC-LC encoding for iOS Safari compatibility.
pub struct FfmpegAacEncoder {
    /// FFmpeg encoder
    encoder: ac_ffmpeg::codec::Encoder,

    /// Encoder configuration
    config: AacEncoderConfig,

    /// Input sample buffer (accumulate to frame_size)
    sample_buffer: Vec<f32>,

    /// Frame counter for statistics
    frames_encoded: u64,
}

impl FfmpegAacEncoder {
    /// Create a new FFmpeg AAC encoder
    pub fn new(config: AacEncoderConfig) -> Result<Self> {
        config.validate()?;

        debug!(
            "Creating AAC-LC encoder: {}Hz, {} channels, {} bps, {:?}",
            config.sample_rate, config.channels, config.bitrate, config.profile
        );

        // Create codec parameters for AAC
        let mut params = ac_ffmpeg::codec::CodecParameters::new();
        params.set_codec_id(ac_ffmpeg::codec::Id::AAC);
        params.set_sample_rate(config.sample_rate as i32);
        params.set_channels(config.channels as i32);
        params.set_bit_rate(config.bitrate as i64);

        // Create encoder
        let mut encoder = ac_ffmpeg::codec::Encoder::new(&params)
            .context("Failed to create AAC encoder")?;

        // Set AAC-LC profile
        encoder
            .set_option("profile", config.profile.ffmpeg_name())
            .context("Failed to set AAC profile")?;

        // Open encoder
        encoder
            .open(None)
            .context("Failed to open AAC encoder")?;

        debug!("AAC-LC encoder initialized successfully");

        Ok(Self {
            encoder,
            config,
            sample_buffer: Vec::with_capacity(2048),
            frames_encoded: 0,
        })
    }

    /// Create audio frame from PCM samples
    fn create_audio_frame(&self, samples: &[f32]) -> Result<ac_ffmpeg::codec::audio::AudioFrame> {
        let samples_per_channel = samples.len() / self.config.channels as usize;

        // Create audio frame with f32 format
        let mut frame = ac_ffmpeg::codec::audio::AudioFrame::new(
            self.config.channels as i32,
            self.config.sample_rate as i32,
            ac_ffmpeg::format::sample::Type::F32,
        )
        .context("Failed to create audio frame")?;

        // Set PTS (presentation timestamp)
        frame.set_pts(self.frames_encoded as i64 * 1024);

        // Copy PCM data into frame (interleaved)
        frame
            .copy_from_interleaved(samples)
            .context("Failed to copy PCM data to frame")?;

        Ok(frame)
    }

    /// Encode a complete AAC frame with ADTS header
    fn encode_frame(&mut self, pcm: &[f32]) -> Result<Vec<u8>> {
        // Create audio frame from PCM samples
        let frame = self.create_audio_frame(pcm)?;

        // Send frame to encoder
        self.encoder
            .send_frame(&frame)
            .context("Failed to send frame to AAC encoder")?;

        let mut output = Vec::new();

        // Receive all encoded packets
        loop {
            match self.encoder.receive_packet() {
                Ok(packet) => {
                    // Add ADTS header to packet
                    let aac_with_adts = self.add_adts_header(&packet)?;
                    output.extend(aac_with_adts);

                    self.frames_encoded += 1;

                    trace!(
                        "Encoded AAC frame: {} PCM samples → {} bytes (with ADTS)",
                        pcm.len(),
                        aac_with_adts.len()
                    );
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

        Ok(output)
    }

    /// Add ADTS header to AAC packet (7 bytes)
    fn add_adts_header(&self, packet: &ac_ffmpeg::codec::Packet) -> Result<Vec<u8>> {
        let aac_data = packet.data();
        let aac_len = aac_data.len();

        // Generate ADTS header
        let adts_header = AdtsHeader::generate(
            self.config.profile,
            self.config.sample_rate,
            self.config.channels,
            aac_len, // AAC data length (header is added by AdtsHeader::generate)
        )?;

        // Combine header + AAC data
        let mut output = Vec::with_capacity(7 + aac_len);
        output.extend_from_slice(&adts_header);
        output.extend_from_slice(aac_data);

        Ok(output)
    }
}

impl AacEncoder for FfmpegAacEncoder {
    fn encode(&mut self, pcm: &[f32]) -> Result<Vec<u8>> {
        // Add samples to buffer
        self.sample_buffer.extend_from_slice(pcm);

        let mut output = Vec::new();

        // Encode complete frames
        let samples_per_frame = self.frame_size() * self.config.channels as usize;

        while self.sample_buffer.len() >= samples_per_frame {
            // Extract one frame worth of samples
            let frame_samples: Vec<f32> =
                self.sample_buffer.drain(..samples_per_frame).collect();

            // Encode frame
            let aac_data = self.encode_frame(&frame_samples)?;
            output.extend(aac_data);
        }

        Ok(output)
    }

    fn frame_size(&self) -> usize {
        1024 // AAC frame size (samples per channel)
    }

    fn flush(&mut self) -> Result<Vec<u8>> {
        let mut output = Vec::new();

        // If there are remaining samples, pad and encode
        if !self.sample_buffer.is_empty() {
            let samples_per_frame = self.frame_size() * self.config.channels as usize;
            let remaining = samples_per_frame - self.sample_buffer.len();

            // Pad with zeros
            self.sample_buffer.resize(samples_per_frame, 0.0);

            // Encode final frame
            let frame_samples: Vec<f32> = self.sample_buffer.drain(..).collect();
            let aac_data = self.encode_frame(&frame_samples)?;
            output.extend(aac_data);

            warn!("Flushed encoder with {} padding samples", remaining);
        }

        debug!("Encoder flushed, total frames encoded: {}", self.frames_encoded);

        Ok(output)
    }

    fn config(&self) -> &AacEncoderConfig {
        &self.config
    }

    fn reset(&mut self) {
        self.sample_buffer.clear();
        self.frames_encoded = 0;
    }

    fn name(&self) -> &str {
        "AAC-LC (FFmpeg)"
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_aac_profile() {
        assert_eq!(AacProfile::AacLc.ffmpeg_name(), "aac_low");
        assert_eq!(AacProfile::AacLc.adts_profile(), 1);
        assert_eq!(AacProfile::HeAac.adts_profile(), 4);
    }

    #[test]
    fn test_config_validation() {
        let config = AacEncoderConfig::default();
        assert!(config.validate().is_ok());

        // Invalid sample rate
        let mut bad_config = config.clone();
        bad_config.sample_rate = 1000;
        assert!(bad_config.validate().is_err());

        // Invalid channels
        let mut bad_config = config.clone();
        bad_config.channels = 0;
        assert!(bad_config.validate().is_err());

        // Invalid bitrate
        let mut bad_config = config.clone();
        bad_config.bitrate = 10000;
        assert!(bad_config.validate().is_err());
    }

    #[test]
    fn test_sample_rate_index() {
        assert_eq!(AdtsHeader::sample_rate_to_index(48000).unwrap(), 3);
        assert_eq!(AdtsHeader::sample_rate_to_index(44100).unwrap(), 4);
        assert_eq!(AdtsHeader::sample_rate_to_index(32000).unwrap(), 5);

        // Invalid sample rate
        assert!(AdtsHeader::sample_rate_to_index(99999).is_err());
    }

    #[test]
    fn test_adts_header_generation() {
        let header = AdtsHeader::generate(AacProfile::AacLc, 48000, 2, 100);
        assert!(header.is_ok());

        let header = header.unwrap();
        assert_eq!(header[0], 0xFF); // Sync word
        assert_eq!(header[1] & 0xF0, 0xF0); // Sync word + MPEG-4
    }

    #[test]
    fn test_encoder_creation() {
        let config = AacEncoderConfig::default();
        let encoder = FfmpegAacEncoder::new(config);
        assert!(encoder.is_ok());

        let encoder = encoder.unwrap();
        assert_eq!(encoder.name(), "AAC-LC (Passthrough)");
        assert_eq!(encoder.frame_size(), 1024);
    }

    #[test]
    fn test_encoder_frame_size() {
        let config = AacEncoderConfig::default();
        let encoder = FfmpegAacEncoder::new(config).unwrap();
        assert_eq!(encoder.frame_size(), 1024);
    }
}
