//! MPEG-TS Demuxer
//!
//! This module provides functionality to demux MPEG Transport Stream packets
//! and extract Packetized Elementary Stream (PES) data for audio processing.
//!
//! # MPEG-TS Packet Structure
//!
//! Each TS packet is exactly 188 bytes:
//! - Sync byte (0x47) - 1 byte
//! - Header - 3 bytes (flags, PID, continuity counter)
//! - Adaptation field (optional) - variable length
//! - Payload - remaining bytes
//!
//! # PES Packet Structure
//!
//! PES packets contain elementary stream data (audio/video frames).
//! They can span multiple TS packets and must be reassembled.

use anyhow::{bail, Result};
use std::collections::HashMap;
use tracing::{debug, trace, warn};

/// MPEG-TS sync byte (first byte of every TS packet)
const TS_SYNC_BYTE: u8 = 0x47;

/// MPEG-TS packet size (188 bytes)
pub const TS_PACKET_SIZE: usize = 188;

/// PAT (Program Association Table) PID
const PAT_PID: u16 = 0x0000;

/// Maximum PES packet size (1MB - reasonable limit for audio)
const MAX_PES_SIZE: usize = 1024 * 1024;

/// Audio codec types detected from stream descriptors
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AudioCodec {
    /// MPEG-1 Layer 2 audio (MP2)
    Mp2,
    /// Dolby Digital (AC-3)
    Ac3,
    /// Advanced Audio Coding (AAC)
    Aac,
    /// Unknown audio codec
    Unknown,
}

impl AudioCodec {
    /// Detect codec from stream type byte
    pub fn from_stream_type(stream_type: u8) -> Self {
        match stream_type {
            0x03 | 0x04 => Self::Mp2,  // MPEG-1/2 Audio
            0x81 => Self::Ac3,           // AC-3 audio
            0x0F => Self::Aac,           // AAC audio (ADTS)
            _ => Self::Unknown,
        }
    }
}

/// MPEG-TS Packet
///
/// Represents a parsed Transport Stream packet (188 bytes).
#[derive(Debug)]
pub struct TsPacket {
    /// Packet Identifier (13 bits)
    pub pid: u16,

    /// Payload Unit Start Indicator (PUSI)
    /// Set to 1 when payload contains the start of a PES packet
    pub payload_start: bool,

    /// Transport Error Indicator
    pub transport_error: bool,

    /// Transport Priority
    pub priority: bool,

    /// Scrambling control (00 = not scrambled)
    pub scrambling: u8,

    /// Adaptation field present
    pub has_adaptation: bool,

    /// Payload present
    pub has_payload: bool,

    /// Continuity counter (4 bits, cycles 0-15)
    pub continuity: u8,

    /// Payload data (slice of original packet)
    pub payload: Vec<u8>,
}

impl TsPacket {
    /// Parse a 188-byte MPEG-TS packet
    pub fn parse(data: &[u8]) -> Result<Self> {
        if data.len() != TS_PACKET_SIZE {
            bail!("Invalid TS packet size: expected {}, got {}", TS_PACKET_SIZE, data.len());
        }

        // Check sync byte
        if data[0] != TS_SYNC_BYTE {
            bail!("Invalid sync byte: expected 0x{:02X}, got 0x{:02X}", TS_SYNC_BYTE, data[0]);
        }

        // Parse header (bytes 1-3)
        let transport_error = (data[1] & 0x80) != 0;
        let payload_start = (data[1] & 0x40) != 0;
        let priority = (data[1] & 0x20) != 0;
        let pid = (((data[1] & 0x1F) as u16) << 8) | (data[2] as u16);

        let scrambling = (data[3] & 0xC0) >> 6;
        let has_adaptation = (data[3] & 0x20) != 0;
        let has_payload = (data[3] & 0x10) != 0;
        let continuity = data[3] & 0x0F;

        // Calculate payload offset
        let mut payload_offset = 4;

        // Skip adaptation field if present
        if has_adaptation {
            let adaptation_length = data[4] as usize;
            payload_offset += 1 + adaptation_length;
        }

        // Extract payload
        let payload = if has_payload && payload_offset < data.len() {
            data[payload_offset..].to_vec()
        } else {
            Vec::new()
        };

        Ok(Self {
            pid,
            payload_start,
            transport_error,
            priority,
            scrambling,
            has_adaptation,
            has_payload,
            continuity,
            payload,
        })
    }

    /// Check if packet is scrambled
    pub fn is_scrambled(&self) -> bool {
        self.scrambling != 0
    }
}

/// PES Packet Buffer
///
/// Accumulates TS packet payloads to reassemble complete PES packets.
#[derive(Debug)]
struct PesBuffer {
    /// Accumulated PES data
    data: Vec<u8>,

    /// Expected continuity counter for next packet
    expected_continuity: u8,

