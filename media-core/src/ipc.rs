// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! The wire this core is spoken to over.
//!
//! One request at a time, length-prefixed, with a version checked once. The
//! layout is defined on the Go side and mirrored here; a golden-frame test on
//! both sides is what keeps the two from drifting, because two implementations of
//! one format always eventually disagree about something.
//!
//! Most of what arrives here is not interpreted: the ingest answers follow from
//! the request alone, because reading transport streams is not this step's claim.
//! The one exception is the audio shadow, which is the whole point of it - an
//! observe request is answered by actually observing the bytes it carries.

use std::io::{self, Read, Write};

use xg2g_media_core::audio::shadow::{Registry, StreamEpoch};

/// The protocol version this build speaks. Checked once, fatal when it differs.
///
/// 2 adds [`MSG_OBSERVE_AUDIO_BATCH`]. There is no negotiation and the message
/// set is closed, so the version is the only thing that says "this peer can be
/// asked what this build wants to ask". A core still answering 1 would refuse an
/// observe request as an unknown message - a round trip later, and looking like a
/// rejected batch rather than a peer that cannot do this at all.
pub const VERSION: u8 = 2;

pub const MSG_HANDSHAKE: u8 = 1;
pub const MSG_INGEST: u8 = 2;
pub const MSG_SET_TARGET_PROGRAM: u8 = 3;
pub const MSG_SHUTDOWN: u8 = 4;
/// One `AudioShadow` call: batches of elementary stream feeds to be observed.
pub const MSG_OBSERVE_AUDIO_BATCH: u8 = 5;

pub const STATUS_OK: u8 = 0;
pub const STATUS_PROTOCOL_VERSION: u8 = 1;
pub const STATUS_MALFORMED: u8 = 2;
pub const STATUS_UNKNOWN_MESSAGE: u8 = 3;

/// version + type + request id.
pub const HEADER_SIZE: usize = 1 + 1 + 4;

/// The u32 in front of a list in an observe body.
const OBSERVE_COUNT_PREFIX: usize = 4;
/// pid, epoch and the feed count.
const OBSERVE_BATCH_OVERHEAD: usize = 2 + 8 + 4;
/// The u32 length in front of a feed's bytes.
const OBSERVE_FEED_OVERHEAD: usize = 4;
/// pid, epoch, channels, flags, acmod, frames.
const OBSERVE_OBSERVATION_SIZE: usize = 2 + 8 + 1 + 1 + 1 + 8;

/// Observation flags, and the whole of what this build knows how to say.
const OBS_FLAG_LFE: u8 = 1 << 0;
const OBS_FLAG_HAS_ACMOD: u8 = 1 << 1;
const OBS_FLAG_DEPENDENT_SUBSTREAM: u8 = 1 << 2;

/// The largest frame either side will hold. Mirrors the Go constant: a length
/// prefix from a peer that is failing must not become an allocation instruction.
pub const MAX_FRAME_SIZE: usize = 8 * 1024 * 1024;

/// One message.
#[derive(Debug)]
pub struct Frame {
    pub version: u8,
    pub kind: u8,
    pub request_id: u32,
    pub body: Vec<u8>,
}

/// Reads one frame, or reports that the peer stopped talking.
///
/// A clean end of stream is `Ok(None)`: the caller closing the socket is how this
/// process is told to finish, and it is not a failure.
pub fn read_frame(r: &mut impl Read) -> io::Result<Option<Frame>> {
    let mut len_buf = [0u8; 4];
    match r.read_exact(&mut len_buf) {
        Ok(()) => {}
        Err(e) if e.kind() == io::ErrorKind::UnexpectedEof => return Ok(None),
        Err(e) => return Err(e),
    }

    let len = u32::from_be_bytes(len_buf) as usize;
    if len < HEADER_SIZE {
        return Err(io::Error::new(
            io::ErrorKind::InvalidData,
            format!("frame announced {len} bytes, a header is {HEADER_SIZE}"),
        ));
    }
    if len > MAX_FRAME_SIZE {
        return Err(io::Error::new(
            io::ErrorKind::InvalidData,
            format!("frame announced {len} bytes, the limit is {MAX_FRAME_SIZE}"),
        ));
    }

    let mut payload = vec![0u8; len];
    r.read_exact(&mut payload)?;

    Ok(Some(Frame {
        version: payload[0],
        kind: payload[1],
        request_id: u32::from_be_bytes([payload[2], payload[3], payload[4], payload[5]]),
        body: payload[HEADER_SIZE..].to_vec(),
    }))
}

