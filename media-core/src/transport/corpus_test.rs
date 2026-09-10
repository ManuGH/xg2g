// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! Authored transport packets with the answers written out by hand.
//!
//! Every expectation here was worked out from the packet bytes and the standard,
//! not by running the parser and recording what it said. A corpus produced from
//! the implementation would agree with it by construction and would prove
//! nothing about whether either is right.
//!
//! The bit positions are the point. A mask, a shift or an offset that is one
//! out is the kind of mistake that still parses a well-formed stream most of the
//! time, so the cases below pin each field against a packet where getting it
//! wrong changes the answer.

use super::*;

/// Builds a packet with an explicit header and payload-only control bits.
fn packet(b1: u8, b2: u8, b3: u8, fill: u8) -> Vec<u8> {
    let mut p = vec![fill; TS_PACKET_LEN];
    p[0] = SYNC_BYTE;
    p[1] = b1;
    p[2] = b2;
    p[3] = b3;
    p
}

#[test]
fn a_payload_only_packet_reads_its_header() {
    // PID 0x0100: byte 1 low five bits are 0x01, byte 2 is 0x00.
    // afc 0b01, continuity counter 5 -> byte 3 is 0b0001_0101.
    let p = packet(0x01, 0x00, 0x15, 0xAA);
    let v = PacketView::parse(&p).expect("well formed");

    assert_eq!(v.pid(), 0x0100);
    assert!(!v.transport_error_indicator());
    assert!(!v.payload_unit_start());
    assert!(!v.transport_priority());
    assert_eq!(v.scrambling_control(), 0);
    assert_eq!(v.adaptation_field_control(), 0b01);
    assert_eq!(v.continuity_counter(), 5);
    assert!(v.has_payload());
    assert!(!v.has_adaptation());
    // Payload begins straight after the four header bytes.
    assert_eq!(v.payload().expect("payload").len(), 184);
    assert_eq!(v.payload().expect("payload")[0], 0xAA);
    assert!(!v.discontinuity_indicator());
}

#[test]
fn the_pid_uses_thirteen_bits_and_not_the_flags_above_them() {
    // All three flag bits set and PID 0x1FFF: byte 1 is 0b1110_0000 | 0x1F.
    let p = packet(0xFF, 0xFF, 0x10, 0x00);
    let v = PacketView::parse(&p).expect("well formed");
    assert_eq!(v.pid(), 0x1FFF);
    assert!(v.transport_error_indicator());
    assert!(v.payload_unit_start());
    assert!(v.transport_priority());
}

#[test]
fn each_flag_is_read_from_its_own_bit() {
    // Error indicator alone.
    let bytes = packet(0x80, 0x21, 0x10, 0);
    let v = PacketView::parse(&bytes).expect("well formed");
    assert!(v.transport_error_indicator());
    assert!(!v.payload_unit_start());
    assert!(!v.transport_priority());
    assert_eq!(v.pid(), 0x0021);

    // Payload unit start alone.
    let bytes = packet(0x40, 0x21, 0x10, 0);
    let v = PacketView::parse(&bytes).expect("well formed");
    assert!(!v.transport_error_indicator());
    assert!(v.payload_unit_start());
    assert!(!v.transport_priority());

    // Priority alone.
    let bytes = packet(0x20, 0x21, 0x10, 0);
    let v = PacketView::parse(&bytes).expect("well formed");
    assert!(!v.transport_error_indicator());
    assert!(!v.payload_unit_start());
    assert!(v.transport_priority());
}

