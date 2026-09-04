// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! How long a chunk takes to interpret.
//!
//! Not a benchmark to be optimised against. The number that matters is the
//! distance to the deadline a caller would impose - 500 ms for one chunk in the
//! Go pipeline - and the shape of the curve as chunks grow. A parser whose cost
//! is linear in the bytes and three orders of magnitude inside the budget needs
//! no tuning; one that is neither needs a look before it is wired to anything.
//!
//! Timed with `Instant` rather than a benchmark harness, because the crate has
//! no dependencies and stable Rust has no built-in one. Allocation counts are
//! not reported: counting them needs a global allocator, which needs `unsafe`,
//! which this crate forbids. What is asserted instead is that the state stays
//! bounded, which is the property the count would have been evidence for.

use super::{PsiCore, TS_PACKET_LEN};
use std::time::{Duration, Instant};

/// Sizes a caller would hand in. The Go normalizer's staging buffer is 4 MiB, so
/// that is the largest chunk this path ever sees.
const TARGET_SIZES: [usize; 4] = [64 * 1024, 256 * 1024, 1024 * 1024, 4 * 1024 * 1024];

/// How many times each size is timed, fewer for the larger ones so the whole
/// thing stays inside a test run.
const ROUNDS: [usize; 4] = [200, 100, 40, 20];

/// The longest a caller would wait for one chunk before deciding the core has
/// stopped answering. Nothing here should come near it.
const CALLER_DEADLINE: Duration = Duration::from_millis(500);

/// What one chunk may cost before this test complains.
///
/// The shipped binary is a release build, and that is the number worth holding
/// to something tight. A debug build of the same code is roughly seven times
/// slower here by construction - overflow checks on, nothing inlined - so
/// holding it to the release bound would fail for a reason that has nothing to
/// do with the parser. Debug keeps the caller's own deadline as a stall
/// detector, which is what a wall-clock assertion in a test run under unknown
/// load can honestly claim.
fn budget() -> Duration {
    if cfg!(debug_assertions) {
        CALLER_DEADLINE
    } else {
        CALLER_DEADLINE / 10
    }
}

fn seal(mut body: Vec<u8>) -> Vec<u8> {
    let crc = super::crc::mpeg2(&body);
    body.extend_from_slice(&crc.to_be_bytes());
    body
}

/// Checks that a hand-written section says how long it actually is.
///
/// The length byte of these fixtures is written out rather than computed, and a
/// wrong one is not visible from the outside: the section is simply refused, the
/// transport benchmarks the reject path instead of the parse path, and the
/// upper-bound assertions hold trivially at zero.
fn assert_length_field_matches(section: &[u8]) {
    let declared = (usize::from(section[1] & 0x0F) << 8) | usize::from(section[2]);
    assert_eq!(
        declared + 3,
        section.len(),
        "section declares {declared} after its length field but is {} bytes",
        section.len()
    );
}

fn pat(version: u8) -> Vec<u8> {
    seal(vec![
        0x00,
        0xB0,
        0x0D,
        0x00,
        0x01,
        0xC0 | ((version & 0x1F) << 1) | 1,
        0x00,
        0x00,
        0x00,
        0x01,
        0xE1,
        0x00,
    ])
}

fn pmt(version: u8) -> Vec<u8> {
    seal(vec![
        0x02,
        0xB0,
        0x21,
        0x00,
        0x01,
        0xC0 | ((version & 0x1F) << 1) | 1,
        0x00,
        0x00,
        0xE1,
        0x01,
        0xF0,
        0x00,
        0x1B,
        0xE1,
        0x01,
        0xF0,
        0x00,
        0x06,
        0xE1,
        0x02,
        0xF0,
        0x0A,
        0x0A,
        0x04,
        b'd',
        b'e',
        b'u',
        0x00,
        0x6A,
        0x02,
        0x80,
        0x02,
    ])
}

fn psi_packet(pid: u16, cc: u8, section: &[u8]) -> Vec<u8> {
    let mut packet = vec![0xFFu8; TS_PACKET_LEN];
    packet[0] = 0x47;
    packet[1] = 0x40 | u8::try_from((pid >> 8) & 0x1F).expect("masked");
    packet[2] = u8::try_from(pid & 0xFF).expect("masked");
    packet[3] = 0x10 | (cc & 0x0F);
    packet[4] = 0x00;
    packet[5..5 + section.len()].copy_from_slice(section);
    packet
}

