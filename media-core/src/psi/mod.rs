// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! What a transport stream's own tables say the programme is.
//!
//! This is the Rust side of a differential migration. The Go implementation in
//! `backend/internal/stream/ingest/mediafacts` is the reference for as long as
//! this step lasts, and the two are held together by a corpus checked in at
//! `testdata/psi-corpus/corpus.txt`: a sequence of calls and the facts, events
//! and raw tables expected after each one. Both implementations are checked
//! against that file, so agreement is a property of a reviewable artefact rather
//! than of anybody's memory of what the other one does.
//!
//! Nothing here is authoritative. This is a library that answers the corpus; it
//! is not wired into the core's socket, and Go remains the parser production
//! reads.
//!
//! The transport this file understands is only what PSI needs: packet framing,
//! the PID, the payload unit start flag, the continuity counter, the adaptation
//! field's effect on where the payload begins, and the pointer field. There is
//! no PES here, no elementary stream payload, no timing.

mod assembler;
mod crc;
mod descriptors;
mod pat;
mod pmt;
mod table;

#[cfg(test)]
mod adversarial_test;
#[cfg(test)]
mod corpus_test;
#[cfg(test)]
mod perf_test;

use assembler::SectionAssembler;
use table::{SectionHeader, TABLE_ID_PAT, TABLE_ID_PMT, TableTracker};

pub use descriptors::ChannelDeclaration;
pub use pmt::{AudioTrack, VideoCodec};

/// The size of a transport stream packet.
pub const TS_PACKET_LEN: usize = 188;

/// The byte every transport stream packet starts with.
pub const SYNC_BYTE: u8 = 0x47;

/// The PID the programme association table is carried on.
const PAT_PID: u16 = 0x0000;

/// Why a chunk was refused.
///
/// A refusal means nothing was interpreted. There is no partial success: a core
/// that consumed less than it was given has moved past bytes the caller is about
/// to throw away, and saying so afterwards is too late to be useful.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum IngestError {
    /// The chunk was not a whole number of transport stream packets.
    UnalignedChunk {
        /// How many bytes were offered.
        len: usize,
    },
}

impl core::fmt::Display for IngestError {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self {
            Self::UnalignedChunk { len } => {
                write!(
                    f,
                    "chunk of {len} bytes is not {TS_PACKET_LEN}-byte packet aligned"
                )
            }
        }
    }
}

impl std::error::Error for IngestError {}

/// Something the core saw that a caller would have to act on.
///
/// Facts describe a state; events describe a moment, and their order within a
/// call is the order they happened in.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PsiEvent {
    /// The programme this stream carries is no longer the one it was: a new PMT
    /// version, a different programme, a changed stream layout.
    ///
    /// It is not a generation. What a caller does with it is the caller's
    /// decision, and this event carries no opinion about it.
    ProgramIdentityChanged,
}

/// What the core knows about the stream right now.
#[derive(Debug, Default, Clone, PartialEq, Eq)]
pub struct PsiFacts {
    /// A PAT naming the programme being followed has been accepted.
    pub has_pat: bool,
    /// A PMT for that programme has been accepted.
    pub has_pmt: bool,
    /// The version of the PMT in force.
    pub pmt_version: u8,
    /// The programme number that PMT declared.
    pub program_number: u16,
    /// The PID the PAT named for the programme, or 0 while none has been.
    pub pmt_pid: u16,
    /// The video stream's PID, or 0 while no table has named one.
    pub video_pid: u16,
    /// Its codec.
    pub video_codec: VideoCodec,
    /// The audio PIDs, in the order the table lists them.
    pub audio_pids: Vec<u16>,
    /// The audio tracks, in the same order.
    pub audio_tracks: Vec<AudioTrack>,
}

