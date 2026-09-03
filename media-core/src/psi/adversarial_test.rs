// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! What the parser does with bytes nobody would send on purpose.
//!
//! The shared corpus states the semantics both implementations owe each other.
//! These tests are the other half: input the corpus does not describe, where the
//! only thing owed is that the parser stays bounded, keeps its state coherent,
//! and never panics. Nothing here invents a new semantic - where a case would
//! need one to have an answer, it asserts only that the answer is refusal.

use super::crc;
use super::table::{MAX_SECTION_LEN, MIN_SECTION_LEN};
use super::{IngestError, PsiCore, TS_PACKET_LEN};

// --- builders -------------------------------------------------------------

/// Appends the CRC a section carries about itself.
fn seal(mut body: Vec<u8>) -> Vec<u8> {
    let crc = crc::mpeg2(&body);
    body.extend_from_slice(&crc.to_be_bytes());
    body
}

/// A section header, with `section_length` describing what follows it.
fn section_header(
    table_id: u8,
    id_extension: u16,
    section_len: usize,
    version: u8,
    section_number: u8,
    last_section_number: u8,
    current_next: u8,
) -> Vec<u8> {
    vec![
        table_id,
        0xB0 | u8::try_from((section_len >> 8) & 0x0F).expect("masked to a nibble"),
        u8::try_from(section_len & 0xFF).expect("masked to a byte"),
        u8::try_from(id_extension >> 8).expect("a u16 halves into bytes"),
        u8::try_from(id_extension & 0xFF).expect("masked to a byte"),
        0xC0 | ((version & 0x1F) << 1) | (current_next & 0x01),
        section_number,
        last_section_number,
    ]
}

/// A PAT naming `programs` as (number, PMT PID) pairs.
fn pat_section(version: u8, section_number: u8, last: u8, programs: &[(u16, u16)]) -> Vec<u8> {
    let section_len = 5 + 4 * programs.len() + 4;
    let mut out = section_header(0x00, 1, section_len, version, section_number, last, 1);
    for (number, pid) in programs {
        out.push(u8::try_from(number >> 8).expect("a u16 halves"));
        out.push(u8::try_from(number & 0xFF).expect("masked"));
        out.push(0xE0 | u8::try_from((pid >> 8) & 0x1F).expect("masked"));
        out.push(u8::try_from(pid & 0xFF).expect("masked"));
    }
    seal(out)
}

/// One elementary stream entry of a PMT.
struct Es {
    stream_type: u8,
    pid: u16,
    descriptors: Vec<u8>,
}

/// A PMT for `program`, listing `streams`.
fn pmt_section(program: u16, version: u8, section_number: u8, last: u8, streams: &[Es]) -> Vec<u8> {
    let es_bytes: usize = streams.iter().map(|s| 5 + s.descriptors.len()).sum();
    let section_len = 9 + es_bytes + 4;
    let mut out = section_header(0x02, program, section_len, version, section_number, last, 1);
    out.extend_from_slice(&[0xE1, 0x01, 0xF0, 0x00]); // PCR PID, program_info_length 0
    for stream in streams {
        out.push(stream.stream_type);
        out.push(0xE0 | u8::try_from((stream.pid >> 8) & 0x1F).expect("masked"));
        out.push(u8::try_from(stream.pid & 0xFF).expect("masked"));
        out.push(0xF0 | u8::try_from((stream.descriptors.len() >> 8) & 0x0F).expect("masked"));
        out.push(u8::try_from(stream.descriptors.len() & 0xFF).expect("masked"));
        out.extend_from_slice(&stream.descriptors);
    }
    seal(out)
}

/// One 188-byte packet carrying `payload`, padded with `pad`.
fn ts_packet(pid: u16, pusi: bool, cc: u8, pad: u8, payload: &[u8]) -> Vec<u8> {
    assert!(
        payload.len() <= TS_PACKET_LEN - 4,
        "payload does not fit a packet"
    );
    let mut packet = vec![pad; TS_PACKET_LEN];
    packet[0] = 0x47;
    packet[1] = u8::try_from((pid >> 8) & 0x1F).expect("masked") | if pusi { 0x40 } else { 0 };
    packet[2] = u8::try_from(pid & 0xFF).expect("masked");
    packet[3] = 0x10 | (cc & 0x0F);
    packet[4..4 + payload.len()].copy_from_slice(payload);
    packet
}

