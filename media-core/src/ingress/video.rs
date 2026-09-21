// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! Raw transport in, video elementary stream out.
//!
//! Everything below this module already existed and none of it is repeated here.
//! [`crate::transport`] says what a packet is, [`crate::psi`] says which PID
//! carries video and what codec it declares, and [`crate::pes`] says where a
//! PES packet begins and where its elementary stream does.
//!
//! What this module establishes is the foundation of video elementary stream
//! processing: tracking video PID and codec from PSI tables, evaluating transport
//! continuity and scrambling facts, validating PES headers, and statefully
//! parsing the elementary stream through an Annex-B start code and NAL unit
//! scanner (SPS/PPS/VPS parameter sets, slice headers, SEI recovery points,
//! and MPEG-2 picture headers).
//!
//! # The reference and the authored PES state machine
//!
//! The Go core in `backend/internal/stream/ingest/mediafacts` is the reference
//! for transport, scrambling facts (`vclr`, `vscr`, `vrun`, `scrconf`), and
//! parameter sets (`ps`).
//!
//! `crate::pes::read_start` is the authored structural PES truth.
//! The Go video path currently carries no cross-packet PES header state; this
//! module explicitly and intentionally tracks [`VideoPosition::InHeader`]:
//! - `InHeader` + clear sequential continuation: skip remaining header bytes,
//!   emit only bytes thereafter as ES, and transition to `InElementaryStream`.
//! - `InHeader` + scrambled continuation: header position lost -> transition
//!   to `AwaitingStart`.
//! - `InHeader` + transport break (`Broken`, `Discontinuous`, same-CC conflict):
//!   header boundary corrupted -> transition to `AwaitingStart`.
//!
//! # PUSI Boundary Invariant
//!
//! Every non-duplicate PUSI packet unconditionally marks the beginning of a new
//! PES and NAL boundary: any previous PES offset is cleared, parameter sets and
//! PES-local NAL observations are reset, and the Annex-B shift register is reset
//! *before* evaluating TEI, transport scrambling, same-CC conflict, or PES header
//! validation.

use crate::audio::observer::Observation;
use crate::ingress::audio::{AudioTrackState, AudioTracker};
use crate::pes::{self, PesHeaderAssembler};
use crate::psi::{ActivePsi, IngestError, PsiCore, PsiEvent, PsiFacts, VideoCodec};
use crate::timing::{PcrTracker, Pid, TimingEvent, TimingSnapshot};
use crate::transport::{Continuity, ContinuityTracker, PacketView, TS_PACKET_LEN};

/// Minimum consecutive scrambled packets required to conclusively confirm a stream as scrambled.
pub(crate) const SCRAMBLED_CONFIRMED_THRESHOLD: u64 = 100;

/// Number of bytes captured after an H.264 slice header NAL byte for slice type classification.
const SLICE_HEADER_CAPTURE_BYTES: usize = 12;

/// Number of bytes captured after an MPEG-2 picture start code for picture coding type.
const MPEG2_PICTURE_HEADER_CAPTURE_BYTES: usize = 3;

/// SEI payload type that marks a random access point (`recovery_point`).
const SEI_PAYLOAD_RECOVERY_POINT: usize = 6;

// H.264 NAL unit types
const H264_NAL_SLICE_NON_IDR: u8 = 1;
const H264_NAL_SLICE_PART_A: u8 = 2;
#[allow(dead_code)] // Used in Step 7c (AU/RAP evaluation)
const H264_NAL_SLICE_IDR: u8 = 5;
const H264_NAL_SEI: u8 = 6;
const H264_NAL_SPS: u8 = 7;
const H264_NAL_PPS: u8 = 8;

// HEVC NAL unit types
#[allow(dead_code)] // Used in Step 7c (AU/RAP evaluation)
const HEVC_NAL_IRAP_FIRST: u8 = 16;
#[allow(dead_code)] // Used in Step 7c (AU/RAP evaluation)
const HEVC_NAL_IRAP_LAST: u8 = 21;
const HEVC_NAL_VPS: u8 = 32;
const HEVC_NAL_SPS: u8 = 33;
const HEVC_NAL_PPS: u8 = 34;
const HEVC_NAL_PREFIX_SEI: u8 = 39;

// MPEG-2 Start Codes
const MPEG2_START_PICTURE: u8 = 0x00;
const MPEG2_START_SEQUENCE: u8 = 0xB3;
const MPEG2_START_GOP: u8 = 0xB8;

/// Strips `0x03` emulation prevention bytes inserted by encoders behind `0x00 0x00`.
#[must_use]
pub(crate) fn remove_emulation_prevention(src: &[u8]) -> Vec<u8> {
    let mut out = Vec::with_capacity(src.len());
    let mut zeros = 0;
    for &b in src {
        if zeros >= 2 && b == 0x03 {
            zeros = 0;
            continue;
        }
        if b == 0x00 {
            zeros += 1;
        } else {
            zeros = 0;
        }
        out.push(b);
    }
    out
}

/// Bitwise reader over an RBSP byte slice.
pub(crate) struct BitReader<'a> {
    data: &'a [u8],
    pos: usize,
}

impl<'a> BitReader<'a> {
    /// Constructs a bit reader for the given RBSP slice.
    #[must_use]
    pub(crate) fn new(data: &'a [u8]) -> Self {
        Self { data, pos: 0 }
    }

    /// Number of bits remaining unread.
    #[must_use]
    pub(crate) fn bits_left(&self) -> usize {
        (self.data.len().saturating_mul(8)).saturating_sub(self.pos)
    }

