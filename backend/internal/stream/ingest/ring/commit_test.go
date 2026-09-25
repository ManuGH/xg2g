// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"context"
	"errors"
	"io"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
	"github.com/ManuGH/xg2g/internal/stream/timeline"
)

type mockFailCore struct {
	result mediafacts.ParseResult
	err    error
}

func (m *mockFailCore) Ingest(ctx context.Context, startOffset int64, chunk []byte) (mediafacts.ParseResult, error) {
	if m.err != nil {
		return mediafacts.ParseResult{}, m.err
	}
	res := m.result
	res.ProcessedThroughOffset = startOffset + int64(len(chunk))
	return res, nil
}

func (m *mockFailCore) SetTargetProgram(ctx context.Context, programNumber uint16) (mediafacts.ParseResult, error) {
	return mediafacts.ParseResult{Coverage: mediafacts.ParseCoverageComplete}, nil
}

func (m *mockFailCore) Reset() {}

func TestMediaCommit_FailClosedOnMediaIndexError(t *testing.T) {
	// ParseResult with non-canonical timing authority (TimingAuthorityNone).
	// MediaIndex.ApplyIngestResult will fail closed with ErrNonCanonicalTiming.
	badRes := mediafacts.ParseResult{
		Coverage: mediafacts.ParseCoverageComplete,
		Timing: mediafacts.TimingResult{
			Authority: mediafacts.TimingAuthorityNone, // non-canonical!
		},
		Events: []mediafacts.Event{
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 0, Joinable: true},
		},
	}

	core := &mockFailCore{result: badRes}
	r := NewMasterRingWithCore(10*TSPacketSize, core, WithCanonicalTimeline())
	defer r.Close()

	data := make([]byte, TSPacketSize)
	data[0] = SyncByte

	_, err := r.Push(context.Background(), data)
	if !errors.Is(err, timeline.ErrNonCanonicalTiming) {
		t.Fatalf("expected ErrNonCanonicalTiming, got: %v", err)
	}

	// Invariant: "Kein Byte ohne Wahrheit, keine Wahrheit ohne Bytes."
	// When MediaIndex fails, PacketStore and AttachIndex must remain untouched.
	if r.Head() != 0 {
		t.Errorf("head mutated: got %d, want 0", r.Head())
	}
	if r.Tail() != 0 {
		t.Errorf("tail mutated: got %d, want 0", r.Tail())
	}
	if len(r.KeyframeOffsets()) != 0 {
		t.Errorf("attachIndex mutated: got %v, want empty", r.KeyframeOffsets())
	}
	if !r.coreUnusable {
		t.Error("expected coreUnusable to be true")
	}

	// Verify timeline index itself was not mutated
	if stats := r.Timeline().Stats(); stats.TotalRAPs != 0 {
		t.Errorf("timeline index mutated: stats=%+v", stats)
	}
}

func TestMediaCommit_AtomicallyPublishesBytesAndTruth(t *testing.T) {
	goodRes := mediafacts.ParseResult{
		Coverage: mediafacts.ParseCoverageComplete,
		Timing: mediafacts.TimingResult{
			Authority: mediafacts.TimingAuthorityCanonical,
			Records: []mediafacts.TimingRecord{
				{
					Type: mediafacts.TimingRecordTypeDiscontinuity,
					Discontinuity: mediafacts.DiscontinuityRecord{
						Scope:         mediafacts.DiscontinuityScopeProgram,
						ObservedAt:    0,
						HasEpochAfter: true,
						EpochAfter:    1,
					},
				},
				{
					Type: mediafacts.TimingRecordTypeRandomAccessPoint,
					RAP: mediafacts.TimingPoint{
						Epoch:      1,
						PID:        256,
						ObservedAt: 0,
						SubjectAt:  0,
						HasPTS:     true,
						PTS90k:     90000,
					},
				},
			},
		},
		Events: []mediafacts.Event{
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 0, Joinable: true},
		},
		Facts: mediafacts.Facts{
			HasPAT:     true,
			HasPMT:     true,
			VideoPID:   256,
			VideoCodec: CodecH264,
		},
	}

	core := &mockFailCore{result: goodRes}
	r := NewMasterRingWithCore(10*TSPacketSize, core, WithCanonicalTimeline())
	defer r.Close()

	data := make([]byte, TSPacketSize)
	data[0] = SyncByte

	n, err := r.Push(context.Background(), data)
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}
	if n != TSPacketSize {
		t.Fatalf("Push returned %d bytes, want %d", n, TSPacketSize)
	}

	// Verify atomic publication of both bytes and metadata
	if r.Head() != int64(TSPacketSize) {
		t.Errorf("head = %d, want %d", r.Head(), TSPacketSize)
	}
	kfs := r.KeyframeOffsets()
	if len(kfs) != 1 || kfs[0] != 0 {
		t.Errorf("attachIndex keyframes = %v, want [0]", kfs)
	}
	facts := r.ReadinessFacts()
	if !facts.HasPAT || !facts.HasPMT || facts.VideoPID != 256 {
		t.Errorf("facts mismatch: %+v", facts)
	}
	if stats := r.Timeline().Stats(); stats.TotalRAPs != 1 {
		t.Errorf("timeline stats = %+v, want TotalRAPs=1", stats)
	}
}

