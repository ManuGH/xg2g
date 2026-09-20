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
//! continuity and scrambling facts, validating PES headers, and routing valid
//! elementary stream bytes to downstream consumers (Annex-B / NAL parsing in
//! later steps).
//!
//! # The reference and the authored PES state machine
//!
//! The Go core in `backend/internal/stream/ingest/mediafacts` is the reference
//! for transport and scrambling facts (`vclr`, `vscr`, `vrun`, `scrconf`).
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

use crate::pes::{self, PesStart};
use crate::psi::{IngestError, PsiCore, PsiEvent, VideoCodec};
use crate::transport::{Continuity, ContinuityTracker, PacketView, TS_PACKET_LEN};

/// Where in a PES packet the next video payload begins.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(super) enum VideoPosition {
    /// A continuation payload is elementary stream.
    ///
    /// This is the initial state of a followed video stream. A capture that
    /// starts in the middle of a PES packet carries video from its first packet,
    /// and mid-stream recording ingress begins immediately.
    InElementaryStream,

    /// An optional PES header has not finished, and this many of its bytes are
    /// still to come.
    InHeader {
        /// How many header bytes the following payloads still carry.
        remaining: usize,
    },

    /// Nothing may be fed until a valid PES packet starts.
    ///
    /// Entered when:
    /// - A payload unit start has an invalid startcode prefix, non-video stream ID,
    ///   or truncated header.
    /// - Transport error indicator (TEI) is asserted on a PUSI or continuation packet.
    /// - A PUSI arrives encrypted (scrambled).
    /// - A packet arrives encrypted while `InHeader`.
    /// - A transport discontinuity, CC gap, or same-CC conflict breaks payload continuity.
    AwaitingStart,
}

/// One observable video elementary stream, followed across the packets that
/// carry it.
#[derive(Debug)]
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
        }
    }
}

/// One run of video elementary stream bytes, extracted from a transport packet.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct VideoFeed<'a> {
    /// Which incarnation of the stream this belonged to. The same PID before
    /// and after a programme identity change is two streams, not one.
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
    /// Whether the video elementary stream is confirmed scrambled (`clear == 0 && scrambled >= 100`).
    pub scrambled_confirmed: bool,
    /// Whether the video follower is currently quarantining payloads until a valid PES start.
    pub awaiting_start: bool,
    /// The caller's byte offset of the packet that began the most recent valid video PES packet,
    /// or `None` if no valid PES packet has begun or if the last PUSI was invalidated.
    pub current_pes_offset: Option<i64>,
    /// Number of valid video PES packet starts observed.
    pub pes_starts: u64,
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
    ///
    /// Changing it ends any stream being followed, because what the PIDs carried
    /// belonged to the programme that was selected.
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
                scrambled_confirmed: f.clear_packets == 0 && f.scrambled_packets >= 100,
                awaiting_start: matches!(f.position, VideoPosition::AwaitingStart),
                current_pes_offset: f.current_pes_offset,
                pes_starts: f.pes_starts,
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
            }
        }
    }

    /// Interprets one chunk of transport.
    ///
    /// `start_offset` is the caller's monotonic coordinate for the first byte of `data`.
    /// Offsets emitted in [`VideoFeed`] and [`VideoFacts`] are caller-owned coordinates
    /// strictly derived from `start_offset + packet_index * TS_PACKET_LEN`.
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

        if view.transport_error_indicator() {
            follower.position = VideoPosition::AwaitingStart;
            return;
        }

        if view.scrambling_control() != 0 {
            follower.scrambled_packets += 1;
            follower.clear_run = 0;
            if view.payload_unit_start()
                || matches!(follower.position, VideoPosition::InHeader { .. })
            {
                follower.position = VideoPosition::AwaitingStart;
            }
            return;
        }

        follower.clear_packets += 1;
        follower.clear_run += 1;

        let es = if view.payload_unit_start() {
            follower.handle_pusi(packet_offset, payload, same_cc_conflict)
        } else {
            follower.handle_cont(payload, continuity, same_cc_conflict)
        };

        if let Some(es) = es {
            out.push(VideoFeed {
                incarnation: self.incarnation,
                pid: follower.pid,
                offset: packet_offset,
                pusi: view.payload_unit_start(),
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
        self.current_pes_offset = None;

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
