#!/usr/bin/env bash
# Copyright (c) 2026 ManuGH
# Licensed under the PolyForm Noncommercial License 1.0.0
#
# Mutation test for the transport packet reader.
#
# The authored corpus says what each packet means. This asks the only question
# that matters about such a corpus: would it notice if the reader were wrong.
#
# The mistakes worth mutating here are all one character long - a mask, a shift,
# an offset by one - and they are exactly the mistakes that still parse a
# well-formed stream most of the time. A corpus that cannot tell payload-starts-
# one-byte-late from correct is a corpus that will pass while the stream is
# quietly misread.
#
# A mutation is a kill only when the corpus failed. It is NOT a kill when the
# mutation did not apply or did not compile: both look like a passing gate from
# a distance, and each is reported on its own line instead of being folded in.
#
# Postcondition: the source is restored and no build output is left whose source
# identity is unclear. The next consumer builds.
#
# Usage: media-core/scripts/mutate-transport.sh   (from the repository root)

set -uo pipefail

cd "$(dirname "$0")/../.." || exit 1

SRC=media-core/src/transport/mod.rs
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

# run_corpus builds and runs the transport corpus, reporting each stage apart.
#
# Compiling is asked first and on its own. A failing test run also prints lines
# beginning with "error:" - "error: test failed, to rerun pass..." - so deciding
# between the two by grepping one combined output reports every kill as a build
# failure. Which is precisely the confusion this harness exists to avoid, and it
# got in here first time round.
#
# Returns: 0 corpus passed, 1 corpus failed, 2 build failed.
run_corpus() {
  local out
  if ! out=$(cd media-core && cargo build --tests 2>&1); then
    LAST_DETAIL=$(grep -m2 -E '^error' <<<"$out" | tr '\n' ' ')
    return 2
  fi
  if ! out=$(cd media-core && cargo test --lib transport 2>&1); then
    LAST_DETAIL=$(grep -m2 -E '^(test .*FAILED|---- .* stdout|assertion)' <<<"$out" | tr '\n' ' ')
    return 1
  fi
  return 0
}

echo "Mutating the transport reader; the authored corpus must go red for each."
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
  if ! grep -qF -- "$from" "$SRC"; then
    log "not applied" "$what"
    notapplied=$((notapplied + 1))
    return
  fi
  python3 - "$SRC" "$from" "$to" <<'PY'
import sys
path, old, new = sys.argv[1], sys.argv[2], sys.argv[3]
s = open(path).read()
open(path, "w").write(s.replace(old, new, 1))
PY
  attempted=$((attempted + 1))
  # Capture the status before anything else runs: after a case statement $? is
  # the status of the case, not of the thing being asked about.
  local rc=0
  run_corpus || rc=$?
  case "$rc" in
    0) log survived "$what"; survived=$((survived + 1)) ;;
    1) log killed "$what"; killed=$((killed + 1)) ;;
    2) log "not built" "$what"; notbuilt=$((notbuilt + 1)) ;;
  esac
}

# --- the header fields ------------------------------------------------------

mutate "read the PID with the flag bits left in" \
  '(u16::from(self.packet[1] & 0x1F) << 8) | u16::from(self.packet[2])' \
  '(u16::from(self.packet[1] & 0x3F) << 8) | u16::from(self.packet[2])'

mutate "shift the PID one bit too far" \
  '(u16::from(self.packet[1] & 0x1F) << 8) | u16::from(self.packet[2])' \
  '(u16::from(self.packet[1] & 0x1F) << 9) | u16::from(self.packet[2])'

mutate "read the payload-start flag from the error bit" \
  'self.packet[1] & 0x40 != 0' \
  'self.packet[1] & 0x80 != 0'

mutate "read the error indicator from the payload-start bit" \
  'self.packet[1] & 0x80 != 0' \
  'self.packet[1] & 0x40 != 0'

mutate "take scrambling control from the adaptation bits" \
  '(self.packet[3] >> 6) & 0x03' \
  '(self.packet[3] >> 4) & 0x03'

