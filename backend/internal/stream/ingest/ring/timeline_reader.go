// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
	"github.com/ManuGH/xg2g/internal/stream/timeline"
)

// ringTimelineReader wraps timeline queries under MasterRing.mu so that timeline truth
// and packet store byte availability share the exact same publication lock.
//
// Invariant: No consumer can observe a Random Access Point or timeline epoch before its
// bytes are committed to the packet store, nor observe a pruned RAP after its bytes have
// been overwritten. All queries are strictly synchronized with MasterRing commits.
type ringTimelineReader struct {
	ring *MasterRing
}

func (tr *ringTimelineReader) ActiveEpoch() (mediafacts.TimelineEpoch, bool) {
	tr.ring.mu.Lock()
	defer tr.ring.mu.Unlock()
	if tr.ring.timelineIndex == nil {
		return 0, false
	}
	return tr.ring.timelineIndex.ActiveEpoch()
}

func (tr *ringTimelineReader) FindPrecedingRAP(offset int64) (timeline.RAPEntry, bool) {
	tr.ring.mu.Lock()
	defer tr.ring.mu.Unlock()
	if tr.ring.timelineIndex == nil {
		return timeline.RAPEntry{}, false
	}
	rap, ok := tr.ring.timelineIndex.FindPrecedingRAP(offset)
	if !ok {
		return timeline.RAPEntry{}, false
	}
	if rap.Offset < tr.ring.store.tailOffset() || rap.Offset > tr.ring.store.headOffset() {
		return timeline.RAPEntry{}, false
	}
	return rap, true
}

func (tr *ringTimelineReader) FindFollowingRAP(offset int64) (timeline.RAPEntry, bool) {
	tr.ring.mu.Lock()
	defer tr.ring.mu.Unlock()
	if tr.ring.timelineIndex == nil {
		return timeline.RAPEntry{}, false
	}
	rap, ok := tr.ring.timelineIndex.FindFollowingRAP(offset)
	if !ok {
		return timeline.RAPEntry{}, false
	}
	if rap.Offset < tr.ring.store.tailOffset() || rap.Offset > tr.ring.store.headOffset() {
		return timeline.RAPEntry{}, false
	}
	return rap, true
}

func (tr *ringTimelineReader) FindRAPPrecedingPTS(epoch mediafacts.TimelineEpoch, pts int64) (timeline.RAPEntry, bool) {
	tr.ring.mu.Lock()
	defer tr.ring.mu.Unlock()
	if tr.ring.timelineIndex == nil {
		return timeline.RAPEntry{}, false
	}
	rap, ok := tr.ring.timelineIndex.FindRAPPrecedingPTS(epoch, pts)
	if !ok {
		return timeline.RAPEntry{}, false
	}
	if rap.Offset < tr.ring.store.tailOffset() || rap.Offset > tr.ring.store.headOffset() {
		return timeline.RAPEntry{}, false
	}
	return rap, true
}

func (tr *ringTimelineReader) FindRAPNearestPTS(epoch mediafacts.TimelineEpoch, pts int64) (timeline.RAPEntry, bool) {
	tr.ring.mu.Lock()
	defer tr.ring.mu.Unlock()
	if tr.ring.timelineIndex == nil {
		return timeline.RAPEntry{}, false
	}
	rap, ok := tr.ring.timelineIndex.FindRAPNearestPTS(epoch, pts)
	if !ok {
		return timeline.RAPEntry{}, false
	}
	if rap.Offset < tr.ring.store.tailOffset() || rap.Offset > tr.ring.store.headOffset() {
		return timeline.RAPEntry{}, false
	}
	return rap, true
}

func (tr *ringTimelineReader) EpochForOffset(offset int64) (timeline.EpochSpan, bool) {
	tr.ring.mu.Lock()
	defer tr.ring.mu.Unlock()
	if tr.ring.timelineIndex == nil {
		return timeline.EpochSpan{}, false
	}
	tail := tr.ring.store.tailOffset()
	head := tr.ring.store.headOffset()
	if offset < tail || offset > head {
		return timeline.EpochSpan{}, false
	}
	return tr.ring.timelineIndex.EpochForOffset(offset)
}

func (tr *ringTimelineReader) RAPsBetween(startOffset, endOffset int64) []timeline.RAPEntry {
	tr.ring.mu.Lock()
	defer tr.ring.mu.Unlock()
	if tr.ring.timelineIndex == nil {
		return nil
	}
	tail := tr.ring.store.tailOffset()
	head := tr.ring.store.headOffset()
	if startOffset < tail {
		startOffset = tail
	}
	if endOffset > head {
		endOffset = head
	}
	if startOffset > endOffset {
		return nil
	}
	return tr.ring.timelineIndex.RAPsBetween(startOffset, endOffset)
}

func (tr *ringTimelineReader) DiscontinuitiesBetween(startOffset, endOffset int64) []timeline.DiscontinuityEntry {
	tr.ring.mu.Lock()
	defer tr.ring.mu.Unlock()
	if tr.ring.timelineIndex == nil {
		return nil
	}
	return tr.ring.timelineIndex.DiscontinuitiesBetween(startOffset, endOffset)
}

func (tr *ringTimelineReader) PCREntries() []timeline.PCREntry {
	tr.ring.mu.Lock()
	defer tr.ring.mu.Unlock()
	if tr.ring.timelineIndex == nil {
		return nil
	}
	return tr.ring.timelineIndex.PCREntries()
}

func (tr *ringTimelineReader) TimingPoints() []timeline.TimingPointEntry {
	tr.ring.mu.Lock()
	defer tr.ring.mu.Unlock()
	if tr.ring.timelineIndex == nil {
		return nil
	}
	return tr.ring.timelineIndex.TimingPoints()
}

func (tr *ringTimelineReader) EpochSpans() []timeline.EpochSpan {
	tr.ring.mu.Lock()
	defer tr.ring.mu.Unlock()
	if tr.ring.timelineIndex == nil {
		return nil
	}
	return tr.ring.timelineIndex.EpochSpans()
}

func (tr *ringTimelineReader) Stats() timeline.TimelineStats {
	tr.ring.mu.Lock()
	defer tr.ring.mu.Unlock()
	if tr.ring.timelineIndex == nil {
		return timeline.TimelineStats{}
	}
	return tr.ring.timelineIndex.Stats()
}
