// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! Authored PES payloads with the answers written out by hand.
//!
//! Worked out from the bytes and the standard, not by running the reader and
//! recording what it said. The arithmetic being pinned is small and entirely
//! off-by-one shaped - nine, plus a length, compared against how much payload
//! there is - which is exactly the sort that parses real streams for hours
//! before handing a consumer one byte of header.
//!
//! The four outcomes are kept apart on purpose. "Not a start", "the header is
//! not all here", "a start whose elementary stream begins in the next payload"
//! and "a start with bytes in it" are different facts, and a reader that
//! collapsed them would leave its caller unable to tell a broken stream from a
//! boundary.

use super::*;

/// A PES payload: start code, stream id, packet length, two flag bytes, the
/// optional header length, then `header` bytes of optional header and `es`
/// bytes of elementary stream.
fn pes(stream_id: u8, packet_length: u16, header: &[u8], es: &[u8]) -> Vec<u8> {
    let mut p = vec![0x00, 0x00, 0x01, stream_id];
    p.extend_from_slice(&packet_length.to_be_bytes());
    p.push(0x80); // '10' marker, no scrambling, no priority
    p.push(0x00); // no PTS, no DTS: this layer reads none of it anyway
    p.push(u8::try_from(header.len()).expect("test header fits"));
    p.extend_from_slice(header);
    p.extend_from_slice(es);
    p
}

#[test]
fn a_video_start_reports_its_stream_and_its_elementary_bytes() {
    let payload = pes(0xE0, 0, &[0x21, 0x00, 0x01], &[0xAA, 0xBB, 0xCC]);
    // 9 fixed + 3 optional = 12; the three bytes after that are the stream.
    match read_start(&payload) {
        PesStart::Complete {
            stream_id,
            packet_length,
            header_data_length,
            es,
        } => {
            assert_eq!(stream_id, 0xE0);
            assert_eq!(packet_length, 0);
            assert_eq!(header_data_length, 3);
            assert_eq!(es, &[0xAA, 0xBB, 0xCC]);
        }
        other => panic!("expected a complete start, got {other:?}"),
    }
    assert!(is_video_stream_id(0xE0));
    assert!(!is_audio_stream_id(0xE0));
}

#[test]
fn every_video_stream_id_in_the_range_is_one() {
    for id in 0xE0..=0xEF_u8 {
        let payload = pes(id, 100, &[], &[0x01]);
        match read_start(&payload) {
            PesStart::Complete { stream_id, .. } => assert_eq!(stream_id, id),
            other => panic!("{id:#04x}: {other:?}"),
        }
        assert!(is_video_stream_id(id), "{id:#04x} should be video");
    }
    // The boundaries, from the outside.
    assert!(!is_video_stream_id(0xDF));
    assert!(!is_video_stream_id(0xF0));
}

#[test]
fn the_audio_ids_the_reference_accepts_are_the_audio_ids() {
    for id in 0xC0..=0xDF_u8 {
        assert!(is_audio_stream_id(id), "{id:#04x} should be audio");
    }
    // AC-3 in DVB arrives as private_stream_1, and extended_stream_id is used
    // for some of it too. Neither is an audio id by the numbering.
    assert!(is_audio_stream_id(0xBD));
    assert!(is_audio_stream_id(0xFD));
    // Outside, on both sides.
    assert!(!is_audio_stream_id(0xBC));
    assert!(!is_audio_stream_id(0xE0));
    assert!(!is_audio_stream_id(0xFC));
    assert!(!is_audio_stream_id(0xFE));
}

#[test]
fn the_reader_does_not_apply_either_role() {
    // A video id in an audio payload and the other way round: the reader
    // reports what is written and leaves the judgement to the caller.
    let video_in_audio = pes(0xE0, 10, &[], &[0x11]);
    match read_start(&video_in_audio) {
        PesStart::Complete { stream_id, .. } => {
            assert_eq!(stream_id, 0xE0);
            assert!(!is_audio_stream_id(stream_id));
        }
        other => panic!("{other:?}"),
    }
    let audio_in_video = pes(0xC0, 10, &[], &[0x11]);
    match read_start(&audio_in_video) {
        PesStart::Complete { stream_id, .. } => {
            assert_eq!(stream_id, 0xC0);
            assert!(!is_video_stream_id(stream_id));
        }
        other => panic!("{other:?}"),
    }
}