    /// Reads a single bit from the stream.
    pub(crate) fn read_bit(&mut self) -> Option<u32> {
        if self.bits_left() == 0 {
            return None;
        }
        let byte_idx = self.pos / 8;
        let bit_idx = 7 - (self.pos % 8);
        self.pos += 1;
        Some(u32::from((self.data[byte_idx] >> bit_idx) & 1))
    }

    /// Reads an unsigned Exp-Golomb coded integer (`ue(v)`).
    ///
    /// Rejects runs of leading zeros exceeding 32 bits to protect against unbounded loops
    /// on corrupted or non-slice bitstreams.
    pub(crate) fn read_ue(&mut self) -> Option<u32> {
        let mut leading_zeros = 0;
        loop {
            let bit = self.read_bit()?;
            if bit == 1 {
                break;
            }
            leading_zeros += 1;
            if leading_zeros > 32 {
                return None;
            }
        }
        if leading_zeros == 0 {
            return Some(0);
        }
        if leading_zeros > 31 {
            return None;
        }
        let mut val: u32 = 0;
        for _ in 0..leading_zeros {
            let bit = self.read_bit()?;
            val = (val << 1) | bit;
        }
        let prefix = (1u32).checked_shl(leading_zeros)?.checked_sub(1)?;
        prefix.checked_add(val)
    }
}

/// Reports whether an H.264 slice header specifies an intra-coded slice (`I` or `SI`).
///
/// Returns `(is_intra, ok)`. If the slice header cannot be completely parsed,
/// `ok` is `false`.
#[must_use]
pub(crate) fn h264_slice_is_intra(captured: &[u8]) -> (bool, bool) {
    let rbsp = remove_emulation_prevention(captured);
    let mut r = BitReader::new(&rbsp);
    if r.read_ue().is_none() {
        return (false, false);
    }
    let Some(slice_type) = r.read_ue() else {
        return (false, false);
    };
    let family = slice_type % 5;
    (family == 2 || family == 4, true)
}

/// State of the streaming SEI message reader.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(super) enum SeiParserState {
    ReadingType {
        accumulated: usize,
    },
    ReadingSize {
        payload_type: usize,
        accumulated: usize,
    },
    SkippingPayload {
        payload_type: usize,
        remaining: usize,
    },
    Done,
}

/// Streaming bounded SEI parser capable of traversing arbitrary-length SEI messages
/// and removing emulation prevention bytes across packet boundaries without allocation.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(super) struct SeiParser {
    pub(super) state: SeiParserState,
    pub(super) zero_count: usize,
    pub(super) skip_header: usize,
}

impl SeiParser {
    #[must_use]
    pub(super) fn new(skip_header: usize) -> Self {
        Self {
            state: SeiParserState::ReadingType { accumulated: 0 },
            zero_count: 0,
            skip_header,
        }
    }

    /// Feeds an elementary stream byte into the SEI parser.
    ///
    /// Returns `true` if a `recovery_point` (payload type 6) message was detected.
    pub(super) fn feed_byte(&mut self, b: u8) -> bool {
        if self.state == SeiParserState::Done {
            return false;
        }

        if self.skip_header > 0 {
            self.skip_header -= 1;
            return false;
        }

        if b == 0x00 {
            self.zero_count += 1;
            return false;
        }

        if b == 0x03 && self.zero_count == 2 {
            // Emulation prevention byte (00 00 03): drop 03, the preceding 00 00 are data.
            self.zero_count = 0;
            let r1 = self.feed_rbsp_byte(0x00);
            let r2 = self.feed_rbsp_byte(0x00);
            return r1 || r2;
        }

        let mut found = false;
        while self.zero_count > 0 {
            self.zero_count -= 1;
            if self.feed_rbsp_byte(0x00) {
                found = true;
            }
        }
        if self.feed_rbsp_byte(b) {
            found = true;
        }
        found
    }

    fn feed_rbsp_byte(&mut self, b: u8) -> bool {
        match self.state {
            SeiParserState::ReadingType {
                ref mut accumulated,
            } => {
                if *accumulated == 0 && b == 0x80 {
                    // rbsp_trailing_bits: clean end of SEI NAL
                    self.state = SeiParserState::Done;
                    return false;
                }
                if b == 0xFF {
                    *accumulated = accumulated.saturating_add(255);
                } else {
                    let payload_type = accumulated.saturating_add(usize::from(b));
                    self.state = SeiParserState::ReadingSize {
                        payload_type,
                        accumulated: 0,
                    };
                }
            }
            SeiParserState::ReadingSize {
                payload_type,
                ref mut accumulated,
            } => {
                if b == 0xFF {
                    *accumulated = accumulated.saturating_add(255);
                } else {
                    let payload_size = accumulated.saturating_add(usize::from(b));
                    if payload_type == SEI_PAYLOAD_RECOVERY_POINT {
                        self.state = SeiParserState::Done;
                        return true;
                    }
                    if payload_size == 0 {
                        self.state = SeiParserState::ReadingType { accumulated: 0 };
                    } else {
                        self.state = SeiParserState::SkippingPayload {
                            payload_type,
                            remaining: payload_size,
                        };
                    }
                }
            }
            SeiParserState::SkippingPayload {
                ref mut remaining, ..
            } => {
                *remaining = remaining.saturating_sub(1);
                if *remaining == 0 {
                    self.state = SeiParserState::ReadingType { accumulated: 0 };
                }
            }
            SeiParserState::Done => {}
        }
        false
    }
}

/// Reports whether an SEI NAL payload contains a `recovery_point` message.
#[cfg(test)]
#[must_use]
pub(crate) fn sei_has_recovery_point(captured: &[u8]) -> bool {
    let mut parser = SeiParser::new(0);
    for &b in captured {
        if parser.feed_byte(b) {
            return true;
        }
    }
    false
}

