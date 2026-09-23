// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

use super::video::{
    BitReader, SCRAMBLED_CONFIRMED_THRESHOLD, h264_slice_is_intra, mpeg2_picture_is_intra,
    remove_emulation_prevention, sei_has_recovery_point,
};
use super::{VideoEvent, VideoIngress};
use crate::psi::VideoCodec;
use crate::timing::{
    ByteOffset, DiscontinuityReason, ExtendedPts90k, Pid, RawDts33, RawPts33, TimelineEpoch,
    TimingField, TimingRecord, TimingResetScope,
};
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
    assert_eq!(facts3.scrambled_packets, SCRAMBLED_CONFIRMED_THRESHOLD);
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

#[test]
fn test_bit_reader_and_exp_golomb() {
    // 0 -> '1'
    let data_zero = [0b1000_0000];
    let mut r = BitReader::new(&data_zero);
    assert_eq!(r.read_ue(), Some(0));

    // 1 -> '010'
    let data_one = [0b0100_0000];
    let mut r = BitReader::new(&data_one);
    assert_eq!(r.read_ue(), Some(1));

    // 2 -> '011'
    let data_two = [0b0110_0000];
    let mut r = BitReader::new(&data_two);
    assert_eq!(r.read_ue(), Some(2));

    // Truncated: 00001 (expects 4 suffix bits, but only 3 remain)
    let data_trunc = [0b0000_1000];
    let mut r = BitReader::new(&data_trunc);
    assert_eq!(r.read_ue(), None);

    // Run of >32 leading zeros is rejected
    let data_zeros = [0x00; 5];
    let mut r = BitReader::new(&data_zeros);
    assert_eq!(r.read_ue(), None);
}

#[test]
fn test_remove_emulation_prevention() {
    // 00 00 03 01 -> 00 00 01
    let input = [0x00, 0x00, 0x03, 0x01];
    assert_eq!(remove_emulation_prevention(&input), vec![0x00, 0x00, 0x01]);

    // Non-emulation: 01 02 03
    let input2 = [0x01, 0x02, 0x03];
    assert_eq!(remove_emulation_prevention(&input2), vec![0x01, 0x02, 0x03]);

    // Multiple emulation bytes: 00 00 03 00 00 03
    let input3 = [0x00, 0x00, 0x03, 0x00, 0x00, 0x03];
    assert_eq!(
        remove_emulation_prevention(&input3),
        vec![0x00, 0x00, 0x00, 0x00]
    );
}

#[test]
fn test_h264_slice_is_intra() {
    // first_mb = 0 ('1'), slice_type = 2 (I-slice, '011') -> 1011 0000 = 0xB0
    assert_eq!(h264_slice_is_intra(&[0xB0]), (true, true));

    // first_mb = 0 ('1'), slice_type = 4 (SI-slice, '00101') -> 1001 0100 = 0x94
    assert_eq!(h264_slice_is_intra(&[0x94]), (true, true));

    // first_mb = 0 ('1'), slice_type = 0 (P-slice, '1') -> 1100 0000 = 0xC0
    assert_eq!(h264_slice_is_intra(&[0xC0]), (false, true));

    // first_mb = 0 ('1'), slice_type = 1 (B-slice, '010') -> 1010 0000 = 0xA0
    assert_eq!(h264_slice_is_intra(&[0xA0]), (false, true));

    // Malformed / truncated
    assert_eq!(h264_slice_is_intra(&[]), (false, false));
    assert_eq!(h264_slice_is_intra(&[0x00]), (false, false));
}

#[test]
fn test_sei_has_recovery_point() {
    // Single SEI message: recovery_point (payload_type = 6, size = 1, body = [0x80])
    let sei1 = [0x06, 0x01, 0x80];
    assert!(sei_has_recovery_point(&sei1));

    // Multiple SEI messages: pic_timing (type 1, size 2) then recovery_point (type 6, size 1)
    let sei2 = [0x01, 0x02, 0x11, 0x22, 0x06, 0x01, 0x80];
    assert!(sei_has_recovery_point(&sei2));

    // No recovery point: only pic_timing (type 1)
    let sei3 = [0x01, 0x02, 0x11, 0x22];
    assert!(!sei_has_recovery_point(&sei3));

    // Extended length runs (payload_type = 255 + 6 = 261 != 6)
    let sei4 = [0xFF, 0x06, 0x01, 0x00];
    assert!(!sei_has_recovery_point(&sei4));

    // Empty or trailing bits only
    assert!(!sei_has_recovery_point(&[]));
    assert!(!sei_has_recovery_point(&[0x80]));
}

#[test]
fn test_mpeg2_picture_is_intra() {
    // picture_coding_type = 1 (I-Frame): bits 5..3 of byte 1 -> 0x08
    assert_eq!(mpeg2_picture_is_intra(&[0x00, 0x08]), (true, true));

    // picture_coding_type = 2 (P-Frame): 0x10
    assert_eq!(mpeg2_picture_is_intra(&[0x00, 0x10]), (false, true));

    // picture_coding_type = 3 (B-Frame): 0x18
    assert_eq!(mpeg2_picture_is_intra(&[0x00, 0x18]), (false, true));

    // Invalid picture coding type 0 or 4
    assert_eq!(mpeg2_picture_is_intra(&[0x00, 0x00]), (false, false));
    assert_eq!(mpeg2_picture_is_intra(&[0x00, 0x20]), (false, false));

    // Truncated (< 2 bytes)
    assert_eq!(mpeg2_picture_is_intra(&[0x00]), (false, false));
    assert_eq!(mpeg2_picture_is_intra(&[]), (false, false));
}

// --- The 4 Explicit User-Required Regressions --------------------------------

fn make_pusi_sps_pps_payload() -> Vec<u8> {
    vec![
        0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x00, 0x00, // PES start
        0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1E, // SPS NAL (type 7)
        0x00, 0x00, 0x01, 0x68, 0xCE, 0x38, 0x80, // PPS NAL (type 8)
    ]
}

#[test]
fn regression_valid_pusi_then_tei_pusi_clears_offset_and_param_sets() {
    let (mut ingress, mut offset) = standard_setup();

    // 1. Valid PUSI with SPS and PPS arrives at offset 376.
    let payload1 = make_pusi_sps_pps_payload();
    let pusi_pkt = make_ts_packet(VIDEO_PID, true, 0, 0, false, &payload1);
    ingress.ingest(offset, &pusi_pkt).expect("valid pusi");
    let facts1 = ingress.facts();
    assert_eq!(facts1.current_pes_offset, Some(offset));
    assert!(facts1.parameter_sets_seen);
    assert_eq!(facts1.clear_packets, 1);
    assert_eq!(facts1.scrambled_packets, 0);
    assert_eq!(facts1.clear_run, 1);
    assert!(!facts1.awaiting_start);

    offset += PACKET_LEN_I64;

    // 2. TEI-damaged PUSI arrives.
    let tei_pusi_pkt = make_ts_packet(VIDEO_PID, true, 1, 0, true, &payload1); // tei = true
    ingress.ingest(offset, &tei_pusi_pkt).expect("tei pusi");
    let facts2 = ingress.facts();

    // Invariant: current_pes_offset cleared, parameter_sets_seen cleared, awaiting_start entered,
    // counters NOT incremented by TEI packet.
    assert_eq!(facts2.current_pes_offset, None);
    assert!(!facts2.parameter_sets_seen);
    assert!(facts2.awaiting_start);
    assert_eq!(facts2.clear_packets, 1);
    assert_eq!(facts2.scrambled_packets, 0);
    assert_eq!(facts2.clear_run, 1);
}

