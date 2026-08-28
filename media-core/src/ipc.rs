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
//! Nothing here interprets media. The answers are derived from the request and
//! from nothing else.

use std::io::{self, Read, Write};

/// The protocol version this build speaks. Checked once, fatal when it differs.
pub const VERSION: u8 = 1;

pub const MSG_HANDSHAKE: u8 = 1;
pub const MSG_INGEST: u8 = 2;
pub const MSG_SET_TARGET_PROGRAM: u8 = 3;
pub const MSG_SHUTDOWN: u8 = 4;

pub const STATUS_OK: u8 = 0;
pub const STATUS_PROTOCOL_VERSION: u8 = 1;
pub const STATUS_MALFORMED: u8 = 2;
pub const STATUS_UNKNOWN_MESSAGE: u8 = 3;

/// version + type + request id.
pub const HEADER_SIZE: usize = 1 + 1 + 4;

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

/// What a request means. Nothing here reads media; the answers follow from the
/// request alone, which is the whole of what this step claims.
pub enum Outcome {
    Answer(Vec<u8>),
    Finished(Vec<u8>),
}

/// Handles one request.
pub fn handle(frame: &Frame) -> Outcome {
    if frame.version != VERSION {
        return Outcome::Answer(vec![STATUS_PROTOCOL_VERSION]);
    }

    match frame.kind {
        // Both carry a program number and nothing else, and neither does anything
        // with it yet; they are one arm because they are one shape, not because
        // the distinction was forgotten.
        MSG_HANDSHAKE | MSG_SET_TARGET_PROGRAM => {
            if frame.body.len() < 2 {
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
        MSG_SHUTDOWN => Outcome::Finished(vec![STATUS_OK]),
        _ => Outcome::Answer(vec![STATUS_UNKNOWN_MESSAGE]),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // The same bytes the Go side asserts. If either edits its encoder, one of the
    // two tests goes red rather than both silently agreeing on something new.
    const GOLDEN_INGEST_ANSWER: &[u8] = &[
        0x00, 0x00, 0x00, 0x0F, // length: header 6 + body 9
        0x01, // version
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

        match handle(&f) {
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
        match handle(&f) {
            Outcome::Answer(a) => assert_eq!(a[0], STATUS_PROTOCOL_VERSION),
            Outcome::Finished(_) => panic!("a version mismatch is not a clean finish"),
        }
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
