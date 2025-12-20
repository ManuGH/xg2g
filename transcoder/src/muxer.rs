// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! MPEG-TS Muxer
//!
//! This module provides functionality to multiplex AAC audio into MPEG Transport Stream.
//! It creates properly formatted TS packets with PES encapsulation, PAT/PMT tables,
//! and timestamp management.
//!
//! # MPEG-TS Structure
//!
//! ```text
//! Transport Stream
//! ├── PAT (Program Association Table) - PID 0x0000
//! ├── PMT (Program Map Table) - PID 0x1000 (configurable)
//! └── Elementary Streams
//!     ├── Video (PID 0x0100) - passthrough
//!     └── Audio (PID 0x0101) - AAC remuxed
//! ```
//!
//! # Usage
//!
//! ```rust,ignore
//! use crate::muxer::{TsMuxer, TsMuxerConfig};
//!
//! let config = TsMuxerConfig::default();
//! let mut muxer = TsMuxer::new(config);
//!
//! // Mux AAC frame
//! let ts_packets = muxer.mux_audio(&aac_data, pts, dts)?;
//! for packet in ts_packets {
//!     output.write_all(&packet)?;
//! }
//! ```

use anyhow::Result;
use tracing::{debug, trace};

use crate::demux::TS_PACKET_SIZE;

/// MPEG-TS Muxer Configuration
#[derive(Debug, Clone)]
pub struct TsMuxerConfig {
    /// Audio PID
    pub audio_pid: u16,

    /// Video PID (for passthrough)
    pub video_pid: u16,

    /// PCR PID (Program Clock Reference)
    pub pcr_pid: u16,

    /// PMT PID (Program Map Table)
    pub pmt_pid: u16,

    /// Transport Stream ID
    pub transport_stream_id: u16,

    /// Program Number
    pub program_number: u16,
}

impl Default for TsMuxerConfig {
    fn default() -> Self {
        Self {
            audio_pid: 0x0101,      // 257
            video_pid: 0x0100,      // 256
            pcr_pid: 0x0100,        // Use video PID for PCR
            pmt_pid: 0x1000,        // 4096
            transport_stream_id: 1,
            program_number: 1,
        }
    }
}

/// MPEG-TS Muxer
///
/// Multiplexes AAC audio into Transport Stream packets.
pub struct TsMuxer {
    /// Configuration
    config: TsMuxerConfig,

    /// Continuity counter for audio PID (0-15)
    audio_continuity: u8,

    /// Continuity counter for video PID (0-15)
    video_continuity: u8,

    /// Continuity counter for PAT (0-15)
    pat_continuity: u8,

    /// Continuity counter for PMT (0-15)
    pmt_continuity: u8,

    /// Current PCR value (27 MHz)
    #[allow(dead_code)]
    pcr: u64,

    /// Packets muxed
    packets_muxed: u64,

    /// PAT/PMT regeneration interval (packets)
    psi_interval: u64,
}

impl TsMuxer {
    /// Create a new MPEG-TS muxer
    pub fn new(config: TsMuxerConfig) -> Self {
        debug!(
            "Creating TS muxer: audio PID {}, video PID {}",
            config.audio_pid, config.video_pid
        );

        Self {
            config,
            audio_continuity: 0,
            video_continuity: 0,
            pat_continuity: 0,
            pmt_continuity: 0,
            pcr: 0,
            packets_muxed: 0,
            psi_interval: 40, // Regenerate PAT/PMT every 40 packets (~1 per second at 50 Mbps)
        }
    }

    /// Mux AAC audio frame into TS packets
    ///
    /// # Arguments
    ///
    /// * `aac_data` - AAC frame with ADTS header
    /// * `pts` - Presentation Time Stamp (90 kHz)
    /// * `dts` - Decode Time Stamp (90 kHz)
    ///
    /// # Returns
    ///
    /// * Vector of 188-byte TS packets
    pub fn mux_audio(&mut self, aac_data: &[u8], pts: u64, dts: u64) -> Result<Vec<[u8; 188]>> {
        let mut packets = Vec::new();

        // Check if we need to regenerate PSI tables
        if self.packets_muxed % self.psi_interval == 0 {
            packets.push(self.generate_pat());
            packets.push(self.generate_pmt());
        }

        // Create PES packet from AAC data
        let pes_data = self.create_pes_packet(aac_data, pts, dts)?;

        // Fragment PES into TS packets
        let ts_packets = self.create_ts_packets(&pes_data, self.config.audio_pid, true)?;
        packets.extend(ts_packets);

        self.packets_muxed += packets.len() as u64;

        Ok(packets)
    }

