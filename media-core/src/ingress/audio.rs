// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! Audio stream tracking and elementary stream routing for all PMT-declared audio PIDs.
//!
//! # Transport Counting vs. Observer Layering
//!
//! Audio transport state (`scrambled_packets`, `clear_packets`, `clear_run`) and continuity
//! tracking are maintained across *all* PMT-declared audio PIDs (MP2, AAC, AC-3, E-AC-3, DTS).
//!
//! Counting occurs strictly at transport level, before observer acceptance:
//! - Exact duplicate (`Continuity::Duplicate`): dropped, no counters incremented.
//! - TEI packet: dropped without incrementing clear or scrambled counters, and without altering `clear_run`.
//! - Adaptation-only packet: does not count, but latches discontinuity indicator (DI) for the stream's next payload packet.
//! - Scrambled packet: increments `scrambled_packets`, resets `clear_run = 0`, and resets PES state to `AwaitingStart` if PUSI or in header.
//! - Clear packet: increments `clear_packets`, increments `clear_run`. A same-CC-conflict packet is physically clear (increments clear counters), even though its payload is suppressed from reaching the observer due to transport conflict.
//!
//! Elementary stream parsing and `Observer` attachment are instantiated exclusively for observable codecs (`ac3`, `eac3`).

use crate::audio::observer::{Observation, Observer};
use crate::pes::{self, PesStart};
use crate::psi::AudioTrack;
use crate::transport::{Continuity, ContinuityTracker, PacketView};

/// Whether a codec is parsed for its channel layout.
#[must_use]
pub fn observable(codec: &str) -> bool {
    codec == "ac3" || codec == "eac3"
}

/// Where in a PES packet the next continuation payload begins.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Position {
    /// Normal state: continuation payload is elementary stream.
    InElementaryStream,
    /// An optional PES header has not finished, with `remaining` bytes left.
    InHeader {
        /// How many header bytes remain across following payloads.
        remaining: usize,
    },
    /// Quarantined until a valid PES start code arrives.
    AwaitingStart,
}

/// Elementary stream observer and PES boundary parser for observable audio streams.
#[derive(Debug)]
pub struct AudioElementaryState {
    /// The frame observer reading channel layout and sync words.
    pub observer: Observer,
    /// Current header/stream parsing position.
    pub position: Position,
    /// Payload units that successfully started an audio PES packet.
    pub pes_starts: u64,
    /// PES packets whose header declared a length exceeding the starting packet.
    pub header_incomplete: u64,
    /// Total elementary stream feeds delivered.
    pub feeds: u64,
}

impl Default for AudioElementaryState {
    fn default() -> Self {
        Self::new()
    }
}

impl AudioElementaryState {
    /// Creates a new elementary stream state machine.
    #[must_use]
    pub fn new() -> Self {
        Self {
            observer: Observer::new(),
            position: Position::InElementaryStream,
            pes_starts: 0,
            header_incomplete: 0,
            feeds: 0,
        }
    }

    /// Reads a payload that begins a payload unit.
    pub fn start<'a>(&mut self, payload: &'a [u8]) -> Option<&'a [u8]> {
        match pes::read_start(payload) {
            PesStart::Complete {
                stream_id,
                es,
                header_data_length: _,
                packet_length: _,
            } if pes::is_audio_stream_id(stream_id) => {
                self.pes_starts += 1;
                self.position = Position::InElementaryStream;
                Some(es)
            }
            PesStart::HeaderIncomplete {
                stream_id,
                remaining_header,
                header_data_length: _,
                packet_length: _,
            } if pes::is_audio_stream_id(stream_id) => {
                self.pes_starts += 1;
                self.header_incomplete += 1;
                self.position = Position::InHeader {
                    remaining: remaining_header,
                };
                None
            }
            _ => {
                self.position = Position::AwaitingStart;
                None
            }
        }
    }

    /// Reads a payload that continues a payload unit already under way.
    pub fn cont<'a>(
        &mut self,
        payload: &'a [u8],
        continuity: Continuity,
        same_cc_conflict: bool,
    ) -> Option<&'a [u8]> {
        match self.position {
            Position::InElementaryStream => {
                if same_cc_conflict {
                    None
                } else {
                    Some(payload)
                }
            }
            Position::AwaitingStart => None,
            Position::InHeader { remaining } => match continuity {
                Continuity::Broken | Continuity::Discontinuous => {
                    self.position = Position::AwaitingStart;
                    None
                }
                Continuity::Duplicate => None,
                Continuity::First | Continuity::Continuous => {
                    if payload.len() < remaining {
                        self.position = Position::InHeader {
                            remaining: remaining - payload.len(),
                        };
                        None
                    } else {
                        self.position = Position::InElementaryStream;
                        Some(&payload[remaining..])
                    }
                }
            },
        }
    }

    fn reset_on_pusi_or_header(&mut self, is_pusi: bool) {
        if is_pusi || matches!(self.position, Position::InHeader { .. }) {
            self.position = Position::AwaitingStart;
        }
    }
}

