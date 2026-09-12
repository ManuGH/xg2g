// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! What the composition decides, tested where the shared corpus cannot see it.
//!
//! The corpus proves that this and the Go reference route the same bytes to the
//! same observers. These are the questions that have no reference answer: the
//! rules this layer had to author because the reference has no state to hold
//! them in, and the state it keeps between packets.

use super::{AudioIngress, Position};
use crate::audio::observer::Observation;
use crate::psi::IngestError;

// --- fixtures --------------------------------------------------------------

const AUDIO_PID: u16 = 0x0100;
const PMT_PID: u16 = 0x1000;
const PROGRAM: u16 = 1;

/// The MPEG-2 CRC, bit by bit. Deliberately not the parser's own: a section
/// whose CRC came from the function that validates it agrees with that function
/// even when both are wrong.
fn crc32(data: &[u8]) -> u32 {
    let mut crc: u32 = 0xFFFF_FFFF;
    for &b in data {
        for bit in (0..8).rev() {
            let top = (crc >> 31) & 1;
            let input = u32::from((b >> bit) & 1);
            crc <<= 1;
            if top ^ input == 1 {
                crc ^= 0x04C1_1DB7;
            }
        }
    }
    crc
}

fn seal(mut body: Vec<u8>) -> Vec<u8> {
    let crc = crc32(&body);
    body.extend_from_slice(&crc.to_be_bytes());
    body
}

/// A one-section table: header, payload, CRC.
fn section(table_id: u8, id_extension: u16, version: u8, payload: &[u8]) -> Vec<u8> {
    let section_len = 9 + payload.len(); // through CRC
    let mut body = vec![
        table_id,
        0xB0 | u8::try_from((section_len >> 8) & 0x0F).unwrap(),
        u8::try_from(section_len & 0xFF).unwrap(),
        u8::try_from(id_extension >> 8).unwrap(),
        u8::try_from(id_extension & 0xFF).unwrap(),
        0xC1 | (version << 1),
        0x00,
        0x00,
    ];
    body.extend_from_slice(payload);
    seal(body)
}

fn pat(version: u8) -> Vec<u8> {
    section(
        0x00,
        1,
        version,
        &[
            u8::try_from(PROGRAM >> 8).unwrap(),
            u8::try_from(PROGRAM & 0xFF).unwrap(),
            0xE0 | u8::try_from(PMT_PID >> 8).unwrap(),
            u8::try_from(PMT_PID & 0xFF).unwrap(),
        ],
    )
}

/// A PMT declaring one elementary stream.
fn pmt(version: u8, pid: u16, stream_type: u8, descriptors: &[u8]) -> Vec<u8> {
    let mut payload = vec![
        0xE0 | u8::try_from(pid >> 8).unwrap(),
        u8::try_from(pid & 0xFF).unwrap(),
        0xF0,
        0x00, // program_info_length
        stream_type,
        0xE0 | u8::try_from(pid >> 8).unwrap(),
        u8::try_from(pid & 0xFF).unwrap(),
        0xF0,
        u8::try_from(descriptors.len()).unwrap(),
    ];
    payload.extend_from_slice(descriptors);
    section(0x02, PROGRAM, version, &payload)
}

/// The DVB AC-3 descriptor, which is how AC-3 on stream type 0x06 says so.
const AC3_DESCRIPTOR: [u8; 2] = [0x6A, 0x00];
/// The DVB enhanced AC-3 descriptor.
const EAC3_DESCRIPTOR: [u8; 2] = [0x7A, 0x00];

fn ts_packet(pid: u16, pusi: bool, cc: u8, payload: &[u8]) -> Vec<u8> {
    assert!(payload.len() <= 184, "payload does not fit one packet");
    let mut p = vec![0xFF; 188];
    p[0] = 0x47;
    p[1] = u8::try_from((pid >> 8) & 0x1F).unwrap();
    if pusi {
        p[1] |= 0x40;
    }
    p[2] = u8::try_from(pid & 0xFF).unwrap();
    p[3] = 0x10 | (cc & 0x0F);
    p[4..4 + payload.len()].copy_from_slice(payload);
    p
}

