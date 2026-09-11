// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! The archived Step-5d transport, replayed through the audio path.
//!
//! This writes what the ingress fed, and writes nothing else. The comparison
//! against the reference is a diff of two files produced independently - see
//! `media-core/scripts/audio-hardware-differential.sh` - rather than one
//! implementation checking itself against a number the other one handed it.
//!
//! Two files per case, because a digest of the bytes would only prove the
//! bytes agree if the digest were right:
//!
//! - `<case>.rust.trace`, one line per feed and per stream still followed
//! - `<case>.rust.es`, the elementary stream bytes in feed order
//!
//! The traces carry the boundaries and the counts; the byte files carry the
//! bytes. Both have to match for the run to mean anything.
//!
//! Skipped unless `XG2G_AUDIO_HARDWARE_DIR` names the archive, because the
//! captures are tens of megabytes of real broadcast and are not in the
//! repository.

use std::collections::BTreeMap;
use std::fmt::Write as _;
use std::fs;
use std::io::Write as _;
use std::path::{Path, PathBuf};

use xg2g_media_core::ingress::AudioIngress;

/// One capture's identity, as the Step-5d manifest states it.
struct Capture {
    size: u64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
enum Step {
    Feed(String),
    SetTarget(u16),
    NewCore,
}

struct Case {
    name: String,
    target: u16,
    chunk: usize,
    steps: Vec<Step>,
}

fn repo_root() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("..")
}

/// The capture identities, read from the Step-5d manifest and from nowhere
/// else. Two copies of a hash are two things that can disagree about which
/// bytes a result came from.
fn captures() -> BTreeMap<String, Capture> {
    let text = fs::read_to_string(repo_root().join("testdata/psi-hardware/manifest.txt"))
        .expect("the Step-5d manifest is checked in");
    let mut out = BTreeMap::new();
    for line in text.lines() {
        let line = line.trim();
        if let Some(rest) = line.strip_prefix("capture ") {
            let parts: Vec<&str> = rest.split_whitespace().collect();
            assert_eq!(parts.len(), 3, "capture line: {line}");
            out.insert(
                parts[0].to_string(),
                Capture {
                    size: parts[1].parse().expect("size"),
                },
            );
        }
    }
    assert!(!out.is_empty(), "the Step-5d manifest names no captures");
    out
}

fn cases() -> Vec<Case> {
    let text = fs::read_to_string(repo_root().join("testdata/audio-ts-hardware/manifest.txt"))
        .expect("the Step-6c manifest is checked in");
    let mut out = Vec::new();
    let mut current: Option<Case> = None;
    for line in text.lines() {
        let line = line.trim();
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        if let Some(v) = line.strip_prefix("version ") {
            assert_eq!(v, "1", "unknown manifest version");
            continue;
        }
        if let Some(name) = line.strip_prefix("case ") {
            current = Some(Case {
                name: name.to_string(),
                target: 0,
                chunk: 0,
                steps: Vec::new(),
            });
            continue;
        }
        if line == "end" {
            let c = current.take().expect("end outside a case");
            assert!(c.chunk > 0, "case {} states no chunk size", c.name);
            assert!(
                c.chunk.is_multiple_of(188),
                "case {} has a chunk size that is not whole packets",
                c.name
            );
            out.push(c);
            continue;
        }
        let c = current.as_mut().expect("line outside a case");
        let parts: Vec<&str> = line.split_whitespace().collect();
        match parts[0] {
            "target" => c.target = parts[1].parse().expect("target"),
            "chunk" => c.chunk = parts[1].parse().expect("chunk"),
            "feed" => c.steps.push(Step::Feed(parts[1].to_string())),
            "settarget" => c
                .steps
                .push(Step::SetTarget(parts[1].parse().expect("settarget"))),
            "newcore" => c.steps.push(Step::NewCore),
            other => panic!("unknown manifest line {other}"),
        }
    }
    assert!(current.is_none(), "a case was never ended");
    assert!(!out.is_empty(), "the manifest names no cases");
    out
}

