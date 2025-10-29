//! Audio Decoder
//!
//! This module provides audio decoding functionality for various codecs.
//! It decodes compressed audio (MP2, AC3) to PCM samples for further processing.
//!
//! # Supported Codecs
//!
//! - **MP2 (MPEG-1 Layer 2)**: Via Symphonia (pure Rust)
//! - **AC3 (Dolby Digital)**: Via ac-ffmpeg (FFmpeg bindings)
//!
//! # Usage
//!
//! ```rust,ignore
//! use crate::decoder::{AudioDecoder, Mp2Decoder};
//!
//! let mut decoder = Mp2Decoder::new()?;
//! let pcm_samples = decoder.decode(pes_data)?;
//! ```

use anyhow::{Context, Result};
use std::io::Cursor;
use symphonia::core::audio::{AudioBufferRef, Signal};
use symphonia::core::codecs::{DecoderOptions, CODEC_TYPE_MP2};
use symphonia::core::formats::FormatOptions;
use symphonia::core::io::MediaSourceStream;
use symphonia::core::meta::MetadataOptions;
use symphonia::core::probe::Hint;
use tracing::{debug, trace, warn};

/// Audio sample format
///
/// PCM samples are represented as f32 in the range [-1.0, 1.0].
/// This is the standard format for audio processing in Rust.
pub type PcmSample = f32;

/// Audio decoder trait
///
/// Defines the interface for all audio decoders.
pub trait AudioDecoder: Send {
    /// Decode compressed audio data to PCM samples
    ///
    /// # Arguments
    ///
    /// * `data` - Compressed audio data (PES payload)
    ///
    /// # Returns
    ///
    /// * Interleaved PCM samples as f32 [-1.0, 1.0]
    /// * For stereo: [L, R, L, R, ...]
    /// * For mono: [M, M, M, ...]
    fn decode(&mut self, data: &[u8]) -> Result<Vec<PcmSample>>;

    /// Get the sample rate in Hz
    fn sample_rate(&self) -> u32;

    /// Get the number of channels
    fn channels(&self) -> u16;

    /// Reset decoder state
    ///
    /// Called when stream discontinuity is detected or on error recovery.
    fn reset(&mut self);

    /// Get decoder name for logging
    fn name(&self) -> &str;
}

/// MP2 Audio Decoder (MPEG-1 Layer 2)
///
/// Uses Symphonia for pure Rust MP2 decoding.
pub struct Mp2Decoder {
    /// Detected sample rate (Hz)
    sample_rate: u32,

    /// Detected channel count
    channels: u16,

    /// Frame counter for statistics
    frames_decoded: u64,
}

impl Mp2Decoder {
    /// Create a new MP2 decoder
    pub fn new() -> Result<Self> {
        Ok(Self {
            sample_rate: 48000, // Default, will be updated from stream
            channels: 2,        // Default stereo
            frames_decoded: 0,
        })
    }

