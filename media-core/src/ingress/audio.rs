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
use crate::pes::{self, PesHeaderAssembler};
use crate::psi::AudioTrack;
use crate::timing::{Pid, TimingEvent};
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
    /// Bounded common PES header assembler for timing extraction.
    pub pes_assembler: PesHeaderAssembler,
}

impl AudioTrackState {
    /// Constructs a new track state from a PMT audio declaration.
    ///
    /// # Panics
    ///
    /// Panics if `track.pid` is not a valid 13-bit PID (`> 0x1FFF`).
    #[must_use]
    pub fn new(track: &AudioTrack) -> Self {
        let elementary = if observable(&track.codec) {
            Some(AudioElementaryState::new())
        } else {
            None
        };
        let pid_typed = Pid::new(track.pid).expect("PMT parser only produces 13-bit PIDs");
        Self {
            pid: track.pid,
            codec: track.codec.clone(),
            continuity: ContinuityTracker::new(),
            clear_packets: 0,
            scrambled_packets: 0,
            elementary,
            pes_assembler: PesHeaderAssembler::new(pid_typed),
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
    /// Returns any elementary stream feed that was produced and fed to an observer,
    /// along with any canonical timing event extracted by the PES assembler.
    #[allow(clippy::too_many_lines)]
    pub fn route<'a>(
        &mut self,
        packet_offset: i64,
        view: &PacketView<'a>,
    ) -> (Option<AudioFeedOutput<'a>>, Option<TimingEvent>) {
        let pid = view.pid();
        let Some(track) = self.tracks.iter_mut().find(|t| t.pid == pid) else {
            return (None, None);
        };

        // 1. Adaptation field only: does not carry payload or advance CC.
        let Some(payload) = view.payload() else {
            if view.discontinuity_indicator() {
                track.continuity.observe_adaptation_only(true);
            }
            return (None, None);
        };

        // 2. Evaluate transport continuity.
        let is_same_cc = track.continuity.is_same_cc(view.continuity_counter());
        let continuity = track.continuity.observe(
            view.bytes(),
            view.continuity_counter(),
            view.discontinuity_indicator(),
        );

        if continuity == Continuity::Duplicate {
            return (None, None);
        }

        let same_cc_conflict =
            continuity == Continuity::Broken && is_same_cc && !view.discontinuity_indicator();

        // 3. Transport Error Indicator (TEI): damaged in transit.
        // Does NOT increment clear or scrambled counters, and does NOT alter clear_run.
        if view.transport_error_indicator() {
            if view.payload_unit_start() || track.pes_assembler.is_in_header() {
                if let Some(elem) = track.elementary.as_mut() {
                    elem.position = Position::AwaitingStart;
                }
                track.pes_assembler.reset();
            }
            return (None, None);
        }

        // 4. Scrambled packet: count against audio scrambling facts.
        if view.scrambling_control() != 0 {
            self.scrambled_packets += 1;
            self.clear_run = 0;
            track.scrambled_packets += 1;
            if view.payload_unit_start() || track.pes_assembler.is_in_header() {
                if let Some(elem) = track.elementary.as_mut() {
                    elem.position = Position::AwaitingStart;
                }
                track.pes_assembler.reset();
            }
            return (None, None);
        }

        // 5. Clear packet: increments clear counters BEFORE observer decision.
        self.clear_packets += 1;
        self.clear_run += 1;
        track.clear_packets += 1;

        if same_cc_conflict
            || (!view.payload_unit_start()
                && (continuity == Continuity::Broken || continuity == Continuity::Discontinuous))
        {
            if let Some(elem) = track.elementary.as_mut() {
                elem.position = Position::AwaitingStart;
            }
            track.pes_assembler.reset();
            return (None, None);
        }

        // In audio, a PUSI packet whose fixed header is cut short by an adaptation field (< 9 bytes)
        // is an unreadable start and quarantines the stream until the next PUSI.
        if view.payload_unit_start() && payload.len() < pes::FIXED_HEADER_LEN {
            track.pes_assembler.reject();
            if let Some(elem) = track.elementary.as_mut() {
                elem.position = Position::AwaitingStart;
            }
            return (None, None);
        }

        // 6. Timing & ES extraction via bounded common PES assembler
        let output = if view.payload_unit_start() {
            track.pes_assembler.feed_pusi(packet_offset, payload)
        } else {
            track.pes_assembler.feed_cont(packet_offset, payload)
        };

        // Update elementary position from assembler state
        if let Some(elem) = track.elementary.as_mut() {
            if let Some(rem) = track.pes_assembler.remaining_header_bytes() {
                elem.position = Position::InHeader { remaining: rem };
            } else if track.pes_assembler.is_in_elementary_stream() {
                elem.position = Position::InElementaryStream;
            } else {
                elem.position = Position::AwaitingStart;
            }
        }

        // Validate audio stream_id if header was emitted
        if let Some(header) = output.header {
            if !pes::is_audio_stream_id(header.stream_id) {
                track.pes_assembler.reject();
                if let Some(elem) = track.elementary.as_mut() {
                    elem.position = Position::AwaitingStart;
                }
                return (None, None);
            }
            if let Some(elem) = track.elementary.as_mut() {
                elem.pes_starts += 1;
                if output.header_spanned_start_packet {
                    elem.header_incomplete += 1;
                }
            }
        }

        let timing_event = output.timing_event;

        // 7. Observer feeding (only for observable streams).
        let Some(elem) = track.elementary.as_mut() else {
            return (None, timing_event);
        };

        let Some(es) = output.es.filter(|bytes| !bytes.is_empty()) else {
            return (None, timing_event);
        };
        elem.observer.feed(es);
        elem.feeds += 1;

        let feed = AudioFeedOutput {
            pid,
            es,
            observation: elem.observer.current(),
        };

        (Some(feed), timing_event)
    }
}
