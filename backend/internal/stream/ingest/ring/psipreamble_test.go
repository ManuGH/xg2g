// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"bytes"
	"context"
	"testing"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// replayPreamble reads a preamble the way a subscriber's decoder would: as a
// transport stream and nothing else.
//
// This is what makes canonical packetization safe to do. The preamble is no
// longer the sender's own packets, so "it is the same bytes we were given" is
// not available as a proof any more - and it was never the property that
// mattered. What matters is that a fresh reader of these packets arrives at the
// same tables, and that is checkable directly.
func replayPreamble(t *testing.T, preamble []byte, target uint16) mediafacts.ParseResult {
	t.Helper()
	if len(preamble)%TSPacketSize != 0 {
		t.Fatalf("preamble of %d bytes is not whole packets", len(preamble))
	}
	res, err := mediafacts.NewGoCore(target).Ingest(context.Background(), 0, preamble)
	if err != nil {
		t.Fatalf("replaying the preamble: %v", err)
	}
	return res
}

func sameSections(a, b [][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !bytes.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}

// TestPreamble_ReplaysToTheSameTables is the R8 ring contract.
//
// A subscriber gets the tables in force ahead of the ring data. It used to get
// the very packets the sender had used, which meant the ring's memory and the
// preamble's size were both decided by how finely the sender fragmented. It now
// gets the same sections in packets this ring builds, and this is the proof that
// nothing was lost on the way: feed the preamble to a core that has seen nothing
// else, and it must reach the same programme, the same PMT PID, the same video
// and audio declarations, and the same section bytes.
func TestPreamble_ReplaysToTheSameTables(t *testing.T) {
	const pmtPID = 100
	const videoPID = 256

	r := NewMasterRing(100 * TSPacketSize)
	defer r.Close()

	for _, pkt := range createMultiPacketPAT(pmtPID) {
		if _, err := r.Push(context.Background(), pkt); err != nil {
			t.Fatalf("push pat: %v", err)
		}
	}
	for _, pkt := range createMultiPacketPMT(pmtPID, videoPID, false) {
		if _, err := r.Push(context.Background(), pkt); err != nil {
			t.Fatalf("push pmt: %v", err)
		}
	}

	preamble := r.PATPMTPreamble()
	if len(preamble) == 0 {
		t.Fatal("no preamble after a PAT and a PMT")
	}
	replayed := replayPreamble(t, preamble, 0)

	if replayed.Facts.PMTPID != pmtPID {
		t.Errorf("replayed PMT PID %d, want %d", replayed.Facts.PMTPID, pmtPID)
	}
	if replayed.Facts.VideoPID != videoPID {
		t.Errorf("replayed video PID %d, want %d", replayed.Facts.VideoPID, videoPID)
	}
	if replayed.Facts.VideoCodec != r.facts.VideoCodec {
		t.Errorf("replayed codec %q, ring says %q", replayed.Facts.VideoCodec, r.facts.VideoCodec)
	}
	if replayed.Facts.ProgramNumber != r.facts.ProgramNumber {
		t.Errorf("replayed programme %d, ring says %d", replayed.Facts.ProgramNumber, r.facts.ProgramNumber)
	}
	if len(replayed.Facts.AudioPIDs) != len(r.facts.AudioPIDs) {
		t.Errorf("replayed %d audio PIDs, ring says %d", len(replayed.Facts.AudioPIDs), len(r.facts.AudioPIDs))
	}
	for i := range replayed.Facts.AudioPIDs {
		if i < len(r.facts.AudioPIDs) && replayed.Facts.AudioPIDs[i] != r.facts.AudioPIDs[i] {
			t.Errorf("audio PID %d replayed as %d, ring says %d", i, replayed.Facts.AudioPIDs[i], r.facts.AudioPIDs[i])
		}
	}

	// And the sections themselves come back byte for byte, which is the
	// difference between re-wrapping a table and re-encoding one.
	if !sameSections(replayed.PSI.PATSections, r.activePSI.PATSections) {
		t.Error("the replayed PAT sections are not the ones the ring holds")
	}
	if !sameSections(replayed.PSI.PMTSections, r.activePSI.PMTSections) {
		t.Error("the replayed PMT sections are not the ones the ring holds")
	}
}

// TestPreamble_IsTheSameHoweverTheSenderFragmented is finding #11 seen from the
// ring: two senders, the same tables, one being as awkward as the syntax allows.
func TestPreamble_IsTheSameHoweverTheSenderFragmented(t *testing.T) {
	section := createPATSectionBytes(1, 100, 0, 1, 0, 0)

	normal := NewMasterRing(100 * TSPacketSize)
	defer normal.Close()
	pkt := make([]byte, TSPacketSize)
	pkt[0] = SyncByte
	pkt[1] = 0x40
	pkt[3] = 0x10
	pkt[4] = 0x00
	copy(pkt[5:], section)
	for i := 5 + len(section); i < TSPacketSize; i++ {
		pkt[i] = 0xFF
	}
	if _, err := normal.Push(context.Background(), pkt); err != nil {
		t.Fatalf("push: %v", err)
	}

	starved := NewMasterRing(100 * TSPacketSize)
	defer starved.Close()
	for _, p := range createMultiPacketPAT(100) {
		if _, err := starved.Push(context.Background(), p); err != nil {
			t.Fatalf("push starved: %v", err)
		}
	}

	a, b := normal.PATPMTPreamble(), starved.PATPMTPreamble()
	if len(a) == 0 || len(b) == 0 {
		t.Fatalf("no preamble: %d and %d bytes", len(a), len(b))
	}
	if !bytes.Equal(a, b) {
		t.Errorf("the same PAT sent two ways produces two different preambles: %d and %d bytes", len(a), len(b))
	}
}

// TestPacketizePSISections_IsWellFormedTransport checks the packetizer against
// the packet structure itself rather than against the parser, so a change that
// both sides would agree on is still caught here.
func TestPacketizePSISections_IsWellFormedTransport(t *testing.T) {
	const pid = 0x0123

	longest := make([]byte, mediafacts.MaxSectionBytes)
	for i := range longest {
		longest[i] = byte(i)
	}
	longest[0] = 0x00

	for _, tc := range []struct {
		name     string
		sections [][]byte
		packets  int
	}{
		{"nothing at all", nil, 0},
		{"an empty list", [][]byte{}, 0},
		{"one short section", [][]byte{createPATSectionBytes(1, 100, 0, 1, 0, 0)}, 1},
		{"one section filling a packet exactly", [][]byte{make([]byte, 183)}, 1},
		{"one byte past a packet", [][]byte{make([]byte, 184)}, 2},
		{"the longest a section may be", [][]byte{longest}, 6},
		{"several sections", [][]byte{make([]byte, 10), make([]byte, 10), make([]byte, 10)}, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := packetizePSISections(pid, tc.sections)
			if len(out) != tc.packets*TSPacketSize {
				t.Fatalf("got %d bytes (%d packets), want %d packets", len(out), len(out)/TSPacketSize, tc.packets)
			}
			assertCanonicalPackets(t, out, pid, tc.sections)
		})
	}
}