mutate "take adaptation control from the scrambling bits" \
  '(self.packet[3] >> 4) & 0x03' \
  '(self.packet[3] >> 6) & 0x03'

mutate "let the counter keep the bits above it" \
  'self.packet[3] & 0x0F' \
  'self.packet[3] & 0x1F'

# --- the boundaries ---------------------------------------------------------

mutate "start the payload one byte early" \
  'let payload_at = start + declared;' \
  'let payload_at = start + declared - 1;'

mutate "start the payload one byte late" \
  'let payload_at = start + declared;' \
  'let payload_at = start + declared + 1;'

mutate "forget the adaptation length byte itself" \
  'let start = HEADER_LEN + 1;
                let payload_at = start + declared;' \
  'let start = HEADER_LEN;
                let payload_at = start + declared;'

mutate "accept an adaptation field that runs past the packet" \
  'if payload_at > TS_PACKET_LEN {' \
  'if payload_at > TS_PACKET_LEN + 1 {'

mutate "treat a packet-filling adaptation field as carrying payload" \
  'let payload = if payload_at == TS_PACKET_LEN {
                    None
                } else {
                    Some(payload_at)
                };' \
  'let payload = Some(payload_at.min(TS_PACKET_LEN - 1));'

mutate "call the reserved adaptation control a payload packet" \
  '0b00 => return Err(TransportError::ReservedAdaptationControl),' \
  '0b00 => (None, Some(HEADER_LEN)),'

mutate "let an adaptation-only packet claim a payload" \
  '(Some((start, start + declared)), None)' \
  '(Some((start, start + declared)), Some(HEADER_LEN + 1))'

mutate "stop checking the sync byte" \
  'if packet[0] != SYNC_BYTE {' \
  'if false {'

mutate "read the discontinuity flag from the wrong bit" \
  'flags & 0x80 != 0' \
  'flags & 0x40 != 0'

# --- continuity -------------------------------------------------------------

mutate "expect the counter to repeat rather than advance" \
  'cc != (self.last_cc.wrapping_add(1)) & 0x0F' \
  'cc != self.last_cc'

mutate "call the wrap from fifteen to zero a break" \
  '(self.last_cc.wrapping_add(1)) & 0x0F' \
  '(self.last_cc + 1)'

mutate "call every repeat a duplicate without comparing the bytes" \
  'if packet == self.last_packet.as_slice() {
                return Continuity::Duplicate;
            }' \
  'return Continuity::Duplicate;'

mutate "call a repeated packet continuous instead of a duplicate" \
  'return Continuity::Duplicate;' \
  'return Continuity::Continuous;'

mutate "stop recording the packet after a break" \
  'self.last_cc = cc;
        self.has_cc = true;' \
  'self.has_cc = true;'

mutate "ignore the low-four-bit mask on the incoming counter" \
  'let cc = continuity_counter & 0x0F;' \
  'let cc = continuity_counter;'

echo
echo "killed:      $killed"
echo "survived:    $survived"
echo "not built:   $notbuilt"
echo "not applied: $notapplied"
echo
echo "Only 'killed' counts. A mutation that did not apply or did not compile was"
echo "never tested, and is a gap in this harness rather than evidence about the code."
echo
echo "The build output has been removed; build again before using the core."

# A harness that killed nothing must not report success. The first version of
# this script read the status of a case statement instead of the run beneath it
# and counted zero kills while every line said killed - and the gate below, as
# it was then written, passed anyway.
if [ "$killed" -ne "$attempted" ]; then
  echo
  echo "killed $killed of $attempted mutations; the counts must agree." >&2
  exit 1
fi
if [ "$attempted" -eq 0 ]; then
  echo
  echo "no mutations were attempted." >&2
  exit 1
fi
if [ "$survived" -ne 0 ] || [ "$notbuilt" -ne 0 ] || [ "$notapplied" -ne 0 ]; then
  exit 1
fi
