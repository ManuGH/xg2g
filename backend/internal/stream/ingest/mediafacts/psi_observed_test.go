// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package mediafacts

import "testing"

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
// Four more lived here and no longer do. The ES_info_length that reached into
// the section CRC and the table_id that a split section header slipped past were
// repaired in R1; the auto-selection that let a later PAT section re-choose, and
// the program number and version that outlived the program they described, were
// repaired in R2. Each expectation was re-authored from the fixed contract and
// moved to where it belongs - the shared corpus for what crosses the Core
// interface, TestPSI_ResetAndATargetSwitchAgreeAboutWhatIsForgotten for the one
// invariant that involves a method the interface deliberately does not carry.
//
// What is left here is the last unadjudicated finding.

// --- stream type 0x87 ----------------------------------------------------

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
