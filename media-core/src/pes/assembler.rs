// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! Bounded common PES header assembler and canonical PTS/DTS timing extraction.
//!
//! This module provides the authoritative PES header and timing parser for all
//! video and audio streams. It buffers at most [`PES_TIMING_PREFIX_LEN`] (19 bytes)
//! across transport packet boundaries to extract [`PesTiming`], and skips remaining
//! optional header bytes before releasing elementary stream payload to callers.

use crate::timing::{ByteOffset, PesTiming, Pid, RawDts33, RawPts33, TimingEvent, TimingField};

/// The fixed header length for PES packets with optional headers:
/// start code (3), stream id (1), packet length (2), flags 1 (1), flags 2 (1), `header_data_length` (1).
pub const PES_FIXED_HEADER_LEN: usize = 9;

/// Maximum number of bytes occupied by PTS (5) + DTS (5) in a PES optional header.
pub const PES_PTS_DTS_BYTES: usize = 10;

/// The maximum prefix length required to parse PES timing:
/// fixed header (9) + PTS (5) + DTS (5) = 19 bytes.
pub const PES_TIMING_PREFIX_LEN: usize = PES_FIXED_HEADER_LEN + PES_PTS_DTS_BYTES;

/// Structural PES header metadata emitted once per valid PES packet.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct PesHeader {
    /// Stream identifier byte (e.g. `0xE0..=0xEF` for video, `0xBD`/`0xC0..=0xDF` for audio).
    pub stream_id: u8,
    /// Declared `PES_packet_length` (0 is legal for video and indicates unbounded length).
    pub packet_length: u16,
    /// Length of the optional header if present. `None` for stream types without optional header.
    pub header_data_length: Option<u8>,
    /// Timing parsed from the optional header.
    pub timing: PesTiming,
}

/// Output produced when feeding a transport packet payload to [`PesHeaderAssembler`].
#[derive(Debug, Default)]
pub struct PesAssembleOutput<'a> {
    /// Structural header emitted exactly once per PES packet upon header validation.
    pub header: Option<PesHeader>,
    /// Canonical timing event emitted once per PES packet carrying timing.
    pub timing_event: Option<TimingEvent>,
    /// Elementary stream payload bytes released from this packet, if any.
    pub es: Option<&'a [u8]>,
    /// Whether the optional header spanned past the PUSI transport packet.
    pub header_spanned_start_packet: bool,
}

/// Parses PES timing from a slice starting at the beginning of a PES packet (`0x00 0x00 0x01`).
///
/// # Invariants
/// - `PTS_DTS_flags == 00`: both `pts` and `dts` are [`TimingField::Absent`].
/// - `PTS_DTS_flags == 01`: forbidden by ISO/IEC 13818-1; both are [`TimingField::Invalid`].
/// - `PTS_DTS_flags == 10`: PTS present. Requires `header_data_length >= 5`, prefix `0b0010`,
///   and 3 marker bits (`bit 0 == 1` on bytes 9, 11, 13). `dts` is `Absent`.
/// - `PTS_DTS_flags == 11`: PTS + DTS present. Requires `header_data_length >= 10`.
///   PTS: prefix `0b0011`, 3 marker bits.
///   DTS: prefix `0b0001`, 3 marker bits.
#[must_use]
pub fn parse_pes_timing(header: &[u8]) -> PesTiming {
    if header.len() < super::MINIMUM_HEADER_LEN {
        return PesTiming::default();
    }
    let stream_id = header[3];
    if !super::has_optional_header(stream_id) {
        return PesTiming::default();
    }
    if header.len() < PES_FIXED_HEADER_LEN {
        return PesTiming {
            pts: TimingField::Invalid,
            dts: TimingField::Invalid,
        };
    }

    let flags2 = header[7];
    let pts_dts_flags = (flags2 >> 6) & 0x03;
    let header_data_len = usize::from(header[8]);

    match pts_dts_flags {
        0b00 => PesTiming {
            pts: TimingField::Absent,
            dts: TimingField::Absent,
        },
        0b01 => {
            // Forbidden by ISO/IEC 13818-1
            PesTiming {
                pts: TimingField::Invalid,
                dts: TimingField::Invalid,
            }
        }
        0b10 => {
            // PTS only
            if header_data_len < 5 || header.len() < 14 {
                return PesTiming {
                    pts: TimingField::Invalid,
                    dts: TimingField::Absent,
                };
            }
            let pts = decode_timestamp(&header[9..14], 0b0010).map_or(TimingField::Invalid, |v| {
                match RawPts33::new(v) {
                    Some(raw) => TimingField::Valid(raw),
                    None => TimingField::Invalid,
                }
            });
            PesTiming {
                pts,
                dts: TimingField::Absent,
            }
        }
        0b11 => {
            // PTS and DTS
            if header_data_len < 10 || header.len() < 19 {
                return PesTiming {
                    pts: TimingField::Invalid,
                    dts: TimingField::Invalid,
                };
            }
            let pts = decode_timestamp(&header[9..14], 0b0011).map_or(TimingField::Invalid, |v| {
                match RawPts33::new(v) {
                    Some(raw) => TimingField::Valid(raw),
                    None => TimingField::Invalid,
                }
            });
            let dts = decode_timestamp(&header[14..19], 0b0001).map_or(TimingField::Invalid, |v| {
                match RawDts33::new(v) {
                    Some(raw) => TimingField::Valid(raw),
                    None => TimingField::Invalid,
                }
            });
            PesTiming { pts, dts }
        }
        _ => unreachable!(),
    }
}

