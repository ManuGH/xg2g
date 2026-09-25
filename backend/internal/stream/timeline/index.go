// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package timeline

import (
	"errors"
	"slices"
	"sort"
	"sync"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

var (
	// ErrNonCanonicalTiming is returned when a ParseResult does not carry canonical timing authority.
	// MediaIndex is strictly a canonical timeline index and fails closed on non-canonical input.
	ErrNonCanonicalTiming = errors.New("timeline index requires canonical timing")

	// ErrIncompleteCoverage is returned when a ParseResult does not carry ParseCoverageComplete.
	// MediaIndex requires complete parse coverage before committing stream truth.
	ErrIncompleteCoverage = errors.New("timeline index requires complete parse coverage")

	// ErrAmbiguousTimingBinding is returned when more than one RAP timing record in a result
	// binds the same Random Access Point offset.
	ErrAmbiguousTimingBinding = errors.New("ambiguous timing binding for random access point")

	// ErrUnmatchedTimingBinding is returned when a RAP timing record binds an offset for which the
	// same result carries no RandomAccessPoint event. media-core publishes a binding at the packet
	// that establishes the RAP, so the two always arrive together; one without the other is a
	// broken publication, not something to hold on to until its partner turns up.
	ErrUnmatchedTimingBinding = errors.New("timing binding without a random access point")

	// ErrInconsistentEpochTransition is returned when a canonical program discontinuity contradicts
	// the active epoch state in the index, preventing state repair or corrupted transitions.
	ErrInconsistentEpochTransition = errors.New("inconsistent canonical epoch transition")

	// ErrNonMonotonicObservedAt is returned when incoming timing records break non-decreasing
	// ObservedAt order, violating the precondition required for binary search and deterministic indexing.
	ErrNonMonotonicObservedAt = errors.New("non-monotonic observed_at in timing records")

	// ErrEventBeyondProcessedBytes is returned when an event or timing record refers to an offset
	// at or beyond ProcessedThroughOffset, violating "no truth without corresponding bytes".
	ErrEventBeyondProcessedBytes = errors.New("event or timing point beyond processed chunk bytes")
)

// MediaIndex provides thread-safe, deterministic indexing and querying of canonical media facts.
// It acts as a single-writer, concurrent-reader index that records transport-stream RAPs,
// canonical PCR samples, PES timing points, discontinuities, and timeline epoch spans.
//
// Invariant: MediaIndex only stores, indexes, and queries canonical truth published by
// media-core via Protocol v7. It never unwraps timestamps, infers epochs, repairs timing, or
// binds a RAP to time itself: a RAP is bound exactly when media-core published a RAP timing
// record for it.
type MediaIndex struct {
	mu sync.RWMutex

	// rapsByOffset maintains RAPs in strictly ascending transport Offset order.
	rapsByOffset []RAPEntry

	// rapsByPTS maintains RAPs per epoch, sorted by (PTS90k, Offset).
	// Only entries with HasPTS == true are tracked here.
	rapsByPTS map[mediafacts.TimelineEpoch][]RAPEntry

	// pcrs maintains canonical PCR samples in ascending ObservedAt order.
	pcrs []PCREntry

	// timingPoints maintains canonical PES timing points in ascending ObservedAt order.
	timingPoints []TimingPointEntry

	// discontinuities maintains discontinuity records in ascending ObservedAt order.
	discontinuities []DiscontinuityEntry

	// epochSpans maintains program epoch spans in chronological order.
	epochSpans []EpochSpan
}

// NewMediaIndex creates an empty canonical MediaIndex.
func NewMediaIndex() *MediaIndex {
	return &MediaIndex{
		rapsByPTS: make(map[mediafacts.TimelineEpoch][]RAPEntry),
	}
}

// ApplyIngestResult ingests one chunk's parse result into the canonical index.
// It accepts transport-plane results from Core.Ingest(...).
//
// If the result does not carry complete coverage or TimingAuthorityCanonical,
// it returns ErrIncompleteCoverage or ErrNonCanonicalTiming without mutating any index state (fail-closed).
func (idx *MediaIndex) ApplyIngestResult(res mediafacts.ParseResult) error {
	if res.Coverage != mediafacts.ParseCoverageComplete {
		return ErrIncompleteCoverage
	}
	if res.Timing.Authority != mediafacts.TimingAuthorityCanonical {
		return ErrNonCanonicalTiming
	}

	// 1. Pre-validation: "No truth without corresponding bytes".
	// All events and timing records must fall within ProcessedThroughOffset (when specified).
	for _, ev := range res.Events {
		if ev.Offset < 0 {
			return ErrEventBeyondProcessedBytes
		}
		if res.ProcessedThroughOffset > 0 {
			if ev.Kind == mediafacts.EventRandomAccessPoint {
				if ev.Offset+mediafacts.TSPacketSize > res.ProcessedThroughOffset {
					return ErrEventBeyondProcessedBytes
				}
			} else if ev.Offset > res.ProcessedThroughOffset {
				return ErrEventBeyondProcessedBytes
			}
		}
	}
	for _, rec := range res.Timing.Records {
		switch rec.Type {
		case mediafacts.TimingRecordTypeRandomAccessPoint:
			if rec.RAP.SubjectAt < 0 || rec.RAP.ObservedAt < 0 {
				return ErrEventBeyondProcessedBytes
			}
			if res.ProcessedThroughOffset > 0 {
				if rec.RAP.SubjectAt+mediafacts.TSPacketSize > res.ProcessedThroughOffset || rec.RAP.ObservedAt > res.ProcessedThroughOffset {
					return ErrEventBeyondProcessedBytes
				}
			}
		case mediafacts.TimingRecordTypePCR:
			if rec.PCR.ObservedAt < 0 {
				return ErrEventBeyondProcessedBytes
			}
			if res.ProcessedThroughOffset > 0 && rec.PCR.ObservedAt > res.ProcessedThroughOffset {
				return ErrEventBeyondProcessedBytes
			}
		case mediafacts.TimingRecordTypePES:
			if rec.PES.ObservedAt < 0 {
				return ErrEventBeyondProcessedBytes
			}
			if res.ProcessedThroughOffset > 0 && rec.PES.ObservedAt > res.ProcessedThroughOffset {
				return ErrEventBeyondProcessedBytes
			}
		case mediafacts.TimingRecordTypeDiscontinuity:
			if rec.Discontinuity.ObservedAt < 0 {
				return ErrEventBeyondProcessedBytes
			}
			if res.ProcessedThroughOffset > 0 && rec.Discontinuity.ObservedAt > res.ProcessedThroughOffset {
				return ErrEventBeyondProcessedBytes
			}
		}
	}

	// 2. Pre-validation: every RAP timing record binds exactly one RAP event of this result.
	rapEvents := make(map[int64]bool)
	for _, ev := range res.Events {
		if ev.Kind == mediafacts.EventRandomAccessPoint {
			rapEvents[ev.Offset] = true
		}
	}
	rapTiming := make(map[int64]mediafacts.TimingPoint)
	for _, rec := range res.Timing.Records {
		if rec.Type != mediafacts.TimingRecordTypeRandomAccessPoint {
			continue
		}
		if _, dup := rapTiming[rec.RAP.SubjectAt]; dup {
			return ErrAmbiguousTimingBinding
		}
		if !rapEvents[rec.RAP.SubjectAt] {
			return ErrUnmatchedTimingBinding
		}
		rapTiming[rec.RAP.SubjectAt] = rec.RAP
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	// 2. Pre-validation: verify non-decreasing ObservedAt / Offset order.
	// Binary search in PruneBefore requires that pcrs, timingPoints, and discontinuities
	// are maintained in non-decreasing ObservedAt order.
	hasLastPCR := len(idx.pcrs) > 0
	var lastPCRObs int64
	if hasLastPCR {
		lastPCRObs = idx.pcrs[len(idx.pcrs)-1].ObservedAt
	}

	hasLastPES := len(idx.timingPoints) > 0
	var lastPESObs int64
	if hasLastPES {
		lastPESObs = idx.timingPoints[len(idx.timingPoints)-1].ObservedAt
	}

	hasLastDisc := len(idx.discontinuities) > 0
	var lastDiscObs int64
	if hasLastDisc {
		lastDiscObs = idx.discontinuities[len(idx.discontinuities)-1].ObservedAt
	}

	for _, rec := range res.Timing.Records {
		switch rec.Type {
		case mediafacts.TimingRecordTypePCR:
			if hasLastPCR && rec.PCR.ObservedAt < lastPCRObs {
				return ErrNonMonotonicObservedAt
			}
			hasLastPCR = true
			lastPCRObs = rec.PCR.ObservedAt

		case mediafacts.TimingRecordTypePES:
			if hasLastPES && rec.PES.ObservedAt < lastPESObs {
				return ErrNonMonotonicObservedAt
			}
			hasLastPES = true
			lastPESObs = rec.PES.ObservedAt

		case mediafacts.TimingRecordTypeDiscontinuity:
			if hasLastDisc && rec.Discontinuity.ObservedAt < lastDiscObs {
				return ErrNonMonotonicObservedAt
			}
			hasLastDisc = true
			lastDiscObs = rec.Discontinuity.ObservedAt
		}
	}

	hasLastRAP := len(idx.rapsByOffset) > 0
	var lastRAPOffset int64
	if hasLastRAP {
		lastRAPOffset = idx.rapsByOffset[len(idx.rapsByOffset)-1].Offset
	}
	for _, ev := range res.Events {
		if ev.Kind == mediafacts.EventRandomAccessPoint {
			if hasLastRAP && ev.Offset < lastRAPOffset {
				return ErrNonMonotonicObservedAt
			}
			hasLastRAP = true
			lastRAPOffset = ev.Offset
		}
	}

	// 3. Pre-validation: verify program epoch transitions against current index state.
	// MediaIndex must never repair or guess epoch history on inconsistent transitions.
	var simActiveEpoch mediafacts.TimelineEpoch
	var simHasActive bool
	for i := len(idx.epochSpans) - 1; i >= 0; i-- {
		if !idx.epochSpans[i].Closed {
			simActiveEpoch = idx.epochSpans[i].Epoch
			simHasActive = true
			break
		}
	}

	for _, rec := range res.Timing.Records {
		if rec.Type == mediafacts.TimingRecordTypeDiscontinuity && rec.Discontinuity.Scope == mediafacts.DiscontinuityScopeProgram {
			d := rec.Discontinuity
			if !d.HasEpochBefore && !d.HasEpochAfter {
				continue
			}
			if !d.HasEpochBefore && d.HasEpochAfter {
				// Starting epoch without predecessor: valid only if no active epoch is open.
				if simHasActive {
					return ErrInconsistentEpochTransition
				}
				simHasActive = true
				simActiveEpoch = d.EpochAfter
				continue
			}
			if d.HasEpochBefore && d.HasEpochAfter {
				// Transition between epochs: predecessor must match the currently active epoch.
				if !simHasActive || simActiveEpoch != d.EpochBefore {
					return ErrInconsistentEpochTransition
				}
				if d.EpochBefore != d.EpochAfter {
					simActiveEpoch = d.EpochAfter
				}
				continue
			}
			if d.HasEpochBefore && !d.HasEpochAfter {
				// Closing active epoch: predecessor must match active epoch.
				if !simHasActive || simActiveEpoch != d.EpochBefore {
					return ErrInconsistentEpochTransition
				}
				simHasActive = false
				continue
			}
		}
	}

	// 3. Process Timing Records (Discontinuities, PCRs, PES points).
	for _, rec := range res.Timing.Records {
		switch rec.Type {
		case mediafacts.TimingRecordTypeDiscontinuity:
			entry := DiscontinuityEntry{
				Scope:          rec.Discontinuity.Scope,
				TrackPID:       rec.Discontinuity.TrackPID,
				Reason:         rec.Discontinuity.Reason,
				ObservedAt:     rec.Discontinuity.ObservedAt,
				HasEpochBefore: rec.Discontinuity.HasEpochBefore,
				EpochBefore:    rec.Discontinuity.EpochBefore,
				HasEpochAfter:  rec.Discontinuity.HasEpochAfter,
				EpochAfter:     rec.Discontinuity.EpochAfter,
			}
			idx.discontinuities = append(idx.discontinuities, entry)

			// Epoch boundaries are formed exclusively from Program-scope discontinuities.
			if rec.Discontinuity.Scope == mediafacts.DiscontinuityScopeProgram {
				idx.applyProgramDiscontinuityLocked(rec.Discontinuity)
			}

		case mediafacts.TimingRecordTypePCR:
			idx.pcrs = append(idx.pcrs, PCREntry{
				Epoch:          rec.PCR.Epoch,
				PCRPID:         rec.PCR.PCRPID,
				ObservedAt:     rec.PCR.ObservedAt,
				ExtendedPCR27m: rec.PCR.ExtendedPCR27m,
			})

		case mediafacts.TimingRecordTypePES:
			idx.timingPoints = append(idx.timingPoints, TimingPointEntry{
				Epoch:      rec.PES.Epoch,
				PID:        rec.PES.PID,
				HasPTS:     rec.PES.HasPTS,
				PTS90k:     rec.PES.PTS90k,
				HasDTS:     rec.PES.HasDTS,
				DTS90k:     rec.PES.DTS90k,
				ObservedAt: rec.PES.ObservedAt,
				SubjectAt:  rec.PES.SubjectAt,
			})
		}
	}

	// 4. Process Events (RAP additions & invalidations).
	for _, ev := range res.Events {
		switch ev.Kind {
		case mediafacts.EventRandomAccessPoint:
			rap := RAPEntry{
				Offset:   ev.Offset,
				Joinable: ev.Joinable,
			}
			if pt, ok := rapTiming[ev.Offset]; ok {
				rap.HasTimingBinding = true
				rap.Epoch = pt.Epoch
				rap.PID = pt.PID
				rap.HasPTS = pt.HasPTS
				rap.PTS90k = pt.PTS90k
				rap.HasDTS = pt.HasDTS
				rap.DTS90k = pt.DTS90k
			}
			idx.insertRAPLocked(rap)

		case mediafacts.EventRandomAccessPointInvalidated:
			idx.removeRAPLocked(ev.Offset)
		}
	}

	return nil
}

// applyProgramDiscontinuityLocked updates epoch spans based on program discontinuity rules:
// - before=None, after=None -> no epoch activated
// - before=None, after=E    -> Epoch E starts at ObservedAt
// - before=E1,   after=E2   -> E1 ends at ObservedAt, E2 starts at ObservedAt (if E1 != E2)
// - before=E,    after=E    -> no new program epoch
// - before=E1,   after=None -> E1 ends at ObservedAt
func (idx *MediaIndex) applyProgramDiscontinuityLocked(d mediafacts.DiscontinuityRecord) {
	if !d.HasEpochBefore && !d.HasEpochAfter {
		return
	}

	if d.HasEpochBefore && d.HasEpochAfter && d.EpochBefore == d.EpochAfter {
		// Same epoch before and after: no new program epoch.
		return
	}

	// Close open span for EpochBefore if present.
	if d.HasEpochBefore {
		for i := len(idx.epochSpans) - 1; i >= 0; i-- {
			if !idx.epochSpans[i].Closed && idx.epochSpans[i].Epoch == d.EpochBefore {
				idx.epochSpans[i].EndOffset = d.ObservedAt
				idx.epochSpans[i].Closed = true
				break
			}
		}
	}

	// Open span for EpochAfter if present.
	if d.HasEpochAfter {
		idx.epochSpans = append(idx.epochSpans, EpochSpan{
			Epoch:       d.EpochAfter,
			StartOffset: d.ObservedAt,
			Closed:      false,
		})
	}
}

// insertRAPLocked inserts or replaces a RAP entry in both offset and PTS indices.
func (idx *MediaIndex) insertRAPLocked(rap RAPEntry) {
	// 1. Insert into rapsByOffset
	n := len(idx.rapsByOffset)
	i := sort.Search(n, func(i int) bool {
		return idx.rapsByOffset[i].Offset >= rap.Offset
	})

	if i < n && idx.rapsByOffset[i].Offset == rap.Offset {
		// Existing RAP at this offset: remove from PTS index first if it had PTS.
		existing := idx.rapsByOffset[i]
		if existing.HasPTS {
			idx.removeRAPFromPTSLocked(existing.Epoch, existing.Offset)
		}
		idx.rapsByOffset[i] = rap
	} else {
		idx.rapsByOffset = append(idx.rapsByOffset, RAPEntry{})
		copy(idx.rapsByOffset[i+1:], idx.rapsByOffset[i:])
		idx.rapsByOffset[i] = rap
	}

	// 2. Insert into rapsByPTS if it carries PTS
	if rap.HasPTS {
		list := idx.rapsByPTS[rap.Epoch]
		m := len(list)
		pos := sort.Search(m, func(j int) bool {
			if list[j].PTS90k == rap.PTS90k {
				return list[j].Offset >= rap.Offset
			}
			return list[j].PTS90k > rap.PTS90k
		})
		list = append(list, RAPEntry{})
		copy(list[pos+1:], list[pos:])
		list[pos] = rap
		idx.rapsByPTS[rap.Epoch] = list
	}
}

// removeRAPLocked removes a RAP entry at offset from both offset and PTS indices.
func (idx *MediaIndex) removeRAPLocked(offset int64) {
	n := len(idx.rapsByOffset)
	i := sort.Search(n, func(i int) bool {
		return idx.rapsByOffset[i].Offset >= offset
	})
	if i < n && idx.rapsByOffset[i].Offset == offset {
		existing := idx.rapsByOffset[i]
		copy(idx.rapsByOffset[i:], idx.rapsByOffset[i+1:])
		idx.rapsByOffset = idx.rapsByOffset[:n-1]

		if existing.HasPTS {
			idx.removeRAPFromPTSLocked(existing.Epoch, offset)
		}
	}
}

// removeRAPFromPTSLocked removes a RAP from rapsByPTS for a specific epoch and offset.
func (idx *MediaIndex) removeRAPFromPTSLocked(epoch mediafacts.TimelineEpoch, offset int64) {
	list := idx.rapsByPTS[epoch]
	for j, entry := range list {
		if entry.Offset == offset {
			copy(list[j:], list[j+1:])
			list = list[:len(list)-1]
			if len(list) == 0 {
				delete(idx.rapsByPTS, epoch)
			} else {
				idx.rapsByPTS[epoch] = list
			}
			break
		}
	}
}

// FindPrecedingRAP finds the RAP with the greatest offset <= the requested offset.
func (idx *MediaIndex) FindPrecedingRAP(offset int64) (RAPEntry, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	n := len(idx.rapsByOffset)
	i := sort.Search(n, func(i int) bool {
		return idx.rapsByOffset[i].Offset > offset
	})
	if i == 0 {
		return RAPEntry{}, false
	}
	return idx.rapsByOffset[i-1], true
}

// FindFollowingRAP finds the RAP with the smallest offset >= the requested offset.
func (idx *MediaIndex) FindFollowingRAP(offset int64) (RAPEntry, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	n := len(idx.rapsByOffset)
	i := sort.Search(n, func(i int) bool {
		return idx.rapsByOffset[i].Offset >= offset
	})
	if i == n {
		return RAPEntry{}, false
	}
	return idx.rapsByOffset[i], true
}

// FindRAPPrecedingPTS finds the RAP in epoch with the greatest PTS90k <= the requested pts.
// If multiple RAPs have the same greatest PTS90k, the RAP with the greatest transport Offset is returned.
func (idx *MediaIndex) FindRAPPrecedingPTS(epoch mediafacts.TimelineEpoch, pts int64) (RAPEntry, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	list := idx.rapsByPTS[epoch]
	n := len(list)
	if n == 0 {
		return RAPEntry{}, false
	}

	i := sort.Search(n, func(i int) bool {
		return list[i].PTS90k > pts
	})
	if i == 0 {
		return RAPEntry{}, false
	}
	return list[i-1], true
}

// distancePTS computes |a - b| safely without signed int64 overflow.
func distancePTS(a, b int64) uint64 {
	if a == b {
		return 0
	}
	if (a >= 0 && b >= 0) || (a < 0 && b < 0) {
		if a > b {
			return uint64(a - b) // #nosec G115 -- a > b and same sign implies 0 < a - b <= math.MaxInt64
		}
		return uint64(b - a) // #nosec G115 -- b > a and same sign implies 0 < b - a <= math.MaxInt64
	}

	var pos, neg int64
	if a >= 0 {
		pos, neg = a, b
	} else {
		pos, neg = b, a
	}
	// pos >= 0 and neg < 0. Since neg <= -1, -(neg + 1) >= 0 and fits non-negative int64.
	return uint64(pos) + uint64(-(neg + 1)) + 1 // #nosec G115 -- pos >= 0 and -(neg + 1) >= 0 are non-negative
}

// FindRAPNearestPTS finds the RAP in epoch whose PTS90k is closest to the requested pts.
// If two different PTS values are equidistant from pts, the preceding PTS is chosen.
// If multiple RAPs share the winning PTS90k, the RAP with the greatest transport Offset is returned.
func (idx *MediaIndex) FindRAPNearestPTS(epoch mediafacts.TimelineEpoch, pts int64) (RAPEntry, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	list := idx.rapsByPTS[epoch]
	n := len(list)
	if n == 0 {
		return RAPEntry{}, false
	}

	i := sort.Search(n, func(i int) bool {
		return list[i].PTS90k > pts
	})

	if i == 0 {
		targetPTS := list[0].PTS90k
		last := sort.Search(n, func(j int) bool {
			return list[j].PTS90k > targetPTS
		})
		return list[last-1], true
	}
	if i == n {
		return list[n-1], true
	}

	targetAfterPTS := list[i].PTS90k
	lastAfter := sort.Search(n, func(j int) bool {
		return list[j].PTS90k > targetAfterPTS
	})
	afterEntry := list[lastAfter-1]

	distBefore := distancePTS(list[i-1].PTS90k, pts)
	distAfter := distancePTS(afterEntry.PTS90k, pts)

	if distBefore <= distAfter {
		return list[i-1], true
	}
	return afterEntry, true
}

// EpochForOffset returns the EpochSpan covering the given byte offset.
// For closed spans: StartOffset <= offset < EndOffset.
// For open spans: StartOffset <= offset.
func (idx *MediaIndex) EpochForOffset(offset int64) (EpochSpan, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	for i := len(idx.epochSpans) - 1; i >= 0; i-- {
		span := idx.epochSpans[i]
		if span.Closed {
			if span.StartOffset <= offset && offset < span.EndOffset {
				return span, true
			}
		} else {
			if span.StartOffset <= offset {
				return span, true
			}
		}
	}
	return EpochSpan{}, false
}

// RAPsBetween returns all RAP entries with startOffset <= Offset <= endOffset.
func (idx *MediaIndex) RAPsBetween(startOffset, endOffset int64) []RAPEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if startOffset > endOffset || len(idx.rapsByOffset) == 0 {
		return nil
	}

	n := len(idx.rapsByOffset)
	start := sort.Search(n, func(i int) bool {
		return idx.rapsByOffset[i].Offset >= startOffset
	})
	end := sort.Search(n, func(i int) bool {
		return idx.rapsByOffset[i].Offset > endOffset
	})

	if start >= end {
		return nil
	}

	result := make([]RAPEntry, end-start)
	copy(result, idx.rapsByOffset[start:end])
	return result
}

// DiscontinuitiesBetween returns all discontinuity entries with startOffset <= ObservedAt <= endOffset.
func (idx *MediaIndex) DiscontinuitiesBetween(startOffset, endOffset int64) []DiscontinuityEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if startOffset > endOffset || len(idx.discontinuities) == 0 {
		return nil
	}

	var result []DiscontinuityEntry
	for _, d := range idx.discontinuities {
		if d.ObservedAt >= startOffset && d.ObservedAt <= endOffset {
			result = append(result, d)
		}
	}
	return result
}

