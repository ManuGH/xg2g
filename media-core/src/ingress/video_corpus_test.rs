// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! The shared raw-transport-to-video-facts corpus, read and enforced.
//!
//! Step 7a focuses strictly on Transport + PES facts:
//! - vpid
//! - codec
//! - vclr (clear packets)
//! - vscr (scrambled packets)
//! - vrun (clear run)
//! - scrconf (scrambled confirmed)
//!
//! NAL, AU, and RAP fields are deferred to Steps 7b and 7c.

use super::VideoIngress;
use std::path::{Path, PathBuf};

fn corpus_path() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../testdata/video-ts-corpus/corpus.txt")
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct ExpectedFacts {
    vpid: u16,
    codec: String,
    ps: bool,
    vclr: u64,
    vscr: u64,
    vrun: u64,
    scrconf: u8,
}

#[derive(Debug, Clone, PartialEq, Eq)]
enum Action {
    Chunk(Vec<u8>),
    Target(u16),
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct Step {
    action: Action,
    facts: Option<ExpectedFacts>,
}

#[derive(Debug)]
struct Case {
    name: String,
    program: u16,
    steps: Vec<Step>,
}

fn parse_hex(s: &str) -> Vec<u8> {
    assert!(s.len().is_multiple_of(2), "odd-length hex");
    (0..s.len() / 2)
        .map(|i| u8::from_str_radix(&s[i * 2..i * 2 + 2], 16).expect("hex"))
        .collect()
}

fn field<'a>(parts: &[&'a str], key: &str) -> &'a str {
    for p in parts {
        if let Some(v) = p.strip_prefix(key) {
            return v;
        }
    }
    panic!("no {key} in {parts:?}");
}

fn parse_facts(parts: &[&str]) -> ExpectedFacts {
    ExpectedFacts {
        vpid: u16::from_str_radix(field(parts, "vpid="), 16).expect("vpid"),
        codec: field(parts, "codec=").to_string(),
        ps: field(parts, "ps=").parse::<u8>().expect("ps") != 0,
        vclr: field(parts, "vclr=").parse().expect("vclr"),
        vscr: field(parts, "vscr=").parse().expect("vscr"),
        vrun: field(parts, "vrun=").parse().expect("vrun"),
        scrconf: field(parts, "scrconf=").parse().expect("scrconf"),
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
            "desc" | "event" | "ref-event" | "diverges" | "ref-facts" => {
                // Not evaluated in Step 7a
            }
            "program" => c.program = parts[1].parse().expect("program"),
            "target" => c.steps.push(Step {
                action: Action::Target(parts[1].parse().expect("target")),
                facts: None,
            }),
            "chunk" => c.steps.push(Step {
                action: Action::Chunk(parse_hex(parts[1])),
                facts: None,
            }),
            "facts" => {
                let facts = parse_facts(&parts[1..]);
                c.steps
                    .last_mut()
                    .expect("facts line before any step")
                    .facts = Some(facts);
            }
            other => panic!("unknown corpus line {other}"),
        }
    }
    assert!(version_seen, "the corpus does not state its format version");
    assert!(current.is_none(), "a case was never ended");
    assert!(!cases.is_empty(), "the corpus is empty");
    cases
}

#[test]
fn video_ts_corpus_all_cases() {
    let text = std::fs::read_to_string(corpus_path()).expect("read corpus.txt");
    let cases = parse_corpus(&text);
    assert!(!cases.is_empty(), "corpus must not be empty");
    assert!(
        cases.len() >= 94,
        "expected at least 94 corpus cases, got {}",
        cases.len()
    );

    for case in &cases {
        let mut ingress = VideoIngress::new(case.program);
        let mut offset: i64 = 0;
        for (step_idx, step) in case.steps.iter().enumerate() {
            match &step.action {
                Action::Target(p) => {
                    ingress.set_target_program(*p);
                }
                Action::Chunk(bytes) => {
                    let outcome = ingress.ingest(offset, bytes).unwrap_or_else(|e| {
                        panic!("case {} step {}: {:?}", case.name, step_idx, e)
                    });
                    let chunk_len = i64::try_from(bytes.len()).unwrap();
                    assert_eq!(
                        outcome.processed_through,
                        offset.saturating_add(chunk_len),
                        "case {} step {}: processed_through mismatch",
                        case.name,
                        step_idx
                    );
                    offset = offset.saturating_add(chunk_len);
                }
            }
            if let Some(expected) = &step.facts {
                let facts = ingress.facts();
                assert_eq!(
                    facts.parameter_sets_seen, expected.ps,
                    "case {} step {}: ps mismatch",
                    case.name, step_idx
                );
                assert_eq!(
                    facts.pid, expected.vpid,
                    "case {} step {}: vpid mismatch",
                    case.name, step_idx
                );
                assert_eq!(
                    facts.codec.as_str(),
                    expected.codec,
                    "case {} step {}: codec mismatch",
                    case.name,
                    step_idx
                );
                assert_eq!(
                    facts.clear_packets, expected.vclr,
                    "case {} step {}: vclr mismatch",
                    case.name, step_idx
                );
                assert_eq!(
                    facts.scrambled_packets, expected.vscr,
                    "case {} step {}: vscr mismatch",
                    case.name, step_idx
                );
                assert_eq!(
                    facts.clear_run, expected.vrun,
                    "case {} step {}: vrun mismatch",
                    case.name, step_idx
                );
                assert_eq!(
                    u8::from(facts.scrambled_confirmed),
                    expected.scrconf,
                    "case {} step {}: scrconf mismatch",
                    case.name,
                    step_idx
                );
            }
        }
    }
}