    /// Pass through video TS packet
    ///
    /// Updates continuity counter for video PID.
    pub fn passthrough_video(&mut self, packet: &[u8; 188]) -> Result<[u8; 188]> {
        let mut output = *packet;

        // Update continuity counter
        output[3] = (output[3] & 0xF0) | self.video_continuity;
        self.video_continuity = (self.video_continuity + 1) & 0x0F;

        self.packets_muxed += 1;

        Ok(output)
    }

    /// Create PES packet from AAC data
    ///
    /// PES packet structure:
    /// - Start code (3 bytes): 0x000001
    /// - Stream ID (1 byte): 0xC0 for audio
    /// - PES length (2 bytes)
    /// - PES header flags (2 bytes)
    /// - PES header length (1 byte)
    /// - Optional fields (PTS, DTS, etc.)
    /// - PES data (AAC)
    fn create_pes_packet(&self, data: &[u8], pts: u64, dts: u64) -> Result<Vec<u8>> {
        let mut pes = Vec::with_capacity(data.len() + 32);

        // PES start code
        pes.extend_from_slice(&[0x00, 0x00, 0x01]);

        // Stream ID (0xC0 = audio stream 0)
        pes.push(0xC0);

        // PES packet length (data + header - 6 bytes)
        // For audio, this should be accurate (not 0)
        let header_len = 13; // PES header with PTS/DTS
        let pes_length = (data.len() + header_len - 6) as u16;
        pes.push((pes_length >> 8) as u8);
        pes.push((pes_length & 0xFF) as u8);

        // PES header flags
        // '10' (fixed) + scrambling (00) + priority (0) + data alignment (1) + copyright (0) + original (0)
        pes.push(0x84); // 10000100

        // PTS/DTS flags (11 = both present) + ESCR (0) + ES rate (0) + DSM trick (0) + additional copy (0) + CRC (0) + extension (0)
        pes.push(0xC0); // 11000000 (PTS + DTS)

        // PES header data length
        pes.push(10); // 5 bytes PTS + 5 bytes DTS

        // PTS (5 bytes) - Presentation Time Stamp
        // Format: '0011' + PTS[32:30] + marker + PTS[29:15] + marker + PTS[14:0] + marker
        Self::write_timestamp(&mut pes, 0x03, pts);

        // DTS (5 bytes) - Decode Time Stamp
        // Format: '0001' + DTS[32:30] + marker + DTS[29:15] + marker + DTS[14:0] + marker
        Self::write_timestamp(&mut pes, 0x01, dts);

        // PES data (AAC)
        pes.extend_from_slice(data);

        trace!("Created PES packet: {} bytes (data: {})", pes.len(), data.len());

        Ok(pes)
    }

    /// Write timestamp (PTS or DTS) in PES format
    ///
    /// Format: prefix(4 bits) + ts[32:30] + marker + ts[29:15] + marker + ts[14:0] + marker
    fn write_timestamp(buffer: &mut Vec<u8>, prefix: u8, ts: u64) {
        // Byte 0: prefix[3:0] + ts[32:30] + marker
        buffer.push(((prefix << 4) | ((ts >> 29) as u8 & 0x0E) | 0x01) as u8);

        // Byte 1-2: ts[29:15] + marker
        buffer.push(((ts >> 22) & 0xFF) as u8);
        buffer.push((((ts >> 14) & 0xFE) | 0x01) as u8);

        // Byte 3-4: ts[14:0] + marker
        buffer.push(((ts >> 7) & 0xFF) as u8);
        buffer.push((((ts << 1) & 0xFE) | 0x01) as u8);
    }

