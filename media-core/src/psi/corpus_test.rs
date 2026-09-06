// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//! The shared PSI corpus, read and enforced.
//!
//! The file this reads is generated from the Go side's authored expectations and
//! checked into the repository. Both implementations are held to it, which is
//! what makes agreement a property of a reviewable artefact rather than of two
//! parsers written by the same hand on the same day.
//!
//! The reader below is deliberately fail-closed. A corpus it cannot parse
//! exactly is a corpus that would silently check less than it claims to, and a
//! test that checks three cases and passes is worse than one that fails.

use super::{ChannelDeclaration, PsiCore, PsiEvent};
use std::collections::BTreeSet;
use std::fmt::Write as _;
use std::path::{Path, PathBuf};

/// The format version this reader understands.
const FORMAT_VERSION: &str = "2";

/// The number of cases the corpus had when this reader was written.
///
/// A floor rather than an equality so cases added later run without a change
/// here, and a floor at all so a reader bug that finds three cases cannot pass.
const MINIMUM_CASES: usize = 79;

fn corpus_path() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../testdata/psi-corpus/corpus.txt")
}

/// One call into the core.
#[derive(Debug)]
enum Call {
    /// `ingest` of these bytes.
    Chunk(Vec<u8>),
    /// `set_target_program`.
    Target(u16),
}

/// What a call has to produce.
#[derive(Debug, Default)]
struct Expect {
    through: i64,
    has_pat: bool,
    has_pmt: bool,
    pmt_version: u8,
    program_number: u16,
    pmt_pid: u16,
    video_pid: u16,
    video_codec: String,
    audio_pids: Vec<u16>,
    tracks: Vec<ExpectTrack>,
    events: Vec<String>,
    /// The accepted sections of each table in force, in `section_number` order,
    /// each as its exact bytes. Version 1 of the corpus named the packets the
    /// sections arrived in; version 2 names the sections, because the
    /// packetization is the sender's and the table is not.
    pat_sections: Vec<Vec<u8>>,
    pmt_sections: Vec<Vec<u8>>,
}

/// One expected audio track line.
#[derive(Debug, PartialEq, Eq)]
struct ExpectTrack {
    pid: u16,
    stream_type: u8,
    codec: String,
    language: String,
    channels: u8,
    multichannel: bool,
    component_type: u8,
    has_component_type: bool,
}

/// One call and its expectation.
#[derive(Debug)]
struct Step {
    call: Call,
    expect: Expect,
}

/// One case: a core built with a target, then a sequence of calls.
#[derive(Debug)]
struct Case {
    name: String,
    note: Option<String>,
    initial_target: u16,
    steps: Vec<Step>,
}

/// Parses hex, refusing anything that is not exactly hex.
fn parse_hex(text: &str) -> Result<Vec<u8>, String> {
    if !text.len().is_multiple_of(2) {
        return Err(format!("odd-length hex, {} characters", text.len()));
    }
    let bytes = text.as_bytes();
    let mut out = Vec::with_capacity(text.len() / 2);
    let mut at = 0;
    while at < bytes.len() {
        let hi = (bytes[at] as char)
            .to_digit(16)
            .ok_or_else(|| format!("bad hex digit at {at}"))?;
        let lo = (bytes[at + 1] as char)
            .to_digit(16)
            .ok_or_else(|| format!("bad hex digit at {}", at + 1))?;
        out.push(u8::try_from(hi * 16 + lo).expect("two hex digits are one byte"));
        at += 2;
    }
    Ok(out)
}

/// Splits `k=v k=v` into a map, refusing a repeated key or a field with no value.
fn parse_fields(text: &str) -> Result<Vec<(String, String)>, String> {
    let mut out: Vec<(String, String)> = Vec::new();
    let mut seen = BTreeSet::new();
    for field in text.split_whitespace() {
        let (key, value) = field
            .split_once('=')
            .ok_or_else(|| format!("field without a value: {field}"))?;
        if !seen.insert(key.to_owned()) {
            return Err(format!("field {key} appears twice"));
        }
        out.push((key.to_owned(), value.to_owned()));
    }
    Ok(out)
}