/// One PSI packet: pointer field, then the section.
fn psi_packet(pid: u16, cc: u8, sect: &[u8]) -> Vec<u8> {
    let mut payload = vec![0x00];
    payload.extend_from_slice(sect);
    ts_packet(pid, true, cc, &payload)
}

/// One 128-byte AC-3 syncframe: 48 kHz, smallest frame, bsid 8.
fn ac3_frame(byte6: u8) -> Vec<u8> {
    let mut f = vec![0u8; 128];
    f[0] = 0x0B;
    f[1] = 0x77;
    f[4] = 0x00;
    f[5] = 8 << 3;
    f[6] = byte6;
    f
}

fn ac3_run(byte6: u8, frames: usize) -> Vec<u8> {
    let mut out = Vec::new();
    for _ in 0..frames {
        out.extend_from_slice(&ac3_frame(byte6));
    }
    out
}

const STEREO: u8 = 0x40;
const SURROUND: u8 = 0xEB;

/// A PES header with an optional part of `header_data_length` bytes.
fn pes_header(stream_id: u8, header_data_length: u8) -> Vec<u8> {
    let mut h = vec![
        0x00,
        0x00,
        0x01,
        stream_id,
        0x00,
        0x00,
        0x80,
        0x00,
        header_data_length,
    ];
    h.extend(std::iter::repeat_n(0x00, header_data_length as usize));
    h
}

/// The tables that put an observable AC-3 stream on [`AUDIO_PID`].
fn program(version: u8, descriptors: &[u8]) -> Vec<u8> {
    let mut out = psi_packet(0, 0, &pat(0));
    out.extend_from_slice(&psi_packet(
        PMT_PID,
        0,
        &pmt(version, AUDIO_PID, 0x06, descriptors),
    ));
    out
}

fn following_ac3() -> AudioIngress {
    let mut ing = AudioIngress::new(PROGRAM);
    let chunk = program(0, &AC3_DESCRIPTOR);
    let out = fed(&mut ing, 0, &chunk);
    assert!(out.is_empty(), "tables are not audio");
    assert_eq!(ing.followed().len(), 1, "the AC-3 track is followed");
    ing
}

fn position(ing: &AudioIngress) -> Position {
    ing.followers[0].position
}

/// A transport payload of exactly 184 bytes: `body`, then the 0xFF stuffing a
/// real packet ends with. The reference does not trim it and neither does this,
/// so an expectation slices the same vector the packet was built from.
fn payload_of(body: &[u8]) -> Vec<u8> {
    let mut p = body.to_vec();
    assert!(p.len() <= 184);
    p.resize(184, 0xFF);
    p
}

/// A PES packet start carrying `frames` AC-3 syncframes, as one payload.
fn ac3_start(byte6: u8, frames: usize) -> Vec<u8> {
    let mut body = pes_header(0xBD, 0);
    body.extend_from_slice(&ac3_run(byte6, frames));
    body.truncate(184);
    payload_of(&body)
}

/// One ingest, with the feeds copied out.
///
/// The feeds an ingest returns borrow the chunk they came from, which is the
/// point: the bytes an observer was given are the bytes that were in the
/// transport, not a re-derivation of them. It does mean a caller has to keep
/// the chunk alive, so these tests copy once here rather than naming every
/// fixture twice.
#[derive(Debug, Clone, PartialEq, Eq)]
struct Fed {
    incarnation: u64,
    pid: u16,
    es: Vec<u8>,
    observation: Observation,
}

fn fed(ing: &mut AudioIngress, offset: i64, chunk: &[u8]) -> Vec<Fed> {
    ing.ingest(offset, chunk)
        .expect("aligned")
        .feeds
        .iter()
        .map(|f| Fed {
            incarnation: f.incarnation,
            pid: f.pid,
            es: f.es.to_vec(),
            observation: f.observation,
        })
        .collect()
}

// --- what the tables decide ------------------------------------------------