// PCREntries returns all indexed PCR entries.
func (idx *MediaIndex) PCREntries() []PCREntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	result := make([]PCREntry, len(idx.pcrs))
	copy(result, idx.pcrs)
	return result
}

// TimingPoints returns all indexed PES timing points.
func (idx *MediaIndex) TimingPoints() []TimingPointEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	result := make([]TimingPointEntry, len(idx.timingPoints))
	copy(result, idx.timingPoints)
	return result
}

// EpochSpans returns a copy of all recorded epoch spans.
func (idx *MediaIndex) EpochSpans() []EpochSpan {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	result := make([]EpochSpan, len(idx.epochSpans))
	copy(result, idx.epochSpans)
	return result
}

// PruneBefore removes entries whose transport relevance lies strictly before tailOffset.
//
// Invariant: Epoch metadata is never pruned so aggressively that remaining entries lose
// their epoch resolution. Any epoch span that overlaps tailOffset (or is open) is preserved.
func (idx *MediaIndex) PruneBefore(tailOffset int64) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// 1. Prune RAPs
	n := len(idx.rapsByOffset)
	rapCut := sort.Search(n, func(i int) bool {
		return idx.rapsByOffset[i].Offset >= tailOffset
	})
	if rapCut > 0 {
		for i := 0; i < rapCut; i++ {
			rap := idx.rapsByOffset[i]
			if rap.HasPTS {
				idx.removeRAPFromPTSLocked(rap.Epoch, rap.Offset)
			}
		}
		copy(idx.rapsByOffset, idx.rapsByOffset[rapCut:])
		idx.rapsByOffset = idx.rapsByOffset[:n-rapCut]
	}

	// 2. Prune PCRs (binary search is valid due to guarded non-decreasing ObservedAt invariant)
	pcrCut := sort.Search(len(idx.pcrs), func(i int) bool {
		return idx.pcrs[i].ObservedAt >= tailOffset
	})
	if pcrCut > 0 {
		copy(idx.pcrs, idx.pcrs[pcrCut:])
		idx.pcrs = idx.pcrs[:len(idx.pcrs)-pcrCut]
	}

	// 3. Prune Timing Points (binary search is valid due to guarded non-decreasing ObservedAt invariant)
	tpCut := sort.Search(len(idx.timingPoints), func(i int) bool {
		return idx.timingPoints[i].ObservedAt >= tailOffset
	})
	if tpCut > 0 {
		copy(idx.timingPoints, idx.timingPoints[tpCut:])
		idx.timingPoints = idx.timingPoints[:len(idx.timingPoints)-tpCut]
	}

	// 4. Prune Discontinuities (binary search is valid due to guarded non-decreasing ObservedAt invariant)
	discCut := sort.Search(len(idx.discontinuities), func(i int) bool {
		return idx.discontinuities[i].ObservedAt >= tailOffset
	})
	if discCut > 0 {
		copy(idx.discontinuities, idx.discontinuities[discCut:])
		idx.discontinuities = idx.discontinuities[:len(idx.discontinuities)-discCut]
	}

	// 5. Prune Epoch Spans:
	// Only remove closed spans whose EndOffset <= tailOffset.
	// Spans with EndOffset > tailOffset or Closed == false MUST be preserved.
	spanKeep := 0
	for i := 0; i < len(idx.epochSpans); i++ {
		span := idx.epochSpans[i]
		if !span.Closed || span.EndOffset > tailOffset {
			idx.epochSpans[spanKeep] = span
			spanKeep++
		}
	}
	idx.epochSpans = idx.epochSpans[:spanKeep]
}