/// Decodes a 33-bit timestamp from 5 bytes, enforcing expected 4-bit prefix and 3 marker bits.
fn decode_timestamp(b: &[u8], expected_prefix: u8) -> Option<u64> {
    if b.len() < 5 {
        return None;
    }
    // Check 4-bit prefix on byte 0
    if (b[0] >> 4) & 0x0F != expected_prefix {
        return None;
    }
    // Check 3 marker bits: byte 0 bit 0, byte 2 bit 0, byte 4 bit 0 must all be 1
    if (b[0] & 0x01 != 1) || (b[2] & 0x01 != 1) || (b[4] & 0x01 != 1) {
        return None;
    }

    let val = (u64::from(b[0] & 0x0E) << 29)
        | (u64::from(b[1]) << 22)
        | (u64::from(b[2] & 0xFE) << 14)
        | (u64::from(b[3]) << 7)
        | (u64::from(b[4]) >> 1);

    Some(val)
}

/// Determines the required timing prefix length from flags and declared `header_data_length`.
#[must_use]
const fn compute_required_prefix_len(pts_dts_flags: u8, header_data_len: usize) -> usize {
    match pts_dts_flags {
        0b10 => {
            if header_data_len < 5 {
                PES_FIXED_HEADER_LEN
            } else {
                PES_FIXED_HEADER_LEN + 5
            }
        }
        0b11 => {
            if header_data_len < 10 {
                PES_FIXED_HEADER_LEN
            } else {
                PES_FIXED_HEADER_LEN + 10
            }
        }
        _ => PES_FIXED_HEADER_LEN,
    }
}

/// Internal state of the bounded PES header assembler.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum State {
    /// Awaiting next PUSI packet.
    AwaitingStart,
    /// Accumulating prefix bytes across packet boundaries.
    CollectingPrefix { required_len: usize },
    /// Timing has been extracted; skipping remaining optional header bytes.
    SkippingHeader { remaining: usize },
    /// Header complete; continuation payload is elementary stream.
    InElementaryStream,
    /// Stream ID was rejected by caller or header corrupted; ignore until next PUSI.
    Rejected,
}

/// Bounded common PES header assembler for one transport stream PID.
///
/// Buffers at most 19 bytes across packet boundaries to determine timing,
/// then skips any remaining optional header bytes before delivering
/// elementary stream payload.
#[derive(Debug)]
pub struct PesHeaderAssembler {
    pid: Pid,
    buf: [u8; PES_TIMING_PREFIX_LEN],
    buf_len: usize,
    pes_start_offset: ByteOffset,
    state: State,
    last_timing: Option<PesTiming>,
    spanned_start_packet: bool,
}