/// Reads a field set, requiring exactly `wanted` and nothing else.
fn take_fields(text: &str, wanted: &[&str]) -> Result<Vec<String>, String> {
    let fields = parse_fields(text)?;
    for (key, _) in &fields {
        if !wanted.contains(&key.as_str()) {
            return Err(format!("unknown field {key}"));
        }
    }
    wanted
        .iter()
        .map(|want| {
            fields
                .iter()
                .find(|(key, _)| key == want)
                .map(|(_, value)| value.clone())
                .ok_or_else(|| format!("missing field {want}"))
        })
        .collect()
}

fn parse_bit(text: &str) -> Result<bool, String> {
    match text {
        "0" => Ok(false),
        "1" => Ok(true),
        other => Err(format!("expected 0 or 1, got {other}")),
    }
}

fn parse_num<T: std::str::FromStr>(text: &str) -> Result<T, String> {
    text.parse().map_err(|_| format!("not a number: {text}"))
}

/// Parses a possibly empty comma separated list.
fn parse_list<T: std::str::FromStr>(text: &str) -> Result<Vec<T>, String> {
    if text.is_empty() {
        return Ok(Vec::new());
    }
    text.split(',').map(parse_num).collect()
}

/// Reads a `facts` line into the expectation it belongs to.
fn apply_facts(expect: &mut Expect, rest: &str) -> Result<(), String> {
    let values = take_fields(
        rest,
        &[
            "through",
            "hasPAT",
            "hasPMT",
            "pmtVersion",
            "programNumber",
            "pmtPID",
            "videoPID",
            "videoCodec",
            "audioPIDs",
        ],
    )?;
    expect.through = parse_num(&values[0])?;
    expect.has_pat = parse_bit(&values[1])?;
    expect.has_pmt = parse_bit(&values[2])?;
    expect.pmt_version = parse_num(&values[3])?;
    expect.program_number = parse_num(&values[4])?;
    expect.pmt_pid = parse_num(&values[5])?;
    expect.video_pid = parse_num(&values[6])?;
    expect.video_codec = values[7].clone();
    expect.audio_pids = parse_list(&values[8])?;
    Ok(())
}

/// Reads one `track` line.
fn apply_track(expect: &mut Expect, rest: &str) -> Result<(), String> {
    let values = take_fields(
        rest,
        &[
            "pid",
            "streamType",
            "codec",
            "lang",
            "channels",
            "multichannel",
            "componentType",
            "hasComponentType",
        ],
    )?;
    expect.tracks.push(ExpectTrack {
        pid: parse_num(&values[0])?,
        stream_type: parse_num(&values[1])?,
        codec: values[2].clone(),
        language: values[3].clone(),
        channels: parse_num(&values[4])?,
        multichannel: parse_bit(&values[5])?,
        component_type: parse_num(&values[6])?,
        has_component_type: parse_bit(&values[7])?,
    });
    Ok(())
}

/// Reads the `psi` line naming the sections of the tables in force.
fn apply_psi(expect: &mut Expect, rest: &str) -> Result<(), String> {
    let values = take_fields(rest, &["pat", "pmt"])?;
    expect.pat_sections = parse_section_list(&values[0])?;
    expect.pmt_sections = parse_section_list(&values[1])?;
    Ok(())
}

/// Reads a comma-separated list of sections, each as hex.
///
/// A section shorter than a header or longer than one may be is refused here
/// rather than compared: the corpus would be describing a table that cannot
/// exist, and a reader that accepted it would be agreeing with a damaged file.
fn parse_section_list(text: &str) -> Result<Vec<Vec<u8>>, String> {
    if text.is_empty() {
        return Ok(Vec::new());
    }
    let mut out = Vec::new();
    for field in text.split(',') {
        let section = parse_hex(field)?;
        if section.len() < 3 || section.len() > super::table::MAX_SECTION_BYTES {
            return Err(format!(
                "a section of {} bytes, which no section may be",
                section.len()
            ));
        }
        out.push(section);
    }
    if out.len() > super::table::MAX_SECTIONS_PER_TABLE {
        return Err(format!(
            "{} sections, more than a table may have",
            out.len()
        ));
    }
    Ok(out)
}

