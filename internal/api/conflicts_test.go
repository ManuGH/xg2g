package api

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/openwebif"
)

func TestDetectConflicts(t *testing.T) {
	// Base proposed timer: 10:00 - 11:00
	baseReq := TimerCreateRequest{
		ServiceRef: "REF:1",
		Begin:      3600,
		End:        7200,
		Name:       "Proposed Show",
	}

	tests := []struct {
		name     string
		proposed TimerCreateRequest
		existing []openwebif.Timer
		wantType []TimerConflictType // Expected conflict types in order
	}{
		{
			name:     "No Overlap - Clean",
			proposed: baseReq,
			existing: []openwebif.Timer{
				{ServiceRef: "REF:1", Begin: 1000, End: 3000, Name: "Early Show", State: 0},
				{ServiceRef: "REF:1", Begin: 7300, End: 8000, Name: "Late Show", State: 0},
			},
			wantType: nil,
		},
		{
			name:     "No Overlap - Boundary Touching",
			proposed: baseReq,
			existing: []openwebif.Timer{
				{ServiceRef: "REF:1", Begin: 3000, End: 3600, Name: "Ends exactly at start", State: 0},
				{ServiceRef: "REF:1", Begin: 7200, End: 8000, Name: "Starts exactly at end", State: 0},
			},
			wantType: nil,
		},
		{
			name:     "Exact Duplicate",
			proposed: baseReq,
			existing: []openwebif.Timer{
				{ServiceRef: "REF:1", Begin: 3600, End: 7200, Name: "Same Show", State: 0},
			},
			wantType: []TimerConflictType{TimerConflictTypeDuplicate},
		},
		{
			name:     "Partial Overlap - Start",
			proposed: baseReq,
			existing: []openwebif.Timer{
				{ServiceRef: "REF:1", Begin: 3000, End: 4000, Name: "Overlaps Start", State: 0},
			},
			wantType: []TimerConflictType{TimerConflictTypeOverlap},
		},
		{
			name:     "Partial Overlap - End",
			proposed: baseReq,
			existing: []openwebif.Timer{
				{ServiceRef: "REF:1", Begin: 7000, End: 8000, Name: "Overlaps End", State: 0},
			},
			wantType: []TimerConflictType{TimerConflictTypeOverlap},
		},
		{
			name:     "Full Containment",
			proposed: baseReq,
			existing: []openwebif.Timer{
				{ServiceRef: "REF:1", Begin: 2000, End: 8000, Name: "Long Show", State: 0},
			},
			wantType: []TimerConflictType{TimerConflictTypeOverlap},
		},
		{
			name:     "Contained Within",
			proposed: baseReq,
			existing: []openwebif.Timer{
				{ServiceRef: "REF:1", Begin: 4000, End: 5000, Name: "Short Show", State: 0},
			},
			wantType: []TimerConflictType{TimerConflictTypeOverlap},
		},
		{
			name: "With Padding - Causes Overlap",
			proposed: func() TimerCreateRequest {
				r := baseReq
				pad := 600 // 10 mins
				r.PaddingBeforeSec = &pad
				return r
			}(),
			existing: []openwebif.Timer{
				{ServiceRef: "REF:1", Begin: 3000, End: 3500, Name: "Safe without padding", State: 0},
			},
			// Proposed Start becomes 3000. Overlap with 3000-3500?
			// pBegin=3000, pEnd=7200. Existing=3000-3500.
			// Overlap: max(3000,3000) < min(7200,3500) -> 3000 < 3500 -> Yes.
			wantType: []TimerConflictType{TimerConflictTypeOverlap},
		},
		{
			name:     "Ignore Finished/Disabled",
			proposed: baseReq,
			existing: []openwebif.Timer{
				{ServiceRef: "REF:1", Begin: 3600, End: 7200, Name: "Finished", State: 3},
				{ServiceRef: "REF:1", Begin: 3600, End: 7200, Name: "Disabled", State: 0, Disabled: 1},
			},
			wantType: nil,
		},
		{
			name:     "Different ServiceRef - No Conflict (One Tuner Assumption checked elsewhere?)",
			proposed: baseReq,
			existing: []openwebif.Timer{
				{ServiceRef: "REF:2", Begin: 3600, End: 7200, Name: "Other Channel", State: 0},
			},
			// Current implementation only checks overlaps on the existing list provided.
			// Wait, the logic I wrote checks ALL existing timers regardless of serviceRef?
			// "If t.serviceRef == proposed.serviceRef" for DUPLICATE.
			// But for match, do we block on other services?
			// User spec: "Overlap = Time interval intersection with ANY existing timer"
			// Wait, really? Usually simple single-tuner logic blocks everything.
			// But Enigma2 is multi-tuner.
			// If I implement "time interval intersection with ANY existing timer", I am assuming SINGLE TUNER model.
			// User said: "On FBC setups where 'free tuners' is not reliably inferable ... For v2.0, time overlap is a truthful, conservative signal."
			// Conservative means: assume single tuner? Or assume conflict if time matches?
			// "Overlap conflict (conservative): Time interval intersection with any existing timer"
			// This suggests we SHOULD flag it even if ServiceRef differs, because we don't know if we have enough tuners.
			// So yes, different ServiceRef SHOULD overlap in Conservative mode.
			wantType: []TimerConflictType{TimerConflictTypeOverlap},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectConflicts(tt.proposed, tt.existing)

			if len(got) != len(tt.wantType) {
				t.Errorf("DetectConflicts() count = %d, want %d", len(got), len(tt.wantType))
				for i, c := range got {
					t.Logf("Got conflict %d: Type=%v Msg=%v", i, c.Type, *c.Message)
				}
				return
			}

			for i, conflict := range got {
				if conflict.Type != tt.wantType[i] {
					t.Errorf("Conflict[%d] type = %v, want %v", i, conflict.Type, tt.wantType[i])
				}
			}
		})
	}
}
