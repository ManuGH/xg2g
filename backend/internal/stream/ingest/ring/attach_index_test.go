// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

func TestAttachIndex_JoinableRAPsAndBounding(t *testing.T) {
	ai := newAttachIndex(3) // limit to 3 keyframes
	if ai.generationValue() != 0 || ai.resumeFloor() != 0 {
		t.Fatalf("expected initial generation 0, got %d", ai.generationValue())
	}

	events := []mediafacts.Event{
		{Kind: mediafacts.EventRandomAccessPoint, Offset: 100, Joinable: true},
		{Kind: mediafacts.EventRandomAccessPoint, Offset: 200, Joinable: false}, // non-joinable -> skipped
		{Kind: mediafacts.EventRandomAccessPoint, Offset: 300, Joinable: true},
		{Kind: mediafacts.EventRandomAccessPoint, Offset: 400, Joinable: true},
		{Kind: mediafacts.EventRandomAccessPoint, Offset: 500, Joinable: true}, // exceeds 3 -> 100 evicted
	}

	genChanged := ai.applyEvents(events)
	if genChanged {
		t.Error("expected genChanged false for RAP events")
	}

	offsets := ai.keyframeOffsetsCopy()
	if len(offsets) != 3 {
		t.Fatalf("expected 3 offsets, got %d", len(offsets))
	}
	expected := []int64{300, 400, 500}
	for i, v := range expected {
		if offsets[i] != v {
			t.Errorf("offset[%d] = %d, want %d", i, offsets[i], v)
		}
	}

	latest, ok := ai.latestKeyframeOffset(0)
	if !ok || latest != 500 {
		t.Errorf("latestKeyframeOffset = %d (ok=%v), want 500", latest, ok)
	}
}

func TestAttachIndex_InvalidateRAP(t *testing.T) {
	ai := newAttachIndex(10)
	ai.applyEvents([]mediafacts.Event{
		{Kind: mediafacts.EventRandomAccessPoint, Offset: 100, Joinable: true},
		{Kind: mediafacts.EventRandomAccessPoint, Offset: 200, Joinable: true},
		{Kind: mediafacts.EventRandomAccessPoint, Offset: 300, Joinable: true},
	})

	// Invalidate offset 200
	ai.applyEvents([]mediafacts.Event{
		{Kind: mediafacts.EventRandomAccessPointInvalidated, Offset: 200},
	})

	offsets := ai.keyframeOffsetsCopy()
	if len(offsets) != 2 || offsets[0] != 100 || offsets[1] != 300 {
		t.Fatalf("unexpected offsets after invalidation: %v", offsets)
	}
}

func TestAttachIndex_ProgramIdentityChange(t *testing.T) {
	ai := newAttachIndex(10)
	ai.applyEvents([]mediafacts.Event{
		{Kind: mediafacts.EventRandomAccessPoint, Offset: 100, Joinable: true},
	})
	if ai.generationValue() != 0 {
		t.Fatalf("initial gen = %d", ai.generationValue())
	}

	genChanged := ai.applyEvents([]mediafacts.Event{
		{Kind: mediafacts.EventProgramIdentityChanged},
	})
	if !genChanged {
		t.Error("expected genChanged true")
	}
	if ai.generationValue() != 1 {
		t.Errorf("gen = %d, want 1", ai.generationValue())
	}
	if len(ai.keyframeOffsetsCopy()) != 0 {
		t.Errorf("expected keyframes cleared, got %v", ai.keyframeOffsetsCopy())
	}
}

func TestAttachIndex_PruneBefore(t *testing.T) {
	ai := newAttachIndex(10)
	ai.applyEvents([]mediafacts.Event{
		{Kind: mediafacts.EventRandomAccessPoint, Offset: 100, Joinable: true},
		{Kind: mediafacts.EventRandomAccessPoint, Offset: 200, Joinable: true},
		{Kind: mediafacts.EventRandomAccessPoint, Offset: 300, Joinable: true},
	})

	ai.pruneBefore(200) // 100 is < 200, pruned
	offsets := ai.keyframeOffsetsCopy()
	if len(offsets) != 2 || offsets[0] != 200 || offsets[1] != 300 {
		t.Fatalf("offsets after prune: %v", offsets)
	}

	ai.pruneBefore(400) // all < 400
	if len(ai.keyframeOffsetsCopy()) != 0 {
		t.Fatalf("expected all pruned, got %v", ai.keyframeOffsetsCopy())
	}
}

func TestAttachIndex_InvalidateOnProgramChange(t *testing.T) {
	ai := newAttachIndex(10)
	ai.applyEvents([]mediafacts.Event{
		{Kind: mediafacts.EventRandomAccessPoint, Offset: 100, Joinable: true},
	})

	ai.invalidateOnProgramChange(500)
	if ai.generationValue() != 1 {
		t.Errorf("generation = %d, want 1", ai.generationValue())
	}
	if ai.resumeFloor() != 500 {
		t.Errorf("resumeFloor = %d, want 500", ai.resumeFloor())
	}
	if len(ai.keyframeOffsetsCopy()) != 0 {
		t.Errorf("keyframes not cleared: %v", ai.keyframeOffsetsCopy())
	}
}