type mockStreamingCore struct {
	mu sync.Mutex
}

func (m *mockStreamingCore) Ingest(ctx context.Context, startOffset int64, chunk []byte) (mediafacts.ParseResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	numPackets := len(chunk) / TSPacketSize
	var events []mediafacts.Event
	var records []mediafacts.TimingRecord

	if startOffset == 0 {
		records = append(records, mediafacts.TimingRecord{
			Type: mediafacts.TimingRecordTypeDiscontinuity,
			Discontinuity: mediafacts.DiscontinuityRecord{
				Scope:         mediafacts.DiscontinuityScopeProgram,
				ObservedAt:    0,
				HasEpochAfter: true,
				EpochAfter:    1,
			},
		})
	}

	for i := 0; i < numPackets; i++ {
		pktOffset := startOffset + int64(i*TSPacketSize)
		if chunk[i*TSPacketSize+1]&0x40 != 0 {
			events = append(events, mediafacts.Event{
				Kind:     mediafacts.EventRandomAccessPoint,
				Offset:   pktOffset,
				Joinable: true,
			})
			records = append(records, mediafacts.TimingRecord{
				Type: mediafacts.TimingRecordTypeRandomAccessPoint,
				RAP: mediafacts.TimingPoint{
					Epoch:      1,
					PID:        256,
					ObservedAt: pktOffset,
					SubjectAt:  pktOffset,
					HasPTS:     true,
					PTS90k:     pktOffset * 90,
				},
			})
		}
	}

	return mediafacts.ParseResult{
		Coverage:               mediafacts.ParseCoverageComplete,
		ProcessedThroughOffset: startOffset + int64(len(chunk)),
		Events:                 events,
		Timing: mediafacts.TimingResult{
			Authority: mediafacts.TimingAuthorityCanonical,
			Records:   records,
		},
		Facts: mediafacts.Facts{
			HasPAT:     true,
			HasPMT:     true,
			PMTPID:     4096,
			VideoPID:   256,
			VideoCodec: CodecH264,
		},
		PSI: mediafacts.ActivePSI{
			PATSections: [][]byte{{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}},
			PMTSections: [][]byte{{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0xAA, 0xBB, 0xCC, 0xDD}},
		},
	}, nil
}

func (m *mockStreamingCore) SetTargetProgram(ctx context.Context, programNumber uint16) (mediafacts.ParseResult, error) {
	return mediafacts.ParseResult{Coverage: mediafacts.ParseCoverageComplete}, nil
}

func (m *mockStreamingCore) Reset() {}