    /// PES packet length (from header, 0 = unbounded)
    pes_length: usize,

    /// Whether we have started receiving PES data
    started: bool,
}

impl PesBuffer {
    fn new() -> Self {
        Self {
            data: Vec::with_capacity(8192),
            expected_continuity: 0,
            started: false,
            pes_length: 0,
        }
    }

    /// Add TS packet payload to PES buffer
    ///
    /// Returns `Some(pes_data)` when a complete PES packet is ready.
    fn add_payload(&mut self, packet: &TsPacket) -> Result<Option<Vec<u8>>> {
        // Check for packet loss (continuity counter mismatch)
        if self.started && packet.continuity != self.expected_continuity {
            warn!(
                "Continuity error for PID {}: expected {}, got {}",
                packet.pid, self.expected_continuity, packet.continuity
            );
            // Reset buffer on error
            self.reset();
            return Ok(None);
        }

        // Update expected continuity counter (cycles 0-15)
        self.expected_continuity = (packet.continuity + 1) & 0x0F;

        // If PUSI flag set, this is the start of a new PES packet
        if packet.payload_start {
            // If we had data buffered, it's incomplete - discard it
            if !self.data.is_empty() {
                warn!("Incomplete PES packet discarded (PID {})", packet.pid);
            }
            self.reset();

            // Parse PES header from payload
            if packet.payload.len() < 6 {
                bail!("PES header too short");
            }

            // Check PES start code (0x000001)
            if packet.payload[0] != 0x00 || packet.payload[1] != 0x00 || packet.payload[2] != 0x01 {
                bail!("Invalid PES start code");
            }

            // PES packet length (bytes 4-5)
            let pes_length = ((packet.payload[4] as usize) << 8) | (packet.payload[5] as usize);

            // Store PES length (0 means unbounded - used for video)
            self.pes_length = if pes_length == 0 {
                MAX_PES_SIZE
            } else {
                pes_length + 6 // +6 for PES header
            };

            self.started = true;
        }

        // Append payload to buffer
        if self.started && !packet.payload.is_empty() {
            self.data.extend_from_slice(&packet.payload);

            // Check if buffer is getting too large
            if self.data.len() > MAX_PES_SIZE {
                warn!("PES buffer too large, resetting");
                self.reset();
                return Ok(None);
            }

            // Check if PES packet is complete
            if self.data.len() >= self.pes_length {
                // Extract complete PES packet
                let pes_data = self.data.clone();
                self.reset();
                return Ok(Some(pes_data));
            }
        }

        Ok(None)
    }

    fn reset(&mut self) {
        self.data.clear();
        self.started = false;
        self.pes_length = 0;
    }
}

/// MPEG-TS Demuxer
///
/// Demultiplexes Transport Stream packets and extracts audio PES data.
pub struct TsDemuxer {
    /// Detected audio PID (auto-detected from PMT)
    audio_pid: Option<u16>,

    /// Detected audio codec type
    audio_codec: AudioCodec,

    /// PES buffers for each PID
    pes_buffers: HashMap<u16, PesBuffer>,

    /// PMT PID (detected from PAT)
    pmt_pid: Option<u16>,

    /// Statistics
    packets_processed: u64,
    audio_packets: u64,
}

impl TsDemuxer {
    /// Create a new MPEG-TS demuxer
    pub fn new() -> Self {
        Self {
            audio_pid: None,
            audio_codec: AudioCodec::Unknown,
            pes_buffers: HashMap::new(),
            pmt_pid: None,
            packets_processed: 0,
            audio_packets: 0,
        }
    }

    /// Process a single TS packet
    ///
    /// Returns `Some(pes_data)` when a complete audio PES packet is ready.
    pub fn process_packet(&mut self, data: &[u8]) -> Result<Option<Vec<u8>>> {
        let packet = TsPacket::parse(data)?;
        self.packets_processed += 1;

        // Skip scrambled packets
        if packet.is_scrambled() {
            trace!("Skipping scrambled packet (PID {})", packet.pid);
            return Ok(None);
        }

        // Handle PAT (Program Association Table)
        if packet.pid == PAT_PID && packet.payload_start {
            self.parse_pat(&packet.payload)?;
        }

        // Handle PMT (Program Map Table)
        if let Some(pmt_pid) = self.pmt_pid {
            if packet.pid == pmt_pid && packet.payload_start {
                self.parse_pmt(&packet.payload)?;
            }
        }

        // Handle audio packets
        if let Some(audio_pid) = self.audio_pid {
            if packet.pid == audio_pid {
                self.audio_packets += 1;

                // Get or create PES buffer for this PID
                let buffer = self.pes_buffers.entry(packet.pid).or_insert_with(PesBuffer::new);

                // Add payload to buffer
                return buffer.add_payload(&packet);
            }
        }

        Ok(None)
    }