#[test]
fn a_header_of_no_length_leaves_the_stream_at_byte_nine() {
    let payload = pes(0xC0, 200, &[], &[0x0B, 0x77]);
    match read_start(&payload) {
        PesStart::Complete {
            header_data_length,
            es,
            ..
        } => {
            assert_eq!(header_data_length, 0);
            assert_eq!(es, &[0x0B, 0x77]);
        }
        other => panic!("{other:?}"),
    }
}

#[test]
fn a_header_ending_one_byte_early_leaves_exactly_one() {
    let payload = pes(0xE0, 0, &[0xFF; 5], &[0x42]);
    match read_start(&payload) {
        PesStart::Complete { es, .. } => assert_eq!(es, &[0x42]),
        other => panic!("{other:?}"),
    }
}

#[test]
fn a_header_ending_exactly_at_the_end_is_a_start_with_no_stream_bytes() {
    // The distinction this pins: the packet does begin a PES packet and the
    // header is entirely here. There simply are no elementary bytes left. A
    // reader that reported this as incomplete would make the caller wait for a
    // header that had already finished.
    let payload = pes(0xE0, 0, &[0xFF; 4], &[]);
    assert_eq!(payload.len(), 13);
    match read_start(&payload) {
        PesStart::Complete {
            header_data_length,
            es,
            ..
        } => {
            assert_eq!(header_data_length, 4);
            assert!(es.is_empty(), "the header ended at the payload's end");
        }
        other => panic!("expected a complete start with no stream bytes, got {other:?}"),
    }
}

#[test]
fn a_header_reaching_past_the_payload_says_so_rather_than_guessing() {
    // Declared 40 bytes of optional header, 10 present. The elementary stream
    // has not begun, and the next payload opens with the remaining 30 header
    // bytes - which is the whole reason this variant exists. Handing those 30
    // to a consumer as elementary stream is the mistake being prevented.
    let mut payload = pes(0xE0, 0, &[], &[]);
    payload[HEADER_DATA_LENGTH_AT] = 40;
    payload.extend_from_slice(&[0xEE; 10]);
    assert_eq!(payload.len(), 19);

    match read_start(&payload) {
        PesStart::HeaderIncomplete {
            stream_id,
            header_data_length,
            remaining_header,
            ..
        } => {
            assert_eq!(stream_id, 0xE0);
            assert_eq!(header_data_length, 40);
            // 9 + 40 = 49 declared, 19 present.
            assert_eq!(remaining_header, 30);
        }
        other => panic!("expected an incomplete header, got {other:?}"),
    }
}

#[test]
fn one_byte_decides_between_complete_and_incomplete() {
    // The boundary itself, from both sides, because "<=" against "<" is the
    // single most likely way to get this wrong.
    let exact = pes(0xE0, 0, &[0xFF; 6], &[]);
    assert!(matches!(read_start(&exact), PesStart::Complete { es, .. } if es.is_empty()));

    let mut one_short = exact.clone();
    one_short[HEADER_DATA_LENGTH_AT] = 7;
    assert!(matches!(
        read_start(&one_short),
        PesStart::HeaderIncomplete {
            remaining_header: 1,
            ..
        }
    ));
}

#[test]
fn the_ids_that_carry_no_optional_header_are_read_that_way() {
    // A padding stream stuffed with 0xFF. Byte eight is 0xFF, so a reader that
    // applied the optional-header layout would declare 255 bytes of header that
    // do not exist and lose everything after them.
    let mut payload = vec![0x00, 0x00, 0x01, 0xBE, 0x00, 0x10];
    payload.extend_from_slice(&[0xFF; 16]);
    match read_start(&payload) {
        PesStart::NoOptionalHeader {
            stream_id,
            packet_length,
            data,
        } => {
            assert_eq!(stream_id, 0xBE);
            assert_eq!(packet_length, 0x0010);
            // Everything after the six fixed bytes, and nothing skipped.
            assert_eq!(data.len(), 16);
            assert!(data.iter().all(|b| *b == 0xFF));
        }
        other => panic!("expected a header-less start, got {other:?}"),
    }
}