#[test]
fn a_declared_codec_this_cannot_read_is_followed_by_nothing() {
    let mut ing = AudioIngress::new(PROGRAM);
    // MPEG-1 layer II audio: a real audio track, and not one whose frames this
    // reads for a channel layout.
    let mut chunk = psi_packet(0, 0, &pat(0));
    chunk.extend_from_slice(&psi_packet(PMT_PID, 0, &pmt(0, AUDIO_PID, 0x03, &[])));
    ing.ingest(0, &chunk).expect("aligned");

    assert!(
        ing.followed().is_empty(),
        "an mp2 track declares audio and gets no observer"
    );

    // And its packets are not fed to anything.
    let out = fed(
        &mut ing,
        376,
        &ts_packet(AUDIO_PID, true, 0, &ac3_start(STEREO, 1)),
    );
    assert!(out.is_empty(), "nothing is following that PID");
}

#[test]
fn both_codecs_whose_frames_are_read_are_followed() {
    for (descriptor, codec) in [(&AC3_DESCRIPTOR, "ac3"), (&EAC3_DESCRIPTOR, "eac3")] {
        let mut ing = AudioIngress::new(PROGRAM);
        let chunk = program(0, descriptor);
        ing.ingest(0, &chunk).expect("aligned");
        let followed = ing.followed();
        assert_eq!(followed.len(), 1, "{codec}");
        assert_eq!(followed[0].codec, codec);
        assert_eq!(followed[0].pid, AUDIO_PID);
    }
}

#[test]
fn a_programme_identity_change_ends_the_stream_on_the_same_pid() {
    let mut ing = following_ac3();

    // A run long enough to establish a layout: one frame is a guess, and the
    // observer says so.
    let mut chunk = Vec::new();
    let mut body = pes_header(0xBD, 0);
    body.extend_from_slice(&ac3_run(STEREO, 4));
    for (i, part) in body.chunks(184).enumerate() {
        chunk.extend_from_slice(&ts_packet(
            AUDIO_PID,
            i == 0,
            u8::try_from(i & 0x0F).unwrap(),
            part,
        ));
    }
    let out = fed(&mut ing, 0, &chunk);
    assert!(!out.is_empty());
    let before = ing.followed();
    assert_eq!(before[0].observation.channels, 2, "stereo established");
    let first_incarnation = ing.incarnation();

    // A new PMT version putting the same PID on the same codec is still a
    // different programme identity.
    let table = psi_packet(PMT_PID, 1, &pmt(1, AUDIO_PID, 0x06, &AC3_DESCRIPTOR));
    ing.ingest(0, &table).expect("aligned");

    let after = ing.followed();
    assert_eq!(after.len(), 1, "the track is declared again");
    assert_eq!(after[0].pid, AUDIO_PID, "on the same PID");
    assert_ne!(
        ing.incarnation(),
        first_incarnation,
        "and it is a different incarnation"
    );
    assert_eq!(
        after[0].observation,
        Observation::default(),
        "nothing the old stream said is carried across"
    );
    assert_eq!(after[0].feeds, 0, "and it has been given nothing yet");
}

#[test]
fn selecting_another_programme_ends_every_stream() {
    let mut ing = following_ac3();
    let incarnation = ing.incarnation();
    ing.set_target_program(2);
    assert!(
        ing.followed().is_empty(),
        "nothing of programme one is left"
    );
    assert_ne!(ing.incarnation(), incarnation);
}

#[test]
fn selecting_the_programme_already_selected_changes_nothing() {
    let mut ing = following_ac3();
    let incarnation = ing.incarnation();
    ing.set_target_program(PROGRAM);
    assert_eq!(ing.incarnation(), incarnation);
    assert_eq!(ing.followed().len(), 1);
}

// --- what a packet is ------------------------------------------------------

#[test]
fn a_chunk_that_is_not_whole_packets_is_refused_entirely() {
    let mut ing = following_ac3();
    let err = ing.ingest(0, &[0x47; 100]).expect_err("not whole packets");
    assert_eq!(err, IngestError::UnalignedChunk { len: 100 });
}