/// The corpus reader, one line at a time.
///
/// A struct rather than one long loop so each kind of line is answered in one
/// small place, and so "what is allowed here" is a question about the state
/// rather than about how far down a function the reader happens to be.
/// Which of a call's expectation lines have been seen. They have to arrive in
/// this order and exactly once, which is what stops a corpus from quietly
/// describing a call twice or not at all.
#[derive(Default)]
struct StepLines {
    facts: bool,
    events: bool,
    psi: bool,
}

#[derive(Default)]
struct Reader {
    cases: Vec<Case>,
    current: Option<Case>,
    pending: Option<(Call, Expect)>,
    saw_version: bool,
    seen: StepLines,
}

impl Reader {
    /// Files the call that has finished, refusing one that never got its
    /// expectation.
    fn close_step(&mut self) -> Result<(), String> {
        let Some((call, expect)) = self.pending.take() else {
            return Ok(());
        };
        if !(self.seen.facts && self.seen.events && self.seen.psi) {
            return Err("a call is missing its facts, events or psi line".into());
        }
        self.current
            .as_mut()
            .ok_or_else(|| "a call outside a case".to_owned())?
            .steps
            .push(Step { call, expect });
        Ok(())
    }

    /// The lines that open, describe and close a case.
    fn case_line(&mut self, keyword: &str, rest: &str) -> Result<(), String> {
        match keyword {
            "version" => {
                if self.saw_version {
                    return Err("the corpus declares its version twice".into());
                }
                if rest != FORMAT_VERSION {
                    return Err(format!("unsupported corpus format version {rest}"));
                }
                self.saw_version = true;
            }
            "case" => {
                if !self.saw_version {
                    return Err("a case before the format version".into());
                }
                if self.current.is_some() {
                    return Err("a case inside a case".into());
                }
                if rest.is_empty() {
                    return Err("a case with no name".into());
                }
                self.current = Some(Case {
                    name: rest.to_owned(),
                    note: None,
                    initial_target: 0,
                    steps: Vec::new(),
                });
            }
            "desc" => {
                let case = self.current.as_mut().ok_or("desc outside a case")?;
                if !case.steps.is_empty() {
                    return Err("desc after the first call".into());
                }
            }
            "note" => {
                let case = self.current.as_mut().ok_or("note outside a case")?;
                if case.note.is_some() {
                    return Err("a case with two notes".into());
                }
                case.note = Some(rest.to_owned());
            }
            "init" => {
                let values = take_fields(rest, &["target"])?;
                let target = parse_num(&values[0])?;
                self.current
                    .as_mut()
                    .ok_or("init outside a case")?
                    .initial_target = target;
            }
            "end" => {
                let case = self.current.take().ok_or("end outside a case")?;
                if !rest.is_empty() {
                    return Err(format!("trailing text after end: {rest}"));
                }
                if case.steps.is_empty() {
                    return Err(format!("case {} has no calls", case.name));
                }
                self.cases.push(case);
            }
            other => return Err(format!("unknown keyword {other}")),
        }
        Ok(())
    }

    /// A line that starts a call.
    fn call_line(&mut self, keyword: &str, rest: &str) -> Result<(), String> {
        if self.current.is_none() {
            return Err("a call outside a case".into());
        }
        let call = if keyword == "chunk" {
            Call::Chunk(parse_hex(rest)?)
        } else {
            Call::Target(parse_num(rest)?)
        };
        self.pending = Some((call, Expect::default()));
        self.seen = StepLines::default();
        Ok(())
    }

