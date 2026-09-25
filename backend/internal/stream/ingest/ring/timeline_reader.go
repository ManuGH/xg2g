// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"slices"

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
	if rap.Offset < tr.ring.store.tailOffset() || rap.Offset >= tr.ring.store.headOffset() {
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
	if rap.Offset < tr.ring.store.tailOffset() || rap.Offset >= tr.ring.store.headOffset() {
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
	if rap.Offset < tr.ring.store.tailOffset() || rap.Offset >= tr.ring.store.headOffset() {
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
	if rap.Offset < tr.ring.store.tailOffset() || rap.Offset >= tr.ring.store.headOffset() {
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
	if offset < tail || offset >= head {
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
	if endOffset >= head {
		endOffset = head - 1
	}
	if startOffset > endOffset {
		return nil
	}
	raw := tr.ring.timelineIndex.RAPsBetween(startOffset, endOffset)
	if len(raw) == 0 {
		return nil
	}
	raps := make([]timeline.RAPEntry, 0, len(raw))
	for _, rap := range raw {
		if rap.Offset >= tail && rap.Offset < head {
			raps = append(raps, rap)
		}
	}
	return raps
}

func (tr *ringTimelineReader) DiscontinuitiesBetween(startOffset, endOffset int64) []timeline.DiscontinuityEntry {
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
	if endOffset >= head {
		endOffset = head - 1
	}
	if startOffset > endOffset {
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

func (tr *ringTimelineReader) PresentationTimeline(epoch mediafacts.TimelineEpoch) (timeline.PresentationTimeline, bool) {
	tr.ring.mu.Lock()
	defer tr.ring.mu.Unlock()
	return tr.presentationTimelineLocked(epoch)
}

func (tr *ringTimelineReader) presentationTimelineLocked(epoch mediafacts.TimelineEpoch) (timeline.PresentationTimeline, bool) {
	if tr.ring.timelineIndex == nil {
		return timeline.PresentationTimeline{}, false
	}
	raw, ok := tr.ring.timelineIndex.PresentationTimeline(epoch)
	if !ok {
		return timeline.PresentationTimeline{}, false
	}

	tail := tr.ring.store.tailOffset()
	head := tr.ring.store.headOffset()

	if raw.Closed && raw.EndOffset <= tail {
		return timeline.PresentationTimeline{}, false
	}
	if raw.StartOffset >= head {
		return timeline.PresentationTimeline{}, false
	}

	pt := raw
	if pt.StartOffset < tail {
		pt.StartOffset = tail
	}
	if pt.Closed {
		if pt.EndOffset > head {
			pt.EndOffset = head
		}
	} else {
		pt.EndOffset = head
	}
	if pt.StartOffset > pt.EndOffset {
		return timeline.PresentationTimeline{}, false
	}

	// Re-evaluate TimingPoints strictly using SubjectAt in [tail, head)
	allTPs := tr.ring.timelineIndex.TimingPoints()
	tracksMap := make(map[uint16]*timeline.TrackPresentation)
	for _, tp := range allTPs {
		if tp.Epoch != epoch || tp.SubjectAt < tail || tp.SubjectAt >= head {
			continue
		}
		t, exists := tracksMap[tp.PID]
		if !exists {
			t = &timeline.TrackPresentation{
				PID: tp.PID,
			}
			tracksMap[tp.PID] = t
		}
		t.SampleCount++
		if tp.HasPTS {
			if !t.HasPTS {
				t.HasPTS = true
				t.EarliestPTS90k = tp.PTS90k
				t.LatestPTS90k = tp.PTS90k
			} else {
				if tp.PTS90k < t.EarliestPTS90k {
					t.EarliestPTS90k = tp.PTS90k
				}
				if tp.PTS90k > t.LatestPTS90k {
					t.LatestPTS90k = tp.PTS90k
				}
			}
		}
	}

	pids := make([]uint16, 0, len(tracksMap))
	for p := range tracksMap {
		pids = append(pids, p)
	}
	slices.Sort(pids)

	pt.Tracks = make([]timeline.TrackPresentation, 0, len(pids))
	for _, p := range pids {
		t := *tracksMap[p]
		if t.HasPTS && t.SampleCount >= 2 && t.LatestPTS90k >= t.EarliestPTS90k {
			t.ObservedSpan90k = t.LatestPTS90k - t.EarliestPTS90k
		}
		pt.Tracks = append(pt.Tracks, t)
	}

	// Re-evaluate RAPs strictly within [tail, head)
	raps := tr.ring.timelineIndex.RAPsBetween(tail, head-1)
	pt.TotalRAPs = 0
	pt.JoinableRAPs = 0
	pt.HasFirstRAP = false
	pt.HasLastRAP = false
	pt.FirstRAPOffset = 0
	pt.LastRAPOffset = 0

	for _, r := range raps {
		if !r.HasTimingBinding || r.Epoch != epoch || r.Offset < tail || r.Offset >= head {
			continue
		}
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

	var clampedDisc []timeline.DiscontinuityEntry
	for _, d := range pt.Discontinuities {
		if d.ObservedAt >= tail && d.ObservedAt < head {
			clampedDisc = append(clampedDisc, d)
		}
	}
	pt.Discontinuities = clampedDisc

	return pt, true
}

func (tr *ringTimelineReader) PresentationTimelines() []timeline.PresentationTimeline {
	tr.ring.mu.Lock()
	defer tr.ring.mu.Unlock()

	if tr.ring.timelineIndex == nil {
		return nil
	}

	spans := tr.ring.timelineIndex.EpochSpans()
	var res []timeline.PresentationTimeline
	for _, s := range spans {
		if pt, ok := tr.presentationTimelineLocked(s.Epoch); ok {
			res = append(res, pt)
		}
	}
	return res
}

func (tr *ringTimelineReader) FindRAPByTime(epoch mediafacts.TimelineEpoch, pts int64, opts timeline.SeekOptions) (timeline.RAPEntry, bool) {
	tr.ring.mu.Lock()
	defer tr.ring.mu.Unlock()

	if tr.ring.timelineIndex == nil {
		return timeline.RAPEntry{}, false
	}

	rap, ok := tr.ring.timelineIndex.FindRAPByTime(epoch, pts, opts)
	if !ok {
		return timeline.RAPEntry{}, false
	}
	if rap.Offset < tr.ring.store.tailOffset() || rap.Offset >= tr.ring.store.headOffset() {
		return timeline.RAPEntry{}, false
	}
	return rap, true
}