    /// Convert Symphonia AudioBufferRef to f32 PCM samples
    fn convert_to_pcm(buffer: &AudioBufferRef) -> Result<Vec<PcmSample>> {
        match buffer {
            // f32 samples - already in correct format
            AudioBufferRef::F32(buf) => {
                let mut samples = Vec::with_capacity(buf.frames() * buf.spec().channels.count());

                // Interleave channels
                for frame_idx in 0..buf.frames() {
                    for chan_idx in 0..buf.spec().channels.count() {
                        let sample = buf.chan(chan_idx)[frame_idx];
                        samples.push(sample);
                    }
                }

                Ok(samples)
            }

            // i16 samples - convert to f32 [-1.0, 1.0]
            AudioBufferRef::S16(buf) => {
                let mut samples = Vec::with_capacity(buf.frames() * buf.spec().channels.count());

                for frame_idx in 0..buf.frames() {
                    for chan_idx in 0..buf.spec().channels.count() {
                        let sample = buf.chan(chan_idx)[frame_idx];
                        // Convert i16 [-32768, 32767] to f32 [-1.0, 1.0]
                        let normalized = sample as f32 / 32768.0;
                        samples.push(normalized);
                    }
                }

                Ok(samples)
            }

            // i32 samples - convert to f32 [-1.0, 1.0]
            AudioBufferRef::S32(buf) => {
                let mut samples = Vec::with_capacity(buf.frames() * buf.spec().channels.count());

                for frame_idx in 0..buf.frames() {
                    for chan_idx in 0..buf.spec().channels.count() {
                        let sample = buf.chan(chan_idx)[frame_idx];
                        // Convert i32 to f32 [-1.0, 1.0]
                        let normalized = sample as f32 / 2147483648.0;
                        samples.push(normalized);
                    }
                }

                Ok(samples)
            }

            // u8 samples - convert to f32 [-1.0, 1.0]
            AudioBufferRef::U8(buf) => {
                let mut samples = Vec::with_capacity(buf.frames() * buf.spec().channels.count());

                for frame_idx in 0..buf.frames() {
                    for chan_idx in 0..buf.spec().channels.count() {
                        let sample = buf.chan(chan_idx)[frame_idx];
                        // Convert u8 [0, 255] to f32 [-1.0, 1.0]
                        let normalized = (sample as f32 - 128.0) / 128.0;
                        samples.push(normalized);
                    }
                }

                Ok(samples)
            }

            _ => anyhow::bail!("Unsupported sample format"),
        }
    }
}

impl AudioDecoder for Mp2Decoder {
    fn decode(&mut self, data: &[u8]) -> Result<Vec<PcmSample>> {
        // Create a cursor from the input data
        let cursor = Cursor::new(data);
        let mss = MediaSourceStream::new(Box::new(cursor), Default::default());

        // Create a hint for the format probe
        let mut hint = Hint::new();
        hint.with_extension("mp2");

        // Probe the format
        let format_opts = FormatOptions::default();
        let metadata_opts = MetadataOptions::default();
        let decoder_opts = DecoderOptions::default();

        let probed = symphonia::default::get_probe()
            .format(&hint, mss, &format_opts, &metadata_opts)
            .context("Failed to probe MP2 format")?;

        let mut format = probed.format;

        // Find the audio track
        let track = format
            .tracks()
            .iter()
            .find(|t| t.codec_params.codec == CODEC_TYPE_MP2)
            .context("No MP2 audio track found")?;

        // Update sample rate and channels from stream
        if let Some(sr) = track.codec_params.sample_rate {
            self.sample_rate = sr;
        }
        if let Some(ch) = track.codec_params.channels {
            self.channels = ch.count() as u16;
        }

        let track_id = track.id;

        // Create decoder for the track
        let mut decoder = symphonia::default::get_codecs()
            .make(&track.codec_params, &decoder_opts)
            .context("Failed to create MP2 decoder")?;

        let mut all_samples = Vec::new();

        // Decode all packets
        loop {
            // Read the next packet
            let packet = match format.next_packet() {
                Ok(packet) => packet,
                Err(symphonia::core::errors::Error::IoError(e))
                    if e.kind() == std::io::ErrorKind::UnexpectedEof =>
                {
                    break; // End of stream
                }
                Err(e) => {
                    warn!("Error reading packet: {}", e);
                    break;
                }
            };

            // Skip packets from other tracks
            if packet.track_id() != track_id {
                continue;
            }

            // Decode the packet
            match decoder.decode(&packet) {
                Ok(audio_buf) => {
                    // Convert to PCM samples
                    let pcm = Self::convert_to_pcm(&audio_buf)?;
                    all_samples.extend(pcm);
                    self.frames_decoded += 1;
                }
                Err(e) => {
                    warn!("Error decoding MP2 frame: {}", e);
                    continue;
                }
            }
        }

        trace!(
            "Decoded {} MP2 samples ({} frames)",
            all_samples.len(),
            self.frames_decoded
        );

        Ok(all_samples)
    }