    /// A line that says what the call in progress has to produce.
    fn expectation_line(&mut self, keyword: &str, rest: &str) -> Result<(), String> {
        let (seen_facts, seen_events) = (self.seen.facts, self.seen.events);
        let (_, expect) = self
            .pending
            .as_mut()
            .ok_or_else(|| format!("{keyword} with no call"))?;
        match keyword {
            "facts" => {
                if seen_facts {
                    return Err("two facts lines for one call".into());
                }
                self.seen.facts = true;
                apply_facts(expect, rest)?;
            }
            "track" => {
                if !seen_facts || seen_events {
                    return Err("a track line outside the facts it belongs to".into());
                }
                apply_track(expect, rest)?;
            }
            "events" => {
                if !seen_facts {
                    return Err("events before facts".into());
                }
                if seen_events {
                    return Err("two events lines for one call".into());
                }
                self.seen.events = true;
                expect.events = rest.split_whitespace().map(str::to_owned).collect();
            }
            "psi" => {
                if !seen_events {
                    return Err("psi before events".into());
                }
                if self.seen.psi {
                    return Err("two psi lines for one call".into());
                }
                self.seen.psi = true;
                apply_psi(expect, rest)?;
            }
            other => return Err(format!("unknown keyword {other}")),
        }
        Ok(())
    }

    /// Everything that has to be true once the last line has been read.
    fn finish(self) -> Result<Vec<Case>, String> {
        if self.current.is_some() {
            return Err("the corpus ended inside a case".into());
        }
        if self.pending.is_some() {
            return Err("the corpus ended inside a call".into());
        }
        if !self.saw_version {
            return Err("the corpus never declared its format version".into());
        }
        Ok(self.cases)
    }
}

/// Reads the whole corpus, or says exactly where it stopped making sense.
fn parse_corpus(text: &str) -> Result<Vec<Case>, String> {
    let mut reader = Reader::default();
    for (line_no, raw) in text.lines().enumerate() {
        let line = raw.trim();
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        let (keyword, rest) = line.split_once(' ').unwrap_or((line, ""));
        let rest = rest.trim();
        let at = |e: String| format!("line {}: {e}", line_no + 1);

        if matches!(keyword, "chunk" | "target" | "end") {
            reader.close_step().map_err(&at)?;
        }
        match keyword {
            "chunk" | "target" => reader.call_line(keyword, rest).map_err(&at)?,
            "facts" | "track" | "events" | "psi" => {
                reader.expectation_line(keyword, rest).map_err(&at)?;
            }
            _ => reader.case_line(keyword, rest).map_err(&at)?,
        }
    }
    reader.finish()
}

fn render_track(
    pid: u16,
    stream_type: u8,
    codec: &str,
    language: &str,
    d: ChannelDeclaration,
) -> String {
    format!(
        "pid={pid} streamType={stream_type} codec={codec} lang={language} channels={} multichannel={} componentType={} hasComponentType={}",
        d.channels,
        u8::from(d.multichannel),
        d.component_type,
        u8::from(d.has_component_type)
    )
}

fn render_expected_track(t: &ExpectTrack) -> String {
    format!(
        "pid={} streamType={} codec={} lang={} channels={} multichannel={} componentType={} hasComponentType={}",
        t.pid,
        t.stream_type,
        t.codec,
        t.language,
        t.channels,
        u8::from(t.multichannel),
        t.component_type,
        u8::from(t.has_component_type)
    )
}

/// Writes a table's sections the way the corpus does, so a disagreement is read
/// against the file rather than against a description of it.
fn render_sections(sections: &[Vec<u8>]) -> String {
    sections
        .iter()
        .map(|section| {
            let mut out = String::with_capacity(section.len() * 2);
            for byte in section {
                write!(out, "{byte:02x}").expect("writing to a String cannot fail");
            }
            out
        })
        .collect::<Vec<_>>()
        .join(",")
}