impl PesHeaderAssembler {
    /// Constructs a new assembler for `pid`.
    #[must_use]
    pub fn new(pid: Pid) -> Self {
        Self {
            pid,
            buf: [0u8; PES_TIMING_PREFIX_LEN],
            buf_len: 0,
            pes_start_offset: ByteOffset::new(0),
            state: State::InElementaryStream,
            last_timing: None,
            spanned_start_packet: false,
        }
    }

    /// Whether the assembler is currently accumulating or skipping header bytes.
    #[must_use]
    pub fn is_in_header(&self) -> bool {
        matches!(
            self.state,
            State::CollectingPrefix { .. } | State::SkippingHeader { .. }
        )
    }

    /// Resets the assembler to awaiting start (e.g. on continuity break, TEI, or scrambling).
    pub fn reset(&mut self) {
        self.buf_len = 0;
        self.state = State::AwaitingStart;
        self.last_timing = None;
        self.spanned_start_packet = false;
    }

    /// Rejects the current PES packet (e.g. stream ID rejected by caller).
    pub fn reject(&mut self) {
        self.buf_len = 0;
        self.state = State::Rejected;
        self.last_timing = None;
        self.spanned_start_packet = false;
    }

    /// The PID this assembler follows.
    #[must_use]
    pub fn pid(&self) -> Pid {
        self.pid
    }

    /// The subject byte offset of the PES packet currently in flight or most recently started.
    #[must_use]
    pub fn subject_at(&self) -> ByteOffset {
        self.pes_start_offset
    }

    /// The last parsed PES timing, if any. Cleared upon [`reset`](Self::reset).
    #[must_use]
    pub fn last_timing(&self) -> Option<PesTiming> {
        self.last_timing
    }

    /// Whether the assembler is currently in the elementary stream state.
    #[must_use]
    pub fn is_in_elementary_stream(&self) -> bool {
        matches!(self.state, State::InElementaryStream)
    }

    /// Whether the assembler is awaiting a new PES start (PUSI) or in rejected state.
    #[must_use]
    pub fn is_awaiting_start(&self) -> bool {
        matches!(self.state, State::AwaitingStart | State::Rejected)
    }

    /// Returns the number of optional header bytes still to be skipped, if in skipping header state.
    #[must_use]
    pub fn remaining_header_bytes(&self) -> Option<usize> {
        match self.state {
            State::SkippingHeader { remaining } => Some(remaining),
            _ => None,
        }
    }

