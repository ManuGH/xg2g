// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package mediafacts

import (
	"context"
	"testing"
)

// retainedPSIBytes is every byte this core is holding on account of PSI.
//
// The corpus cannot see this. It compares facts and the tables in force, and a
// core that also kept a copy of every packet it had ever been given would agree
// with it on all of them - which is exactly what the reference used to do. So
// the memory the parser holds is stated here as a number, and the tests below
// hold that number to a bound rather than to a hope.
func retainedPSIBytes(c *GoCore) int {
	n := 0
	for _, a := range []*psiStreamAssembler{&c.patAssembler, &c.pmtAssembler} {
		n += len(a.buf) + len(a.lastPacket)
	}
	for _, t := range []*tableSectionTracker{&c.patTracker, &c.pmtTracker} {
		for _, sec := range t.sections {
			n += len(sec)
		}
	}
	for _, list := range [][][]byte{c.activePATSections, c.activePMTSections} {
		for _, sec := range list {
			n += len(sec)
		}
	}
	return n
}

// starvedPackets delivers a section one payload byte at a time.
//
// Each packet carries an adaptation field that eats the rest of the payload, so
// the transport spends 188 bytes to move one byte of table. That is legal: a
// sender may fragment as finely as it likes. It is also the cheapest way to ask
// a parser to hold a lot of memory for very little table, which is why it is
// what the bound has to be measured against.
//
// The first packet carries two payload bytes rather than one - the pointer
// field and the section's first byte - because a payload unit start carrying
// only a pointer field begins no section here. That is existing behaviour and
// not what this file is about; it only sets where the adversary starts.
func starvedPackets(pid uint16, startCC uint8, section []byte) [][]byte {
	payloads := [][]byte{{0x00, section[0]}}
	for _, b := range section[1:] {
		payloads = append(payloads, []byte{b})
	}

	out := make([][]byte, 0, len(payloads))
	cc := startCC
	for i, payload := range payloads {
		pkt := make([]byte, TSPacketSize)
		pkt[0] = SyncByte
		pkt[1] = byte((pid >> 8) & 0x1F)
		if i == 0 {
			pkt[1] |= 0x40 // payload_unit_start_indicator
		}
		pkt[2] = byte(pid & 0xFF)
		pkt[3] = 0x30 | (cc & 0x0F) // adaptation field, then payload
		cc = (cc + 1) & 0x0F

		start := TSPacketSize - len(payload)
		// #nosec G115 -- start-5 is at most 183 for a payload of one byte
		pkt[4] = byte(start - 5) // adaptation_field_length
		pkt[5] = 0x00            // no flags; the rest of the field is stuffing
		for j := 6; j < start; j++ {
			pkt[j] = 0xFF
		}
		copy(pkt[start:], payload)
		out = append(out, pkt)
	}
	return out
}

// TestPSI_TransportFragmentationDoesNotChangeWhatIsHeld is the central R8
// claim, stated as a differential between two deliveries of the same table.
//
// A PAT sent in six packets and the same PAT sent one payload byte at a time are
// the same PAT. Before this, the second cost 528 retained packets - 96 KiB - for
// a table of 1024 bytes, because the packets that carried it were kept as part
// of the answer. The sender chose how much memory xg2g spent.
func TestPSI_TransportFragmentationDoesNotChangeWhatIsHeld(t *testing.T) {
	section := patOfSize(253, psiPMTPID1)
	if len(section) != maxPSISectionBytes {
		t.Fatalf("the fixture is %d bytes, not the largest a section may be", len(section))
	}
	ctx := context.Background()

	normal := NewGoCore(1)
	normalRes, err := normal.Ingest(ctx, 0, chunkOf(psiPackets(0, 0, 0, section)...))
	if err != nil {
		t.Fatalf("normal ingest: %v", err)
	}
	starved := NewGoCore(1)
	starvedPkts := starvedPackets(0, 0, section)
	starvedRes, err := starved.Ingest(ctx, 0, chunkOf(starvedPkts...))
	if err != nil {
		t.Fatalf("starved ingest: %v", err)
	}

	if got := len(starvedPkts); got != len(section) {
		t.Fatalf("the adversary sent %d packets for a %d byte section", got, len(section))
	}
	if normalRes.Facts.PMTPID != starvedRes.Facts.PMTPID {
		t.Fatalf("the two deliveries chose different PMT PIDs: %d and %d",
			normalRes.Facts.PMTPID, starvedRes.Facts.PMTPID)
	}
	if !sectionListsEqual(normalRes.PSI.PATSections, starvedRes.PSI.PATSections) {
		t.Fatalf("the same PAT delivered two ways is held as two different tables:\n normal  %v\n starved %v",
			sectionSizes(normalRes.PSI.PATSections), sectionSizes(starvedRes.PSI.PATSections))
	}
	if a, b := retainedPSIBytes(normal), retainedPSIBytes(starved); a != b {
		t.Fatalf("the starved delivery costs %d retained bytes against %d: fragmentation is still amplifying state", b, a)
	}
	t.Logf("1024-byte PAT: %d packets retains %d bytes, %d packets retains %d bytes",
		len(psiPackets(0, 0, 0, section)), retainedPSIBytes(normal),
		len(starvedPkts), retainedPSIBytes(starved))
}