// ActiveEpoch reports the currently open/active TimelineEpoch if one is tracked.
func (idx *MediaIndex) ActiveEpoch() (mediafacts.TimelineEpoch, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	for i := len(idx.epochSpans) - 1; i >= 0; i-- {
		if !idx.epochSpans[i].Closed {
			return idx.epochSpans[i].Epoch, true
		}
	}
	return 0, false
}

// Stats returns a point-in-time snapshot of index metrics.
func (idx *MediaIndex) Stats() TimelineStats {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	bound := 0
	for _, r := range idx.rapsByOffset {
		if r.HasTimingBinding {
			bound++
		}
	}

	return TimelineStats{
		TotalRAPs:    len(idx.rapsByOffset),
		BoundRAPs:    bound,
		EpochSpans:   len(idx.epochSpans),
		TimingPoints: len(idx.timingPoints),
		PCREntries:   len(idx.pcrs),
		EpochKeys:    len(idx.rapsByPTS),
	}
}

// PresentationTimeline computes presentation timing and RAP metrics for an epoch.
// PES points are evaluated using SubjectAt. Tracks are keyed by PID without inferred media types.
func (idx *MediaIndex) PresentationTimeline(epoch mediafacts.TimelineEpoch) (PresentationTimeline, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.presentationTimelineLocked(epoch)
}