#[test]
fn a_scrambled_packet_is_counted_and_fed_to_nothing() {
    let mut ing = following_ac3();
    let mut pkt = ts_packet(AUDIO_PID, true, 0, &ac3_start(STEREO, 1));
    pkt[3] |= 0x80; // transport_scrambling_control = 2

    let out = fed(&mut ing, 0, &pkt);
    assert!(out.is_empty(), "encrypted bytes are not elementary stream");
    let followed = ing.followed();
    assert_eq!(followed[0].scrambled_packets, 1);
    assert_eq!(followed[0].clear_packets, 0);
    assert!(
        !followed[0].observation.known(),
        "a scrambled packet cannot establish a layout"
    );
}

#[test]
fn a_packet_with_no_payload_leaves_the_stream_where_it_was() {
    let mut ing = following_ac3();
    // A header that reaches past this packet, so there is state to disturb.
    let first = payload_of(&pes_header(0xBD, 200)[..184]);
    let out = fed(&mut ing, 0, &ts_packet(AUDIO_PID, true, 0, &first));
    assert!(out.is_empty());
    let was = position(&ing);
    assert!(matches!(was, Position::InHeader { .. }));

    // An adaptation-only packet on the same PID: no payload, and no counter
    // advance either.
    let mut af = vec![0xFF; 188];
    af[0] = 0x47;
    af[1] = u8::try_from(AUDIO_PID >> 8).unwrap();
    af[2] = u8::try_from(AUDIO_PID & 0xFF).unwrap();
    af[3] = 0x20; // adaptation field only
    af[4] = 183;
    let out = fed(&mut ing, 188, &af);
    assert!(out.is_empty());
    assert_eq!(
        position(&ing),
        was,
        "the header is still the one being read"
    );
}

// --- where the elementary stream begins ------------------------------------

#[test]
fn a_pes_header_ending_exactly_at_the_payload_end_feeds_nothing() {
    let mut ing = following_ac3();
    // 9 + 175 = 184: the header ends on the last byte of the payload, so the
    // elementary stream begins in the next one.
    let header = pes_header(0xBD, 175);
    assert_eq!(header.len(), 184);
    let out = fed(&mut ing, 0, &ts_packet(AUDIO_PID, true, 0, &header));
    assert!(out.is_empty(), "an empty run is not a feed");
    assert_eq!(
        position(&ing),
        Position::InElementaryStream,
        "and the stream begins in the next payload"
    );
}

/// The four ways a payload unit start can fail to say where audio begins.
fn unreadable_starts() -> Vec<(&'static str, Vec<u8>)> {
    let mut no_prefix = vec![0x00, 0x00, 0x02, 0xBD, 0x00, 0x00, 0x80, 0x00, 0x00];
    no_prefix.extend_from_slice(&ac3_run(SURROUND, 1));
    no_prefix.truncate(184);

    let mut video = pes_header(0xE0, 0);
    video.extend_from_slice(&ac3_run(SURROUND, 1));
    video.truncate(184);

    // 0xFF carries no optional header at all, so byte eight is not a length.
    let mut no_header = vec![0x00, 0x00, 0x01, 0xFF, 0xFF, 0xFF];
    no_header.extend_from_slice(&ac3_run(SURROUND, 1));
    no_header.truncate(184);

    // An adaptation field of 178 bytes leaves five for the payload: too few to
    // hold the fixed header, so neither the stream id nor the length of the
    // optional header was ever read. Where the elementary stream begins is not
    // unread here, it is unknowable from what arrived.
    let mut short = vec![0xFFu8; 188];
    short[0] = 0x47;
    short[1] = 0x40 | u8::try_from((AUDIO_PID >> 8) & 0x1F).unwrap();
    short[2] = u8::try_from(AUDIO_PID & 0xFF).unwrap();
    short[3] = 0x30;
    short[4] = 178;
    short[5] = 0x00;
    short[183..188].copy_from_slice(&[0x00, 0x00, 0x01, 0xBD, 0x00]);

    vec![
        (
            "a start code prefix that is not 00 00 01",
            ts_packet(AUDIO_PID, true, 0, &payload_of(&no_prefix)),
        ),
        ("a fixed header cut short by an adaptation field", short),
        (
            "a video stream id on a PID the table calls audio",
            ts_packet(AUDIO_PID, true, 0, &payload_of(&video)),
        ),
        (
            "a stream id that carries no optional header and is not audio",
            ts_packet(AUDIO_PID, true, 0, &payload_of(&no_header)),
        ),
    ]
}