/// Writes one answer.
pub fn write_frame(w: &mut impl Write, kind: u8, request_id: u32, body: &[u8]) -> io::Result<()> {
    let len = HEADER_SIZE + body.len();
    if len > MAX_FRAME_SIZE {
        return Err(io::Error::new(
            io::ErrorKind::InvalidData,
            "answer exceeds the frame limit",
        ));
    }

    // Bounded by the check above, so this cannot lose anything - written as a
    // conversion that says so rather than a cast that assumes it.
    let len_u32 = u32::try_from(len).map_err(|_| {
        io::Error::new(
            io::ErrorKind::InvalidData,
            "answer length does not fit the prefix",
        )
    })?;

    let mut out = Vec::with_capacity(4 + len);
    out.extend_from_slice(&len_u32.to_be_bytes());
    out.push(VERSION);
    out.push(kind);
    out.extend_from_slice(&request_id.to_be_bytes());
    out.extend_from_slice(body);
    w.write_all(&out)?;
    w.flush()
}

/// What a request means.
pub enum Outcome {
    Answer(Vec<u8>),
    Finished(Vec<u8>),
}

/// Everything one connection remembers between requests.
///
/// A shadow's observers are stateful and live for as long as the stream does, so
/// they cannot be rebuilt per request: the answer to the second batch of a stream
/// depends on the first. They belong to the connection, and they end with it -
/// which is also why the Go side gives its shadow a peer of its own rather than
/// sharing one with an authoritative core.
#[derive(Debug, Default)]
pub struct Session {
    audio: Registry,
}

impl Session {
    /// A session that has been asked nothing yet.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    /// Handles one request.
    pub fn handle(&mut self, frame: &Frame) -> Outcome {
        if frame.version != VERSION {
            return Outcome::Answer(vec![STATUS_PROTOCOL_VERSION]);
        }

        match frame.kind {
            // Both carry a program number and nothing else, and neither does anything
            // with it yet; they are one arm because they are one shape, not because
            // the distinction was forgotten.
            MSG_HANDSHAKE | MSG_SET_TARGET_PROGRAM => {
                // Exactly two. A body that is longer carries something this build does
                // not know about, and answering OK to it would claim otherwise.
                if frame.body.len() != 2 {
                    return Outcome::Answer(vec![STATUS_MALFORMED]);
                }
                Outcome::Answer(vec![STATUS_OK])
            }
            MSG_INGEST => {
                if frame.body.len() < 8 {
                    return Outcome::Answer(vec![STATUS_MALFORMED]);
                }
                let start =
                    u64::from_be_bytes(frame.body[..8].try_into().expect("checked length above"));
                let chunk = frame.body.len() - 8;
                // Everything it was given, interpreted by nobody. The offset is what
                // the caller checks, so answering it honestly is the whole job here.
                let through = start.saturating_add(chunk as u64);

                let mut body = Vec::with_capacity(9);
                body.push(STATUS_OK);
                body.extend_from_slice(&through.to_be_bytes());
                Outcome::Answer(body)
            }
            MSG_OBSERVE_AUDIO_BATCH => Outcome::Answer(self.observe_audio(&frame.body)),
            MSG_SHUTDOWN => {
                if !frame.body.is_empty() {
                    return Outcome::Answer(vec![STATUS_MALFORMED]);
                }
                Outcome::Finished(vec![STATUS_OK])
            }
            _ => Outcome::Answer(vec![STATUS_UNKNOWN_MESSAGE]),
        }
    }

