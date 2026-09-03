#!/usr/bin/env bash
# Copyright (c) 2026 ManuGH
# Licensed under the PolyForm Noncommercial License 1.0.0
#
# Mutation test for the PSI corpus.
#
# A corpus that passes only because it was written against the implementation
# proves nothing. This applies one deliberate defect at a time to the Go PSI
# reference and requires the corpus test to go red for it. A mutation that
# SURVIVES is a semantics the corpus does not actually pin, and is a finding.
#
# Usage: backend/scripts/mutate-psi-corpus.sh
# Run from the repository root. Restores every mutated file on exit.

set -uo pipefail

cd "$(dirname "$0")/.." || exit 1   # backend/

PKG=./internal/stream/ingest/mediafacts
CORE=internal/stream/ingest/mediafacts/core.go
PARSE=internal/stream/ingest/mediafacts/parse.go
PSI=internal/stream/ingest/mediafacts/psi.go

BACKUP=$(mktemp -d)
cp "$CORE" "$BACKUP/core.go"
cp "$PARSE" "$BACKUP/parse.go"
cp "$PSI" "$BACKUP/psi.go"
restore() {
  cp "$BACKUP/core.go" "$CORE"
  cp "$BACKUP/parse.go" "$PARSE"
  cp "$BACKUP/psi.go" "$PSI"
  rm -rf "$BACKUP"
}
trap restore EXIT

survived=0
killed=0
broken=0
equivalent=0

# mutate <name> <file> <literal-from> <literal-to> [expected-occurrences]
#
# The occurrence count is part of the mutation: an anchor that stopped matching,
# or started matching more places than intended, is reported rather than silently
# applied somewhere else.
mutate() {
  local name=$1 file=$2 from=$3 to=$4 want=${5:-1}
  restore_one() { cp "$BACKUP/$(basename "$file")" "$file"; }
  restore_one

  if ! FROM="$from" TO="$to" WANT="$want" python3 - "$file" <<'PY'
import os, sys
path = sys.argv[1]
src = open(path).read()
frm, to, want = os.environ["FROM"], os.environ["TO"], int(os.environ["WANT"])
if src.count(frm) != want:
    sys.exit(f"anchor appears {src.count(frm)} times, need exactly {want}")
open(path, "w").write(src.replace(frm, to))
PY
  then
    printf '  ANCHOR  %-58s (source changed; mutation not applied)\n' "$name"
    broken=$((broken + 1))
    restore_one
    return
  fi

  local out
  out=$(go test "$PKG" -run 'TestPSICorpus_TheGoCoreMeetsTheAuthoredExpectations' 2>&1)
  local rc=$?
  restore_one

  if [ $rc -eq 0 ]; then
    if [ -n "${EQUIVALENT_REASON:-}" ]; then
      printf '  equivalent %s\n' "$name"
      printf '             %s\n' "$EQUIVALENT_REASON"
      equivalent=$((equivalent + 1))
    else
      printf '  SURVIVED %-58s <-- the corpus does not pin this\n' "$name"
      survived=$((survived + 1))
    fi
  elif grep -q 'build failed\|cannot use\|undefined:' <<<"$out"; then
    printf '  ANCHOR  %-58s (mutation did not compile)\n' "$name"
    broken=$((broken + 1))
  else
    printf '  killed   %s\n' "$name"
    killed=$((killed + 1))
  fi
  unset EQUIVALENT_REASON
}

echo "Mutating the Go PSI reference; every mutation must make the corpus red."
echo

mutate "accept a section whose CRC is wrong" "$CORE" \
  'if CalculateMPEG2CRC32(table) != 0 {' 'if false {'

mutate "accept a section that is not yet current" "$CORE" \
  'if currentNext != 1 {' 'if currentNext != 1 && false {'

mutate "activate a table with a section still missing" "$PSI" \
  'if _, ok := t.sections[i]; !ok {
				return false
			}' 'if _, ok := t.sections[i]; !ok {
				_ = ok
			}'

mutate "keep the in-flight sections across a version change" "$PSI" \
  'if !t.hasInFlight || t.inFlightVersion != version || t.lastSectionNum != lastSectionNum {' \
  'if !t.hasInFlight {'