    /// Parse PAT (Program Association Table) to find PMT PID
    fn parse_pat(&mut self, payload: &[u8]) -> Result<()> {
        // Skip pointer field
        if payload.is_empty() {
            return Ok(());
        }
        let pointer = payload[0] as usize;
        let data = &payload[1 + pointer..];

        if data.len() < 8 {
            return Ok(()); // Too short
        }

        // Table ID should be 0x00 for PAT
        if data[0] != 0x00 {
            return Ok(());
        }

        // Section length
        let section_length = (((data[1] & 0x0F) as usize) << 8) | (data[2] as usize);

        if data.len() < 8 + section_length {
            return Ok(()); // Incomplete
        }

        // Parse program entries (skip first 8 bytes of header)
        let mut offset = 8;
        while offset + 4 <= section_length + 3 {
            let program_number = ((data[offset] as u16) << 8) | (data[offset + 1] as u16);
            let pid = (((data[offset + 2] & 0x1F) as u16) << 8) | (data[offset + 3] as u16);

            if program_number != 0 {
                // Found PMT PID
                self.pmt_pid = Some(pid);
                debug!("Detected PMT PID: {}", pid);
                break;
            }

            offset += 4;
        }

        Ok(())
    }

    /// Parse PMT (Program Map Table) to find audio PID and codec
    fn parse_pmt(&mut self, payload: &[u8]) -> Result<()> {
        // Skip pointer field
        if payload.is_empty() {
            return Ok(());
        }
        let pointer = payload[0] as usize;
        let data = &payload[1 + pointer..];

        if data.len() < 12 {
            return Ok(()); // Too short
        }

        // Table ID should be 0x02 for PMT
        if data[0] != 0x02 {
            return Ok(());
        }

        // Section length
        let section_length = (((data[1] & 0x0F) as usize) << 8) | (data[2] as usize);

        // Program info length
        let program_info_length = (((data[10] & 0x0F) as usize) << 8) | (data[11] as usize);

        // Parse stream entries
        let mut offset = 12 + program_info_length;
        while offset + 5 <= section_length + 3 {
            let stream_type = data[offset];
            let pid = (((data[offset + 1] & 0x1F) as u16) << 8) | (data[offset + 2] as u16);
            let es_info_length = (((data[offset + 3] & 0x0F) as usize) << 8) | (data[offset + 4] as usize);

            // Check if this is an audio stream
            let codec = AudioCodec::from_stream_type(stream_type);
            if codec != AudioCodec::Unknown {
                self.audio_pid = Some(pid);
                self.audio_codec = codec;
                debug!("Detected audio: PID {}, codec {:?}", pid, codec);
                break;
            }

            offset += 5 + es_info_length;
        }

        Ok(())
    }

    /// Get detected audio PID
    pub fn audio_pid(&self) -> Option<u16> {
        self.audio_pid
    }

    /// Get detected audio codec
    pub fn audio_codec(&self) -> AudioCodec {
        self.audio_codec
    }

    /// Get statistics
    pub fn stats(&self) -> (u64, u64) {
        (self.packets_processed, self.audio_packets)
    }
}

impl Default for TsDemuxer {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_valid_packet() {
        // Create a minimal valid TS packet
        let mut packet = [0u8; TS_PACKET_SIZE];
        packet[0] = TS_SYNC_BYTE;  // Sync byte
        packet[1] = 0x40;           // PUSI flag set
        packet[2] = 0x00;           // PID = 0x0000 (PAT)
        packet[3] = 0x10;           // Has payload, continuity = 0

        let result = TsPacket::parse(&packet);
        assert!(result.is_ok());

        let parsed = result.unwrap();
        assert_eq!(parsed.pid, 0x0000);
        assert!(parsed.payload_start);
        assert!(!parsed.transport_error);
        assert!(parsed.has_payload);
    }

    #[test]
    fn test_parse_invalid_sync() {
        let mut packet = [0u8; TS_PACKET_SIZE];
        packet[0] = 0xFF;  // Wrong sync byte

        let result = TsPacket::parse(&packet);
        assert!(result.is_err());
    }

    #[test]
    fn test_codec_detection() {
        assert_eq!(AudioCodec::from_stream_type(0x03), AudioCodec::Mp2);
        assert_eq!(AudioCodec::from_stream_type(0x04), AudioCodec::Mp2);
        assert_eq!(AudioCodec::from_stream_type(0x81), AudioCodec::Ac3);
        assert_eq!(AudioCodec::from_stream_type(0x0F), AudioCodec::Aac);
        assert_eq!(AudioCodec::from_stream_type(0xFF), AudioCodec::Unknown);
    }

    #[test]
    fn test_demuxer_creation() {
        let demuxer = TsDemuxer::new();
        assert_eq!(demuxer.audio_pid(), None);
        assert_eq!(demuxer.audio_codec(), AudioCodec::Unknown);
    }
}
