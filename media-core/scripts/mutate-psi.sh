#!/usr/bin/env bash
# Copyright (c) 2026 ManuGH
# Licensed under the PolyForm Noncommercial License 1.0.0
#
# Mutation test for the Rust PSI parser.
#
# The corpus and the adversarial tests pass. That is worth exactly as much as the
# proof that they would stop passing if the parser were wrong. This applies one
# deliberate defect at a time and requires the suite to go red for it. A mutation
# that SURVIVES is a semantics nothing pins, and is a finding.
#
# Usage: media-core/scripts/mutate-psi.sh   (from anywhere)

set -uo pipefail
cd "$(dirname "$0")/.." || exit 1   # media-core/

SRC=src/psi
BACKUP=$(mktemp -d)
cp -R "$SRC" "$BACKUP/psi"
restore() { rm -rf "$SRC"; cp -R "$BACKUP/psi" "$SRC"; }
cleanup() { restore; rm -rf "$BACKUP"; }
trap cleanup EXIT

killed=0
survived=0
broken=0

# mutate <name> <file> <from> <to>
mutate() {
  local name=$1 file=$2 from=$3 to=$4
  restore

  if ! FROM="$from" TO="$to" python3 - "$SRC/$file" <<'PY'
import os, sys
path = sys.argv[1]
src = open(path).read()
frm, to = os.environ["FROM"], os.environ["TO"]
if src.count(frm) != 1:
    sys.exit(f"anchor appears {src.count(frm)} times, need exactly 1")
open(path, "w").write(src.replace(frm, to))
PY
  then
    printf '  ANCHOR   %-56s (source changed; mutation not applied)\n' "$name"
    broken=$((broken + 1))
    return
  fi

  local out
  out=$(cargo test --lib psi:: 2>&1)
  local rc=$?

  if [ $rc -eq 0 ]; then
    printf '  SURVIVED %-56s <-- nothing pins this\n' "$name"
    survived=$((survived + 1))
  elif grep -qE 'could not compile|^error\[E[0-9]+\]:' <<<"$out"; then
    printf '  ANCHOR   %-56s (mutation did not compile)\n' "$name"
    broken=$((broken + 1))
  else
    printf '  killed   %-56s (%s)\n' "$name" \
      "$(grep -oE '^test psi::[a-z_:]+ \.\.\. FAILED' <<<"$out" | head -1 | sed 's/^test //; s/ \.\.\. FAILED//')"
    killed=$((killed + 1))
  fi
}

echo "Mutating the Rust PSI parser; every mutation must make the suite red."
echo

mutate "stop checking that a section's bytes are intact" table.rs \
  'if crc::mpeg2(section) != 0 {' 'if false {'

mutate "accept a completed section of any table" table.rs \
  'if section[0] != expected_table_id {' 'if false {'

mutate "act on a table announced for later" table.rs \
  'if section[5] & 0x01 != 1 {' 'if false {'

mutate "call a table complete before all its sections are here" table.rs \
  '        let wanted = usize::from(last_section_number) + 1;
        if generation.sections.len() != wanted {
            return false;
        }
        (0..=last_section_number).all(|n| generation.sections.contains_key(&n))' \
  '        true'

mutate "accept a PMT whichever programme it names" mod.rs \
  'if header.id_extension != self.selected_program_number() {
            return;
        }' 'if false {
            return;
        }'

mutate "let a foreign PMT reach the tracker before it is refused" mod.rs \
  '        if header.id_extension != self.selected_program_number() {
            return;
        }
        if !self.pmt_tracker.add(
            header.version,
            header.section_number,
            header.last_section_number,
            section,
            packets,
        ) {
            return;
        }' \
  '        let complete = self.pmt_tracker.add(
            header.version,
            header.section_number,
            header.last_section_number,
            section,
            packets,
        );
        if header.id_extension != self.selected_program_number() {
            return;
        }
        if !complete {
            return;
        }'

mutate "let the last section of a PAT re-choose the programme" pat.rs \
  'for section in sections {' 'for section in sections.iter().rev() {'

mutate "accept an audio stream on a PID that cannot carry one" pmt.rs \
  'if declares_audio(stream_type, block) && can_carry_elementary_stream(pid) {' \
  'if declares_audio(stream_type, block) {'

mutate "bound an elementary stream entry by the section, not the loop" pmt.rs \
  'if entry_end > end {' 'if entry_end > section.len() {'

mutate "stop the descriptor walk at the first tag it does not know" descriptors.rs \
  '            tag::REGISTRATION => {' '            _ => return false,
            tag::REGISTRATION => {'

echo
echo "killed:   $killed"
echo "survived: $survived"
echo "anchor:   $broken"
if [ "$survived" -ne 0 ] || [ "$broken" -ne 0 ]; then
  exit 1
fi