func (idx *MediaIndex) presentationTimelineLocked(epoch mediafacts.TimelineEpoch) (PresentationTimeline, bool) {
	var span EpochSpan
	var foundSpan bool
	for _, s := range idx.epochSpans {
		if s.Epoch == epoch {
			span = s
			foundSpan = true
			break
		}
	}
	if !foundSpan {
		return PresentationTimeline{}, false
	}

	pt := PresentationTimeline{
		Epoch:       epoch,
		StartOffset: span.StartOffset,
		EndOffset:   span.EndOffset,
		Closed:      span.Closed,
	}

	// 1. Evaluate RAPs for this epoch
	for _, r := range idx.rapsByOffset {
		if r.Epoch == epoch {
			if !pt.HasFirstRAP {
				pt.HasFirstRAP = true
				pt.FirstRAPOffset = r.Offset
			}
			pt.HasLastRAP = true
			pt.LastRAPOffset = r.Offset
			pt.TotalRAPs++
			if r.Joinable {
				pt.JoinableRAPs++
			}
		}
	}

	// 2. Evaluate TimingPoints for this epoch, grouped strictly by PID
	tracksMap := make(map[uint16]*TrackPresentation)
	for _, tp := range idx.timingPoints {
		if tp.Epoch != epoch {
			continue
		}
		track, exists := tracksMap[tp.PID]
		if !exists {
			track = &TrackPresentation{
				PID: tp.PID,
			}
			tracksMap[tp.PID] = track
		}
		track.SampleCount++
		if tp.HasPTS {
			if !track.HasPTS {
				track.HasPTS = true
				track.EarliestPTS90k = tp.PTS90k
				track.LatestPTS90k = tp.PTS90k
			} else {
				if tp.PTS90k < track.EarliestPTS90k {
					track.EarliestPTS90k = tp.PTS90k
				}
				if tp.PTS90k > track.LatestPTS90k {
					track.LatestPTS90k = tp.PTS90k
				}
			}
		}
	}

	pids := make([]uint16, 0, len(tracksMap))
	for pid := range tracksMap {
		pids = append(pids, pid)
	}
	slices.Sort(pids)

	pt.Tracks = make([]TrackPresentation, 0, len(pids))
	for _, pid := range pids {
		t := *tracksMap[pid]
		if t.HasPTS && t.SampleCount >= 2 && t.LatestPTS90k >= t.EarliestPTS90k {
			t.ObservedSpan90k = t.LatestPTS90k - t.EarliestPTS90k
		}
		pt.Tracks = append(pt.Tracks, t)
	}

	// 3. Collect Discontinuities for this epoch
	for _, d := range idx.discontinuities {
		if d.EpochBefore == epoch || d.EpochAfter == epoch {
			pt.Discontinuities = append(pt.Discontinuities, d)
		}
	}

	return pt, true
}

