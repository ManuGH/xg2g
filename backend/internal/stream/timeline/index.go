// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package timeline

import (
	"errors"
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

	// ErrAmbiguousTimingBinding is returned when multiple PES timing records claim the same SubjectAt
	// byte offset as a Random Access Point, preventing unambiguous binding.
	ErrAmbiguousTimingBinding = errors.New("ambiguous timing binding for random access point")

	// ErrInconsistentEpochTransition is returned when a canonical program discontinuity contradicts
	// the active epoch state in the index, preventing state repair or corrupted transitions.
	ErrInconsistentEpochTransition = errors.New("inconsistent canonical epoch transition")
)

// MediaIndex provides thread-safe, deterministic indexing and querying of canonical media facts.
// It acts as a single-writer, concurrent-reader index that records transport-stream RAPs,
// canonical PCR samples, PES timing points, discontinuities, and timeline epoch spans.
//
// Invariant: MediaIndex only stores, indexes, and queries canonical truth published by
// media-core via Protocol v6. It never unwraps timestamps, infers epochs, or repairs timing.
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

	// 1. Pre-validation: verify unambiguous PES timing binding for RAP events.
	pesBySubjectAt := make(map[int64][]mediafacts.TimingPoint)
	for _, rec := range res.Timing.Records {
		if rec.Type == mediafacts.TimingRecordTypePES {
			pesBySubjectAt[rec.PES.SubjectAt] = append(pesBySubjectAt[rec.PES.SubjectAt], rec.PES)
		}
	}

	for _, ev := range res.Events {
		if ev.Kind == mediafacts.EventRandomAccessPoint {
			if len(pesBySubjectAt[ev.Offset]) > 1 {
				return ErrAmbiguousTimingBinding
			}
		}
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	// 2. Pre-validation: verify program epoch transitions against current index state.
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
			if matches := pesBySubjectAt[ev.Offset]; len(matches) == 1 {
				pes := matches[0]
				rap.HasTimingBinding = true
				rap.Epoch = pes.Epoch
				rap.PID = pes.PID
				rap.HasPTS = pes.HasPTS
				rap.PTS90k = pes.PTS90k
				rap.HasDTS = pes.HasDTS
				rap.DTS90k = pes.DTS90k
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
			idx.rapsByPTS[epoch] = list[:len(list)-1]
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

	// 2. Prune PCRs
	pcrCut := 0
	for pcrCut < len(idx.pcrs) && idx.pcrs[pcrCut].ObservedAt < tailOffset {
		pcrCut++
	}
	if pcrCut > 0 {
		copy(idx.pcrs, idx.pcrs[pcrCut:])
		idx.pcrs = idx.pcrs[:len(idx.pcrs)-pcrCut]
	}

	// 3. Prune Timing Points
	tpCut := 0
	for tpCut < len(idx.timingPoints) && idx.timingPoints[tpCut].ObservedAt < tailOffset {
		tpCut++
	}
	if tpCut > 0 {
		copy(idx.timingPoints, idx.timingPoints[tpCut:])
		idx.timingPoints = idx.timingPoints[:len(idx.timingPoints)-tpCut]
	}

	// 4. Prune Discontinuities
	discCut := 0
	for discCut < len(idx.discontinuities) && idx.discontinuities[discCut].ObservedAt < tailOffset {
		discCut++
	}
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
