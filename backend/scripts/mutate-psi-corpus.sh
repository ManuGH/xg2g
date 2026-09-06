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
RINGPKG=./internal/stream/ingest/ring
CORE=internal/stream/ingest/mediafacts/core.go
PARSE=internal/stream/ingest/mediafacts/parse.go
PSI=internal/stream/ingest/mediafacts/psi.go
# The ring turns the sections the core keeps back into transport packets. It is
# mutated here rather than in a harness of its own because it is the other half
# of one contract: the core may only stop retaining the sender's packets if what
# replaces them delivers the same tables.
RING=internal/stream/ingest/ring/psipreamble.go

BACKUP=$(mktemp -d)
cp "$CORE" "$BACKUP/core.go"
cp "$PARSE" "$BACKUP/parse.go"
cp "$PSI" "$BACKUP/psi.go"
cp "$RING" "$BACKUP/psipreamble.go"
restore() {
  cp "$BACKUP/core.go" "$CORE"
  cp "$BACKUP/parse.go" "$PARSE"
  cp "$BACKUP/psi.go" "$PSI"
  cp "$BACKUP/psipreamble.go" "$RING"
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
    # Same cleanup as the end of the function. Without it a reason set for THIS
    # mutation outlives the early return and the next surviving mutation is
    # reported as equivalent under a justification that was never about it.
    unset EQUIVALENT_REASON
    return
  fi

  local out rc
  # The corpus and the contract tests both pin invariants a mutation can break.
  # Running only the corpus would let a mutation survive because the test that
  # would have caught it was never asked - which is how the assembler's bounded
  # state went unpinned until it was mutated.
  #
  # The ring's preamble tests run for every mutation too, not only for the ones
  # that touch the ring: a core that quietly dropped a section would still have
  # to get past the replay.
  out=$(go test "$PKG" -run 'TestPSICorpus_TheGoCoreMeetsTheAuthoredExpectations|TestPSI_' 2>&1)
  rc=$?
  if [ $rc -eq 0 ]; then
    out=$(go test "$RINGPKG" -run 'TestPreamble_|TestPacketizePSISections|TestMasterRing_MultiPacketPATPMT_Assembly|TestMasterRing_MultiSectionPAT_TargetOnlyInSection1|TestMasterRing_MultipleSectionsSamePacket' 2>&1)
    rc=$?
  fi
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

mutate "collect a section that numbers itself beyond its own table" "$CORE" \
  'if sectionNum > lastSectionNum {
		return
	}' 'if false {
		return
	}'

mutate "resume the scan inside a section whose length was refused" "$CORE" \
  'return consumed + len(chunk)' 'return consumed'

mutate "let a PAT declare less than its own syntax costs" "$PSI" \
  '	minPATSectionLength = 9' '	minPATSectionLength = 0'

mutate "give a PMT the shorter floor a PAT has" "$PSI" \
  '	minPMTSectionLength = 13' '	minPMTSectionLength = 9'

mutate "keep the section in flight after refusing its declaration" "$CORE" \
  '				assembler.buf = assembler.buf[:0]
				assembler.sectionLen = 0
				// Everything handed in is given up' \
  '				_ = assembler
				// Everything handed in is given up'

# There is no longer a mutation that makes the assembler keep the packets a
# refused section arrived in, because there is no longer a field to keep them
# in. That is the point of R8 and not a gap in this harness: the retention is
# gone structurally rather than guarded by a test. What is still mutable - the
# section bytes themselves surviving a refusal - is the mutation above and the
# one at the end of this file, and both are killed.

mutate "let a PAT or PMT declare the whole twelve-bit length" "$PSI" \
  'length > maxPSISectionLength {' 'length > 0x0FFF {'

mutate "stop requiring the long section syntax" "$PSI" \
  'if prefix[1]&0x80 == 0 || prefix[1]&0x40 != 0 {' 'if prefix[1]&0x40 != 0 {'

mutate "stop requiring the fixed zero bit" "$PSI" \
  'if prefix[1]&0x80 == 0 || prefix[1]&0x40 != 0 {' 'if prefix[1]&0x80 == 0 {'