/// Reports whether an MPEG-2 picture header specifies an I-frame (`picture_coding_type == 1`).
#[must_use]
pub(crate) fn mpeg2_picture_is_intra(data: &[u8]) -> (bool, bool) {
    if data.len() < 2 {
        return (false, false);
    }
    let coding_type = (data[1] >> 3) & 0x07;
    if !(1..=3).contains(&coding_type) {
        return (false, false);
    }
    (coding_type == 1, true)
}

/// Kind of NAL body capture in progress.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(super) enum NalCaptureKind {
    None,
    H264SliceHeader,
    Mpeg2PictureHeader,
}

/// Where in a PES packet the next video payload begins.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(super) enum VideoPosition {
    /// A continuation payload is elementary stream.
    InElementaryStream,

    /// Nothing may be fed until a valid PES packet starts.
    AwaitingStart,
}

/// An event emitted during video stream ingestion or configuration.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum VideoEvent {
    /// The programme carrying this video stream changed identity (PMT update, target change, etc.).
    ProgramIdentityChanged,

    /// A valid random access point (attach point) was established.
    RandomAccessPoint {
        /// Byte offset in the caller's coordinate system where the access unit's PES began.
        offset: i64,
        /// Whether the access unit contained no scrambled transport packets.
        joinable: bool,
    },

    /// A previously published random access point was corrupted later in its access unit
    /// (e.g. by transport break, TEI, or scrambled packet) and is no longer an attach point.
    RandomAccessPointInvalidated {
        /// Byte offset of the invalidated PES start.
        offset: i64,
    },
}

/// One observable video elementary stream, followed across the packets that carry it.
#[derive(Debug)]
#[allow(clippy::struct_excessive_bools)]
pub(super) struct VideoFollower {
    pub(super) pid: u16,
    pub(super) codec: VideoCodec,
    pub(super) position: VideoPosition,
    pub(super) continuity: ContinuityTracker,
    pub(super) clear_packets: u64,
    pub(super) scrambled_packets: u64,
    pub(super) clear_run: u64,
    pub(super) current_pes_offset: Option<i64>,
    pub(super) pes_starts: u64,
    pub(super) pes_has_keyframe: bool,

    // NAL & Annex-B scanner state
    pub(super) annex_b_state: u32,
    pub(super) expecting_nal_byte: bool,
    pub(super) nal_kind: NalCaptureKind,
    pub(super) nal_left: usize,
    pub(super) nal_skip: usize,
    pub(super) nal_buf: Vec<u8>,
    pub(super) sei_parser: Option<SeiParser>,

    // Parameter sets & observations (PES-local)
    pub(super) pes_has_sps: bool,
    pub(super) pes_has_pps: bool,
    pub(super) pes_has_vps: bool,
    pub(super) pes_has_recovery_point: bool,

    // Access Unit state (AU-local)
    pub(super) au_has_irap: bool,
    pub(super) au_has_recovery_point: bool,
    pub(super) au_vcl_count: u64,
    pub(super) au_intra_vcl_count: u64,
    pub(super) au_predicted_vcl_count: u64,
    pub(super) au_scrambled_packets: u64,
    pub(super) au_continuity_broken: bool,
    pub(super) au_clean_rap_incremented: bool,
    pub(super) au_published_rap_offset: Option<i64>,
    pub(super) au_rap_invalidated: bool,

    // Cumulative facts counters
    pub(super) irap_points: u64,
    pub(super) intra_points: u64,
    pub(super) recovery_point_seis: u64,
    pub(super) predicted_rejected: u64,
    pub(super) unreadable_slices: u64,
    pub(super) clean_rap_count: u64,
    pub(super) clean_access_units: u64,
    pub(super) pes_assembler: PesHeaderAssembler,
}

impl VideoFollower {
    fn new(pid: u16, codec: VideoCodec) -> Self {
        let pid_typed = Pid::new(pid).expect("PMT parser only produces 13-bit PIDs");
        Self {
            pid,
            codec,
            position: VideoPosition::InElementaryStream,
            continuity: ContinuityTracker::new(),
            clear_packets: 0,
            scrambled_packets: 0,
            clear_run: 0,
            current_pes_offset: None,
            pes_starts: 0,
            pes_has_keyframe: false,
            annex_b_state: 0xFFFF_FFFF,
            expecting_nal_byte: false,
            nal_kind: NalCaptureKind::None,
            nal_left: 0,
            nal_skip: 0,
            nal_buf: Vec::new(),
            sei_parser: None,
            pes_has_sps: false,
            pes_has_pps: false,
            pes_has_vps: false,
            pes_has_recovery_point: false,
            au_has_irap: false,
            au_has_recovery_point: false,
            au_vcl_count: 0,
            au_intra_vcl_count: 0,
            au_predicted_vcl_count: 0,
            au_scrambled_packets: 0,
            au_continuity_broken: false,
            au_clean_rap_incremented: false,
            au_published_rap_offset: None,
            au_rap_invalidated: false,
            irap_points: 0,
            intra_points: 0,
            recovery_point_seis: 0,
            predicted_rejected: 0,
            unreadable_slices: 0,
            clean_rap_count: 0,
            clean_access_units: 0,
            pes_assembler: PesHeaderAssembler::new(pid_typed),
        }
    }

    /// Resets the PES boundary state and Annex-B scanner for a new payload unit.
    fn reset_pes_boundary(&mut self) {
        self.current_pes_offset = None;
        self.pes_has_keyframe = false;
        self.pes_has_sps = false;
        self.pes_has_pps = false;
        self.pes_has_vps = false;
        self.pes_has_recovery_point = false;
        self.reset_annex_b();
    }