#[test]
fn regression_valid_pusi_then_scrambled_pusi_clears_offset_and_param_sets() {
    let (mut ingress, mut offset) = standard_setup();

    // 1. Valid PUSI with SPS and PPS.
    let payload1 = make_pusi_sps_pps_payload();
    let pusi_pkt = make_ts_packet(VIDEO_PID, true, 0, 0, false, &payload1);
    ingress.ingest(offset, &pusi_pkt).expect("valid pusi");
    let facts1 = ingress.facts();
    assert_eq!(facts1.current_pes_offset, Some(offset));
    assert!(facts1.parameter_sets_seen);

    offset += PACKET_LEN_I64;

    // 2. Scrambled PUSI arrives (scrambling_control = 2).
    let scrambled_pusi_pkt = make_ts_packet(VIDEO_PID, true, 1, 2, false, &payload1);
    ingress
        .ingest(offset, &scrambled_pusi_pkt)
        .expect("scrambled pusi");
    let facts2 = ingress.facts();

    // Invariant: current_pes_offset cleared, parameter_sets_seen cleared, vscr incremented,
    // vrun reset to 0, awaiting_start entered.
    assert_eq!(facts2.current_pes_offset, None);
    assert!(!facts2.parameter_sets_seen);
    assert_eq!(facts2.scrambled_packets, 1);
    assert_eq!(facts2.clear_run, 0);
    assert!(facts2.awaiting_start);
}

#[test]
fn regression_valid_pusi_then_invalid_clear_pusi_clears_offset_and_param_sets() {
    let (mut ingress, mut offset) = standard_setup();

    // 1. Valid PUSI with SPS and PPS.
    let payload1 = make_pusi_sps_pps_payload();
    let pusi_pkt = make_ts_packet(VIDEO_PID, true, 0, 0, false, &payload1);
    ingress.ingest(offset, &pusi_pkt).expect("valid pusi");
    let facts1 = ingress.facts();
    assert_eq!(facts1.current_pes_offset, Some(offset));
    assert!(facts1.parameter_sets_seen);

    offset += PACKET_LEN_I64;

    // 2. Invalid clear PUSI arrives (bad prefix: 00 00 00 00).
    let invalid_payload = vec![0x00, 0x00, 0x00, 0x00, 0x11, 0x22];
    let invalid_pusi_pkt = make_ts_packet(VIDEO_PID, true, 1, 0, false, &invalid_payload);
    ingress
        .ingest(offset, &invalid_pusi_pkt)
        .expect("invalid pusi");
    let facts2 = ingress.facts();

    // Invariant: current_pes_offset cleared, parameter_sets_seen cleared, awaiting_start entered.
    assert_eq!(facts2.current_pes_offset, None);
    assert!(!facts2.parameter_sets_seen);
    assert!(facts2.awaiting_start);
}

#[test]
fn regression_valid_pusi_then_duplicate_pusi_preserves_offset_and_param_sets() {
    let (mut ingress, offset) = standard_setup();

    // 1. Valid PUSI with SPS and PPS.
    let payload1 = make_pusi_sps_pps_payload();
    let pusi_pkt = make_ts_packet(VIDEO_PID, true, 0, 0, false, &payload1);
    ingress.ingest(offset, &pusi_pkt).expect("valid pusi");
    let facts1 = ingress.facts();
    assert_eq!(facts1.current_pes_offset, Some(offset));
    assert!(facts1.parameter_sets_seen);

    let dup_offset = offset + PACKET_LEN_I64;

    // 2. Exact duplicate of the same PUSI packet arrives.
    ingress
        .ingest(dup_offset, &pusi_pkt)
        .expect("duplicate pusi");
    let facts2 = ingress.facts();

    // Invariant: Duplicate packet is discarded; current_pes_offset and parameter_sets_seen
    // remain completely preserved!
    assert_eq!(facts2.current_pes_offset, Some(offset));
    assert!(facts2.parameter_sets_seen);
    assert_eq!(facts2.clear_packets, 1);
    assert_eq!(facts2.clear_run, 1);
    assert!(!facts2.awaiting_start);
}

#[test]
fn continuation_spanning_nal_units_across_packets() {
    let (mut ingress, mut offset) = standard_setup();

    // Packet 1: PUSI with PES header and start of SPS (startcode + NAL header + 2 bytes).
    let pusi_payload = vec![
        0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x00, 0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00,
    ];
    let p1 = make_ts_packet(VIDEO_PID, true, 0, 0, false, &pusi_payload);
    ingress.ingest(offset, &p1).expect("p1");
    offset += PACKET_LEN_I64;

    let facts1 = ingress.facts();
    assert!(facts1.pes_has_sps);
    assert!(!facts1.pes_has_pps);
    assert!(!facts1.parameter_sets_seen);

    // Packet 2: Continuation packet with rest of SPS, then PPS.
    let cont_payload = vec![0x1E, 0x00, 0x00, 0x01, 0x68, 0xCE, 0x38, 0x80];
    let p2 = make_ts_packet(VIDEO_PID, false, 1, 0, false, &cont_payload);
    ingress.ingest(offset, &p2).expect("p2");

    let facts2 = ingress.facts();
    assert!(facts2.pes_has_sps);
    assert!(facts2.pes_has_pps);
    assert!(facts2.parameter_sets_seen);
}

#[test]
fn transport_break_on_continuation_resets_annex_b_scanner() {
    let (mut ingress, mut offset) = standard_setup();

    // PUSI with valid PES start.
    let pusi_payload = vec![
        0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x00, 0x00, 0x00, 0x00, 0x01,
        0x67, // SPS start
    ];
    let p1 = make_ts_packet(VIDEO_PID, true, 0, 0, false, &pusi_payload);
    ingress.ingest(offset, &p1).expect("p1");
    offset += PACKET_LEN_I64;

    // Continuation with CC jump (cc 0 -> cc 5, without DI): Broken gap.
    let cont_payload = vec![0x42, 0x00];
    let p2 = make_ts_packet(VIDEO_PID, false, 5, 0, false, &cont_payload);
    ingress.ingest(offset, &p2).expect("p2 broken");

    let facts = ingress.facts();
    assert!(facts.awaiting_start);
}

#[test]
fn regression_short_malformed_slice_before_pusi_increments_unreadable_slices() {
    let (mut ingress, mut offset) = standard_setup();

    // PES A: starts an H.264 non-IDR slice (type 1), but provides only 1 malformed byte (0x00).
    // The capture budget is 12 bytes, so the capture remains pending at the end of PES A.
    let first_pusi_payload = vec![
        0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x00, 0x00, // PES start
        0x00, 0x00, 0x01, 0x01, // H.264 non-IDR slice startcode + header byte
        0x00, // 1 byte captured (budget is 12 -> capture pending)
    ];
    let p_a = make_ts_packet(VIDEO_PID, true, 0, 0, false, &first_pusi_payload);
    ingress.ingest(offset, &p_a).expect("pes a");
    offset += PACKET_LEN_I64;

    let facts_mid = ingress.facts();
    // Budget was not reached yet, so unreadable_slices is still 0 before PES boundary.
    assert_eq!(facts_mid.unreadable_slices, 0);

    // PES B: arrives as a new PUSI packet.
    let next_pusi_payload = vec![
        0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x00, 0x00, // PES start
        0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1E, // SPS
    ];
    let p_b = make_ts_packet(VIDEO_PID, true, 1, 0, false, &next_pusi_payload);
    ingress.ingest(offset, &p_b).expect("pes b");

    let facts_after = ingress.facts();
    // Invariant: at the PUSI boundary of PES B, consume_capture() was called on PES A's
    // pending capture. The malformed slice header fails parsing and increments unreadable_slices!
    assert_eq!(facts_after.unreadable_slices, 1);
}

