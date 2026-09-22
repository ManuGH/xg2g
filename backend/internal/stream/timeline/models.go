// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package timeline

import (
	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// RAPEntry represents a canonical Random Access Point (RAP) indexed by transport offset.
type RAPEntry struct {
	// Offset is the byte position of the RAP in the caller's monotonic coordinate system.
	Offset int64

	// Joinable reports whether the access unit was clear (unscrambled) and suitable as a join point.
	Joinable bool

	// Epoch is the timeline epoch established by canonical media-core timing.
	Epoch mediafacts.TimelineEpoch

	// PID is the elementary stream PID carrying this access unit.
	PID uint16

	// HasPTS reports whether canonical PTS is present.
	HasPTS bool

	// PTS90k is the unwrapped 90 kHz PTS.
	PTS90k int64

	// HasDTS reports whether canonical DTS is present.
	HasDTS bool

	// DTS90k is the unwrapped 90 kHz DTS.
	DTS90k int64
}

// TimingPointEntry records a canonical PES timing point.
type TimingPointEntry struct {
	Epoch      mediafacts.TimelineEpoch
	PID        uint16
	HasPTS     bool
	PTS90k     int64
	HasDTS     bool
	DTS90k     int64
	ObservedAt int64
	SubjectAt  int64
}

// PCREntry records an unwrapped 27 MHz PCR sample aligned to the timeline phase.
// It carries canonical samples only: no interpolation or clock rate estimation is performed.
type PCREntry struct {
	Epoch          mediafacts.TimelineEpoch
	PCRPID         uint16
	ObservedAt     int64
	ExtendedPCR27m int64
}

// DiscontinuityEntry records an observed timeline discontinuity edge.
type DiscontinuityEntry struct {
	Scope          mediafacts.DiscontinuityScope
	TrackPID       uint16 // 0 if Scope is DiscontinuityScopeProgram
	Reason         mediafacts.DiscontinuityReason
	ObservedAt     int64
	HasEpochBefore bool
	EpochBefore    mediafacts.TimelineEpoch
	HasEpochAfter  bool
	EpochAfter     mediafacts.TimelineEpoch
}

// EpochSpan records the byte range covered by a TimelineEpoch.
// Closed epochs cover the half-open interval [StartOffset, EndOffset).
// Open/active epochs cover [StartOffset, ∞) with Closed=false.
type EpochSpan struct {
	Epoch       mediafacts.TimelineEpoch
	StartOffset int64
	EndOffset   int64
	Closed      bool
}