#[test]
fn scrambling_and_adaptation_control_do_not_overlap() {
    // Byte 3: scrambling 0b10, afc 0b01, counter 0b0011.
    let bytes = packet(0x00, 0x21, 0b1001_0011, 0);
    let v = PacketView::parse(&bytes).expect("well formed");
    assert_eq!(v.scrambling_control(), 0b10);
    assert_eq!(v.adaptation_field_control(), 0b01);
    assert_eq!(v.continuity_counter(), 3);

    // And the other way round: scrambling 0b01, afc 0b11.
    let mut p = packet(0x00, 0x21, 0b0111_1111, 0);
    p[4] = 0; // zero-length adaptation field
    let v = PacketView::parse(&p).expect("well formed");
    assert_eq!(v.scrambling_control(), 0b01);
    assert_eq!(v.adaptation_field_control(), 0b11);
    assert_eq!(v.continuity_counter(), 0x0F);
}

#[test]
fn a_clear_packet_and_a_scrambled_one_differ_only_in_the_bits() {
    let clear_bytes = packet(0x00, 0x64, 0x10, 0);
    let scrambled_bytes = packet(0x00, 0x64, 0x90, 0);
    let clear = PacketView::parse(&clear_bytes).expect("well formed");
    assert_eq!(clear.scrambling_control(), 0);
    // Same packet, even key.
    let scrambled = PacketView::parse(&scrambled_bytes).expect("well formed");
    assert_eq!(scrambled.scrambling_control(), 0b10);
    // The reading is the same in every other respect: this layer has no opinion
    // about what scrambling means.
    assert_eq!(clear.pid(), scrambled.pid());
    assert_eq!(clear.has_payload(), scrambled.has_payload());
    assert_eq!(
        clear.payload().map(<[u8]>::len),
        scrambled.payload().map(<[u8]>::len)
    );
}

#[test]
fn an_adaptation_field_moves_the_payload_by_its_length_plus_one() {
    let mut p = packet(0x00, 0x64, 0x30, 0xBB); // afc 0b11
    p[4] = 7; // seven adaptation bytes after the length byte
    for b in p.iter_mut().skip(5).take(7) {
        *b = 0xCC;
    }
    let v = PacketView::parse(&p).expect("well formed");

    assert!(v.has_adaptation());
    assert!(v.has_payload());
    assert_eq!(v.adaptation_field().expect("adaptation").len(), 7);
    // 4 header + 1 length + 7 field = 12; 188 - 12 = 176.
    assert_eq!(v.payload().expect("payload").len(), 176);
    assert_eq!(v.payload().expect("payload")[0], 0xBB);
}

#[test]
fn a_zero_length_adaptation_field_leaves_the_payload_one_byte_later() {
    let mut p = packet(0x00, 0x64, 0x30, 0xBB);
    p[4] = 0;
    let v = PacketView::parse(&p).expect("well formed");
    assert!(v.has_adaptation());
    assert_eq!(v.adaptation_field().expect("adaptation").len(), 0);
    // 4 header + 1 length = 5; 188 - 5 = 183.
    assert_eq!(v.payload().expect("payload").len(), 183);
    // A field with no bytes announces nothing.
    assert!(!v.discontinuity_indicator());
}

#[test]
fn an_adaptation_only_packet_has_no_payload() {
    let mut p = packet(0x00, 0x64, 0x20, 0xDD); // afc 0b10
    p[4] = 183; // the rest of the packet
    let v = PacketView::parse(&p).expect("well formed");
    assert_eq!(v.adaptation_field_control(), 0b10);
    assert!(v.has_adaptation());
    assert!(!v.has_payload());
    assert!(v.payload().is_none());
    assert_eq!(v.adaptation_field().expect("adaptation").len(), 183);
}

#[test]
fn the_largest_adaptation_field_that_still_leaves_a_payload_leaves_one_byte() {
    let mut p = packet(0x00, 0x64, 0x30, 0x00);
    p[4] = 182; // 4 + 1 + 182 = 187, one byte left
    p[187] = 0xEE;
    let v = PacketView::parse(&p).expect("well formed");
    assert_eq!(v.payload().expect("payload"), &[0xEE]);
}