func TestMediaCommit_ConcurrentTimelineAndBytePublication(t *testing.T) {
	core := &mockStreamingCore{}
	// Small ring capacity: 10 packets (1880 bytes) to force wrap-arounds and tail pruning.
	// WithCanonicalTimeline constructs and owns the index internally, eliminating any retained pointer bypass.
	r := NewMasterRingWithCore(10*TSPacketSize, core, WithCanonicalTimeline())
	defer r.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	const iterations = 300
	chunkSize := 2 * TSPacketSize

	// Writer goroutine: repeatedly pushes chunks with alternating RAPs.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			data := make([]byte, chunkSize)
			// Packet 0: RAP
			data[0] = SyncByte
			data[1] = 0x40 // PUSI set -> RAP
			data[2] = byte(i & 0xFF)
			// Packet 1: non-RAP
			data[TSPacketSize] = SyncByte
			data[TSPacketSize+1] = 0x00
			data[TSPacketSize+2] = byte(i & 0xFF)

			if _, err := r.Push(ctx, data); err != nil {
				return
			}
		}
	}()

	// Reader goroutine 1: TimelineReader & atomic consistency queries under MasterRing.mu.
	wg.Add(1)
	go func() {
		defer wg.Done()
		tl := r.Timeline()
		buf := make([]byte, TSPacketSize)

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// Invariant 1 (Strict Atomic Publication): Observed under a single MasterRing.mu lock acquisition.
			// Proves that at the exact linearization point of publication, head, tail, and timeline truth are in lockstep.
			obs, ok := r.TimelineObservation()
			if ok && obs.HasLatest {
				if obs.LatestRAP.Offset+int64(TSPacketSize) > obs.Head {
					t.Errorf("ATOMIC INVARIANT VIOLATION: LatestRAP %d exceeds Head %d under single lock", obs.LatestRAP.Offset, obs.Head)
				}
				if obs.LatestRAP.Offset < obs.Tail {
					t.Errorf("ATOMIC INVARIANT VIOLATION: LatestRAP %d is below Tail %d under single lock", obs.LatestRAP.Offset, obs.Tail)
				}

				// If not pruned, verify actual bytes in PacketStore
				n, _, err := r.ReadAt(buf, obs.LatestRAP.Offset)
				if err == nil {
					if n < TSPacketSize {
						t.Errorf("ReadAt returned short read: %d < %d", n, TSPacketSize)
					}
					if buf[0] != SyncByte {
						t.Errorf("corrupted byte at LatestRAP %d: got 0x%02x, want 0x%02x", obs.LatestRAP.Offset, buf[0], SyncByte)
					}
					if buf[1]&0x40 == 0 {
						t.Errorf("byte at LatestRAP %d lost PUSI/RAP marker: 0x%02x", obs.LatestRAP.Offset, buf[1])
					}
				} else if !errors.Is(err, ErrSubscriberOverrun) {
					t.Errorf("unexpected ReadAt error: %v", err)
				}
			}

			// Invariant 2: External TimelineReader queries synchronized under MasterRing.mu.
			if rap, ok := tl.FindPrecedingRAP(math.MaxInt64); ok {
				// Reading bytes at rap.Offset must either succeed with valid sync byte
				// or fail with ErrSubscriberOverrun if concurrent writer advanced tail in between.
				// It must NEVER return io.EOF (which would indicate uncommitted bytes).
				n, _, err := r.ReadAt(buf, rap.Offset)
				if err == nil {
					if n < TSPacketSize {
						t.Errorf("ReadAt returned short read at RAP %d: %d < %d", rap.Offset, n, TSPacketSize)
					}
					if buf[0] != SyncByte {
						t.Errorf("corrupted byte at RAP %d: got 0x%02x, want 0x%02x", rap.Offset, buf[0], SyncByte)
					}
				} else if errors.Is(err, io.EOF) {
					t.Errorf("INVARIANT VIOLATION: RAP at %d published in Timeline before bytes committed in store (EOF)", rap.Offset)
				} else if !errors.Is(err, ErrSubscriberOverrun) {
					t.Errorf("unexpected ReadAt error at RAP %d: %v", rap.Offset, err)
				}
			}

			// Invariant 3: RAPsBetween for current tail must never return RAPs below that tail.
			tail := r.Tail()
			raps := tl.RAPsBetween(tail, math.MaxInt64)
			head := r.Head()
			for _, rap := range raps {
				if rap.Offset < tail {
					t.Errorf("INVARIANT VIOLATION: Timeline returned pruned RAP at %d (tail=%d)", rap.Offset, tail)
				}
				if rap.Offset+int64(TSPacketSize) > head {
					t.Errorf("INVARIANT VIOLATION: Timeline returned uncommitted RAP at %d (head=%d)", rap.Offset, head)
				}
			}

			if r.Head() >= int64(iterations*chunkSize) {
				return
			}
		}
	}()

	// Reader goroutine 2: Active subscriber reading stream concurrently.
	wg.Add(1)
	go func() {
		defer wg.Done()
		readBuf := make([]byte, chunkSize)
		sub := r.NewSubscriberReader(0)

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			n, err := sub.Read(readBuf)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
					return
				}
			}
			if n > 0 && readBuf[0] != SyncByte {
				t.Errorf("subscriber read corrupted sync byte: 0x%02x", readBuf[0])
			}

			if r.Head() >= int64(iterations*chunkSize) {
				return
			}
		}
	}()

	wg.Wait()
}