    /// Fragment PES packet into TS packets
    ///
    /// # Arguments
    ///
    /// * `pes_data` - Complete PES packet
    /// * `pid` - PID for these packets
    /// * `is_audio` - true if audio stream (for continuity counter)
    fn create_ts_packets(
        &mut self,
        pes_data: &[u8],
        pid: u16,
        is_audio: bool,
    ) -> Result<Vec<[u8; 188]>> {
        let mut packets = Vec::new();
        let mut offset = 0;
        let mut first_packet = true;

        while offset < pes_data.len() {
            let mut packet = [0xFF_u8; TS_PACKET_SIZE]; // Padding with 0xFF

            // Sync byte
            packet[0] = 0x47;

            // Header byte 1: PUSI (if first) + Transport Priority + PID[12:8]
            packet[1] = if first_packet { 0x40 } else { 0x00 } | ((pid >> 8) as u8 & 0x1F);

            // Header byte 2: PID[7:0]
            packet[2] = (pid & 0xFF) as u8;

            // Header byte 3: Scrambling (00) + Adaptation (01) + Continuity
            let continuity = if is_audio {
                let cc = self.audio_continuity;
                self.audio_continuity = (self.audio_continuity + 1) & 0x0F;
                cc
            } else {
                let cc = self.video_continuity;
                self.video_continuity = (self.video_continuity + 1) & 0x0F;
                cc
            };

            packet[3] = 0x10 | continuity; // Payload present, no adaptation field

            // Calculate payload size
            let payload_start = 4;
            let available = TS_PACKET_SIZE - payload_start;
            let remaining = pes_data.len() - offset;
            let to_copy = available.min(remaining);

            // Copy payload
            packet[payload_start..payload_start + to_copy]
                .copy_from_slice(&pes_data[offset..offset + to_copy]);

            // If this is the last packet and doesn't fill the payload, padding (0xFF) is already there

            packets.push(packet);
            offset += to_copy;
            first_packet = false;
        }

        trace!("Fragmented PES into {} TS packets", packets.len());

        Ok(packets)
    }

    /// Generate PAT (Program Association Table) packet
    ///
    /// PAT maps program numbers to PMT PIDs.
    pub fn generate_pat(&mut self) -> [u8; 188] {
        let mut packet = [0xFF_u8; TS_PACKET_SIZE];

        // TS Header
        packet[0] = 0x47; // Sync byte
        packet[1] = 0x40; // PUSI set, PID = 0x0000
        packet[2] = 0x00;
        packet[3] = 0x10 | self.pat_continuity; // Payload present
        self.pat_continuity = (self.pat_continuity + 1) & 0x0F;

        // Pointer field (0 = table starts immediately)
        packet[4] = 0x00;

        // PAT Table
        let mut offset = 5;

        // Table ID (0x00 = PAT)
        packet[offset] = 0x00;
        offset += 1;

        // Section syntax indicator + reserved + section length
        let section_length = 13; // 5 (header after length) + 4 (program entry) + 4 (CRC)
        packet[offset] = 0xB0 | ((section_length >> 8) as u8);
        packet[offset + 1] = (section_length & 0xFF) as u8;
        offset += 2;

        // Transport stream ID
        packet[offset] = (self.config.transport_stream_id >> 8) as u8;
        packet[offset + 1] = (self.config.transport_stream_id & 0xFF) as u8;
        offset += 2;

        // Version + current/next
        packet[offset] = 0xC1; // Version 0, current
        offset += 1;

        // Section number
        packet[offset] = 0x00;
        offset += 1;

        // Last section number
        packet[offset] = 0x00;
        offset += 1;

        // Program entry: Program number + PMT PID
        packet[offset] = (self.config.program_number >> 8) as u8;
        packet[offset + 1] = (self.config.program_number & 0xFF) as u8;
        packet[offset + 2] = 0xE0 | ((self.config.pmt_pid >> 8) as u8);
        packet[offset + 3] = (self.config.pmt_pid & 0xFF) as u8;
        offset += 4;

        // CRC32 (simplified - should be calculated properly)
        // For now, use dummy CRC (proper implementation would calculate actual CRC)
        packet[offset..offset + 4].copy_from_slice(&[0x00, 0x00, 0x00, 0x00]);

        trace!("Generated PAT");

        packet
    }

