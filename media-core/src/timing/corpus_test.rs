// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! Authored timing publication corpus test.
//!
//! Reads `testdata/timing-corpus/corpus.txt` and verifies that Rust `VideoIngress`
//! produces the exact authored canonical timeline events and records.

#![allow(clippy::similar_names, clippy::too_many_lines, clippy::collapsible_if)]

use std::path::{Path, PathBuf};

use crate::ingress::video::{VideoEvent, VideoIngress};
use crate::timing::types::{
    DiscontinuityReason, ExtendedDts90k, ExtendedPts90k, TimelineEpoch, TimingPoint, TimingRecord,
    TimingResetScope,
};

fn corpus_path() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../testdata/timing-corpus/corpus.txt")
}

#[derive(Debug, PartialEq, Eq)]
enum ExpectedRecord {
    Pes {
        epoch: u64,
        pid: u16,
        pts: Option<i64>,
        dts: Option<i64>,
        obs: i64,
        sub: i64,
    },
    Pcr {
        epoch: u64,
        pid: u16,
        pcr: i64,
        obs: i64,
    },
    Disc {
        scope: String,
        reason: String,
        obs: i64,
        before: Option<u64>,
        after: Option<u64>,
    },
}

#[derive(Debug)]
struct Step {
    chunk: Vec<u8>,
    expected_epoch: Option<u64>,
    events: Vec<VideoEvent>,
    records: Vec<ExpectedRecord>,
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

fn parse_event(parts: &[&str]) -> VideoEvent {
    match parts[0] {
        "identity" => VideoEvent::ProgramIdentityChanged,
        "rap" => {
            let offset: i64 = field(&parts[1..], "offset=").parse().expect("offset");
            let joinable: bool = field(&parts[1..], "joinable=")
                .parse::<u8>()
                .expect("joinable")
                != 0;
            VideoEvent::RandomAccessPoint { offset, joinable }
        }
        "rap_invalidated" => {
            let offset: i64 = field(&parts[1..], "offset=").parse().expect("offset");
            VideoEvent::RandomAccessPointInvalidated { offset }
        }
        other => panic!("unknown event type {other}"),
    }
}

fn parse_record(parts: &[&str]) -> ExpectedRecord {
    match parts[0] {
        "pes" => {
            let epoch: u64 = field(&parts[1..], "epoch=").parse().expect("epoch");
            let pid: u16 = field(&parts[1..], "pid=").parse().expect("pid");
            let pts_str = field(&parts[1..], "pts=");
            let pts = if pts_str == "none" {
                None
            } else {
                Some(pts_str.parse().expect("pts"))
            };
            let dts_str = field(&parts[1..], "dts=");
            let dts = if dts_str == "none" {
                None
            } else {
                Some(dts_str.parse().expect("dts"))
            };
            let obs: i64 = field(&parts[1..], "obs=").parse().expect("obs");
            let sub: i64 = field(&parts[1..], "sub=").parse().expect("sub");
            ExpectedRecord::Pes {
                epoch,
                pid,
                pts,
                dts,
                obs,
                sub,
            }
        }
        "pcr" => {
            let epoch: u64 = field(&parts[1..], "epoch=").parse().expect("epoch");
            let pid: u16 = field(&parts[1..], "pid=").parse().expect("pid");
            let pcr: i64 = field(&parts[1..], "pcr=").parse().expect("pcr");
            let obs: i64 = field(&parts[1..], "obs=").parse().expect("obs");
            ExpectedRecord::Pcr {
                epoch,
                pid,
                pcr,
                obs,
            }
        }
        "disc" => {
            let scope = field(&parts[1..], "scope=").to_string();
            let reason = field(&parts[1..], "reason=").to_string();
            let obs: i64 = field(&parts[1..], "obs=").parse().expect("obs");
            let before_str = field(&parts[1..], "before=");
            let before = if before_str == "none" {
                None
            } else {
                Some(before_str.parse().expect("before"))
            };
            let after_str = field(&parts[1..], "after=");
            let after = if after_str == "none" {
                None
            } else {
                Some(after_str.parse().expect("after"))
            };
            ExpectedRecord::Disc {
                scope,
                reason,
                obs,
                before,
                after,
            }
        }
        other => panic!("unknown record type {other}"),
    }
}

fn parse_corpus(text: &str) -> Vec<Case> {
    let mut cases = Vec::new();
    let mut current: Option<Case> = None;
    let mut step: Option<Step> = None;

    for line in text.lines() {
        let line = line.trim();
        if line.is_empty() || line.starts_with('#') {
            continue;
        }

        let parts: Vec<&str> = line.split_whitespace().collect();
        match parts[0] {
            "version" => assert_eq!(parts[1], "1", "unsupported version"),
            "case" => {
                current = Some(Case {
                    name: parts[1].to_string(),
                    program: 1,
                    steps: Vec::new(),
                });
            }
            "desc" => {}
            "program" => {
                if let Some(ref mut c) = current {
                    c.program = parts[1].parse().expect("program");
                }
            }
            "chunk" => {
                if let Some(s) = step.take() {
                    if let Some(ref mut c) = current {
                        c.steps.push(s);
                    }
                }
                step = Some(Step {
                    chunk: parse_hex(parts[1]),
                    expected_epoch: None,
                    events: Vec::new(),
                    records: Vec::new(),
                });
            }
            "epoch" => {
                let ep = if parts[1] == "none" {
                    None
                } else {
                    Some(parts[1].parse().expect("epoch"))
                };
                if let Some(ref mut s) = step {
                    s.expected_epoch = ep;
                }
            }
            "event" => {
                if let Some(ref mut s) = step {
                    s.events.push(parse_event(&parts[1..]));
                }
            }
            "record" => {
                if let Some(ref mut s) = step {
                    s.records.push(parse_record(&parts[1..]));
                }
            }
            "end" => {
                if let Some(s) = step.take() {
                    if let Some(ref mut c) = current {
                        c.steps.push(s);
                    }
                }
                if let Some(c) = current.take() {
                    cases.push(c);
                }
            }
            other => panic!("unexpected corpus line: {other}"),
        }
    }

    cases
}

#[test]
fn the_rust_core_answers_the_authored_timing_corpus() {
    let text = std::fs::read_to_string(corpus_path()).expect("read timing corpus");
    let cases = parse_corpus(&text);
    assert!(cases.len() >= 4, "expected at least 4 cases");

    for c in cases {
        let mut ingress = VideoIngress::new(c.program);
        let mut offset = 0i64;

        for (step_idx, step) in c.steps.into_iter().enumerate() {
            let outcome = ingress.ingest(offset, &step.chunk).unwrap_or_else(|e| {
                panic!("case {} step {} ingest error: {:?}", c.name, step_idx, e)
            });
            offset = outcome.processed_through;

            // 1. Check active epoch
            let actual_epoch = ingress.timeline().active_epoch().map(TimelineEpoch::get);
            assert_eq!(
                actual_epoch, step.expected_epoch,
                "case {} step {}: epoch mismatch",
                c.name, step_idx
            );

            // 2. Check events
            assert_eq!(
                outcome.events, step.events,
                "case {} step {}: events mismatch",
                c.name, step_idx
            );

            // 3. Check timing records
            assert_eq!(
                outcome.timing_records.len(),
                step.records.len(),
                "case {} step {}: record count mismatch. Got: {:?}",
                c.name,
                step_idx,
                outcome.timing_records
            );

            for (rec_idx, (actual, expected)) in outcome
                .timing_records
                .into_iter()
                .zip(step.records)
                .enumerate()
            {
                match (actual, expected) {
                    (
                        TimingRecord::Pes(TimingPoint {
                            epoch,
                            pid,
                            pts,
                            dts,
                            observed_at,
                            subject_at,
                        }),
                        ExpectedRecord::Pes {
                            epoch: exp_epoch,
                            pid: exp_pid,
                            pts: exp_pts,
                            dts: exp_dts,
                            obs: exp_obs,
                            sub: exp_sub,
                        },
                    ) => {
                        assert_eq!(epoch.get(), exp_epoch);
                        assert_eq!(pid.get(), exp_pid);
                        assert_eq!(pts.map(ExtendedPts90k::get), exp_pts);
                        assert_eq!(dts.map(ExtendedDts90k::get), exp_dts);
                        assert_eq!(observed_at.get(), exp_obs);
                        assert_eq!(subject_at.get(), exp_sub);
                    }
                    (
                        TimingRecord::Pcr {
                            epoch,
                            pid,
                            observed_at,
                            pcr_27m,
                        },
                        ExpectedRecord::Pcr {
                            epoch: exp_epoch,
                            pid: exp_pid,
                            pcr: exp_pcr,
                            obs: exp_obs,
                        },
                    ) => {
                        assert_eq!(epoch.get(), exp_epoch);
                        assert_eq!(pid.get(), exp_pid);
                        assert_eq!(observed_at.get(), exp_obs);
                        assert_eq!(pcr_27m.get(), exp_pcr);
                    }
                    (
                        TimingRecord::Discontinuity {
                            scope,
                            reason,
                            observed_at,
                            epoch_before,
                            epoch_after,
                        },
                        ExpectedRecord::Disc {
                            scope: exp_scope,
                            reason: exp_reason,
                            obs: exp_obs,
                            before: exp_before,
                            after: exp_after,
                        },
                    ) => {
                        let scope_str = match scope {
                            TimingResetScope::Program => "program".to_string(),
                            TimingResetScope::Track(p) => format!("track:{}", p.get()),
                        };
                        assert_eq!(scope_str, exp_scope);
                        let reason_str = match reason {
                            DiscontinuityReason::ProgramIdentityChanged => {
                                "program_identity_changed"
                            }
                            DiscontinuityReason::PcrPidChanged => "pcr_pid_changed",
                            DiscontinuityReason::PcrDiscontinuityIndicator => {
                                "pcr_discontinuity_indicator"
                            }
                            DiscontinuityReason::TransportTimingLoss => "transport_timing_loss",
                        };
                        assert_eq!(reason_str, exp_reason);
                        assert_eq!(observed_at.get(), exp_obs);
                        assert_eq!(epoch_before.map(TimelineEpoch::get), exp_before);
                        assert_eq!(epoch_after.map(TimelineEpoch::get), exp_after);
                    }
                    (act, exp) => panic!(
                        "case {} step {} rec {}: record variant mismatch: act={:?}, exp={:?}",
                        c.name, step_idx, rec_idx, act, exp
                    ),
                }
            }
        }
    }
}
