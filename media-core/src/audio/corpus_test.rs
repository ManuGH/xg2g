// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! The shared corpus, read and enforced.
//!
//! The file this reads is generated from the Go side's authored expectations and
//! checked into the repository. Both implementations are held to it, which is
//! what makes agreement a property of a reviewable artefact rather than of two
//! parsers that happen to have been written by the same hand on the same day.
//!
//! Feed boundaries are part of each case. Concatenating the feeds and calling
//! `feed` once would be a different input, and passing that would prove less.

use super::observer::{Observation, Observer};
use std::path::{Path, PathBuf};

fn corpus_path() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../testdata/audio-corpus/corpus.txt")
}

struct Step {
    feed: Vec<u8>,
    want: Observation,
}

struct Case {
    name: String,
    steps: Vec<Step>,
}

fn parse_hex(s: &str) -> Vec<u8> {
    assert!(s.len().is_multiple_of(2), "odd-length hex: {s}");
    let bytes = s.as_bytes();
    let mut out = Vec::with_capacity(s.len() / 2);
    let mut i = 0;
    while i < bytes.len() {
        let hi = (bytes[i] as char)
            .to_digit(16)
            .unwrap_or_else(|| panic!("bad hex digit at {i}"));
        let lo = (bytes[i + 1] as char)
            .to_digit(16)
            .unwrap_or_else(|| panic!("bad hex digit at {}", i + 1));
        out.push(u8::try_from(hi * 16 + lo).expect("two hex digits are a byte"));
        i += 2;
    }
    out
}

fn parse_observation(s: &str) -> Observation {
    let mut o = Observation::default();
    for field in s.split_whitespace() {
        let (key, value) = field
            .split_once('=')
            .unwrap_or_else(|| panic!("field without a value: {field}"));
        let n: u64 = value
            .parse()
            .unwrap_or_else(|e| panic!("value of {key} is not a number: {e}"));
        match key {
            "channels" => o.channels = u8::try_from(n).expect("channel counts are small"),
            "lfe" => o.lfe = n == 1,
            "acmod" => o.acmod = u8::try_from(n).expect("acmod is three bits"),
            "hasAcmod" => o.has_acmod = n == 1,
            "dependent" => o.dependent_substream = n == 1,
            "frames" => o.frames = n,
            other => panic!("unknown observation field: {other}"),
        }
    }
    o
}

fn parse_corpus(text: &str) -> Vec<Case> {
    let mut cases = Vec::new();
    let mut current: Option<Case> = None;
    let mut pending_feed: Option<Vec<u8>> = None;

    for (lineno, raw) in text.lines().enumerate() {
        let line = raw.trim();
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        let (keyword, rest) = line.split_once(' ').unwrap_or((line, ""));
        match keyword {
            "version" => assert_eq!(rest, "1", "unsupported corpus format version"),
            "case" => {
                current = Some(Case {
                    name: rest.to_string(),
                    steps: Vec::new(),
                });
            }
            "desc" => {}
            "feed" => {
                assert!(
                    pending_feed.is_none(),
                    "two feeds without a want, line {}",
                    lineno + 1
                );
                pending_feed = Some(parse_hex(rest));
            }
            "want" => {
                let feed = pending_feed
                    .take()
                    .unwrap_or_else(|| panic!("want without a feed, line {}", lineno + 1));
                current
                    .as_mut()
                    .unwrap_or_else(|| panic!("step outside a case, line {}", lineno + 1))
                    .steps
                    .push(Step {
                        feed,
                        want: parse_observation(rest),
                    });
            }
            "end" => {
                assert!(pending_feed.is_none(), "case ended with a feed and no want");
                cases.push(current.take().expect("end without a case"));
            }
            other => panic!("unknown corpus keyword {other} on line {}", lineno + 1),
        }
    }
    assert!(current.is_none(), "corpus ended inside a case");
    cases
}

#[test]
fn the_rust_observer_answers_the_shared_corpus() {
    let path = corpus_path();
    let text =
        std::fs::read_to_string(&path).unwrap_or_else(|e| panic!("read {}: {e}", path.display()));
    let cases = parse_corpus(&text);
    assert!(
        cases.len() > 20,
        "corpus looks truncated: {} cases",
        cases.len()
    );

    let mut failures = Vec::new();
    for case in &cases {
        let mut o = Observer::new();
        for (i, step) in case.steps.iter().enumerate() {
            o.feed(&step.feed);
            let got = o.current();
            if got != step.want {
                failures.push(format!(
                    "{}: after feed {} of {} ({} bytes)\n   got {:?}\n  want {:?}",
                    case.name,
                    i + 1,
                    case.steps.len(),
                    step.feed.len(),
                    got,
                    step.want
                ));
            }
            // Known() is derived, not stored: it must follow the channel count in
            // every case rather than being a second opinion about it.
            assert_eq!(
                got.known(),
                got.channels > 0,
                "{}: known() disagrees with the channel count",
                case.name
            );
        }
    }
    assert!(
        failures.is_empty(),
        "{} case(s) disagree:\n{}",
        failures.len(),
        failures.join("\n")
    );
}

#[test]
fn the_corpus_covers_what_it_claims_to() {
    let text = std::fs::read_to_string(corpus_path()).expect("read corpus");
    let cases = parse_corpus(&text);
    let names: Vec<&str> = cases.iter().map(|c| c.name.as_str()).collect();

    // Not a substitute for reading the file - a reminder that these classes are
    // the reason it exists, so a regeneration that quietly drops one is noticed.
    for required in [
        "ac3_acmod7_lfe",
        "ac3_reserved_fscod",
        "ac3_reserved_bsid",
        "eac3_dependent_is_flagged_not_counted",
        "eac3_foreign_substream_carries_no_count",
        "ac3_header_split_after_1_bytes",
        "ac3_frame_payload_crosses_the_feed_boundary",
        "ac3_sync_pattern_inside_frame_payload_is_not_a_frame",
        "ac3_two_agreeing_frames_are_not_enough",
        "ac3_stereo_to_51_without_a_reset",
        "ac3_51_to_stereo_without_a_reset",
    ] {
        assert!(
            names.contains(&required),
            "corpus no longer covers {required}"
        );
    }
}
