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

type customMockCore struct {
	mediafacts.Core
	ingestFn func(ctx context.Context, startOffset int64, data []byte) (mediafacts.ParseResult, error)
	targetFn func(ctx context.Context, progNum uint16) (mediafacts.ParseResult, error)
}

func (m customMockCore) Ingest(ctx context.Context, startOffset int64, data []byte) (mediafacts.ParseResult, error) {
	if m.ingestFn != nil {
		return m.ingestFn(ctx, startOffset, data)
	}
	return m.Core.Ingest(ctx, startOffset, data)
}

func (m customMockCore) SetTargetProgram(ctx context.Context, progNum uint16) (mediafacts.ParseResult, error) {
	if m.targetFn != nil {
		return m.targetFn(ctx, progNum)
	}
	return m.Core.SetTargetProgram(ctx, progNum)
}

func tsPacketChunk(numPackets int) []byte {
	buf := make([]byte, numPackets*TSPacketSize)
	for i := 0; i < numPackets; i++ {
		buf[i*TSPacketSize] = SyncByte
	}
	return buf
}

// TestLifecycle_NonCanonicalTimingErrorRetiresCoreWithoutMutation verifies that
// when a ring is configured with a canonical timeline index, any ingest result
// carrying non-canonical timing authority fails with ErrNonCanonicalTiming, retires
// the core immediately, and causes zero ring mutation (all-or-nothing commit).
func TestLifecycle_NonCanonicalTimingErrorRetiresCoreWithoutMutation(t *testing.T) {
	idx := timeline.NewMediaIndex()
	baseCore := mediafacts.NewGoCore(1)

	mock := customMockCore{
		Core: baseCore,
		ingestFn: func(ctx context.Context, startOffset int64, data []byte) (mediafacts.ParseResult, error) {
			return mediafacts.ParseResult{
				Coverage:               mediafacts.ParseCoverageComplete,
				ProcessedThroughOffset: startOffset + int64(len(data)),
				Timing: mediafacts.TimingResult{
					Authority: mediafacts.TimingAuthorityNone, // Non-canonical (e.g. GoCore)!
				},
				Events: []mediafacts.Event{
					{Kind: mediafacts.EventRandomAccessPoint, Offset: startOffset, Joinable: true},
				},
			}, nil
		},
	}

	r := NewMasterRingWithCore(10*TSPacketSize, mock, WithTimelineIndex(idx))
	data := tsPacketChunk(2)

	_, err := r.Push(context.Background(), data)
	if !errors.Is(err, timeline.ErrNonCanonicalTiming) {
		t.Fatalf("expected ErrNonCanonicalTiming, got: %v", err)
	}

	// Verify zero ring mutation
	if r.Head() != 0 {
		t.Errorf("head mutated: got %d, want 0", r.Head())
	}
	if r.Tail() != 0 {
		t.Errorf("tail mutated: got %d, want 0", r.Tail())
	}
	if r.Generation() != 0 {
		t.Errorf("generation mutated: got %d, want 0", r.Generation())
	}
	if len(r.KeyframeOffsets()) != 0 {
		t.Errorf("keyframeOffsets mutated: got %v", r.KeyframeOffsets())
	}

	// Verify core retired
	if !r.coreUnusable {
		t.Error("expected coreUnusable to be true")
	}

	// Verify timeline index unmutated
	stats := idx.Stats()
	if stats.TotalRAPs != 0 || stats.PCREntries != 0 {
		t.Errorf("timeline index mutated: %+v", stats)
	}

	// Subsequent Push fails closed immediately with ErrCoreUnusable
	_, err = r.Push(context.Background(), data)
	if !errors.Is(err, ErrCoreUnusable) {
		t.Fatalf("expected ErrCoreUnusable on subsequent Push, got: %v", err)
	}
}

