// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! The shared raw-transport corpus, read and enforced.
//!
//! The file is rendered from expectations authored on the Go side and checked
//! into the repository. Both implementations are held to it, so agreement is a
//! property of a reviewable artefact rather than of two parsers written by the
//! same hand on the same afternoon.
//!
//! One case carries `diverges`. Its `feed` lines are still the authored answer
//! and this is still held to them; its `ref-feed` lines record what the
//! reference does instead, and are read here only to assert that the two
//! really do differ. A divergence nobody can see in the file would be a claim,
//! not a record.

use super::AudioIngress;
use crate::audio::observer::Observation;
use std::path::{Path, PathBuf};

fn corpus_path() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../testdata/audio-ts-corpus/corpus.txt")
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct Feed {
    incarnation: usize,
    pid: u16,
    es: Vec<u8>,
    observation: Observation,
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct Stream {
    pid: u16,
    codec: String,
    feeds: u64,
    observation: Observation,
}

#[derive(Debug, Clone, PartialEq, Eq)]
enum Step {
    Chunk(Vec<u8>),
    Target(u16),
}

#[derive(Debug)]
struct Case {
    name: String,
    program: u16,
    steps: Vec<Step>,
    want: Vec<Feed>,
    streams: Vec<Stream>,
    diverges: Option<String>,
    reference: Vec<Feed>,
    reference_streams: Vec<Stream>,
}

fn parse_hex(s: &str) -> Vec<u8> {
    assert!(s.len().is_multiple_of(2), "odd-length hex");
    (0..s.len() / 2)
        .map(|i| u8::from_str_radix(&s[i * 2..i * 2 + 2], 16).expect("hex"))
        .collect()
}

/// Reads `key=value` pairs out of the tail of a line.
fn field<'a>(parts: &[&'a str], key: &str) -> &'a str {
    for p in parts {
        if let Some(v) = p.strip_prefix(key) {
            return v;
        }
    }
    panic!("no {key} in {parts:?}");
}

fn observation_of(parts: &[&str]) -> Observation {
    Observation {
        channels: field(parts, "channels=").parse().expect("channels"),
        lfe: field(parts, "lfe=") == "1",
        acmod: field(parts, "acmod=").parse().expect("acmod"),
        has_acmod: field(parts, "hasAcmod=") == "1",
        dependent_substream: field(parts, "dependent=") == "1",
        frames: field(parts, "frames=").parse().expect("frames"),
    }
}

fn feed_of(parts: &[&str]) -> Feed {
    Feed {
        incarnation: field(parts, "inc=").parse().expect("inc"),
        pid: u16::from_str_radix(field(parts, "pid="), 16).expect("pid"),
        es: parse_hex(field(parts, "es=")),
        observation: observation_of(parts),
    }
}

fn stream_of(parts: &[&str]) -> Stream {
    Stream {
        pid: u16::from_str_radix(field(parts, "pid="), 16).expect("pid"),
        codec: field(parts, "codec=").to_string(),
        feeds: field(parts, "feeds=").parse().expect("feeds"),
        observation: observation_of(parts),
    }
}

fn parse_corpus(text: &str) -> Vec<Case> {
    let mut cases = Vec::new();
    let mut current: Option<Case> = None;
    let mut version_seen = false;
    for line in text.lines() {
        let line = line.trim_end();
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        if let Some(v) = line.strip_prefix("version ") {
            assert_eq!(v, "1", "unknown corpus format version");
            version_seen = true;
            continue;
        }
        if let Some(name) = line.strip_prefix("case ") {
            assert!(current.is_none(), "case inside a case");
            current = Some(Case {
                name: name.to_string(),
                program: 0,
                steps: Vec::new(),
                want: Vec::new(),
                streams: Vec::new(),
                diverges: None,
                reference: Vec::new(),
                reference_streams: Vec::new(),
            });
            continue;
        }
        if line == "end" {
            cases.push(current.take().expect("end outside a case"));
            continue;
        }
        let c = current.as_mut().expect("line outside a case");
        let trimmed = line.trim_start();
        let parts: Vec<&str> = trimmed.split(' ').collect();
        match parts[0] {
            "desc" => {}
            "program" => c.program = parts[1].parse().expect("program"),
            "target" => c
                .steps
                .push(Step::Target(parts[1].parse().expect("target"))),
            "chunk" => c.steps.push(Step::Chunk(parse_hex(parts[1]))),
            "diverges" => c.diverges = Some(parts[1..].join(" ")),
            "feed" => c.want.push(feed_of(&parts[1..])),
            "stream" => c.streams.push(stream_of(&parts[1..])),
            "ref-feed" => c.reference.push(feed_of(&parts[1..])),
            "ref-stream" => c.reference_streams.push(stream_of(&parts[1..])),
            other => panic!("unknown corpus line {other}"),
        }
    }
    assert!(version_seen, "the corpus does not state its format version");
    assert!(current.is_none(), "a case was never ended");
    assert!(!cases.is_empty(), "the corpus is empty");
    cases
}

/// The one order that survives rechunking: streams in the order their first
/// feed appeared, and each stream's feeds in the order they were given.
fn canonical(feeds: &[Feed]) -> Vec<Feed> {
    let mut order: Vec<(usize, u16)> = Vec::new();
    for f in feeds {
        let key = (f.incarnation, f.pid);
        if !order.contains(&key) {
            order.push(key);
        }
    }
    let mut out = Vec::with_capacity(feeds.len());
    for key in order {
        out.extend(
            feeds
                .iter()
                .filter(|f| (f.incarnation, f.pid) == key)
                .cloned(),
        );
    }
    out
}