/// The packets one section needs, with the pointer field on the first.
fn psi_packets(pid: u16, start_cc: u8, section: &[u8]) -> Vec<Vec<u8>> {
    let mut body = vec![0x00];
    body.extend_from_slice(section);
    let mut out = Vec::new();
    let mut cc = start_cc;
    let mut pusi = true;
    let mut rest = body.as_slice();
    while !rest.is_empty() {
        let take = rest.len().min(TS_PACKET_LEN - 4);
        out.push(ts_packet(pid, pusi, cc, 0xFF, &rest[..take]));
        rest = &rest[take..];
        cc = (cc + 1) & 0x0F;
        pusi = false;
    }
    out
}

fn chunk_of(packets: &[Vec<u8>]) -> Vec<u8> {
    packets.iter().flat_map(|p| p.iter().copied()).collect()
}

/// A core already following programme 1 on PID 256.
fn following_program_one() -> (PsiCore, usize) {
    let mut core = PsiCore::new(1);
    let packets = psi_packets(0, 0, &pat_section(0, 0, 0, &[(1, 256)]));
    let count = packets.len();
    core.ingest(0, &chunk_of(&packets))
        .expect("a PAT is packet aligned");
    (core, count)
}

// --- shape of the call ----------------------------------------------------

#[test]
fn a_chunk_that_is_not_whole_packets_is_refused_entirely() {
    let mut core = PsiCore::new(1);
    for len in [1usize, 187, 189, TS_PACKET_LEN * 2 - 1] {
        let err = core
            .ingest(0, &vec![0x47; len])
            .expect_err("an unaligned chunk is refused");
        assert_eq!(err, IngestError::UnalignedChunk { len });
    }
    // The refusals left nothing behind: a PAT still selects.
    let packets = psi_packets(0, 0, &pat_section(0, 0, 0, &[(1, 256)]));
    let outcome = core.ingest(0, &chunk_of(&packets)).expect("aligned");
    assert_eq!(outcome.facts.pmt_pid, 256);
}

#[test]
fn an_empty_chunk_consumes_nothing_and_reports_where_it_was() {
    let mut core = PsiCore::new(1);
    let outcome = core
        .ingest(9_400_000, &[])
        .expect("an empty chunk is aligned");
    assert_eq!(outcome.processed_through, 9_400_000);
    assert!(outcome.events.is_empty());
}

#[test]
fn a_chunk_reports_the_offset_one_past_its_last_byte() {
    let mut core = PsiCore::new(1);
    let packets = psi_packets(0, 0, &pat_section(0, 0, 0, &[(1, 256)]));
    let data = chunk_of(&packets);
    let outcome = core.ingest(9_400_000, &data).expect("aligned");
    assert_eq!(
        outcome.processed_through,
        9_400_000 + i64::try_from(data.len()).expect("small")
    );
}

// --- packets that carry nothing -------------------------------------------

#[test]
fn packets_that_cannot_carry_a_table_change_nothing() {
    let section = pat_section(0, 0, 0, &[(1, 256)]);
    let good = psi_packets(0, 0, &section)[0].clone();

    let mut broken_sync = good.clone();
    broken_sync[0] = 0x48;

    let mut no_payload = good.clone();
    no_payload[3] &= 0x0F; // adaptation_field_control = 0

    let mut adaptation_only = good.clone();
    adaptation_only[3] = 0x20 | (adaptation_only[3] & 0x0F);

    // An adaptation field claiming the rest of the packet leaves no payload
    // behind it, whatever the flag says.
    let mut adaptation_eats_payload = good.clone();
    adaptation_eats_payload[3] = 0x30 | (adaptation_eats_payload[3] & 0x0F);
    adaptation_eats_payload[4] = 183;

    let wrong_pid = ts_packet(0x0064, true, 0, 0xFF, &{
        let mut p = vec![0x00];
        p.extend_from_slice(&section);
        p
    });

    for (what, packet) in [
        ("a bad sync byte", broken_sync),
        ("no payload", no_payload),
        ("adaptation only", adaptation_only),
        (
            "an adaptation field that eats the payload",
            adaptation_eats_payload,
        ),
        ("a PID nothing is read on", wrong_pid),
        ("the null PID", ts_packet(0x1FFF, false, 0, 0xFF, &[])),
    ] {
        let mut core = PsiCore::new(1);
        let outcome = core.ingest(0, &packet).expect("one packet is aligned");
        assert_eq!(outcome.facts.pmt_pid, 0, "{what} named a PMT PID");
        assert!(!outcome.facts.has_pat, "{what} counted as a PAT");
        assert!(outcome.events.is_empty(), "{what} produced an event");
    }
}