// TestLifecycle_ProductionZap_NewPipelinePerTarget verifies that in production,
// channel switching constructs a new pipeline/ring for the new program target.
// Both rings maintain completely isolated MediaIndex instances and state.
func TestLifecycle_ProductionZap_NewPipelinePerTarget(t *testing.T) {
	idx1 := timeline.NewMediaIndex()
	idx2 := timeline.NewMediaIndex()

	core1 := customMockCore{
		Core: mediafacts.NewGoCore(1),
		ingestFn: func(ctx context.Context, startOffset int64, data []byte) (mediafacts.ParseResult, error) {
			return mediafacts.ParseResult{
				Coverage:               mediafacts.ParseCoverageComplete,
				ProcessedThroughOffset: startOffset + int64(len(data)),
				Timing: mediafacts.TimingResult{
					Authority: mediafacts.TimingAuthorityCanonical,
					Records: []mediafacts.TimingRecord{
						{
							Type: mediafacts.TimingRecordTypeDiscontinuity,
							Discontinuity: mediafacts.DiscontinuityRecord{
								Scope:         mediafacts.DiscontinuityScopeProgram,
								ObservedAt:    startOffset,
								HasEpochAfter: true,
								EpochAfter:    1,
							},
						},
						{
							Type: mediafacts.TimingRecordTypeRandomAccessPoint,
							RAP: mediafacts.TimingPoint{
								Epoch:      1,
								PID:        256,
								ObservedAt: startOffset,
								SubjectAt:  startOffset,
								HasPTS:     true,
								PTS90k:     90000,
							},
						},
					},
				},
				Events: []mediafacts.Event{
					{Kind: mediafacts.EventRandomAccessPoint, Offset: startOffset, Joinable: true},
				},
			}, nil
		},
	}

	core2 := customMockCore{
		Core: mediafacts.NewGoCore(2),
		ingestFn: func(ctx context.Context, startOffset int64, data []byte) (mediafacts.ParseResult, error) {
			return mediafacts.ParseResult{
				Coverage:               mediafacts.ParseCoverageComplete,
				ProcessedThroughOffset: startOffset + int64(len(data)),
				Timing: mediafacts.TimingResult{
					Authority: mediafacts.TimingAuthorityCanonical,
					Records: []mediafacts.TimingRecord{
						{
							Type: mediafacts.TimingRecordTypeDiscontinuity,
							Discontinuity: mediafacts.DiscontinuityRecord{
								Scope:         mediafacts.DiscontinuityScopeProgram,
								ObservedAt:    startOffset,
								HasEpochAfter: true,
								EpochAfter:    1,
							},
						},
						{
							Type: mediafacts.TimingRecordTypeRandomAccessPoint,
							RAP: mediafacts.TimingPoint{
								Epoch:      1,
								PID:        300,
								ObservedAt: startOffset,
								SubjectAt:  startOffset,
								HasPTS:     true,
								PTS90k:     180000,
							},
						},
					},
				},
				Events: []mediafacts.Event{
					{Kind: mediafacts.EventRandomAccessPoint, Offset: startOffset, Joinable: true},
				},
			}, nil
		},
	}

	ring1 := NewMasterRingWithCore(10*TSPacketSize, core1, WithTimelineIndex(idx1))
	ring2 := NewMasterRingWithCore(10*TSPacketSize, core2, WithTimelineIndex(idx2))

	data := tsPacketChunk(2)
	if _, err := ring1.Push(context.Background(), data); err != nil {
		t.Fatalf("ring1 push failed: %v", err)
	}
	if _, err := ring2.Push(context.Background(), data); err != nil {
		t.Fatalf("ring2 push failed: %v", err)
	}

	// Verify both rings have independent timelines
	stats1 := ring1.Timeline().Stats()
	stats2 := ring2.Timeline().Stats()

	if stats1.TotalRAPs != 1 || stats1.BoundRAPs != 1 {
		t.Fatalf("unexpected stats1: %+v", stats1)
	}
	if stats2.TotalRAPs != 1 || stats2.BoundRAPs != 1 {
		t.Fatalf("unexpected stats2: %+v", stats2)
	}

	rap1, _ := ring1.Timeline().FindFollowingRAP(0)
	rap2, _ := ring2.Timeline().FindFollowingRAP(0)

	if rap1.PID != 256 || rap2.PID != 300 {
		t.Fatalf("expected PIDs 256 and 300, got %d and %d", rap1.PID, rap2.PID)
	}
}

