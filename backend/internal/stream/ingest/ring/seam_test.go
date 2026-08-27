// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// Three rules hold this boundary up, and each one is here because breaking it
// would be silent:
//
//	the ring owns the bytes and their offsets
//	the ring owns the generation
//	the core owns what the bytes mean
//
// The third is enforced by the compiler - MasterRing has no parser state left to
// reach for. The other two are enforced here.

// streamWithEntryPoint returns PAT, PMT and one IDR access unit as raw transport,
// which is the smallest input that makes a core report an entry point.
func streamWithEntryPoint(videoPID uint16) []byte {
	const pmtPID = 100

	var out []byte
	for _, pkt := range createMultiPacketPAT(pmtPID) {
		out = append(out, pkt...)
	}
	for _, pkt := range createMultiPacketPMT(pmtPID, videoPID, false) {
		out = append(out, pkt...)
	}

	es := append(startCode(), nalHdrSPS)
	es = append(es, 0x01, 0x02)
	es = append(es, startCode()...)
	es = append(es, nalHdrPPS, 0x03)
	es = append(es, nal(nalHdrIDR, sliceHeaderI)...)
	return append(out, createVideoPESPacket(videoPID, true, 0, es)...)
}

func rapOffsets(events []mediafacts.Event) []int64 {
	var out []int64
	for _, ev := range events {
		if ev.Kind == mediafacts.EventRandomAccessPoint {
			out = append(out, ev.Offset)
		}
	}
	return out
}

// An entry point is only useful if its offset means the same thing to the reader
// that will seek to it. The core is told where the chunk starts and reports in
// that system, so the same bytes ingested at a different base yield offsets that
// differ by exactly the base - never an internal counter that happens to agree
// while the ring starts at zero.
func TestSeam_EntryPointOffsetsAreInTheCallersCoordinateSystem(t *testing.T) {
	const videoPID = 256
	const base = int64(64 * 1024 * 1024)

	data := streamWithEntryPoint(videoPID)

	atZero, err := mediafacts.NewGoCore(1).Ingest(0, data)
	if err != nil {
		t.Fatalf("ingest at 0: %v", err)
	}
	atBase, err := mediafacts.NewGoCore(1).Ingest(base, data)
	if err != nil {
		t.Fatalf("ingest at base: %v", err)
	}

	zeroRAPs, baseRAPs := rapOffsets(atZero.Events), rapOffsets(atBase.Events)
	if len(zeroRAPs) == 0 {
		t.Fatal("no entry point reported; the fixture no longer exercises this")
	}
	if len(zeroRAPs) != len(baseRAPs) {
		t.Fatalf("got %d entry points at 0 and %d at base, want the same stream to read the same", len(zeroRAPs), len(baseRAPs))
	}
	for i := range zeroRAPs {
		if got := baseRAPs[i] - zeroRAPs[i]; got != base {
			t.Errorf("entry point %d moved by %d, want exactly the chunk's own start offset %d", i, got, base)
		}
	}

	if atBase.ProcessedThroughOffset != base+int64(len(data)) {
		t.Errorf("ProcessedThroughOffset = %d, want %d", atBase.ProcessedThroughOffset, base+int64(len(data)))
	}
}

// The core reports that the program's identity changed. It does not decide that a
// new lifecycle epoch has begun, and it cannot: nothing it returns is a
// generation. The ring turns the event into one, which is what keeps subscriber
// recovery, worker cuts and the entry-point index answering to a single owner.
func TestSeam_TheCoreReportsIdentityAndTheRingDecidesTheEpoch(t *testing.T) {
	const pmtPID = 100
	const videoPID = 256

	core := mediafacts.NewGoCore(1)

	var identityEvents int
	ingest := func(packets [][]byte) {
		t.Helper()
		for _, pkt := range packets {
			res, err := core.Ingest(0, pkt)
			if err != nil {
				t.Fatalf("ingest: %v", err)
			}
			for _, ev := range res.Events {
				if ev.Kind == mediafacts.EventProgramIdentityChanged {
					identityEvents++
				}
			}
		}
	}

	ingest(createMultiPacketPAT(pmtPID))
	ingest(createMultiPacketPMT(pmtPID, videoPID, false))
	afterFirst := identityEvents
	if afterFirst == 0 {
		t.Fatal("first PAT/PMT reported no identity change")
	}

	// The same table again is not a new identity.
	ingest(createMultiPacketPMT(pmtPID, videoPID, false))
	if identityEvents != afterFirst {
		t.Errorf("a repeated PMT reported %d further identity changes, want 0", identityEvents-afterFirst)
	}

	// A new version is.
	ingest(createMultiPacketPMTWithVersion(pmtPID, videoPID, false, true, 1, 1))
	if identityEvents <= afterFirst {
		t.Error("a new PMT version reported no identity change")
	}
}

// The same run through the ring: every identity change becomes exactly one
// generation, and it is the ring that counts them.
func TestSeam_EveryIdentityChangeBecomesExactlyOneGeneration(t *testing.T) {
	const pmtPID = 100
	const videoPID = 256

	r := NewMasterRingWithProgram(400*TSPacketSize, 1)
	defer r.Close()

	before := r.Generation()

	for _, pkt := range createMultiPacketPAT(pmtPID) {
		if _, err := r.Push(pkt); err != nil {
			t.Fatalf("push PAT: %v", err)
		}
	}
	for _, pkt := range createMultiPacketPMT(pmtPID, videoPID, false) {
		if _, err := r.Push(pkt); err != nil {
			t.Fatalf("push PMT: %v", err)
		}
	}
	afterFirst := r.Generation()
	if afterFirst <= before {
		t.Fatalf("Generation = %d after the first PAT/PMT, want it to have advanced from %d", afterFirst, before)
	}

	for _, pkt := range createMultiPacketPMT(pmtPID, videoPID, false) {
		if _, err := r.Push(pkt); err != nil {
			t.Fatalf("push repeated PMT: %v", err)
		}
	}
	if now := r.Generation(); now != afterFirst {
		t.Errorf("Generation moved from %d to %d on a repeated PMT - the epoch tracks identity, not packets", afterFirst, now)
	}

	for _, pkt := range createMultiPacketPMTWithVersion(pmtPID, videoPID, false, true, 1, 1) {
		if _, err := r.Push(pkt); err != nil {
			t.Fatalf("push new PMT version: %v", err)
		}
	}
	if now := r.Generation(); now <= afterFirst {
		t.Errorf("Generation = %d after a new PMT version, want it past %d", now, afterFirst)
	}
}

// An entry point found before an identity change belongs to the program that
// ended. Replaying the events in order is what drops it; replaying them as two
// independent lists would keep it and hand a subscriber an attach point into a
// stream that no longer exists.
func TestSeam_EntryPointsBeforeAnIdentityChangeAreDropped(t *testing.T) {
	events := []mediafacts.Event{
		{Kind: mediafacts.EventRandomAccessPoint, Offset: 100},
		{Kind: mediafacts.EventRandomAccessPoint, Offset: 200},
		{Kind: mediafacts.EventProgramIdentityChanged},
		{Kind: mediafacts.EventRandomAccessPoint, Offset: 300},
	}

	r := NewMasterRing(400 * TSPacketSize)
	defer r.Close()

	r.mu.Lock()
	r.applyLocked(mediafacts.ParseResult{Events: events})
	r.mu.Unlock()

	got := r.KeyframeOffsets()
	if len(got) != 1 || got[0] != 300 {
		t.Errorf("KeyframeOffsets = %v, want only the entry point that arrived after the identity change", got)
	}
}