// --- sections that are not tables -----------------------------------------

#[test]
fn sections_that_cannot_be_read_are_refused() {
    let good = pat_section(0, 0, 0, &[(1, 256)]);

    let mut bad_crc = good.clone();
    let last = bad_crc.len() - 1;
    bad_crc[last] ^= 0xFF;

    let not_current = {
        let mut s = section_header(0x00, 1, 5 + 4 + 4, 0, 0, 0, 0);
        s.extend_from_slice(&[0x00, 0x01, 0xE1, 0x00]);
        seal(s)
    };

    let foreign_table = {
        let mut s = good.clone();
        s[0] = 0x42; // service description section
        let body_len = s.len() - 4;
        let crc = crc::mpeg2(&s[..body_len]);
        s[body_len..].copy_from_slice(&crc.to_be_bytes());
        s
    };

    // Eleven bytes with a valid CRC: one short of the fields the parser reads.
    let too_short = seal(vec![0x00, 0xB0, 0x08, 0x00, 0x01, 0xC1, 0x00]);
    assert_eq!(too_short.len(), MIN_SECTION_LEN - 1);

    for (what, section) in [
        ("a broken CRC", bad_crc),
        ("a table announced for later", not_current),
        ("another table's section", foreign_table),
        ("a section too short to hold its fields", too_short),
    ] {
        let mut core = PsiCore::new(1);
        let outcome = core
            .ingest(0, &chunk_of(&psi_packets(0, 0, &section)))
            .expect("aligned");
        assert_eq!(outcome.facts.pmt_pid, 0, "{what} was accepted");
        assert!(!outcome.facts.has_pat, "{what} was accepted");
    }
}

#[test]
fn section_lengths_at_and_past_their_bounds_are_survived() {
    // section_length is twelve bits, so this is the largest a section can claim.
    let claims_the_maximum = {
        let mut payload = vec![0x00, 0x00, 0xB0 | 0x0F, 0xFF];
        payload.resize(TS_PACKET_LEN - 4, 0x00);
        ts_packet(0, true, 0, 0x00, &payload)
    };
    assert_eq!(MAX_SECTION_LEN, 3 + 0x0FFF);

    // A length of zero names a section shorter than its own header.
    let claims_nothing = ts_packet(0, true, 0, 0xFF, &[0x00, 0x00, 0xB0, 0x00]);

    // A pointer field past the end of the payload.
    let pointer_past_the_end = ts_packet(0, true, 0, 0xFF, &[0xFF, 0x00, 0xB0, 0x0D]);

    for (what, packet) in [
        ("a section claiming the maximum length", claims_the_maximum),
        ("a section claiming no length", claims_nothing),
        ("a pointer field past the payload", pointer_past_the_end),
    ] {
        let mut core = PsiCore::new(1);
        let outcome = core.ingest(0, &packet).expect("aligned");
        assert!(!outcome.facts.has_pat, "{what} produced a table");
    }
}

#[test]
fn a_table_is_not_read_until_every_section_of_it_is_here() {
    let mut core = PsiCore::new(1);
    // Section 0 of 2 carries the target, so a table read early would find it.
    let section0 = pat_section(0, 0, 1, &[(1, 256)]);
    let outcome = core
        .ingest(0, &chunk_of(&psi_packets(0, 0, &section0)))
        .expect("aligned");
    assert_eq!(outcome.facts.pmt_pid, 0, "half a table was read");

    // The section the table actually declares completes it.
    let section1 = pat_section(0, 1, 1, &[(9, 0x0900)]);
    let outcome = core
        .ingest(0, &chunk_of(&psi_packets(0, 1, &section1)))
        .expect("aligned");
    assert_eq!(outcome.facts.pmt_pid, 256);
}