fn null_packet() -> Vec<u8> {
    let mut packet = vec![0xFFu8; TS_PACKET_LEN];
    packet[0] = 0x47;
    packet[1] = 0x1F;
    packet[2] = 0xFF;
    packet[3] = 0x10;
    packet
}

/// A transport shaped like a real one: tables repeat on a carousel and almost
/// everything between them is something this parser does not read.
fn representative(packets: usize) -> Vec<u8> {
    let mut out = Vec::with_capacity(packets * TS_PACKET_LEN);
    let null = null_packet();
    for index in 0..packets {
        let carousel = index % 100;
        let version = u8::try_from((index / 100) % 32).expect("masked to five bits");
        if carousel == 0 {
            out.extend_from_slice(&psi_packet(
                0,
                u8::try_from(index / 100 % 16).expect("masked"),
                &pat(0),
            ));
        } else if carousel == 1 {
            out.extend_from_slice(&psi_packet(
                256,
                u8::try_from(index / 100 % 16).expect("masked"),
                &pmt(version),
            ));
        } else {
            out.extend_from_slice(&null);
        }
    }
    out
}

/// Every packet a table, which no transport does. The worst case for a parser
/// that only spends time on tables.
fn all_tables(packets: usize) -> Vec<u8> {
    let mut out = Vec::with_capacity(packets * TS_PACKET_LEN);
    for index in 0..packets {
        let version = u8::try_from(index % 32).expect("masked to five bits");
        if index % 2 == 0 {
            out.extend_from_slice(&psi_packet(
                0,
                u8::try_from(index % 16).expect("masked"),
                &pat(0),
            ));
        } else {
            out.extend_from_slice(&psi_packet(
                256,
                u8::try_from(index % 16).expect("masked"),
                &pmt(version),
            ));
        }
    }
    out
}

fn percentile(sorted: &[Duration], fraction: f64) -> Duration {
    if sorted.is_empty() {
        return Duration::ZERO;
    }
    #[expect(
        clippy::cast_precision_loss,
        clippy::cast_possible_truncation,
        clippy::cast_sign_loss,
        reason = "an index into a list of at most a few hundred timings"
    )]
    let index = ((sorted.len() as f64 - 1.0) * fraction).round() as usize;
    sorted[index.min(sorted.len() - 1)]
}

fn measure(name: &str, build: fn(usize) -> Vec<u8>) {
    for (size, rounds) in TARGET_SIZES.iter().zip(ROUNDS) {
        let packets = size / TS_PACKET_LEN;
        let data = build(packets);
        let mut timings = Vec::with_capacity(rounds);

        for _ in 0..rounds {
            let mut core = PsiCore::new(1);
            let started = Instant::now();
            let outcome = core
                .ingest(0, &data)
                .expect("packet aligned by construction");
            timings.push(started.elapsed());
            // Read something out so the call cannot be optimised away, and check
            // the state stayed bounded while it ran.
            assert!(outcome.facts.audio_pids.len() <= 8);
            assert!(outcome.active.pat.len() <= 8);
            assert!(outcome.active.pmt.len() <= 8);
        }

        timings.sort_unstable();
        let p50 = percentile(&timings, 0.50);
        let p99 = percentile(&timings, 0.99);
        let max = *timings.last().expect("rounds is never zero");
        println!(
            "  {name:<15} {:>5} KiB ({packets:>5} packets) n={rounds:<4} p50={p50:>12.3?} p99={p99:>12.3?} max={max:>12.3?}",
            data.len() / 1024
        );

        assert!(
            max < budget(),
            "{name} at {} KiB took {max:?}, against a budget of {:?}",
            data.len() / 1024,
            budget()
        );
    }
}

#[test]
fn interpreting_a_chunk_stays_far_inside_what_a_caller_would_wait() {
    assert_length_field_matches(&pat(0));
    assert_length_field_matches(&pmt(0));
    println!(
        "PSI parse cost, one Ingest per timing ({} build, budget {:?}):",
        if cfg!(debug_assertions) {
            "debug"
        } else {
            "release"
        },
        budget()
    );
    measure("representative", representative);
    measure("every packet PSI", all_tables);
}