    /// Resets only the per-AU tracking state for a new access unit.
    /// Never resets cumulative facts counters.
    fn reset_access_unit_state(&mut self) {
        self.au_has_irap = false;
        self.au_has_recovery_point = false;
        self.au_vcl_count = 0;
        self.au_intra_vcl_count = 0;
        self.au_predicted_vcl_count = 0;
        self.au_scrambled_packets = 0;
        self.au_continuity_broken = false;
        self.au_clean_rap_incremented = false;
        self.au_published_rap_offset = None;
        self.au_rap_invalidated = false;
    }

    /// Resets the Annex-B shift register and clears any in-progress NAL capture.
    fn reset_annex_b(&mut self) {
        self.annex_b_state = 0xFFFF_FFFF;
        self.expecting_nal_byte = false;
        self.nal_kind = NalCaptureKind::None;
        self.nal_left = 0;
        self.nal_skip = 0;
        self.nal_buf.clear();
        self.sei_parser = None;
    }

    fn begin_capture(&mut self, kind: NalCaptureKind, budget: usize, skip: usize) {
        self.nal_kind = kind;
        self.nal_left = budget;
        self.nal_skip = skip;
        self.nal_buf.clear();
    }

    fn consume_capture(&mut self, events: &mut Vec<VideoEvent>) {
        if self.nal_kind == NalCaptureKind::None || self.nal_buf.is_empty() {
            self.nal_kind = NalCaptureKind::None;
            self.nal_left = 0;
            self.nal_skip = 0;
            self.nal_buf.clear();
            return;
        }

        match self.nal_kind {
            NalCaptureKind::H264SliceHeader => {
                let (is_intra, ok) = h264_slice_is_intra(&self.nal_buf);
                if !ok {
                    self.unreadable_slices = self.unreadable_slices.saturating_add(1);
                } else if is_intra {
                    self.au_intra_vcl_count = self.au_intra_vcl_count.saturating_add(1);
                } else {
                    self.au_predicted_vcl_count = self.au_predicted_vcl_count.saturating_add(1);
                }
            }
            NalCaptureKind::Mpeg2PictureHeader => {
                let (is_intra, ok) = mpeg2_picture_is_intra(&self.nal_buf);
                if !ok {
                    self.unreadable_slices = self.unreadable_slices.saturating_add(1);
                } else if is_intra {
                    self.au_has_irap = true;
                    self.au_intra_vcl_count = self.au_intra_vcl_count.saturating_add(1);
                    self.index_random_access_point(true, events);
                } else {
                    self.au_predicted_vcl_count = self.au_predicted_vcl_count.saturating_add(1);
                }
            }
            NalCaptureKind::None => {}
        }

        self.nal_kind = NalCaptureKind::None;
        self.nal_left = 0;
        self.nal_skip = 0;
        self.nal_buf.clear();
    }

    /// Records the current access unit as a random access point (attach point).
    fn index_random_access_point(&mut self, irap: bool, events: &mut Vec<VideoEvent>) {
        if self.pes_has_keyframe || self.current_pes_offset.is_none() {
            return;
        }
        self.pes_has_keyframe = true;

        if irap {
            self.irap_points = self.irap_points.saturating_add(1);
        } else {
            self.intra_points = self.intra_points.saturating_add(1);
        }

        if self.au_has_recovery_point {
            self.recovery_point_seis = self.recovery_point_seis.saturating_add(1);
        }

        if self.au_scrambled_packets == 0 {
            self.clean_rap_count = self.clean_rap_count.saturating_add(1);
            self.au_clean_rap_incremented = true;
        }

        let offset = self.current_pes_offset.unwrap();
        self.au_published_rap_offset = Some(offset);
        events.push(VideoEvent::RandomAccessPoint {
            offset,
            joinable: self.au_scrambled_packets == 0,
        });
    }

    /// Invalidates a previously emitted random access point if it was corrupted later in its AU.
    fn invalidate_published_rap(&mut self, events: &mut Vec<VideoEvent>) {
        if self.au_clean_rap_incremented {
            self.clean_rap_count = self.clean_rap_count.saturating_sub(1);
            self.au_clean_rap_incremented = false;
        }
        if self.au_rap_invalidated {
            return;
        }
        if let Some(offset) = self.au_published_rap_offset {
            self.au_rap_invalidated = true;
            events.push(VideoEvent::RandomAccessPointInvalidated { offset });
        }
    }

    /// Finalizes the access unit that just ended at a PES boundary.
    fn finalize_access_unit(&mut self, events: &mut Vec<VideoEvent>) {
        self.consume_capture(events);

        if self.current_pes_offset.is_none() || self.au_vcl_count == 0 {
            return;
        }

        if self.au_scrambled_packets == 0 {
            self.clean_access_units = self.clean_access_units.saturating_add(1);
        }

        if self.au_continuity_broken || self.pes_has_keyframe {
            return;
        }

        // Decoder configuration gate:
        // H.264: SPS + PPS
        // H.265: VPS + SPS + PPS
        // MPEG-2: Sequence Header (0xB3, stored in pes_has_sps)
        let has_parameter_sets = match self.codec {
            VideoCodec::H264 => self.pes_has_sps && self.pes_has_pps,
            VideoCodec::H265 => self.pes_has_sps && self.pes_has_pps && self.pes_has_vps,
            VideoCodec::Mpeg2 => self.pes_has_sps,
            VideoCodec::Unknown => false,
        };

        if !has_parameter_sets {
            return;
        }

        let joinable = match self.codec {
            VideoCodec::H264 => {
                self.au_vcl_count > 0 && self.au_intra_vcl_count == self.au_vcl_count
            }
            VideoCodec::H265 => self.au_has_recovery_point,
            VideoCodec::Mpeg2 | VideoCodec::Unknown => false,
        };

        if !joinable {
            if self.au_has_recovery_point || self.au_predicted_vcl_count > 0 {
                self.predicted_rejected = self.predicted_rejected.saturating_add(1);
            }
            return;
        }

        self.index_random_access_point(false, events);
    }