#[test]
fn regression_pending_capture_aborted_on_transport_break_without_consume() {
    let (mut ingress, mut offset) = standard_setup();

    // PES A: starts an H.264 non-IDR slice with 1 byte (pending capture).
    let pusi_payload = vec![
        0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x00, 0x00, 0x00, 0x00, 0x01, 0x01, 0x00,
    ];
    let p1 = make_ts_packet(VIDEO_PID, true, 0, 0, false, &pusi_payload);
    ingress.ingest(offset, &p1).expect("p1");
    offset += PACKET_LEN_I64;

    assert_eq!(ingress.facts().unreadable_slices, 0);

    // Continuation packet arrives with an unannounced CC gap (cc 0 -> cc 5, without DI): Broken.
    let cont_payload = vec![0x11, 0x22];
    let p2 = make_ts_packet(VIDEO_PID, false, 5, 0, false, &cont_payload);
    ingress.ingest(offset, &p2).expect("p2 broken");

    let facts = ingress.facts();
    assert!(facts.awaiting_start);
    // Invariant: On transport breaks (loss in transit), the pending capture is aborted
    // (discarded) rather than consumed. unreadable_slices stays 0!
    assert_eq!(facts.unreadable_slices, 0);
}

#[test]
fn test_sei_cross_packet_emulation_prevention_and_recovery_point() {
    let (mut ingress, mut offset) = standard_setup();

    // Packet 1: PUSI with PES header + SEI NAL header (0x06) + message 1 (type=1, size=4).
    // Message 1 payload has 0xAA, followed by [0x00, 0x00] right at the end of packet 1.
    let p1_payload = vec![
        0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x00, 0x00, // PES start
        0x00, 0x00, 0x01, 0x06, // SEI startcode + NAL header (H.264 SEI = 6)
        0x01, // payload_type = 1
        0x04, // payload_size = 4
        0xAA, // 1st payload byte
        0x00, 0x00, // zero bytes at end of packet 1
    ];
    let p1 = make_ts_packet(VIDEO_PID, true, 0, 0, false, &p1_payload);
    ingress.ingest(offset, &p1).expect("p1");
    offset += PACKET_LEN_I64;

    // In packet 1, recovery point has NOT been seen yet.
    assert!(!ingress.facts().pes_has_recovery_point);

    // Packet 2: Continuation with emulation prevention byte 0x03 followed by remaining payload byte 0xBB
    // of message 1, and then message 2 which is the recovery point (type=6, size=2, 0x80, 0x80).
    // 00 00 03 BB -> emulation prevention 03 is stripped, leaving 00 00 BB as RBSP bytes.
    let p2_payload = vec![
        0x03, 0xBB, // completes message 1 (4 bytes: AA, 00, 00, BB)
        0x06, // message 2: payload_type = 6 (recovery_point)
        0x02, // payload_size = 2
        0x80, 0x80, // payload
    ];
    let p2 = make_ts_packet(VIDEO_PID, false, 1, 0, false, &p2_payload);
    ingress.ingest(offset, &p2).expect("p2");

    // After parsing recovery_point SEI following the cross-packet emulation prevention:
    assert!(ingress.facts().pes_has_recovery_point);
}

#[test]
fn test_sei_startcode_boundary_non_consumption() {
    let (mut ingress, offset) = standard_setup();

    // Ingest an SEI message with declared size 10, but terminated early by an Annex-B startcode
    // for SPS (0x67).
    let payload = vec![
        0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x00, 0x00, // PES start
        0x00, 0x00, 0x01, 0x06, // SEI NAL
        0x01, // payload_type = 1
        0x0A, // payload_size = 10 (expects 10 bytes)
        0xAA, 0xBB, // 2 payload bytes
        0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1E, // Annex-B startcode for SPS!
    ];
    let p = make_ts_packet(VIDEO_PID, true, 0, 0, false, &payload);
    ingress.ingest(offset, &p).expect("ingest");

    // The SEI parser must NOT consume the startcode bytes (00 00 01) or SPS header (67).
    // SPS must be recognized cleanly!
    assert!(ingress.facts().pes_has_sps);
}

#[test]
fn test_unreadable_slice_does_not_increment_predicted_rejected() {
    let (mut ingress, mut offset) = standard_setup();

    // PES 1: SPS + PPS and an unreadable slice header (NAL type 1 with invalid data).
    let p1_payload = vec![
        0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x00, 0x00, // PES start
        0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1E, // SPS
        0x00, 0x00, 0x01, 0x68, 0xCE, 0x38, 0x80, // PPS
        0x00, 0x00, 0x01, 0x01, 0x00, // malformed slice header
    ];
    let p1 = make_ts_packet(VIDEO_PID, true, 0, 0, false, &p1_payload);
    ingress.ingest(offset, &p1).expect("p1");
    offset += PACKET_LEN_I64;

    // PES 2: Next PUSI triggers finalization of PES 1's AU.
    let p2_payload = vec![
        0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x00, 0x00, // PES start
        0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1E, // SPS
    ];
    let p2 = make_ts_packet(VIDEO_PID, true, 1, 0, false, &p2_payload);
    ingress.ingest(offset, &p2).expect("p2");

    let facts = ingress.facts();
    assert_eq!(facts.unreadable_slices, 1);
    // Unreadable slice must NOT be counted as predicted_rejected!
    assert_eq!(facts.predicted_rejected, 0);
}

#[test]
fn test_provisional_rap_invalidation_on_scrambled_continuation_and_gap() {
    let (mut ingress, mut offset) = standard_setup();

    // 1. Clear IDR AU -> RAP is emitted.
    let p1_payload = vec![
        0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x00, 0x00, // PES start
        0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1E, // SPS
        0x00, 0x00, 0x01, 0x68, 0xCE, 0x38, 0x80, // PPS
        0x00, 0x00, 0x01, 0x65, 0x88, 0x84, 0x21, 0xA0, 0x33, 0xFF, // IDR slice
    ];
    let p1 = make_ts_packet(VIDEO_PID, true, 0, 0, false, &p1_payload);
    let out1 = ingress.ingest(offset, &p1).expect("p1");
    assert!(out1.events.contains(&VideoEvent::RandomAccessPoint {
        offset: 376,
        joinable: true,
    }));
    assert_eq!(ingress.facts().clean_rap_count, 1);
    offset += PACKET_LEN_I64;

    // 2. Continuation packet is scrambled -> provisional RAP is invalidated.
    let p2_payload = vec![0x11, 0x22, 0x33];
    let p2 = make_ts_packet(VIDEO_PID, false, 1, 2, false, &p2_payload); // scrambling = 2
    let out2 = ingress.ingest(offset, &p2).expect("p2 scrambled");
    assert!(
        out2.events
            .contains(&VideoEvent::RandomAccessPointInvalidated { offset: 376 })
    );
    assert_eq!(ingress.facts().clean_rap_count, 0);
}

#[test]
fn test_set_target_program_emits_identity_events() {
    let (mut ingress, _offset) = standard_setup();

    // In standard_setup, target is 1. Program 1 has an active epoch from PMT.
    assert_eq!(ingress.timeline().active_epoch(), Some(TimelineEpoch(0)));

    // Calling with the same target program 1 is a strict no-op:
    // no events emitted, active epoch preserved, follower preserved.
    let events_same = ingress.set_target_program(1);
    assert!(events_same.is_empty());
    assert_eq!(ingress.timeline().active_epoch(), Some(TimelineEpoch(0)));
    assert_eq!(ingress.facts().pid, VIDEO_PID);

    // Changing to program 2 emits ProgramIdentityChanged, resets the follower,
    // and cleanly deactivates the timeline (active_epoch = None).
    let events = ingress.set_target_program(2);
    assert_eq!(events, vec![VideoEvent::ProgramIdentityChanged]);
    assert_eq!(ingress.timeline().active_epoch(), None);
    let facts = ingress.facts();
    assert_eq!(facts.pid, 0);
    assert_eq!(facts.codec, VideoCodec::Unknown);

    // Calling again with the same target program 2 is a no-op:
    // no events emitted, active_epoch remains None.
    let events_noop = ingress.set_target_program(2);
    assert!(events_noop.is_empty());
    assert_eq!(ingress.timeline().active_epoch(), None);
}

