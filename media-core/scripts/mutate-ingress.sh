#!/usr/bin/env bash
# Copyright (c) 2026 ManuGH
# Licensed under the PolyForm Noncommercial License 1.0.0
#
# Mutation test for the audio ingress.
#
# The layers below this one are already mutated by harnesses of their own. What
# nothing else can test is the composition: which observer a packet's bytes go
# to, which of them are elementary stream, and what survives a table that
# changed. Every mutation below is something that parses real broadcast for
# hours and then describes the wrong audio - a PID routed to its neighbour, an
# observer kept across a programme change, one byte of PES header handed to a
# frame parser.
#
# Anchors must match EXACTLY ONCE. An anchor that appears twice would mutate
# whichever occurrence came first and leave the other doing the right thing, and
# a mutation that only half-applies is a mutation whose survival means nothing.
# An anchor that has drifted out of the source entirely is a gap, not a pass.
#
# And every mutation must produce a program no other one produced. Exactly-once
# anchoring is not enough for that: an anchor written with less indentation
# than its line still matches that line exactly once, and two differently named
# mutations then test the same change twice. Both are killed, the count says
# two, and the site one of them was meant for is never mutated at all. So each
# mutated source is hashed, and a repeat is refused before it is run.
#
# Compiling is asked before running, on its own, because a failing `cargo test`
# also prints lines beginning with "error:" and grepping one combined output
# reports every kill as a build failure.
#
# Postcondition: source restored, no build output left whose source identity is
# unclear. The next consumer builds.
#
# Usage: media-core/scripts/mutate-ingress.sh   (from the repository root)

set -uo pipefail

cd "$(dirname "$0")/../.." || exit 1

SRC=media-core/src/ingress/mod.rs
BIN=media-core/target/release/xg2g-media-core

BACKUP=$(mktemp -d)
cp "$SRC" "$BACKUP/mod.rs"
restore() {
  cp "$BACKUP/mod.rs" "$SRC"
  rm -rf "$BACKUP"
  rm -f "$BIN"
}
trap restore EXIT

killed=0
survived=0
notbuilt=0
notapplied=0
duplicates=0
attempted=0
DIGESTS=" "

log() { printf '  %-12s %s\n' "$1" "$2"; }

# Returns: 0 corpus passed, 1 corpus failed, 2 build failed.
run_corpus() {
  local out
  if ! out=$(cd media-core && cargo build --tests 2>&1); then
    LAST_DETAIL=$(grep -m2 -E '^error' <<<"$out" | tr '\n' ' ')
    return 2
  fi
  if ! out=$(cd media-core && cargo test --lib ingress 2>&1); then
    LAST_DETAIL=$(grep -m2 -E '^(test .*FAILED|---- .* stdout|assertion)' <<<"$out" | tr '\n' ' ')
    return 1
  fi
  return 0
}

echo "Mutating the audio ingress; the shared corpus must go red for each."
echo

if ! run_corpus; then
  echo "  baseline    the unmutated ingress does not pass its own corpus"
  echo "              ${LAST_DETAIL:-}"
  exit 1
fi
log baseline "the unmutated ingress passes the corpus"