#[test]
fn every_id_without_an_optional_header_is_known() {
    // The list from the standard, and the ones either side of each of them.
    for id in [0xBC_u8, 0xBE, 0xBF, 0xF0, 0xF1, 0xF2, 0xF8, 0xFF] {
        assert!(
            !has_optional_header(id),
            "{id:#04x} carries no optional header"
        );
    }
    for id in [
        0xBB_u8, 0xBD, 0xC0, 0xE0, 0xEF, 0xF3, 0xF7, 0xF9, 0xFD, 0xFE,
    ] {
        assert!(has_optional_header(id), "{id:#04x} does carry one");
    }
}

#[test]
fn a_header_less_id_needs_only_six_bytes_to_be_read() {
    // Six bytes is the whole header for these ids, so a payload of exactly six
    // is a complete start with no data rather than a truncated one.
    let payload = vec![0x00, 0x00, 0x01, 0xBF, 0x00, 0x00];
    match read_start(&payload) {
        PesStart::NoOptionalHeader { data, .. } => assert!(data.is_empty()),
        other => panic!("{other:?}"),
    }
    // Five is not enough to read the packet length.
    assert_eq!(
        read_start(&payload[..5]),
        PesStart::Truncated { available: 5 }
    );
}

#[test]
fn a_payload_without_the_start_code_is_not_a_start() {
    let mut payload = pes(0xE0, 0, &[], &[0x01, 0x02]);
    payload[2] = 0x02; // 00 00 02
    assert_eq!(read_start(&payload), PesStart::NotAStart);

    payload[1] = 0x01; // 00 01 02
    payload[2] = 0x01;
    assert_eq!(read_start(&payload), PesStart::NotAStart);

    // Continuation bytes that happen to look like nothing in particular.
    assert_eq!(read_start(&[0xAA, 0xBB, 0xCC, 0xDD]), PesStart::NotAStart);
}

#[test]
fn too_few_bytes_to_judge_is_not_the_same_as_not_a_start() {
    // Under three bytes the start code cannot be compared at all.
    assert_eq!(read_start(&[]), PesStart::Truncated { available: 0 });
    assert_eq!(
        read_start(&[0x00, 0x00]),
        PesStart::Truncated { available: 2 }
    );
    // The prefix is there but the fixed header is not, so the optional length
    // could not be read and nothing is known about where the stream begins.
    for len in START_CODE.len()..FIXED_HEADER_LEN {
        let payload = &pes(0xE0, 0, &[], &[])[..len];
        assert_eq!(
            read_start(payload),
            PesStart::Truncated { available: len },
            "a {len}-byte payload"
        );
    }
    // And at nine it becomes readable.
    let nine = &pes(0xE0, 0, &[], &[])[..FIXED_HEADER_LEN];
    assert!(matches!(read_start(nine), PesStart::Complete { es, .. } if es.is_empty()));
}

#[test]
fn the_packet_length_field_is_read_as_written() {
    // Zero is legal and means unbounded for video; it is a field here, not an
    // instruction to buffer anything.
    let unbounded = pes(0xE0, 0, &[], &[0x01]);
    assert!(matches!(
        read_start(&unbounded),
        PesStart::Complete {
            packet_length: 0,
            ..
        }
    ));

    let bounded = pes(0xC0, 0xABCD, &[], &[0x01]);
    assert!(matches!(
        read_start(&bounded),
        PesStart::Complete {
            packet_length: 0xABCD,
            ..
        }
    ));

    // Both bytes, in the right order.
    let payload = pes(0xE0, 0x0102, &[], &[]);
    assert_eq!(payload[4], 0x01);
    assert_eq!(payload[5], 0x02);
}

// --- through a transport packet -------------------------------------------

use crate::transport::{PacketView, TS_PACKET_LEN};