/// Everything one call has to get right, compared exactly.
///
/// Nothing is sorted or normalised before comparison: where order is part of the
/// contract - the audio lists follow the table, events follow the stream,
/// sections follow `section_number` - sorting would hide precisely the
/// disagreements worth finding.
fn compare(outcome: &super::Outcome, expect: &Expect) -> Vec<String> {
    let mut bad = Vec::new();

    let got_facts = format!(
        "through={} hasPAT={} hasPMT={} pmtVersion={} programNumber={} pmtPID={} videoPID={} videoCodec={} audioPIDs={}",
        outcome.processed_through,
        u8::from(outcome.facts.has_pat),
        u8::from(outcome.facts.has_pmt),
        outcome.facts.pmt_version,
        outcome.facts.program_number,
        outcome.facts.pmt_pid,
        outcome.facts.video_pid,
        outcome.facts.video_codec.as_str(),
        join_pids(&outcome.facts.audio_pids)
    );
    let want_facts = format!(
        "through={} hasPAT={} hasPMT={} pmtVersion={} programNumber={} pmtPID={} videoPID={} videoCodec={} audioPIDs={}",
        expect.through,
        u8::from(expect.has_pat),
        u8::from(expect.has_pmt),
        expect.pmt_version,
        expect.program_number,
        expect.pmt_pid,
        expect.video_pid,
        expect.video_codec,
        join_pids(&expect.audio_pids)
    );
    if got_facts != want_facts {
        bad.push(format!(
            "  facts got  {got_facts}\n  facts want {want_facts}"
        ));
    }

    if outcome.facts.audio_tracks.len() == expect.tracks.len() {
        for (n, track) in outcome.facts.audio_tracks.iter().enumerate() {
            let got = render_track(
                track.pid,
                track.stream_type,
                &track.codec,
                &track.language,
                track.declared,
            );
            let want = render_expected_track(&expect.tracks[n]);
            if got != want {
                bad.push(format!("  track {n} got  {got}\n  track {n} want {want}"));
            }
        }
    } else {
        bad.push(format!(
            "  tracks got {}, want {}",
            outcome.facts.audio_tracks.len(),
            expect.tracks.len()
        ));
    }

    let got_events: Vec<&str> = outcome
        .events
        .iter()
        .map(|e| match e {
            PsiEvent::ProgramIdentityChanged => "identity",
        })
        .collect();
    if got_events.join(" ") != expect.events.join(" ") {
        bad.push(format!(
            "  events got  [{}]\n  events want [{}]",
            got_events.join(" "),
            expect.events.join(" ")
        ));
    }

    let got_psi = format!(
        "pat={} pmt={}",
        render_sections(&outcome.active.pat_sections),
        render_sections(&outcome.active.pmt_sections)
    );
    let want_psi = format!(
        "pat={} pmt={}",
        render_sections(&expect.pat_sections),
        render_sections(&expect.pmt_sections)
    );
    if got_psi != want_psi {
        bad.push(format!("  psi got  {got_psi}\n  psi want {want_psi}"));
    }

    bad
}

fn join_pids(pids: &[u16]) -> String {
    pids.iter()
        .map(u16::to_string)
        .collect::<Vec<_>>()
        .join(",")
}

#[test]
fn the_rust_core_answers_the_shared_corpus() {
    let path = corpus_path();
    let text =
        std::fs::read_to_string(&path).unwrap_or_else(|e| panic!("read {}: {e}", path.display()));
    let cases = parse_corpus(&text).unwrap_or_else(|e| panic!("parse corpus: {e}"));

    assert!(
        cases.len() >= MINIMUM_CASES,
        "the corpus reader found {} cases and the corpus had at least {MINIMUM_CASES}; \
         a reader that finds fewer would pass while checking less",
        cases.len()
    );

    let mut calls = 0usize;
    let mut failures: Vec<String> = Vec::new();

    for case in &cases {
        let mut core = PsiCore::new(case.initial_target);
        let mut offset = 0i64;

        for (index, step) in case.steps.iter().enumerate() {
            calls += 1;
            let outcome = match &step.call {
                Call::Chunk(bytes) => {
                    let result = core
                        .ingest(offset, bytes)
                        .unwrap_or_else(|e| panic!("{}: step {}: {e}", case.name, index + 1));
                    offset += i64::try_from(bytes.len()).expect("a chunk fits an i64");
                    result
                }
                Call::Target(program) => core.set_target_program(*program),
            };

            let bad = compare(&outcome, &step.expect);
            if !bad.is_empty() {
                failures.push(format!(
                    "{} step {} of {}:\n{}",
                    case.name,
                    index + 1,
                    case.steps.len(),
                    bad.join("\n")
                ));
            }
        }
    }

    assert!(
        failures.is_empty(),
        "{} of {} calls across {} cases disagree:\n{}",
        failures.len(),
        calls,
        cases.len(),
        failures.join("\n\n")
    );
    println!("corpus: {} cases, {calls} calls, all exact", cases.len());
}

