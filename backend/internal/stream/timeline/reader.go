// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package timeline

import (
	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// TimelineReader provides a read-only view of canonical timeline truth.
// It directly mirrors the querying and inspection methods of MediaIndex,
// allowing consumers (such as MasterRing.Timeline()) to read canonical
// media facts without exposure to mutation methods.
type TimelineReader interface {
	ActiveEpoch() (mediafacts.TimelineEpoch, bool)
	FindPrecedingRAP(offset int64) (RAPEntry, bool)
	FindFollowingRAP(offset int64) (RAPEntry, bool)
	FindRAPPrecedingPTS(epoch mediafacts.TimelineEpoch, pts int64) (RAPEntry, bool)
	FindRAPNearestPTS(epoch mediafacts.TimelineEpoch, pts int64) (RAPEntry, bool)
	EpochForOffset(offset int64) (EpochSpan, bool)
	RAPsBetween(startOffset, endOffset int64) []RAPEntry
	DiscontinuitiesBetween(startOffset, endOffset int64) []DiscontinuityEntry
	PCREntries() []PCREntry
	TimingPoints() []TimingPointEntry
	EpochSpans() []EpochSpan
	Stats() TimelineStats
	PresentationTimeline(epoch mediafacts.TimelineEpoch) (PresentationTimeline, bool)
	PresentationTimelines() []PresentationTimeline
	FindRAPByTime(epoch mediafacts.TimelineEpoch, pts int64, opts SeekOptions) (RAPEntry, bool)
}