    /// Feeds elementary stream bytes into the stateful Annex-B scanner.
    fn feed_es(&mut self, es: &[u8], events: &mut Vec<VideoEvent>) {
        for &b in es {
            self.annex_b_state = (self.annex_b_state << 8) | u32::from(b);
            let is_start_code = (self.annex_b_state & 0x00FF_FFFF) == 0x0000_0001;

            if is_start_code {
                self.sei_parser = None;

                if self.nal_left > 0 {
                    if self.nal_buf.ends_with(&[0x00, 0x00]) {
                        self.nal_buf.truncate(self.nal_buf.len() - 2);
                    }
                    self.consume_capture(events);
                }

                self.expecting_nal_byte = true;
                continue;
            }

            if self.expecting_nal_byte {
                self.expecting_nal_byte = false;
                self.consume_capture(events);

                match self.codec {
                    VideoCodec::H264 => self.classify_h264(b, events),
                    VideoCodec::H265 => self.classify_hevc(b, events),
                    VideoCodec::Mpeg2 => self.classify_mpeg2(b),
                    VideoCodec::Unknown => {}
                }
                continue;
            }

            if let Some(ref mut sei) = self.sei_parser {
                if sei.feed_byte(b) {
                    self.pes_has_recovery_point = true;
                    self.au_has_recovery_point = true;
                }
            } else if self.nal_left > 0 {
                if self.nal_skip > 0 {
                    self.nal_skip -= 1;
                } else {
                    self.nal_buf.push(b);
                    self.nal_left -= 1;
                    if self.nal_left == 0 {
                        self.consume_capture(events);
                    }
                }
            }
        }
    }

    fn classify_h264(&mut self, b: u8, events: &mut Vec<VideoEvent>) {
        match b & 0x1F {
            H264_NAL_SLICE_IDR => {
                self.au_has_irap = true;
                self.au_vcl_count = self.au_vcl_count.saturating_add(1);
                self.au_intra_vcl_count = self.au_intra_vcl_count.saturating_add(1);
                self.index_random_access_point(true, events);
            }
            H264_NAL_SLICE_NON_IDR | H264_NAL_SLICE_PART_A => {
                self.au_vcl_count = self.au_vcl_count.saturating_add(1);
                self.begin_capture(
                    NalCaptureKind::H264SliceHeader,
                    SLICE_HEADER_CAPTURE_BYTES,
                    0,
                );
            }
            H264_NAL_SPS => {
                self.pes_has_sps = true;
            }
            H264_NAL_PPS => {
                self.pes_has_pps = true;
            }
            H264_NAL_SEI => {
                self.sei_parser = Some(SeiParser::new(0));
            }
            _ => {}
        }
    }

    fn classify_hevc(&mut self, b: u8, events: &mut Vec<VideoEvent>) {
        let nal_type = (b >> 1) & 0x3F;
        match nal_type {
            HEVC_NAL_VPS => {
                self.pes_has_vps = true;
            }
            HEVC_NAL_SPS => {
                self.pes_has_sps = true;
            }
            HEVC_NAL_PPS => {
                self.pes_has_pps = true;
            }
            HEVC_NAL_PREFIX_SEI => {
                self.sei_parser = Some(SeiParser::new(1));
            }
            t if (HEVC_NAL_IRAP_FIRST..=HEVC_NAL_IRAP_LAST).contains(&t) => {
                self.au_has_irap = true;
                self.au_vcl_count = self.au_vcl_count.saturating_add(1);
                self.au_intra_vcl_count = self.au_intra_vcl_count.saturating_add(1);
                self.index_random_access_point(true, events);
            }
            t if t <= 9 => {
                self.au_vcl_count = self.au_vcl_count.saturating_add(1);
                self.au_predicted_vcl_count = self.au_predicted_vcl_count.saturating_add(1);
            }
            _ => {}
        }
    }

    fn classify_mpeg2(&mut self, b: u8) {
        match b {
            MPEG2_START_SEQUENCE => {
                self.pes_has_sps = true;
            }
            MPEG2_START_GOP => {
                self.pes_has_vps = true;
            }
            MPEG2_START_PICTURE => {
                self.au_vcl_count = self.au_vcl_count.saturating_add(1);
                self.begin_capture(
                    NalCaptureKind::Mpeg2PictureHeader,
                    MPEG2_PICTURE_HEADER_CAPTURE_BYTES,
                    0,
                );
            }
            _ => {}
        }
    }

    fn parameter_sets_seen(&self) -> bool {
        match self.codec {
            VideoCodec::H265 => self.pes_has_sps && self.pes_has_pps && self.pes_has_vps,
            VideoCodec::Mpeg2 => self.pes_has_sps,
            _ => self.pes_has_sps && self.pes_has_pps,
        }
    }
}

/// One run of video elementary stream bytes, extracted from a transport packet.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct VideoFeed<'a> {
    /// Which incarnation of the stream this belonged to.
    pub incarnation: u64,
    /// The PID it arrived on.
    pub pid: u16,
    /// Caller-owned monotonic byte offset of the packet that produced this feed.
    pub offset: i64,
    /// Whether this payload packet signaled payload unit start (PUSI).
    pub pusi: bool,
    /// The elementary stream bytes, borrowed from the packet payload.
    pub es: &'a [u8],
}

