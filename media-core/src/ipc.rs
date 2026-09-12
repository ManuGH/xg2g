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
//! What arrives here is interpreted. An ingest request carries raw transport
//! stream bytes and is answered by reading them: the PSI parser this crate owns
//! is handed the chunk, and what it made of it is what goes back. An observe
//! request is answered by actually observing the audio it carries.
//!
//! This module is transport and nothing else. It decodes a request, calls a
//! parser, and encodes the answer - it does not read a table itself, because a
//! second PSI implementation living in the wire code is precisely the thing this
//! whole migration exists to avoid.
//!
//! What it answers about is narrower than what the Go core answers about: PSI,
//! and nothing else. That is said out loud in every result, because the caller
//! cannot tell "no entry point was seen" from "nobody looked" by reading a zero.

use std::io::{self, Read, Write};

use xg2g_media_core::audio::shadow::{Registry, StreamEpoch};
use xg2g_media_core::psi::{
    ActivePsi, Outcome as PsiOutcome, PsiCore, PsiEvent, PsiFacts, VideoCodec,
};

/// The protocol version this build speaks. Checked once, fatal when it differs.
///
/// 2 adds [`MSG_OBSERVE_AUDIO_BATCH`]. There is no negotiation and the message
/// set is closed, so the version is the only thing that says "this peer can be
/// asked what this build wants to ask". A core still answering 1 would refuse an
/// observe request as an unknown message - a round trip later, and looking like a
/// rejected batch rather than a peer that cannot do this at all.
///
/// 3 makes ingest and set-target answer with what was read rather than with an
/// offset alone, and makes the handshake's programme number the core's initial
/// target. A v2 peer answers the old short body, which a v3 caller would read as
/// a coverage and an offset that are not there - so this is exactly what the
/// version is for. There is no shim.
pub const VERSION: u8 = 3;

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

/// What a result covers. Mirrors `mediafacts.ParseCoverage`.
///
/// This core reads PSI, so every result it produces says so. A caller that
/// commits stream truth requires complete coverage and must refuse this - which
/// is the point: the absence of the fields this core does not fill has to be a
/// statement, because their zero values are all legitimate answers.
const COVERAGE_PSI_ONLY: u8 = 1;

/// Event kinds on the wire. A random access point is a statement about video
/// payload, which this coverage does not include, so there is exactly one.
const EVENT_PROGRAM_IDENTITY_CHANGED: u8 = 1;

const FACT_HAS_PAT: u8 = 1 << 0;
const FACT_HAS_PMT: u8 = 1 << 1;

const TRACK_MULTICHANNEL: u8 = 1 << 0;
const TRACK_HAS_COMPONENT_TYPE: u8 = 1 << 1;

/// Closed sets travel as numbers, not as the strings this crate happens to use.
/// A spelling on the wire is a spelling two implementations can disagree about.
const VIDEO_CODEC_UNKNOWN: u8 = 0;
const VIDEO_CODEC_H264: u8 = 1;
const VIDEO_CODEC_H265: u8 = 2;
const VIDEO_CODEC_MPEG2: u8 = 3;

const AUDIO_CODEC_UNKNOWN: u8 = 0;
const AUDIO_CODEC_MP2: u8 = 1;
const AUDIO_CODEC_AAC: u8 = 2;
const AUDIO_CODEC_AC3: u8 = 3;
const AUDIO_CODEC_EAC3: u8 = 4;
const AUDIO_CODEC_DTS: u8 = 5;

/// A language is always three bytes: the descriptor's three, or those of `und`.
const LANGUAGE_LEN: usize = 3;

/// The fixed part of a result: status, coverage, offset, event count, facts
/// flags, PMT version, programme number, PMT PID, video PID, video codec, the
/// two audio counts and the two section counts.
const RESULT_FIXED_SIZE: usize = 1 + 1 + 8 + 4 + 1 + 1 + 2 + 2 + 2 + 1 + 4 + 4 + 2 + 2;
const EVENT_SIZE: usize = 1 + 8 + 1;
const AUDIO_PID_SIZE: usize = 2;
const AUDIO_TRACK_SIZE: usize = 2 + 1 + 1 + LANGUAGE_LEN + 1 + 1 + 1;
const SECTION_PREFIX: usize = 2;