    fn sample_rate(&self) -> u32 {
        self.sample_rate
    }

    fn channels(&self) -> u16 {
        self.channels
    }

    fn reset(&mut self) {
        self.frames_decoded = 0;
    }

    fn name(&self) -> &str {
        "MP2 (Symphonia)"
    }
}

impl Default for Mp2Decoder {
    fn default() -> Self {
        Self::new().expect("Failed to create MP2 decoder")
    }
}

/// AC3 Audio Decoder (Dolby Digital)
///
/// Uses ac-ffmpeg bindings for AC3 decoding via FFmpeg libavcodec.
///
/// **Note:** This requires FFmpeg libraries to be installed on the system.
pub struct Ac3Decoder {
    /// FFmpeg decoder context
    decoder: Option<ac_ffmpeg::codec::AudioDecoder>,

    /// Detected sample rate (Hz)
    sample_rate: u32,

    /// Detected channel count
    channels: u16,

    /// Frame counter for statistics
    frames_decoded: u64,
}

impl Ac3Decoder {
    /// Create a new AC3 decoder
    pub fn new() -> Result<Self> {
        Ok(Self {
            decoder: None,
            sample_rate: 48000, // Default, will be updated from stream
            channels: 2,        // Default stereo (will be updated)
            frames_decoded: 0,
        })
    }

    /// Initialize FFmpeg decoder (lazy initialization)
    fn ensure_decoder(&mut self) -> Result<()> {
        if self.decoder.is_none() {
            let decoder = ac_ffmpeg::codec::AudioDecoder::from_codec_name("ac3")
                .context("Failed to create AC3 decoder")?;
            self.decoder = Some(decoder);
            debug!("Initialized AC3 decoder");
        }
        Ok(())
    }

    /// Convert FFmpeg audio frame to f32 PCM samples
    fn convert_frame_to_pcm(frame: &ac_ffmpeg::codec::AudioFrame) -> Result<Vec<PcmSample>> {
        let mut samples = Vec::new();

        // Get audio parameters
        let channels = frame.channels() as usize;
        let sample_count = frame.samples();

        // FFmpeg audio frames are planar (one array per channel)
        // We need to interleave them for our format

        // Convert samples based on format
        for sample_idx in 0..sample_count {
            for ch_idx in 0..channels {
                let plane = frame.plane(ch_idx);

                // Assuming f32 planar format (most common for decoded audio)
                // If format is different, we'll need to handle conversion
                if plane.len() >= (sample_idx + 1) * 4 {
                    let offset = sample_idx * 4;
                    let bytes = &plane[offset..offset + 4];
                    let sample = f32::from_le_bytes([bytes[0], bytes[1], bytes[2], bytes[3]]);
                    samples.push(sample);
                }
            }
        }

        Ok(samples)
    }
}

impl AudioDecoder for Ac3Decoder {
    fn decode(&mut self, data: &[u8]) -> Result<Vec<PcmSample>> {
        self.ensure_decoder()?;

        let decoder = self.decoder.as_mut().unwrap();

        // Create packet from data
        let packet = ac_ffmpeg::codec::Packet::new(data);

        // Decode packet
        decoder
            .push(packet)
            .context("Failed to push packet to AC3 decoder")?;

        let mut all_samples = Vec::new();

        // Retrieve all decoded frames
        loop {
            match decoder.take() {
                Ok(Some(frame)) => {
                    // Update sample rate and channels from first frame
                    if self.frames_decoded == 0 {
                        self.sample_rate = frame.sample_rate();
                        self.channels = frame.channels() as u16;
                        debug!(
                            "AC3 stream: {}Hz, {} channels",
                            self.sample_rate, self.channels
                        );
                    }

                    // Convert frame to PCM
                    let pcm = Self::convert_frame_to_pcm(&frame)?;
                    all_samples.extend(pcm);
                    self.frames_decoded += 1;
                }
                Ok(None) => break, // No more frames available
                Err(e) => {
                    warn!("Error decoding AC3 frame: {}", e);
                    break;
                }
            }
        }

        trace!(
            "Decoded {} AC3 samples ({} frames)",
            all_samples.len(),
            self.frames_decoded
        );

        Ok(all_samples)
    }