/// The raw packets of the tables in force.
///
/// The core parses these and hands the bytes back anyway, because a subscriber
/// joining mid-stream is given the tables as they were broadcast. Rebuilding
/// them would mean encoding PSI in a second place, with a second answer.
#[derive(Debug, Default, Clone, PartialEq, Eq)]
pub struct ActivePsi {
    /// The packets of the PAT in force.
    pub pat: Vec<Vec<u8>>,
    /// The packets of the PMT in force.
    pub pmt: Vec<Vec<u8>>,
}

/// What one call meant.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Outcome {
    /// The offset one past the last byte interpreted.
    pub processed_through: i64,
    /// What happened during the call, in order.
    pub events: Vec<PsiEvent>,
    /// The state afterwards.
    pub facts: PsiFacts,
    /// The raw tables afterwards.
    pub active: ActivePsi,
}

/// Reads the programme association and programme map tables of one transport.
///
/// Not safe for concurrent use, and deliberately not internally synchronised:
/// the caller already serialises access to the bytes it hands in.
#[derive(Default)]
pub struct PsiCore {
    /// The programme to follow, or 0 for whichever the PAT offers first.
    target_program_number: u16,

    /// Section assembly for PID 0.
    pat_assembler: SectionAssembler,
    /// Section assembly for the selected PMT PID.
    pmt_assembler: SectionAssembler,
    /// Sections of the PAT generation being collected.
    pat_tracker: TableTracker,
    /// Sections of the PMT generation being collected.
    pmt_tracker: TableTracker,

    /// The programme and PID the PAT chose, held as one.
    selection: Option<pat::Selection>,

    /// A PAT naming the programme has been accepted.
    has_pat_version: bool,
    /// Its version.
    pat_version: u8,
    /// A PMT for the programme has been accepted.
    has_pmt_version: bool,
    /// Its version.
    pmt_version: u8,
    /// The programme number it declared.
    pmt_program_number: u16,

    /// The raw packets of the PAT in force.
    raw_pat: Vec<Vec<u8>>,
    /// The raw packets of the PMT in force.
    raw_pmt: Vec<Vec<u8>>,

    /// What the PMT in force said the programme is made of.
    streams: pmt::Streams,

    /// What the call in progress has meant so far.
    events: Vec<PsiEvent>,
}

impl PsiCore {
    /// A core with no stream state, following `target_program_number`.
    ///
    /// A target of 0 means "whichever programme the PAT offers first".
    #[must_use]
    pub fn new(target_program_number: u16) -> Self {
        Self {
            target_program_number,
            ..Self::default()
        }
    }

    /// Interprets one chunk of transport stream.
    ///
    /// `start_offset` is the chunk's position in the caller's own byte
    /// coordinate system, and the offset reported back is in that same system.
    ///
    /// # Errors
    ///
    /// Returns [`IngestError::UnalignedChunk`] when the chunk is not a whole
    /// number of packets. Nothing is interpreted in that case.
    pub fn ingest(&mut self, start_offset: i64, data: &[u8]) -> Result<Outcome, IngestError> {
        if !data.len().is_multiple_of(TS_PACKET_LEN) {
            return Err(IngestError::UnalignedChunk { len: data.len() });
        }
        self.events.clear();
        for packet in data.chunks_exact(TS_PACKET_LEN) {
            self.index_packet(packet);
        }
        let consumed = i64::try_from(data.len()).unwrap_or(i64::MAX);
        Ok(self.outcome(start_offset.saturating_add(consumed)))
    }

    /// Selects the programme to follow.
    ///
    /// Changing it discards everything read about the previous one. Selecting
    /// the programme already being followed is not a change and costs nothing.
    ///
    /// It interprets no bytes, so it reports no position in the stream.
    pub fn set_target_program(&mut self, program_number: u16) -> Outcome {
        self.events.clear();
        if self.target_program_number != program_number {
            self.target_program_number = program_number;
            self.has_pat_version = false;
            self.pat_version = 0;
            self.forget_pmt_identity();
            self.selection = None;
            self.pat_assembler.reset();
            self.pmt_assembler.reset();
            self.pat_tracker.reset();
            self.pmt_tracker.reset();
            self.raw_pat.clear();
            self.reset_program_state();
        }
        self.outcome(0)
    }

