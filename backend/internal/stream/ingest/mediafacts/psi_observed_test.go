// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package mediafacts

import (
	"context"
	"testing"
)

// Observed behaviour of the Go PSI reference that has NOT been adjudicated.
//
// These are deliberately not in the shared corpus. The corpus is what a second
// implementation is held to, and holding one to a behaviour nobody has decided
// is right means the second implementation gets written to reproduce a defect
// on purpose - which is the specific failure this whole migration is shaped to
// avoid.
//
// So each test here pins what the reference does today, states the question it
// raises, and says what would have to happen before it becomes corpus material.
// A red test here means the behaviour changed, which is either the fix landing
// or a regression; either way it needs a person, not a re-record.
//
// Every one of these was found by reading the parser during the step 5a
// inventory and then proved here. None of them is a hypothesis.
//
// Two more lived here and no longer do. The ES_info_length that reached into the
// section CRC, and the table_id that a split section header slipped past, were
// adjudicated as defects and repaired; their expectations were re-authored from
// the fixed contract and moved into the shared corpus, where a second
// implementation is held to them. What is left here is still unadjudicated.

// --- 1. auto-select over a multi-section PAT ------------------------------

// With no target program the core takes "the first program in the PAT". Over a
// PAT that spans several sections it does not: the inner loop breaks out of the
// section, but the outer loop only stops early when a target was named, so each
// later section overwrites the choice and the last one wins.
//
//	for sIdx := 0; sIdx <= lastSectionNum; sIdx++ {
//	        ... matchedPID = progPID; break            // first program of THIS section
//	        if matchedPID > 0 && c.targetProgramNumber > 0 { break }   // never taken when target == 0
//	}
//
// Question: should an unspecified target follow the first program of the table,
// or the first program of the last section? The first is what the code says it
// does. Nothing in xg2g calls the core with target 0 in production - the session
// layer always names a service - so this has no live consequence today.
func TestObserved_AutoSelectOverAMultiSectionPATTakesTheLastSection(t *testing.T) {
	sec0 := patSection(psiTSID, 0, 0, 1, 1, patProgram{3, psiPMTPID3})
	sec1 := patSection(psiTSID, 0, 1, 1, 1, patProgram{5, psiPMTPID2})

	c := NewGoCore(0)
	res, err := c.Ingest(context.Background(), 0,
		chunkOf(flatten(psiPackets(0, 0, 0, sec0), psiPackets(0, 1, 0, sec1))...))
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	if got, want := res.Facts.PMTPID, uint16(psiPMTPID2); got != want {
		t.Fatalf("auto-select picked PMT PID 0x%04X, and this test records that it picks 0x%04X "+
			"(the first program of the LAST section) rather than 0x%04X (the first program of the table). "+
			"If this changed on purpose, move the case into the corpus; if not, it is a regression.",
			got, want, uint16(psiPMTPID3))
	}
}

// --- 2. stream type 0x87 -------------------------------------------------

// A/52 Annex A and the ATSC registration give 0x81 to AC-3 and 0x87 to E-AC-3.
// AudioCodecFromStreamType maps both to "ac3".
//
// Question: should a 0x87 stream be reported as "eac3"? The codec string reaches
// the client capability check and the ffmpeg audio mapping, so this is not only
// cosmetic - but the observer that reads the elementary stream detects E-AC-3
// from bsid regardless of the label, and every DVB service measured here carries
// E-AC-3 as stream type 0x06 with an E-AC-3 descriptor, which is mapped
// correctly. 0x87 is the ATSC shape, which this deployment does not receive.
func TestObserved_StreamType0x87IsReportedAsAC3(t *testing.T) {
	if got := AudioCodecFromStreamType(0x87, nil); got != "ac3" {
		t.Fatalf("stream type 0x87 now reports %q; this test recorded %q. "+
			"ATSC registers 0x87 as E-AC-3, so %q may be the intended fix - decide it, do not re-record it.",
			got, "ac3", "eac3")
	}
	if got := AudioCodecFromStreamType(0x81, nil); got != "ac3" {
		t.Fatalf("stream type 0x81 reports %q, want %q", got, "ac3")
	}
}

// --- 3. Reset clears what SetTargetProgram does not ------------------------

// Reset is not part of the Core interface, so it is not corpus material, but it
// is the other half of the stale-fact finding recorded in the corpus case
// a_target_switch_leaves_the_old_program_number_and_version_behind: Reset
// rebuilds the whole core and so clears pmtProgramNumber and pmtVersion, while
// SetTargetProgram clears neither. Whatever is decided for one has to be decided
// for the other, and this test is where the asymmetry is visible.
func TestObserved_ResetClearsTheProgramNumberThatSetTargetProgramKeeps(t *testing.T) {
	pat := patSection(psiTSID, 0, 0, 0, 1, patProgram{1, psiPMTPID1}, patProgram{2, psiPMTPID2})
	pmt := pmtSection(1, psiVideoPID, 9, 0, 0, 1, nil, esH264(psiVideoPID))

	build := func() *GoCore {
		c := NewGoCore(1)
		if _, err := c.Ingest(context.Background(), 0,
			chunkOf(flatten(psiPackets(0, 0, 0, pat), psiPackets(psiPMTPID1, 0, 0, pmt))...)); err != nil {
			t.Fatalf("ingest: %v", err)
		}
		return c
	}

	switched, err := build().SetTargetProgram(context.Background(), 2)
	if err != nil {
		t.Fatalf("set target: %v", err)
	}
	if switched.Facts.ProgramNumber != 1 || switched.Facts.PMTVersion != 9 {
		t.Fatalf("SetTargetProgram now reports programNumber=%d pmtVersion=%d; this test recorded "+
			"1 and 9 surviving the switch. If they are cleared now, update the corpus case too.",
			switched.Facts.ProgramNumber, switched.Facts.PMTVersion)
	}

	reset := build().Reset()
	if reset.Facts.ProgramNumber != 0 || reset.Facts.PMTVersion != 0 {
		t.Fatalf("Reset left programNumber=%d pmtVersion=%d, want both 0",
			reset.Facts.ProgramNumber, reset.Facts.PMTVersion)
	}
}