/// A section numbered outside the table is collected like any other, and the
/// generation then holds more sections than the table declares - so it stops
/// completing, and goes on not completing even once the section that was
/// genuinely missing arrives. Only a new generation clears it.
///
/// Verified against the Go reference before being asserted here: both
/// implementations do exactly this. The corpus states the first half of it
/// (`a_section_numbered_outside_the_table_does_not_complete_it`) and not the
/// second, so this test is Rust-side coverage of shared behaviour rather than a
/// semantic of its own.
#[test]
fn a_stray_section_number_holds_the_generation_until_a_new_one_starts() {
    let mut core = PsiCore::new(1);
    for (what, packets) in [
        (
            "section 0 of 2",
            psi_packets(0, 0, &pat_section(0, 0, 1, &[(1, 256)])),
        ),
        (
            "a section numbered 5",
            psi_packets(0, 1, &pat_section(0, 5, 1, &[(9, 0x0900)])),
        ),
        (
            "section 1 of 2",
            psi_packets(0, 2, &pat_section(0, 1, 1, &[(9, 0x0900)])),
        ),
    ] {
        let outcome = core.ingest(0, &chunk_of(&packets)).expect("aligned");
        assert_eq!(outcome.facts.pmt_pid, 0, "the table completed after {what}");
    }

    // A new version is a new generation, and the stray does not travel with it.
    for packets in [
        psi_packets(0, 3, &pat_section(1, 0, 1, &[(1, 256)])),
        psi_packets(0, 4, &pat_section(1, 1, 1, &[(9, 0x0900)])),
    ] {
        core.ingest(0, &chunk_of(&packets)).expect("aligned");
    }
    let outcome = core.ingest(0, &[]).expect("aligned");
    assert_eq!(
        outcome.facts.pmt_pid, 256,
        "a new generation did not recover"
    );
}

#[test]
fn a_table_declaring_255_sections_never_completes_and_stays_bounded() {
    let mut core = PsiCore::new(1);
    for number in 0..=60u8 {
        let section = pat_section(0, number, 255, &[(1, 256)]);
        let outcome = core
            .ingest(0, &chunk_of(&psi_packets(0, number & 0x0F, &section)))
            .expect("aligned");
        assert_eq!(outcome.facts.pmt_pid, 0, "an incomplete table was read");
    }
}

#[test]
fn a_repeated_section_number_replaces_rather_than_accumulates() {
    let mut core = PsiCore::new(1);
    for round in 0..40u8 {
        let section = pat_section(0, 0, 0, &[(1, 256 + u16::from(round % 2))]);
        let outcome = core
            .ingest(0, &chunk_of(&psi_packets(0, round & 0x0F, &section)))
            .expect("aligned");
        assert!(outcome.facts.pmt_pid == 256 || outcome.facts.pmt_pid == 257);
    }
}

// --- programme identity ---------------------------------------------------

#[test]
fn a_pmt_for_another_programme_leaves_the_table_being_assembled_alone() {
    let (mut core, pat_packets) = following_program_one();
    assert!(pat_packets >= 1);

    let mine0 = pmt_section(
        1,
        0,
        0,
        1,
        &[Es {
            stream_type: 0x1B,
            pid: 257,
            descriptors: vec![],
        }],
    );
    let foreign = pmt_section(
        2,
        0,
        0,
        0,
        &[Es {
            stream_type: 0x1B,
            pid: 999,
            descriptors: vec![],
        }],
    );
    let mine1 = pmt_section(
        1,
        0,
        1,
        1,
        &[Es {
            stream_type: 0x03,
            pid: 258,
            descriptors: vec![],
        }],
    );

    let outcome = core
        .ingest(
            0,
            &chunk_of(
                &[
                    psi_packets(256, 0, &mine0),
                    psi_packets(256, 1, &foreign),
                    psi_packets(256, 2, &mine1),
                ]
                .concat(),
            ),
        )
        .expect("aligned");

    assert!(outcome.facts.has_pmt, "the table never completed");
    assert_eq!(outcome.facts.program_number, 1);
    assert_eq!(outcome.facts.video_pid, 257, "section 0 was lost");
    assert_eq!(outcome.facts.audio_pids, vec![258], "section 1 was lost");
    for packet in &outcome.active.pmt {
        assert!(
            !chunk_of(&psi_packets(256, 1, &foreign))
                .windows(TS_PACKET_LEN)
                .any(|w| w == packet),
            "a refused table's packet reached the active PMT"
        );
    }
}

