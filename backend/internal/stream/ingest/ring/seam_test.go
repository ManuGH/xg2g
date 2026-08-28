// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"context"
	"errors"
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

	atZero, err := mediafacts.NewGoCore(1).Ingest(context.Background(), 0, data)
	if err != nil {
		t.Fatalf("ingest at 0: %v", err)
	}
	atBase, err := mediafacts.NewGoCore(1).Ingest(context.Background(), base, data)
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
			res, err := core.Ingest(context.Background(), 0, pkt)
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
		if _, err := r.Push(context.Background(), pkt); err != nil {
			t.Fatalf("push PAT: %v", err)
		}
	}
	for _, pkt := range createMultiPacketPMT(pmtPID, videoPID, false) {
		if _, err := r.Push(context.Background(), pkt); err != nil {
			t.Fatalf("push PMT: %v", err)
		}
	}
	afterFirst := r.Generation()
	if afterFirst <= before {
		t.Fatalf("Generation = %d after the first PAT/PMT, want it to have advanced from %d", afterFirst, before)
	}

	for _, pkt := range createMultiPacketPMT(pmtPID, videoPID, false) {
		if _, err := r.Push(context.Background(), pkt); err != nil {
			t.Fatalf("push repeated PMT: %v", err)
		}
	}
	if now := r.Generation(); now != afterFirst {
		t.Errorf("Generation moved from %d to %d on a repeated PMT - the epoch tracks identity, not packets", afterFirst, now)
	}

	for _, pkt := range createMultiPacketPMTWithVersion(pmtPID, videoPID, false, true, 1, 1) {
		if _, err := r.Push(context.Background(), pkt); err != nil {
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

// A snapshot the caller can write through is not a snapshot. The pre-seam code
// copied these slices on the way out; the boundary made that easy to lose,
// because the cached facts already look like a private copy - they are, of the
// core's state, and shared with every caller of this one.
func TestSeam_ReadinessFactsIsIndependentOfTheRing(t *testing.T) {
	r := NewMasterRingWithProgram(400*TSPacketSize, 1)
	defer r.Close()

	pushAll(t, r, obsPAT(), obsPMT(0,
		ac3Track(mtGerman, "deu", 0x85),
		ac3Track(mtEnglish, "eng", 0x82),
	))

	first := r.ReadinessFacts()
	if len(first.AudioPIDs) == 0 || len(first.AudioTracks) == 0 {
		t.Fatal("fixture no longer produces audio tracks; this test would prove nothing")
	}

	wantPID := first.AudioPIDs[0]
	wantCodec := first.AudioTracks[0].Codec
	wantScramblingPID := first.Scrambling.AudioPIDs[0]

	// A consumer doing what consumers do with a value they were handed.
	first.AudioPIDs[0] = 0xBEEF
	first.AudioTracks[0].Codec = "clobbered"
	first.Scrambling.AudioPIDs[0] = 0xBEEF

	second := r.ReadinessFacts()
	if second.AudioPIDs[0] != wantPID {
		t.Errorf("AudioPIDs[0] = %d after a caller wrote to an earlier snapshot, want %d", second.AudioPIDs[0], wantPID)
	}
	if second.AudioTracks[0].Codec != wantCodec {
		t.Errorf("AudioTracks[0].Codec = %q, want %q", second.AudioTracks[0].Codec, wantCodec)
	}
	if second.Scrambling.AudioPIDs[0] != wantScramblingPID {
		t.Errorf("Scrambling.AudioPIDs[0] = %d, want %d", second.Scrambling.AudioPIDs[0], wantScramblingPID)
	}

	// And the two snapshots must not share backing arrays with each other either.
	second.AudioPIDs[0] = 0xF00D
	if third := r.ReadinessFacts(); third.AudioPIDs[0] != wantPID {
		t.Errorf("AudioPIDs[0] = %d, want %d - snapshots share a backing array", third.AudioPIDs[0], wantPID)
	}
}

// shortCore interprets everything it is given and then understates how far it
// got, which is what a truncated IPC response or a core that gave up mid-chunk
// would look like from the ring's side.
type shortCore struct {
	mediafacts.Core
	shortBy int64
}

func (c shortCore) Ingest(ctx context.Context, startOffset int64, data []byte) (mediafacts.ParseResult, error) {
	res, err := c.Core.Ingest(ctx, startOffset, data)
	res.ProcessedThroughOffset -= c.shortBy
	// Events the ring must not act on, precisely because the chunk is short.
	res.Events = append(res.Events, mediafacts.Event{Kind: mediafacts.EventProgramIdentityChanged})
	return res, err
}

// Bytes the ring cannot explain are worse than bytes it does not have. A chunk
// the core did not fully interpret is refused, and nothing moves with it.
func TestSeam_AChunkTheCoreDidNotFinishIsNotCommitted(t *testing.T) {
	const pmtPID = 100

	r := NewMasterRingWithProgram(400*TSPacketSize, 1)
	defer r.Close()
	r.core = shortCore{Core: r.core, shortBy: TSPacketSize}

	headBefore := r.Head()
	tailBefore := r.Tail()
	genBefore := r.Generation()
	kfBefore := len(r.KeyframeOffsets())
	factsBefore := r.ReadinessFacts()

	data := createMultiPacketPAT(pmtPID)[0]
	n, err := r.Push(context.Background(), data)

	if !errors.Is(err, ErrCoreIncomplete) {
		t.Fatalf("Push err = %v, want ErrCoreIncomplete", err)
	}
	if n != 0 {
		t.Errorf("Push n = %d, want 0", n)
	}
	if got := r.Head(); got != headBefore {
		t.Errorf("Head = %d, want %d - bytes were committed for a chunk that was refused", got, headBefore)
	}
	if got := r.Tail(); got != tailBefore {
		t.Errorf("Tail = %d, want %d", got, tailBefore)
	}
	if got := r.Generation(); got != genBefore {
		t.Errorf("Generation = %d, want %d - the refused chunk's events were applied", got, genBefore)
	}
	if got := len(r.KeyframeOffsets()); got != kfBefore {
		t.Errorf("KeyframeOffsets len = %d, want %d", got, kfBefore)
	}
	if got := r.ReadinessFacts(); got.HasPAT != factsBefore.HasPAT || got.ProgramNumber != factsBefore.ProgramNumber {
		t.Errorf("facts moved on a refused chunk: %+v", got)
	}
	if got := r.BufferedBytes(); got != 0 {
		t.Errorf("BufferedBytes = %d, want 0 - refused bytes are readable", got)
	}
}

// The zero value of an event is what a truncated frame or a failed decode
// produces. It must mean nothing, not "a new programme began".
func TestSeam_TheZeroEventIsNotALifecycleChange(t *testing.T) {
	r := NewMasterRing(400 * TSPacketSize)
	defer r.Close()

	r.mu.Lock()
	genBefore := r.generation
	r.applyLocked(mediafacts.ParseResult{Events: []mediafacts.Event{
		{},                                // zero value
		{Kind: mediafacts.EventUnknown},   // named, still nothing
		{Kind: mediafacts.EventKind(200)}, // a kind this build does not know
		{Kind: mediafacts.EventKind(200), Offset: 4242},
	}})
	genAfter := r.generation
	kf := len(r.keyframeOffsets)
	r.mu.Unlock()

	if genAfter != genBefore {
		t.Errorf("Generation moved from %d to %d on events that say nothing", genBefore, genAfter)
	}
	if kf != 0 {
		t.Errorf("KeyframeOffsets len = %d, want 0", kf)
	}
}

// The PMT PID is what the PAT says, so it is a fact about the stream rather than
// a helper for tests. It has to survive a remapping, and a remapping is an
// identity change.
func TestSeam_PMTPIDFollowsThePAT(t *testing.T) {
	core := mediafacts.NewGoCore(1)

	ingest := func(packets [][]byte) mediafacts.ParseResult {
		t.Helper()
		var last mediafacts.ParseResult
		for _, pkt := range packets {
			res, err := core.Ingest(context.Background(), 0, pkt)
			if err != nil {
				t.Fatalf("ingest: %v", err)
			}
			last = res
		}
		return last
	}

	ingest(createMultiPacketPAT(100))
	if got := core.Snapshot().PMTPID; got != 100 {
		t.Fatalf("Facts.PMTPID = %d, want 100", got)
	}

	// The same program is remapped to a different PMT PID.
	res := ingest(createMultiPacketPATWithVersion(200, 1, 1))
	if got := core.Snapshot().PMTPID; got != 200 {
		t.Errorf("Facts.PMTPID = %d after the PAT remapped the program, want 200", got)
	}

	identity := false
	for _, ev := range res.Events {
		if ev.Kind == mediafacts.EventProgramIdentityChanged {
			identity = true
		}
	}
	if !identity {
		t.Error("a PAT that moved the program to another PMT PID reported no identity change")
	}
}
