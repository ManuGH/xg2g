// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! Raw transport in, audio elementary stream out.
//!
//! Everything below this module already existed and none of it is repeated
//! here. [`crate::transport`] says what a packet is, [`crate::psi`] says which
//! PIDs carry audio and what codec they declare, [`crate::pes`] says where a
//! PES packet begins and where its elementary stream does, and
//! [`crate::audio::observer::Observer`] reads frames. What was missing is the
//! part that joins them: which observer a packet's bytes belong to, and which
//! of those bytes are elementary stream at all.
//!
//! The observer stays what it was. It is handed elementary stream payload and
//! nothing else - no PID, no packet, no table - so what it reads remains a
//! property of the audio rather than of how the audio arrived. Teaching it
//! transport would have made the layering a comment rather than a fact.
//!
//! # What this decides, and what it refuses to
//!
//! It decides which bytes are elementary stream. It does not decide what they
//! mean for a viewer: a scrambled packet is a packet whose bytes are not fed,
//! not a failed CAM, an unusable channel or a reason to fall back. Those are
//! product decisions and they are made elsewhere, on facts this layer supplies
//! rather than on conclusions it draws.
//!
//! # The reference and the one place it is not followed
//!
//! The Go core in `backend/internal/stream/ingest/mediafacts` is the reference
//! for as long as this step lasts, and the two are held together by a corpus of
//! raw transport checked in at `testdata/audio-ts-corpus/corpus.txt`.
//!
//! There is one authored case where this and the reference disagree on purpose,
//! and it is worth stating in the module that contains the difference rather
//! than only in a pull request. When a PES header reaches past the end of the
//! packet that started it, the reference feeds nothing for that packet and then
//! feeds the next continuation whole - so the rest of the header arrives at its
//! observer as elementary stream. This carries the length of the remainder
//! instead, and feeds only what follows it. The bytes the reference feeds there
//! are not elementary stream, and the corpus records the difference as a
//! difference rather than adopting it.

use crate::audio::observer::{Observation, Observer};
use crate::pes::{self, PesStart};
use crate::psi::{AudioTrack, IngestError, PsiCore, PsiEvent};
use crate::transport::{Continuity, ContinuityTracker, PacketView, TS_PACKET_LEN};

/// One run of elementary stream bytes, exactly as an observer was given them.
///
/// Where the bytes were cut is part of what happened and not an artefact of it.
/// An observer carries a partial frame header across a call and skips payload
/// that ran past the end of one, so the same bytes joined differently are a
/// different input - which is why this records a feed per packet rather than a
/// total per chunk.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct AudioFeed<'a> {
    /// Which incarnation of the stream this belonged to. The same PID before
    /// and after a programme identity change is two streams, not one.
    pub incarnation: u64,
    /// The PID it arrived on.
    pub pid: u16,
    /// The bytes, borrowed from the chunk they came in.
    pub es: &'a [u8],
    /// What the observer said after being given them.
    pub observation: Observation,
}

/// What one chunk meant.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AudioOutcome<'a> {
    /// The offset one past the last byte interpreted.
    pub processed_through: i64,
    /// The feeds the chunk produced, in the order they happened.
    pub feeds: Vec<AudioFeed<'a>>,
}

/// An audio stream being followed, and what it has been seen to carry.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FollowedStream {
    /// Which incarnation of the stream this is.
    pub incarnation: u64,
    /// The PID the table put it on.
    pub pid: u16,
    /// The codec the table declared.
    pub codec: String,
    /// What its observer has established.
    pub observation: Observation,
    /// How many feeds it has been given since it began.
    pub feeds: u64,
    /// Clear packets seen on the PID since it began.
    pub clear_packets: u64,
    /// Scrambled packets seen on the PID since it began. Counted, and fed to
    /// nothing.
    pub scrambled_packets: u64,
    /// Payload units that began an audio PES packet.
    pub pes_starts: u64,
    /// How many of those declared an optional header reaching past the packet
    /// that carried it.
    ///
    /// The one place this and the reference answer differently, counted, so
    /// that "the difference never arises on real transport" can be a
    /// measurement rather than an expectation.
    pub header_incomplete: u64,
}

/// Where in a PES packet the next continuation payload begins.
///
/// Three states rather than two, because "we do not know" and "we know these
/// bytes are not elementary stream" are different answers and only one of them
/// is a reason to feed nothing.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Position {
    /// A continuation payload is elementary stream as far as anything here can
    /// tell.
    ///
    /// This is the ordinary state and also the honest answer after a payload
    /// unit this layer could not read: the bytes after a PES header it did not
    /// recognise are still that packet's payload, and a packet's payload on an
    /// elementary stream PID is elementary stream. Refusing them would be
    /// stricter without being truer.
    InElementaryStream,

    /// An optional PES header has not finished, and this many of its bytes are
    /// still to come.
    InHeader {
        /// How many header bytes the following payloads still carry.
        remaining: usize,
    },

    /// Nothing may be fed until a PES packet starts.
    ///
    /// Entered only from [`Position::InHeader`], and only when the bytes that
    /// would have completed that header cannot be accounted for: the transport
    /// lost a packet, or the ones carrying it were scrambled. Feeding what
    /// arrives next would mean feeding from an offset nothing established.
    AwaitingStart,
}