#[test]
fn an_adaptation_field_filling_the_packet_leaves_no_payload() {
    // afc says payload follows, the length says there is no room for one. The
    // packet is readable and carries nothing, which is what is reported.
    let mut p = packet(0x00, 0x64, 0x30, 0x00);
    p[4] = 183; // 4 + 1 + 183 = 188
    let v = PacketView::parse(&p).expect("readable, if malformed");
    assert!(v.has_adaptation());
    assert!(!v.has_payload());
}

#[test]
fn an_adaptation_field_past_the_end_is_refused_rather_than_clamped() {
    let mut p = packet(0x00, 0x64, 0x30, 0x00);
    p[4] = 184; // one byte too many
    assert_eq!(
        PacketView::parse(&p).unwrap_err(),
        TransportError::AdaptationOverrun { declared: 184 }
    );

    let mut p = packet(0x00, 0x64, 0x20, 0x00); // adaptation only
    p[4] = 184;
    assert_eq!(
        PacketView::parse(&p).unwrap_err(),
        TransportError::AdaptationOverrun { declared: 184 }
    );

    let mut p = packet(0x00, 0x64, 0x30, 0x00);
    p[4] = 255;
    assert_eq!(
        PacketView::parse(&p).unwrap_err(),
        TransportError::AdaptationOverrun { declared: 255 }
    );
}

#[test]
fn the_reserved_adaptation_control_is_refused() {
    let p = packet(0x00, 0x64, 0x00, 0);
    assert_eq!(
        PacketView::parse(&p).unwrap_err(),
        TransportError::ReservedAdaptationControl
    );
}

#[test]
fn a_packet_without_the_sync_byte_is_refused() {
    let mut p = packet(0x00, 0x64, 0x10, 0);
    p[0] = 0x46;
    assert_eq!(
        PacketView::parse(&p).unwrap_err(),
        TransportError::NoSyncByte(0x46)
    );
}

#[test]
fn a_slice_that_is_not_one_packet_is_refused() {
    assert_eq!(
        PacketView::parse(&[SYNC_BYTE; 187]).unwrap_err(),
        TransportError::WrongLength(187)
    );
    assert_eq!(
        PacketView::parse(&[SYNC_BYTE; 189]).unwrap_err(),
        TransportError::WrongLength(189)
    );
    assert_eq!(
        PacketView::parse(&[]).unwrap_err(),
        TransportError::WrongLength(0)
    );
}

#[test]
fn the_discontinuity_flag_is_the_top_bit_of_the_first_adaptation_byte() {
    let mut announced = packet(0x00, 0x64, 0x30, 0x00);
    announced[4] = 1;
    announced[5] = 0x80;
    assert!(
        PacketView::parse(&announced)
            .expect("well formed")
            .discontinuity_indicator()
    );

    // Every other flag in that byte set, the discontinuity one clear.
    let mut quiet = packet(0x00, 0x64, 0x30, 0x00);
    quiet[4] = 1;
    quiet[5] = 0x7F;
    assert!(
        !PacketView::parse(&quiet)
            .expect("well formed")
            .discontinuity_indicator()
    );

    // A packet with no adaptation field announces nothing.
    assert!(
        !PacketView::parse(&packet(0x00, 0x64, 0x10, 0))
            .expect("well formed")
            .discontinuity_indicator()
    );
}

// --- continuity -----------------------------------------------------------

fn cc_packet(counter: u8, fill: u8) -> Vec<u8> {
    packet(0x00, 0x64, 0x10 | (counter & 0x0F), fill)
}

#[test]
fn the_first_packet_on_a_pid_has_nothing_to_compare_against() {
    let mut t = ContinuityTracker::new();
    let p = cc_packet(9, 0x01);
    assert_eq!(t.observe(&p, 9), Continuity::First);
}