// TestPSI_ACarouselOfStarvedTablesStaysBounded is finding #11 itself: not one
// delivery, but the carousel repeating forever the way a real transport does.
func TestPSI_ACarouselOfStarvedTablesStaysBounded(t *testing.T) {
	section := patOfSize(253, psiPMTPID1)
	ctx := context.Background()
	c := NewGoCore(1)

	var first int
	for round := 0; round < 40; round++ {
		// #nosec G115 -- masked to four bits by the packet builder
		cc := uint8((round * 3) & 0x0F)
		if _, err := c.Ingest(ctx, 0, chunkOf(starvedPackets(0, cc, section)...)); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		got := retainedPSIBytes(c)
		if round == 0 {
			first = got
			continue
		}
		if got != first {
			t.Fatalf("round %d retains %d bytes, round 1 retained %d", round+1, got, first)
		}
	}
	// What a single-section table may cost: the tracker's copy of the generation
	// being assembled, the copy held as the table in force, and the one packet
	// kept to recognise an exact carousel duplicate. Nothing scales with how the
	// sender chose to cut it up.
	bound := 2*maxPSISectionBytes + TSPacketSize
	if first > bound {
		t.Fatalf("one PAT section costs %d retained bytes, past the %d a section may cost", first, bound)
	}
	t.Logf("40 carousel rounds of a starved 1024-byte PAT: %d bytes retained throughout (bound %d)", first, bound)
}

// TestPSI_ActivePSIIsBoundedByWhatTheTablesMayBe pins the derived ceiling.
//
// The numbers are not a policy. A section is at most 1024 bytes because
// ISO/IEC 13818-1 caps section_length at 1021, and a table is at most 256
// sections because section_number is a byte and a section outside its own table
// is refused. Everything else follows, and this is the arithmetic written down
// where a change to either bound will fail it.
func TestPSI_ActivePSIIsBoundedByWhatTheTablesMayBe(t *testing.T) {
	if maxPSISectionBytes != 1024 {
		t.Fatalf("a section is %d bytes at most, not 1024", maxPSISectionBytes)
	}
	if maxSectionsPerTable != 256 {
		t.Fatalf("a table has %d sections at most, not 256", maxSectionsPerTable)
	}
	if maxActiveTableBytes != 256*1024 {
		t.Fatalf("one active table is bounded at %d bytes, not 256 KiB", maxActiveTableBytes)
	}
	if maxActivePSIBytes != 512*1024 {
		t.Fatalf("ActivePSI is bounded at %d bytes, not 512 KiB", maxActivePSIBytes)
	}
}

// TestPSI_ATableUsingEveryNumberItMayCompletes is the largest generation the
// syntax allows, which is also the one that used not to terminate.
//
// last_section_number 255 means 256 sections. Every walk over them counted in a
// uint8 bounded by 255, so the counter wrapped to zero instead of ending and the
// completeness check span forever - holding the caller's lock, on input that is
// entirely legal. It is reached by delivering a table that uses all 256 numbers,
// which nothing prevents and nothing should.
func TestPSI_ATableUsingEveryNumberItMayCompletes(t *testing.T) {
	ctx := context.Background()
	c := NewGoCore(1)

	// Sections 0..254 name nothing this core is looking for; section 255 names
	// the target, so completion is observable rather than assumed.
	var pkts [][]byte
	cc := uint8(0)
	sections := make([][]byte, 0, maxSectionsPerTable)
	for n := 0; n < maxSectionsPerTable; n++ {
		prog := patProgram{9, 0x0900}
		if n == maxSectionsPerTable-1 {
			prog = patProgram{1, psiPMTPID1}
		}
		// #nosec G115 -- n < 256
		sec := patSection(psiTSID, 0, uint8(n), 255, 1, prog)
		sections = append(sections, sec)
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
	if !sectionListsEqual(res.PSI.PATSections, sections) {
		t.Fatalf("the table in force is %v, not the 256 sections delivered", sectionSizes(res.PSI.PATSections))
	}
	if n := retainedPSIBytes(c); n > maxActivePSIBytes {
		t.Fatalf("the largest legal table retains %d bytes, past the %d bound", n, maxActivePSIBytes)
	}

	// And it does not accumulate: the same table again replaces itself.
	before := retainedPSIBytes(c)
	if _, err := c.Ingest(ctx, 0, chunkOf(pkts...)); err != nil {
		t.Fatalf("second delivery: %v", err)
	}
	if after := retainedPSIBytes(c); after != before {
		t.Fatalf("a repeat of the same 256-section table took retention from %d to %d", before, after)
	}
	t.Logf("256 sections, %d bytes of table, %d bytes retained", len(sections)*len(sections[0]), before)
}

func sectionListsEqual(a, b [][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if string(a[i]) != string(b[i]) {
			return false
		}
	}
	return true
}

func sectionSizes(in [][]byte) []int {
	out := make([]int, len(in))
	for i, s := range in {
		out[i] = len(s)
	}
	return out
}