/// One observable audio elementary stream, followed across the packets that
/// carry it.
#[derive(Debug)]
struct Follower {
    pid: u16,
    codec: String,
    observer: Observer,
    position: Position,
    /// The PID's own continuity, used for one decision and no other: whether
    /// the bytes that were to complete a PES header actually arrived. Audio
    /// observation itself does not react to the counter - a stream that lost a
    /// packet is a stream with a gap in it, not a stream whose channel layout
    /// has been withdrawn.
    continuity: ContinuityTracker,
    feeds: u64,
    clear_packets: u64,
    scrambled_packets: u64,
    pes_starts: u64,
    header_incomplete: u64,
}

impl Follower {
    fn new(track: &AudioTrack) -> Self {
        Self {
            pid: track.pid,
            codec: track.codec.clone(),
            observer: Observer::new(),
            // Nothing has said where this stream is yet, and a continuation
            // arriving before any start is still elementary stream: a capture
            // that begins in the middle of a PES packet carries audio from its
            // first packet, and the observer resynchronises on its own.
            position: Position::InElementaryStream,
            continuity: ContinuityTracker::new(),
            feeds: 0,
            clear_packets: 0,
            scrambled_packets: 0,
            pes_starts: 0,
            header_incomplete: 0,
        }
    }

    /// Reads a payload that begins a payload unit.
    fn start<'a>(&mut self, payload: &'a [u8]) -> Option<&'a [u8]> {
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
                // The elementary stream has not begun. What the next payloads
                // start with is the rest of this header, and a consumer told
                // nothing would read it as audio.
                self.position = Position::InHeader {
                    remaining: remaining_header,
                };
                None
            }
            // A payload unit that is not an audio PES packet: no start code, a
            // stream id that is not audio, too few bytes to say, or a stream id
            // that carries no optional header at all - none of which is an
            // audio stream id. Nothing is fed from this payload, because where
            // the elementary stream would begin in it is exactly what could not
            // be read. What follows is still this PID's payload.
            _ => {
                self.position = Position::InElementaryStream;
                None
            }
        }
    }

    /// Reads a payload that continues a payload unit already under way.
    fn cont<'a>(&mut self, payload: &'a [u8], continuity: Continuity) -> Option<&'a [u8]> {
        match self.position {
            Position::InElementaryStream => Some(payload),
            Position::AwaitingStart => None,
            Position::InHeader { remaining } => match continuity {
                // The transport says bytes are missing. How many of them were
                // header is not knowable, so the offset the rest of this packet
                // would be read from is not either. Skipping `remaining` anyway
                // would be treating a loss as though it had arrived.
                Continuity::Broken => {
                    self.position = Position::AwaitingStart;
                    None
                }
                // The same packet said twice carries the same header bytes
                // twice. Consuming them again would step over payload that
                // never arrived.
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
}

/// Follows the observable audio of one transport.
///
/// It owns the table reading it needs, because which PIDs are audio and what
/// they declare is not something a caller can be asked to keep in step: the
/// answer changes inside a chunk, at the packet that changed it, and a stage
/// told about it afterwards would route that chunk's audio to the observers of
/// a programme that had already ended.
///
/// What it does not own is anything above the bytes. There is no generation
/// here, no session, no lease, no receiver and no viewer policy. The
/// incarnation counter below is not a product generation: it exists so that
/// state cannot survive the table that named the stream it belongs to, and it
/// is deliberately not offered as anything else.
///
/// Not safe for concurrent use, like the cores beneath it.
#[derive(Debug)]
pub struct AudioIngress {
    psi: PsiCore,
    followers: Vec<Follower>,
    /// Turns whenever the programme's identity does. The same PID on either
    /// side of that is two elementary streams that share a number.
    incarnation: u64,
}

impl AudioIngress {
    /// An ingress following one programme, or whichever the PAT offers first.
    #[must_use]
    pub fn new(target_program_number: u16) -> Self {
        Self {
            psi: PsiCore::new(target_program_number),
            followers: Vec::new(),
            incarnation: 0,
        }
    }

    /// Interprets one chunk of transport.
    ///
    /// # Errors
    ///
    /// Returns [`IngestError::UnalignedChunk`] when the chunk is not a whole
    /// number of packets. Nothing is interpreted in that case - the same rule
    /// the PSI core states, for the same reason: a chunk boundary can only fall
    /// between packets.
    pub fn ingest<'a>(
        &mut self,
        start_offset: i64,
        data: &'a [u8],
    ) -> Result<AudioOutcome<'a>, IngestError> {
        if !data.len().is_multiple_of(TS_PACKET_LEN) {
            return Err(IngestError::UnalignedChunk { len: data.len() });
        }
        let mut feeds = Vec::new();
        self.psi.begin_chunk();
        for packet in data.chunks_exact(TS_PACKET_LEN) {
            // The table is read first, and the audio of this same packet is
            // routed afterwards. A PMT completing here ends the streams it used
            // to describe, and a packet that carried audio of the old programme
            // would otherwise reach an observer the table has just retired.
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
            self.route(&view, &mut feeds);
        }
        let consumed = i64::try_from(data.len()).unwrap_or(i64::MAX);
        Ok(AudioOutcome {
            processed_through: start_offset.saturating_add(consumed),
            feeds,
        })
    }

    /// Selects the programme to follow.
    ///
    /// Changing it ends every stream being followed, for the same reason a PMT
    /// change does: what the PIDs carried belonged to the programme that was
    /// selected, and it is no longer selected.
    pub fn set_target_program(&mut self, program_number: u16) {
        let outcome = self.psi.set_target_program(program_number);
        if outcome.events.contains(&PsiEvent::ProgramIdentityChanged) {
            self.reprogram();
        }
    }

    /// Which incarnation the streams being followed belong to.
    #[must_use]
    pub fn incarnation(&self) -> u64 {
        self.incarnation
    }

    /// The audio tracks the table in force declares, observable or not.
    ///
    /// A track this cannot read is still a track. Saying so is what keeps "no
    /// observation" apart from "no audio", which are different answers and only
    /// one of them is about the stream.
    #[must_use]
    pub fn declared(&self) -> &[AudioTrack] {
        self.psi.audio_tracks()
    }

    /// The streams being followed, in the order the table lists them.
    #[must_use]
    pub fn followed(&self) -> Vec<FollowedStream> {
        self.followers
            .iter()
            .map(|f| FollowedStream {
                incarnation: self.incarnation,
                pid: f.pid,
                codec: f.codec.clone(),
                observation: f.observer.current(),
                feeds: f.feeds,
                clear_packets: f.clear_packets,
                scrambled_packets: f.scrambled_packets,
                pes_starts: f.pes_starts,
                header_incomplete: f.header_incomplete,
            })
            .collect()
    }

    /// Ends every stream being followed and begins the ones the table in force
    /// now declares.
    ///
    /// Nothing is carried over, including for a PID that is in both tables with
    /// the same codec. A programme whose identity changed may put a different
    /// elementary stream on the same number, and an observation carried across
    /// would describe audio that is not there any more - which is the one
    /// mistake a reused PID makes impossible to notice afterwards.
    fn reprogram(&mut self) {
        self.incarnation += 1;
        self.followers.clear();
        for track in self.psi.audio_tracks() {
            if observable(&track.codec) {
                self.followers.push(Follower::new(track));
            }
        }
    }

    /// Routes one packet to the stream it belongs to, if that stream is one
    /// being followed.
    fn route<'a>(&mut self, view: &PacketView<'a>, out: &mut Vec<AudioFeed<'a>>) {
        let pid = view.pid();
        let Some(index) = self.followers.iter().position(|f| f.pid == pid) else {
            return;
        };
        // A packet with no payload carries nothing to feed and does not advance
        // the continuity counter either, so it is not offered to the tracker.
        let Some(payload) = view.payload() else {
            return;
        };
        let incarnation = self.incarnation;
        let follower = &mut self.followers[index];

        let continuity = follower
            .continuity
            .observe(view.bytes(), view.continuity_counter());

        if view.scrambling_control() != 0 {
            follower.scrambled_packets += 1;
            // Encrypted bytes are not fed, and they are not counted towards a
            // header either: a header whose remainder arrived scrambled has not
            // been read, and the next clear payload begins somewhere nothing
            // here knows.
            if matches!(follower.position, Position::InHeader { .. }) {
                follower.position = Position::AwaitingStart;
            }
            return;
        }
        follower.clear_packets += 1;

        let es = if view.payload_unit_start() {
            follower.start(payload)
        } else {
            follower.cont(payload, continuity)
        };

        // An empty run is not a feed. It is what a PES header ending exactly at
        // the end of its packet leaves behind, and an observer given nothing has
        // been told nothing.
        let Some(es) = es.filter(|es| !es.is_empty()) else {
            return;
        };
        follower.observer.feed(es);
        follower.feeds += 1;
        out.push(AudioFeed {
            incarnation,
            pid,
            es,
            observation: follower.observer.current(),
        });
    }
}

/// Whether the frame headers of a codec are read for their channel layout.
///
/// The same two the reference reads. A codec whose frames this cannot parse
/// gets no observer rather than a guessed observation, and a track declared in
/// the table is still a track - it is the observation that is absent, not the
/// stream.
fn observable(codec: &str) -> bool {
    codec == "ac3" || codec == "eac3"
}

#[cfg(test)]
mod ingress_test;

#[cfg(test)]
mod corpus_test;
