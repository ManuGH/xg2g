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

use crate::pes::{self, PesStart};
use crate::psi::{IngestError, PsiCore, PsiEvent, VideoCodec};
use crate::transport::{Continuity, ContinuityTracker, PacketView, TS_PACKET_LEN};

/// Minimum consecutive scrambled packets required to conclusively confirm a stream as scrambled.
pub const SCRAMBLED_CONFIRMED_THRESHOLD: u64 = 100;

/// Number of bytes captured after an H.264 slice header NAL byte for slice type classification.
const SLICE_HEADER_CAPTURE_BYTES: usize = 12;

/// Number of bytes captured after an SEI NAL byte for recovery point detection.
const SEI_CAPTURE_BYTES: usize = 48;

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
pub fn remove_emulation_prevention(src: &[u8]) -> Vec<u8> {
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
pub struct BitReader<'a> {
    data: &'a [u8],
    pos: usize,
}

impl<'a> BitReader<'a> {
    /// Constructs a bit reader for the given RBSP slice.
    #[must_use]
    pub fn new(data: &'a [u8]) -> Self {
        Self { data, pos: 0 }
    }

    /// Number of bits remaining unread.
    #[must_use]
    pub fn bits_left(&self) -> usize {
        (self.data.len().saturating_mul(8)).saturating_sub(self.pos)
    }

    /// Reads a single bit from the stream.
    pub fn read_bit(&mut self) -> Option<u32> {
        if self.pos >= self.data.len().saturating_mul(8) {
            return None;
        }
        let byte_idx = self.pos >> 3;
        let bit_idx = 7 - (self.pos & 7);
        self.pos += 1;
        Some(u32::from((self.data[byte_idx] >> bit_idx) & 1))
    }

    /// Reads an unsigned Exp-Golomb coded integer.
    ///
    /// Rejects runs of leading zeros exceeding 32 bits to protect against unbounded loops
    /// on corrupted or non-slice bitstreams.
    pub fn read_ue(&mut self) -> Option<u32> {
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
        if self.bits_left() < leading_zeros {
            return None;
        }
        let mut suffix = 0u32;
        for _ in 0..leading_zeros {
            let bit = self.read_bit()?;
            suffix = (suffix << 1) | bit;
        }
        let val = (1u64 << leading_zeros)
            .saturating_sub(1)
            .saturating_add(u64::from(suffix));
        u32::try_from(val).ok()
    }
}

/// Reports whether an H.264 slice header names an intra-coded slice type.
///
/// Returns `(is_intra, ok)`. If the slice header cannot be completely parsed,
/// `ok` is `false`.
#[must_use]
pub fn h264_slice_is_intra(captured: &[u8]) -> (bool, bool) {
    let rbsp = remove_emulation_prevention(captured);
    let mut r = BitReader::new(&rbsp);
    if r.read_ue().is_none() {
        return (false, false);
    }
    let Some(slice_type) = r.read_ue() else {
        return (false, false);
    };
    match slice_type % 5 {
        2 | 4 => (true, true),
        _ => (false, true),
    }
}

/// Reports whether an SEI NAL payload contains a `recovery_point` message.
#[must_use]
pub fn sei_has_recovery_point(captured: &[u8]) -> bool {
    let rbsp = remove_emulation_prevention(captured);
    let mut pos = 0;
    while pos < rbsp.len() {
        if rbsp[pos] == 0x80 {
            return false;
        }
        let mut payload_type: usize = 0;
        while pos < rbsp.len() && rbsp[pos] == 0xFF {
            payload_type = payload_type.saturating_add(255);
            pos += 1;
        }
        if pos >= rbsp.len() {
            return false;
        }
        payload_type = payload_type.saturating_add(usize::from(rbsp[pos]));
        pos += 1;

        let mut payload_size: usize = 0;
        while pos < rbsp.len() && rbsp[pos] == 0xFF {
            payload_size = payload_size.saturating_add(255);
            pos += 1;
        }
        if pos >= rbsp.len() {
            return false;
        }
        payload_size = payload_size.saturating_add(usize::from(rbsp[pos]));
        pos += 1;

        if payload_type == SEI_PAYLOAD_RECOVERY_POINT {
            return true;
        }
        if payload_size > rbsp.len().saturating_sub(pos) {
            return false;
        }
        pos += payload_size;
    }
    false
}