/// State of one PMT-declared audio stream.
#[derive(Debug)]
pub struct AudioTrackState {
    /// The stream's packet identifier.
    pub pid: u16,
    /// Codec string declared in the PMT.
    pub codec: String,
    /// Transport continuity tracker for duplicate suppression and loss detection.
    pub continuity: ContinuityTracker,
    /// Physical clear packets seen on this PID.
    pub clear_packets: u64,
    /// Scrambled packets seen on this PID.
    pub scrambled_packets: u64,
    /// Elementary state parser and observer if this codec is observable.
    pub elementary: Option<AudioElementaryState>,
}

impl AudioTrackState {
    /// Constructs a new track state from a PMT audio declaration.
    #[must_use]
    pub fn new(track: &AudioTrack) -> Self {
        let elementary = if observable(&track.codec) {
            Some(AudioElementaryState::new())
        } else {
            None
        };
        Self {
            pid: track.pid,
            codec: track.codec.clone(),
            continuity: ContinuityTracker::new(),
            clear_packets: 0,
            scrambled_packets: 0,
            elementary,
        }
    }

    /// Point-in-time observation established for this track.
    #[must_use]
    pub fn observation(&self) -> Observation {
        self.elementary
            .as_ref()
            .map_or(Observation::default(), |e| e.observer.current())
    }
}

/// One elementary stream feed produced during routing.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct AudioFeedOutput<'a> {
    /// The stream PID the feed arrived on.
    pub pid: u16,
    /// Elementary stream bytes.
    pub es: &'a [u8],
    /// Observation resulting from feeding the observer.
    pub observation: Observation,
}

/// Tracks all PMT-declared audio streams and global audio scrambling counters.
#[derive(Debug, Default)]
pub struct AudioTracker {
    /// Track states for all PMT-declared audio PIDs.
    pub tracks: Vec<AudioTrackState>,
    /// Global scrambled packets across all declared audio streams.
    pub scrambled_packets: u64,
    /// Global clear packets across all declared audio streams.
    pub clear_packets: u64,
    /// Consecutive clear audio packets since last scrambled packet.
    pub clear_run: u64,
}

impl AudioTracker {
    /// Creates an empty audio tracker.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    /// Resets all audio streams and counters (e.g. on PMT change or target program change).
    pub fn reset_with_tracks(&mut self, tracks: &[AudioTrack]) {
        self.scrambled_packets = 0;
        self.clear_packets = 0;
        self.clear_run = 0;
        self.tracks.clear();
        for track in tracks {
            self.tracks.push(AudioTrackState::new(track));
        }
    }

    /// Returns whether any track matches the given PID.
    #[must_use]
    pub fn handles_pid(&self, pid: u16) -> bool {
        self.tracks.iter().any(|t| t.pid == pid)
    }

    /// Routes one packet to its corresponding audio track.
    ///
    /// Returns any elementary stream feed that was produced and fed to an observer.
    pub fn route<'a>(&mut self, view: &PacketView<'a>) -> Option<AudioFeedOutput<'a>> {
        let pid = view.pid();
        let track = self.tracks.iter_mut().find(|t| t.pid == pid)?;

        // 1. Adaptation field only: does not carry payload or advance CC.
        let Some(payload) = view.payload() else {
            if view.discontinuity_indicator() {
                track.continuity.observe_adaptation_only(true);
            }
            return None;
        };

        // 2. Evaluate transport continuity.
        let is_same_cc = track.continuity.is_same_cc(view.continuity_counter());
        let continuity = track.continuity.observe(
            view.bytes(),
            view.continuity_counter(),
            view.discontinuity_indicator(),
        );

        if continuity == Continuity::Duplicate {
            return None;
        }

        let same_cc_conflict =
            continuity == Continuity::Broken && is_same_cc && !view.discontinuity_indicator();

        // 3. Transport Error Indicator (TEI): damaged in transit.
        // Does NOT increment clear or scrambled counters, and does NOT alter clear_run.
        if view.transport_error_indicator() {
            if let Some(elem) = track.elementary.as_mut() {
                elem.reset_on_pusi_or_header(view.payload_unit_start());
            }
            return None;
        }

        // 4. Scrambled packet: count against audio scrambling facts.
        if view.scrambling_control() != 0 {
            self.scrambled_packets += 1;
            self.clear_run = 0;
            track.scrambled_packets += 1;
            if let Some(elem) = track.elementary.as_mut() {
                elem.reset_on_pusi_or_header(view.payload_unit_start());
            }
            return None;
        }

        // 5. Clear packet: increments clear counters BEFORE observer decision.
        self.clear_packets += 1;
        self.clear_run += 1;
        track.clear_packets += 1;

        // 6. Observer feeding (only for observable streams).
        let elem = track.elementary.as_mut()?;

        let es = if view.payload_unit_start() {
            if same_cc_conflict {
                elem.position = Position::AwaitingStart;
                None
            } else {
                elem.start(payload)
            }
        } else {
            elem.cont(payload, continuity, same_cc_conflict)
        };

        let es = es.filter(|bytes| !bytes.is_empty())?;
        elem.observer.feed(es);
        elem.feeds += 1;

        Some(AudioFeedOutput {
            pid,
            es,
            observation: elem.observer.current(),
        })
    }
}