#[test]
fn an_elementary_stream_entry_never_reads_the_sections_own_crc() {
    // The entry declares four descriptor bytes that are not inside the loop, so
    // the four after it are the CRC. Nothing may read them.
    let (mut core, _) = following_program_one();
    let mut section = pmt_section(
        1,
        0,
        0,
        0,
        &[
            Es {
                stream_type: 0x1B,
                pid: 257,
                descriptors: vec![],
            },
            Es {
                stream_type: 0x06,
                pid: 258,
                descriptors: vec![0x6A, 0x02, 0x80, 0x02],
            },
        ],
    );
    // Drop the descriptors, keep the length claiming them, reseal.
    let keep = section.len() - 8;
    section.truncate(keep);
    section.extend_from_slice(&[0, 0, 0, 0]);
    let new_len = section.len() - 3;
    section[1] = 0xB0 | u8::try_from((new_len >> 8) & 0x0F).expect("masked");
    section[2] = u8::try_from(new_len & 0xFF).expect("masked");
    let body_len = section.len() - 4;
    let crc = crc::mpeg2(&section[..body_len]);
    section[body_len..].copy_from_slice(&crc.to_be_bytes());

    let outcome = core
        .ingest(0, &chunk_of(&psi_packets(256, 0, &section)))
        .expect("aligned");
    assert!(outcome.facts.has_pmt);
    assert_eq!(
        outcome.facts.video_pid, 257,
        "the entry before the bad one was lost"
    );
    assert!(
        outcome.facts.audio_pids.is_empty(),
        "an entry whose descriptors ran past the loop produced a track: {:?}",
        outcome.facts.audio_tracks
    );
}

#[test]
fn a_descriptor_longer_than_its_block_ends_the_walk_without_reading_on() {
    let (mut core, _) = following_program_one();
    // A language descriptor claiming 200 bytes inside a 6-byte block, followed
    // by what would be an AC-3 descriptor if the walk carried on.
    let descriptors = vec![0x0A, 0xC8, b'd', b'e', b'u', 0x00];
    let section = pmt_section(
        1,
        0,
        0,
        0,
        &[Es {
            stream_type: 0x03,
            pid: 258,
            descriptors,
        }],
    );
    let outcome = core
        .ingest(0, &chunk_of(&psi_packets(256, 0, &section)))
        .expect("aligned");
    assert_eq!(outcome.facts.audio_pids, vec![258]);
    assert_eq!(
        outcome.facts.audio_tracks[0].language, "und",
        "a descriptor that does not fit was read anyway"
    );
}

// --- segmentation ---------------------------------------------------------

#[test]
fn where_a_chunk_was_cut_does_not_change_what_the_stream_meant() {
    // The same packets in the same order, offered as one call and as many. A
    // chunk boundary is transport segmentation: the assembler carries a partial
    // section across calls, so the stream is the same stream.
    let packets = [
        psi_packets(0, 0, &pat_section(0, 0, 0, &[(1, 256)])),
        psi_packets(
            256,
            0,
            &pmt_section(
                1,
                0,
                0,
                0,
                &[
                    Es {
                        stream_type: 0x1B,
                        pid: 257,
                        descriptors: vec![],
                    },
                    Es {
                        stream_type: 0x06,
                        pid: 258,
                        descriptors: vec![
                            0x0A, 0x04, b'd', b'e', b'u', 0x00, 0x6A, 0x02, 0x80, 0x02,
                        ],
                    },
                ],
            ),
        ),
    ]
    .concat();

    let mut whole = PsiCore::new(1);
    let all = chunk_of(&packets);
    let expected = whole.ingest(0, &all).expect("aligned").facts;

    for group in 1..=packets.len() {
        let mut split = PsiCore::new(1);
        let mut offset = 0i64;
        for window in packets.chunks(group) {
            let data = chunk_of(window);
            split.ingest(offset, &data).expect("aligned");
            offset += i64::try_from(data.len()).expect("small");
        }
        let got = split.ingest(offset, &[]).expect("aligned").facts;
        assert_eq!(
            got, expected,
            "cutting the stream every {group} packets changed it"
        );
    }
}

