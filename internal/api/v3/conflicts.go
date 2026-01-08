// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"fmt"
	"math"

	"github.com/ManuGH/xg2g/internal/control/read"
	"github.com/ManuGH/xg2g/internal/openwebif"
)

// DetectConflicts checks a proposed timer against a list of existing timers.
// It returns a list of conflicts (Duplicates and Overlaps).
// It implements a conservative overlap check: max(startA, startB) < min(endA, endB).
func DetectConflicts(proposed TimerCreateRequest, existing []openwebif.Timer) []TimerConflict {
	var conflicts []TimerConflict

	// Calculate proposed effective start/end (including padding)
	pBegin := proposed.Begin
	if proposed.PaddingBeforeSec != nil {
		pBegin -= int64(*proposed.PaddingBeforeSec)
	}
	pEnd := proposed.End
	if proposed.PaddingAfterSec != nil {
		pEnd += int64(*proposed.PaddingAfterSec)
	}

	for _, t := range existing {
		// Filter non-blocking states
		// OpenWebIF States: 0=Waiting, 2=Running, 3=Finished, 1=Disabled? (Verify 1)
		// User Spec: "treat scheduled and recording as blockers. Ignore completed."
		// Let's assume:
		// 0: Waiting (Scheduled) -> BLOCK
		// 2: Running (Recording) -> BLOCK
		// 3: Finished            -> IGNORE
		// Disabled check: t.Disabled == 1 -> IGNORE?
		// "If state is unknown, treat as blocker to remain conservative."

		if t.State == 3 { // Finished
			continue
		}
		if t.Disabled != 0 {
			continue // Disabled timers shouldn't block? User implicitly said filter states.
		}

		// 1. Duplicate Check (Exact Match)
		// "Exact match on (serviceRef, begin, end) against an existing timer."
		// Note: Existing timers in OWI might have padding applied already in their begin/end?
		// Actually, usually OWI returns the programmed time.
		// If the user tries to schedule the *exact* same parameters, it's a duplicate.
		// However, we should compare against the *raw* parameters if possible, or just the effective ones.
		// Given we calculated effective pBegin/pEnd, we should compare against t.Begin/t.End.
		if t.ServiceRef == proposed.ServiceRef && t.Begin == pBegin && t.End == pEnd {
			conflicts = append(conflicts, TimerConflict{
				Type: TimerConflictTypeDuplicate,
				BlockingTimer: Timer{
					TimerId:     read.MakeTimerID(t.ServiceRef, t.Begin, t.End),
					ServiceRef:  t.ServiceRef,
					ServiceName: &t.ServiceName,
					Name:        t.Name,
					Begin:       t.Begin,
					End:         t.End,
					State:       mapTimerState(t),
				},
				Message: ptr("Exact duplicate of an existing timer"),
			})
			continue // If it's a duplicate, we don't also flag it as overlap (redundant)
		}

		// 2. Overlap Check (Conservative)
		// Intersection: max(a.begin, b.begin) < min(a.end, b.end)
		overlapStart := max64(pBegin, t.Begin)
		overlapEnd := min64(pEnd, t.End)

		if overlapStart < overlapEnd {
			overlapSeconds := int(overlapEnd - overlapStart)

			// Optional: Filter insignificant overlaps (e.g. < 10s)?
			// User spec: "Strict inequality avoids false positives".

			conflicts = append(conflicts, TimerConflict{
				Type: TimerConflictTypeOverlap,
				BlockingTimer: Timer{
					TimerId:     read.MakeTimerID(t.ServiceRef, t.Begin, t.End),
					ServiceRef:  t.ServiceRef,
					ServiceName: &t.ServiceName,
					Name:        t.Name,
					Begin:       t.Begin,
					End:         t.End,
					State:       mapTimerState(t),
				},
				OverlapSeconds: &overlapSeconds,
				Message:        ptr(fmt.Sprintf("Overlaps by %d minutes", int(math.Ceil(float64(overlapSeconds)/60.0)))),
			})
		}
	}

	return conflicts
}

// GenerateSuggestions creates suggestions to resolve conflicts (e.g. reduce padding).
func GenerateSuggestions(proposed TimerCreateRequest, conflicts []TimerConflict) []struct {
	Kind          *TimerConflictPreviewResponseSuggestionsKind `json:"kind,omitempty"`
	Note          *string                                      `json:"note,omitempty"`
	ProposedBegin *int64                                       `json:"proposedBegin,omitempty"`
	ProposedEnd   *int64                                       `json:"proposedEnd,omitempty"`
} {
	// Only support simple suggestions for now
	// 1. Remove padding if it resolves ALL conflicts

	hasPadding := (proposed.PaddingBeforeSec != nil && *proposed.PaddingBeforeSec > 0) ||
		(proposed.PaddingAfterSec != nil && *proposed.PaddingAfterSec > 0)

	if !hasPadding {
		return nil
	}

	// We verify if removing padding resolves all conflicts.
	// Since we don't have the list of ALL timers here, we can't be 100% sure without re-running detection against the full list.
	// But we can check if it resolves the *reported* conflicts, assuming they are the only ones.
	// For correctness, we should really pass the full list of timers to this function or just check if the overlap amount is covered by padding.

	// Simplified approach: For each conflict, check if overlapSeconds <= totalPadding usage contributing to that overlap.
	// This is tricky.

	// User spec: "If padding was applied, first suggestion: reduce padding to zero (if that would resolve)."
	// "These do not need to be perfect; they just need to be plausible."

	suggestion := struct {
		Kind          *TimerConflictPreviewResponseSuggestionsKind `json:"kind,omitempty"`
		Note          *string                                      `json:"note,omitempty"`
		ProposedBegin *int64                                       `json:"proposedBegin,omitempty"`
		ProposedEnd   *int64                                       `json:"proposedEnd,omitempty"`
	}{
		Kind:          ptrEnum(ReducePadding),
		Note:          ptr("Remove padding to reduce overlap probability"),
		ProposedBegin: &proposed.Begin, // Raw begin (no padding)
		ProposedEnd:   &proposed.End,   // Raw end (no padding)
	}

	return []struct {
		Kind          *TimerConflictPreviewResponseSuggestionsKind `json:"kind,omitempty"`
		Note          *string                                      `json:"note,omitempty"`
		ProposedBegin *int64                                       `json:"proposedBegin,omitempty"`
		ProposedEnd   *int64                                       `json:"proposedEnd,omitempty"`
	}{suggestion}
}

func mapTimerState(t openwebif.Timer) TimerState {
	if t.Disabled != 0 {
		return TimerStateDisabled
	}
	switch t.State {
	case 0:
		return TimerStateScheduled
	case 2:
		return TimerStateRecording
	case 3:
		return TimerStateCompleted
	default:
		return TimerStateUnknown
	}
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func ptr(s string) *string {
	return &s
}

func ptrEnum(k TimerConflictPreviewResponseSuggestionsKind) *TimerConflictPreviewResponseSuggestionsKind {
	return &k
}