#[test]
fn the_counter_advancing_by_one_is_continuous() {
    let mut t = ContinuityTracker::new();
    assert_eq!(t.observe(&cc_packet(0, 0x01), 0), Continuity::First);
    assert_eq!(t.observe(&cc_packet(1, 0x02), 1), Continuity::Continuous);
    assert_eq!(t.observe(&cc_packet(2, 0x03), 2), Continuity::Continuous);
}

#[test]
fn the_counter_wraps_from_fifteen_to_zero_without_breaking() {
    let mut t = ContinuityTracker::new();
    assert_eq!(t.observe(&cc_packet(14, 0x01), 14), Continuity::First);
    assert_eq!(t.observe(&cc_packet(15, 0x02), 15), Continuity::Continuous);
    // The wrap is the normal case, not a gap.
    assert_eq!(t.observe(&cc_packet(0, 0x03), 0), Continuity::Continuous);
    assert_eq!(t.observe(&cc_packet(1, 0x04), 1), Continuity::Continuous);
}

#[test]
fn the_same_counter_with_the_same_bytes_is_the_transport_repeating_itself() {
    let mut t = ContinuityTracker::new();
    let p = cc_packet(7, 0x55);
    assert_eq!(t.observe(&p, 7), Continuity::First);
    assert_eq!(t.observe(&p, 7), Continuity::Duplicate);
    // A duplicate changes nothing, so the next packet is still judged against
    // the packet that was repeated.
    assert_eq!(t.observe(&cc_packet(8, 0x56), 8), Continuity::Continuous);
}

#[test]
fn the_same_counter_with_different_bytes_is_broken() {
    let mut t = ContinuityTracker::new();
    assert_eq!(t.observe(&cc_packet(7, 0x55), 7), Continuity::First);
    // One counter value cannot describe two different packets.
    assert_eq!(t.observe(&cc_packet(7, 0x66), 7), Continuity::Broken);
}

#[test]
fn a_skipped_counter_is_broken_and_then_recovers() {
    let mut t = ContinuityTracker::new();
    assert_eq!(t.observe(&cc_packet(3, 0x01), 3), Continuity::First);
    assert_eq!(t.observe(&cc_packet(5, 0x02), 5), Continuity::Broken);
    // The break records the packet that arrived, so one loss does not make
    // every packet after it look lost.
    assert_eq!(t.observe(&cc_packet(6, 0x03), 6), Continuity::Continuous);
}

#[test]
fn a_counter_going_backwards_is_broken() {
    let mut t = ContinuityTracker::new();
    assert_eq!(t.observe(&cc_packet(8, 0x01), 8), Continuity::First);
    assert_eq!(t.observe(&cc_packet(7, 0x02), 7), Continuity::Broken);
}

#[test]
fn a_reset_makes_the_next_packet_the_first_again() {
    let mut t = ContinuityTracker::new();
    assert_eq!(t.observe(&cc_packet(3, 0x01), 3), Continuity::First);
    t.reset();
    // Without the reset this would be a gap; after it there is nothing to
    // compare against.
    assert_eq!(t.observe(&cc_packet(9, 0x02), 9), Continuity::First);
}

#[test]
fn the_tracker_reports_what_it_is_holding() {
    let mut t = ContinuityTracker::new();
    assert_eq!(t.retained_bytes(), 0);
    t.observe(&cc_packet(1, 0x01), 1);
    assert_eq!(t.retained_bytes(), TS_PACKET_LEN);
    t.reset();
    assert_eq!(t.retained_bytes(), 0);
}

#[test]
fn only_the_low_four_bits_of_the_counter_are_the_counter() {
    let mut t = ContinuityTracker::new();
    // The caller passes the whole byte; the tracker must mask it. Byte 3 of a
    // packet carries scrambling and adaptation bits above the counter, and a
    // tracker comparing the whole byte would call a scrambling change a gap.
    assert_eq!(t.observe(&cc_packet(1, 0x01), 0xF1), Continuity::First);
    assert_eq!(t.observe(&cc_packet(2, 0x02), 0x02), Continuity::Continuous);
}