#[test]
fn a_payload_unit_that_is_not_an_audio_pes_packet_quarantines_what_follows() {
    // A payload unit start is the start of a PES packet - 13818-1 2.4.3.6 - so
    // when it is not one this path can read, the payloads until the next start
    // are the body of that same unreadable packet. None of them is audio whose
    // boundary anything established.
    //
    // The continuation below carries a valid 5.1 syncframe on a stereo
    // programme. Fed, it would not merely be wasted: it would count a layout
    // that was never broadcast.
    for (what, start) in unreadable_starts() {
        let mut ing = following_ac3();
        let out = fed(&mut ing, 0, &start);
        assert!(
            out.is_empty(),
            "{what}: where the elementary stream begins was not read"
        );
        assert_eq!(
            position(&ing),
            Position::AwaitingStart,
            "{what}: and what follows is that packet's body"
        );

        let body = payload_of(&ac3_run(SURROUND, 1)[..128]);
        let out = fed(&mut ing, 188, &ts_packet(AUDIO_PID, false, 1, &body));
        assert!(
            out.is_empty(),
            "{what}: the continuation belongs to the unreadable packet"
        );
        assert_eq!(
            position(&ing),
            Position::AwaitingStart,
            "{what}: and still nothing has said where audio begins"
        );

        // A payload unit that does say begins the stream again, from its own
        // boundary rather than from where the wait started.
        let recovery = ac3_start(STEREO, 1);
        let out = fed(&mut ing, 376, &ts_packet(AUDIO_PID, true, 2, &recovery));
        assert_eq!(out.len(), 1, "{what}: a valid audio start recovers");
        assert_eq!(out[0].es, recovery[9..], "{what}: from after its header");
        assert_eq!(position(&ing), Position::InElementaryStream);
    }
}

#[test]
fn a_valid_start_recovers_the_wait_even_when_its_own_header_is_incomplete() {
    let mut ing = following_ac3();
    let (_, refused) = unreadable_starts().remove(2);
    fed(&mut ing, 0, &refused);
    assert_eq!(position(&ing), Position::AwaitingStart);

    // An audio PES packet whose optional header reaches past its packet is
    // still an audio PES packet: it says where its elementary stream begins,
    // just not inside this payload. That ends the wait and begins the header
    // state - the recovery is into the right state, not merely out of the
    // wrong one.
    let header = payload_of(&pes_header(0xBD, 200)[..184]);
    let out = fed(&mut ing, 188, &ts_packet(AUDIO_PID, true, 1, &header));
    assert!(out.is_empty(), "the elementary stream has not begun");
    assert_eq!(position(&ing), Position::InHeader { remaining: 25 });

    let mut rest = vec![0x00; 25];
    rest.extend_from_slice(&ac3_run(STEREO, 1)[..128]);
    let rest = payload_of(&rest);
    let out = fed(&mut ing, 376, &ts_packet(AUDIO_PID, false, 2, &rest));
    assert_eq!(out.len(), 1, "and then the stream continues normally");
    assert_eq!(out[0].es, &rest[25..]);
}