mutate() {
  local what="$1" from="$2" to="$3"
  cp "$BACKUP/mod.rs" "$SRC"

  # The count and the edit are one step, and its exit status is checked. An
  # edit whose failure went unread would leave the source pristine, the corpus
  # would pass against unmutated code, and the mutation would be logged as a
  # survivor - a harness reporting a gap it had invented.
  local digest
  if ! digest=$(python3 - "$SRC" "$from" "$to" <<'PY'
import sys, hashlib
path, old, new = sys.argv[1], sys.argv[2], sys.argv[3]
s = open(path).read()
n = s.count(old)
if n != 1:
    sys.stderr.write(f"anchor matched {n} times, want exactly 1\n")
    sys.exit(1)
out = s.replace(old, new, 1)
open(path, "w").write(out)
print(hashlib.sha256(out.encode()).hexdigest())
PY
  ); then
    log "not applied" "$what"
    notapplied=$((notapplied + 1))
    return
  fi

  case "$DIGESTS" in
    *" $digest "*)
      log duplicate "$what -- the same program another mutation already produced"
      duplicates=$((duplicates + 1))
      return
      ;;
  esac
  DIGESTS="$DIGESTS$digest "

  attempted=$((attempted + 1))
  local rc=0
  run_corpus || rc=$?
  case "$rc" in
    0) log survived "$what"; survived=$((survived + 1)) ;;
    1) log killed "$what"; killed=$((killed + 1)) ;;
    # The reason is printed rather than swallowed. A build that failed once and
    # not again is the kind of thing nobody can diagnose afterwards from a
    # count, and it is exactly the kind of thing that could be hiding a
    # mutation that was never tested.
    2) log "not built" "$what -- ${LAST_DETAIL:-no detail}"; notbuilt=$((notbuilt + 1)) ;;
  esac
}

# --- which stream a packet belongs to ---------------------------------------

mutate "route a packet to the stream beside it" \
  'let Some(index) = self.followers.iter().position(|f| f.pid == pid) else {' \
  'let Some(index) = self.followers.iter().position(|f| f.pid != pid) else {'

mutate "route every packet to the first stream" \
  'let Some(index) = self.followers.iter().position(|f| f.pid == pid) else {' \
  'let Some(index) = (if self.followers.is_empty() { None } else { Some(0) }) else {'

mutate "follow a PID one number off" \
  '            if observable(&track.codec) {
                self.followers.push(Follower::new(track));' \
  '            if observable(&track.codec) {
                let mut t = track.clone();
                t.pid += 1;
                self.followers.push(Follower::new(&t));'

mutate "route the table's own PID as audio" \
  'if pid == self.psi.pmt_pid() || pid == self.psi.video_pid() {' \
  'if pid == self.psi.video_pid() {'

mutate "route the video PID as audio" \
  'if pid == self.psi.pmt_pid() || pid == self.psi.video_pid() {' \
  'if pid == self.psi.pmt_pid() {'

# --- which codecs are read --------------------------------------------------

mutate "observe every declared audio codec" \
  'codec == "ac3" || codec == "eac3"' \
  '!codec.is_empty()'

mutate "stop observing enhanced AC-3" \
  'codec == "ac3" || codec == "eac3"' \
  'codec == "ac3"'

mutate "stop observing AC-3" \
  'codec == "ac3" || codec == "eac3"' \
  'codec == "eac3"'

# --- what a programme change ends -------------------------------------------

mutate "keep the observers across a programme change" \
  '        self.incarnation += 1;
        self.followers.clear();' \
  '        self.incarnation += 1;
        if self.followers.is_empty() {
            self.followers.clear();
        }'

mutate "never turn the incarnation" \
  '        self.incarnation += 1;
        self.followers.clear();' \
  '        self.followers.clear();'

mutate "act on a programme change at the end of the chunk" \
  '            if changed {
                self.reprogram();
            }' \
  '            if false && changed {
                self.reprogram();
            }'

mutate "let a target change leave the streams standing" \
  '        if outcome.events.contains(&PsiEvent::ProgramIdentityChanged) {
            self.reprogram();
        }' \
  '        let _ = outcome;'

# --- where the elementary stream begins -------------------------------------

mutate "drop the first byte of every elementary stream run" \
  '                self.pes_starts += 1;
                self.position = Position::InElementaryStream;
                Some(es)' \
  '                self.pes_starts += 1;
                self.position = Position::InElementaryStream;
                Some(if es.is_empty() { es } else { &es[1..] })'

mutate "trim one byte off the end of every run" \
  '        follower.observer.feed(es);' \
  '        let es = &es[..es.len() - 1];
        follower.observer.feed(es);'

mutate "feed a payload unit start as though it continued one" \
  '        let es = if view.payload_unit_start() {
            follower.start(payload)
        } else {
            follower.cont(payload, continuity)
        };' \
  '        let es = follower.cont(payload, continuity);'