// TestPacketizePSISections_RefusesWhatCannotBeASection is the defensive edge.
// The core only ever hands over sections it has accepted, so reaching these
// means state upstream is damaged - which is a reason to emit nothing, not to
// emit a packet claiming to carry a section that cannot be read, and not to
// panic in the middle of building a subscriber's preamble.
func TestPacketizePSISections_RefusesWhatCannotBeASection(t *testing.T) {
	for _, tc := range []struct {
		name    string
		section []byte
	}{
		{"empty", []byte{}},
		{"one byte", []byte{0x00}},
		{"two bytes", []byte{0x00, 0xB0}},
		{"one byte past the largest a section may be", make([]byte, mediafacts.MaxSectionBytes+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if out := packetizePSISections(0x0100, [][]byte{tc.section}); len(out) != 0 {
				t.Fatalf("emitted %d bytes for a %d byte section", len(out), len(tc.section))
			}
			// And it does not take the good sections down with it.
			good := createPATSectionBytes(1, 100, 0, 1, 0, 0)
			out := packetizePSISections(0x0100, [][]byte{tc.section, good})
			if len(out) != TSPacketSize {
				t.Fatalf("with one good section beside it, got %d bytes", len(out))
			}
		})
	}
}

// TestPacketizePSISections_ContinuityWraps walks a table long enough to take the
// counter past fifteen, which is where an increment that forgot to wrap would
// put a value the four-bit field cannot hold.
func TestPacketizePSISections_ContinuityWraps(t *testing.T) {
	sections := make([][]byte, 20)
	for i := range sections {
		sections[i] = make([]byte, 10)
	}
	out := packetizePSISections(0x0100, sections)
	if len(out) != 20*TSPacketSize {
		t.Fatalf("got %d packets, want 20", len(out)/TSPacketSize)
	}
	for i := 0; i < 20; i++ {
		if got, want := out[i*TSPacketSize+3]&0x0F, uint8(i%16); got != want {
			t.Fatalf("packet %d has continuity counter %d, want %d", i, got, want)
		}
	}
}