// TestMediaCommit_TimelineReaderBlocksDuringCommit proves that when a caller holds an
// already-obtained TimelineReader, all query operations block deterministically while
// MasterRing.mu is held (simulating an active commit transaction in progress), preventing
// any intermediate state observation.
func TestMediaCommit_TimelineReaderBlocksDuringCommit(t *testing.T) {
	r := NewMasterRingWithCore(10*TSPacketSize, mediafacts.NewGoCore(1), WithCanonicalTimeline())
	defer r.Close()

	tl := r.Timeline()

	// Simulate an active commit transaction by acquiring MasterRing.mu
	r.mu.Lock()

	queryDone := make(chan struct{})
	go func() {
		// Attempt query via previously obtained reader. Must block on r.mu.
		_, _ = tl.FindPrecedingRAP(math.MaxInt64)
		close(queryDone)
	}()

	// Deterministic assertion: query must NOT return while r.mu is held.
	select {
	case <-queryDone:
		r.mu.Unlock()
		t.Fatal("ATOMIC INVARIANT VIOLATION: TimelineReader query returned while MasterRing.mu was locked")
	case <-time.After(50 * time.Millisecond):
		// Expected: query is properly blocked waiting for MasterRing.mu publication lock
	}

	// Release MasterRing.mu (simulating commit completion)
	r.mu.Unlock()

	// The blocked query must now unblock promptly
	select {
	case <-queryDone:
		// Succeeded
	case <-time.After(1 * time.Second):
		t.Fatal("TimelineReader query failed to unblock after MasterRing.mu was unlocked")
	}
}