/// What one chunk of transport stream produced for video.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct VideoOutcome<'a> {
    /// The caller-owned offset one past the last byte interpreted.
    pub processed_through: i64,
    /// The feeds the chunk produced, in the order they occurred.
    pub feeds: Vec<VideoFeed<'a>>,
    /// Events emitted during processing of this chunk, in exact order of occurrence.
    pub events: Vec<VideoEvent>,
    /// Timing events emitted during processing of this chunk, in exact order of occurrence.
    pub timing_events: Vec<TimingEvent>,
}

/// Facts established about the video elementary stream.
#[derive(Debug, Clone, PartialEq, Eq)]
#[allow(clippy::struct_excessive_bools)]
pub struct VideoFacts {
    /// The PID carrying the video stream, or 0 if none has been declared.
    pub pid: u16,
    /// The video codec declared by the PMT.
    pub codec: VideoCodec,
    /// Clear packets seen on the video PID since the programme began.
    pub clear_packets: u64,
    /// Scrambled packets seen on the video PID since the programme began.
    pub scrambled_packets: u64,
    /// Number of consecutive clear video packets seen since the last scrambled packet.
    pub clear_run: u64,
    /// Whether the video elementary stream is confirmed scrambled.
    pub scrambled_confirmed: bool,
    /// Whether the video follower is currently quarantining payloads until a valid PES start.
    pub awaiting_start: bool,
    /// The caller's byte offset of the packet that began the most recent valid video PES packet.
    pub current_pes_offset: Option<i64>,
    /// Number of valid video PES packet starts observed.
    pub pes_starts: u64,
    /// Whether parameter sets configuring a decoder were observed in the current PES packet.
    pub parameter_sets_seen: bool,
    /// Whether an SPS (or MPEG-2 Sequence Header) was seen in the current PES packet.
    pub pes_has_sps: bool,
    /// Whether a PPS was seen in the current PES packet.
    pub pes_has_pps: bool,
    /// Whether a VPS (or MPEG-2 GOP Header) was seen in the current PES packet.
    pub pes_has_vps: bool,
    /// Whether an SEI recovery point was observed in the current PES packet.
    pub pes_has_recovery_point: bool,
    /// Number of slices whose headers could not be read.
    pub unreadable_slices: u64,
    /// Number of IRAP access units observed.
    pub irap_points: u64,
    /// Number of non-IRAP all-intra access units observed.
    pub intra_points: u64,
    /// Number of admitted access units that carried an SEI recovery point.
    pub recovery_point_seis: u64,
    /// Number of access units carrying parameter sets that were rejected as predicted.
    pub predicted_rejected: u64,
    /// Number of entry points whose own access unit contained no scrambled packet.
    pub clean_rap_count: u64,
    /// Number of complete access units that arrived without an encrypted packet.
    pub clean_access_units: u64,
}

/// A point-in-time projection of PSI facts, active tables, video facts, and audio facts.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct VideoSnapshot {
    /// What the core knows about the transport programme's PSI tables.
    pub psi: PsiFacts,
    /// The active table sections in force.
    pub active_psi: ActivePsi,
    /// The current facts established about the video stream.
    pub video: VideoFacts,
    /// Global audio scrambling facts: (`audio_scrambled`, `audio_clear`, `audio_clear_run`).
    pub audio_scrambling: (u64, u64, u64),
    /// Per-track observations in the order of declared audio tracks: (pid, observation).
    pub audio_observations: Vec<(u16, Observation)>,
}

/// Follows the observable video stream of one transport programme.
#[derive(Debug)]
pub struct VideoIngress {
    psi: PsiCore,
    follower: Option<VideoFollower>,
    audio: AudioTracker,
    timing: PcrTracker,
    incarnation: u64,
}

impl VideoIngress {
    /// An ingress following one programme, or whichever the PAT offers first.
    #[must_use]
    pub fn new(target_program_number: u16) -> Self {
        Self {
            psi: PsiCore::new(target_program_number),
            follower: None,
            audio: AudioTracker::new(),
            timing: PcrTracker::new(),
            incarnation: 0,
        }
    }

    /// A point-in-time projection of PSI, video, and audio facts.
    #[must_use]
    pub fn snapshot(&self) -> VideoSnapshot {
        let psi_snap = self.psi.snapshot();
        let audio_observations = psi_snap
            .facts
            .audio_tracks
            .iter()
            .map(|track| {
                let obs = self
                    .audio
                    .tracks
                    .iter()
                    .find(|t| t.pid == track.pid)
                    .map_or_else(Observation::default, AudioTrackState::observation);
                (track.pid, obs)
            })
            .collect();
        VideoSnapshot {
            psi: psi_snap.facts,
            active_psi: psi_snap.active,
            video: self.facts(),
            audio_scrambling: (
                self.audio.scrambled_packets,
                self.audio.clear_packets,
                self.audio.clear_run,
            ),
            audio_observations,
        }
    }

    /// Reference to the audio tracker holding all audio streams and scrambling counters.
    #[must_use]
    pub fn audio_tracker(&self) -> &AudioTracker {
        &self.audio
    }

    /// A point-in-time snapshot of the timing tracker's state.
    #[must_use]
    pub fn timing_snapshot(&self) -> TimingSnapshot {
        self.timing.snapshot()
    }

    /// Selects the programme to follow, returning any identity events emitted.
    pub fn set_target_program(&mut self, program_number: u16) -> Vec<VideoEvent> {
        let outcome = self.psi.set_target_program(program_number);
        let mut events = Vec::new();
        for event in outcome.events {
            if event == PsiEvent::ProgramIdentityChanged {
                self.reprogram();
                events.push(VideoEvent::ProgramIdentityChanged);
            }
        }
        events
    }

