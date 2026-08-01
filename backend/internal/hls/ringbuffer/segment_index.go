package ringbuffer

import (
	"sort"
	"time"
)

// SegmentIndex manages in-memory segments per service with wall-clock time indexing.
type SegmentIndex struct {
	byService map[string][]*InternalSegment
}

// NewSegmentIndex initializes an empty segment index.
func NewSegmentIndex() *SegmentIndex {
	return &SegmentIndex{
		byService: make(map[string][]*InternalSegment),
	}
}

// AddSegment inserts a new segment into the index in sequence order.
func (idx *SegmentIndex) AddSegment(seg *InternalSegment) {
	service := seg.ID.ServiceRef
	segments := idx.byService[service]
	segments = append(segments, seg)
	sort.Slice(segments, func(i, j int) bool {
		return segments[i].Sequence < segments[j].Sequence
	})
	idx.byService[service] = segments
}

// SelectRange filters non-deleting segments falling within [start, end].
func (idx *SegmentIndex) SelectRange(serviceRef string, start, end time.Time) []*InternalSegment {
	segments := idx.byService[serviceRef]
	var matched []*InternalSegment

	for _, seg := range segments {
		// Ignore segments marked for deletion or missing
		if seg.State == SegmentDeleting || seg.State == SegmentMissing {
			continue
		}

		// Check overlap with requested window
		if seg.EndWallTime.After(start) && seg.StartWallTime.Before(end) {
			matched = append(matched, seg)
		}
	}

	return matched
}

// GetByID looks up a segment by its compound ID.
func (idx *SegmentIndex) GetByID(id SegmentID) (*InternalSegment, bool) {
	segments, ok := idx.byService[id.ServiceRef]
	if !ok {
		return nil, false
	}
	for _, seg := range segments {
		if seg.ID == id {
			return seg, true
		}
	}
	return nil, false
}

// MarkForDeletion transitions active, unreserved segments to SegmentDeleting state.
func (idx *SegmentIndex) MarkForDeletion(id SegmentID) bool {
	seg, ok := idx.GetByID(id)
	if !ok {
		return false
	}
	if seg.State == SegmentReserved {
		return false // Cannot delete a reserved segment
	}
	seg.State = SegmentDeleting
	return true
}

// RemoveSegment deletes a segment entry from the index after disk deletion.
func (idx *SegmentIndex) RemoveSegment(id SegmentID) {
	segments, ok := idx.byService[id.ServiceRef]
	if !ok {
		return
	}
	var filtered []*InternalSegment
	for _, seg := range segments {
		if seg.ID != id {
			filtered = append(filtered, seg)
		}
	}
	if len(filtered) == 0 {
		delete(idx.byService, id.ServiceRef)
	} else {
		idx.byService[id.ServiceRef] = filtered
	}
}
