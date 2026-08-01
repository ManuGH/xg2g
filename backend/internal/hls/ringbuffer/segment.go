package ringbuffer

import (
	"fmt"
	"time"
)

// SegmentState tracks the authoritative lifecycle state of a segment within the ringbuffer.
type SegmentState uint8

const (
	SegmentActive SegmentState = iota
	SegmentReserved
	SegmentDeleting
	SegmentMissing
)

func (s SegmentState) String() string {
	switch s {
	case SegmentActive:
		return "ACTIVE"
	case SegmentReserved:
		return "RESERVED"
	case SegmentDeleting:
		return "DELETING"
	case SegmentMissing:
		return "MISSING"
	default:
		return "UNKNOWN"
	}
}

// SegmentID uniquely identifies a segment across stream restarts and session boundaries.
type SegmentID struct {
	ServiceRef string `json:"service_ref"`
	SessionID  string `json:"session_id"`
	Sequence   uint64 `json:"sequence"`
}

func (id SegmentID) String() string {
	return fmt.Sprintf("%s:%s:%d", id.ServiceRef, id.SessionID, id.Sequence)
}

// Completeness describes the availability status of requested segments in the ringbuffer.
type Completeness string

const (
	CompletenessComplete     Completeness = "COMPLETE"
	CompletenessPartialStart Completeness = "PARTIAL_AT_START"
	CompletenessPartialEnd   Completeness = "PARTIAL_AT_END"
	CompletenessPartialBoth  Completeness = "PARTIAL_AT_BOTH"
	CompletenessGapped       Completeness = "GAPPED"
	CompletenessUnavailable  Completeness = "UNAVAILABLE"
)

// Gap represents a temporal or media-time discontinuity within a segment range.
type Gap struct {
	StartPTS90k   int64     `json:"start_pts_90k"`
	EndPTS90k     int64     `json:"end_pts_90k"`
	StartWallTime time.Time `json:"start_wall_time"`
	EndWallTime   time.Time `json:"end_wall_time"`
	DurationSec   float64   `json:"duration_sec"`
	Reason        string    `json:"reason"`
}

// InternalSegment holds complete metadata and mutable lifecycle state for a segment.
type InternalSegment struct {
	ID             SegmentID           `json:"id"`
	Path           string              `json:"path"`
	Sequence       uint64              `json:"sequence"`
	StartPTS90k    int64               `json:"start_pts_90k"`
	EndPTS90k      int64               `json:"end_pts_90k"`
	PTSEpoch       uint32              `json:"pts_epoch"`
	StartWallTime  time.Time           `json:"start_wall_time"`
	EndWallTime    time.Time           `json:"end_wall_time"`
	SizeBytes      int64               `json:"size_bytes"`
	Discontinuity  bool                `json:"discontinuity"`
	CodecHash      string              `json:"codec_hash"`
	State          SegmentState        `json:"state"`
	ReservationIDs map[string]struct{} `json:"reservation_ids"`
}

// IsReserved returns true if one or more active reservations hold this segment.
func (seg *InternalSegment) IsReserved() bool {
	return len(seg.ReservationIDs) > 0
}

// SegmentHandle exposes immutable segment metadata to callers (reservations & jobs).
type SegmentHandle struct {
	ID            SegmentID `json:"id"`
	Path          string    `json:"path"`
	Sequence      uint64    `json:"sequence"`
	StartPTS90k   int64     `json:"start_pts_90k"`
	EndPTS90k     int64     `json:"end_pts_90k"`
	PTSEpoch      uint32    `json:"pts_epoch"`
	StartWallTime time.Time `json:"start_wall_time"`
	EndWallTime   time.Time `json:"end_wall_time"`
	SizeBytes     int64     `json:"size_bytes"`
	Discontinuity bool      `json:"discontinuity"`
	CodecHash     string    `json:"codec_hash"`
}

// RangeProbe represents the result of probing segment availability over a time window.
type RangeProbe struct {
	RequestedStart  time.Time    `json:"requested_start"`
	RequestedEnd    time.Time    `json:"requested_end"`
	AvailableStart  *time.Time   `json:"available_start,omitempty"`
	AvailableEnd    *time.Time   `json:"available_end,omitempty"`
	Completeness    Completeness `json:"completeness"`
	Gaps            []Gap        `json:"gaps,omitempty"`
	SegmentCount    int          `json:"segment_count"`
	TotalBytes      int64        `json:"total_bytes"`
	CodecChanges    int          `json:"codec_changes"`
	Discontinuities int          `json:"discontinuities"`
}