/// Reports whether an MPEG-2 picture header specifies an I-frame (`picture_coding_type == 1`).
#[must_use]
pub fn mpeg2_picture_is_intra(data: &[u8]) -> (bool, bool) {
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
    Sei,
    Mpeg2PictureHeader,
}

/// Where in a PES packet the next video payload begins.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(super) enum VideoPosition {
    /// A continuation payload is elementary stream.
    InElementaryStream,

    /// An optional PES header has not finished, and this many of its bytes are still to come.
    InHeader { remaining: usize },

    /// Nothing may be fed until a valid PES packet starts.
    AwaitingStart,
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

    // NAL & Annex-B scanner state
    pub(super) annex_b_state: u32,
    pub(super) expecting_nal_byte: bool,
    pub(super) nal_kind: NalCaptureKind,
    pub(super) nal_left: usize,
    pub(super) nal_skip: usize,
    pub(super) nal_buf: Vec<u8>,

    // Parameter sets & observations
    pub(super) pes_has_sps: bool,
    pub(super) pes_has_pps: bool,
    pub(super) pes_has_vps: bool,
    pub(super) pes_has_recovery_point: bool,
    pub(super) unreadable_slices: u64,
}

impl VideoFollower {
    fn new(pid: u16, codec: VideoCodec) -> Self {
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
            annex_b_state: 0xFFFF_FFFF,
            expecting_nal_byte: false,
            nal_kind: NalCaptureKind::None,
            nal_left: 0,
            nal_skip: 0,
            nal_buf: Vec::new(),
            pes_has_sps: false,
            pes_has_pps: false,
            pes_has_vps: false,
            pes_has_recovery_point: false,
            unreadable_slices: 0,
        }
    }

    /// Begins a new PUSI boundary on every non-duplicate PUSI packet.
    ///
    /// Clears any in-flight PES offset, resets parameter sets and PES observations,
    /// and resets the Annex-B shift register. Must be called before TEI, scrambling,
    /// same-CC conflict, or PES header validation.
    fn begin_pusi_boundary(&mut self) {
        self.current_pes_offset = None;
        self.pes_has_sps = false;
        self.pes_has_pps = false;
        self.pes_has_vps = false;
        self.pes_has_recovery_point = false;
        self.reset_annex_b();
    }

    /// Resets the Annex-B shift register and clears any in-progress NAL capture.
    fn reset_annex_b(&mut self) {
        self.annex_b_state = 0xFFFF_FFFF;
        self.expecting_nal_byte = false;
        self.nal_kind = NalCaptureKind::None;
        self.nal_left = 0;
        self.nal_skip = 0;
        self.nal_buf.clear();
    }

    fn begin_capture(&mut self, kind: NalCaptureKind, budget: usize, skip: usize) {
        self.nal_kind = kind;
        self.nal_left = budget;
        self.nal_skip = skip;
        self.nal_buf.clear();
    }

    fn consume_capture(&mut self) {
        if self.nal_kind == NalCaptureKind::None || self.nal_buf.is_empty() {
            self.nal_kind = NalCaptureKind::None;
            self.nal_left = 0;
            self.nal_skip = 0;
            self.nal_buf.clear();
            return;
        }

        match self.nal_kind {
            NalCaptureKind::H264SliceHeader => {
                let (_is_intra, ok) = h264_slice_is_intra(&self.nal_buf);
                if !ok {
                    self.unreadable_slices = self.unreadable_slices.saturating_add(1);
                }
            }
            NalCaptureKind::Sei => {
                if sei_has_recovery_point(&self.nal_buf) {
                    self.pes_has_recovery_point = true;
                }
            }
            NalCaptureKind::Mpeg2PictureHeader => {
                let (_is_intra, ok) = mpeg2_picture_is_intra(&self.nal_buf);
                if !ok {
                    self.unreadable_slices = self.unreadable_slices.saturating_add(1);
                }
            }
            NalCaptureKind::None => {}
        }

        self.nal_kind = NalCaptureKind::None;
        self.nal_left = 0;
        self.nal_skip = 0;
        self.nal_buf.clear();
    }

    /// Feeds elementary stream bytes into the stateful Annex-B scanner.
    fn feed_es(&mut self, es: &[u8]) {
        for &b in es {
            self.annex_b_state = (self.annex_b_state << 8) | u32::from(b);

            if self.nal_left > 0 {
                if self.nal_skip > 0 {
                    self.nal_skip -= 1;
                } else {
                    self.nal_buf.push(b);
                    self.nal_left -= 1;
                    if self.nal_left == 0 {
                        self.consume_capture();
                    }
                }
            }

            if self.expecting_nal_byte {
                self.expecting_nal_byte = false;
                self.consume_capture();

                match self.codec {
                    VideoCodec::H264 => self.classify_h264(b),
                    VideoCodec::H265 => self.classify_hevc(b),
                    VideoCodec::Mpeg2 => self.classify_mpeg2(b),
                    VideoCodec::Unknown => {}
                }
            }

            if (self.annex_b_state & 0x00FF_FFFF) == 0x0000_0001 {
                self.expecting_nal_byte = true;
            }
        }
    }

    fn classify_h264(&mut self, b: u8) {
        match b & 0x1F {
            H264_NAL_SLICE_NON_IDR | H264_NAL_SLICE_PART_A => {
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
                self.begin_capture(NalCaptureKind::Sei, SEI_CAPTURE_BYTES, 0);
            }
            // IDR slice and other NAL types do not initiate parameter captures.
            _ => {}
        }
    }

    fn classify_hevc(&mut self, b: u8) {
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
                self.begin_capture(NalCaptureKind::Sei, SEI_CAPTURE_BYTES, 1);
            }
            // IRAP and non-IRAP VCL slices do not initiate parameter captures.
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
}

