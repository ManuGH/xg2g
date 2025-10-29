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

// ac-ffmpeg imports for AC3 decoder
use ac_ffmpeg::codec::Decoder; // Trait for decoder methods
// Temporarily disabled until ac-ffmpeg 0.19 API is properly researched
// use ac_ffmpeg::codec::audio::{AudioDecoder as FfmpegAudioDecoder, AudioFrame};
// use ac_ffmpeg::codec::Decoder;
// use ac_ffmpeg::packet::Packet;

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
        // Create a cursor from owned data (required for 'static lifetime)
        let owned_data = data.to_vec();
        let cursor = Cursor::new(owned_data);
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

/// AC3 Audio Decoder (Dolby Digital via FFmpeg)
///
/// Decodes AC3 audio (including 5.1 surround) to PCM stereo using ac-ffmpeg.
/// Implements proper AC3 → PCM decoding with automatic 5.1 → stereo downmix.
pub struct Ac3Decoder {
    /// FFmpeg audio decoder (lazy-initialized)
    decoder: Option<ac_ffmpeg::codec::audio::AudioDecoder>,

    /// Detected sample rate (Hz)
    sample_rate: u32,

    /// Output channels (always 2 for stereo)
    channels: u16,

    /// Frame counter for statistics
    frames_decoded: u64,

    /// Initialization flag
    initialized: bool,
}

impl Ac3Decoder {
    /// Create a new AC3 decoder
    pub fn new() -> Result<Self> {
        debug!("Creating AC3 decoder (FFmpeg-based, will initialize on first packet)");
        Ok(Self {
            decoder: None,
            sample_rate: 48000, // Default, updated from stream
            channels: 2,        // Always output stereo
            frames_decoded: 0,
            initialized: false,
        })
    }

    /// Initialize decoder on first packet
    fn init_decoder(&mut self) -> Result<()> {
        if self.initialized {
            return Ok(());
        }

        debug!("Initializing AC3 decoder with FFmpeg");

        // Create codec parameters for AC3 using builder pattern
        let codec_params = ac_ffmpeg::codec::AudioCodecParameters::builder("ac3")
            .context("Failed to create AC3 codec parameters")?
            .sample_rate(48000) // Default, will be auto-detected from stream
            .build();

        // Create decoder using builder pattern
        let decoder = ac_ffmpeg::codec::audio::AudioDecoder::from_codec_parameters(&codec_params)
            .context("Failed to create AC3 decoder builder")?
            .build()
            .context("Failed to build AC3 decoder")?;

        self.decoder = Some(decoder);
        self.initialized = true;

        debug!("AC3 decoder initialized successfully");
        Ok(())
    }

    /// Downmix multi-channel audio to stereo
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
        // Standard layout: FL, FR, FC, LFE, BL, BR
        let frame_count = samples.len() / input_channels;
        let mut stereo = Vec::with_capacity(frame_count * 2);

        for frame_idx in 0..frame_count {
            let base = frame_idx * input_channels;

            let fl = samples[base];
            let fr = samples[base + 1];
            let fc = samples.get(base + 2).copied().unwrap_or(0.0);
            let bl = samples.get(base + 4).copied().unwrap_or(0.0);
            let br = samples.get(base + 5).copied().unwrap_or(0.0);

            // Downmix formula: L = FL + 0.7*FC + 0.5*BL
            let left = fl + (fc * 0.7) + (bl * 0.5);
            let right = fr + (fc * 0.7) + (br * 0.5);

            // Prevent clipping
            stereo.push(left.clamp(-1.0, 1.0));
            stereo.push(right.clamp(-1.0, 1.0));
        }

