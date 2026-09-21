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

pub mod audio;
pub use audio::{
    AudioElementaryState, AudioFeedOutput, AudioTrackState, AudioTracker, Position, observable,
};

pub mod video;
pub use video::{VideoEvent, VideoFacts, VideoFeed, VideoIngress, VideoOutcome, VideoSnapshot};

use crate::audio::observer::Observation;
use crate::psi::{AudioTrack, IngestError, PsiCore, PsiEvent};
use crate::transport::{PacketView, TS_PACKET_LEN};

/// One run of elementary stream bytes, exactly as an observer was given them.
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
    pub header_incomplete: u64,
}

/// Follows the observable audio of one transport.
#[derive(Debug)]
pub struct AudioIngress {
    psi: PsiCore,
    audio: AudioTracker,
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
            audio: AudioTracker::new(),
            incarnation: 0,
        }
    }

    /// Interprets one chunk of transport.
    ///
    /// # Errors
    ///
    /// Returns [`IngestError::UnalignedChunk`] when the chunk is not a whole
    /// number of packets.
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
        for (idx, packet) in data.chunks_exact(TS_PACKET_LEN).enumerate() {
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
            let packet_offset = start_offset.saturating_add(
                i64::try_from(idx.saturating_mul(TS_PACKET_LEN)).unwrap_or(i64::MAX),
            );
            self.route(packet_offset, &view, &mut feeds);
        }
        let consumed = i64::try_from(data.len()).unwrap_or(i64::MAX);
        Ok(AudioOutcome {
            processed_through: start_offset.saturating_add(consumed),
            feeds,
        })
    }

    /// Selects the programme to follow.
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
    #[must_use]
    pub fn declared(&self) -> &[AudioTrack] {
        self.psi.audio_tracks()
    }

    /// The streams being followed, in the order the table lists them.
    #[must_use]
    pub fn followed(&self) -> Vec<FollowedStream> {
        self.audio
            .tracks
            .iter()
            .filter_map(|t| {
                t.elementary.as_ref().map(|elem| FollowedStream {
                    incarnation: self.incarnation,
                    pid: t.pid,
                    codec: t.codec.clone(),
                    observation: elem.observer.current(),
                    feeds: elem.feeds,
                    clear_packets: t.clear_packets,
                    scrambled_packets: t.scrambled_packets,
                    pes_starts: elem.pes_starts,
                    header_incomplete: elem.header_incomplete,
                })
            })
            .collect()
    }

    /// Access to the underlying audio tracker.
    #[must_use]
    pub fn tracker(&self) -> &AudioTracker {
        &self.audio
    }

    /// Ends every stream being followed and begins the ones the table in force
    /// now declares.
    fn reprogram(&mut self) {
        self.incarnation += 1;
        self.audio.reset_with_tracks(self.psi.audio_tracks());
    }

    /// Routes one packet to the stream it belongs to.
    fn route<'a>(
        &mut self,
        packet_offset: i64,
        view: &PacketView<'a>,
        out: &mut Vec<AudioFeed<'a>>,
    ) {
        let pid = view.pid();
        if pid == self.psi.pmt_pid() || pid == self.psi.video_pid() {
            return;
        }
        if let Some(feed) = self.audio.route(packet_offset, view).feed {
            out.push(AudioFeed {
                incarnation: self.incarnation,
                pid: feed.pid,
                es: feed.es,
                observation: feed.observation,
            });
        }
    }
}

#[cfg(test)]
mod ingress_test;

#[cfg(test)]
mod corpus_test;

#[cfg(test)]
mod video_test;

#[cfg(test)]
mod video_corpus_test;