/// Follows the observable video stream of one transport programme.
#[derive(Debug)]
pub struct VideoIngress {
    psi: PsiCore,
    follower: Option<VideoFollower>,
    incarnation: u64,
}

impl VideoIngress {
    /// An ingress following one programme, or whichever the PAT offers first.
    #[must_use]
    pub fn new(target_program_number: u16) -> Self {
        Self {
            psi: PsiCore::new(target_program_number),
            follower: None,
            incarnation: 0,
        }
    }

    /// Selects the programme to follow.
    pub fn set_target_program(&mut self, program_number: u16) {
        let outcome = self.psi.set_target_program(program_number);
        if outcome.events.contains(&PsiEvent::ProgramIdentityChanged) {
            self.reprogram();
        }
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
        self.psi.begin_chunk();
        for (packet_idx, packet) in data.chunks_exact(TS_PACKET_LEN).enumerate() {
            let rel_offset =
                i64::try_from(packet_idx.saturating_mul(TS_PACKET_LEN)).unwrap_or(i64::MAX);
            let packet_offset = start_offset.saturating_add(rel_offset);
            let changed = self
                .psi
                .index_packet(packet)
                .contains(&PsiEvent::ProgramIdentityChanged);
            if changed {
                self.reprogram();
            }
            let Ok(view) = PacketView::parse(packet) else {
                continue;
            };
            self.route(packet_offset, &view, &mut feeds);
        }
        let consumed = i64::try_from(data.len()).unwrap_or(i64::MAX);
        Ok(VideoOutcome {
            processed_through: start_offset.saturating_add(consumed),
            feeds,
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
    }

    /// Routes one packet to the video follower.
    fn route<'a>(
        &mut self,
        packet_offset: i64,
        view: &PacketView<'a>,
        out: &mut Vec<VideoFeed<'a>>,
    ) {
        let pid = view.pid();
        if pid == 0 || pid == self.psi.pmt_pid() {
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

        let same_cc_conflict =
            continuity == Continuity::Broken && is_same_cc && !view.discontinuity_indicator();

        let is_pusi = view.payload_unit_start();

        // Boundary invariant: every non-duplicate PUSI packet unconditionally marks
        // a new PES/NAL boundary before TEI, scrambling, or validation.
        if is_pusi {
            follower.begin_pusi_boundary();
        }

        if view.transport_error_indicator() {
            follower.position = VideoPosition::AwaitingStart;
            if !is_pusi {
                follower.reset_annex_b();
            }
            return;
        }

        if view.scrambling_control() != 0 {
            follower.scrambled_packets += 1;
            follower.clear_run = 0;
            if is_pusi || matches!(follower.position, VideoPosition::InHeader { .. }) {
                follower.position = VideoPosition::AwaitingStart;
            }
            return;
        }

        follower.clear_packets += 1;
        follower.clear_run += 1;

        let es = if is_pusi {
            follower.handle_pusi(packet_offset, payload, same_cc_conflict)
        } else {
            follower.handle_cont(payload, continuity, same_cc_conflict)
        };

        if let Some(es) = es {
            follower.feed_es(es);
            out.push(VideoFeed {
                incarnation: self.incarnation,
                pid: follower.pid,
                offset: packet_offset,
                pusi: is_pusi,
                es,
            });
        }
    }
}

impl VideoFollower {
    fn handle_pusi<'a>(
        &mut self,
        packet_offset: i64,
        payload: &'a [u8],
        same_cc_conflict: bool,
    ) -> Option<&'a [u8]> {
        if same_cc_conflict {
            self.position = VideoPosition::AwaitingStart;
            return None;
        }

        match pes::read_start(payload) {
            PesStart::Complete { stream_id, es, .. } if pes::is_video_stream_id(stream_id) => {
                self.current_pes_offset = Some(packet_offset);
                self.position = VideoPosition::InElementaryStream;
                self.pes_starts += 1;
                if es.is_empty() { None } else { Some(es) }
            }
            PesStart::HeaderIncomplete {
                stream_id,
                remaining_header,
                ..
            } if pes::is_video_stream_id(stream_id) => {
                self.current_pes_offset = Some(packet_offset);
                self.position = VideoPosition::InHeader {
                    remaining: remaining_header,
                };
                self.pes_starts += 1;
                None
            }
            _ => {
                self.position = VideoPosition::AwaitingStart;
                None
            }
        }
    }

    fn handle_cont<'a>(
        &mut self,
        payload: &'a [u8],
        continuity: Continuity,
        same_cc_conflict: bool,
    ) -> Option<&'a [u8]> {
        match self.position {
            VideoPosition::AwaitingStart => None,
            VideoPosition::InHeader { ref mut remaining } => {
                if continuity == Continuity::Broken
                    || continuity == Continuity::Discontinuous
                    || same_cc_conflict
                {
                    self.position = VideoPosition::AwaitingStart;
                    self.reset_annex_b();
                    return None;
                }
                if payload.len() < *remaining {
                    *remaining -= payload.len();
                    None
                } else {
                    let rem = *remaining;
                    self.position = VideoPosition::InElementaryStream;
                    let es = &payload[rem..];
                    if es.is_empty() { None } else { Some(es) }
                }
            }
            VideoPosition::InElementaryStream => {
                if continuity == Continuity::Broken
                    || continuity == Continuity::Discontinuous
                    || same_cc_conflict
                {
                    self.position = VideoPosition::AwaitingStart;
                    self.reset_annex_b();
                    None
                } else if payload.is_empty() {
                    None
                } else {
                    Some(payload)
                }
            }
        }
    }
}