    /// What the two section assemblers are holding. See
    /// [`SectionAssembler::retained`].
    #[cfg(test)]
    pub(super) fn retained(&self) -> [(usize, usize, usize); 2] {
        [self.pat_assembler.retained(), self.pmt_assembler.retained()]
    }

    /// The PID the PAT named, or 0 while none has been.
    fn pmt_pid(&self) -> u16 {
        self.selection.map_or(0, |s| s.pmt_pid)
    }

    /// The programme number the PAT chose, or 0 while none has been.
    fn selected_program_number(&self) -> u16 {
        self.selection.map_or(0, |s| s.program_number)
    }

    /// Builds the answer for the call that is ending.
    fn outcome(&self, processed_through: i64) -> Outcome {
        Outcome {
            processed_through,
            events: self.events.clone(),
            facts: PsiFacts {
                has_pat: self.has_pat_version,
                has_pmt: self.has_pmt_version,
                pmt_version: self.pmt_version,
                program_number: self.pmt_program_number,
                pmt_pid: self.pmt_pid(),
                video_pid: self.streams.video_pid,
                video_codec: self.streams.video_codec,
                audio_pids: self.streams.audio_pids.clone(),
                audio_tracks: self.streams.audio_tracks.clone(),
            },
            active: ActivePsi {
                pat: self.raw_pat.clone(),
                pmt: self.raw_pmt.clone(),
            },
        }
    }

    /// Routes one packet to the table it belongs to, if any.
    fn index_packet(&mut self, packet: &[u8]) {
        if packet[0] != SYNC_BYTE {
            return;
        }
        let pid = (u16::from(packet[1] & 0x1F) << 8) | u16::from(packet[2]);
        let pusi = packet[1] & 0x40 != 0;
        let adaptation = (packet[3] >> 4) & 0x03;
        // 0x01 is payload only, 0x03 is an adaptation field ahead of a payload.
        // The other two carry no payload at all.
        if adaptation != 0x01 && adaptation != 0x03 {
            return;
        }
        let mut payload_at = 4;
        if adaptation == 0x03 {
            payload_at = 5 + usize::from(packet[4]);
            if payload_at >= TS_PACKET_LEN {
                // The adaptation field claims the rest of the packet, so there
                // is no payload behind it whatever the flag said.
                return;
            }
        }
        let payload = &packet[payload_at..];

        if pid == PAT_PID {
            self.feed_table(true, packet, pusi, payload);
        } else if self.pmt_pid() > 0 && pid == self.pmt_pid() {
            self.feed_table(false, packet, pusi, payload);
        }
        // Everything else is an elementary stream. This step reads tables only.
    }

    /// Assembles one packet's payload and interprets whatever it completed.
    fn feed_table(&mut self, is_pat: bool, packet: &[u8], pusi: bool, payload: &[u8]) {
        let expected = if is_pat { TABLE_ID_PAT } else { TABLE_ID_PMT };
        let completed = if is_pat {
            self.pat_assembler.accept(packet, pusi, payload, expected)
        } else {
            self.pmt_assembler.accept(packet, pusi, payload, expected)
        };
        for section in completed {
            self.accept_section(is_pat, &section.bytes, &section.packets);
        }
    }

    /// Decides whether a completed section describes this stream, and reads it
    /// if it does.
    fn accept_section(&mut self, is_pat: bool, section: &[u8], packets: &[Vec<u8>]) {
        let expected = if is_pat { TABLE_ID_PAT } else { TABLE_ID_PMT };
        let Some(header) = SectionHeader::read(section, expected) else {
            return;
        };
        if is_pat {
            self.accept_pat_section(&header, section, packets);
        } else {
            self.accept_pmt_section(&header, section, packets);
        }
    }