EQUIVALENT_REASON="since a section numbering itself beyond its own table is refused before the tracker, every stored section number is inside 0..last_section_number - so a count of last+1 already implies each slot is filled, and the per-slot loop cannot be the check that fails. It is kept as the guard that would still hold if the numbering check above it were ever removed"
mutate "activate a table with a section still missing" "$PSI" \
  'if _, ok := t.sections[uint8(n)]; !ok {
				return false
			}' 'if _, ok := t.sections[uint8(n)]; !ok {
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

EQUIVALENT_REASON="since the program-number acceptance check, progNum always equals selectedProgramNumber here, and every change to selectedProgramNumber goes through forgetPMTIdentityLocked and so clears hasPMTVersion - the first term always fires first. The condition is kept as the guard that would still hold if the acceptance check above it were ever removed"
mutate "miss a PMT program number change" "$CORE" \
  'isChanged := !c.hasPMTVersion || version != c.pmtVersion || progNum != c.pmtProgramNumber' \
  'isChanged := !c.hasPMTVersion || version != c.pmtVersion'

mutate "accept a PMT whichever program it names" "$CORE" \
  'if progNum != c.selectedProgramNumber {
			return
		}' 'if false {
			return
		}'

mutate "let a foreign PMT reach the tracker before it is refused" "$CORE" \
  'if progNum != c.selectedProgramNumber {
			return
		}

		tableComplete := c.pmtTracker.addSection(version, sectionNum, lastSectionNum, table)' \
  'tableComplete := c.pmtTracker.addSection(version, sectionNum, lastSectionNum, table)
		if progNum != c.selectedProgramNumber {
			return
		}'

mutate "do not record which program an unnamed target selected" "$CORE" \
  '					matchedPID, matchedProgram = progPID, progNum
					break
				}' '					matchedPID = progPID
					break
				}'

mutate "treat the selection as the PMT PID alone" "$CORE" \
  'if c.pmtPID != matchedPID || c.selectedProgramNumber != matchedProgram {' \
  'if c.pmtPID != matchedPID {'

mutate "keep the program number and version behind a false HasPMT" "$CORE" \
  'c.hasPMTVersion = false
	c.pmtVersion = 0
	c.pmtProgramNumber = 0' 'c.hasPMTVersion = false'

mutate "let a later PAT section re-choose an auto-selected program" "$CORE" \
  '			if matchedPID > 0 {
				break
			}' '			if matchedPID > 0 && c.targetProgramNumber > 0 {
				break
			}'

mutate "let the last video stream win instead of the first" "$CORE" \
  'if c.videoPID == 0 {' 'if true {' 3

mutate "treat a duplicate packet as new data" "$CORE" \
  'if bytes.Equal(pkt, assembler.lastPacket) {
				// Exact byte-for-byte duplicate TS packet: silently ignore
				return
			}' \
  'if bytes.Equal(pkt, assembler.lastPacket) && false {
				// Exact byte-for-byte duplicate TS packet: silently ignore
				return
			}'

mutate "ignore a continuity counter gap" "$CORE" \
  '} else if cc != (assembler.lastCC+1)&0x0F {' '} else if false {'

mutate "ignore the pointer field and start at the payload" "$CORE" \
  'offset = 1 + pointerField' 'offset = 1'

mutate "do not stop the scan at a table this PID is not read for" "$CORE" \
  'if payload[offset] != expectedTableIDFor(isPAT) {' \
  'if payload[offset] != expectedTableIDFor(isPAT) && false {'

mutate "interpret a completed section without checking which table it is" "$CORE" \
  'if table[0] != expectedTableIDFor(isPAT) {' 'if table[0] != expectedTableIDFor(isPAT) && false {'

mutate "bound an ES entry by the section instead of the elementary stream loop" "$CORE" \
  'if i+5+esInfoLen > esEnd {' 'if i+5+esInfoLen > len(sData) {'

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

mutate "call the ATSC enhanced stream type plain AC-3 again" "$PARSE" \
  '		return "eac3"
	case 0x06:' '		return "ac3"
	case 0x06:'

