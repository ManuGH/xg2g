#!/usr/bin/env bash
# Copyright (c) 2026 ManuGH
# Licensed under the PolyForm Noncommercial License 1.0.0
#
# Mutation test for the PES boundary reader.
#
# Everything this reader gets wrong, it gets wrong by one: nine against eight,
# byte eight against byte seven, "<=" against "<". Those all parse real
# broadcast for hours and then hand a consumer a header byte, so the corpus has
# to be able to tell them apart and this is how that is checked.
#
# Anchors must match EXACTLY ONCE. An anchor that appears twice would mutate
# whichever occurrence came first and leave the other doing the right thing, and
# a mutation that only half-applies is a mutation whose survival means nothing.
# An anchor that has drifted out of the source entirely is a gap, not a pass.
#
# Compiling is asked before running, on its own, because a failing `cargo test`
# also prints lines beginning with "error:" and grepping one combined output
# reports every kill as a build failure.
#
# Postcondition: source restored, no build output left whose source identity is
# unclear. The next consumer builds.
#
# Usage: media-core/scripts/mutate-pes.sh   (from the repository root)

set -uo pipefail

cd "$(dirname "$0")/../.." || exit 1

SRC=media-core/src/pes/mod.rs
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
attempted=0

log() { printf '  %-12s %s\n' "$1" "$2"; }

# Returns: 0 corpus passed, 1 corpus failed, 2 build failed.
run_corpus() {
  local out
  if ! out=$(cd media-core && cargo build --tests 2>&1); then
    LAST_DETAIL=$(grep -m2 -E '^error' <<<"$out" | tr '\n' ' ')
    return 2
  fi
  if ! out=$(cd media-core && cargo test --lib pes 2>&1); then
    LAST_DETAIL=$(grep -m2 -E '^(test .*FAILED|---- .* stdout|assertion)' <<<"$out" | tr '\n' ' ')
    return 1
  fi
  return 0
}

echo "Mutating the PES boundary reader; the authored corpus must go red for each."
echo

if ! run_corpus; then
  echo "  baseline    the unmutated reader does not pass its own corpus"
  echo "              ${LAST_DETAIL:-}"
  exit 1
fi
log baseline "the unmutated reader passes the corpus"

mutate() {
  local what="$1" from="$2" to="$3"
  cp "$BACKUP/mod.rs" "$SRC"

  # The count and the edit are one step, and its exit status is checked.
  #
  # Splitting them meant grep counted matching *lines* while python counted
  # occurrences, so the two guards could disagree about the same anchor. Worse,
  # the edit's status went unread: a python that failed for any reason - a
  # tripped assertion, an unusable interpreter, an I/O error - left the source
  # pristine, and the corpus then passed against unmutated code and the mutation
  # was logged as a survivor. A harness reporting a corpus gap it had invented.
  if ! python3 - "$SRC" "$from" "$to" <<'PY'
import sys
path, old, new = sys.argv[1], sys.argv[2], sys.argv[3]
s = open(path).read()
n = s.count(old)
if n != 1:
    sys.stderr.write(f"anchor matched {n} times, want exactly 1\n")
    sys.exit(1)
open(path, "w").write(s.replace(old, new, 1))
PY
  then
    log "not applied" "$what"
    notapplied=$((notapplied + 1))
    return
  fi

  attempted=$((attempted + 1))
  local rc=0
  run_corpus || rc=$?
  case "$rc" in
    0) log survived "$what"; survived=$((survived + 1)) ;;
    1) log killed "$what"; killed=$((killed + 1)) ;;
    2) log "not built" "$what"; notbuilt=$((notbuilt + 1)) ;;
  esac
}

# --- the start code ---------------------------------------------------------

mutate "change the last byte of the start code" \
  'const START_CODE: [u8; 3] = [0x00, 0x00, 0x01];' \
  'const START_CODE: [u8; 3] = [0x00, 0x00, 0x02];'

mutate "change the first byte of the start code" \
  'const START_CODE: [u8; 3] = [0x00, 0x00, 0x01];' \
  'const START_CODE: [u8; 3] = [0x01, 0x00, 0x01];'

mutate "compare only two bytes of the start code" \
  'if payload[..START_CODE.len()] != START_CODE {' \
  'if payload[..2] != START_CODE[..2] {'

# --- the fixed header -------------------------------------------------------

mutate "need one byte fewer than the fixed header" \
  'const FIXED_HEADER_LEN: usize = 9;' \
  'const FIXED_HEADER_LEN: usize = 8;'

mutate "need one byte more than the fixed header" \
  'const FIXED_HEADER_LEN: usize = 9;' \
  'const FIXED_HEADER_LEN: usize = 10;'