// TestLifecycle_ContractZap_SameRingZapLifecycle verifies the same-ring zap lifecycle contract:
// 1. Initial program running in Epoch 1
// 2. SetTargetProgram called: invalidates ring state, core prepares pending closing edge
// 3. First packet of next chunk emits closing edge closing Epoch 1 at first packet offset
// 4. New program starts in Epoch 2
// 5. MediaIndex transitions cleanly without ErrInconsistentEpochTransition
func TestLifecycle_ContractZap_SameRingZapLifecycle(t *testing.T) {
	idx := timeline.NewMediaIndex()

	chunkSize := int64(2 * TSPacketSize)
	var step int

	mock := customMockCore{
		Core: mediafacts.NewGoCore(1),
		targetFn: func(ctx context.Context, progNum uint16) (mediafacts.ParseResult, error) {
			step = 1
			return mediafacts.ParseResult{
				Coverage: mediafacts.ParseCoverageComplete,
				Events: []mediafacts.Event{
					{Kind: mediafacts.EventProgramIdentityChanged},
				},
			}, nil
		},
		ingestFn: func(ctx context.Context, startOffset int64, data []byte) (mediafacts.ParseResult, error) {
			if step == 0 {
				// Program 1 initial chunk
				return mediafacts.ParseResult{
					Coverage:               mediafacts.ParseCoverageComplete,
					ProcessedThroughOffset: startOffset + int64(len(data)),
					Timing: mediafacts.TimingResult{
						Authority: mediafacts.TimingAuthorityCanonical,
						Records: []mediafacts.TimingRecord{
							{
								Type: mediafacts.TimingRecordTypeDiscontinuity,
								Discontinuity: mediafacts.DiscontinuityRecord{
									Scope:         mediafacts.DiscontinuityScopeProgram,
									Reason:        mediafacts.DiscontinuityReasonProgramIdentityChanged,
									ObservedAt:    startOffset,
									HasEpochAfter: true,
									EpochAfter:    1,
								},
							},
							{
								Type: mediafacts.TimingRecordTypeRandomAccessPoint,
								RAP: mediafacts.TimingPoint{
									Epoch:      1,
									PID:        256,
									ObservedAt: startOffset,
									SubjectAt:  startOffset,
									HasPTS:     true,
									PTS90k:     90000,
								},
							},
						},
					},
					Events: []mediafacts.Event{
						{Kind: mediafacts.EventProgramIdentityChanged},
						{Kind: mediafacts.EventRandomAccessPoint, Offset: startOffset, Joinable: true},
					},
				}, nil
			}

			// Step 1: Program 2 first chunk arrives after zap.
			// Core publishes closing edge of Epoch 1 at startOffset, followed by Epoch 2 opening.
			return mediafacts.ParseResult{
				Coverage:               mediafacts.ParseCoverageComplete,
				ProcessedThroughOffset: startOffset + int64(len(data)),
				Timing: mediafacts.TimingResult{
					Authority: mediafacts.TimingAuthorityCanonical,
					Records: []mediafacts.TimingRecord{
						// Closing edge from Step 9b-1 fix
						{
							Type: mediafacts.TimingRecordTypeDiscontinuity,
							Discontinuity: mediafacts.DiscontinuityRecord{
								Scope:          mediafacts.DiscontinuityScopeProgram,
								Reason:         mediafacts.DiscontinuityReasonProgramIdentityChanged,
								ObservedAt:     startOffset,
								HasEpochBefore: true,
								EpochBefore:    1,
							},
						},
						// Opening edge of new program
						{
							Type: mediafacts.TimingRecordTypeDiscontinuity,
							Discontinuity: mediafacts.DiscontinuityRecord{
								Scope:         mediafacts.DiscontinuityScopeProgram,
								Reason:        mediafacts.DiscontinuityReasonProgramIdentityChanged,
								ObservedAt:    startOffset,
								HasEpochAfter: true,
								EpochAfter:    2,
							},
						},
						{
							Type: mediafacts.TimingRecordTypeRandomAccessPoint,
							RAP: mediafacts.TimingPoint{
								Epoch:      2,
								PID:        300,
								ObservedAt: startOffset,
								SubjectAt:  startOffset,
								HasPTS:     true,
								PTS90k:     180000,
							},
						},
					},
				},
				Events: []mediafacts.Event{
					{Kind: mediafacts.EventProgramIdentityChanged},
					{Kind: mediafacts.EventRandomAccessPoint, Offset: startOffset, Joinable: true},
				},
			}, nil
		},
	}

	r := NewMasterRingWithCore(20*TSPacketSize, mock, WithTimelineIndex(idx))
	data := tsPacketChunk(2)

	// Ingest program 1
	if _, err := r.Push(context.Background(), data); err != nil {
		t.Fatalf("program 1 push failed: %v", err)
	}

	activeEpoch, hasActive := r.Timeline().ActiveEpoch()
	if !hasActive || activeEpoch != 1 {
		t.Fatalf("expected active epoch 1, got %v (%d)", hasActive, activeEpoch)
	}

	// Trigger zap
	if err := r.SetTargetProgram(context.Background(), 2); err != nil {
		t.Fatalf("SetTargetProgram failed: %v", err)
	}

	// Ingest program 2 first chunk
	if _, err := r.Push(context.Background(), data); err != nil {
		t.Fatalf("program 2 push failed: %v", err)
	}

	// Verify clean epoch transition
	activeEpoch, hasActive = r.Timeline().ActiveEpoch()
	if !hasActive || activeEpoch != 2 {
		t.Fatalf("expected active epoch 2, got %v (%d)", hasActive, activeEpoch)
	}

	spans := r.Timeline().EpochSpans()
	if len(spans) != 2 {
		t.Fatalf("expected 2 epoch spans, got %d", len(spans))
	}
	if !spans[0].Closed || spans[0].EndOffset != chunkSize {
		t.Fatalf("expected epoch 1 closed at %d, got %+v", chunkSize, spans[0])
	}
	if spans[1].Closed || spans[1].StartOffset != chunkSize {
		t.Fatalf("expected epoch 2 open at %d, got %+v", chunkSize, spans[1])
	}

	stats := r.Timeline().Stats()
	if stats.TotalRAPs != 2 || stats.BoundRAPs != 2 || stats.BoundRAPRatio() != 1.0 {
		t.Fatalf("unexpected stats after zap: %+v", stats)
	}
}
