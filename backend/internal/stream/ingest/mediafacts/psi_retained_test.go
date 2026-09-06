// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package mediafacts

import (
	"context"
	"testing"
)

// TestPSI_ATableUsingEveryNumberItMayCompletes is the largest generation the
// syntax allows, which is also the one that used not to terminate.
//
// last_section_number 255 means 256 sections. Every walk over them counted in a
// uint8 bounded by 255, so the counter wrapped to zero instead of ending and the
// completeness check span forever - holding the caller's lock, on input that is
// entirely legal and that nothing else in this parser refuses. It is reached by
// delivering a table that uses all 256 numbers, which the syntax allows and
// which no conforming reader may reject.
//
// This test does not need a timeout to be a test: before the fix it does not
// fail, it hangs, and the package's own test deadline is what reports it.
func TestPSI_ATableUsingEveryNumberItMayCompletes(t *testing.T) {
	ctx := context.Background()
	c := NewGoCore(1)

	// Sections 0..254 name nothing this core is looking for; section 255 names
	// the target, so completion is observable rather than assumed.
	var pkts [][]byte
	cc := uint8(0)
	for n := 0; n < 256; n++ {
		prog := patProgram{9, 0x0900}
		if n == 255 {
			prog = patProgram{1, psiPMTPID1}
		}
		// #nosec G115 -- n < 256
		sec := patSection(psiTSID, 0, uint8(n), 255, 1, prog)
		pkts = append(pkts, psiPackets(0, cc, 0, sec)...)
		cc = (cc + 1) & 0x0F
	}

	res, err := c.Ingest(ctx, 0, chunkOf(pkts...))
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if res.Facts.PMTPID != uint16(psiPMTPID1) {
		t.Fatalf("a complete 256-section PAT did not name the target: PMT PID %d", res.Facts.PMTPID)
	}
}