#[test]
#[allow(clippy::too_many_lines, clippy::similar_names)]
fn test_set_target_program_idempotency_and_reactivation_lifecycle() {
    let (mut ingress, mut offset) = standard_setup();

    // 1. Establish PCR and PES timing in Epoch 0
    let make_pcr = |pid: u16, base: u64, ext: u16| -> Vec<u8> {
        let mut pkt = vec![0xFF; TS_PACKET_LEN];
        pkt[0] = 0x47;
        pkt[1] = u8::try_from((pid >> 8) & 0x1F).unwrap();
        pkt[2] = u8::try_from(pid & 0xFF).unwrap();
        pkt[3] = 0x20;
        pkt[4] = 7;
        pkt[5] = 0x10;
        pkt[6] = u8::try_from((base >> 25) & 0xFF).unwrap();
        pkt[7] = u8::try_from((base >> 17) & 0xFF).unwrap();
        pkt[8] = u8::try_from((base >> 9) & 0xFF).unwrap();
        pkt[9] = u8::try_from((base >> 1) & 0xFF).unwrap();
        pkt[10] = u8::try_from(((base & 1) << 7) | 0x7E | ((u64::from(ext) >> 8) & 1)).unwrap();
        pkt[11] = u8::try_from(ext & 0xFF).unwrap();
        pkt
    };

    let pcr1 = make_pcr(VIDEO_PID, 90_000, 0); // 90,000 * 300 = 27,000,000
    ingress.ingest(offset, &pcr1).expect("ingest pcr1");
    offset += PACKET_LEN_I64;

    let target_pts = 90_000;
    let mut pes_payload = vec![0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05];
    pes_payload.extend_from_slice(&encode_ts(0b0010, target_pts));
    pes_payload.extend_from_slice(&[0x00, 0x00, 0x00, 0x01, 0x09, 0xF0]); // AUD NAL
    let pkt_pes = make_ts_packet(VIDEO_PID, true, 0, 0, false, &pes_payload);
    let out_pes = ingress.ingest(offset, &pkt_pes).expect("ingest pes");
    offset += PACKET_LEN_I64;

    assert_eq!(ingress.timeline().active_epoch(), Some(TimelineEpoch(0)));
    assert_eq!(out_pes.timing_records.len(), 1);

    // 2. Control plane: call set_target_program(1) with the SAME target
    let events_same = ingress.set_target_program(1);
    assert!(events_same.is_empty(), "same target must emit no events");
    assert_eq!(
        ingress.timeline().active_epoch(),
        Some(TimelineEpoch(0)),
        "active epoch must be preserved"
    );

    // Ingest next video PES packet: unwrapper continues seamlessly in Epoch 0
    let next_pts = 93_000;
    let mut pes_payload2 = vec![0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05];
    pes_payload2.extend_from_slice(&encode_ts(0b0010, next_pts));
    pes_payload2.extend_from_slice(&[0x00, 0x00, 0x00, 0x01, 0x09, 0xF0]);
    let pkt_pes2 = make_ts_packet(VIDEO_PID, true, 1, 0, false, &pes_payload2);
    let out_pes2 = ingress.ingest(offset, &pkt_pes2).expect("ingest pes2");
    offset += PACKET_LEN_I64;

    assert_eq!(out_pes2.timing_records.len(), 1);
    if let TimingRecord::Pes(ref pes) = out_pes2.timing_records[0] {
        assert_eq!(pes.epoch, TimelineEpoch(0));
        assert_eq!(
            pes.pts,
            Some(ExtendedPts90k::new(i64::try_from(next_pts).unwrap()))
        );
    } else {
        panic!("expected PES timing record");
    }

    // 3. Control plane: switch to program 2
    let events_switch = ingress.set_target_program(2);
    assert_eq!(events_switch, vec![VideoEvent::ProgramIdentityChanged]);
    assert_eq!(
        ingress.timeline().active_epoch(),
        None,
        "timeline must be deactivated"
    );

    // Repeated switch to program 2 is idempotent
    let events_switch_repeat = ingress.set_target_program(2);
    assert!(events_switch_repeat.is_empty());
    assert_eq!(ingress.timeline().active_epoch(), None);

    // 4. Transport plane: ingest PAT and PMT for program 2
    let prog2: u16 = 2;
    let pmt2_pid: u16 = 0x0200;
    let vpid2: u16 = 0x0201;

    let pat_sect = section(
        0x00,
        1,
        0,
        &[
            u8::try_from(prog2 >> 8).unwrap(),
            u8::try_from(prog2 & 0xFF).unwrap(),
            0xE0 | u8::try_from(pmt2_pid >> 8).unwrap(),
            u8::try_from(pmt2_pid & 0xFF).unwrap(),
        ],
    );
    let mut pat2_payload = vec![0x00];
    pat2_payload.extend_from_slice(&pat_sect);
    let pat2_pkt = make_ts_packet(0x0000, true, 0, 0, false, &pat2_payload);

    let pmt2_body = vec![
        0xE0 | u8::try_from(vpid2 >> 8).unwrap(),
        u8::try_from(vpid2 & 0xFF).unwrap(),
        0xF0,
        0x00, // program_info_length
        0x1B, // H.264
        0xE0 | u8::try_from(vpid2 >> 8).unwrap(),
        u8::try_from(vpid2 & 0xFF).unwrap(),
        0xF0,
        0x00, // ES info length: 0
    ];
    let pmt2_sect = section(0x02, prog2, 0, &pmt2_body);
    let mut pmt2_payload = vec![0x00];
    pmt2_payload.extend_from_slice(&pmt2_sect);
    let pmt2_pkt = make_ts_packet(pmt2_pid, true, 0, 0, false, &pmt2_payload);

    let mut prog2_ts = pat2_pkt;
    prog2_ts.extend_from_slice(&pmt2_pkt);

    let pmt_offset = offset + PACKET_LEN_I64;
    let out_reactivate = ingress.ingest(offset, &prog2_ts).expect("ingest pat/pmt");
    let _ = out_reactivate;

    assert_eq!(
        ingress.timeline().active_epoch(),
        Some(TimelineEpoch(1)),
        "epoch 1 must be minted upon PMT acceptance"
    );

    // Transport plane records:
    // 1. PAT acceptance reports program identity changed at offset (before: None, after: None)
    // 2. PMT acceptance reports program identity changed at pmt_offset and mints epoch 1 (before: None, after: 1)
    let disc_records: Vec<_> = out_reactivate
        .timing_records
        .iter()
        .filter_map(|r| match r {
            TimingRecord::Discontinuity {
                scope,
                reason,
                observed_at,
                epoch_before,
                epoch_after,
            } => Some((*scope, *reason, *observed_at, *epoch_before, *epoch_after)),
            _ => None,
        })
        .collect();

    assert_eq!(
        disc_records.len(),
        2,
        "must emit 2 transport discontinuity records (PAT and PMT)"
    );
    assert_eq!(
        disc_records[0],
        (
            TimingResetScope::Program,
            DiscontinuityReason::ProgramIdentityChanged,
            ByteOffset::new(offset),
            None,
            None
        )
    );
    assert_eq!(
        disc_records[1],
        (
            TimingResetScope::Program,
            DiscontinuityReason::ProgramIdentityChanged,
            ByteOffset::new(pmt_offset),
            None,
            Some(TimelineEpoch(1))
        )
    );
}