    /// Feeds a packet payload that starts a PES packet (PUSI = true).
    #[allow(clippy::too_many_lines)]
    pub fn feed_pusi<'a>(
        &mut self,
        packet_offset: i64,
        payload: &'a [u8],
    ) -> PesAssembleOutput<'a> {
        self.buf_len = 0;
        self.pes_start_offset = ByteOffset::new(packet_offset);
        self.last_timing = None;
        self.spanned_start_packet = false;

        // Verify start code bytes available in this payload
        if payload.is_empty() || payload[0] != 0x00 {
            self.state = State::AwaitingStart;
            return PesAssembleOutput::default();
        }
        if payload.len() == 1 {
            self.buf[0] = 0x00;
            self.buf_len = 1;
            self.state = State::CollectingPrefix { required_len: 3 };
            self.spanned_start_packet = true;
            return PesAssembleOutput::default();
        }
        if payload[1] != 0x00 {
            self.state = State::AwaitingStart;
            return PesAssembleOutput::default();
        }
        if payload.len() == 2 {
            self.buf[0] = 0x00;
            self.buf[1] = 0x00;
            self.buf_len = 2;
            self.state = State::CollectingPrefix { required_len: 3 };
            self.spanned_start_packet = true;
            return PesAssembleOutput::default();
        }
        if payload[2] != 0x01 {
            self.state = State::AwaitingStart;
            return PesAssembleOutput::default();
        }

        // Start code (00 00 01) is complete. Need minimum header (6 bytes)
        if payload.len() < super::MINIMUM_HEADER_LEN {
            self.buf[..payload.len()].copy_from_slice(payload);
            self.buf_len = payload.len();
            self.state = State::CollectingPrefix {
                required_len: super::MINIMUM_HEADER_LEN,
            };
            self.spanned_start_packet = true;
            return PesAssembleOutput::default();
        }

        let stream_id = payload[3];
        let packet_length = u16::from_be_bytes([payload[4], payload[5]]);

        if !super::has_optional_header(stream_id) {
            let timing = PesTiming::default();
            self.last_timing = Some(timing);
            self.state = State::InElementaryStream;
            let header = PesHeader {
                stream_id,
                packet_length,
                header_data_length: None,
                timing,
            };
            let event = TimingEvent {
                observed_at: ByteOffset::new(packet_offset),
                subject_at: self.pes_start_offset,
                pid: self.pid,
                timing,
            };
            let es = &payload[super::MINIMUM_HEADER_LEN..];
            return PesAssembleOutput {
                header: Some(header),
                timing_event: Some(event),
                es: Some(es),
                header_spanned_start_packet: false,
            };
        }

        if payload.len() < PES_FIXED_HEADER_LEN {
            self.buf[..payload.len()].copy_from_slice(payload);
            self.buf_len = payload.len();
            self.state = State::CollectingPrefix {
                required_len: PES_FIXED_HEADER_LEN,
            };
            self.spanned_start_packet = true;
            return PesAssembleOutput::default();
        }

        let flags2 = payload[7];
        let pts_dts_flags = (flags2 >> 6) & 0x03;
        let header_data_len = usize::from(payload[8]);
        let required_len = compute_required_prefix_len(pts_dts_flags, header_data_len);

        if payload.len() < required_len {
            self.buf[..payload.len()].copy_from_slice(payload);
            self.buf_len = payload.len();
            self.state = State::CollectingPrefix { required_len };
            self.spanned_start_packet = true;
            return PesAssembleOutput::default();
        }

        // We have at least required_len bytes: timing can be extracted immediately!
        let timing = parse_pes_timing(&payload[..required_len]);
        self.last_timing = Some(timing);
        let header = PesHeader {
            stream_id,
            packet_length,
            header_data_length: Some(payload[8]),
            timing,
        };
        let event = TimingEvent {
            observed_at: ByteOffset::new(packet_offset),
            subject_at: self.pes_start_offset,
            pid: self.pid,
            timing,
        };

        let total_header_len = PES_FIXED_HEADER_LEN + header_data_len;

        if payload.len() >= total_header_len {
            self.state = State::InElementaryStream;
            let es = &payload[total_header_len..];
            PesAssembleOutput {
                header: Some(header),
                timing_event: Some(event),
                es: Some(es),
                header_spanned_start_packet: false,
            }
        } else {
            let remaining = total_header_len - payload.len();
            self.state = State::SkippingHeader { remaining };
            self.spanned_start_packet = true;
            PesAssembleOutput {
                header: Some(header),
                timing_event: Some(event),
                es: None,
                header_spanned_start_packet: true,
            }
        }
    }

    /// Feeds a continuation payload (PUSI = false).
    #[allow(clippy::too_many_lines)]
    pub fn feed_cont<'a>(
        &mut self,
        packet_offset: i64,
        payload: &'a [u8],
    ) -> PesAssembleOutput<'a> {
        match self.state {
            State::AwaitingStart | State::Rejected => PesAssembleOutput::default(),

            State::InElementaryStream => PesAssembleOutput {
                header: None,
                timing_event: None,
                es: Some(payload),
                header_spanned_start_packet: false,
            },

            State::SkippingHeader { ref mut remaining } => {
                if payload.len() < *remaining {
                    *remaining -= payload.len();
                    PesAssembleOutput::default()
                } else {
                    let rem = *remaining;
                    self.state = State::InElementaryStream;
                    PesAssembleOutput {
                        header: None,
                        timing_event: None,
                        es: Some(&payload[rem..]),
                        header_spanned_start_packet: false,
                    }
                }
            }

            State::CollectingPrefix { mut required_len } => {
                let mut pos = 0;

                // Step 1: Complete 3-byte start code (0x00 0x00 0x01)
                while self.buf_len < 3 && pos < payload.len() {
                    let b = payload[pos];
                    pos += 1;
                    if self.buf_len == 1 && b != 0x00 {
                        self.state = State::AwaitingStart;
                        return PesAssembleOutput::default();
                    }
                    if self.buf_len == 2 && b != 0x01 {
                        self.state = State::AwaitingStart;
                        return PesAssembleOutput::default();
                    }
                    self.buf[self.buf_len] = b;
                    self.buf_len += 1;
                }

                if self.buf_len < 3 {
                    return PesAssembleOutput::default();
                }

                // Step 2: Accumulate up to MINIMUM_HEADER_LEN (6)
                if self.buf_len < super::MINIMUM_HEADER_LEN {
                    let needed = super::MINIMUM_HEADER_LEN - self.buf_len;
                    let take = (payload.len() - pos).min(needed);
                    self.buf[self.buf_len..self.buf_len + take]
                        .copy_from_slice(&payload[pos..pos + take]);
                    self.buf_len += take;
                    pos += take;

                    if self.buf_len < super::MINIMUM_HEADER_LEN {
                        return PesAssembleOutput::default();
                    }
                }

                let stream_id = self.buf[3];
                let packet_length = u16::from_be_bytes([self.buf[4], self.buf[5]]);

                // If stream carries no optional header, data starts at offset 6
                if !super::has_optional_header(stream_id) {
                    let timing = PesTiming::default();
                    self.last_timing = Some(timing);
                    self.state = State::InElementaryStream;
                    let header = PesHeader {
                        stream_id,
                        packet_length,
                        header_data_length: None,
                        timing,
                    };
                    let event = TimingEvent {
                        observed_at: ByteOffset::new(packet_offset),
                        subject_at: self.pes_start_offset,
                        pid: self.pid,
                        timing,
                    };
                    return PesAssembleOutput {
                        header: Some(header),
                        timing_event: Some(event),
                        es: Some(&payload[pos..]),
                        header_spanned_start_packet: self.spanned_start_packet,
                    };
                }

                // Step 3: Accumulate up to PES_FIXED_HEADER_LEN (9)
                if self.buf_len < PES_FIXED_HEADER_LEN {
                    let needed = PES_FIXED_HEADER_LEN - self.buf_len;
                    let take = (payload.len() - pos).min(needed);
                    self.buf[self.buf_len..self.buf_len + take]
                        .copy_from_slice(&payload[pos..pos + take]);
                    self.buf_len += take;
                    pos += take;

                    if self.buf_len < PES_FIXED_HEADER_LEN {
                        self.state = State::CollectingPrefix {
                            required_len: PES_FIXED_HEADER_LEN,
                        };
                        return PesAssembleOutput::default();
                    }
                }

                // Step 4: Now we have 9 bytes; compute target timing prefix length
                let flags2 = self.buf[7];
                let pts_dts_flags = (flags2 >> 6) & 0x03;
                let header_data_len = usize::from(self.buf[8]);
                required_len = compute_required_prefix_len(pts_dts_flags, header_data_len);

                if self.buf_len < required_len {
                    let needed = required_len - self.buf_len;
                    let take = (payload.len() - pos).min(needed);
                    self.buf[self.buf_len..self.buf_len + take]
                        .copy_from_slice(&payload[pos..pos + take]);
                    self.buf_len += take;
                    pos += take;

                    if self.buf_len < required_len {
                        self.state = State::CollectingPrefix { required_len };
                        return PesAssembleOutput::default();
                    }
                }

                // Step 5: Timing prefix is complete; parse timing immediately!
                let timing = parse_pes_timing(&self.buf[..self.buf_len]);
                self.last_timing = Some(timing);
                let header = PesHeader {
                    stream_id,
                    packet_length,
                    header_data_length: Some(self.buf[8]),
                    timing,
                };
                let event = TimingEvent {
                    observed_at: ByteOffset::new(packet_offset),
                    subject_at: self.pes_start_offset,
                    pid: self.pid,
                    timing,
                };

                // Step 6: Skip remaining optional header bytes
                let total_header_len = PES_FIXED_HEADER_LEN + header_data_len;
                if total_header_len > self.buf_len {
                    let remaining_in_header = total_header_len - self.buf_len;
                    let remaining_in_packet = payload.len() - pos;

                    if remaining_in_packet >= remaining_in_header {
                        self.state = State::InElementaryStream;
                        pos += remaining_in_header;
                        PesAssembleOutput {
                            header: Some(header),
                            timing_event: Some(event),
                            es: Some(&payload[pos..]),
                            header_spanned_start_packet: self.spanned_start_packet,
                        }
                    } else {
                        let rem = remaining_in_header - remaining_in_packet;
                        self.state = State::SkippingHeader { remaining: rem };
                        PesAssembleOutput {
                            header: Some(header),
                            timing_event: Some(event),
                            es: None,
                            header_spanned_start_packet: self.spanned_start_packet,
                        }
                    }
                } else {
                    self.state = State::InElementaryStream;
                    PesAssembleOutput {
                        header: Some(header),
                        timing_event: Some(event),
                        es: Some(&payload[pos..]),
                        header_spanned_start_packet: self.spanned_start_packet,
                    }
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Helper to construct a 5-byte encoded PTS or DTS timestamp.
    fn encode_ts(prefix: u8, val: u64) -> [u8; 5] {
        let mut b = [0u8; 5];
        b[0] = (prefix << 4) | (u8::try_from((val >> 29) & 0x0E).unwrap()) | 1;
        b[1] = u8::try_from((val >> 22) & 0xFF).unwrap();
        b[2] = (u8::try_from((val >> 14) & 0xFE).unwrap()) | 1;
        b[3] = u8::try_from((val >> 7) & 0xFF).unwrap();
        b[4] = (u8::try_from((val << 1) & 0xFE).unwrap()) | 1;
        b
    }

    #[test]
    fn parse_timing_flags_00_absent() {
        let mut hdr = [0u8; 9];
        hdr[0..3].copy_from_slice(&[0x00, 0x00, 0x01]);
        hdr[3] = 0xE0; // video
        hdr[7] = 0x00; // pts_dts_flags == 00
        hdr[8] = 0x00; // header_data_length == 0

        let timing = parse_pes_timing(&hdr);
        assert!(timing.pts.is_absent());
        assert!(timing.dts.is_absent());
    }

    #[test]
    fn parse_timing_flags_01_forbidden() {
        let mut hdr = [0u8; 9];
        hdr[0..3].copy_from_slice(&[0x00, 0x00, 0x01]);
        hdr[3] = 0xE0;
        hdr[7] = 0x40; // pts_dts_flags == 01 (forbidden)
        hdr[8] = 0x05;

        let timing = parse_pes_timing(&hdr);
        assert!(timing.pts.is_invalid());
        assert!(timing.dts.is_invalid());
    }

    #[test]
    fn parse_timing_flags_10_pts_valid_and_bounds() {
        let mut hdr = [0u8; 14];
        hdr[0..3].copy_from_slice(&[0x00, 0x00, 0x01]);
        hdr[3] = 0xE0;
        hdr[7] = 0x80; // pts_dts_flags == 10
        hdr[8] = 0x05; // header_data_length == 5

        let target_pts_val = 0x1_2345_6789;
        hdr[9..14].copy_from_slice(&encode_ts(0b0010, target_pts_val));

        let timing = parse_pes_timing(&hdr);
        assert_eq!(
            timing.pts,
            TimingField::Valid(RawPts33::new(target_pts_val).unwrap())
        );
        assert!(timing.dts.is_absent());

        // Corrupt marker bit 0 on byte 9
        let mut hdr_bad = hdr;
        hdr_bad[9] &= 0xFE;
        let timing_bad = parse_pes_timing(&hdr_bad);
        assert!(timing_bad.pts.is_invalid());
        assert!(timing_bad.dts.is_absent());

        // Corrupt prefix on byte 9 (0b0011 instead of 0b0010)
        let mut hdr_bad_prefix = hdr;
        hdr_bad_prefix[9] = (hdr_bad_prefix[9] & 0x0F) | (0b0011 << 4);
        let timing_bad_pfx = parse_pes_timing(&hdr_bad_prefix);
        assert!(timing_bad_pfx.pts.is_invalid());

        // Header data length too short (< 5)
        let mut hdr_short = hdr;
        hdr_short[8] = 4;
        let timing_short = parse_pes_timing(&hdr_short);
        assert!(timing_short.pts.is_invalid());
    }

    #[test]
    fn parse_timing_flags_11_pts_dts_valid() {
        let mut hdr = [0u8; 19];
        hdr[0..3].copy_from_slice(&[0x00, 0x00, 0x01]);
        hdr[3] = 0xBD; // audio
        hdr[7] = 0xC0; // pts_dts_flags == 11
        hdr[8] = 0x0A; // header_data_length == 10

        let pts_val = 90_000 * 2; // 2 seconds
        let dts_val = 90_000; // 1 second
        hdr[9..14].copy_from_slice(&encode_ts(0b0011, pts_val));
        hdr[14..19].copy_from_slice(&encode_ts(0b0001, dts_val));

        let timing = parse_pes_timing(&hdr);
        assert_eq!(
            timing.pts,
            TimingField::Valid(RawPts33::new(pts_val).unwrap())
        );
        assert_eq!(
            timing.dts,
            TimingField::Valid(RawDts33::new(dts_val).unwrap())
        );

        // Corrupt DTS marker bit
        let mut hdr_bad_dts = hdr;
        hdr_bad_dts[18] &= 0xFE;
        let timing_bad_dts = parse_pes_timing(&hdr_bad_dts);
        assert_eq!(
            timing_bad_dts.pts,
            TimingField::Valid(RawPts33::new(pts_val).unwrap())
        );
        assert!(timing_bad_dts.dts.is_invalid());

        // Header data length too short (< 10)
        let mut hdr_short = hdr;
        hdr_short[8] = 9;
        let timing_short = parse_pes_timing(&hdr_short);
        assert!(timing_short.pts.is_invalid());
        assert!(timing_short.dts.is_invalid());
    }

    #[test]
    fn assembler_single_packet_complete() {
        let pid = Pid::new(256).unwrap();
        let mut assembler = PesHeaderAssembler::new(pid);

        let mut payload = vec![0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05];
        payload.extend_from_slice(&encode_ts(0b0010, 810_000));
        payload.extend_from_slice(&[0x11, 0x22, 0x33, 0x44]); // 4 bytes ES

        let out = assembler.feed_pusi(1000, &payload);
        let header = out.header.expect("header emitted");
        assert_eq!(header.stream_id, 0xE0);
        assert_eq!(header.packet_length, 0);
        assert_eq!(header.header_data_length, Some(5));

        let event = out.timing_event.expect("timing event emitted");
        assert_eq!(event.observed_at, ByteOffset::new(1000));
        assert_eq!(event.subject_at, ByteOffset::new(1000));
        assert_eq!(event.pid, pid);
        assert_eq!(
            event.timing.pts,
            TimingField::Valid(RawPts33::new(810_000).unwrap())
        );
        assert_eq!(out.es, Some(&[0x11, 0x22, 0x33, 0x44][..]));
        assert!(!out.header_spanned_start_packet);
        assert!(assembler.is_in_elementary_stream());
    }

    #[test]
    fn assembler_exhaustive_splits_1_to_19() {
        let pid = Pid::new(256).unwrap();
        let pts_val = 90_000 * 5;
        let dts_val = 90_000 * 4;

        // Construct a complete 19-byte PES header with PTS + DTS followed by 16 bytes ES
        let mut full_header = vec![0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0xC0, 0x0A];
        full_header.extend_from_slice(&encode_ts(0b0011, pts_val));
        full_header.extend_from_slice(&encode_ts(0b0001, dts_val));
        assert_eq!(full_header.len(), PES_TIMING_PREFIX_LEN);

        let es_data = vec![0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE, 0xBA, 0xBE];

        // Test every split point from 1 up to PES_TIMING_PREFIX_LEN - 1
        for split in 1..PES_TIMING_PREFIX_LEN {
            let mut assembler = PesHeaderAssembler::new(pid);

            let pusi_bytes = &full_header[..split];
            let mut cont_bytes = full_header[split..].to_vec();
            cont_bytes.extend_from_slice(&es_data);

            let out1 = assembler.feed_pusi(1000, pusi_bytes);
            // In all splits 1..19, prefix is incomplete in PUSI
            assert!(
                out1.header.is_none(),
                "split {split}: header should be None in PUSI"
            );
            assert!(
                out1.timing_event.is_none(),
                "split {split}: timing should be None in PUSI"
            );
            assert!(
                out1.es.is_none(),
                "split {split}: es should be None in PUSI"
            );
            assert!(!assembler.is_in_elementary_stream());

            let out2 = assembler.feed_cont(1188, &cont_bytes);
            let header = out2
                .header
                .unwrap_or_else(|| panic!("split {split}: header should be Some in cont"));
            assert_eq!(header.stream_id, 0xE0);
            assert_eq!(header.header_data_length, Some(10));

            let event = out2
                .timing_event
                .unwrap_or_else(|| panic!("split {split}: timing event should be Some in cont"));
            assert_eq!(
                event.observed_at,
                ByteOffset::new(1188),
                "split {split}: observed_at must be cont offset"
            );
            assert_eq!(
                event.subject_at,
                ByteOffset::new(1000),
                "split {split}: subject_at must be PUSI offset"
            );
            assert_eq!(
                event.timing.pts,
                TimingField::Valid(RawPts33::new(pts_val).unwrap())
            );
            assert_eq!(
                event.timing.dts,
                TimingField::Valid(RawDts33::new(dts_val).unwrap())
            );

            assert_eq!(
                out2.es,
                Some(&es_data[..]),
                "split {split}: ES released must match exactly"
            );
            assert!(
                out2.header_spanned_start_packet,
                "split {split}: header must be marked as spanned"
            );
            assert!(assembler.is_in_elementary_stream());
        }
    }

    #[test]
    fn assembler_adversarial_split_start_code_rejection() {
        let pid = Pid::new(256).unwrap();

        // 1. Split after 1 byte [0x00], followed by corrupt byte [0xFF]
        let mut assembler = PesHeaderAssembler::new(pid);
        let out1 = assembler.feed_pusi(1000, &[0x00]);
        assert!(out1.header.is_none());
        let out2 = assembler.feed_cont(1188, &[0xFF, 0x01, 0xE0, 0x00, 0x00]);
        assert!(out2.header.is_none());
        assert!(!assembler.is_in_elementary_stream());

        // 2. Split after 2 bytes [0x00, 0x00], followed by corrupt byte [0x02] (not 0x01)
        let mut assembler2 = PesHeaderAssembler::new(pid);
        let out1 = assembler2.feed_pusi(1000, &[0x00, 0x00]);
        assert!(out1.header.is_none());
        let out2 = assembler2.feed_cont(1188, &[0x02, 0xE0, 0x00, 0x00]);
        assert!(out2.header.is_none());
        assert!(!assembler2.is_in_elementary_stream());
    }

    #[test]
    fn assembler_reset_clears_last_timing() {
        let pid = Pid::new(256).unwrap();
        let mut assembler = PesHeaderAssembler::new(pid);

        let mut payload = vec![0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05];
        payload.extend_from_slice(&encode_ts(0b0010, 810_000));
        let out = assembler.feed_pusi(1000, &payload);
        assert!(out.header.is_some());
        assert!(assembler.last_timing().is_some());

        assembler.reset();
        assert!(assembler.last_timing().is_none());
        assert!(!assembler.is_in_elementary_stream());
    }
}
