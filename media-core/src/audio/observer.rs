// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! One audio elementary stream, followed for as long as it runs.

use super::ac3::{FrameHeader, HEADER_BYTES, SYNC_HI, SYNC_LO, parse_sync_frame};

/// How many consecutive frames must agree before the observation changes.
///
/// A syncword is two bytes and audio payload contains them by chance, so a
/// single parse is a guess. A run broken by noise simply restarts; the previous
/// observation stands until a new one is actually established, so the failure
/// direction is "keeps saying what it last proved", never "invents something
/// new".
pub const STABLE_FRAMES: u32 = 3;

/// How much tail is kept for the next feed. A syncframe header split across two
/// feeds is completed from it; anything longer cannot be the start of an
/// unexamined header.
const CARRY_BYTES: usize = HEADER_BYTES - 1;

/// What an elementary stream has been seen to carry.
///
/// The default is "nothing has been established", which is a different state
/// from stereo. Nothing here fills that in: a stream that has not said how many
/// channels it has does not have two.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct Observation {
    /// Full-bandwidth channels plus LFE, or 0 when no layout has been
    /// established.
    pub channels: u8,
    /// The low frequency effects channel, included in `channels` and named
    /// separately because a layout decision may want it.
    pub lfe: bool,
    /// The raw audio coding mode the count was derived from.
    pub acmod: u8,
    /// Whether `acmod` means anything yet.
    pub has_acmod: bool,
    /// E-AC-3 dependent substream frames were seen on this stream. `channels`
    /// then describes the independent substream only, and the programme may
    /// carry more than it says.
    pub dependent_substream: bool,
    /// Syncframes parsed since the observation started, so a consumer can tell a
    /// settled reading from a stream that has barely arrived.
    pub frames: u64,
}

impl Observation {
    /// Whether the stream has said how many channels it carries.
    #[must_use]
    pub const fn known(&self) -> bool {
        self.channels > 0
    }

    /// Compares only the fields a change has to be proved for.
    const fn same_layout(&self, other: &Self) -> bool {
        self.channels == other.channels && self.lfe == other.lfe && self.acmod == other.acmod
    }
}

/// Follows one audio elementary stream and reports what its frames say about the
/// channel layout.
///
/// It never stops looking. A service changes its audio between programmes as a
/// matter of course - the same stream going from AC-3 2.0 to 5.1 for a film and
/// back for the advertisements - so a single reading taken at the start is not
/// an answer, it is an answer with an expiry date nobody recorded.
#[derive(Debug, Default)]
pub struct Observer {
    carry: Vec<u8>,

    /// Frame payload that ran past the end of a feed, dropped from the front of
    /// the next one instead of being scanned for syncwords it cannot
    /// legitimately contain.
    skip: usize,

    current: Observation,
    candidate: Observation,
    candidate_run: u32,

    frames: u64,
    dependent_seen: bool,
}

impl Observer {
    /// An observer with nothing established yet.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    /// Consumes elementary stream bytes.
    ///
    /// Feed boundaries are arbitrary: a syncframe split across two calls is
    /// parsed from the second. They are not, however, invisible - where the
    /// caller cut the bytes is part of the input, which is why the corpus
    /// records feeds rather than one blob.
    pub fn feed(&mut self, es: &[u8]) {
        if es.is_empty() {
            return;
        }

        let mut es = es;
        if self.skip > 0 {
            if self.skip >= es.len() {
                self.skip -= es.len();
                return;
            }
            es = &es[self.skip..];
            self.skip = 0;
        }

        let buf: Vec<u8> = if self.carry.is_empty() {
            es.to_vec()
        } else {
            let mut b = core::mem::take(&mut self.carry);
            b.extend_from_slice(es);
            b
        };

        let mut i = 0usize;
        while i + HEADER_BYTES <= buf.len() {
            if buf[i] != SYNC_HI || buf[i + 1] != SYNC_LO {
                i += 1;
                continue;
            }
            let Some(h) = parse_sync_frame(&buf[i..]) else {
                i += 1;
                continue;
            };
            self.observe(h);

            // Step over the whole frame. Its payload is compressed audio and
            // carries the sync pattern by chance often enough to matter, so
            // rescanning it is how a parser talks itself into a layout the
            // stream never had.
            i += if h.size_bytes > HEADER_BYTES {
                h.size_bytes
            } else {
                HEADER_BYTES
            };
        }

        if i > buf.len() {
            self.skip = i - buf.len();
            self.carry.clear();
            return;
        }

        let tail = &buf[i..];
        let tail = if tail.len() > CARRY_BYTES {
            &tail[tail.len() - CARRY_BYTES..]
        } else {
            tail
        };
        self.carry.clear();
        self.carry.extend_from_slice(tail);
    }