#[test]
fn test_ingress_timing_tracking() {
    let (mut ingress, mut offset) = standard_setup();

    assert_eq!(ingress.timing_snapshot().pcr_pid, Pid::new(VIDEO_PID));
    assert_eq!(ingress.timing_snapshot().pcr_count, 0);

    let make_pcr = |pid: u16, base: u64, ext: u16| -> Vec<u8> {
        let mut pkt = vec![0xFF; TS_PACKET_LEN];
        pkt[0] = 0x47;
        pkt[1] = u8::try_from((pid >> 8) & 0x1F).unwrap();
        pkt[2] = u8::try_from(pid & 0xFF).unwrap();
        pkt[3] = 0x20;
        pkt[4] = 7;
        pkt[5] = 0x10;
        pkt[6] = u8::try_from((base >> 25) & 0xFF).unwrap();
        pkt[7] = u8::try_from((base >> 17) & 0xFF).unwrap();
        pkt[8] = u8::try_from((base >> 9) & 0xFF).unwrap();
        pkt[9] = u8::try_from((base >> 1) & 0xFF).unwrap();
        pkt[10] = (u8::try_from((base & 0x01) << 7).unwrap())
            | 0x7E
            | (u8::try_from((ext >> 8) & 0x01).unwrap());
        pkt[11] = u8::try_from(ext & 0xFF).unwrap();
        pkt
    };

    // First PCR packet on VIDEO_PID
    let p1 = make_pcr(VIDEO_PID, 0, 0);
    ingress.ingest(offset, &p1).expect("p1");
    offset += 20_000;

    // Second PCR packet (40ms later = 1,080,000 ticks, 20,000 bytes delta = 4 Mbps)
    let delta_ticks: u64 = 1_080_000;
    let p2 = make_pcr(VIDEO_PID, delta_ticks / 300, (delta_ticks % 300) as u16);
    ingress.ingest(offset, &p2).expect("p2");

    let snap = ingress.timing_snapshot();
    assert_eq!(snap.pcr_count, 2);
    assert_eq!(snap.bitrate_bps.get(), 4_000_000);
    assert_eq!(snap.last_pcr_offset, ByteOffset::new(offset));
}

fn encode_ts(prefix: u8, val: u64) -> [u8; 5] {
    let mut b = [0u8; 5];
    b[0] = (prefix << 4) | (u8::try_from((val >> 29) & 0x0E).unwrap()) | 1;
    b[1] = u8::try_from((val >> 22) & 0xFF).unwrap();
    b[2] = (u8::try_from((val >> 14) & 0xFE).unwrap()) | 1;
    b[3] = u8::try_from((val >> 7) & 0xFF).unwrap();
    b[4] = (u8::try_from((val << 1) & 0xFE).unwrap()) | 1;
    b
}

#[test]
fn test_video_timing_event_single_and_split() {
    let (mut ingress, mut offset) = standard_setup();

    // 1. Single packet PUSI with complete PES header and PTS (flags == 10)
    let target_pts = 90_000 * 5; // 5.0 seconds
    let mut pes_payload = vec![0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05];
    pes_payload.extend_from_slice(&encode_ts(0b0010, target_pts));
    pes_payload.extend_from_slice(&[0x00, 0x00, 0x00, 0x01, 0x09, 0xF0]); // AUD NAL
    let pkt1 = make_ts_packet(VIDEO_PID, true, 0, 0, false, &pes_payload);

    let outcome1 = ingress.ingest(offset, &pkt1).expect("ingest pkt1");
    assert_eq!(outcome1.timing_events.len(), 1);
    let evt1 = outcome1.timing_events[0];
    assert_eq!(evt1.pid, Pid::new(VIDEO_PID).unwrap());
    assert_eq!(evt1.observed_at, ByteOffset::new(offset));
    assert_eq!(evt1.subject_at, ByteOffset::new(offset));
    assert_eq!(
        evt1.timing.pts,
        TimingField::Valid(RawPts33::new(target_pts).unwrap())
    );
    assert!(evt1.timing.dts.is_absent());

    offset += PACKET_LEN_I64;

    // 2. Split PES header: PUSI packet contains only 10 bytes of PES (9 fixed + 1 PTS byte)
    let split_pts = 90_000 * 6;
    let enc_pts = encode_ts(0b0010, split_pts);
    let mut pusi_payload = vec![0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05];
    pusi_payload.push(enc_pts[0]);
    let pkt2 = make_ts_packet(VIDEO_PID, true, 1, 0, false, &pusi_payload);

    let pusi_offset = offset;
    let outcome2 = ingress.ingest(offset, &pkt2).expect("ingest pkt2");
    // Prefix not yet complete -> 0 timing events emitted
    assert_eq!(outcome2.timing_events.len(), 0);

    offset += PACKET_LEN_I64;

    // Continuation packet contains remaining 4 bytes of PTS + ES bytes
    let mut cont_payload = enc_pts[1..].to_vec();
    cont_payload.extend_from_slice(&[0x00, 0x00, 0x00, 0x01, 0x09, 0xF0]);
    let pkt3 = make_ts_packet(VIDEO_PID, false, 2, 0, false, &cont_payload);

    let outcome3 = ingress.ingest(offset, &pkt3).expect("ingest pkt3");
    // Prefix complete -> timing event emitted with dual coordinates!
    assert_eq!(outcome3.timing_events.len(), 1);
    let evt3 = outcome3.timing_events[0];
    assert_eq!(evt3.pid, Pid::new(VIDEO_PID).unwrap());
    assert_eq!(evt3.observed_at, ByteOffset::new(offset)); // observed on continuation packet
    assert_eq!(evt3.subject_at, ByteOffset::new(pusi_offset)); // subject at PUSI packet
    assert_eq!(
        evt3.timing.pts,
        TimingField::Valid(RawPts33::new(split_pts).unwrap())
    );
}