    /// Observes one request's batches and lays out the answer.
    ///
    /// One observation per batch, in the order asked, each labelled with the
    /// stream and epoch it is about. Nothing partial is ever returned: a body this
    /// build cannot read in full is refused before a single observer is touched,
    /// because feeding half a request would leave every stream in it at a point
    /// the caller cannot know.
    fn observe_audio(&mut self, body: &[u8]) -> Vec<u8> {
        let Some(batches) = decode_observe_request(body) else {
            return vec![STATUS_MALFORMED];
        };
        // The answer has to fit a frame too. Every batch costs 14 bytes in the
        // request and 21 in the answer, so a request made entirely of empty batches
        // can be framed while its answer cannot. The Go side refuses to send one;
        // this refuses to be the second place that assumes it did.
        let answer_size = 1 + OBSERVE_COUNT_PREFIX + batches.len() * OBSERVE_OBSERVATION_SIZE;
        if answer_size > MAX_FRAME_SIZE - HEADER_SIZE {
            return vec![STATUS_MALFORMED];
        }
        let Ok(count) = u32::try_from(batches.len()) else {
            return vec![STATUS_MALFORMED];
        };

        let mut out = Vec::with_capacity(answer_size);
        out.push(STATUS_OK);
        out.extend_from_slice(&count.to_be_bytes());
        for b in &batches {
            let key = StreamEpoch {
                pid: b.pid,
                epoch: b.epoch,
            };
            let obs = self.audio.observe(key, b.feeds.iter().copied());

            let mut flags = 0u8;
            if obs.lfe {
                flags |= OBS_FLAG_LFE;
            }
            if obs.has_acmod {
                flags |= OBS_FLAG_HAS_ACMOD;
            }
            if obs.dependent_substream {
                flags |= OBS_FLAG_DEPENDENT_SUBSTREAM;
            }

            out.extend_from_slice(&b.pid.to_be_bytes());
            out.extend_from_slice(&b.epoch.to_be_bytes());
            out.push(obs.channels);
            out.push(flags);
            out.push(obs.acmod);
            out.extend_from_slice(&obs.frames.to_be_bytes());
        }
        // Only now. One request may carry the end of one epoch and the start of
        // the next, and the batch for the older one is exactly the one a PID-reuse
        // bug shows up in.
        self.audio.retire_past_epochs();
        out
    }
}

/// One batch as it arrived, borrowing the feeds from the request body.
struct RequestBatch<'a> {
    pid: u16,
    epoch: u64,
    feeds: Vec<&'a [u8]>,
}

/// Reads an observe request, or reports that it is not one.
///
/// `None` for anything that is not exactly the agreed layout: truncated, short of
/// what it announced, or carrying bytes after the last batch. Trailing bytes are
/// not a longer message, they are a different one.
fn decode_observe_request(body: &[u8]) -> Option<Vec<RequestBatch<'_>>> {
    let mut r = Reader::new(body);
    let count = r.u32()?;
    // A count is not an allocation instruction. Every batch costs at least its own
    // overhead, so a body too small to hold that many is failing before anything
    // is reserved for it.
    let max = body.len().checked_sub(OBSERVE_COUNT_PREFIX)? / OBSERVE_BATCH_OVERHEAD;
    let count = usize::try_from(count).ok()?;
    if count > max {
        return None;
    }

    let mut batches = Vec::with_capacity(count);
    for _ in 0..count {
        let pid = r.u16()?;
        let epoch = r.u64()?;
        let feed_count = usize::try_from(r.u32()?).ok()?;
        if feed_count > r.left() / OBSERVE_FEED_OVERHEAD {
            return None;
        }
        let mut feeds = Vec::with_capacity(feed_count);
        for _ in 0..feed_count {
            let len = usize::try_from(r.u32()?).ok()?;
            feeds.push(r.bytes(len)?);
        }
        batches.push(RequestBatch { pid, epoch, feeds });
    }
    if r.left() != 0 {
        return None;
    }
    Some(batches)
}

/// Walks a body without ever reading past it.
struct Reader<'a> {
    b: &'a [u8],
    i: usize,
}

impl<'a> Reader<'a> {
    const fn new(b: &'a [u8]) -> Self {
        Self { b, i: 0 }
    }

    const fn left(&self) -> usize {
        self.b.len() - self.i
    }

