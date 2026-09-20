// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

use super::VideoIngress;
use crate::psi::VideoCodec;
use crate::transport::TS_PACKET_LEN;

const PROGRAM: u16 = 1;
const PMT_PID: u16 = 0x1000;
const VIDEO_PID: u16 = 0x0100;
const PACKET_LEN_I64: i64 = 188;

// --- MPEG-TS fixture helpers ------------------------------------------------

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

fn section(table_id: u8, id_extension: u16, version: u8, payload: &[u8]) -> Vec<u8> {
    let section_len = 9 + payload.len();
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

fn pat_packet(version: u8) -> Vec<u8> {
    let sect = section(
        0x00,
        1,
        version,
        &[
            u8::try_from(PROGRAM >> 8).unwrap(),
            u8::try_from(PROGRAM & 0xFF).unwrap(),
            0xE0 | u8::try_from(PMT_PID >> 8).unwrap(),
            u8::try_from(PMT_PID & 0xFF).unwrap(),
        ],
    );
    let mut payload = vec![0x00];
    payload.extend_from_slice(&sect);
    make_ts_packet(0x0000, true, 0, 0, false, &payload)
}

fn pmt_packet(version: u8, vpid: u16, stream_type: u8) -> Vec<u8> {
    let payload = vec![
        0xE0 | u8::try_from(vpid >> 8).unwrap(),
        u8::try_from(vpid & 0xFF).unwrap(),
        0xF0,
        0x00, // program_info_length
        stream_type,
        0xE0 | u8::try_from(vpid >> 8).unwrap(),
        u8::try_from(vpid & 0xFF).unwrap(),
        0xF0,
        0x00, // ES info length: 0
    ];
    let sect = section(0x02, PROGRAM, version, &payload);
    let mut pkt_payload = vec![0x00];
    pkt_payload.extend_from_slice(&sect);
    make_ts_packet(PMT_PID, true, 0, 0, false, &pkt_payload)
}

fn make_ts_packet(
    pid: u16,
    pusi: bool,
    cc: u8,
    scrambling: u8,
    tei: bool,
    payload: &[u8],
) -> Vec<u8> {
    assert!(payload.len() <= 184, "payload exceeds 184 bytes");
    let mut p = vec![0xFF; TS_PACKET_LEN];
    p[0] = 0x47;
    p[1] = u8::try_from((pid >> 8) & 0x1F).unwrap();
    if pusi {
        p[1] |= 0x40;
    }
    if tei {
        p[1] |= 0x80;
    }
    p[2] = u8::try_from(pid & 0xFF).unwrap();

    let afc = if payload.len() == 184 {
        0x01 // payload only
    } else {
        0x03 // adaptation field + payload
    };
    p[3] = (afc << 4) | ((scrambling & 0x03) << 6) | (cc & 0x0F);

    if payload.len() == 184 {
        p[4..].copy_from_slice(payload);
    } else if payload.len() == 183 {
        p[4] = 0; // AF length = 0
        p[5..].copy_from_slice(payload);
    } else {
        let af_len = 184 - 1 - payload.len();
        p[4] = u8::try_from(af_len).unwrap();
        p[5] = 0x00; // AF flags
        let payload_start = 5 + af_len;
        p[payload_start..].copy_from_slice(payload);
    }
    p
}

fn make_adaptation_only_di_packet(pid: u16, cc: u8) -> Vec<u8> {
    let mut p = vec![0xFF; TS_PACKET_LEN];
    p[0] = 0x47;
    p[1] = u8::try_from((pid >> 8) & 0x1F).unwrap();
    p[2] = u8::try_from(pid & 0xFF).unwrap();
    p[3] = 0x20 | (cc & 0x0F); // AFC = 0x02 (adaptation field only)
    p[4] = 183; // AF length (fills remainder of packet)
    p[5] = 0x80; // Discontinuity indicator set
    p
}

fn standard_setup() -> (VideoIngress, i64) {
    let mut ingress = VideoIngress::new(PROGRAM);
    let mut data = pat_packet(0);
    data.extend_from_slice(&pmt_packet(0, VIDEO_PID, 0x1B)); // 0x1B = H.264
    let outcome = ingress.ingest(0, &data).expect("setup ingest");
    assert_eq!(outcome.processed_through, 376);
    let facts = ingress.facts();
    assert_eq!(facts.pid, VIDEO_PID);
    assert_eq!(facts.codec, VideoCodec::H264);
    (ingress, 376)
}

// --- Tests ------------------------------------------------------------------

#[test]
fn initial_state_in_elementary_stream_accepts_first_continuation() {
    let (mut ingress, mut offset) = standard_setup();

    // The stream begins with a continuation packet (PUSI = false, CC = 0).
    let continuation_payload = vec![0xAA, 0xBB, 0xCC, 0xDD];
    let packet = make_ts_packet(VIDEO_PID, false, 0, 0, false, &continuation_payload);
    let outcome = ingress
        .ingest(offset, &packet)
        .expect("ingest continuation");
    offset += PACKET_LEN_I64;
    assert_eq!(outcome.processed_through, offset);

    // Initial state InElementaryStream must emit this payload as ES immediately.
    assert_eq!(outcome.feeds.len(), 1);
    let feed = outcome.feeds[0];
    assert_eq!(feed.pid, VIDEO_PID);
    assert_eq!(feed.offset, 376);
    assert!(!feed.pusi);
    assert_eq!(feed.es, &continuation_payload);

    let facts = ingress.facts();
    assert_eq!(facts.clear_packets, 1);
    assert_eq!(facts.clear_run, 1);
    assert_eq!(facts.scrambled_packets, 0);
    assert_eq!(facts.current_pes_offset, None); // mid-PES, no PES start seen
    assert!(!facts.awaiting_start);
}

#[test]
fn pusi_clears_current_pes_offset_first_before_validation() {
    let (mut ingress, mut offset) = standard_setup();

    // Step 1: Feed a valid video PES start packet.
    // 00 00 01 E0, length 00 00, flags 80 00, header_data_length 00, ES payload
    let mut valid_pusi_payload = vec![0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x00, 0x00];
    valid_pusi_payload.extend_from_slice(&[0x00, 0x00, 0x01, 0x67, 0x42]); // NAL SPS
    let p1 = make_ts_packet(VIDEO_PID, true, 0, 0, false, &valid_pusi_payload);
    let outcome1 = ingress.ingest(offset, &p1).expect("valid pusi");
    let p1_offset = offset;
    offset += PACKET_LEN_I64;

    assert_eq!(outcome1.feeds.len(), 1);
    assert_eq!(outcome1.feeds[0].offset, p1_offset);
    assert!(outcome1.feeds[0].pusi);
    assert_eq!(outcome1.feeds[0].es, &[0x00, 0x00, 0x01, 0x67, 0x42]);

    let facts = ingress.facts();
    assert_eq!(facts.current_pes_offset, Some(p1_offset));
    assert_eq!(facts.pes_starts, 1);
    assert!(!facts.awaiting_start);

    // Step 2: Feed an invalid PUSI packet (wrong prefix 00 00 02).
    let invalid_pusi_payload = vec![0x00, 0x00, 0x02, 0xE0, 0x00, 0x00, 0x80, 0x00, 0x00, 0xFF];
    let p2 = make_ts_packet(VIDEO_PID, true, 1, 0, false, &invalid_pusi_payload);
    let outcome2 = ingress.ingest(offset, &p2).expect("invalid pusi");

    assert_eq!(outcome2.feeds.len(), 0);
    let facts2 = ingress.facts();
    // current_pes_offset MUST be cleared to None, not left at Some(p1_offset)!
    assert_eq!(facts2.current_pes_offset, None);
    assert_eq!(facts2.clear_packets, 2);
    assert_eq!(facts2.clear_run, 2);
    assert!(facts2.awaiting_start);
}

#[test]
fn in_header_skips_remaining_and_emits_es_on_clear_continuation() {
    let (mut ingress, mut offset) = standard_setup();

    // PUSI packet with header_data_length = 10, but only 5 bytes fit in packet payload.
    // 9 fixed bytes + 5 optional bytes = 14 bytes total payload.
    let pusi_payload = vec![
        0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x0A, // fixed 9 bytes (hdr_len = 10)
        0x01, 0x02, 0x03, 0x04, 0x05, // 5 of 10 optional header bytes
    ];
    let p1 = make_ts_packet(VIDEO_PID, true, 0, 0, false, &pusi_payload);
    let p1_offset = offset;
    let outcome1 = ingress.ingest(offset, &p1).expect("incomplete header pusi");
    offset += PACKET_LEN_I64;

    assert_eq!(outcome1.feeds.len(), 0); // ES has not begun
    let facts1 = ingress.facts();
    assert_eq!(facts1.current_pes_offset, Some(p1_offset));
    assert_eq!(facts1.pes_starts, 1);
    assert!(!facts1.awaiting_start);

    // Continuation packet: carries the remaining 5 header bytes followed by ES.
    let mut cont_payload = vec![0x06, 0x07, 0x08, 0x09, 0x0A]; // remaining 5 header bytes
    let es_data = vec![0x00, 0x00, 0x01, 0x65, 0x88]; // IDR slice
    cont_payload.extend_from_slice(&es_data);

    let p2 = make_ts_packet(VIDEO_PID, false, 1, 0, false, &cont_payload);
    let outcome2 = ingress.ingest(offset, &p2).expect("header continuation");

    assert_eq!(outcome2.feeds.len(), 1);
    assert_eq!(outcome2.feeds[0].offset, p1_offset + PACKET_LEN_I64);
    assert!(!outcome2.feeds[0].pusi);
    assert_eq!(outcome2.feeds[0].es, &es_data);

    let facts2 = ingress.facts();
    assert_eq!(facts2.clear_packets, 2);
    assert_eq!(facts2.clear_run, 2);
    assert!(!facts2.awaiting_start);
}

#[test]
fn in_header_transitions_to_awaiting_start_on_scrambled_continuation() {
    let (mut ingress, mut offset) = standard_setup();

    // PUSI with incomplete header (5 of 10 bytes).
    let pusi_payload = vec![
        0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x0A, 0x01, 0x02, 0x03, 0x04, 0x05,
    ];
    let p1 = make_ts_packet(VIDEO_PID, true, 0, 0, false, &pusi_payload);
    ingress.ingest(offset, &p1).expect("incomplete header pusi");
    offset += PACKET_LEN_I64;

    // Scrambled continuation arrives while InHeader.
    let cont_payload = vec![0xFF; 20];
    let p2 = make_ts_packet(VIDEO_PID, false, 1, 2, false, &cont_payload); // scrambling = 2
    let outcome2 = ingress.ingest(offset, &p2).expect("scrambled continuation");
    offset += PACKET_LEN_I64;

    assert_eq!(outcome2.feeds.len(), 0);
    let facts2 = ingress.facts();
    assert_eq!(facts2.scrambled_packets, 1);
    assert_eq!(facts2.clear_run, 0);
    assert!(facts2.awaiting_start);

    // Next continuation is clear, but must be quarantined because we are in AwaitingStart.
    let p3 = make_ts_packet(VIDEO_PID, false, 2, 0, false, &[0x11, 0x22, 0x33]);
    let outcome3 = ingress
        .ingest(offset, &p3)
        .expect("quarantined continuation");

    assert_eq!(outcome3.feeds.len(), 0);
    let facts3 = ingress.facts();
    assert_eq!(facts3.clear_packets, 2); // 1 from pusi, 1 from p3
    assert_eq!(facts3.clear_run, 1);
    assert!(facts3.awaiting_start);
}

#[test]
fn in_header_transitions_to_awaiting_start_on_cc_gap() {
    let (mut ingress, mut offset) = standard_setup();

    // PUSI with incomplete header (5 of 10 bytes), CC = 0.
    let pusi_payload = vec![
        0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x0A, 0x01, 0x02, 0x03, 0x04, 0x05,
    ];
    let p1 = make_ts_packet(VIDEO_PID, true, 0, 0, false, &pusi_payload);
    ingress.ingest(offset, &p1).expect("incomplete header pusi");
    offset += PACKET_LEN_I64;

    // Continuation with CC = 5 (gap).
    let cont_payload = vec![0x06, 0x07, 0x08, 0x09, 0x0A, 0xEE];
    let p2 = make_ts_packet(VIDEO_PID, false, 5, 0, false, &cont_payload);
    let outcome2 = ingress.ingest(offset, &p2).expect("gap continuation");

    assert_eq!(outcome2.feeds.len(), 0);
    let facts2 = ingress.facts();
    assert_eq!(facts2.clear_packets, 2);
    assert_eq!(facts2.clear_run, 2);
    assert!(facts2.awaiting_start);
}

#[test]
fn caller_owned_monotonic_offsets_across_multiple_ingest_calls() {
    let mut ingress = VideoIngress::new(PROGRAM);

    // Initial offset is 100_000.
    let mut data1 = pat_packet(0);
    data1.extend_from_slice(&pmt_packet(0, VIDEO_PID, 0x1B));
    let outcome1 = ingress.ingest(100_000, &data1).expect("chunk 1");
    assert_eq!(outcome1.processed_through, 100_376);

    // Second call continues at 100_376.
    let valid_pusi_payload = vec![0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x00, 0x00, 0x42];
    let p1 = make_ts_packet(VIDEO_PID, true, 0, 0, false, &valid_pusi_payload);
    let outcome2 = ingress
        .ingest(outcome1.processed_through, &p1)
        .expect("chunk 2");
    assert_eq!(outcome2.processed_through, 100_564);
    assert_eq!(outcome2.feeds.len(), 1);
    assert_eq!(outcome2.feeds[0].offset, 100_376);

    let facts = ingress.facts();
    assert_eq!(facts.current_pes_offset, Some(100_376));

    // Third call continues at 100_564.
    let p2 = make_ts_packet(VIDEO_PID, false, 1, 0, false, &[0x43, 0x44]);
    let outcome3 = ingress
        .ingest(outcome2.processed_through, &p2)
        .expect("chunk 3");
    assert_eq!(outcome3.processed_through, 100_752);
    assert_eq!(outcome3.feeds.len(), 1);
    assert_eq!(outcome3.feeds[0].offset, 100_564);
}

#[test]
fn exact_duplicate_dropped_without_counter_increments() {
    let (mut ingress, mut offset) = standard_setup();

    let payload = vec![0x10, 0x20, 0x30];
    let packet = make_ts_packet(VIDEO_PID, false, 0, 0, false, &payload);

    let outcome1 = ingress.ingest(offset, &packet).expect("packet 1");
    offset += PACKET_LEN_I64;
    assert_eq!(outcome1.feeds.len(), 1);

    let facts1 = ingress.facts();
    assert_eq!(facts1.clear_packets, 1);
    assert_eq!(facts1.clear_run, 1);

    // Exact duplicate with same CC and same bytes.
    let outcome2 = ingress.ingest(offset, &packet).expect("duplicate packet");
    assert_eq!(outcome2.feeds.len(), 0);

    let facts2 = ingress.facts();
    assert_eq!(facts2.clear_packets, 1); // unchanged!
    assert_eq!(facts2.clear_run, 1); // unchanged!
}

#[test]
fn adaptation_only_di_arms_pending_discontinuity() {
    let (mut ingress, mut offset) = standard_setup();

    // Packet 0: normal clear packet, CC = 0.
    let p0 = make_ts_packet(VIDEO_PID, false, 0, 0, false, &[0xAA]);
    ingress.ingest(offset, &p0).expect("p0");
    offset += PACKET_LEN_I64;

    // Packet 1: Adaptation-only packet with discontinuity indicator, CC = 0.
    let ad_pkt = make_adaptation_only_di_packet(VIDEO_PID, 0);
    let outcome_ad = ingress.ingest(offset, &ad_pkt).expect("adaptation-only DI");
    offset += PACKET_LEN_I64;
    assert_eq!(outcome_ad.feeds.len(), 0);

    let facts_ad = ingress.facts();
    assert_eq!(facts_ad.clear_packets, 1); // Not incremented for adaptation-only

    // Packet 2: Next payload packet jumps CC to 8.
    // Because pending DI was armed, this is treated as Discontinuous (announced),
    // breaking continuation cleanly into AwaitingStart.
    let p2 = make_ts_packet(VIDEO_PID, false, 8, 0, false, &[0xBB]);
    let outcome2 = ingress
        .ingest(offset, &p2)
        .expect("p2 jump with pending DI");

    assert_eq!(outcome2.feeds.len(), 0); // Broken into AwaitingStart
    let facts2 = ingress.facts();
    assert_eq!(facts2.clear_packets, 2); // physical clear packet counted
    assert_eq!(facts2.clear_run, 2);
    assert!(facts2.awaiting_start);
}

#[test]
fn scrambling_counters_and_confirmation_at_100_packets() {
    let (mut ingress, mut offset) = standard_setup();

    // 10 clear packets.
    let mut clear_data = Vec::new();
    for cc in 0..10 {
        clear_data.extend_from_slice(&make_ts_packet(VIDEO_PID, false, cc, 0, false, &[0x11]));
    }
    ingress.ingest(offset, &clear_data).expect("clear data");
    offset = offset.saturating_add(i64::try_from(clear_data.len()).unwrap());

    let facts1 = ingress.facts();
    assert_eq!(facts1.clear_packets, 10);
    assert_eq!(facts1.clear_run, 10);
    assert_eq!(facts1.scrambled_packets, 0);
    assert!(!facts1.scrambled_confirmed);

    // 1 scrambled packet: resets clear_run.
    let scr_pkt = make_ts_packet(VIDEO_PID, false, 10, 1, false, &[0x22]);
    ingress.ingest(offset, &scr_pkt).expect("scrambled packet");

    let facts2 = ingress.facts();
    assert_eq!(facts2.clear_packets, 10);
    assert_eq!(facts2.clear_run, 0);
    assert_eq!(facts2.scrambled_packets, 1);
    assert!(!facts2.scrambled_confirmed); // clear_packets != 0

    // Fresh ingress with 100 scrambled packets from start.
    let (mut ingress2, mut offset2) = standard_setup();
    let mut scr_data = Vec::new();
    for i in 0..100 {
        scr_data.extend_from_slice(&make_ts_packet(
            VIDEO_PID,
            false,
            u8::try_from(i % 16).unwrap(),
            2,
            false,
            &[0x33],
        ));
    }
    ingress2
        .ingest(offset2, &scr_data)
        .expect("100 scrambled packets");
    offset2 = offset2.saturating_add(i64::try_from(scr_data.len()).unwrap());

    let facts3 = ingress2.facts();
    assert_eq!(facts3.clear_packets, 0);
    assert_eq!(facts3.scrambled_packets, 100);
    assert_eq!(facts3.clear_run, 0);
    assert!(facts3.scrambled_confirmed);

    // 1 clear packet arrives: breaks confirmation.
    let clear_pkt = make_ts_packet(VIDEO_PID, false, 4, 0, false, &[0x44]);
    ingress2
        .ingest(offset2, &clear_pkt)
        .expect("clear packet after 100 scrambled");

    let facts4 = ingress2.facts();
    assert_eq!(facts4.clear_packets, 1);
    assert_eq!(facts4.clear_run, 1);
    assert!(!facts4.scrambled_confirmed);
}

#[test]
fn tei_packet_does_not_increment_counters_and_quarantines() {
    let (mut ingress, mut offset) = standard_setup();

    // Feed 3 clear packets.
    let mut clear_data = Vec::new();
    for cc in 0..3 {
        clear_data.extend_from_slice(&make_ts_packet(VIDEO_PID, false, cc, 0, false, &[0x55]));
    }
    ingress.ingest(offset, &clear_data).expect("clear data");
    offset = offset.saturating_add(i64::try_from(clear_data.len()).unwrap());

    let facts1 = ingress.facts();
    assert_eq!(facts1.clear_packets, 3);
    assert_eq!(facts1.clear_run, 3);

    // Packet with TEI = 1 arrives.
    let tei_pkt = make_ts_packet(VIDEO_PID, false, 3, 0, true, &[0x66]); // tei = true
    let outcome = ingress.ingest(offset, &tei_pkt).expect("tei packet");
    offset += PACKET_LEN_I64;

    assert_eq!(outcome.feeds.len(), 0);
    let facts2 = ingress.facts();
    // Neither clear nor scrambled incremented, clear_run unmodified!
    assert_eq!(facts2.clear_packets, 3);
    assert_eq!(facts2.scrambled_packets, 0);
    assert_eq!(facts2.clear_run, 3);
    assert!(facts2.awaiting_start);

    // Resync: valid PUSI packet restores stream.
    let valid_pusi = vec![0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x00, 0x00, 0x77];
    let p_resync = make_ts_packet(VIDEO_PID, true, 4, 0, false, &valid_pusi);
    let outcome_resync = ingress.ingest(offset, &p_resync).expect("resync pusi");

    assert_eq!(outcome_resync.feeds.len(), 1);
    assert!(outcome_resync.feeds[0].pusi);
    assert_eq!(outcome_resync.feeds[0].es, &[0x77]);

    let facts3 = ingress.facts();
    assert_eq!(facts3.clear_packets, 4);
    assert_eq!(facts3.clear_run, 4);
    assert!(!facts3.awaiting_start);
}