// TestPreamble_IsBoundedByWhatTheTablesMayBe writes down the ceiling an
// adversary can reach, derived from the section bounds rather than chosen.
func TestPreamble_IsBoundedByWhatTheTablesMayBe(t *testing.T) {
	sections := make([][]byte, mediafacts.MaxSectionsPerTable)
	for i := range sections {
		sections[i] = make([]byte, mediafacts.MaxSectionBytes)
	}
	out := packetizePSISections(0x0100, sections)

	const perSection = 6 // 183 payload bytes in the first packet, 184 in each of five more
	wantPackets := mediafacts.MaxSectionsPerTable * perSection
	if len(out) != wantPackets*TSPacketSize {
		t.Fatalf("the largest table packetizes to %d packets, want %d", len(out)/TSPacketSize, wantPackets)
	}
	if len(out) != 288768 {
		t.Fatalf("one table at its ceiling is %d bytes, want 288768", len(out))
	}
	if both := 2 * len(out); both != 577536 {
		t.Fatalf("a PAT and PMT preamble at the ceiling is %d bytes, want 577536", both)
	}
	t.Logf("adversarial ceiling: %d bytes per table, %d for a PAT and PMT preamble", len(out), 2*len(out))
}

// assertCanonicalPackets holds the output to the shape a decoder expects, and
// then reads the sections back out of it. The second half is what makes this a
// test of the packetization rather than of the header fields: the bytes a
// reader recovers must be the bytes that went in, and everything after them
// must be stuffing.
func assertCanonicalPackets(t *testing.T, out []byte, pid uint16, sections [][]byte) {
	t.Helper()
	if len(out)%TSPacketSize != 0 {
		t.Fatalf("%d bytes is not whole packets", len(out))
	}
	starts := 0
	cc := uint8(0)
	var recovered [][]byte
	var current []byte
	for off := 0; off < len(out); off += TSPacketSize {
		pkt := out[off : off+TSPacketSize]
		if pkt[0] != SyncByte {
			t.Fatalf("packet at %d does not start with a sync byte", off/TSPacketSize)
		}
		if got := (uint16(pkt[1]&0x1F) << 8) | uint16(pkt[2]); got != pid {
			t.Fatalf("packet %d is on PID %d, want %d", off/TSPacketSize, got, pid)
		}
		if pkt[1]&0x80 != 0 {
			t.Fatalf("packet %d has the transport error indicator set", off/TSPacketSize)
		}
		if pkt[3]&0xC0 != 0 {
			t.Fatalf("packet %d claims to be scrambled", off/TSPacketSize)
		}
		if got := (pkt[3] >> 4) & 0x03; got != 0x01 {
			t.Fatalf("packet %d has adaptation field control %d, want payload only", off/TSPacketSize, got)
		}
		if got := pkt[3] & 0x0F; got != cc {
			t.Fatalf("packet %d has continuity counter %d, want %d", off/TSPacketSize, got, cc)
		}
		cc = (cc + 1) & 0x0F

		body := pkt[4:]
		if pkt[1]&0x40 != 0 {
			starts++
			if pkt[4] != 0x00 {
				t.Fatalf("packet %d starts a section with pointer_field %d", off/TSPacketSize, pkt[4])
			}
			if current != nil {
				recovered = append(recovered, current)
			}
			current = nil
			body = body[1:]
		} else if current == nil {
			t.Fatalf("packet %d continues a section that never started", off/TSPacketSize)
		}

		want := sections[len(recovered)]
		take := len(want) - len(current)
		if take > len(body) {
			take = len(body)
		}
		current = append(current, body[:take]...)
		// Whatever is left of this packet after the section ends is stuffing.
		// A packet padded with anything else offers a reader bytes it will try
		// to make a section out of.
		for i := take; i < len(body); i++ {
			if body[i] != 0xFF {
				t.Fatalf("packet %d has 0x%02X where it should be stuffed", off/TSPacketSize, body[i])
			}
		}
	}
	if current != nil {
		recovered = append(recovered, current)
	}

	if starts != len(sections) {
		t.Fatalf("%d packets carry a payload unit start, want one per section (%d)", starts, len(sections))
	}
	if !sameSections(recovered, sections) {
		t.Fatalf("read %d sections back out, put %d in", len(recovered), len(sections))
	}
}