// PresentationTimelines returns PresentationTimeline for all tracked epochs in chronological order.
func (idx *MediaIndex) PresentationTimelines() []PresentationTimeline {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	res := make([]PresentationTimeline, 0, len(idx.epochSpans))
	for _, span := range idx.epochSpans {
		if pt, ok := idx.presentationTimelineLocked(span.Epoch); ok {
			res = append(res, pt)
		}
	}
	return res
}

// FindRAPByTime finds a RAP in epoch matching target PTS subject to SeekOptions.
func (idx *MediaIndex) FindRAPByTime(epoch mediafacts.TimelineEpoch, pts int64, opts SeekOptions) (RAPEntry, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	raw := idx.rapsByPTS[epoch]
	if len(raw) == 0 {
		return RAPEntry{}, false
	}

	var list []RAPEntry
	if opts.JoinableOnly {
		list = make([]RAPEntry, 0, len(raw))
		for _, r := range raw {
			if r.Joinable {
				list = append(list, r)
			}
		}
	} else {
		list = raw
	}

	n := len(list)
	if n == 0 {
		return RAPEntry{}, false
	}

	switch opts.Mode {
	case SeekModePreceding:
		i := sort.Search(n, func(i int) bool {
			return list[i].PTS90k > pts
		})
		if i == 0 {
			return RAPEntry{}, false
		}
		return list[i-1], true

	case SeekModeFollowing:
		i := sort.Search(n, func(i int) bool {
			return list[i].PTS90k >= pts
		})
		if i == n {
			return RAPEntry{}, false
		}
		return list[i], true

	case SeekModeNearest:
		i := sort.Search(n, func(i int) bool {
			return list[i].PTS90k > pts
		})
		if i == 0 {
			targetPTS := list[0].PTS90k
			last := sort.Search(n, func(j int) bool {
				return list[j].PTS90k > targetPTS
			})
			return list[last-1], true
		}
		if i == n {
			return list[n-1], true
		}

		targetAfterPTS := list[i].PTS90k
		lastAfter := sort.Search(n, func(j int) bool {
			return list[j].PTS90k > targetAfterPTS
		})
		afterEntry := list[lastAfter-1]

		distBefore := distancePTS(list[i-1].PTS90k, pts)
		distAfter := distancePTS(afterEntry.PTS90k, pts)

		if distBefore <= distAfter {
			return list[i-1], true
		}
		return afterEntry, true

	default:
		return RAPEntry{}, false
	}
}