/// Wraps a PES payload in a transport packet.
fn ts(pid: u16, pusi: bool, scrambling: u8, adaptation: &[u8], payload: &[u8]) -> Vec<u8> {
    let mut p = vec![0xFF; TS_PACKET_LEN];
    p[0] = 0x47;
    p[1] = u8::try_from(pid >> 8).expect("pid fits") & 0x1F;
    if pusi {
        p[1] |= 0x40;
    }
    p[2] = u8::try_from(pid & 0xFF).expect("pid fits");
    let afc = if adaptation.is_empty() { 0b01 } else { 0b11 };
    p[3] = (scrambling << 6) | (afc << 4);
    let mut at = 4;
    if !adaptation.is_empty() {
        p[4] = u8::try_from(adaptation.len()).expect("adaptation fits");
        p[5..5 + adaptation.len()].copy_from_slice(adaptation);
        at = 5 + adaptation.len();
    }
    let room = TS_PACKET_LEN - at;
    let take = payload.len().min(room);
    p[at..at + take].copy_from_slice(&payload[..take]);
    p
}

#[test]
fn a_packet_that_does_not_start_a_unit_is_not_asked() {
    let payload = pes(0xE0, 0, &[], &[0x01, 0x02]);
    let packet = ts(0x0100, false, 0, &[], &payload);
    let view = PacketView::parse(&packet).expect("well formed");
    // Continuation. Whether a PES packet starts here is not a question about
    // these bytes, and the reader says so by declining rather than by scanning.
    assert_eq!(read_packet_start(&view), None);
}

#[test]
fn a_scrambled_packet_is_not_scanned_for_a_start_code() {
    let payload = pes(0xE0, 0, &[], &[0x01, 0x02]);
    let packet = ts(0x0100, true, 0b10, &[], &payload);
    let view = PacketView::parse(&packet).expect("well formed");
    assert_eq!(view.scrambling_control(), 0b10);
    // Encrypted bytes contain a start code eventually and it means nothing.
    assert_eq!(read_packet_start(&view), None);
}

#[test]
fn an_adaptation_field_before_the_payload_does_not_move_the_pes_header() {
    let payload = pes(0xE0, 0, &[0x11, 0x22], &[0x33, 0x44]);
    let packet = ts(0x0100, true, 0, &[0x00; 20], &payload);
    let view = PacketView::parse(&packet).expect("well formed");
    match read_packet_start(&view).expect("a start") {
        PesStart::Complete {
            stream_id,
            header_data_length,
            es,
            ..
        } => {
            assert_eq!(stream_id, 0xE0);
            assert_eq!(header_data_length, 2);
            // The payload begins after the adaptation field, and the PES header
            // is measured from there rather than from the packet.
            assert_eq!(&es[..2], &[0x33, 0x44]);
        }
        other => panic!("{other:?}"),
    }
}

#[test]
fn an_adaptation_field_can_leave_too_little_payload_for_the_header() {
    // 183 adaptation bytes leave one byte of payload: not enough to compare the
    // start code, let alone read the header.
    let packet = ts(0x0100, true, 0, &[0x00; 182], &[0x00]);
    let view = PacketView::parse(&packet).expect("well formed");
    assert_eq!(
        read_packet_start(&view),
        Some(PesStart::Truncated { available: 1 })
    );
}

#[test]
fn a_packet_with_no_payload_has_no_start_to_read() {
    let mut packet = vec![0xFF; TS_PACKET_LEN];
    packet[0] = 0x47;
    packet[1] = 0x41; // pusi, pid 0x0100 high bits
    packet[2] = 0x00;
    packet[3] = 0b0010_0000; // adaptation only
    packet[4] = 183;
    let view = PacketView::parse(&packet).expect("well formed");
    assert!(!view.has_payload());
    assert_eq!(read_packet_start(&view), None);
}