    /// Which incarnation the stream being followed belongs to.
    #[must_use]
    pub fn incarnation(&self) -> u64 {
        self.incarnation
    }

    /// The current facts established about the video stream.
    #[must_use]
    pub fn facts(&self) -> VideoFacts {
        if let Some(ref f) = self.follower {
            VideoFacts {
                pid: f.pid,
                codec: f.codec,
                clear_packets: f.clear_packets,
                scrambled_packets: f.scrambled_packets,
                clear_run: f.clear_run,
                scrambled_confirmed: f.clear_packets == 0
                    && f.scrambled_packets >= SCRAMBLED_CONFIRMED_THRESHOLD,
                awaiting_start: matches!(f.position, VideoPosition::AwaitingStart),
                current_pes_offset: f.current_pes_offset,
                pes_starts: f.pes_starts,
                parameter_sets_seen: f.parameter_sets_seen(),
                pes_has_sps: f.pes_has_sps,
                pes_has_pps: f.pes_has_pps,
                pes_has_vps: f.pes_has_vps,
                pes_has_recovery_point: f.pes_has_recovery_point,
                unreadable_slices: f.unreadable_slices,
                irap_points: f.irap_points,
                intra_points: f.intra_points,
                recovery_point_seis: f.recovery_point_seis,
                predicted_rejected: f.predicted_rejected,
                clean_rap_count: f.clean_rap_count,
                clean_access_units: f.clean_access_units,
            }
        } else {
            VideoFacts {
                pid: self.psi.video_pid(),
                codec: self.psi.video_codec(),
                clear_packets: 0,
                scrambled_packets: 0,
                clear_run: 0,
                scrambled_confirmed: false,
                awaiting_start: false,
                current_pes_offset: None,
                pes_starts: 0,
                parameter_sets_seen: false,
                pes_has_sps: false,
                pes_has_pps: false,
                pes_has_vps: false,
                pes_has_recovery_point: false,
                unreadable_slices: 0,
                irap_points: 0,
                intra_points: 0,
                recovery_point_seis: 0,
                predicted_rejected: 0,
                clean_rap_count: 0,
                clean_access_units: 0,
            }
        }
    }