#[test]
fn the_archived_transport_is_replayed_through_the_audio_path() {
    let Ok(dir) = std::env::var("XG2G_AUDIO_HARDWARE_DIR") else {
        eprintln!("XG2G_AUDIO_HARDWARE_DIR is unset; the archive is not here");
        return;
    };
    let out_dir = std::env::var("XG2G_AUDIO_HARDWARE_OUT")
        .expect("XG2G_AUDIO_HARDWARE_OUT must say where the traces go");
    let dir = PathBuf::from(dir);
    let out_dir = PathBuf::from(out_dir);
    fs::create_dir_all(&out_dir).expect("create the output directory");

    let identities = captures();
    let mut loaded: BTreeMap<String, Vec<u8>> = BTreeMap::new();

    for case in cases() {
        let mut ing = AudioIngress::new(case.target);
        let mut trace = String::new();
        let mut bytes: Vec<u8> = Vec::new();
        let mut offset: i64 = 0;
        // Incarnations are numbered by the order their first feed appears, so
        // the trace says where the stream was split without saying what either
        // side called the pieces.
        //
        // Keyed by the core it came from as well as by its own number: a new
        // core starts counting again, and two streams from different cores
        // sharing a number would be reported as one.
        let mut seen: Vec<(usize, u64)> = Vec::new();
        let mut core_index = 0usize;

        for step in &case.steps {
            match step {
                Step::SetTarget(n) => ing.set_target_program(*n),
                Step::NewCore => {
                    // Production destroys the parser when the upstream ends. A
                    // new core is the boundary, and the incarnation numbering
                    // carries across it because the trace is the case's, not
                    // the core's.
                    ing = AudioIngress::new(case.target);
                    offset = 0;
                    core_index += 1;
                }
                Step::Feed(name) => {
                    let data = loaded.entry(name.clone()).or_insert_with(|| {
                        let path = dir.join(format!("{name}.ts"));
                        let data = fs::read(&path)
                            .unwrap_or_else(|e| panic!("read {}: {e}", path.display()));
                        let want = identities
                            .get(name)
                            .unwrap_or_else(|| panic!("no capture named {name} in the manifest"));
                        assert_eq!(
                            data.len() as u64,
                            want.size,
                            "{name} is not the capture the manifest names"
                        );
                        data
                    });
                    for part in data.chunks(case.chunk) {
                        let outcome = ing.ingest(offset, part).expect("aligned");
                        offset = outcome.processed_through;
                        for f in outcome.feeds {
                            let key = (core_index, f.incarnation);
                            if !seen.contains(&key) {
                                seen.push(key);
                            }
                            let inc = seen.iter().position(|k| *k == key).expect("just seen");
                            writeln!(trace, "feed inc={inc} pid={:04x} len={}", f.pid, f.es.len())
                                .expect("write");
                            bytes.extend_from_slice(f.es);
                        }
                    }
                }
            }
        }

        for s in ing.followed() {
            let o = s.observation;
            writeln!(
                trace,
                "stream pid={:04x} codec={} feeds={} channels={} lfe={} acmod={} hasAcmod={} dependent={} frames={}",
                s.pid, s.codec, s.feeds, o.channels, u8::from(o.lfe), o.acmod,
                u8::from(o.has_acmod), u8::from(o.dependent_substream), o.frames
            )
            .expect("write");
            // Not compared with the reference, which counts none of this.
            // Recorded because "the divergence never arises on real transport"
            // has to be a number somebody measured.
            writeln!(
                trace,
                "counts pid={:04x} clear={} scrambled={} pesStarts={} headerIncomplete={}",
                s.pid, s.clear_packets, s.scrambled_packets, s.pes_starts, s.header_incomplete
            )
            .expect("write");
        }

        fs::write(out_dir.join(format!("{}.rust.trace", case.name)), &trace)
            .expect("write the trace");
        let mut f = fs::File::create(out_dir.join(format!("{}.rust.es", case.name)))
            .expect("create the byte file");
        f.write_all(&bytes).expect("write the bytes");
    }
}