#[test]
fn test_audio_timing_events_on_all_pmt_declared_tracks() {
    let mut ingress = VideoIngress::new(PROGRAM);
    let mut data = pat_packet(0);

    // PMT declares:
    // Video: PID 0x100
    // Audio 1 (AC-3, stream_type 0x06 with DVB AC-3): PID 0x101
    // Audio 2 (MP2, stream_type 0x03, non-observable): PID 0x102
    let audio1_pid = 0x0101;
    let audio2_pid = 0x0102;
    let pmt_bytes = vec![
        0xE0 | u8::try_from(VIDEO_PID >> 8).unwrap(),
        u8::try_from(VIDEO_PID & 0xFF).unwrap(),
        0xF0,
        0x00, // program_info_length
        // Stream 1: Video (0x1B = H.264)
        0x1B,
        0xE0 | u8::try_from(VIDEO_PID >> 8).unwrap(),
        u8::try_from(VIDEO_PID & 0xFF).unwrap(),
        0xF0,
        0x00,
        // Stream 2: Audio 1 (0x06 with AC-3 descriptor tag 0x6A)
        0x06,
        0xE0 | u8::try_from(audio1_pid >> 8).unwrap(),
        u8::try_from(audio1_pid & 0xFF).unwrap(),
        0xF0,
        0x03, // descriptor length 3
        0x6A, // DVB AC-3 descriptor
        0x01, // descriptor length 1
        0x00, // component type
        // Stream 3: Audio 2 (0x03 = ISO/IEC 11172-3 Audio / MP2, non-observable)
        0x03,
        0xE0 | u8::try_from(audio2_pid >> 8).unwrap(),
        u8::try_from(audio2_pid & 0xFF).unwrap(),
        0xF0,
        0x00,
    ];
    let sect = section(0x02, PROGRAM, 0, &pmt_bytes);
    let mut ts_payload = vec![0x00];
    ts_payload.extend_from_slice(&sect);
    data.extend_from_slice(&make_ts_packet(PMT_PID, true, 0, 0, false, &ts_payload));

    let outcome = ingress.ingest(0, &data).expect("setup");
    let mut offset = outcome.processed_through;

    // 1. Packet on Audio 1 (AC-3, PID 0x101) with PTS + DTS
    let ac3_pts_90k = 90_000 * 10;
    let ac3_decoding_ts = 90_000 * 9;
    let mut ac3_payload = vec![0x00, 0x00, 0x01, 0xBD, 0x00, 0x00, 0x80, 0xC0, 0x0A];
    ac3_payload.extend_from_slice(&encode_ts(0b0011, ac3_pts_90k));
    ac3_payload.extend_from_slice(&encode_ts(0b0001, ac3_decoding_ts));
    ac3_payload.extend_from_slice(&[0x0B, 0x77]); // AC-3 sync
    let pkt_ac3 = make_ts_packet(audio1_pid, true, 0, 0, false, &ac3_payload);

    let outcome_ac3 = ingress.ingest(offset, &pkt_ac3).expect("ingest ac3");
    assert_eq!(outcome_ac3.timing_events.len(), 1);
    let evt_ac3 = outcome_ac3.timing_events[0];
    assert_eq!(evt_ac3.pid, Pid::new(audio1_pid).unwrap());
    assert_eq!(
        evt_ac3.timing.pts,
        TimingField::Valid(RawPts33::new(ac3_pts_90k).unwrap())
    );
    assert_eq!(
        evt_ac3.timing.dts,
        TimingField::Valid(RawDts33::new(ac3_decoding_ts).unwrap())
    );

    offset += PACKET_LEN_I64;

    // 2. Packet on Audio 2 (MP2, PID 0x102, non-observable) with PTS
    let mp2_pts = 90_000 * 15;
    let mut mp2_payload = vec![0x00, 0x00, 0x01, 0xC0, 0x00, 0x00, 0x80, 0x80, 0x05];
    mp2_payload.extend_from_slice(&encode_ts(0b0010, mp2_pts));
    mp2_payload.extend_from_slice(&[0xFF, 0xFD]); // MP2 sync
    let pkt_mp2 = make_ts_packet(audio2_pid, true, 0, 0, false, &mp2_payload);

    let outcome_mp2 = ingress.ingest(offset, &pkt_mp2).expect("ingest mp2");
    assert_eq!(outcome_mp2.timing_events.len(), 1);
    let evt_mp2 = outcome_mp2.timing_events[0];
    assert_eq!(evt_mp2.pid, Pid::new(audio2_pid).unwrap());
    assert_eq!(
        evt_mp2.timing.pts,
        TimingField::Valid(RawPts33::new(mp2_pts).unwrap())
    );
    assert!(evt_mp2.timing.dts.is_absent());
}

#[test]
fn test_timing_assembler_resets_on_tei_and_cc_break() {
    let (mut ingress, mut offset) = standard_setup();

    // Start a split PES on VIDEO_PID (9 fixed bytes + 1 PTS byte)
    let pts = 90_000 * 20;
    let enc_pts = encode_ts(0b0010, pts);
    let mut pusi_payload = vec![0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05];
    pusi_payload.push(enc_pts[0]);
    let pkt1 = make_ts_packet(VIDEO_PID, true, 1, 0, false, &pusi_payload);
    ingress.ingest(offset, &pkt1).expect("ingest pkt1");
    offset += PACKET_LEN_I64;

    // Send a TEI packet on VIDEO_PID
    let pkt_tei = make_ts_packet(VIDEO_PID, false, 2, 0, true, &[0xFF; 20]);
    ingress.ingest(offset, &pkt_tei).expect("ingest tei");
    offset += PACKET_LEN_I64;

    // Continuation packet arrives (CC = 3): because TEI reset the assembler,
    // this continuation packet must not complete the corrupted prior PES header!
    let mut cont_payload = enc_pts[1..].to_vec();
    cont_payload.extend_from_slice(&[0x00, 0x00, 0x00, 0x01, 0x09, 0xF0]);
    let pkt3 = make_ts_packet(VIDEO_PID, false, 3, 0, false, &cont_payload);
    let outcome = ingress.ingest(offset, &pkt3).expect("ingest pkt3");
    assert_eq!(outcome.timing_events.len(), 0);
}

#[test]
fn video_pusi_split_at_2_bytes_assembles_header_and_timing() {
    let (mut ingress, mut offset) = standard_setup();
    let p1_offset = offset;

    // Packet 1: PUSI = true, but payload has only 2 bytes [0x00, 0x00]
    let p1 = make_ts_packet(VIDEO_PID, true, 1, 0, false, &[0x00, 0x00]);
    let outcome1 = ingress.ingest(offset, &p1).expect("ingest p1");
    offset += PACKET_LEN_I64;

    assert_eq!(outcome1.timing_events.len(), 0);
    assert_eq!(outcome1.feeds.len(), 0);
    let facts1 = ingress.facts();
    assert_eq!(facts1.pes_starts, 0); // Not prematurely incremented
    assert_eq!(facts1.current_pes_offset, None);

    // Packet 2: Continuation carrying rest of fixed header + PTS + H.264 IDR slice
    let pts = 90_000 * 25;
    let enc_pts = encode_ts(0b0010, pts);
    let mut cont_payload = vec![0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05];
    cont_payload.extend_from_slice(&enc_pts);
    let idr_slice = [0x00, 0x00, 0x01, 0x65, 0x88];
    cont_payload.extend_from_slice(&idr_slice);

    let p2 = make_ts_packet(VIDEO_PID, false, 2, 0, false, &cont_payload);
    let outcome2 = ingress.ingest(offset, &p2).expect("ingest p2");

    assert_eq!(outcome2.timing_events.len(), 1);
    let evt = outcome2.timing_events[0];
    assert_eq!(evt.pid, Pid::new(VIDEO_PID).unwrap());
    assert_eq!(evt.subject_at, crate::timing::ByteOffset::new(p1_offset));
    assert_eq!(
        evt.timing.pts,
        TimingField::Valid(RawPts33::new(pts).unwrap())
    );

    assert_eq!(outcome2.feeds.len(), 1);
    assert_eq!(outcome2.feeds[0].es, &idr_slice);

    let facts2 = ingress.facts();
    assert_eq!(facts2.pes_starts, 1);
    assert_eq!(facts2.current_pes_offset, Some(p1_offset));
}

#[test]
fn video_pusi_split_at_6_bytes_assembles_header_and_timing() {
    let (mut ingress, mut offset) = standard_setup();
    let p1_offset = offset;

    // Packet 1: PUSI = true, payload has 6 bytes (start code + stream_id + packet_length)
    let p1 = make_ts_packet(
        VIDEO_PID,
        true,
        1,
        0,
        false,
        &[0x00, 0x00, 0x01, 0xE0, 0x00, 0x00],
    );
    let outcome1 = ingress.ingest(offset, &p1).expect("ingest p1");
    offset += PACKET_LEN_I64;

    assert_eq!(outcome1.timing_events.len(), 0);
    assert_eq!(outcome1.feeds.len(), 0);
    let facts1 = ingress.facts();
    assert_eq!(facts1.pes_starts, 0);

    // Packet 2: Continuation with flags + header_data_length (3 bytes) + PTS (5 bytes) + ES
    let pts = 90_000 * 30;
    let enc_pts = encode_ts(0b0010, pts);
    let mut cont_payload = vec![0x80, 0x80, 0x05];
    cont_payload.extend_from_slice(&enc_pts);
    let idr_slice = [0x00, 0x00, 0x01, 0x65, 0x88];
    cont_payload.extend_from_slice(&idr_slice);

    let p2 = make_ts_packet(VIDEO_PID, false, 2, 0, false, &cont_payload);
    let outcome2 = ingress.ingest(offset, &p2).expect("ingest p2");

    assert_eq!(outcome2.timing_events.len(), 1);
    let evt = outcome2.timing_events[0];
    assert_eq!(evt.pid, Pid::new(VIDEO_PID).unwrap());
    assert_eq!(evt.subject_at, crate::timing::ByteOffset::new(p1_offset));
    assert_eq!(
        evt.timing.pts,
        TimingField::Valid(RawPts33::new(pts).unwrap())
    );

    assert_eq!(outcome2.feeds.len(), 1);
    assert_eq!(outcome2.feeds[0].es, &idr_slice);

    let facts2 = ingress.facts();
    assert_eq!(facts2.pes_starts, 1);
    assert_eq!(facts2.current_pes_offset, Some(p1_offset));
}

