// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package mediafacts

import "fmt"

// TimingAuthority declares who produced the timing information in a ParseResult.
// GoCore does not compute canonical timing and reports TimingAuthorityNone.
// RemoteCore speaking Protocol v6+ reports TimingAuthorityCanonical.
type TimingAuthority uint8

const (
	TimingAuthorityUnknown TimingAuthority = iota
	TimingAuthorityNone
	TimingAuthorityCanonical
)

func (a TimingAuthority) String() string {
	switch a {
	case TimingAuthorityUnknown:
		return "unknown"
	case TimingAuthorityNone:
		return "none"
	case TimingAuthorityCanonical:
		return "canonical"
	default:
		return fmt.Sprintf("TimingAuthority(%d)", uint8(a))
	}
}

// TimelineEpoch is an epoch counter identifying a continuous, monotonically
// unwrapped timeline interval.
type TimelineEpoch uint64

// TimingPoint carries unwrapped 90 kHz PTS/DTS coordinates for an access unit.
type TimingPoint struct {
	Epoch      TimelineEpoch
	PID        uint16
	HasPTS     bool
	PTS90k     int64
	HasDTS     bool
	DTS90k     int64
	ObservedAt int64
	SubjectAt  int64
}

// PCRPoint carries an unwrapped 27 MHz PCR sample aligned to the timeline phase.
type PCRPoint struct {
	Epoch          TimelineEpoch
	PCRPID         uint16
	ObservedAt     int64
	ExtendedPCR27m int64
}

// DiscontinuityScope declares the scope of a timeline discontinuity.
type DiscontinuityScope uint8

const (
	DiscontinuityScopeUnknown DiscontinuityScope = iota
	DiscontinuityScopeProgram
	DiscontinuityScopeTrack
)

func (s DiscontinuityScope) String() string {
	switch s {
	case DiscontinuityScopeUnknown:
		return "unknown"
	case DiscontinuityScopeProgram:
		return "program"
	case DiscontinuityScopeTrack:
		return "track"
	default:
		return fmt.Sprintf("DiscontinuityScope(%d)", uint8(s))
	}
}

// DiscontinuityReason declares what triggered a timeline discontinuity or re-anchoring.
type DiscontinuityReason uint8

const (
	DiscontinuityReasonUnknown DiscontinuityReason = iota
	DiscontinuityReasonProgramIdentityChanged
	DiscontinuityReasonPCRPIDChanged
	DiscontinuityReasonPCRDiscontinuityIndicator
	DiscontinuityReasonTransportTimingLoss
)

func (r DiscontinuityReason) String() string {
	switch r {
	case DiscontinuityReasonUnknown:
		return "unknown"
	case DiscontinuityReasonProgramIdentityChanged:
		return "program_identity_changed"
	case DiscontinuityReasonPCRPIDChanged:
		return "pcr_pid_changed"
	case DiscontinuityReasonPCRDiscontinuityIndicator:
		return "pcr_discontinuity_indicator"
	case DiscontinuityReasonTransportTimingLoss:
		return "transport_timing_loss"
	default:
		return fmt.Sprintf("DiscontinuityReason(%d)", uint8(r))
	}
}

// DiscontinuityRecord describes an observed timeline discontinuity edge.
type DiscontinuityRecord struct {
	Scope          DiscontinuityScope
	TrackPID       uint16 // 0 if Scope is DiscontinuityScopeProgram
	Reason         DiscontinuityReason
	ObservedAt     int64
	HasEpochBefore bool
	EpochBefore    TimelineEpoch
	HasEpochAfter  bool
	EpochAfter     TimelineEpoch
}

// TimingRecordType identifies the kind of timing record.
type TimingRecordType uint8

const (
	TimingRecordTypeUnknown TimingRecordType = iota
	TimingRecordTypePES
	TimingRecordTypePCR
	TimingRecordTypeDiscontinuity
	TimingRecordTypeRandomAccessPoint
)

func (t TimingRecordType) String() string {
	switch t {
	case TimingRecordTypeUnknown:
		return "unknown"
	case TimingRecordTypePES:
		return "pes"
	case TimingRecordTypePCR:
		return "pcr"
	case TimingRecordTypeDiscontinuity:
		return "discontinuity"
	case TimingRecordTypeRandomAccessPoint:
		return "rap"
	default:
		return fmt.Sprintf("TimingRecordType(%d)", uint8(t))
	}
}

// TimingRecord is a discriminated union of timing events within a chunk.
type TimingRecord struct {
	Type TimingRecordType
	PES  TimingPoint
	// RAP binds a random access point to the canonical timing of the PES that
	// carries it: SubjectAt is the RAP's offset, ObservedAt the packet at which the
	// RAP was established, and Epoch, PID, PTS and DTS are that PES's timing.
	//
	// media-core publishes it in the same result as the RandomAccessPoint event it
	// binds, even when the PES header arrived chunks earlier - an all-intra H.264
	// access unit or an HEVC recovery point is only a RAP once it has ended. A RAP
	// without such a record has no canonical timing; there is nothing to join.
	RAP           TimingPoint
	PCR           PCRPoint
	Discontinuity DiscontinuityRecord
}

// TimingResult is the timing section of a ParseResult.
type TimingResult struct {
	// Authority states whether canonical timing is present.
	Authority TimingAuthority

	// HasActiveEpoch states whether an active program epoch is established.
	HasActiveEpoch bool

	// ActiveEpoch is the active timeline epoch for the current program.
	ActiveEpoch TimelineEpoch

	// Records contains the ordered timing records observed in this chunk.
	Records []TimingRecord
}
