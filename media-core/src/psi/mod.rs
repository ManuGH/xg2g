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

pub use crate::transport::{PacketView, SYNC_BYTE, TS_PACKET_LEN};

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

/// The accepted sections of the tables in force.
///
/// Each entry is one complete section exactly as accepted: `table_id` through
/// CRC, byte for byte. The core parses these and hands the bytes back anyway,
/// because a subscriber joining mid-stream is given the tables ahead of the
/// stream, and re-deriving them would mean encoding PSI in a second place with
/// a second answer.
///
/// What is deliberately not here is the transport packets the sections arrived
/// in. The same table can be sent in six packets or in a thousand one-byte
/// ones; both mean the same table, so both must leave the same state behind.
/// Retaining the packets made the memory held a function of the sender's
/// fragmentation rather than of the table's content, and the bound follows from
/// the syntax instead:
///
/// - a section is at most 1024 bytes and a table at most 256 sections
/// - so each list is at most 256 KiB, and an `ActivePsi` at most 512 KiB
///
/// Sections are in the order the table numbers them, `section_number`
/// ascending, which is also the order they must be delivered in.
#[derive(Debug, Default, Clone, PartialEq, Eq)]
pub struct ActivePsi {
    /// The accepted sections of the PAT in force.
    pub pat_sections: Vec<Vec<u8>>,
    /// The accepted sections of the PMT in force.
    pub pmt_sections: Vec<Vec<u8>>,
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
#[derive(Debug, Default)]
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

    /// The accepted sections of the PAT in force, copied out of the tracker so
    /// a later in-flight generation cannot reach them.
    active_pat_sections: Vec<Vec<u8>>,
    /// The accepted sections of the PMT in force.
    active_pmt_sections: Vec<Vec<u8>>,

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
        self.begin_chunk();
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
            self.active_pat_sections.clear();
            self.reset_program_state();
        }
        self.outcome(0)
    }

    /// What the two section assemblers are holding. See
    /// [`SectionAssembler::retained`].
    #[cfg(test)]
    pub(super) fn retained(&self) -> [(usize, usize); 2] {
        [self.pat_assembler.retained(), self.pmt_assembler.retained()]
    }

    /// Every byte this core is holding on account of PSI.
    ///
    /// The corpus cannot see this. It compares facts and the tables in force,
    /// and a core that also kept a copy of every packet it had ever been given
    /// would agree with it on all of them - which is what this parser used to
    /// do. So the memory is stated as a number and held to a bound.
    #[cfg(test)]
    pub(super) fn retained_bytes(&self) -> usize {
        let assemblers = self.pat_assembler.retained_bytes() + self.pmt_assembler.retained_bytes();
        let trackers = self.pat_tracker.retained() + self.pmt_tracker.retained();
        let active: usize = self
            .active_pat_sections
            .iter()
            .chain(&self.active_pmt_sections)
            .map(Vec::len)
            .sum();
        assemblers + trackers + active
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
        // What the two tables together may cost, checked where they are handed
        // out. Not a policy: a section is at most 1024 bytes and a table at most
        // 256 sections, so this follows.
        debug_assert!(
            self.active_pat_sections
                .iter()
                .chain(&self.active_pmt_sections)
                .map(Vec::len)
                .sum::<usize>()
                <= table::MAX_ACTIVE_PSI_BYTES
        );
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
                pat_sections: self.active_pat_sections.clone(),
                pmt_sections: self.active_pmt_sections.clone(),
            },
        }
    }

    /// Begins a chunk, forgetting the events of the one before it.
    ///
    /// [`PsiCore::ingest`] does this itself. It is public for the caller that
    /// drives [`PsiCore::index_packet`] directly and has to mark where one
    /// call's events end.
    pub fn begin_chunk(&mut self) {
        self.events.clear();
    }

    /// Routes one packet to the table it belongs to, and reports what it meant.
    ///
    /// The step [`PsiCore::ingest`] takes for each packet of a chunk, offered
    /// on its own because a stage that follows the same transport has to act on
    /// a programme change at the packet it happened on rather than at the end of
    /// the chunk. What it returns is this packet's events alone: the ones
    /// earlier packets of the chunk produced stay in the chunk's outcome and
    /// out of this answer, so a caller cannot mistake a change it has already
    /// acted on for one that has just happened.
    pub fn index_packet(&mut self, packet: &[u8]) -> &[PsiEvent] {
        let before = self.events.len();
        // What a packet is gets decided in one place. A packet this layer cannot
        // read - no sync byte, the reserved adaptation control, a field reaching
        // past the end - is skipped rather than guessed at, which is what this
        // code did when it read the header itself.
        if let Ok(view) = PacketView::parse(packet) {
            // No payload means nothing for a table to be assembled from, whether
            // the packet carries an adaptation field only or one that swallowed
            // the payload it promised.
            if let Some(payload) = view.payload() {
                let pid = view.pid();
                if pid == PAT_PID {
                    self.feed_table(true, &view, payload);
                } else if self.pmt_pid() > 0 && pid == self.pmt_pid() {
                    self.feed_table(false, &view, payload);
                }
                // Everything else is an elementary stream. This step reads
                // tables only.
            }
        }
        &self.events[before..]
    }

    /// The audio tracks the table in force declares, in the order it lists them.
    ///
    /// Borrowed rather than cloned: a caller asking this per packet would
    /// otherwise pay for a copy of every track on every packet of the stream.
    #[must_use]
    pub fn audio_tracks(&self) -> &[AudioTrack] {
        &self.streams.audio_tracks
    }

    /// Assembles one packet's payload and interprets whatever it completed.
    fn feed_table(&mut self, is_pat: bool, view: &PacketView<'_>, payload: &[u8]) {
        let expected = if is_pat { TABLE_ID_PAT } else { TABLE_ID_PMT };
        let packet = view.bytes();
        let cc = view.continuity_counter();
        let pusi = view.payload_unit_start();
        let completed = if is_pat {
            self.pat_assembler
                .accept(packet, cc, pusi, payload, expected)
        } else {
            self.pmt_assembler
                .accept(packet, cc, pusi, payload, expected)
        };
        for section in completed {
            self.accept_section(is_pat, &section.bytes);
        }
    }

    /// Decides whether a completed section describes this stream, and reads it
    /// if it does.
    fn accept_section(&mut self, is_pat: bool, section: &[u8]) {
        let expected = if is_pat { TABLE_ID_PAT } else { TABLE_ID_PMT };
        let Some(header) = SectionHeader::read(section, expected) else {
            return;
        };
        if is_pat {
            self.accept_pat_section(&header, section);
        } else {
            self.accept_pmt_section(&header, section);
        }
    }

    /// Collects a PAT section and, once the table is whole, chooses a programme.
    fn accept_pat_section(&mut self, header: &SectionHeader, section: &[u8]) {
        if !self.pat_tracker.add(
            header.version,
            header.section_number,
            header.last_section_number,
            section,
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
        self.active_pat_sections = self.pat_tracker.owned_sections();
    }

    /// Collects a PMT section for the selected programme, and reads the table
    /// once it is whole.
    fn accept_pmt_section(&mut self, header: &SectionHeader, section: &[u8]) {
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
        self.active_pmt_sections = self.pmt_tracker.owned_sections();
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
        self.active_pat_sections.clear();
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
        self.active_pmt_sections.clear();
        self.events.push(PsiEvent::ProgramIdentityChanged);
    }
}