#[test]
fn video_rejects_pes_with_invalid_byte_6_marker_bits() {
    let (mut ingress, offset) = standard_setup();

    // 1. Single-packet PUSI with byte 6 = 0x00 (bits 7..6 != '10')
    let pts = 90_000 * 35;
    let enc_pts = encode_ts(0b0010, pts);
    let mut bad_payload = vec![0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x00, 0x80, 0x05];
    bad_payload.extend_from_slice(&enc_pts);
    bad_payload.extend_from_slice(&[0x00, 0x00, 0x01, 0x65, 0x88]);

    let p1 = make_ts_packet(VIDEO_PID, true, 1, 0, false, &bad_payload);
    let outcome1 = ingress.ingest(offset, &p1).expect("ingest p1");

    assert_eq!(outcome1.timing_events.len(), 0);
    assert_eq!(outcome1.feeds.len(), 0);
    let facts1 = ingress.facts();
    assert_eq!(facts1.pes_starts, 0);
    assert_eq!(facts1.current_pes_offset, None);
    assert!(facts1.awaiting_start);

    // 2. Cross-packet: PUSI ends before byte 6 (6 bytes: 00 00 01 E0 00 00),
    // and continuation packet delivers corrupt byte 6 = 0x40.
    let (mut ingress2, mut offset2) = standard_setup();
    let p_pusi = make_ts_packet(
        VIDEO_PID,
        true,
        1,
        0,
        false,
        &[0x00, 0x00, 0x01, 0xE0, 0x00, 0x00],
    );
    let out_pusi = ingress2.ingest(offset2, &p_pusi).expect("ingest p_pusi");
    offset2 += PACKET_LEN_I64;
    assert_eq!(out_pusi.timing_events.len(), 0);
    assert_eq!(out_pusi.feeds.len(), 0);

    let mut cont_payload = vec![0x40, 0x80, 0x05];
    cont_payload.extend_from_slice(&enc_pts);
    cont_payload.extend_from_slice(&[0x00, 0x00, 0x01, 0x65, 0x88]);
    let p_cont = make_ts_packet(VIDEO_PID, false, 2, 0, false, &cont_payload);
    let out_cont = ingress2.ingest(offset2, &p_cont).expect("ingest p_cont");

    assert_eq!(out_cont.timing_events.len(), 0);
    assert_eq!(out_cont.feeds.len(), 0);
    let facts2 = ingress2.facts();
    assert_eq!(facts2.pes_starts, 0);
    assert_eq!(facts2.current_pes_offset, None);
    assert!(facts2.awaiting_start);
}

#[test]
fn timeline_epoch_decoupled_from_pat_and_pmt_lifecycle() {
    let mut ingress = VideoIngress::new(PROGRAM);
    assert!(ingress.timeline().active_epoch().is_none());

    // 1. PAT arrives alone: selection changes, but no PMT yet.
    let pat = pat_packet(0);
    let outcome_pat = ingress.ingest(0, &pat).expect("ingest pat");
    assert_eq!(outcome_pat.events, vec![VideoEvent::ProgramIdentityChanged]);
    // Active epoch must remain None because no valid PMT exists yet!
    assert!(ingress.timeline().active_epoch().is_none());

    // 2. PMT arrives: program definition complete.
    let pmt = pmt_packet(0, VIDEO_PID, 0x1B);
    let pmt_res = ingress.ingest(PACKET_LEN_I64, &pmt).expect("ingest pmt");
    assert_eq!(pmt_res.events, vec![VideoEvent::ProgramIdentityChanged]);
    // Active epoch must now be TimelineEpoch(0)!
    assert_eq!(
        ingress.timeline().active_epoch(),
        Some(crate::timing::TimelineEpoch::new(0))
    );

    // 3. PMT update (version increments): epoch advances to 1!
    let pmt_v1 = pmt_packet(1, VIDEO_PID, 0x1B);
    let v1_pmt_res = ingress
        .ingest(PACKET_LEN_I64 * 2, &pmt_v1)
        .expect("ingest pmt v1");
    assert_eq!(v1_pmt_res.events, vec![VideoEvent::ProgramIdentityChanged]);
    assert_eq!(
        ingress.timeline().active_epoch(),
        Some(crate::timing::TimelineEpoch::new(1))
    );
}

#[test]
fn timeline_rap_pts_binding_and_invalidation() {
    let (mut ingress, mut offset) = standard_setup();
    assert_eq!(
        ingress.timeline().active_epoch(),
        Some(crate::timing::TimelineEpoch::new(0))
    );

    // Send PUSI with IDR slice and PTS
    let pts = 90_000 * 10;
    let enc_pts = encode_ts(0b0010, pts);
    let mut pusi_payload = vec![0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05];
    pusi_payload.extend_from_slice(&enc_pts);
    let idr_slice = [0x00, 0x00, 0x01, 0x65, 0x88];
    pusi_payload.extend_from_slice(&idr_slice);

    let p1_offset = offset;
    let p1 = make_ts_packet(VIDEO_PID, true, 1, 0, false, &pusi_payload);
    let outcome1 = ingress.ingest(offset, &p1).expect("ingest p1");
    offset += PACKET_LEN_I64;

    assert!(
        outcome1
            .events
            .iter()
            .any(|e| matches!(e, VideoEvent::RandomAccessPoint { offset, joinable: true } if *offset == p1_offset))
    );

    // The RAP at p1_offset is published bound to its PES's PTS, in the same outcome.
    let bindings = rap_timing_records(&outcome1.timing_records);
    assert_eq!(bindings.len(), 1, "one RAP, one binding: {bindings:?}");
    assert_eq!(
        bindings[0].subject_at,
        crate::timing::ByteOffset::new(p1_offset)
    );
    assert_eq!(
        bindings[0].observed_at,
        crate::timing::ByteOffset::new(p1_offset)
    );
    assert_eq!(
        bindings[0].pts,
        Some(crate::timing::ExtendedPts90k::new(
            i64::try_from(pts).unwrap()
        ))
    );

    // Now send continuation packet with TEI, which invalidates the published RAP.
    // The invalidation is the event; nothing re-binds and nothing is re-published.
    let p2 = make_ts_packet(VIDEO_PID, false, 2, 0, true, &[0x00, 0x11, 0x22]);
    let outcome2 = ingress.ingest(offset, &p2).expect("ingest p2 with TEI");

    assert!(outcome2.events.iter().any(
        |e| matches!(e, VideoEvent::RandomAccessPointInvalidated { offset } if *offset == p1_offset)
    ));
    assert!(rap_timing_records(&outcome2.timing_records).is_empty());
}

// --- RAP timing publication across chunk boundaries -------------------------

fn rap_timing_records(records: &[TimingRecord]) -> Vec<crate::timing::TimingPoint> {
    records
        .iter()
        .filter_map(|r| match r {
            TimingRecord::RandomAccessPoint(pt) => Some(*pt),
            _ => None,
        })
        .collect()
}

/// A PES start carrying `pts` and, after the header, `es`.
fn pes_start_payload(pts: u64, es: &[u8]) -> Vec<u8> {
    let mut payload = vec![0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05];
    payload.extend_from_slice(&encode_ts(0b0010, pts));
    payload.extend_from_slice(es);
    payload
}