mutate "follow any program, not the target" "$CORE" \
  'if progNum == c.targetProgramNumber {' 'if true {'

mutate "follow program number 0 (the NIT)" "$CORE" \
  'if progNum == 0 {' 'if false {'

mutate "miss a PMT version change" "$CORE" \
  'isChanged := !c.hasPMTVersion || version != c.pmtVersion || progNum != c.pmtProgramNumber' \
  'isChanged := !c.hasPMTVersion'

mutate "miss a PMT program number change" "$CORE" \
  'isChanged := !c.hasPMTVersion || version != c.pmtVersion || progNum != c.pmtProgramNumber' \
  'isChanged := !c.hasPMTVersion || version != c.pmtVersion'

mutate "do not follow the PMT PID when the PAT moves it" "$CORE" \
  'if c.pmtPID != matchedPID {' 'if false {'

mutate "let the last video stream win instead of the first" "$CORE" \
  'if c.videoPID == 0 {' 'if true {' 3

mutate "treat a duplicate packet as new data" "$CORE" \
  'if bytes.Equal(pkt, assembler.lastPacket) {
				// Exact byte-for-byte duplicate TS packet: silently ignore
				return
			}' \
  'if false {
				// Exact byte-for-byte duplicate TS packet: silently ignore
				return
			}'

mutate "ignore a continuity counter gap" "$CORE" \
  '} else if cc != (assembler.lastCC+1)&0x0F {' '} else if false {'

mutate "ignore the pointer field and start at the payload" "$CORE" \
  'offset = 1 + pointerField' 'offset = 1'

mutate "accept any table_id where the header is scanned" "$CORE" \
  'if tableID != expectedTableID {' 'if tableID != expectedTableID && false {'

EQUIVALENT_REASON="the PAT and PMT scans each re-check len(sData) < 12 per section, so for every section the packet scanner can deliver the early guard changes nothing observable"
mutate "accept a section too short to hold its fields" "$CORE" \
  'if len(table) < 12 {' 'if len(table) < 3 {'

mutate "report a chunk as partly interpreted" "$CORE" \
  'return c.result(startOffset + int64(len(data))), nil' \
  'return c.result(startOffset), nil'

mutate "say nothing when the program identity changes" "$CORE" \
  'c.events = append(c.events, Event{Kind: EventProgramIdentityChanged})
}
func (c *GoCore) parseVideoPacketLocked' \
  '_ = 0
}
func (c *GoCore) parseVideoPacketLocked'

mutate "keep the old streams across a program change" "$CORE" \
  'c.audioPIDs = nil
	c.audioTracks = nil' '_ = 0'

mutate "treat every private stream as audio" "$PARSE" \
  'case 0x06:
		return hasAudioDescriptor(descriptors)' \
  'case 0x06:
		return true'

mutate "count a multichannel declaration as six channels" "$PARSE" \
  'd.Multichannel = true' 'd.Channels = 6' 2

mutate "report no language instead of und" "$PARSE" \
  'return "und"' 'return ""'

mutate "let a PID of 0x1FFF become an audio stream" "$PARSE" \
  'if pid == 0 || pid == 0x1FFF {' 'if false {'

mutate "read the AC-3 component type without its presence flag" "$PARSE" \
  'if len(body) < 2 || body[0]&ac3ComponentTypeFlagBit == 0 {' 'if len(body) < 2 {'

mutate "stop the descriptor walk at the first unknown tag" "$PARSE" \
  'switch tag {
		case descriptorAC3, descriptorEnhAC3, descriptorDTS, descriptorAAC, descriptorDTSHD:
			return true' \
  'switch tag {
		default:
			return false
		case descriptorAC3, descriptorEnhAC3, descriptorDTS, descriptorAAC, descriptorDTSHD:
			return true'

echo
echo "killed:     $killed"
echo "equivalent: $equivalent"
echo "survived:   $survived"
echo "anchor:     $broken"
if [ "$survived" -ne 0 ] || [ "$broken" -ne 0 ]; then
  exit 1
fi
