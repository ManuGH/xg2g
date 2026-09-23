// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"context"
	"errors"
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
	idx := timeline.NewMediaIndex()

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
	r := NewMasterRingWithCore(10*TSPacketSize, core, WithTimelineIndex(idx))
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
	idx := timeline.NewMediaIndex()

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
	r := NewMasterRingWithCore(10*TSPacketSize, core, WithTimelineIndex(idx))
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