# Findings 4's three shapes: no guard at all, and each of the two lists keeping a
# guard the other lost. The third is expressible only because the mutation moves
# the append outside the accepted branch - in the source both lists are built
# inside one decision, which is what stops them drifting.
mutate "accept an audio stream on a PID that cannot carry one" "$CORE" \
  'if isAudioStreamType(st, descriptors) && canCarryElementaryStream(elemPID) {' \
  'if isAudioStreamType(st, descriptors) {'

mutate "guard the PID list but not the track list" "$CORE" \
  'if isAudioStreamType(st, descriptors) && canCarryElementaryStream(elemPID) {
							c.audioPIDs = appendPID(c.audioPIDs, elemPID)' \
  'if isAudioStreamType(st, descriptors) {
							if canCarryElementaryStream(elemPID) {
								c.audioPIDs = appendPID(c.audioPIDs, elemPID)
							}'

mutate "guard the track list but not the PID list" "$CORE" \
  'if isAudioStreamType(st, descriptors) && canCarryElementaryStream(elemPID) {
							c.audioPIDs = appendPID(c.audioPIDs, elemPID)' \
  'if isAudioStreamType(st, descriptors) {
							c.audioPIDs = appendPID(c.audioPIDs, elemPID)
						}
						if isAudioStreamType(st, descriptors) && canCarryElementaryStream(elemPID) {'

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

# --- R8: the tables in force are sections, not the packets that carried them --

mutate "keep the bytes of a section that was refused for its length" "$CORE" \
  '				assembler.buf = assembler.buf[:0]
				assembler.sectionLen = 0
				// Everything handed in is given up, not only the header bytes.' \
  '				assembler.sectionLen = 0
				// Everything handed in is given up, not only the header bytes.'

mutate "publish the generation being assembled as the table in force" "$CORE" \
  'PATSections: cloneSliceList(c.activePATSections),' \
  'PATSections: cloneSliceList(c.patTracker.generation(c.patTracker.lastSectionNum)),'

mutate "drop the last section when copying a completed generation" "$PSI" \
  '	out := make([][]byte, 0, int(lastSectionNum)+1)
	for n := 0; n <= int(lastSectionNum); n++ {' \
  '	out := make([][]byte, 0, int(lastSectionNum)+1)
	for n := 0; n < int(lastSectionNum); n++ {'

mutate "hold a table's sections in the reverse of the order it numbers them" "$PSI" \
  '	for n := 0; n <= int(lastSectionNum); n++ {
		// #nosec G115 -- n is bounded by lastSectionNum, itself a uint8
		out = append(out, cloneSlice(t.sections[uint8(n)]))' \
  '	for n := int(lastSectionNum); n >= 0; n-- {
		// #nosec G115 -- n is bounded by lastSectionNum, itself a uint8
		out = append(out, cloneSlice(t.sections[uint8(n)]))'

mutate "count a table's sections in a counter its own numbering can overflow" "$PSI" \
  '		for n := 0; n <= int(lastSectionNum); n++ {
			// #nosec G115 -- n is bounded by lastSectionNum, itself a uint8
			if _, ok := t.sections[uint8(n)]; !ok {' \
  '		for n := uint8(0); n <= lastSectionNum; n++ {
			// #nosec G115 -- n is bounded by lastSectionNum, itself a uint8
			if _, ok := t.sections[n]; !ok {'

mutate "start a section without the pointer field that says where it begins" "$RING" \
  '				body[0] = 0x00
				body = body[1:]' \
  '				body[0] = 0x00'

mutate "mark every packet as starting a section" "$RING" \
  '				pusi = false' '				_ = pusi'

mutate "leave the continuity counter where it was" "$RING" \
  '			cc = (cc + 1) & 0x0F' '			cc = cc + 0'

mutate "pad a section's last packet with zero bytes instead of stuffing" "$RING" \
  '				body[i] = 0xFF' '				body[i] = 0x00'

mutate "emit a section too long to be one" "$RING" \
  'if len(section) < psiSectionHeaderBytes || len(section) > mediafacts.MaxSectionBytes {' \
  'if len(section) < psiSectionHeaderBytes {'

echo
echo "killed:     $killed"
echo "equivalent: $equivalent"
echo "survived:   $survived"
echo "anchor:     $broken"
if [ "$survived" -ne 0 ] || [ "$broken" -ne 0 ]; then
  exit 1
fi