    /// Collects a PAT section and, once the table is whole, chooses a programme.
    fn accept_pat_section(&mut self, header: &SectionHeader, section: &[u8], packets: &[Vec<u8>]) {
        if !self.pat_tracker.add(
            header.version,
            header.section_number,
            header.last_section_number,
            section,
            packets,
        ) {
            return;
        }

        let chosen = pat::select(&self.pat_tracker.sections(), self.target_program_number);
        let Some(chosen) = chosen else {
            // A complete PAT in force that does not name the programme is the
            // transport saying it is not here. Keeping the PID it used to be on
            // would leave this core reading a table for a programme this PAT
            // does not carry.
            if self.selection.is_some() {
                self.drop_program_selection();
            }
            return;
        };

        // The selection changes when either half of it does. A PID that stays
        // the same while the programme on it changes - which only an unnamed
        // target can do - is still a different selection.
        if self.selection != Some(chosen) {
            self.selection = Some(chosen);
            self.pmt_assembler.reset();
            self.pmt_tracker.reset();
            self.forget_pmt_identity();
            self.reset_program_state();
        }
        self.has_pat_version = true;
        self.pat_version = header.version;
        self.raw_pat = self.pat_tracker.packets();
    }

    /// Collects a PMT section for the selected programme, and reads the table
    /// once it is whole.
    fn accept_pmt_section(&mut self, header: &SectionHeader, section: &[u8], packets: &[Vec<u8>]) {
        // The PAT chose a programme and the PID its table is on together, so a
        // section on that PID naming a different programme is not this
        // programme's table.
        //
        // Refused here, before the tracker, and not once it completes: a new
        // generation discards the sections already collected, so a table that
        // was never going to be accepted would destroy one that was being
        // assembled correctly.
        if header.id_extension != self.selected_program_number() {
            return;
        }
        if !self.pmt_tracker.add(
            header.version,
            header.section_number,
            header.last_section_number,
            section,
            packets,
        ) {
            return;
        }

        let changed = !self.has_pmt_version
            || header.version != self.pmt_version
            || header.id_extension != self.pmt_program_number;
        if changed {
            self.has_pmt_version = true;
            self.pmt_version = header.version;
            self.pmt_program_number = header.id_extension;
            self.reset_program_state();
            self.streams = pmt::interpret(&self.pmt_tracker.sections());
        }
        self.raw_pmt = self.pmt_tracker.packets();
    }

    /// Gives up everything the current PMT said about which programme this is.
    ///
    /// Having a PMT, the programme number and the table version are one fact -
    /// what the table in force says - so they are given up together. Leaving the
    /// number and the version behind a false `has_pmt` would describe a
    /// programme that has already been discarded.
    fn forget_pmt_identity(&mut self) {
        self.has_pmt_version = false;
        self.pmt_version = 0;
        self.pmt_program_number = 0;
    }

    /// Gives up the PAT's choice of programme and PID, and everything downstream.
    ///
    /// `has_pat` goes with it. It never meant "a PAT arrived"; it is only set
    /// where a PAT named the programme being followed, so leaving it standing
    /// would be the same half-fact as a programme number outliving its
    /// programme. The raw PAT goes for the same reason.
    fn drop_program_selection(&mut self) {
        self.selection = None;
        self.has_pat_version = false;
        self.pat_version = 0;
        self.raw_pat.clear();
        self.pmt_assembler.reset();
        self.pmt_tracker.reset();
        self.forget_pmt_identity();
        self.reset_program_state();
    }

    /// Drops everything read about the streams of a programme, and says so.
    ///
    /// What the caller does with the event is the caller's decision; this layer
    /// reports that the programme's identity changed and nothing more.
    fn reset_program_state(&mut self) {
        self.streams = pmt::Streams::default();
        self.raw_pmt.clear();
        self.events.push(PsiEvent::ProgramIdentityChanged);
    }
}