    fn bytes(&mut self, n: usize) -> Option<&'a [u8]> {
        let out = self.b.get(self.i..self.i.checked_add(n)?)?;
        self.i += n;
        Some(out)
    }

    fn u16(&mut self) -> Option<u16> {
        Some(u16::from_be_bytes(self.bytes(2)?.try_into().ok()?))
    }

    fn u32(&mut self) -> Option<u32> {
        Some(u32::from_be_bytes(self.bytes(4)?.try_into().ok()?))
    }

    fn u64(&mut self) -> Option<u64> {
        Some(u64::from_be_bytes(self.bytes(8)?.try_into().ok()?))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // The same bytes the Go side asserts. If either edits its encoder, one of the
    // two tests goes red rather than both silently agreeing on something new.
    const GOLDEN_INGEST_ANSWER: &[u8] = &[
        0x00, 0x00, 0x00, 0x0F, // length: header 6 + body 9
        0x02, // version
        0x02, // ingest
        0x00, 0x00, 0x00, 0x07, // request id 7
        0x00, // status ok
        0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x04, 0x2A, // through = 1066
    ];

    #[test]
    fn an_ingest_answer_is_on_the_wire_exactly_as_agreed() {
        let mut body = Vec::new();
        body.push(STATUS_OK);
        body.extend_from_slice(&1066u64.to_be_bytes());

        let mut out = Vec::new();
        write_frame(&mut out, MSG_INGEST, 7, &body).expect("write");
        assert_eq!(out, GOLDEN_INGEST_ANSWER);
    }

    #[test]
    fn ingest_reports_everything_it_was_handed() {
        let mut body = 1000u64.to_be_bytes().to_vec();
        body.extend_from_slice(&[0u8; 66]);
        let f = Frame {
            version: VERSION,
            kind: MSG_INGEST,
            request_id: 1,
            body,
        };

        match Session::new().handle(&f) {
            Outcome::Answer(a) => {
                assert_eq!(a[0], STATUS_OK);
                assert_eq!(u64::from_be_bytes(a[1..9].try_into().unwrap()), 1066);
            }
            Outcome::Finished(_) => panic!("ingest should not finish the session"),
        }
    }

    #[test]
    fn a_version_this_build_does_not_know_is_refused_before_anything_else() {
        let f = Frame {
            version: VERSION + 1,
            kind: MSG_INGEST,
            request_id: 1,
            body: vec![],
        };
        match Session::new().handle(&f) {
            Outcome::Answer(a) => assert_eq!(a[0], STATUS_PROTOCOL_VERSION),
            Outcome::Finished(_) => panic!("a version mismatch is not a clean finish"),
        }
    }

    // The observe request and its answer, byte for byte, as the Go side builds and
    // reads them. One batch, pid 300, epoch 2, two feeds - the second one a single
    // byte, so that a reader which quietly joined the feeds would still have to
    // produce these exact lengths to pass.
    const GOLDEN_OBSERVE_REQUEST: &[u8] = &[
        0x00, 0x00, 0x00, 0x23, // length: header 6 + body 29
        0x02, // version
        0x05, // observe audio batch
        0x00, 0x00, 0x00, 0x09, // request id 9
        0x00, 0x00, 0x00, 0x01, // one batch
        0x01, 0x2C, // pid 300
        0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, // epoch 2
        0x00, 0x00, 0x00, 0x02, // two feeds
        0x00, 0x00, 0x00, 0x02, 0x0B, 0x77, // feed 0
        0x00, 0x00, 0x00, 0x01, 0xAA, // feed 1
    ];

    const GOLDEN_OBSERVE_ANSWER: &[u8] = &[
        0x00, 0x00, 0x00, 0x20, // length: header 6 + body 26
        0x02, // version
        0x05, // observe audio batch
        0x00, 0x00, 0x00, 0x09, // request id 9
        0x00, // status ok
        0x00, 0x00, 0x00, 0x01, // one observation
        0x01, 0x2C, // pid 300
        0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, // epoch 2
        0x06, // channels
        0x03, // flags: lfe | has acmod
        0x07, // acmod
        0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x05, 0xB4, // frames 1460
    ];

    #[test]
    fn an_observe_request_is_read_exactly_as_the_other_side_writes_it() {
        let body = &GOLDEN_OBSERVE_REQUEST[4 + HEADER_SIZE..];
        let batches = decode_observe_request(body).expect("the agreed layout");
        assert_eq!(batches.len(), 1);
        assert_eq!(batches[0].pid, 300);
        assert_eq!(batches[0].epoch, 2);
        assert_eq!(batches[0].feeds, vec![&[0x0Bu8, 0x77][..], &[0xAAu8][..]]);
    }

    #[test]
    fn an_observe_answer_is_on_the_wire_exactly_as_agreed() {
        let mut body = vec![STATUS_OK];
        body.extend_from_slice(&1u32.to_be_bytes());
        body.extend_from_slice(&300u16.to_be_bytes());
        body.extend_from_slice(&2u64.to_be_bytes());
        body.push(6);
        body.push(OBS_FLAG_LFE | OBS_FLAG_HAS_ACMOD);
        body.push(7);
        body.extend_from_slice(&1460u64.to_be_bytes());

        let mut out = Vec::new();
        write_frame(&mut out, MSG_OBSERVE_AUDIO_BATCH, 9, &body).expect("write");
        assert_eq!(out, GOLDEN_OBSERVE_ANSWER);
    }

    /// An AC-3 syncframe of 128 bytes, acmod 2 (stereo), no LFE.
    fn ac3_stereo() -> Vec<u8> {
        let mut f = vec![0u8; 128];
        f[0] = 0x0B;
        f[1] = 0x77;
        f[4] = 0x00;
        f[5] = 8 << 3;
        f[6] = 2 << 5;
        f
    }

    fn observe_request(batches: &[(u16, u64, Vec<Vec<u8>>)]) -> Frame {
        let mut body = Vec::new();
        body.extend_from_slice(&u32::try_from(batches.len()).unwrap().to_be_bytes());
        for (pid, epoch, feeds) in batches {
            body.extend_from_slice(&pid.to_be_bytes());
            body.extend_from_slice(&epoch.to_be_bytes());
            body.extend_from_slice(&u32::try_from(feeds.len()).unwrap().to_be_bytes());
            for f in feeds {
                body.extend_from_slice(&u32::try_from(f.len()).unwrap().to_be_bytes());
                body.extend_from_slice(f);
            }
        }
        Frame {
            version: VERSION,
            kind: MSG_OBSERVE_AUDIO_BATCH,
            request_id: 1,
            body,
        }
    }

    fn answered(out: &[u8]) -> Vec<(u16, u64, u8, u8, u8, u64)> {
        assert_eq!(out[0], STATUS_OK, "status");
        let count = u32::from_be_bytes(out[1..5].try_into().unwrap()) as usize;
        let mut got = Vec::with_capacity(count);
        for i in 0..count {
            let at = 5 + i * OBSERVE_OBSERVATION_SIZE;
            got.push((
                u16::from_be_bytes(out[at..at + 2].try_into().unwrap()),
                u64::from_be_bytes(out[at + 2..at + 10].try_into().unwrap()),
                out[at + 10],
                out[at + 11],
                out[at + 12],
                u64::from_be_bytes(out[at + 13..at + 21].try_into().unwrap()),
            ));
        }
        got
    }

    #[test]
    fn one_observation_per_batch_in_the_order_asked() {
        let audio: Vec<u8> = std::iter::repeat_n(ac3_stereo(), 3).flatten().collect();
        let f = observe_request(&[
            (300, 1, vec![audio.clone()]),
            (301, 1, vec![audio.clone()]),
            (300, 2, vec![audio.clone()]),
        ]);
        let mut s = Session::new();
        let Outcome::Answer(out) = s.handle(&f) else {
            panic!("observe should not finish the session")
        };
        let got = answered(&out);
        assert_eq!(got.len(), 3);
        assert_eq!(
            got.iter().map(|o| (o.0, o.1)).collect::<Vec<_>>(),
            vec![(300, 1), (301, 1), (300, 2)],
            "answers are labelled with the stream and epoch they are about, in order"
        );
        for o in &got {
            assert_eq!(o.5, 3, "three syncframes were fed");
        }
    }

    #[test]
    fn a_stream_carries_its_state_from_one_request_to_the_next() {
        let audio: Vec<u8> = std::iter::repeat_n(ac3_stereo(), 3).flatten().collect();
        let mut s = Session::new();

        let first = observe_request(&[(300, 1, vec![audio.clone()])]);
        let Outcome::Answer(a) = s.handle(&first) else {
            panic!("answer")
        };
        assert_eq!(answered(&a)[0].5, 3);

        let second = observe_request(&[(300, 1, vec![audio])]);
        let Outcome::Answer(b) = s.handle(&second) else {
            panic!("answer")
        };
        assert_eq!(
            answered(&b)[0].5,
            6,
            "the observer is the same one, not a new one per request"
        );
    }

    #[test]
    fn a_malformed_body_is_refused_without_touching_any_observer() {
        let audio: Vec<u8> = std::iter::repeat_n(ac3_stereo(), 3).flatten().collect();
        let mut session = Session::new();

        // Announces two batches, carries one.
        let mut short_count = observe_request(&[(300, 1, vec![audio.clone()])]);
        short_count.body[0..4].copy_from_slice(&2u32.to_be_bytes());
        let Outcome::Answer(refused_count) = session.handle(&short_count) else {
            panic!("answer")
        };
        assert_eq!(refused_count, vec![STATUS_MALFORMED]);

        // Trailing bytes after the last batch.
        let mut trailing = observe_request(&[(300, 1, vec![audio.clone()])]);
        trailing.body.push(0x00);
        let Outcome::Answer(refused_trailing) = session.handle(&trailing) else {
            panic!("answer")
        };
        assert_eq!(refused_trailing, vec![STATUS_MALFORMED]);

        // A feed longer than the body that carries it.
        let mut oversized = observe_request(&[(300, 1, vec![audio.clone()])]);
        let at = 4 + OBSERVE_BATCH_OVERHEAD;
        oversized.body[at..at + 4].copy_from_slice(&u32::MAX.to_be_bytes());
        let Outcome::Answer(refused_feed) = session.handle(&oversized) else {
            panic!("answer")
        };
        assert_eq!(refused_feed, vec![STATUS_MALFORMED]);

        // And none of it moved the session on: the stream still starts from zero.
        let good = observe_request(&[(300, 1, vec![audio])]);
        let Outcome::Answer(accepted) = session.handle(&good) else {
            panic!("answer")
        };
        assert_eq!(answered(&accepted)[0].5, 3);
    }

    #[test]
    fn a_request_whose_answer_could_not_be_framed_is_refused() {
        // Empty batches: 14 bytes each to ask, 21 each to answer. Enough of them
        // and the request fits while the answer does not.
        let count =
            (MAX_FRAME_SIZE - HEADER_SIZE - OBSERVE_COUNT_PREFIX) / OBSERVE_OBSERVATION_SIZE + 1;
        let mut body = Vec::with_capacity(OBSERVE_COUNT_PREFIX + count * OBSERVE_BATCH_OVERHEAD);
        body.extend_from_slice(&u32::try_from(count).unwrap().to_be_bytes());
        for _ in 0..count {
            body.extend_from_slice(&0u16.to_be_bytes());
            body.extend_from_slice(&0u64.to_be_bytes());
            body.extend_from_slice(&0u32.to_be_bytes());
        }
        assert!(
            HEADER_SIZE + body.len() <= MAX_FRAME_SIZE,
            "the request itself has to fit, or this proves nothing"
        );

        let f = Frame {
            version: VERSION,
            kind: MSG_OBSERVE_AUDIO_BATCH,
            request_id: 1,
            body,
        };
        let Outcome::Answer(a) = Session::new().handle(&f) else {
            panic!("answer")
        };
        assert_eq!(a, vec![STATUS_MALFORMED]);
    }

    #[test]
    fn a_frame_that_promises_more_than_the_limit_is_refused_without_allocating() {
        let mut input = Vec::new();
        let too_big = u32::try_from(MAX_FRAME_SIZE + 1).expect("fits a u32");
        input.extend_from_slice(&too_big.to_be_bytes());
        let err = read_frame(&mut input.as_slice()).expect_err("must refuse");
        assert_eq!(err.kind(), io::ErrorKind::InvalidData);
    }

    #[test]
    fn a_closed_stream_is_a_clean_end_not_a_failure() {
        let empty: &[u8] = &[];
        assert!(read_frame(&mut { empty }).expect("clean eof").is_none());
    }

    #[test]
    fn a_frame_cut_short_is_a_failure() {
        // Announces a full header, delivers two bytes of it.
        let input = vec![0x00, 0x00, 0x00, 0x06, 0x01, 0x02];
        assert!(read_frame(&mut input.as_slice()).is_err());
    }
}