    /// Folds one frame into the running state.
    fn observe(&mut self, h: FrameHeader) {
        if h.dependent {
            self.dependent_seen = true;
            return;
        }
        if h.channels == 0 {
            return;
        }
        self.frames += 1;

        let next = Observation {
            channels: h.channels,
            lfe: h.lfe,
            acmod: h.acmod,
            has_acmod: true,
            ..Observation::default()
        };
        if self.candidate_run > 0 && next.same_layout(&self.candidate) {
            self.candidate_run += 1;
        } else {
            self.candidate = next;
            self.candidate_run = 1;
        }
        if self.candidate_run >= STABLE_FRAMES {
            self.current = next;
        }
    }

    /// The established observation, or the default while none has been.
    #[must_use]
    pub fn current(&self) -> Observation {
        Observation {
            frames: self.frames,
            dependent_substream: self.dependent_seen,
            ..self.current
        }
    }
}

#[cfg(test)]
mod tests {
    use super::{Observation, Observer};

    /// byte 6 per acmod, written out from A/52 rather than produced by the same
    /// offset arithmetic the parser uses. lfeon lands on the bit named here.
    const LFE_BIT: [u32; 8] = [4, 4, 2, 2, 2, 0, 2, 0];

    fn ac3(acmod: u8, lfe: bool) -> Vec<u8> {
        let mut byte6 = acmod << 5;
        if lfe {
            byte6 |= 1 << LFE_BIT[usize::from(acmod)];
        }
        let mut f = vec![0u8; 128];
        f[0] = 0x0B;
        f[1] = 0x77;
        f[2] = 0xAA;
        f[3] = 0xBB;
        f[4] = 0x00; // fscod 48 kHz, frmsizecod 0 -> 128 bytes
        f[5] = 8 << 3; // bsid 8
        f[6] = byte6;
        f
    }

    fn channels(acmod: u8, lfe: bool) -> u8 {
        let n = [2u8, 1, 2, 3, 3, 4, 4, 5][usize::from(acmod)];
        if lfe { n + 1 } else { n }
    }

    /// A different question from the corpus, kept apart on purpose: the same
    /// logical elementary stream, cut in every way, has to end at the same
    /// observation. The corpus proves cross-language agreement at fixed
    /// boundaries; this proves the observer does not depend on them.
    #[test]
    fn the_final_answer_does_not_depend_on_where_the_bytes_were_cut() {
        let mut stream = vec![0xFFu8; 3];
        for _ in 0..3 {
            stream.extend_from_slice(&ac3(2, false));
        }
        for _ in 0..3 {
            stream.extend_from_slice(&ac3(7, true));
        }
        let want = Observation {
            channels: channels(7, true),
            lfe: true,
            acmod: 7,
            has_acmod: true,
            dependent_substream: false,
            frames: 6,
        };

        for size in [
            1usize,
            2,
            3,
            5,
            7,
            13,
            64,
            127,
            128,
            129,
            200,
            512,
            stream.len(),
        ] {
            let mut o = Observer::new();
            for chunk in stream.chunks(size) {
                o.feed(chunk);
            }
            assert_eq!(o.current(), want, "cut into {size}-byte feeds");
        }
    }

    /// Any bytes at all, and the invariants still hold. Not a fuzzer - the
    /// fuzzing lives on the Go side, where findings are promoted into the shared
    /// corpus - but enough that an obviously wrong index or a panic on garbage
    /// does not need a second language to be noticed.
    #[test]
    fn arbitrary_bytes_never_break_the_invariants() {
        let mut state: u64 = 0x2545_F491_4F6C_DD1D;
        let mut next = || {
            state ^= state << 13;
            state ^= state >> 7;
            state ^= state << 17;
            u8::try_from(state & 0xFF).expect("masked to a byte")
        };

        for round in 0..200 {
            let mut o = Observer::new();
            let mut last_frames = 0u64;
            for _ in 0..64 {
                let len = usize::from(next()) + 1;
                let bytes: Vec<u8> = (0..len).map(|_| next()).collect();
                o.feed(&bytes);
                let got = o.current();
                assert_eq!(got.known(), got.channels > 0, "round {round}");
                assert!(
                    got.channels == 0 || got.has_acmod,
                    "a channel count without the acmod it came from, round {round}"
                );
                assert!(got.channels <= 6, "round {round}: {got:?}");
                assert!(got.acmod <= 7, "round {round}: {got:?}");
                assert!(got.frames >= last_frames, "the frame count went backwards");
                last_frames = got.frames;
            }
        }
    }
}