#[test]
fn the_corpus_carries_no_unadjudicated_semantics() {
    let text = std::fs::read_to_string(corpus_path()).expect("read corpus");
    let cases = parse_corpus(&text).expect("parse corpus");
    let noted: Vec<&str> = cases
        .iter()
        .filter(|c| c.note.is_some())
        .map(|c| c.name.as_str())
        .collect();
    assert!(
        noted.is_empty(),
        "a note marks behaviour nobody has ruled on, and a second implementation must not be \
         written to reproduce it. Still noted: {noted:?}"
    );
}

#[test]
fn the_corpus_reader_refuses_what_it_cannot_read_exactly() {
    let good = "version 2\n\
                case a\n  desc d\n  init target=1\n  chunk 47\n  facts through=1 hasPAT=0 hasPMT=0 \
                pmtVersion=0 programNumber=0 pmtPID=0 videoPID=0 videoCodec=unknown audioPIDs=\n  \
                events\n  psi pat= pmt=\nend\n";
    assert!(parse_corpus(good).is_ok(), "the control case must parse");

    for (what, text) in [
        ("unknown keyword", good.replace("  desc d", "  colour blue")),
        ("unknown field", good.replace("hasPAT=0", "hasPAT=0 hasSDT=1")),
        ("duplicate field", good.replace("hasPAT=0", "hasPAT=0 hasPAT=1")),
        ("missing field", good.replace(" hasPMT=0", "")),
        ("bad hex", good.replace("chunk 47", "chunk 4z")),
        ("odd hex", good.replace("chunk 47", "chunk 475")),
        ("field without a value", good.replace("hasPAT=0", "hasPAT")),
        ("two facts lines", good.replace("  events\n", "  facts through=1 hasPAT=0 hasPMT=0 pmtVersion=0 programNumber=0 pmtPID=0 videoPID=0 videoCodec=unknown audioPIDs=\n  events\n")),
        ("a version this reader does not know", good.replace("version 2", "version 3")),
        ("the version this reader replaced", good.replace("version 2", "version 1")),
        ("no version", good.replace("version 2\n", "")),
        ("case inside a case", good.replace("  desc d", "case b")),
        ("call with no expectation", good.replace("  facts through=1 hasPAT=0 hasPMT=0 pmtVersion=0 programNumber=0 pmtPID=0 videoPID=0 videoCodec=unknown audioPIDs=\n", "")),
        ("trailing text after end", good.replace("end\n", "end now\n")),
        ("unterminated case", good.replace("end\n", "")),
        ("not a number", good.replace("init target=1", "init target=x")),
        ("bad boolean", good.replace("hasPAT=0", "hasPAT=2")),
        ("psi before events", good.replace("  events\n  psi pat= pmt=\n", "  psi pat= pmt=\n  events\n")),
        ("a psi section in odd-length hex", good.replace("psi pat= pmt=", "psi pat=00b00 pmt=")),
        ("a psi section that is not hex", good.replace("psi pat= pmt=", "psi pat=00b0zz pmt=")),
        ("a psi section too short to be one", good.replace("psi pat= pmt=", "psi pat=00b0 pmt=")),
        (
            "a psi section longer than a section may be",
            good.replace("psi pat= pmt=", &format!("psi pat={} pmt=", "00".repeat(super::table::MAX_SECTION_BYTES + 1))),
        ),
    ] {
        assert!(
            parse_corpus(&text).is_err(),
            "the reader accepted a corpus with {what}, so a damaged corpus could check less than it claims"
        );
    }
}