#[test]
fn a_programme_change_does_not_carry_the_wait_into_the_new_stream() {
    let mut ing = following_ac3();
    let (_, refused) = unreadable_starts().remove(2);
    fed(&mut ing, 0, &refused);
    assert_eq!(position(&ing), Position::AwaitingStart);

    // The same PID under a new table version is a different elementary stream,
    // and nothing has been refused on that one.
    let table = psi_packet(PMT_PID, 1, &pmt(1, AUDIO_PID, 0x06, &AC3_DESCRIPTOR));
    ing.ingest(188, &table).expect("aligned");
    assert_eq!(
        position(&ing),
        Position::InElementaryStream,
        "a stream that has had nothing refused is not waiting"
    );

    let body = payload_of(&ac3_run(STEREO, 1)[..128]);
    let out = fed(&mut ing, 376, &ts_packet(AUDIO_PID, false, 1, &body));
    assert_eq!(out.len(), 1, "so its continuation is elementary stream");
}

#[test]
fn a_scrambled_payload_unit_start_does_not_begin_a_wait() {
    // Pinned rather than decided. An encrypted payload unit start says a PES
    // packet begins and does not say what it is, which is the same shape of
    // problem as the four above - but a scrambled packet is turned away in the
    // scrambling branch, before any of this is asked, and both this and the
    // reference leave the position alone there.
    //
    // If that is ever changed it should be because someone decided to change
    // it, beside the descramble grace window and with the reference. This test
    // fails if it changes here by accident.
    let mut ing = following_ac3();
    let start = ac3_start(STEREO, 1);
    let out = fed(&mut ing, 0, &ts_packet(AUDIO_PID, true, 0, &start));
    assert_eq!(out.len(), 1);

    let mut scrambled = ts_packet(AUDIO_PID, true, 1, &ac3_start(SURROUND, 1));
    scrambled[3] |= 0x80;
    let out = fed(&mut ing, 188, &scrambled);
    assert!(out.is_empty(), "encrypted bytes reach no observer");
    assert_eq!(
        position(&ing),
        Position::InElementaryStream,
        "and the position is left where it was"
    );

    let body = payload_of(&ac3_run(STEREO, 1)[..128]);
    let out = fed(&mut ing, 376, &ts_packet(AUDIO_PID, false, 2, &body));
    assert_eq!(
        out.len(),
        1,
        "so the clear continuation after it is still fed"
    );
}

#[test]
fn a_continuation_before_any_start_is_elementary_stream() {
    let mut ing = following_ac3();
    let body = payload_of(&ac3_run(STEREO, 2)[..184]);
    let out = fed(&mut ing, 0, &ts_packet(AUDIO_PID, false, 0, &body));
    assert_eq!(
        out.len(),
        1,
        "a capture beginning inside a PES packet still carries audio"
    );
    assert_eq!(out[0].es, body);
}

// --- the header that reaches past its packet -------------------------------

#[test]
fn a_header_reaching_past_its_packet_keeps_its_remainder_out_of_the_stream() {
    let mut ing = following_ac3();

    // A payload is 184 bytes, so a header reaches past it once 9 + its optional
    // part does: 9 + 200 = 209 leaves 25 bytes of header for the packets after.
    let first = payload_of(&pes_header(0xBD, 200)[..184]);
    let out = fed(&mut ing, 0, &ts_packet(AUDIO_PID, true, 0, &first));
    assert!(out.is_empty(), "the elementary stream has not begun");
    assert_eq!(position(&ing), Position::InHeader { remaining: 25 });

    // The next payload is 25 bytes of header and then audio.
    let mut second = vec![0x00; 25];
    second.extend_from_slice(&ac3_run(STEREO, 1)[..128]);
    let second = payload_of(&second);
    let out = fed(&mut ing, 188, &ts_packet(AUDIO_PID, false, 1, &second));
    assert_eq!(out.len(), 1);
    assert_eq!(
        out[0].es,
        &second[25..],
        "only what follows the header reaches the observer"
    );
    assert_eq!(position(&ing), Position::InElementaryStream);
}