    /// Interprets one chunk of transport.
    ///
    /// # Errors
    ///
    /// Returns [`IngestError::UnalignedChunk`] when the chunk is not an exact multiple
    /// of 188-byte transport stream packets.
    pub fn ingest<'a>(
        &mut self,
        start_offset: i64,
        data: &'a [u8],
    ) -> Result<VideoOutcome<'a>, IngestError> {
        if !data.len().is_multiple_of(TS_PACKET_LEN) {
            return Err(IngestError::UnalignedChunk { len: data.len() });
        }
        let mut feeds = Vec::new();
        let mut events = Vec::new();
        let mut timing_events = Vec::new();
        self.psi.begin_chunk();
        for (packet_idx, packet) in data.chunks_exact(TS_PACKET_LEN).enumerate() {
            let rel_offset =
                i64::try_from(packet_idx.saturating_mul(TS_PACKET_LEN)).unwrap_or(i64::MAX);
            let packet_offset = start_offset.saturating_add(rel_offset);
            let identity_changed_count = self
                .psi
                .index_packet(packet)
                .iter()
                .filter(|&&e| e == PsiEvent::ProgramIdentityChanged)
                .count();
            for _ in 0..identity_changed_count {
                self.reprogram();
                events.push(VideoEvent::ProgramIdentityChanged);
            }
            let Ok(view) = PacketView::parse(packet) else {
                continue;
            };
            self.timing.observe(packet_offset, &view);
            self.route(
                packet_offset,
                &view,
                &mut feeds,
                &mut events,
                &mut timing_events,
            );
        }
        let consumed = i64::try_from(data.len()).unwrap_or(i64::MAX);
        Ok(VideoOutcome {
            processed_through: start_offset.saturating_add(consumed),
            feeds,
            events,
            timing_events,
        })
    }

    /// Resets the follower when the programme's identity changes.
    fn reprogram(&mut self) {
        self.incarnation += 1;
        let vpid = self.psi.video_pid();
        if vpid > 0 {
            self.follower = Some(VideoFollower::new(vpid, self.psi.video_codec()));
        } else {
            self.follower = None;
        }
        self.audio.reset_with_tracks(self.psi.audio_tracks());
        self.timing.rebind(self.psi.pcr_pid().and_then(Pid::new));
    }

    /// Routes one packet to the video follower or audio tracker.
    #[allow(clippy::too_many_lines)]
    fn route<'a>(
        &mut self,
        packet_offset: i64,
        view: &PacketView<'a>,
        out_feeds: &mut Vec<VideoFeed<'a>>,
        out_events: &mut Vec<VideoEvent>,
        out_timing_events: &mut Vec<TimingEvent>,
    ) {
        let pid = view.pid();
        if pid == 0 || pid == self.psi.pmt_pid() {
            return;
        }
        let is_video = self.follower.as_ref().is_some_and(|f| f.pid == pid);
        if !is_video {
            if self.audio.handles_pid(pid) {
                let (_feed, audio_timing) = self.audio.route(packet_offset, view);
                if let Some(evt) = audio_timing {
                    out_timing_events.push(evt);
                }
            }
            return;
        }
        let Some(ref mut follower) = self.follower else {
            return;
        };
        if pid != follower.pid {
            return;
        }

        let Some(payload) = view.payload() else {
            if view.discontinuity_indicator() {
                follower.continuity.observe_adaptation_only(true);
            }
            return;
        };

        let is_same_cc = follower.continuity.is_same_cc(view.continuity_counter());
        let continuity = follower.continuity.observe(
            view.bytes(),
            view.continuity_counter(),
            view.discontinuity_indicator(),
        );

        if continuity == Continuity::Duplicate {
            return;
        }

        let is_pusi = view.payload_unit_start();

        if is_pusi {
            let broken_boundary =
                continuity == Continuity::Broken || continuity == Continuity::Discontinuous;

            if broken_boundary {
                follower.au_continuity_broken = true;
                follower.pes_assembler.reset();
            }

            // 1. Finalize the preceding AU before evaluating the new payload unit
            follower.finalize_access_unit(out_events);

            // 2. Invalidate any provisional RAP published by the old AU if boundary was broken
            if broken_boundary {
                follower.invalidate_published_rap(out_events);
            }

            // 3. Reset PES boundary and per-AU state
            follower.reset_pes_boundary();
            follower.reset_access_unit_state();

            // 4. Handle TEI, scrambling, same-CC conflict, and PES header validation for new PUSI
            if view.transport_error_indicator() {
                follower.au_continuity_broken = true;
                follower.position = VideoPosition::AwaitingStart;
                follower.invalidate_published_rap(out_events);
                follower.pes_assembler.reset();
                return;
            }

            if view.scrambling_control() != 0 {
                follower.scrambled_packets = follower.scrambled_packets.saturating_add(1);
                follower.au_scrambled_packets = follower.au_scrambled_packets.saturating_add(1);
                follower.clear_run = 0;
                follower.position = VideoPosition::AwaitingStart;
                follower.invalidate_published_rap(out_events);
                follower.pes_assembler.reset();
                return;
            }

            follower.clear_packets = follower.clear_packets.saturating_add(1);
            follower.clear_run = follower.clear_run.saturating_add(1);

            let same_cc_conflict =
                continuity == Continuity::Broken && is_same_cc && !view.discontinuity_indicator();
            if same_cc_conflict {
                follower.position = VideoPosition::AwaitingStart;
                follower.pes_assembler.reset();
                return;
            }

            // Extract timing & elementary stream via bounded common PES assembler
            let output = follower.pes_assembler.feed_pusi(packet_offset, payload);
            if let Some(header) = output.header {
                if !pes::is_video_stream_id(header.stream_id) {
                    follower.pes_assembler.reject();
                    follower.position = VideoPosition::AwaitingStart;
                    return;
                }
                follower.current_pes_offset = Some(follower.pes_assembler.subject_at().get());
                follower.position = VideoPosition::InElementaryStream;
                follower.pes_starts += 1;
            } else if follower.pes_assembler.is_awaiting_start() {
                // Invalid start code or rejected stream on PUSI
                follower.position = VideoPosition::AwaitingStart;
                return;
            }

            if let Some(evt) = output.timing_event {
                out_timing_events.push(evt);
            }

            if let Some(es) = output.es {
                follower.feed_es(es, out_events);
                out_feeds.push(VideoFeed {
                    incarnation: self.incarnation,
                    pid: follower.pid,
                    offset: packet_offset,
                    pusi: true,
                    es,
                });
            }
            return;
        }

        // Continuation packet (is_pusi == false)
        if view.transport_error_indicator() {
            follower.pes_assembler.reset();
            if !matches!(follower.position, VideoPosition::AwaitingStart) {
                follower.reset_annex_b();
                follower.au_continuity_broken = true;
                follower.position = VideoPosition::AwaitingStart;
                follower.invalidate_published_rap(out_events);
            }
            return;
        }

        if view.scrambling_control() != 0 {
            follower.scrambled_packets = follower.scrambled_packets.saturating_add(1);
            follower.au_scrambled_packets = follower.au_scrambled_packets.saturating_add(1);
            follower.clear_run = 0;
            follower.invalidate_published_rap(out_events);
            if follower.pes_assembler.is_in_header() {
                follower.pes_assembler.reset();
                follower.position = VideoPosition::AwaitingStart;
            }
            return;
        }

        follower.clear_packets = follower.clear_packets.saturating_add(1);
        follower.clear_run = follower.clear_run.saturating_add(1);

        let same_cc_conflict =
            continuity == Continuity::Broken && is_same_cc && !view.discontinuity_indicator();

        if continuity == Continuity::Broken
            || continuity == Continuity::Discontinuous
            || same_cc_conflict
        {
            follower.reset_annex_b();
            follower.au_continuity_broken = true;
            follower.position = VideoPosition::AwaitingStart;
            follower.invalidate_published_rap(out_events);
            follower.pes_assembler.reset();
            return;
        }

        if follower.pes_assembler.is_awaiting_start() {
            follower.position = VideoPosition::AwaitingStart;
            return;
        }

        // Extract timing & elementary stream via bounded common PES assembler
        let output = follower.pes_assembler.feed_cont(packet_offset, payload);
        if let Some(header) = output.header {
            if !pes::is_video_stream_id(header.stream_id) {
                follower.pes_assembler.reject();
                follower.position = VideoPosition::AwaitingStart;
                return;
            }
            follower.current_pes_offset = Some(follower.pes_assembler.subject_at().get());
            follower.position = VideoPosition::InElementaryStream;
            follower.pes_starts += 1;
        }

        if let Some(evt) = output.timing_event {
            out_timing_events.push(evt);
        }

        if let Some(es) = output.es {
            follower.feed_es(es, out_events);
            out_feeds.push(VideoFeed {
                incarnation: self.incarnation,
                pid: follower.pid,
                offset: packet_offset,
                pusi: false,
                es,
            });
        }
    }
}