#[test]
fn consecutive_starts_are_each_read_on_their_own() {
    // Two starts with a continuation between them. Nothing is carried across:
    // this reader has no state, so the second start cannot be affected by the
    // first, and the continuation is not mistaken for either.
    let first = pes(0xE0, 0, &[0x01], &[0xA1, 0xA2]);
    let second = pes(0xE0, 0, &[0x02, 0x03], &[0xB1]);

    let p1 = ts(0x0100, true, 0, &[], &first);
    let pc = ts(0x0100, false, 0, &[], &[0xC1, 0xC2, 0xC3]);
    let p2 = ts(0x0100, true, 0, &[], &second);

    let v1 = PacketView::parse(&p1).expect("well formed");
    let vc = PacketView::parse(&pc).expect("well formed");
    let v2 = PacketView::parse(&p2).expect("well formed");

    match read_packet_start(&v1).expect("a start") {
        PesStart::Complete {
            header_data_length,
            es,
            ..
        } => {
            assert_eq!(header_data_length, 1);
            assert_eq!(&es[..2], &[0xA1, 0xA2]);
        }
        other => panic!("{other:?}"),
    }
    assert_eq!(read_packet_start(&vc), None);
    match read_packet_start(&v2).expect("a start") {
        PesStart::Complete {
            header_data_length,
            es,
            ..
        } => {
            assert_eq!(header_data_length, 2);
            assert_eq!(es[0], 0xB1);
        }
        other => panic!("{other:?}"),
    }
}

/// Counts what real broadcast actually does at PES starts.
///
/// The question this exists to answer is not "does the reader work" - the
/// authored cases answer that - but "does the case the authored cases worry
/// about ever happen". A header reaching past its first payload is easy to
/// reason about and easy to get wrong, and whether it is a real risk or a
/// hypothetical one is a fact about broadcasters rather than about code.
///
/// Skipped without the Step 5d archive, which is deliberately not in the
/// repository.
#[test]
fn real_broadcast_pes_starts_are_counted() {
    let Ok(dir) = std::env::var("XG2G_PSI_HARDWARE_DIR") else {
        eprintln!("XG2G_PSI_HARDWARE_DIR not set; skipping the archived-capture scan");
        return;
    };

    // PIDs proven in Step 5d: ORF1 HD video and its two audio tracks, and the
    // PULS 4 MPEG-2 video and audio.
    for (name, pids) in [
        ("A_orf1hd", vec![1920_u16, 1921, 1922]),
        ("B1_puls4", vec![1791_u16, 1792]),
        ("I_post", vec![1920_u16, 1921]),
    ] {
        let path = std::path::Path::new(&dir).join(format!("{name}.ts"));
        let Ok(data) = std::fs::read(&path) else {
            eprintln!("{name}: not in the archive; skipping");
            continue;
        };

        for pid in pids {
            let (mut starts, mut incomplete, mut truncated, mut not_a_start) =
                (0u64, 0u64, 0u64, 0u64);
            let mut no_optional = 0u64;
            let (mut empty_es, mut scrambled, mut continuations) = (0u64, 0u64, 0u64);
            let mut ids = std::collections::BTreeSet::new();
            let mut header_lengths = std::collections::BTreeSet::new();

            for chunk in data.chunks_exact(TS_PACKET_LEN) {
                let Ok(view) = PacketView::parse(chunk) else {
                    continue;
                };
                if view.pid() != pid || !view.has_payload() {
                    continue;
                }
                if view.scrambling_control() != 0 {
                    scrambled += 1;
                    continue;
                }
                if !view.payload_unit_start() {
                    continuations += 1;
                    continue;
                }
                match read_packet_start(&view) {
                    Some(PesStart::Complete {
                        stream_id,
                        header_data_length,
                        es,
                        ..
                    }) => {
                        starts += 1;
                        ids.insert(stream_id);
                        header_lengths.insert(header_data_length);
                        if es.is_empty() {
                            empty_es += 1;
                        }
                    }
                    Some(PesStart::HeaderIncomplete {
                        stream_id,
                        header_data_length,
                        ..
                    }) => {
                        incomplete += 1;
                        ids.insert(stream_id);
                        header_lengths.insert(header_data_length);
                    }
                    Some(PesStart::NoOptionalHeader { stream_id, .. }) => {
                        no_optional += 1;
                        ids.insert(stream_id);
                    }
                    Some(PesStart::Truncated { .. }) => truncated += 1,
                    Some(PesStart::NotAStart) => not_a_start += 1,
                    None => {}
                }
            }

            eprintln!(
                "{name} pid {pid}: starts {starts}, header-incomplete {incomplete}, \
                 no-optional-header {no_optional}, truncated {truncated}, \
                 pusi-without-start-code {not_a_start}, \
                 start-with-no-es {empty_es}, continuations {continuations}, \
                 scrambled {scrambled}, stream_ids {ids:02x?}, header_data_lengths {header_lengths:?}"
            );
        }
    }
}