#[test]
fn a_header_spanning_two_continuations_is_stepped_over_in_both() {
    let mut ing = following_ac3();

    // 9 + 255 = 264; the first payload holds 184, leaving 80.
    let first = payload_of(&pes_header(0xBD, 255)[..184]);
    ing.ingest(0, &ts_packet(AUDIO_PID, true, 0, &first))
        .expect("aligned");
    assert_eq!(position(&ing), Position::InHeader { remaining: 80 });

    // A continuation is 184 bytes whether the sender meant them or not, so a
    // remainder shorter than one is stepped over inside it. To spend a whole
    // packet on header the adaptation field would have to shorten the payload,
    // which is what this second case does.
    let mut short = vec![0xFF; 188];
    short[0] = 0x47;
    short[1] = u8::try_from(AUDIO_PID >> 8).unwrap();
    short[2] = u8::try_from(AUDIO_PID & 0xFF).unwrap();
    short[3] = 0x31; // adaptation field and payload, cc = 1
    short[4] = 123; // leaving 60 bytes of payload
    short[5] = 0x00;
    let out = fed(&mut ing, 188, &short);
    assert!(out.is_empty(), "sixty bytes of a header are all header");
    assert_eq!(position(&ing), Position::InHeader { remaining: 20 });

    let mut third = vec![0x00; 20];
    third.extend_from_slice(&ac3_run(STEREO, 1)[..128]);
    let third = payload_of(&third);
    let out = fed(&mut ing, 376, &ts_packet(AUDIO_PID, false, 2, &third));
    assert_eq!(out.len(), 1);
    assert_eq!(out[0].es, &third[20..]);
}

#[test]
fn a_continuity_break_inside_a_header_waits_for_the_next_start() {
    let mut ing = following_ac3();

    let first = payload_of(&pes_header(0xBD, 255)[..184]);
    ing.ingest(0, &ts_packet(AUDIO_PID, true, 0, &first))
        .expect("aligned");
    assert_eq!(position(&ing), Position::InHeader { remaining: 80 });

    // The counter jumps: a packet was lost, and how much of it was header is
    // not knowable.
    let body = payload_of(&ac3_run(STEREO, 1));
    let out = fed(&mut ing, 188, &ts_packet(AUDIO_PID, false, 7, &body));
    assert!(
        out.is_empty(),
        "an offset nothing established is not an offset"
    );
    assert_eq!(position(&ing), Position::AwaitingStart);

    // And it keeps waiting rather than guessing.
    let out = fed(&mut ing, 376, &ts_packet(AUDIO_PID, false, 8, &body));
    assert!(out.is_empty());

    // Until a PES packet starts, which says where the stream is again.
    let start = ac3_start(STEREO, 1);
    let out = fed(&mut ing, 564, &ts_packet(AUDIO_PID, true, 9, &start));
    assert_eq!(out.len(), 1);
    assert_eq!(out[0].es, &start[9..]);
}

#[test]
fn a_scrambled_packet_inside_a_header_waits_for_the_next_start() {
    let mut ing = following_ac3();

    let first = payload_of(&pes_header(0xBD, 255)[..184]);
    ing.ingest(0, &ts_packet(AUDIO_PID, true, 0, &first))
        .expect("aligned");

    let mut scrambled = ts_packet(AUDIO_PID, false, 1, &[0x00; 184]);
    scrambled[3] |= 0x80;
    ing.ingest(188, &scrambled).expect("aligned");
    assert_eq!(
        position(&ing),
        Position::AwaitingStart,
        "a header whose remainder arrived encrypted has not been read"
    );

    let body = payload_of(&ac3_run(STEREO, 1));
    let out = fed(&mut ing, 376, &ts_packet(AUDIO_PID, false, 2, &body));
    assert!(out.is_empty());
}