    /// Generate PMT (Program Map Table) packet
    ///
    /// PMT maps elementary stream PIDs and types.
    pub fn generate_pmt(&mut self) -> [u8; 188] {
        let mut packet = [0xFF_u8; TS_PACKET_SIZE];

        // TS Header
        packet[0] = 0x47; // Sync byte
        packet[1] = 0x40 | ((self.config.pmt_pid >> 8) as u8 & 0x1F); // PUSI set
        packet[2] = (self.config.pmt_pid & 0xFF) as u8;
        packet[3] = 0x10 | self.pmt_continuity; // Payload present
        self.pmt_continuity = (self.pmt_continuity + 1) & 0x0F;

        // Pointer field
        packet[4] = 0x00;

        // PMT Table
        let mut offset = 5;

        // Table ID (0x02 = PMT)
        packet[offset] = 0x02;
        offset += 1;

        // Section syntax indicator + section length
        let section_length = 18; // Header + streams + CRC
        packet[offset] = 0xB0 | ((section_length >> 8) as u8);
        packet[offset + 1] = (section_length & 0xFF) as u8;
        offset += 2;

        // Program number
        packet[offset] = (self.config.program_number >> 8) as u8;
        packet[offset + 1] = (self.config.program_number & 0xFF) as u8;
        offset += 2;

        // Version + current/next
        packet[offset] = 0xC1;
        offset += 1;

        // Section number
        packet[offset] = 0x00;
        offset += 1;

        // Last section number
        packet[offset] = 0x00;
        offset += 1;

        // PCR PID
        packet[offset] = 0xE0 | ((self.config.pcr_pid >> 8) as u8);
        packet[offset + 1] = (self.config.pcr_pid & 0xFF) as u8;
        offset += 2;

        // Program info length (0 = no descriptors)
        packet[offset] = 0xF0;
        packet[offset + 1] = 0x00;
        offset += 2;

        // Stream entry: Video (H.264)
        packet[offset] = 0x1B; // Stream type (H.264)
        packet[offset + 1] = 0xE0 | ((self.config.video_pid >> 8) as u8);
        packet[offset + 2] = (self.config.video_pid & 0xFF) as u8;
        packet[offset + 3] = 0xF0; // ES info length
        packet[offset + 4] = 0x00;
        offset += 5;

        // Stream entry: Audio (AAC)
        packet[offset] = 0x0F; // Stream type (AAC ADTS)
        packet[offset + 1] = 0xE0 | ((self.config.audio_pid >> 8) as u8);
        packet[offset + 2] = (self.config.audio_pid & 0xFF) as u8;
        packet[offset + 3] = 0xF0; // ES info length
        packet[offset + 4] = 0x00;
        offset += 5;

        // CRC32 (dummy)
        packet[offset..offset + 4].copy_from_slice(&[0x00, 0x00, 0x00, 0x00]);

        trace!("Generated PMT");

        packet
    }

    /// Get statistics
    pub fn stats(&self) -> (u64, u8, u8) {
        (
            self.packets_muxed,
            self.audio_continuity,
            self.video_continuity,
        )
    }
}

impl Default for TsMuxer {
    fn default() -> Self {
        Self::new(TsMuxerConfig::default())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_muxer_creation() {
        let config = TsMuxerConfig::default();
        let muxer = TsMuxer::new(config);

        assert_eq!(muxer.audio_continuity, 0);
        assert_eq!(muxer.video_continuity, 0);
    }

    #[test]
    fn test_pat_generation() {
        let mut muxer = TsMuxer::default();
        let pat = muxer.generate_pat();

        assert_eq!(pat[0], 0x47); // Sync byte
        assert_eq!(pat[1] & 0x40, 0x40); // PUSI set
        assert_eq!(pat[5], 0x00); // Table ID = PAT
    }

    #[test]
    fn test_pmt_generation() {
        let mut muxer = TsMuxer::default();
        let pmt = muxer.generate_pmt();

        assert_eq!(pmt[0], 0x47); // Sync byte
        assert_eq!(pmt[1] & 0x40, 0x40); // PUSI set
        assert_eq!(pmt[5], 0x02); // Table ID = PMT
    }

    #[test]
    fn test_continuity_counter() {
        let mut muxer = TsMuxer::default();

        // Mux some dummy data
        let aac_data = vec![0xFF; 100];
        let packets = muxer.mux_audio(&aac_data, 0, 0).unwrap();

        // Check that continuity counters increment
        assert!(packets.len() > 2); // Should have PAT, PMT, and data packets

        // Continuity should have incremented
        assert_eq!(muxer.pat_continuity, 1);
        assert_eq!(muxer.pmt_continuity, 1);
        assert!(muxer.audio_continuity > 0);
    }

    #[test]
    fn test_ts_packet_size() {
        let mut muxer = TsMuxer::default();
        let aac_data = vec![0xFF; 500];
        let packets = muxer.mux_audio(&aac_data, 90000, 90000).unwrap();

        // All packets must be exactly 188 bytes
        for packet in packets {
            assert_eq!(packet.len(), TS_PACKET_SIZE);
        }
    }
}