/// Checks the reference's own PES-start coordinates against this reader's.
///
/// The Go side writes, for each archived capture, every offset at which it
/// raised a random access point. That offset is `currentPESOffset`, assigned
/// when the reference recognised a video PES start, so each one is a coordinate
/// the reference itself produced at a start.
///
/// This recomputes the video PES starts from the same bytes and requires every
/// reference coordinate to be among them. Containment rather than equality, and
/// deliberately so: only the PES packets that turned out to carry an IDR raise
/// the event, so the reference's list is a subset by construction. Claiming
/// equality would be claiming more than the surface can show.
///
/// What this does prove is the thing Step 6b has to get right: the coordinate.
/// A reader that reported the PES prefix byte, or the first elementary byte, or
/// the payload offset instead of the packet offset would fail here on the first
/// capture.
#[test]
fn the_reference_pes_offsets_are_all_video_pes_starts() {
    let (Ok(dir), Ok(offsets_path)) = (
        std::env::var("XG2G_PSI_HARDWARE_DIR"),
        std::env::var("XG2G_PES_OFFSETS_IN"),
    ) else {
        eprintln!("archive or reference offsets not set; skipping the containment check");
        return;
    };
    let blob = std::fs::read_to_string(&offsets_path).expect("read the reference offsets");

    let mut checked = 0usize;
    for (name, pid) in [("A_orf1hd", 1920_u16), ("B1_puls4", 1791), ("I_post", 1920)] {
        let Some(reference) = reference_offsets(&blob, name) else {
            eprintln!("{name}: no reference offsets; skipping");
            continue;
        };
        let path = std::path::Path::new(&dir).join(format!("{name}.ts"));
        let Ok(data) = std::fs::read(&path) else {
            continue;
        };

        // Every offset at which a video PES packet starts, in this reader's
        // opinion, in the caller's coordinate system: the offset of the packet.
        //
        // A start whose optional header runs past the payload counts. The
        // reference commits currentPESOffset as soon as it recognises the start
        // code and only afterwards decides there are no elementary bytes here,
        // so excluding those would fail the containment on a case this reader
        // classifies correctly - blaming the reader for being more precise.
        let mut starts = std::collections::BTreeSet::new();
        let mut offset = 0i64;
        for chunk in data.chunks_exact(TS_PACKET_LEN) {
            if let Ok(view) = PacketView::parse(chunk)
                && view.pid() == pid
                && let Some(
                    PesStart::Complete { stream_id, .. }
                    | PesStart::HeaderIncomplete { stream_id, .. },
                ) = read_packet_start(&view)
                && is_video_stream_id(stream_id)
            {
                starts.insert(offset);
            }
            offset += i64::try_from(TS_PACKET_LEN).expect("a packet length fits an i64");
        }

        let missing: Vec<i64> = reference
            .iter()
            .copied()
            .filter(|o| !starts.contains(o))
            .collect();
        eprintln!(
            "{name} pid {pid}: {} reference coordinates, {} video PES starts here, {} not matched",
            reference.len(),
            starts.len(),
            missing.len()
        );
        assert!(
            missing.is_empty(),
            "{name}: the reference reported a PES start at {:?} where this reader sees none",
            &missing[..missing.len().min(5)]
        );
        assert!(!reference.is_empty(), "{name}: nothing to check");
        checked += reference.len();
    }
    assert!(checked > 0, "no coordinates were checked");
    eprintln!("{checked} reference PES-start coordinates all matched");
}

/// Pulls one capture's offsets out of the Go side's JSON.
///
/// A hand-written scan rather than a dependency: the document is one flat object
/// of arrays of integers, and adding a parser to the core's dependencies to read
/// a test fixture would be a poor trade.
fn reference_offsets(blob: &str, name: &str) -> Option<Vec<i64>> {
    let key = format!("\"{name}\":[");
    let at = blob.find(&key)? + key.len();
    let end = blob[at..].find(']')? + at;
    let body = &blob[at..end];
    if body.trim().is_empty() {
        return Some(Vec::new());
    }
    Some(
        body.split(',')
            .map(|n| n.trim().parse::<i64>().expect("an integer offset"))
            .collect(),
    )
}