#[test]
fn a_repeated_packet_inside_a_header_does_not_step_over_it_twice() {
    // A payload short enough to be all header: an adaptation field of 123 bytes
    // leaves 60, and the remainder after the start is 80.
    fn short_payload(cc: u8) -> Vec<u8> {
        let mut p = vec![0x00; 188];
        p[0] = 0x47;
        p[1] = u8::try_from(AUDIO_PID >> 8).unwrap();
        p[2] = u8::try_from(AUDIO_PID & 0xFF).unwrap();
        p[3] = 0x30 | (cc & 0x0F);
        p[4] = 123;
        p
    }

    let first = payload_of(&pes_header(0xBD, 255)[..184]);
    let mut tail = vec![0x00; 20];
    tail.extend_from_slice(&ac3_run(STEREO, 1)[..128]);
    let tail = payload_of(&tail);

    // Once: 80 - 60 = 20 left, then the 20 are stepped over and the audio is
    // fed.
    let mut ing = following_ac3();
    ing.ingest(0, &ts_packet(AUDIO_PID, true, 0, &first))
        .expect("aligned");
    assert_eq!(position(&ing), Position::InHeader { remaining: 80 });
    assert!(fed(&mut ing, 188, &short_payload(1)).is_empty());
    assert_eq!(position(&ing), Position::InHeader { remaining: 20 });
    let out = fed(&mut ing, 376, &ts_packet(AUDIO_PID, false, 2, &tail));
    assert_eq!(out.len(), 1);
    assert_eq!(out[0].es, &tail[20..]);

    // Twice, with the counter standing still: the transport said the same
    // packet twice and it carries the same header bytes twice.
    let mut ing2 = following_ac3();
    ing2.ingest(0, &ts_packet(AUDIO_PID, true, 0, &first))
        .expect("aligned");
    let mut doubled = short_payload(1);
    doubled.extend_from_slice(&short_payload(1));
    assert!(fed(&mut ing2, 188, &doubled).is_empty());
    assert_eq!(
        position(&ing2),
        Position::InHeader { remaining: 20 },
        "a repeat does not advance the header"
    );
    let out = fed(&mut ing2, 564, &ts_packet(AUDIO_PID, false, 2, &tail));
    assert_eq!(out.len(), 1);
    assert_eq!(
        out[0].es,
        &tail[20..],
        "and the stream still begins where the header ends"
    );
}

// --- what the feeds say ----------------------------------------------------

#[test]
fn every_feed_carries_the_observation_that_followed_it() {
    let mut ing = following_ac3();
    let frames = ac3_run(STEREO, 2);

    let mut first = pes_header(0xBD, 0);
    first.extend_from_slice(&frames[..128]);
    let first = payload_of(&first);
    let second = payload_of(&frames[128..]);
    let mut chunk = ts_packet(AUDIO_PID, true, 0, &first);
    chunk.extend_from_slice(&ts_packet(AUDIO_PID, false, 1, &second));

    let out = ing.ingest(0, &chunk).expect("aligned");
    assert_eq!(out.feeds.len(), 2);
    assert_eq!(out.feeds[0].observation.frames, 1, "one frame so far");
    assert_eq!(out.feeds[1].observation.frames, 2, "and then two");
    assert_eq!(out.feeds[0].incarnation, out.feeds[1].incarnation);
    assert_eq!(
        out.processed_through, 376,
        "one past the last byte of the two packets"
    );
}

#[test]
fn a_layout_change_inside_one_stream_is_seen_without_the_table_changing() {
    let mut ing = following_ac3();

    let mut chunk = Vec::new();
    let mut first = pes_header(0xBD, 0);
    first.extend_from_slice(&ac3_run(STEREO, 4));
    for (i, part) in first.chunks(184).enumerate() {
        chunk.extend_from_slice(&ts_packet(
            AUDIO_PID,
            i == 0,
            u8::try_from(i & 0x0F).unwrap(),
            part,
        ));
    }
    let out = fed(&mut ing, 0, &chunk);
    assert_eq!(out.last().expect("fed").observation.channels, 2);

    let mut chunk2 = Vec::new();
    let mut second = pes_header(0xBD, 0);
    second.extend_from_slice(&ac3_run(SURROUND, 4));
    for (i, part) in second.chunks(184).enumerate() {
        chunk2.extend_from_slice(&ts_packet(
            AUDIO_PID,
            i == 0,
            u8::try_from((i + 4) & 0x0F).unwrap(),
            part,
        ));
    }
    let out = fed(&mut ing, i64::try_from(chunk.len()).unwrap(), &chunk2);
    assert_eq!(
        out.last().expect("fed").observation.channels,
        6,
        "the observation moves with the audio, not with the table"
    );
}