// --- arbitrary bytes ------------------------------------------------------

/// A deterministic generator, so a failure is reproducible from its seed and the
/// crate keeps its zero dependencies.
struct Xorshift(u64);

impl Xorshift {
    fn next(&mut self) -> u64 {
        let mut x = self.0;
        x ^= x << 13;
        x ^= x >> 7;
        x ^= x << 17;
        self.0 = x;
        x
    }

    fn byte(&mut self) -> u8 {
        u8::try_from(self.next() & 0xFF).expect("masked to a byte")
    }
}

#[test]
fn arbitrary_bytes_never_panic_and_never_grow_without_bound() {
    let mut rng = Xorshift(0x2026_0903_5B00_1234);
    for seed_round in 0..400 {
        let mut core = PsiCore::new(u16::from(rng.byte()));
        for _ in 0..12 {
            let packets = 1 + usize::from(rng.byte() % 8);
            let mut data = vec![0u8; packets * TS_PACKET_LEN];
            for byte in &mut data {
                *byte = rng.byte();
            }
            // Half the rounds get well-formed framing, so the parser is driven
            // past the sync check rather than bouncing off it every time.
            if seed_round % 2 == 0 {
                for packet in data.chunks_exact_mut(TS_PACKET_LEN) {
                    packet[0] = 0x47;
                    packet[1] &= 0x5F; // keep PID small, PUSI as drawn
                    packet[2] = 0x00;
                    packet[3] = 0x10 | (packet[3] & 0x0F);
                }
            }
            let outcome = core
                .ingest(0, &data)
                .expect("packet aligned by construction");
            assert!(
                outcome.facts.audio_pids.len() <= 64,
                "audio PID list grew without bound"
            );
            assert!(
                outcome.facts.audio_tracks.len() <= 64,
                "track list grew without bound"
            );
            assert!(
                outcome.active.pat.len() <= 256,
                "raw PAT grew without bound"
            );
            assert!(
                outcome.active.pmt.len() <= 256,
                "raw PMT grew without bound"
            );
            assert!(outcome.events.len() <= 4096, "events grew without bound");
        }
    }
}

#[test]
fn uniform_bytes_never_panic() {
    for fill in [0x00u8, 0xFF, 0x47] {
        let mut core = PsiCore::new(1);
        let outcome = core
            .ingest(0, &vec![fill; TS_PACKET_LEN * 32])
            .expect("aligned");
        assert!(!outcome.facts.has_pmt, "uniform {fill:#04x} produced a PMT");
    }
}

#[test]
fn every_single_byte_mutation_of_a_good_stream_is_survived() {
    let packets = [
        psi_packets(0, 0, &pat_section(0, 0, 0, &[(1, 256)])),
        psi_packets(
            256,
            0,
            &pmt_section(
                1,
                0,
                0,
                0,
                &[Es {
                    stream_type: 0x1B,
                    pid: 257,
                    descriptors: vec![],
                }],
            ),
        ),
    ]
    .concat();
    let good = chunk_of(&packets);

    for index in 0..good.len() {
        for xor in [0x01u8, 0x80, 0xFF] {
            let mut broken = good.clone();
            broken[index] ^= xor;
            let mut core = PsiCore::new(1);
            // The only requirement is that it answers at all.
            let outcome = core.ingest(0, &broken).expect("aligned");
            assert!(outcome.facts.audio_pids.len() <= 64);
        }
    }
}