// TestMediaCommit_DeterministicVisibilityWindow_UncommittedAndPrunedClamped proves that
// TimelineReader never publishes uncommitted or pruned RAPs under any circumstance.
// Even if the underlying MediaIndex contains records beyond head or before tail (simulating
// the original visibility window), TimelineReader clamps queries strictly to [tail, head).
func TestMediaCommit_DeterministicVisibilityWindow_UncommittedAndPrunedClamped(t *testing.T) {
	r := NewMasterRingWithCore(10*TSPacketSize, mediafacts.NewGoCore(1), WithCanonicalTimeline())
	defer r.Close()

	tl := r.Timeline()

	// 1. NEGATIVTEST: Intermediate state where MediaIndex has accepted an ingest result
	// with a RAP at offset 188 and Epoch 1, but bytes have NOT yet been committed to packetStore (head == 0).
	r.mu.Lock()
	err := r.timelineIndex.ApplyIngestResult(mediafacts.ParseResult{
		Coverage: mediafacts.ParseCoverageComplete,
		Timing: mediafacts.TimingResult{
			Authority: mediafacts.TimingAuthorityCanonical,
			Records: []mediafacts.TimingRecord{
				{
					Type: mediafacts.TimingRecordTypeDiscontinuity,
					Discontinuity: mediafacts.DiscontinuityRecord{
						Scope:         mediafacts.DiscontinuityScopeProgram,
						ObservedAt:    0,
						HasEpochAfter: true,
						EpochAfter:    1,
					},
				},
				{
					Type: mediafacts.TimingRecordTypeRandomAccessPoint,
					RAP: mediafacts.TimingPoint{
						Epoch:      1,
						PID:        256,
						ObservedAt: 188,
						SubjectAt:  188,
						HasPTS:     true,
						PTS90k:     90000,
					},
				},
			},
		},
		Events: []mediafacts.Event{
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 188, Joinable: true},
		},
	})
	if err != nil {
		r.mu.Unlock()
		t.Fatalf("ApplyIngestResult failed: %v", err)
	}
	// head is still 0!
	if r.store.headOffset() != 0 {
		r.mu.Unlock()
		t.Fatalf("head = %d, want 0", r.store.headOffset())
	}
	r.mu.Unlock()

	// External TimelineReader query MUST NOT observe the uncommitted RAP or epoch!
	if rap, ok := tl.FindPrecedingRAP(math.MaxInt64); ok {
		t.Fatalf("NEGATIVTEST FAILED: Uncommitted RAP at %d observed before bytes committed (head=%d)", rap.Offset, r.Head())
	}
	if rap, ok := tl.FindFollowingRAP(0); ok {
		t.Fatalf("NEGATIVTEST FAILED: Uncommitted RAP at %d observed via FindFollowingRAP before bytes committed", rap.Offset)
	}
	if raps := tl.RAPsBetween(0, 1000); len(raps) != 0 {
		t.Fatalf("NEGATIVTEST FAILED: Uncommitted RAPs observed via RAPsBetween: %+v", raps)
	}
	if span, ok := tl.EpochForOffset(188); ok {
		t.Fatalf("NEGATIVTEST FAILED: Uncommitted Epoch observed via EpochForOffset: %+v", span)
	}

	// 2. POSITIVE COMMIT: Write bytes up to offset 376 (covers the RAP at 188).
	data := make([]byte, 2*TSPacketSize)
	for i := range data {
		data[i] = SyncByte
	}
	data[TSPacketSize+1] = 0x40 // PUSI on second packet (offset 188)

	r.mu.Lock()
	r.store.writeCommitted(data)
	r.mu.Unlock()

	if r.Head() != int64(2*TSPacketSize) {
		t.Fatalf("head = %d, want %d", r.Head(), 2*TSPacketSize)
	}

	// Now that bytes are in store, the RAP MUST be visible and readable
	rap, ok := tl.FindPrecedingRAP(math.MaxInt64)
	if !ok {
		t.Fatal("RAP at 188 not found after byte commit")
	}
	if rap.Offset != 188 {
		t.Fatalf("got RAP at %d, want 188", rap.Offset)
	}

	buf := make([]byte, TSPacketSize)
	n, _, err := r.ReadAt(buf, rap.Offset)
	if err != nil || n != TSPacketSize || buf[0] != SyncByte || buf[1]&0x40 == 0 {
		t.Fatalf("ReadAt bytes corrupted or failed: n=%d, err=%v, buf[0]=0x%02x, buf[1]=0x%02x", n, err, buf[0], buf[1])
	}

	// 3. NEGATIVTEST PRUNING: Advance tail past offset 188 without pruning timelineIndex
	// (simulating the intermediate pruning window where bytes are overwritten before index prune).
	r.mu.Lock()
	// Write enough packets to push tail past 188 in a 10-packet ring
	extraData := make([]byte, 12*TSPacketSize)
	for i := range extraData {
		extraData[i] = SyncByte
	}
	r.store.writeCommitted(extraData)
	if r.store.tailOffset() <= 188 {
		r.mu.Unlock()
		t.Fatalf("tail = %d, want > 188", r.store.tailOffset())
	}
	r.mu.Unlock()

	// Querying offset 188 MUST return not found because bytes are pruned!
	if rap, ok := tl.FindPrecedingRAP(188); ok {
		t.Fatalf("NEGATIVTEST PRUNING FAILED: Pruned RAP at %d observed after byte eviction (tail=%d)", rap.Offset, r.Tail())
	}
	if raps := tl.RAPsBetween(0, 188); len(raps) != 0 {
		t.Fatalf("NEGATIVTEST PRUNING FAILED: Pruned RAPs observed via RAPsBetween: %+v", raps)
	}
	if _, ok := tl.EpochForOffset(188); ok {
		t.Fatalf("NEGATIVTEST PRUNING FAILED: Pruned Epoch observed for offset 188 (tail=%d)", r.Tail())
	}
}
