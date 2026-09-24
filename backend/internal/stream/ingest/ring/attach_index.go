// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import "github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"

// attachIndex is a lightweight, derived join and recovery cache for subscribers.
//
// Invariant: attachIndex is NEVER a source of media truth. Canonical media and timing
// truth belongs exclusively to timeline.MediaIndex. attachIndex exists strictly to
// serve subscriber attach and overrun recovery in O(1) time without querying the
// timeline index for recent joinable access units.
//
// Invariant: attachIndex.generation is the ring's topology epoch counter and MUST
// NEVER be equated with or converted to timeline.TimelineEpoch.
type attachIndex struct {
	maxKeyframes          int
	keyframeOffsets       []int64
	generation            uint64
	generationResumeFloor int64
}

// newAttachIndex creates a new derived join cache with the specified capacity limit.
func newAttachIndex(maxKeyframes int) *attachIndex {
	if maxKeyframes < 1 {
		maxKeyframes = 1
	}
	return &attachIndex{
		maxKeyframes: maxKeyframes,
	}
}

// applyEvents processes events from an ingested chunk under the coordinator lock.
// It reports whether the stream generation advanced.
func (ai *attachIndex) applyEvents(events []mediafacts.Event) (generationChanged bool) {
	for _, ev := range events {
		switch ev.Kind {
		case mediafacts.EventProgramIdentityChanged:
			ai.keyframeOffsets = ai.keyframeOffsets[:0]
			ai.generation++
			generationChanged = true
		case mediafacts.EventRandomAccessPoint:
			if ev.Joinable {
				ai.keyframeOffsets = append(ai.keyframeOffsets, ev.Offset)
				if len(ai.keyframeOffsets) > ai.maxKeyframes {
					ai.keyframeOffsets = ai.keyframeOffsets[1:]
				}
			}
		case mediafacts.EventRandomAccessPointInvalidated:
			for i, off := range ai.keyframeOffsets {
				if off == ev.Offset {
					ai.keyframeOffsets = append(ai.keyframeOffsets[:i], ai.keyframeOffsets[i+1:]...)
					break
				}
			}
		default:
			// EventUnknown or unrecognized kinds are ignored for join caching.
		}
	}
	return generationChanged
}

// invalidateOnProgramChange invalidates join state when a control-plane program change
// resulted in an actual EventProgramIdentityChanged. It must NOT be called on no-op changes.
func (ai *attachIndex) invalidateOnProgramChange(currentHead int64) {
	ai.keyframeOffsets = ai.keyframeOffsets[:0]
	ai.generation++
	ai.generationResumeFloor = currentHead
}

// setGenerationResumeFloor records the recovery boundary established by an identity change.
func (ai *attachIndex) setGenerationResumeFloor(floor int64) {
	ai.generationResumeFloor = floor
}

// pruneBefore drops all indexed random access points that have fallen behind the buffer tail.
func (ai *attachIndex) pruneBefore(tail int64) {
	validIdx := -1
	for i, offset := range ai.keyframeOffsets {
		if offset >= tail {
			validIdx = i
			break
		}
	}
	if validIdx == -1 {
		ai.keyframeOffsets = ai.keyframeOffsets[:0]
	} else if validIdx > 0 {
		ai.keyframeOffsets = ai.keyframeOffsets[validIdx:]
	}
}

// latestKeyframeOffset returns the newest random access point still held by the buffer.
func (ai *attachIndex) latestKeyframeOffset(tail int64) (int64, bool) {
	if len(ai.keyframeOffsets) == 0 {
		return 0, false
	}
	latest := ai.keyframeOffsets[len(ai.keyframeOffsets)-1]
	if latest < tail {
		return 0, false
	}
	return latest, true
}

// keyframeOffsetsCopy returns a copy of the current random access offsets.
func (ai *attachIndex) keyframeOffsetsCopy() []int64 {
	out := make([]int64, len(ai.keyframeOffsets))
	copy(out, ai.keyframeOffsets)
	return out
}

func (ai *attachIndex) generationValue() uint64 {
	return ai.generation
}

func (ai *attachIndex) resumeFloor() int64 {
	return ai.generationResumeFloor
}