mutate "feed a continuation as though it started a PES packet" \
  '        let es = if view.payload_unit_start() {
            follower.start(payload)
        } else {
            follower.cont(payload, continuity)
        };' \
  '        let es = follower.start(payload);'

mutate "feed an empty run as a feed of its own" \
  '        let Some(es) = es.filter(|es| !es.is_empty()) else {
            return;
        };' \
  '        let Some(es) = es else {
            return;
        };'

# --- the header that reaches past its packet --------------------------------

mutate "treat an incomplete header as a complete one" \
  '                self.position = Position::InHeader {
                    remaining: remaining_header,
                };
                None' \
  '                self.position = Position::InElementaryStream;
                None'

mutate "step over one byte too few of a header remainder" \
  '                        self.position = Position::InElementaryStream;
                        Some(&payload[remaining..])' \
  '                        self.position = Position::InElementaryStream;
                        Some(&payload[remaining - 1..])'

mutate "step over one byte too many of a header remainder" \
  '                        self.position = Position::InElementaryStream;
                        Some(&payload[remaining..])' \
  '                        self.position = Position::InElementaryStream;
                        Some(&payload[remaining + 1..])'

mutate "carry the header state past the next payload unit start" \
  '            _ => {
                self.position = Position::InElementaryStream;
                None
            }' \
  '            _ => None,'

mutate "skip a header remainder across a continuity break" \
  '                Continuity::Broken => {
                    self.position = Position::AwaitingStart;
                    None
                }' \
  '                Continuity::Broken => {
                    self.position = Position::InElementaryStream;
                    Some(payload)
                }'

mutate "step over a header remainder twice on a repeated packet" \
  '                Continuity::Duplicate => None,' \
  '                Continuity::Duplicate => {
                    self.position = Position::InElementaryStream;
                    Some(&payload[remaining..])
                }'

mutate "resume feeding while still waiting for a start" \
  'Position::AwaitingStart => None,' \
  'Position::AwaitingStart => Some(payload),'

# --- scrambling --------------------------------------------------------------

mutate "feed a scrambled payload" \
  '        if view.scrambling_control() != 0 {
            follower.scrambled_packets += 1;' \
  '        if false && view.scrambling_control() != 0 {
            follower.scrambled_packets += 1;'

mutate "let a scrambled packet complete a header" \
  '            if matches!(follower.position, Position::InHeader { .. }) {
                follower.position = Position::AwaitingStart;
            }' \
  '            if matches!(follower.position, Position::InHeader { .. }) {
                follower.position = Position::InElementaryStream;
            }'

# --- what is fed to whom -----------------------------------------------------

mutate "report the feed under the incarnation it ended in" \
  '        let incarnation = self.incarnation;
        let follower = &mut self.followers[index];' \
  '        let follower = &mut self.followers[index];
        let incarnation = 0;'

mutate "feed a packet with no payload" \
  '        let Some(payload) = view.payload() else {
            return;
        };' \
  '        let payload = view.payload().unwrap_or(view.bytes());'

echo
echo "killed:      $killed"
echo "survived:    $survived"
echo "not built:   $notbuilt"
echo "not applied: $notapplied"
echo "duplicate:   $duplicates"
echo
echo "Only 'killed' counts. A mutation that did not apply uniquely, or did not"
echo "compile, was never tested - a gap in this harness rather than evidence"
echo "about the code."
echo
echo "The build output has been removed; build again before using the core."

if [ "$attempted" -eq 0 ]; then
  echo; echo "no mutations were attempted." >&2; exit 1
fi
if [ "$killed" -ne "$attempted" ]; then
  echo; echo "killed $killed of $attempted attempted mutations; the counts must agree." >&2; exit 1
fi
if [ "$survived" -ne 0 ] || [ "$notbuilt" -ne 0 ] || [ "$notapplied" -ne 0 ] || [ "$duplicates" -ne 0 ]; then
  exit 1
fi