/// PAT + PMT for `stream_type`, then three video PESes: an access unit that is a
/// random access point only once it has ended (`intra_au`), a predicted one, and
/// the start of a third that ends the second. The first PES spans four packets,
/// the way a real intra picture spans many, so the packet that establishes the RAP
/// is well after the packet that carried its timing.
fn delayed_rap_stream(
    stream_type: u8,
    intra_au: &[u8],
    predicted_au: &[u8],
) -> (Vec<u8>, i64, i64, u64) {
    let mut data = pat_packet(0);
    data.extend_from_slice(&pmt_packet(0, VIDEO_PID, stream_type));
    let filler = [0xAA; 184];
    let pts_a = 90_000 * 20;
    let rap_offset = i64::try_from(data.len()).unwrap();
    let mut cc = 0u8;
    let mut push = |data: &mut Vec<u8>, pusi: bool, payload: &[u8]| {
        data.extend_from_slice(&make_ts_packet(VIDEO_PID, pusi, cc, 0, false, payload));
        cc = (cc + 1) & 0x0F;
    };
    push(&mut data, true, &pes_start_payload(pts_a, intra_au));
    for _ in 0..3 {
        push(&mut data, false, &filler);
    }
    let established_at = i64::try_from(data.len()).unwrap();
    push(
        &mut data,
        true,
        &pes_start_payload(pts_a + 3600, predicted_au),
    );
    push(&mut data, false, &filler);
    push(
        &mut data,
        true,
        &pes_start_payload(pts_a + 7200, predicted_au),
    );
    (data, rap_offset, established_at, pts_a)
}

/// Everything a consumer of one chunk sees about RAPs: the events, and the RAP
/// timing records, each list per outcome.
type RapView = Vec<(Vec<VideoEvent>, Vec<crate::timing::TimingPoint>)>;

fn ingest_in_chunks(data: &[u8], packets_per_chunk: usize) -> RapView {
    let mut ingress = VideoIngress::new(PROGRAM);
    let mut view = Vec::new();
    for (i, chunk) in data.chunks(packets_per_chunk * TS_PACKET_LEN).enumerate() {
        let start = i64::try_from(i * packets_per_chunk * TS_PACKET_LEN).unwrap();
        let outcome = ingress.ingest(start, chunk).expect("aligned chunk");
        let events: Vec<VideoEvent> = outcome
            .events
            .iter()
            .filter(|e| !matches!(e, VideoEvent::ProgramIdentityChanged))
            .copied()
            .collect();
        view.push((events, rap_timing_records(&outcome.timing_records)));
    }
    view
}

/// The RAP is published bound, in the outcome that carries its event, at every
/// chunking - and every chunking publishes the same thing.
fn assert_rap_binding_is_chunk_independent(stream_type: u8, intra_au: &[u8], predicted_au: &[u8]) {
    let (data, rap_offset, established_at, pts) =
        delayed_rap_stream(stream_type, intra_au, predicted_au);
    let packets = data.len() / TS_PACKET_LEN;

    let whole = ingest_in_chunks(&data, packets);
    let flatten = |view: &RapView| -> (Vec<VideoEvent>, Vec<crate::timing::TimingPoint>) {
        let mut events = Vec::new();
        let mut records = Vec::new();
        for (e, r) in view {
            events.extend(e.iter().copied());
            records.extend(r.iter().copied());
        }
        (events, records)
    };
    let (whole_events, whole_records) = flatten(&whole);
    assert_eq!(
        whole_events,
        vec![VideoEvent::RandomAccessPoint {
            offset: rap_offset,
            joinable: true
        }],
        "exactly one RAP, at the start of the intra access unit"
    );
    assert_eq!(whole_records.len(), 1);
    let binding = whole_records[0];
    assert_eq!(binding.subject_at.get(), rap_offset);
    assert_eq!(binding.observed_at.get(), established_at);
    assert_eq!(binding.epoch, crate::timing::TimelineEpoch::new(0));
    assert_eq!(binding.pid.get(), VIDEO_PID);
    assert_eq!(
        binding.pts,
        Some(crate::timing::ExtendedPts90k::new(
            i64::try_from(pts).unwrap()
        ))
    );

    for packets_per_chunk in 1..=packets {
        let view = ingest_in_chunks(&data, packets_per_chunk);
        assert_eq!(
            flatten(&view),
            (whole_events.clone(), whole_records.clone()),
            "chunk size {packets_per_chunk} packets publishes something else"
        );
        for (events, records) in &view {
            for event in events {
                if let VideoEvent::RandomAccessPoint { offset, .. } = *event {
                    assert!(
                        records.iter().any(|r| r.subject_at.get() == offset),
                        "chunk size {packets_per_chunk}: RAP at {offset} without its binding in the same outcome"
                    );
                }
            }
        }
    }
}

#[test]
fn rap_established_at_access_unit_end_is_published_bound_h264_all_intra() {
    // SPS, PPS, then a non-IDR slice whose header says I: joinable only once the
    // access unit has ended with no predicted slice in it.
    let intra = [
        0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1E, // SPS
        0x00, 0x00, 0x01, 0x68, 0xCE, 0x38, 0x80, // PPS
        0x00, 0x00, 0x01, 0x41, 0xB0, 0xAA, 0xAA, // non-IDR slice, slice_type I
    ];
    let predicted = [0x00, 0x00, 0x01, 0x41, 0xC0, 0xAA, 0xAA]; // slice_type P
    assert_rap_binding_is_chunk_independent(0x1B, &intra, &predicted);
}

#[test]
fn rap_established_at_access_unit_end_is_published_bound_hevc_recovery_point() {
    // VPS, SPS, PPS, a prefix SEI carrying a recovery point, then a TRAIL_R slice:
    // joinable only once the access unit has ended.
    let intra = [
        0x00, 0x00, 0x01, 0x40, 0x01, 0x0C, // VPS
        0x00, 0x00, 0x01, 0x42, 0x01, 0x01, // SPS
        0x00, 0x00, 0x01, 0x44, 0x01, 0xC1, // PPS
        0x00, 0x00, 0x01, 0x4E, 0x01, 0x06, 0x01, 0x80, 0x80, // prefix SEI: recovery_point
        0x00, 0x00, 0x01, 0x02, 0x01, 0xAA, 0xAA, // TRAIL_R
    ];
    let predicted = [0x00, 0x00, 0x01, 0x02, 0x01, 0xAA, 0xAA];
    assert_rap_binding_is_chunk_independent(0x24, &intra, &predicted);
}

#[test]
fn timeline_track_reset_on_cc_break_does_not_bump_epoch() {
    let (mut ingress, mut offset) = standard_setup();
    let initial_epoch = ingress.timeline().active_epoch();
    assert_eq!(initial_epoch, Some(crate::timing::TimelineEpoch::new(0)));

    // Send valid PUSI
    let pts = 90_000 * 5;
    let enc_pts = encode_ts(0b0010, pts);
    let mut pusi_payload = vec![0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05];
    pusi_payload.extend_from_slice(&enc_pts);
    pusi_payload.extend_from_slice(&[0x00, 0x00, 0x01, 0x41, 0x9A]); // non-IDR slice

    let p1 = make_ts_packet(VIDEO_PID, true, 1, 0, false, &pusi_payload);
    ingress.ingest(offset, &p1).expect("ingest p1");
    offset += PACKET_LEN_I64;

    assert!(ingress.timeline().last_video_point().is_some());

    // Send unannounced CC jump (CC jumps from 1 to 5) on video PID
    let p2 = make_ts_packet(VIDEO_PID, false, 5, 0, false, &[0xAA, 0xBB]);
    ingress.ingest(offset, &p2).expect("ingest p2 with cc jump");

    // TimelineEpoch must NOT advance!
    assert_eq!(ingress.timeline().active_epoch(), initial_epoch);
    // Track-local timing point was reset:
    assert!(ingress.timeline().last_video_point().is_none());
}
