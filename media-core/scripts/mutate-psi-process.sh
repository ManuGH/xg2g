#!/usr/bin/env bash
# Copyright (c) 2026 ManuGH
# Licensed under the PolyForm Noncommercial License 1.0.0
#
# Mutation test for the Go/Rust PSI differential across the real process
# boundary.
#
# The differential starts a real media core, speaks protocol v3 to it over a
# Unix socket, and compares every call against both the Go reference and the
# authored corpus. This asks the only question that matters about such a test:
# does it depend on what the Rust core actually said?
#
# A mutation is only a kill when the differential disagreed about semantics. It
# is NOT a kill when the mutation failed to apply, failed to compile, or the
# process failed to start - each of those looks like a passing gate from a
# distance and is reported on its own line here rather than being folded in.
# That distinction is the whole point: a harness that counted a build failure as
# a kill would report a healthy differential for a core that was never run.
#
# Runs on Linux, where RemoteCore has the process identity it requires. On a Mac
# the differential skips, and a skip is not evidence - run this on a Linux host.
#
# Usage: media-core/scripts/mutate-psi-process.sh
# Run from the repository root. Restores the mutated file on exit.

set -uo pipefail

cd "$(dirname "$0")/../.." || exit 1   # repository root

IPC=media-core/src/ipc.rs
PKG=./internal/stream/ingest/remotecore
DIFF_TEST=TestPSIDifferential_TheRealRustCoreAgreesCallByCall
BIN=media-core/target/release/xg2g-media-core

BACKUP=$(mktemp -d)
cp "$IPC" "$BACKUP/ipc.rs"

# Restoring the source is only half of leaving this tree in a state someone can
# trust. Every mutation builds, and the last one to build wins the file at
# $BIN - so a run that restored ipc.rs and stopped there left a clean source
# beside a binary compiled from a mutant. Source and binary would disagree
# about what the core does, silently, and the next reader of that binary would
# be testing a mutation while believing they were testing the core.
#
# That is the same "which code did we actually test" question 5c exists to
# answer, so the postcondition is made unambiguous instead: the mutated binary
# is deleted, and the next consumer has to build. Deleting is deliberate rather
# than rebuilding here - a cleanup rebuild would have to succeed to mean
# anything, and a harness that ends by hiding a failed build is the problem
# again in a new place.
restore() {
  cp "$BACKUP/ipc.rs" "$IPC"
  rm -rf "$BACKUP"
  rm -f "$BIN"
}
trap restore EXIT

killed=0
survived=0
anchor=0
notbuilt=0
notlaunched=0

log() { printf '  %-10s %s\n' "$1" "$2"; }

# build_and_run applies nothing; it builds the current tree and runs the
# differential, reporting each stage separately.
#
# Returns: 0 test passed, 1 test failed, 2 build failed, 3 launch failed.
build_and_run() {
  local out
  rm -f "$BIN"
  if ! out=$(cd media-core && cargo build --release --locked 2>&1); then
    LAST_DETAIL=$(grep -m3 '^error' <<<"$out" | tr '\n' ' ')
    return 2
  fi
  if [ ! -x "$BIN" ]; then
    LAST_DETAIL="cargo reported success but produced no binary"
    return 2
  fi
  # The binary has to be newer than the source it claims to be built from.
  #
  # Deleting it is not enough: cargo publishes the artifact as a hard link to a
  # file under deps/, so a build it considers fresh re-creates the link with the
  # OLD timestamp and the previous mutation's code. That is a stale binary that
  # looks like a successful build, and it would make every mutation below report
  # whatever the last one did. Checked rather than assumed, because a harness
  # that silently tests the wrong binary is the failure this whole file exists
  # to rule out.
  if [ "$IPC" -nt "$BIN" ]; then
    LAST_DETAIL="the binary is older than $IPC; cargo did not rebuild it"
    return 2
  fi
  # The core exits immediately when it has no socket to speak on; that it runs
  # at all is what is being checked, because a binary that cannot start would
  # make every differential below skip rather than fail.
  if ! out=$("./$BIN" 2>&1) && [ -n "$out" ]; then
    LAST_DETAIL="launch: $out"
    return 3
  fi
  if ! out=$(cd backend && XG2G_MEDIA_CORE_BIN="$PWD/../$BIN" \
      go test "$PKG" -run "$DIFF_TEST" -count=1 -timeout 600s 2>&1); then
    # Prefer the line that says what the two disagreed about. Falling back to
    # the FAIL header would report that something went red without saying that
    # it went red for a reason about PSI, which is the only reason that counts.
    LAST_DETAIL=$(grep -m2 -E '^ +(facts|psi|events|track) (go|rust|want)' <<<"$out" | tr '\n' ' ')
    if [ -z "$LAST_DETAIL" ]; then
      LAST_DETAIL="NO SEMANTIC DIFF REPORTED: $(grep -m2 -E '\.go:[0-9]+:|--- FAIL' <<<"$out" | tr '\n' ' ')"
    fi
    return 1
  fi
  # A differential that skipped is not a differential that passed.
  if grep -q 'no test files\|SKIP' <<<"$out"; then
    LAST_DETAIL="the differential skipped: $(grep -m1 SKIP <<<"$out")"
    return 3
  fi
  return 0
}