        stereo
    }

    /// Convert audio frame to PCM f32 samples (interleaved)
    fn frame_to_pcm(&self, frame: &ac_ffmpeg::codec::audio::AudioFrame) -> Result<Vec<f32>> {
        let channel_layout = frame.channel_layout();
        let channels = channel_layout.channels() as usize;
        let samples_per_channel = frame.samples();
        let total_samples = samples_per_channel * (channels as usize);

        let mut pcm = Vec::with_capacity(total_samples);

        // Get sample format
        let format = frame.sample_format();
        let format_name = format.name();
        let is_planar = format.is_planar();

        // Get planes
        let planes = frame.planes();

        // Handle different sample formats
        // Common formats: fltp (f32 planar), s16p (i16 planar), flt (f32 packed), s16 (i16 packed)
        if format_name.starts_with("fltp") || format_name == "flt" {
            // Float format
            if is_planar {
                // Planar f32: each channel in separate plane
                for sample_idx in 0..samples_per_channel {
                    for ch in 0..channels {
                        let plane_bytes = planes[ch].data();
                        let samples_f32 = unsafe {
                            std::slice::from_raw_parts(
                                plane_bytes.as_ptr() as *const f32,
                                samples_per_channel,
                            )
                        };
                        pcm.push(samples_f32[sample_idx]);
                    }
                }
            } else {
                // Interleaved f32: all channels in one plane
                let plane_bytes = planes[0].data();
                let samples_f32 = unsafe {
                    std::slice::from_raw_parts(
                        plane_bytes.as_ptr() as *const f32,
                        total_samples,
                    )
                };
                pcm.extend_from_slice(samples_f32);
            }
        } else if format_name.starts_with("s16p") || format_name == "s16" {
            // i16 format
            if is_planar {
                // Planar i16: each channel in separate plane
                for sample_idx in 0..samples_per_channel {
                    for ch in 0..channels {
                        let plane_bytes = planes[ch].data();
                        let samples_i16 = unsafe {
                            std::slice::from_raw_parts(
                                plane_bytes.as_ptr() as *const i16,
                                samples_per_channel,
                            )
                        };
                        pcm.push(samples_i16[sample_idx] as f32 / 32768.0);
                    }
                }
            } else {
                // Interleaved i16: all channels in one plane
                let plane_bytes = planes[0].data();
                let samples_i16 = unsafe {
                    std::slice::from_raw_parts(
                        plane_bytes.as_ptr() as *const i16,
                        total_samples,
                    )
                };
                for &sample in samples_i16 {
                    pcm.push(sample as f32 / 32768.0);
                }
            }
        } else {
            warn!("Unsupported AC3 sample format: {}, returning silence", format_name);
            pcm.resize(total_samples, 0.0);
        }

        Ok(pcm)
    }
}

impl AudioDecoder for Ac3Decoder {
    fn decode(&mut self, data: &[u8]) -> Result<Vec<PcmSample>> {
        // Initialize decoder on first call
        if !self.initialized {
            self.init_decoder()?;
        }

        // Create packet from raw AC3 PES data
        let mut packet_mut = ac_ffmpeg::packet::PacketMut::new(data.len());
        packet_mut.data_mut().copy_from_slice(data);
        let packet = packet_mut.freeze();

        // Push packet to decoder (borrow ends after this call)
        self.decoder.as_mut()
            .context("AC3 decoder not initialized")?
            .push(packet)
            .context("Failed to push packet to AC3 decoder")?;

        let mut all_samples = Vec::new();

        // Take all decoded frames
        loop {
            let frame_opt = self.decoder.as_mut()
                .context("AC3 decoder not initialized")?
                .take()
                .context("Failed to take frame from AC3 decoder")?;

            let frame = match frame_opt {
                Some(f) => f,
                None => break,
            };

            // Update sample rate from stream
            self.sample_rate = frame.sample_rate();

            let channel_layout = frame.channel_layout();
            let input_channels = channel_layout.channels() as usize;

            trace!(
                "Decoded AC3 frame: {} samples/channel, {} channels, {}Hz",
                frame.samples(),
                input_channels,
                self.sample_rate
            );

            // Convert frame to PCM (decoder borrow released, can call self methods)
            let pcm = self.frame_to_pcm(&frame)?;

            // Downmix to stereo if needed
            let stereo = if input_channels != 2 {
                trace!("Downmixing {} channels to stereo", input_channels);
                self.downmix_to_stereo(pcm, input_channels)
            } else {
                pcm
            };

            all_samples.extend(stereo);
            self.frames_decoded += 1;
        }

        trace!(
            "AC3 decode complete: {} PCM samples from {} bytes (frame #{})",
            all_samples.len(),
            data.len(),
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
        self.decoder = None;
        self.frames_decoded = 0;
        self.initialized = false;
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
        assert_eq!(decoder.name(), "AC3 (Passthrough)");
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