    fn sample_rate(&self) -> u32 {
        self.sample_rate
    }

    fn channels(&self) -> u16 {
        self.channels
    }

    fn reset(&mut self) {
        // Flush decoder
        if let Some(decoder) = &mut self.decoder {
            let _ = decoder.flush();
        }
        self.frames_decoded = 0;
    }

    fn name(&self) -> &str {
        "AC3 (FFmpeg)"
    }
}

impl Default for Ac3Decoder {
    fn default() -> Self {
        Self::new().expect("Failed to create AC3 decoder")
    }
}

/// Auto-detecting decoder wrapper
///
/// Automatically selects the appropriate decoder based on codec type.
pub struct AutoDecoder {
    decoder: Box<dyn AudioDecoder>,
    codec_type: crate::demux::AudioCodec,
}

impl AutoDecoder {
    /// Create a new auto-detecting decoder
    pub fn new(codec: crate::demux::AudioCodec) -> Result<Self> {
        let decoder: Box<dyn AudioDecoder> = match codec {
            crate::demux::AudioCodec::Mp2 => {
                debug!("Creating MP2 decoder");
                Box::new(Mp2Decoder::new()?)
            }
            crate::demux::AudioCodec::Ac3 => {
                debug!("Creating AC3 decoder");
                Box::new(Ac3Decoder::new()?)
            }
            crate::demux::AudioCodec::Aac => {
                anyhow::bail!("AAC decoding not needed (already in target format)")
            }
            crate::demux::AudioCodec::Unknown => {
                anyhow::bail!("Cannot create decoder for unknown codec")
            }
        };

        Ok(Self {
            decoder,
            codec_type: codec,
        })
    }

    /// Get the codec type
    pub fn codec_type(&self) -> crate::demux::AudioCodec {
        self.codec_type
    }
}

impl AudioDecoder for AutoDecoder {
    fn decode(&mut self, data: &[u8]) -> Result<Vec<PcmSample>> {
        self.decoder.decode(data)
    }

    fn sample_rate(&self) -> u32 {
        self.decoder.sample_rate()
    }

    fn channels(&self) -> u16 {
        self.decoder.channels()
    }

    fn reset(&mut self) {
        self.decoder.reset()
    }

    fn name(&self) -> &str {
        self.decoder.name()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_mp2_decoder_creation() {
        let decoder = Mp2Decoder::new();
        assert!(decoder.is_ok());

        let decoder = decoder.unwrap();
        assert_eq!(decoder.name(), "MP2 (Symphonia)");
        assert_eq!(decoder.sample_rate(), 48000); // Default
        assert_eq!(decoder.channels(), 2); // Default
    }

    #[test]
    fn test_ac3_decoder_creation() {
        let decoder = Ac3Decoder::new();
        assert!(decoder.is_ok());

        let decoder = decoder.unwrap();
        assert_eq!(decoder.name(), "AC3 (FFmpeg)");
    }

    #[test]
    fn test_auto_decoder_mp2() {
        let result = AutoDecoder::new(crate::demux::AudioCodec::Mp2);
        assert!(result.is_ok());

        let decoder = result.unwrap();
        assert_eq!(decoder.codec_type(), crate::demux::AudioCodec::Mp2);
    }

    #[test]
    fn test_auto_decoder_unknown() {
        let result = AutoDecoder::new(crate::demux::AudioCodec::Unknown);
        assert!(result.is_err());
    }

    #[test]
    fn test_auto_decoder_aac() {
        // AAC shouldn't need decoding (already target format)
        let result = AutoDecoder::new(crate::demux::AudioCodec::Aac);
        assert!(result.is_err());
    }
}
