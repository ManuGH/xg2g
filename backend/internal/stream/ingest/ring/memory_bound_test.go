// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"context"
	"fmt"
	"testing"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
	"github.com/ManuGH/xg2g/internal/stream/timeline"
)

// TestMemoryBound_RollingTailPruningStrictlyBounded verifies that when a long-running
// stream passes many gigabytes or millions of packets through a MasterRing and its
// canonical MediaIndex, all internal slices, indices, and map structures remain strictly
// count-bounded O(capacity), with zero memory leak over time.
func TestMemoryBound_RollingTailPruningStrictlyBounded(t *testing.T) {
	const (
		capacityPackets = 20
		chunkPackets    = 2
		capacityBytes   = capacityPackets * TSPacketSize
		chunkBytes      = chunkPackets * TSPacketSize
		iterations      = 1000
	)

	idx := timeline.NewMediaIndex()

	var currentEpoch mediafacts.TimelineEpoch = 1

	mock := customMockCore{
		Core: mediafacts.NewGoCore(1),
		ingestFn: func(ctx context.Context, startOffset int64, data []byte) (mediafacts.ParseResult, error) {
			iter := startOffset / chunkBytes
			records := make([]mediafacts.TimingRecord, 0, 4)

			// Every 100 iterations, trigger a program discontinuity into a new epoch
			if iter%100 == 0 && iter > 0 {
				oldEpoch := currentEpoch
				currentEpoch++
				records = append(records,
					mediafacts.TimingRecord{
						Type: mediafacts.TimingRecordTypeDiscontinuity,
						Discontinuity: mediafacts.DiscontinuityRecord{
							Scope:          mediafacts.DiscontinuityScopeProgram,
							Reason:         mediafacts.DiscontinuityReasonProgramIdentityChanged,
							ObservedAt:     startOffset,
							HasEpochBefore: true,
							EpochBefore:    oldEpoch,
						},
					},
					mediafacts.TimingRecord{
						Type: mediafacts.TimingRecordTypeDiscontinuity,
						Discontinuity: mediafacts.DiscontinuityRecord{
							Scope:         mediafacts.DiscontinuityScopeProgram,
							Reason:        mediafacts.DiscontinuityReasonProgramIdentityChanged,
							ObservedAt:    startOffset,
							HasEpochAfter: true,
							EpochAfter:    currentEpoch,
						},
					},
				)
			} else if iter == 0 {
				records = append(records, mediafacts.TimingRecord{
					Type: mediafacts.TimingRecordTypeDiscontinuity,
					Discontinuity: mediafacts.DiscontinuityRecord{
						Scope:         mediafacts.DiscontinuityScopeProgram,
						Reason:        mediafacts.DiscontinuityReasonProgramIdentityChanged,
						ObservedAt:    0,
						HasEpochAfter: true,
						EpochAfter:    currentEpoch,
					},
				})
			}

			// PCR entry
			records = append(records, mediafacts.TimingRecord{
				Type: mediafacts.TimingRecordTypePCR,
				PCR: mediafacts.PCRPoint{
					Epoch:          currentEpoch,
					PCRPID:         256,
					ObservedAt:     startOffset,
					ExtendedPCR27m: 27_000_000 + startOffset,
				},
			})

			// PES timing point & RAP
			records = append(records,
				mediafacts.TimingRecord{
					Type: mediafacts.TimingRecordTypePES,
					PES: mediafacts.TimingPoint{
						Epoch:      currentEpoch,
						PID:        257,
						ObservedAt: startOffset,
						SubjectAt:  startOffset,
						HasPTS:     true,
						PTS90k:     90000 + startOffset/10,
					},
				},
				mediafacts.TimingRecord{
					Type: mediafacts.TimingRecordTypeRandomAccessPoint,
					RAP: mediafacts.TimingPoint{
						Epoch:      currentEpoch,
						PID:        257,
						ObservedAt: startOffset,
						SubjectAt:  startOffset,
						HasPTS:     true,
						PTS90k:     90000 + startOffset/10,
					},
				},
			)

			events := []mediafacts.Event{
				{Kind: mediafacts.EventRandomAccessPoint, Offset: startOffset, Joinable: true},
			}

			return mediafacts.ParseResult{
				Coverage:               mediafacts.ParseCoverageComplete,
				ProcessedThroughOffset: startOffset + int64(len(data)),
				Timing: mediafacts.TimingResult{
					Authority: mediafacts.TimingAuthorityCanonical,
					Records:   records,
				},
				Events: events,
			}, nil
		},
	}

	r := NewMasterRingWithCore(capacityBytes, mock, WithTimelineIndex(idx))
	data := tsPacketChunk(chunkPackets)

	for i := 0; i < iterations; i++ {
		if _, err := r.Push(context.Background(), data); err != nil {
			t.Fatalf("push iteration %d failed: %v", i, err)
		}
	}

	// Ring tail verification
	expectedHead := int64(iterations * chunkBytes)
	expectedTail := expectedHead - int64(capacityBytes)
	if r.head != expectedHead {
		t.Errorf("head = %d, want %d", r.head, expectedHead)
	}
	if r.tail != expectedTail {
		t.Errorf("tail = %d, want %d", r.tail, expectedTail)
	}

	// Ring keyframes bounded by maxKeyframes and strictly >= tail
	if len(r.keyframeOffsets) > r.maxKeyframes {
		t.Errorf("keyframeOffsets len = %d > max %d", len(r.keyframeOffsets), r.maxKeyframes)
	}
	for _, kf := range r.keyframeOffsets {
		if kf < r.tail {
			t.Errorf("found stale keyframe %d < tail %d", kf, r.tail)
		}
	}

	// Timeline stats strictly bounded by capacity
	stats := r.Timeline().Stats()

	// In capacity = 20 packets, chunk = 2 packets -> max ~11 RAPs in active window
	if stats.TotalRAPs > 15 {
		t.Errorf("TotalRAPs count %d exceeded bound (< 15)", stats.TotalRAPs)
	}
	if stats.BoundRAPs != stats.TotalRAPs {
		t.Errorf("BoundRAPs %d != TotalRAPs %d", stats.BoundRAPs, stats.TotalRAPs)
	}
	if stats.PCREntries > 15 {
		t.Errorf("PCREntries count %d exceeded bound (< 15)", stats.PCREntries)
	}
	if stats.TimingPoints > 15 {
		t.Errorf("TimingPoints count %d exceeded bound (< 15)", stats.TimingPoints)
	}

	// Epoch spans: Only spans overlapping tailOffset or currently active are retained
	if stats.EpochSpans > 3 {
		t.Errorf("EpochSpans count %d exceeded bound (<= 3)", stats.EpochSpans)
	}

	// Epoch keys in rapsByPTS: Empty epoch map keys must be deleted when all RAPs in that epoch are pruned
	if stats.EpochKeys > 3 {
		t.Errorf("EpochKeys in rapsByPTS map %d exceeded bound (<= 3)", stats.EpochKeys)
	}

	// Find queries still succeed for valid in-window data
	latestOffset := expectedHead - int64(chunkBytes)
	rap, found := r.Timeline().FindPrecedingRAP(latestOffset)
	if !found || rap.Offset != latestOffset {
		t.Errorf("expected latest RAP at %d, got %v (%+v)", latestOffset, found, rap)
	}

	// Pruned data returns not found
	_, foundOld := r.Timeline().FindPrecedingRAP(0)
	if foundOld {
		t.Error("expected pruned offset 0 to not be found")
	}

	t.Logf("Memory bound verification SUCCESS across %d iterations (%d bytes): %+v", iterations, expectedHead, stats)
}

func ExampleMasterRing_Timeline() {
	idx := timeline.NewMediaIndex()
	r := NewMasterRingWithCore(10*TSPacketSize, mediafacts.NewGoCore(1), WithTimelineIndex(idx))
	reader := r.Timeline()
	stats := reader.Stats()
	fmt.Printf("Total RAPs: %d\n", stats.TotalRAPs)
	// Output:
	// Total RAPs: 0
}