# mutate <name> <literal-from> <literal-to> [expected-occurrences]
mutate() {
  local name=$1 from=$2 to=$3 want=${4:-1}
  cp "$BACKUP/ipc.rs" "$IPC"

  if ! FROM="$from" TO="$to" WANT="$want" python3 - "$IPC" <<'PY'
import os, sys
path = sys.argv[1]
src = open(path).read()
frm, to, want = os.environ["FROM"], os.environ["TO"], int(os.environ["WANT"])
if src.count(frm) != want:
    sys.exit(f"anchor appears {src.count(frm)} times, need exactly {want}")
open(path, "w").write(src.replace(frm, to))
PY
  then
    log "ANCHOR" "$name"
    log "" "the source moved; the mutation was never applied"
    anchor=$((anchor + 1))
    cp "$BACKUP/ipc.rs" "$IPC"
    return
  fi

  LAST_DETAIL=""
  build_and_run
  local rc=$?
  cp "$BACKUP/ipc.rs" "$IPC"

  case $rc in
    0)
      log "SURVIVED" "$name"
      log "" "the differential passed against a core that was lying"
      survived=$((survived + 1))
      ;;
    1)
      log "killed" "$name"
      log "" "${LAST_DETAIL:0:150}"
      killed=$((killed + 1))
      ;;
    2)
      log "NOT BUILT" "$name"
      log "" "${LAST_DETAIL:0:150}"
      notbuilt=$((notbuilt + 1))
      ;;
    3)
      log "NOT RUN" "$name"
      log "" "${LAST_DETAIL:0:150}"
      notlaunched=$((notlaunched + 1))
      ;;
  esac
}

echo "Mutating the Rust core; the real-process differential must go red for each."
echo

# The baseline. If this is not green, nothing below means anything.
LAST_DETAIL=""
build_and_run
baseline=$?
if [ $baseline -ne 0 ]; then
  echo "  BASELINE FAILED (rc=$baseline): ${LAST_DETAIL:0:200}"
  echo "  Nothing was mutated. A harness that cannot pass unmutated proves nothing."
  exit 1
fi
echo "  baseline   the unmutated core passes the differential"
echo

mutate "answer a successful result with no PSI in it" \
  '                Outcome::Answer(encode_psi_result(&outcome))
            }
            MSG_OBSERVE_AUDIO_BATCH' \
  '                let mut blank = outcome;
                blank.events.clear();
                blank.facts = PsiFacts::default();
                blank.active = ActivePsi::default();
                Outcome::Answer(encode_psi_result(&blank))
            }
            MSG_OBSERVE_AUDIO_BATCH'

mutate "ignore the programme the handshake named" \
  'self.psi = Some(PsiCore::new(target));' \
  'let _ = target;
                self.psi = Some(PsiCore::new(0));'

mutate "drop the PMT sections from the answer" \
  'body.extend_from_slice(&count16(table.len()).to_be_bytes());
        for section in table {' \
  'let table = if std::ptr::eq(table, &active.pmt_sections) { &Vec::new() } else { table };
        body.extend_from_slice(&count16(table.len()).to_be_bytes());
        for section in table {'

mutate "hold the PAT sections in the reverse of the order the table numbers them" \
  '    for table in [&active.pat_sections, &active.pmt_sections] {' \
  '    let reversed: Vec<Vec<u8>> = active.pat_sections.iter().rev().cloned().collect();
    for table in [&reversed, &active.pmt_sections] {'

# Well formed on purpose: the count and the body must agree, or the frame is
# malformed and the decoder kills it before the differential ever compares
# anything. A mutation caught by the wire is not evidence that the differential
# reads events.
mutate "say nothing happened when the programme identity changed" \
  'fn encode_events(body: &mut Vec<u8>, events: &[PsiEvent]) {' \
  'fn encode_events(body: &mut Vec<u8>, events: &[PsiEvent]) {
    let events: &[PsiEvent] = &[];'

mutate "report one byte more than was interpreted" \
  'body.extend_from_slice(&(outcome.processed_through as u64).to_be_bytes());' \
  'body.extend_from_slice(&((outcome.processed_through as u64) + 1).to_be_bytes());'

echo
echo "killed:      $killed"
echo "survived:    $survived"
echo "anchor:      $anchor"
echo "not built:   $notbuilt"
echo "not run:     $notlaunched"
echo
echo "Only 'killed' counts. A mutation that did not build or did not run was not"
echo "tested, and is a gap in this harness rather than evidence about the code."
echo
echo "The mutated $BIN has been removed. Build again before using the core;"
echo "a binary left behind by this script would be the last mutant, not the core."
if [ "$survived" -ne 0 ] || [ "$anchor" -ne 0 ] || [ "$notbuilt" -ne 0 ] || [ "$notlaunched" -ne 0 ]; then
  exit 1
fi
