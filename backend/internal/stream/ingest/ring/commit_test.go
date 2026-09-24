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
			VideoPID:   256,
			VideoCodec: CodecH264,
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
				if rap.Offset+int64(TSPacketSize) > r.Head() {
					t.Errorf("INVARIANT VIOLATION: RAP at %d published in Timeline before bytes committed in store", rap.Offset)
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