/// Transport packets are 188 bytes and a chunk is whole packets. Checked here as
/// well as by the caller: this side cannot trust the caller either.
const TS_PACKET_LEN: usize = 188;

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
    /// The PSI parser for this connection, established by the handshake.
    ///
    /// One connection is one stream, so one PSI lifecycle: the answer to the
    /// second chunk depends on the first, and a core rebuilt per request would
    /// have no table in force to answer with. Absent until the handshake, so an
    /// ingest before one is a protocol error rather than a core invented on the
    /// spot with a target nobody chose.
    psi: Option<PsiCore>,
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
            // The handshake establishes the initial target; set-target changes it
            // later. They carry the same two bytes and mean different things, which
            // is why they are no longer one arm: establishing state produces no
            // event, and changing it does.
            MSG_HANDSHAKE => {
                // Exactly two. A body that is longer carries something this build does
                // not know about, and answering OK to it would claim otherwise.
                let Some(target) = two_byte_program(&frame.body) else {
                    return Outcome::Answer(vec![STATUS_MALFORMED]);
                };
                // Built with the target rather than built at zero and then told:
                // the second is a set-target call, and a set-target call from
                // nothing to the programme already being followed is a change the
                // stream never made. The handshake answers a status alone, so
                // there is nowhere for such an event to go even if it existed.
                self.psi = Some(PsiCore::new(target));
                Outcome::Answer(vec![STATUS_OK])
            }
            MSG_SET_TARGET_PROGRAM => {
                let Some(target) = two_byte_program(&frame.body) else {
                    return Outcome::Answer(vec![STATUS_MALFORMED]);
                };
                let Some(psi) = self.psi.as_mut() else {
                    return Outcome::Answer(vec![STATUS_MALFORMED]);
                };
                let outcome = psi.set_target_program(target);
                Outcome::Answer(encode_psi_result(&outcome))
            }
            MSG_INGEST => {
                if frame.body.len() < 8 {
                    return Outcome::Answer(vec![STATUS_MALFORMED]);
                }
                let start =
                    u64::from_be_bytes(frame.body[..8].try_into().expect("checked length above"));
                let chunk = &frame.body[8..];
                // A chunk that is not whole packets is not a chunk. The caller
                // refuses it too; this side refuses it because it cannot assume
                // the caller did.
                if !chunk.len().is_multiple_of(TS_PACKET_LEN) {
                    return Outcome::Answer(vec![STATUS_MALFORMED]);
                }
                let Ok(start) = i64::try_from(start) else {
                    return Outcome::Answer(vec![STATUS_MALFORMED]);
                };
                let Some(psi) = self.psi.as_mut() else {
                    return Outcome::Answer(vec![STATUS_MALFORMED]);
                };
                let Ok(outcome) = psi.ingest(start, chunk) else {
                    // The parser refuses what it cannot interpret. Its refusal is
                    // this answer's refusal; nothing is invented in between.
                    return Outcome::Answer(vec![STATUS_MALFORMED]);
                };
                Outcome::Answer(encode_psi_result(&outcome))
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

/// Reads the two bytes a handshake or set-target request carries.
///
/// Exactly two. A longer body carries something this build does not know about,
/// and answering OK to it would claim otherwise.
fn two_byte_program(body: &[u8]) -> Option<u16> {
    if body.len() != 2 {
        return None;
    }
    Some(u16::from_be_bytes([body[0], body[1]]))
}

/// The size the answer for this outcome will encode to.
///
/// Computed before anything is built, in checked arithmetic. A response that
/// cannot fit the frame must fail rather than be discovered half-written: the
/// alternative is allocating megabytes to find out they were not wanted, which
/// is the same mistake as trusting a length prefix, made from the other side.
fn psi_result_size(outcome: &PsiOutcome) -> Option<usize> {
    let mut size = RESULT_FIXED_SIZE;
    size = size.checked_add(outcome.events.len().checked_mul(EVENT_SIZE)?)?;
    size = size.checked_add(outcome.facts.audio_pids.len().checked_mul(AUDIO_PID_SIZE)?)?;
    size = size.checked_add(
        outcome
            .facts
            .audio_tracks
            .len()
            .checked_mul(AUDIO_TRACK_SIZE)?,
    )?;
    for table in [&outcome.active.pat_sections, &outcome.active.pmt_sections] {
        for section in table {
            size = size
                .checked_add(SECTION_PREFIX)?
                .checked_add(section.len())?;
        }
    }
    Some(size)
}

/// Lays out one result. See the layout comment in the Go `psiresult.go`.
///
/// The two answers that carry a result use this one function, because they carry
/// the same thing: what the core knows now. A second layout would be a second
/// place for the two implementations to drift.
fn encode_psi_result(outcome: &PsiOutcome) -> Vec<u8> {
    let Some(size) = psi_result_size(outcome) else {
        return vec![STATUS_MALFORMED];
    };
    if size > MAX_FRAME_SIZE - HEADER_SIZE {
        // Fail closed rather than raise the ceiling. With PSI held to the bounds
        // the syntax gives - 256 sections of at most 1024 bytes per table - a
        // real answer is a few hundred kilobytes at its worst, so reaching this
        // means something upstream is not what it claims to be.
        return vec![STATUS_MALFORMED];
    }

    let mut body = Vec::with_capacity(size);
    body.push(STATUS_OK);
    body.push(COVERAGE_PSI_ONLY);
    #[allow(clippy::cast_sign_loss)] // an offset is never negative; the caller checks it too
    body.extend_from_slice(&(outcome.processed_through as u64).to_be_bytes());

    encode_events(&mut body, &outcome.events);
    if encode_facts(&mut body, &outcome.facts).is_none() {
        // A declaration this protocol has no way to say. Refused rather than
        // sent as the nearest thing that fits: see wire_audio_codec.
        return vec![STATUS_MALFORMED];
    }
    encode_active_psi(&mut body, &outcome.active);

    debug_assert_eq!(
        body.len(),
        size,
        "the preflight size and the encoding disagree"
    );
    body
}

fn encode_events(body: &mut Vec<u8>, events: &[PsiEvent]) {
    body.extend_from_slice(&count32(events.len()).to_be_bytes());
    for event in events {
        match event {
            PsiEvent::ProgramIdentityChanged => {
                body.push(EVENT_PROGRAM_IDENTITY_CHANGED);
                // Offset and joinable are the Go event's shape. A PSI event
                // carries neither, and says so as zeroes rather than by being a
                // different size from the events a later step will add.
                body.extend_from_slice(&0u64.to_be_bytes());
                body.push(0);
            }
        }
    }
}

/// The wire number for a codec this protocol knows, or nothing.
///
/// `unknown` is one of the values it knows: the parser says it when a stream is
/// audio but its codec cannot be named, and that is an answer. What has no
/// number here is a codec this build has never heard of - a spelling from a
/// newer parser, or an empty string from one that failed.
///
/// Those must not become `unknown` on the way out. `unknown` already means
/// something, and answering it for a codec the parser did name would turn a
/// value this protocol cannot carry into a different, legitimate one - and the
/// reader would have no way to tell. The encoding fails instead, so the day a
/// codec is added to the parser and not to the wire is the day this refuses to
/// answer rather than the day it starts lying.
fn wire_audio_codec(codec: &str) -> Option<u8> {
    match codec {
        "unknown" => Some(AUDIO_CODEC_UNKNOWN),
        "mp2" => Some(AUDIO_CODEC_MP2),
        "aac" => Some(AUDIO_CODEC_AAC),
        "ac3" => Some(AUDIO_CODEC_AC3),
        "eac3" => Some(AUDIO_CODEC_EAC3),
        "dts" => Some(AUDIO_CODEC_DTS),
        _ => None,
    }
}

/// Lays out the facts, or reports that they cannot be said on this protocol.
fn encode_facts(body: &mut Vec<u8>, facts: &PsiFacts) -> Option<()> {
    let mut flags = 0u8;
    if facts.has_pat {
        flags |= FACT_HAS_PAT;
    }
    if facts.has_pmt {
        flags |= FACT_HAS_PMT;
    }
    body.push(flags);
    body.push(facts.pmt_version);
    body.extend_from_slice(&facts.program_number.to_be_bytes());
    body.extend_from_slice(&facts.pmt_pid.to_be_bytes());
    body.extend_from_slice(&facts.video_pid.to_be_bytes());
    body.push(match facts.video_codec {
        VideoCodec::Unknown => VIDEO_CODEC_UNKNOWN,
        VideoCodec::H264 => VIDEO_CODEC_H264,
        VideoCodec::H265 => VIDEO_CODEC_H265,
        VideoCodec::Mpeg2 => VIDEO_CODEC_MPEG2,
    });

    body.extend_from_slice(&count32(facts.audio_pids.len()).to_be_bytes());
    for pid in &facts.audio_pids {
        body.extend_from_slice(&pid.to_be_bytes());
    }

    body.extend_from_slice(&count32(facts.audio_tracks.len()).to_be_bytes());
    for track in &facts.audio_tracks {
        body.extend_from_slice(&track.pid.to_be_bytes());
        body.push(track.stream_type);
        body.push(wire_audio_codec(&track.codec)?);
        // Exactly three bytes, or nothing. The parser produces the descriptor's
        // three or those of `und`, so any other length is a parser this build
        // does not match.
        //
        // Padding it to `und` would be the same mistake as mapping an unknown
        // codec to `unknown`: `und` is a legitimate answer, meaning the table
        // declared no language, and a reader given it cannot tell that from a
        // language the encoder could not represent.
        let language: [u8; LANGUAGE_LEN] = track.language.as_bytes().try_into().ok()?;
        body.extend_from_slice(&language);
        body.push(track.declared.channels);
        let mut track_flags = 0u8;
        if track.declared.multichannel {
            track_flags |= TRACK_MULTICHANNEL;
        }
        if track.declared.has_component_type {
            track_flags |= TRACK_HAS_COMPONENT_TYPE;
        }
        body.push(track_flags);
        body.push(track.declared.component_type);
    }
    Some(())
}

fn encode_active_psi(body: &mut Vec<u8>, active: &ActivePsi) {
    for table in [&active.pat_sections, &active.pmt_sections] {
        // A table has at most 256 sections, so the count is a u16 - a u8 cannot
        // hold 256, and the one value it cannot hold is the legal maximum.
        body.extend_from_slice(&count16(table.len()).to_be_bytes());
        for section in table {
            body.extend_from_slice(&count16(section.len()).to_be_bytes());
            body.extend_from_slice(section);
        }
    }
}

/// Narrows a length that the preflight has already proved fits a frame.
///
/// Saturating rather than truncating: a value past what the field holds is a
/// bug, and a truncation would encode a small number that the reader believes.
fn count32(n: usize) -> u32 {
    u32::try_from(n).unwrap_or(u32::MAX)
}

fn count16(n: usize) -> u16 {
    u16::try_from(n).unwrap_or(u16::MAX)
}

#[cfg(test)]
mod tests {
    use super::*;

    /// An empty PSI result: nothing read, nothing in force.
    ///
    /// The same bytes the Go side asserts. If either edits its encoder, one of
    /// the two tests goes red rather than both silently agreeing on something
    /// new - which is the only thing keeping two implementations of one format
    /// from drifting apart.
    const GOLDEN_EMPTY_RESULT: &[u8] = &[
        0x00, // status ok
        0x01, // coverage: PSI only
        0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x04, 0x2A, // through = 1066
        0x00, 0x00, 0x00, 0x00, // no events
        0x00, // no PAT, no PMT
        0x00, // PMT version 0
        0x00, 0x00, // programme 0
        0x00, 0x00, // PMT PID 0
        0x00, 0x00, // video PID 0
        0x00, // video codec unknown
        0x00, 0x00, 0x00, 0x00, // no audio PIDs
        0x00, 0x00, 0x00, 0x00, // no audio tracks
        0x00, 0x00, // no PAT sections
        0x00, 0x00, // no PMT sections
    ];

    #[test]
    fn an_empty_result_is_on_the_wire_exactly_as_agreed() {
        let outcome = PsiOutcome {
            processed_through: 1066,
            events: Vec::new(),
            facts: PsiFacts::default(),
            active: ActivePsi::default(),
        };
        assert_eq!(encode_psi_result(&outcome), GOLDEN_EMPTY_RESULT);
    }

    /// A result with everything in it: an event, both tables in force, a video
    /// stream and one audio track with a full declaration.
    const GOLDEN_FULL_RESULT: &[u8] = &[
        0x00, // status ok
        0x01, // coverage: PSI only
        0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xBC, // through = 188
        0x00, 0x00, 0x00, 0x01, // one event
        0x01, // programme identity changed
        0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // offset 0
        0x00, // not joinable
        0x03, // has PAT and PMT
        0x05, // PMT version 5
        0x00, 0x01, // programme 1
        0x01, 0x00, // PMT PID 256
        0x01, 0x01, // video PID 257
        0x01, // H.264
        0x00, 0x00, 0x00, 0x01, // one audio PID
        0x01, 0x02, // PID 258
        0x00, 0x00, 0x00, 0x01, // one audio track
        0x01, 0x02, // PID 258
        0x06, // stream type 0x06
        0x03, // ac3
        b'd', b'e', b'u', // language
        0x02, // two channels
        0x02, // has a component type, not multichannel
        0x04, // component type 4
        0x00, 0x01, // one PAT section
        0x00, 0x04, // four bytes
        0x00, 0xB0, 0x0D, 0x99, //
        0x00, 0x01, // one PMT section
        0x00, 0x03, // three bytes
        0x02, 0xB0, 0x21, //
    ];

    #[test]
    fn a_full_result_is_on_the_wire_exactly_as_agreed() {
        let outcome = PsiOutcome {
            processed_through: 188,
            events: vec![PsiEvent::ProgramIdentityChanged],
            facts: PsiFacts {
                has_pat: true,
                has_pmt: true,
                pmt_version: 5,
                program_number: 1,
                pmt_pid: 256,
                video_pid: 257,
                video_codec: VideoCodec::H264,
                audio_pids: vec![258],
                audio_tracks: vec![xg2g_media_core::psi::AudioTrack {
                    pid: 258,
                    stream_type: 0x06,
                    codec: "ac3".to_string(),
                    language: "deu".to_string(),
                    declared: xg2g_media_core::psi::ChannelDeclaration {
                        channels: 2,
                        multichannel: false,
                        component_type: 4,
                        has_component_type: true,
                    },
                }],
            },
            active: ActivePsi {
                pat_sections: vec![vec![0x00, 0xB0, 0x0D, 0x99]],
                pmt_sections: vec![vec![0x02, 0xB0, 0x21]],
            },
        };
        assert_eq!(encode_psi_result(&outcome), GOLDEN_FULL_RESULT);
    }

    /// A chunk that is not whole transport packets is not a chunk. The caller
    /// refuses it too; this side refuses it because it cannot assume the caller
    /// did.
    /// A declaration this protocol cannot say is refused, not rounded to the
    /// nearest thing that fits.
    ///
    /// Both of these normalise into values that already mean something else:
    /// `unknown` is what the parser says for audio whose codec it cannot name,
    /// and `und` is what it says when the table declared no language. Answering
    /// either for a value the encoder could not represent would hand the reader
    /// a legitimate answer it has no way to distrust - and the day a codec is
    /// added to the parser and not to the wire, that is a silent wrong answer
    /// rather than a loud refusal.
    #[test]
    fn a_declaration_this_protocol_cannot_say_is_refused() {
        let track = |codec: &str, language: &str| PsiOutcome {
            processed_through: 188,
            events: Vec::new(),
            facts: PsiFacts {
                audio_pids: vec![258],
                audio_tracks: vec![xg2g_media_core::psi::AudioTrack {
                    pid: 258,
                    stream_type: 0x06,
                    codec: codec.to_string(),
                    language: language.to_string(),
                    declared: xg2g_media_core::psi::ChannelDeclaration::default(),
                }],
                ..PsiFacts::default()
            },
            active: ActivePsi::default(),
        };

        // The values the parser actually produces are all sayable, including
        // the two that mean "nothing was declared".
        for codec in ["unknown", "mp2", "aac", "ac3", "eac3", "dts"] {
            let answer = encode_psi_result(&track(codec, "und"));
            assert_eq!(
                answer[0], STATUS_OK,
                "codec {codec} with language und was refused"
            );
        }
        for language in ["und", "deu", "eng"] {
            let answer = encode_psi_result(&track("ac3", language));
            assert_eq!(answer[0], STATUS_OK, "language {language} was refused");
        }

        // And nothing else is.
        for (codec, language, what) in [
            ("opus", "und", "a codec this build has never heard of"),
            ("", "und", "no codec at all"),
            ("AC3", "und", "a codec spelled differently"),
            ("ac3", "de", "a language of two bytes"),
            ("ac3", "deutsch", "a language of seven bytes"),
            ("ac3", "", "no language at all"),
        ] {
            let answer = encode_psi_result(&track(codec, language));
            assert_eq!(
                answer,
                vec![STATUS_MALFORMED],
                "{what} was answered instead of refused"
            );
        }
    }

    #[test]
    fn a_chunk_that_is_not_whole_packets_is_refused() {
        let mut session = handshaken(1);
        let mut body = 1000u64.to_be_bytes().to_vec();
        body.extend_from_slice(&[0u8; 66]);
        match session.handle(&ingest_frame(body)) {
            Outcome::Answer(a) => assert_eq!(a[0], STATUS_MALFORMED),
            Outcome::Finished(_) => panic!("ingest should not finish the session"),
        }
    }

    /// An ingest before a handshake has no core to reach. Refused rather than
    /// answered by a core invented on the spot with a target nobody chose.
    #[test]
    fn an_ingest_before_a_handshake_is_refused() {
        let body = 0u64.to_be_bytes().to_vec();
        match Session::new().handle(&ingest_frame(body)) {
            Outcome::Answer(a) => assert_eq!(a[0], STATUS_MALFORMED),
            Outcome::Finished(_) => panic!("ingest should not finish the session"),
        }
    }

    /// The handshake establishes the target, and the core that follows it reads
    /// the programme that target names.
    #[test]
    fn the_handshake_target_is_the_core_target() {
        let mut session = handshaken(2);
        let mut body = 0u64.to_be_bytes().to_vec();
        body.extend_from_slice(&two_programme_pat_packet());

        let Outcome::Answer(answer) = session.handle(&ingest_frame(body)) else {
            panic!("ingest should not finish the session");
        };
        assert_eq!(answer[0], STATUS_OK);
        assert_eq!(
            pmt_pid_of(&answer),
            0x0200,
            "the handshake target was not followed"
        );
    }

    /// Reads the PMT PID out of an answer.
    ///
    /// Counted rather than indexed at a literal: the facts sit after the events,
    /// so where they begin depends on how many there were. A test that hard-coded
    /// an offset would pass or fail for reasons that have nothing to do with the
    /// field it names.
    fn pmt_pid_of(answer: &[u8]) -> u16 {
        let event_count =
            u32::from_be_bytes(answer[10..14].try_into().expect("event count")) as usize;
        let facts = 14 + event_count * EVENT_SIZE;
        let pmt_pid = facts + 1 + 1 + 2; // flags, version, programme number
        u16::from_be_bytes([answer[pmt_pid], answer[pmt_pid + 1]])
    }

    fn handshaken(target: u16) -> Session {
        let mut session = Session::new();
        let f = Frame {
            version: VERSION,
            kind: MSG_HANDSHAKE,
            request_id: 1,
            body: target.to_be_bytes().to_vec(),
        };
        match session.handle(&f) {
            Outcome::Answer(a) => assert_eq!(a[0], STATUS_OK),
            Outcome::Finished(_) => panic!("a handshake should not finish the session"),
        }
        session
    }

    fn ingest_frame(body: Vec<u8>) -> Frame {
        Frame {
            version: VERSION,
            kind: MSG_INGEST,
            request_id: 1,
            body,
        }
    }

    /// One packet carrying a PAT that names programme 1 on PID 0x0100 and
    /// programme 2 on PID 0x0200.
    fn two_programme_pat_packet() -> Vec<u8> {
        // The CRC is written out rather than computed: the parser this crate
        // owns is what the test is about, and a fixture that computed its own
        // CRC with the same code would agree with itself. A wrong one here is
        // caught immediately - the section would be refused and the assertion
        // below would find PMT PID 0.
        let section = [
            0x00, 0xB0, 0x11, 0x00, 0x01, 0xC1, 0x00, 0x00, // header
            0x00, 0x01, 0xE1, 0x00, // programme 1 -> 0x0100
            0x00, 0x02, 0xE2, 0x00, // programme 2 -> 0x0200
            0x39, 0x89, 0xA5, 0xA9, // CRC-32/MPEG-2
        ];

        let mut packet = vec![0x47, 0x40, 0x00, 0x10, 0x00];
        packet.extend_from_slice(&section);
        packet.resize(TS_PACKET_LEN, 0xFF);
        packet
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
        0x03, // version
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
        0x03, // version
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