/// Runs one case, with the incarnations numbered the way the corpus numbers
/// them: by the order their first feed appears, not by this core's counter.
fn run(case: &Case, rechunk: Option<usize>) -> (Vec<Feed>, Vec<Stream>) {
    let mut ing = AudioIngress::new(case.program);
    let mut raw: Vec<(u64, u16, Vec<u8>, Observation)> = Vec::new();
    let mut offset: i64 = 0;
    for step in &case.steps {
        match step {
            Step::Target(n) => ing.set_target_program(*n),
            Step::Chunk(data) => {
                let size = rechunk.unwrap_or(data.len()).max(188);
                for part in data.chunks(size) {
                    let out = ing.ingest(offset, part).expect("aligned");
                    offset = out.processed_through;
                    for f in out.feeds {
                        raw.push((f.incarnation, f.pid, f.es.to_vec(), f.observation));
                    }
                }
            }
        }
    }

    let mut seen: Vec<u64> = Vec::new();
    let feeds: Vec<Feed> = raw
        .into_iter()
        .map(|(incarnation, pid, es, observation)| {
            if !seen.contains(&incarnation) {
                seen.push(incarnation);
            }
            Feed {
                incarnation: seen.iter().position(|i| *i == incarnation).expect("seen"),
                pid,
                es,
                observation,
            }
        })
        .collect();

    let streams = ing
        .followed()
        .into_iter()
        .map(|s| Stream {
            pid: s.pid,
            codec: s.codec,
            feeds: s.feeds,
            observation: s.observation,
        })
        .collect();
    (canonical(&feeds), streams)
}

fn describe(f: &Feed) -> String {
    let o = &f.observation;
    format!(
        "inc={} pid={:04x} es={} channels={} lfe={} acmod={} hasAcmod={} dependent={} frames={}",
        f.incarnation,
        f.pid,
        f.es.iter().fold(String::new(), |mut acc, b| {
            use std::fmt::Write as _;
            let _ = write!(acc, "{b:02x}");
            acc
        }),
        o.channels,
        u8::from(o.lfe),
        o.acmod,
        u8::from(o.has_acmod),
        u8::from(o.dependent_substream),
        o.frames
    )
}

fn compare(name: &str, what: &str, got: &[Feed], want: &[Feed]) {
    assert_eq!(
        got.len(),
        want.len(),
        "{name}: {what} count\n  got  {}\n  want {}",
        got.len(),
        want.len()
    );
    for (i, (g, w)) in got.iter().zip(want).enumerate() {
        assert_eq!(
            describe(g),
            describe(w),
            "{name}: {what} {i}\n  got  {}\n  want {}",
            describe(g),
            describe(w)
        );
    }
}

#[test]
fn the_rust_ingress_answers_the_shared_corpus() {
    let text = std::fs::read_to_string(corpus_path()).expect("the corpus is checked in");
    let cases = parse_corpus(&text);
    for case in &cases {
        let (feeds, streams) = run(case, None);
        compare(&case.name, "feed", &feeds, &canonical(&case.want));
        assert_eq!(
            streams.len(),
            case.streams.len(),
            "{}: streams followed",
            case.name
        );
        for (got, want) in streams.iter().zip(&case.streams) {
            assert_eq!(got, want, "{}: stream", case.name);
        }
    }
}

/// Where a call was cut is the caller's business and no part of the answer.
#[test]
fn where_a_chunk_was_cut_changes_nothing() {
    let text = std::fs::read_to_string(corpus_path()).expect("the corpus is checked in");
    for case in parse_corpus(&text) {
        let (base, base_streams) = run(&case, None);
        for size in [188usize, 376, 564, 348 * 188] {
            let (got, streams) = run(&case, Some(size));
            compare(
                &case.name,
                &format!("feed at {size} bytes a call"),
                &got,
                &base,
            );
            assert_eq!(streams, base_streams, "{} at {size}", case.name);
        }
    }
}

/// The divergence is a difference, and this is where it is held to being one.
///
/// If the reference's trace ever equalled the authored one, the case would have
/// stopped testing anything and nobody would be told.
#[test]
fn the_recorded_divergence_really_is_one() {
    let text = std::fs::read_to_string(corpus_path()).expect("the corpus is checked in");
    let cases = parse_corpus(&text);
    let diverging: Vec<&super::corpus_test::Case> =
        cases.iter().filter(|c| c.diverges.is_some()).collect();
    assert_eq!(
        diverging.len(),
        1,
        "exactly one case may diverge from the reference"
    );
    let case = diverging[0];
    assert!(
        !case.reference.is_empty(),
        "{}: a divergence with no reference trace records nothing",
        case.name
    );
    let authored = canonical(&case.want);
    let reference = canonical(&case.reference);
    assert_ne!(
        authored.iter().map(describe).collect::<Vec<_>>(),
        reference.iter().map(describe).collect::<Vec<_>>(),
        "{}: the reference trace is identical to the authored one",
        case.name
    );
    // And this side answers the authored one.
    let (feeds, _) = run(case, None);
    compare(&case.name, "feed", &feeds, &authored);
}