mutate "read the optional length from the byte before it" \
  'const HEADER_DATA_LENGTH_AT: usize = 8;' \
  'const HEADER_DATA_LENGTH_AT: usize = 7;'

mutate "read the optional length from the byte after it" \
  'const HEADER_DATA_LENGTH_AT: usize = 8;' \
  'const HEADER_DATA_LENGTH_AT: usize = 9;'

# --- where the elementary stream begins -------------------------------------

mutate "start the elementary stream one byte early" \
  'let es_start = FIXED_HEADER_LEN + usize::from(header_data_length);' \
  'let es_start = FIXED_HEADER_LEN + usize::from(header_data_length) - 1;'

mutate "start the elementary stream one byte late" \
  'let es_start = FIXED_HEADER_LEN + usize::from(header_data_length);' \
  'let es_start = FIXED_HEADER_LEN + usize::from(header_data_length) + 1;'

mutate "forget the optional header entirely" \
  'let es_start = FIXED_HEADER_LEN + usize::from(header_data_length);' \
  'let es_start = FIXED_HEADER_LEN;'

mutate "call a header ending exactly at the end incomplete" \
  'if es_start <= payload.len() {' \
  'if es_start < payload.len() {'

mutate "call a header reaching one byte past the end complete" \
  'if es_start <= payload.len() {' \
  'if es_start <= payload.len() + 1 {'

mutate "misreport how much header is still to come" \
  'remaining_header: es_start - payload.len(),' \
  'remaining_header: es_start - payload.len() + 1,'

# --- what the caller is told -------------------------------------------------

mutate "read the packet length from the wrong bytes" \
  'let packet_length = u16::from_be_bytes([payload[4], payload[5]]);' \
  'let packet_length = u16::from_be_bytes([payload[5], payload[6]]);'

mutate "read the packet length in the wrong order" \
  'u16::from_be_bytes([payload[4], payload[5]])' \
  'u16::from_le_bytes([payload[4], payload[5]])'

mutate "read the stream id from the byte before it" \
  'let stream_id = payload[3];' \
  'let stream_id = payload[2];'

# --- transport-level preconditions -------------------------------------------

mutate "read a start out of a continuation packet" \
  'if !view.payload_unit_start() || view.scrambling_control() != 0 {' \
  'if view.scrambling_control() != 0 {'

mutate "scan a scrambled payload for a start code" \
  'if !view.payload_unit_start() || view.scrambling_control() != 0 {' \
  'if !view.payload_unit_start() {'

# --- which ids carry an optional header --------------------------------------

mutate "give every stream id an optional header" \
  '    !matches!(
        stream_id,
        0xBC | 0xBE | 0xBF | 0xF0 | 0xF1 | 0xF2 | 0xF8 | 0xFF
    )' \
  '    let _ = stream_id;
    true'

mutate "give padding streams an optional header" \
  '0xBC | 0xBE | 0xBF | 0xF0 | 0xF1 | 0xF2 | 0xF8 | 0xFF' \
  '0xBC | 0xBF | 0xF0 | 0xF1 | 0xF2 | 0xF8 | 0xFF'

mutate "start a header-less packet's data one byte late" \
  'data: &payload[MINIMUM_HEADER_LEN..],' \
  'data: &payload[MINIMUM_HEADER_LEN + 1..],'

mutate "need the full fixed header before reading the stream id" \
  'const MINIMUM_HEADER_LEN: usize = 6;' \
  'const MINIMUM_HEADER_LEN: usize = 9;'

# --- the role predicates -----------------------------------------------------

mutate "widen the video stream ids by one below" \
  '(0xE0..=0xEF).contains(&stream_id)' \
  '(0xDF..=0xEF).contains(&stream_id)'

mutate "narrow the video stream ids by one above" \
  '(0xE0..=0xEF).contains(&stream_id)' \
  '(0xE0..=0xEE).contains(&stream_id)'

mutate "let video ids count as audio" \
  'stream_id == 0xBD || stream_id == 0xFD || (0xC0..=0xDF).contains(&stream_id)' \
  'stream_id == 0xBD || stream_id == 0xFD || (0xC0..=0xEF).contains(&stream_id)'

mutate "drop private_stream_1 from the audio ids" \
  'stream_id == 0xBD || stream_id == 0xFD || (0xC0..=0xDF).contains(&stream_id)' \
  'stream_id == 0xFD || (0xC0..=0xDF).contains(&stream_id)'

echo
echo "killed:      $killed"
echo "survived:    $survived"
echo "not built:   $notbuilt"
echo "not applied: $notapplied"
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
if [ "$survived" -ne 0 ] || [ "$notbuilt" -ne 0 ] || [ "$notapplied" -ne 0 ]; then
  exit 1
fi
