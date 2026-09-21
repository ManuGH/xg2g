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
    /// Accumulating prefix bytes up to `required_len` (<= 19 bytes).
    CollectingPrefix { required_len: usize },
    /// Timing has been extracted; skipping remaining optional header bytes.
    SkippingHeader { remaining: usize },
    /// Header complete; continuation payload is elementary stream.
    InElementaryStream,
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
            state: State::AwaitingStart,
            last_timing: None,
        }
    }

    /// Resets the assembler to awaiting start (e.g. on continuity break, TEI, or scrambling).
    pub fn reset(&mut self) {
        self.buf_len = 0;
        self.state = State::AwaitingStart;
    }

    /// The PID this assembler follows.
    #[must_use]
    pub fn pid(&self) -> Pid {
        self.pid
    }

    /// The last parsed PES timing, if any.
    #[must_use]
    pub fn last_timing(&self) -> Option<PesTiming> {
        self.last_timing
    }

    /// Whether the assembler is currently in the elementary stream state.
    #[must_use]
    pub fn is_in_elementary_stream(&self) -> bool {
        matches!(self.state, State::InElementaryStream)
    }

    /// Feeds a packet payload that starts a PES packet (PUSI = true).
    ///
    /// Returns `(Option<TimingEvent>, Option<&[u8]>)`.
    pub fn feed_pusi<'a>(
        &mut self,
        packet_offset: i64,
        payload: &'a [u8],
    ) -> (Option<TimingEvent>, Option<&'a [u8]>) {
        self.buf_len = 0;
        self.pes_start_offset = ByteOffset::new(packet_offset);

        // Verify PES start code: 0x00 0x00 0x01
        if payload.len() < 3 || payload[..3] != [0x00, 0x00, 0x01] {
            self.state = State::AwaitingStart;
            return (None, None);
        }

        if payload.len() < super::MINIMUM_HEADER_LEN {
            self.buf[..payload.len()].copy_from_slice(payload);
            self.buf_len = payload.len();
            self.state = State::CollectingPrefix {
                required_len: super::MINIMUM_HEADER_LEN,
            };
            return (None, None);
        }

        let stream_id = payload[3];
        if !super::has_optional_header(stream_id) {
            let timing = PesTiming::default();
            self.last_timing = Some(timing);
            self.state = State::InElementaryStream;
            let event = TimingEvent {
                observed_at: ByteOffset::new(packet_offset),
                subject_at: self.pes_start_offset,
                pid: self.pid,
                timing,
            };
            let es = &payload[super::MINIMUM_HEADER_LEN..];
            return (Some(event), Some(es));
        }

        if payload.len() < PES_FIXED_HEADER_LEN {
            self.buf[..payload.len()].copy_from_slice(payload);
            self.buf_len = payload.len();
            self.state = State::CollectingPrefix {
                required_len: PES_FIXED_HEADER_LEN,
            };
            return (None, None);
        }

        let flags2 = payload[7];
        let pts_dts_flags = (flags2 >> 6) & 0x03;
        let header_data_len = usize::from(payload[8]);
        let required_len = compute_required_prefix_len(pts_dts_flags, header_data_len);

        if payload.len() < required_len {
            self.buf[..payload.len()].copy_from_slice(payload);
            self.buf_len = payload.len();
            self.state = State::CollectingPrefix { required_len };
            return (None, None);
        }

        // We have at least required_len bytes: timing can be extracted immediately!
        let timing = parse_pes_timing(&payload[..required_len]);
        self.last_timing = Some(timing);
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
            (Some(event), Some(es))
        } else {
            let remaining = total_header_len - payload.len();
            self.state = State::SkippingHeader { remaining };
            (Some(event), None)
        }
    }

    /// Feeds a continuation payload (PUSI = false).
    ///
    /// Returns `(Option<TimingEvent>, Option<&[u8]>)`.
    pub fn feed_cont<'a>(
        &mut self,
        packet_offset: i64,
        payload: &'a [u8],
    ) -> (Option<TimingEvent>, Option<&'a [u8]>) {
        match self.state {
            State::AwaitingStart => (None, None),

            State::CollectingPrefix { mut required_len } => {
                let mut pos = 0;

                // Step 1: Accumulate up to MINIMUM_HEADER_LEN (6)
                if self.buf_len < super::MINIMUM_HEADER_LEN {
                    let needed = super::MINIMUM_HEADER_LEN - self.buf_len;
                    let take = (payload.len() - pos).min(needed);
                    self.buf[self.buf_len..self.buf_len + take]
                        .copy_from_slice(&payload[pos..pos + take]);
                    self.buf_len += take;
                    pos += take;

                    if self.buf_len < super::MINIMUM_HEADER_LEN {
                        return (None, None);
                    }
                }

                // If stream carries no optional header, data starts at offset 6
                if !super::has_optional_header(self.buf[3]) {
                    let timing = PesTiming::default();
                    self.last_timing = Some(timing);
                    self.state = State::InElementaryStream;
                    let event = TimingEvent {
                        observed_at: ByteOffset::new(packet_offset),
                        subject_at: self.pes_start_offset,
                        pid: self.pid,
                        timing,
                    };
                    return (Some(event), Some(&payload[pos..]));
                }

                // Step 2: Accumulate up to PES_FIXED_HEADER_LEN (9)
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
                        return (None, None);
                    }
                }

                // Step 3: Now we have 9 bytes; compute target timing prefix length
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
                        return (None, None);
                    }
                }

                // Step 4: Timing prefix is complete; parse timing immediately!
                let timing = parse_pes_timing(&self.buf[..self.buf_len]);
                self.last_timing = Some(timing);
                let event = TimingEvent {
                    observed_at: ByteOffset::new(packet_offset),
                    subject_at: self.pes_start_offset,
                    pid: self.pid,
                    timing,
                };

                // Step 5: Skip remaining optional header bytes
                let total_header_len = PES_FIXED_HEADER_LEN + header_data_len;
                if total_header_len > self.buf_len {
                    let remaining_in_header = total_header_len - self.buf_len;
                    let remaining_in_packet = payload.len() - pos;

                    if remaining_in_packet >= remaining_in_header {
                        self.state = State::InElementaryStream;
                        pos += remaining_in_header;
                        (Some(event), Some(&payload[pos..]))
                    } else {
                        let rem = remaining_in_header - remaining_in_packet;
                        self.state = State::SkippingHeader { remaining: rem };
                        (Some(event), None)
                    }
                } else {
                    self.state = State::InElementaryStream;
                    (Some(event), Some(&payload[pos..]))
                }
            }

            State::SkippingHeader { ref mut remaining } => {
                if payload.len() < *remaining {
                    *remaining -= payload.len();
                    (None, None)
                } else {
                    let rem = *remaining;
                    self.state = State::InElementaryStream;
                    (None, Some(&payload[rem..]))
                }
            }

            State::InElementaryStream => (None, Some(payload)),
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

        let target_pts = 0x1_2345_6789;
        hdr[9..14].copy_from_slice(&encode_ts(0b0010, target_pts));

        let timing = parse_pes_timing(&hdr);
        assert_eq!(
            timing.pts,
            TimingField::Valid(RawPts33::new(target_pts).unwrap())
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

        let target_pts = 90_000 * 2; // 2 seconds
        let target_dts = 90_000 * 1; // 1 second
        hdr[9..14].copy_from_slice(&encode_ts(0b0011, target_pts));
        hdr[14..19].copy_from_slice(&encode_ts(0b0001, target_dts));

        let timing = parse_pes_timing(&hdr);
        assert_eq!(
            timing.pts,
            TimingField::Valid(RawPts33::new(target_pts).unwrap())
        );
        assert_eq!(
            timing.dts,
            TimingField::Valid(RawDts33::new(target_dts).unwrap())
        );

        // Corrupt DTS marker bit
        let mut hdr_bad_dts = hdr;
        hdr_bad_dts[18] &= 0xFE;
        let timing_bad_dts = parse_pes_timing(&hdr_bad_dts);
        assert_eq!(
            timing_bad_dts.pts,
            TimingField::Valid(RawPts33::new(target_pts).unwrap())
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

        let (event, es) = assembler.feed_pusi(1000, &payload);
        let event = event.expect("timing event emitted");
        assert_eq!(event.observed_at, ByteOffset::new(1000));
        assert_eq!(event.subject_at, ByteOffset::new(1000));
        assert_eq!(event.pid, pid);
        assert_eq!(
            event.timing.pts,
            TimingField::Valid(RawPts33::new(810_000).unwrap())
        );
        assert_eq!(es, Some(&[0x11, 0x22, 0x33, 0x44][..]));
        assert!(assembler.is_in_elementary_stream());
    }

    #[test]
    fn assembler_split_header_across_packets_dual_coordinates() {
        let pid = Pid::new(256).unwrap();
        let mut assembler = PesHeaderAssembler::new(pid);

        // Packet 1 (PUSI): only 10 bytes of PES packet arrived (9 fixed header + 1 byte of PTS)
        let mut p1 = vec![0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05];
        let encoded_pts = encode_ts(0b0010, 999_000);
        p1.push(encoded_pts[0]);

        let (event1, es1) = assembler.feed_pusi(0, &p1);
        assert!(event1.is_none());
        assert!(es1.is_none());
        assert!(!assembler.is_in_elementary_stream());

        // Packet 2 (Continuation at offset 188): remaining 4 bytes of PTS + 10 bytes of ES
        let mut p2 = encoded_pts[1..].to_vec();
        p2.extend_from_slice(&[0xAA, 0xBB, 0xCC]);

        let (event2, es2) = assembler.feed_cont(188, &p2);
        let event = event2.expect("timing event emitted on packet 2");
        assert_eq!(event.observed_at, ByteOffset::new(188)); // observed in packet 2
        assert_eq!(event.subject_at, ByteOffset::new(0)); // subject at packet 1
        assert_eq!(
            event.timing.pts,
            TimingField::Valid(RawPts33::new(999_000).unwrap())
        );
        assert_eq!(es2, Some(&[0xAA, 0xBB, 0xCC][..]));
        assert!(assembler.is_in_elementary_stream());
    }

    #[test]
    fn assembler_large_optional_header_timing_immediate_skips_remainder() {
        let pid = Pid::new(256).unwrap();
        let mut assembler = PesHeaderAssembler::new(pid);

        // header_data_length = 30 bytes (5 bytes PTS + 25 bytes other optional fields)
        let mut p1 = vec![0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 30];
        let encoded_pts = encode_ts(0b0010, 450_000);
        p1.extend_from_slice(&encoded_pts);
        p1.extend_from_slice(&[0xFF; 10]); // 10 bytes of additional header in p1

        // In p1: 9 + 5 + 10 = 24 bytes total. Total header is 9 + 30 = 39 bytes.
        // 15 bytes of header remain to be skipped.
        let (event1, es1) = assembler.feed_pusi(0, &p1);
        let event = event1.expect("timing emitted immediately after 14 bytes");
        assert_eq!(event.observed_at, ByteOffset::new(0));
        assert_eq!(event.subject_at, ByteOffset::new(0));
        assert_eq!(
            event.timing.pts,
            TimingField::Valid(RawPts33::new(450_000).unwrap())
        );
        assert!(es1.is_none()); // ES not started yet because 15 header bytes remain
        assert!(!assembler.is_in_elementary_stream());

        // Packet 2: provides 15 remaining header bytes + 5 bytes ES
        let mut p2 = vec![0xFF; 15];
        p2.extend_from_slice(&[0xDE, 0xAD, 0xBE, 0xEF]);

        let (event2, es2) = assembler.feed_cont(188, &p2);
        assert!(event2.is_none()); // no duplicate timing event!
        assert_eq!(es2, Some(&[0xDE, 0xAD, 0xBE, 0xEF][..]));
        assert!(assembler.is_in_elementary_stream());
    }

    #[test]
    fn assembler_short_header_data_len_emits_invalid_immediately() {
        let pid = Pid::new(256).unwrap();
        let mut assembler = PesHeaderAssembler::new(pid);

        // Declares PTS (flags == 10), but header_data_length == 2
        let p1 = vec![
            0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x02, 0xAA, 0xBB, 0xCC,
        ];
        let (event, es) = assembler.feed_pusi(500, &p1);
        let event = event.expect("timing emitted immediately as invalid");
        assert!(event.timing.pts.is_invalid());
        assert_eq!(es, Some(&[0xCC][..])); // 9 + 2 = 11 bytes header, index 11 is 0xCC
        assert!(assembler.is_in_elementary_stream());
    }
}
