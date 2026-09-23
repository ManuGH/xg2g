// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"fmt"
	"math"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// The PSI+Video result envelope, protocol v4.
//
// One layout for both answers that carry a result - ingest and set-target -
// because they return the same thing: what the core knows now. A second layout
// would be a second place for the two implementations to drift.
//
//	u8      status                  StatusOK, or nothing follows
//	u8      coverage                what the answer covers
//	u64     processed-through offset
//	u32     event count
//	          u8   kind
//	          u64  offset
//	          u8   flags            bit 0 joinable
//	u8      facts flags             bit 0 has PAT, bit 1 has PMT
//	u8      PMT version
//	u16     program number
//	u16     PMT PID
//	u16     video PID
//	u8      video codec
//	u32     audio PID count
//	          u16  PID
//	u32     audio track count
//	          u16  PID
//	          u8   stream type
//	          u8   codec
//	          [3]  language
//	          u8   declared channels
//	          u8   flags            bit 0 multichannel, bit 1 has component type
//	          u8   component type
//	          u8   observed channels
//	          u8   observed flags   bit 0 LFE, bit 1 has acmod, bit 2 dependent substream
//	          u8   observed acmod
//	          u64  observed frames
//	u8      video facts flags       bit 0 parameter sets seen, bit 1 scrambled confirmed
//	u64     clean entry points
//	u64     clean access units
//	u64     IRAP points
//	u64     intra points
//	u64     recovery point SEIs
//	u64     predicted rejected
//	u64     unreadable slices
//	u64     video scrambled packets
//	u64     video clear packets
//	u64     video clear run
//	u64     audio scrambled packets
//	u64     audio clear packets
//	u64     audio clear run
//	u16     PAT section count
//	          u16  length, then that many bytes
//	u16     PMT section count
//	          u16  length, then that many bytes
//
// Every integer is big-endian and every width is fixed. Language is three bytes
// with no length prefix because it is always exactly three: the descriptor form
// is three, and the absence form is "und".
//
// Counts are the fields a decoder would act on before reading anything else, so
// each is checked against what is left of the frame before a single element is
// allocated. A peer that announces a million tracks is not asking for memory, it
// is failing.

// Wire enums. Closed sets get numbers rather than the strings the Go types
// happen to use: a string on the wire is an invitation to send one nobody
// expects, and the two implementations do not share a definition of what the
// spellings are.
const (
	wireCoverageUnknown  uint8 = 0
	wireCoveragePSIOnly  uint8 = 1
	wireCoverageComplete uint8 = 2
	wireCoveragePSIVideo uint8 = 3
)

const (
	wireVideoCodecUnknown uint8 = 0
	wireVideoCodecH264    uint8 = 1
	wireVideoCodecH265    uint8 = 2
	wireVideoCodecMPEG2   uint8 = 3
)

const (
	wireAudioCodecUnknown uint8 = 0
	wireAudioCodecMP2     uint8 = 1
	wireAudioCodecAAC     uint8 = 2
	wireAudioCodecAC3     uint8 = 3
	wireAudioCodecEAC3    uint8 = 4
	wireAudioCodecDTS     uint8 = 5
)

const (
	wireEventProgramIdentityChanged       uint8 = 1
	wireEventRandomAccessPoint            uint8 = 2
	wireEventRandomAccessPointInvalidated uint8 = 3
)

const (
	wireFactHasPAT uint8 = 1 << 0
	wireFactHasPMT uint8 = 1 << 1

	wireEventJoinable uint8 = 1 << 0

	wireTrackMultichannel     uint8 = 1 << 0
	wireTrackHasComponentType uint8 = 1 << 1

	wireObsFlagLFE                uint8 = 1 << 0
	wireObsFlagHasACMod           uint8 = 1 << 1
	wireObsFlagDependentSubstream uint8 = 1 << 2

	wireVideoFactParameterSetsSeen  uint8 = 1 << 0
	wireVideoFactScrambledConfirmed uint8 = 1 << 1
)

// Fixed sizes, named so the bound checks below read as arithmetic about the
// format rather than as magic numbers.
const (
	wireLanguageSize         = 3
	wireSectionHeaderBytes   = 3
	wireEventSize            = 1 + 8 + 1
	wireAudioPIDSize         = 2
	wireAudioObservationSize = 1 + 1 + 1 + 8
	wireAudioTrackSize       = 2 + 1 + 1 + wireLanguageSize + 1 + 1 + 1 + wireAudioObservationSize
	wireVideoFactsSize       = 1 + 8 + 8 + (5 * 8) + (3 * 8)
	wireAudioScramblingSize  = 3 * 8
	wireFactsBlockSize       = wireVideoFactsSize + wireAudioScramblingSize
	wireSectionMinSize       = 2

	wireEnvelopeHeaderSize  = 1 + 1 + 8 + 2
	wireSectionHeaderSize   = 1 + 1 + 2 + 4
	wireTimingRecordMinSize = 27
)

// maxActiveTableBytes is what one table in force may cost, from the bounds
// mediafacts derives: 256 sections of at most 1024 bytes.
const maxActiveTableBytes = mediafacts.MaxSectionsPerTable * mediafacts.MaxSectionBytes

// short is the one error a truncated result produces, so every read site says
// the same thing about the same failure.
func short(what string) error {
	return fmt.Errorf("%w: the result ended before its %s", mediafacts.ErrCoreInvalidResponse, what)
}

// boundedCount reads a count and refuses one the rest of the frame cannot hold.
//
// This is the check that matters. `make([]T, n)` with an n the peer chose is an
// allocation instruction; multiplying it by the smallest an element can be and
// comparing against what is left turns it back into a claim that can be false.
func boundedCount(r *reader, width int, what string) (int, error) {
	raw, ok := r.uint32()
	if !ok {
		return 0, short(what + " count")
	}
	n := int64(raw)
	if width > 0 && n*int64(width) > int64(r.left()) {
		return 0, fmt.Errorf("%w: %d %s do not fit in %d remaining bytes",
			mediafacts.ErrCoreInvalidResponse, n, what, r.left())
	}
	return int(n), nil
}

// decodePSIResult reads one v6 result envelope.
//
// The status byte is consumed here so the offsets in the layout above are the
// offsets in the frame. A body that does not end exactly where the last field
// ends is refused: trailing bytes are another message's shape, and a decoder
// that ignores them cannot tell a peer it agrees with from one it does not.
func decodePSIResult(body []byte) (mediafacts.ParseResult, error) {
	r := &reader{b: body}

	status, ok := r.uint8()
	if !ok {
		return mediafacts.ParseResult{}, short("status")
	}
	if status != StatusOK {
		return mediafacts.ParseResult{}, fmt.Errorf("%w: status %d", mediafacts.ErrCoreInvalidResponse, status)
	}

	rawCoverage, ok := r.uint8()
	if !ok {
		return mediafacts.ParseResult{}, short("coverage")
	}
	var parsed mediafacts.ParseResult
	switch rawCoverage {
	case wireCoverageComplete:
		parsed.Coverage = mediafacts.ParseCoverageComplete
	case wireCoveragePSIOnly:
		return mediafacts.ParseResult{}, fmt.Errorf(
			"%w: peer claims psi-only coverage, which is not valid in protocol v6",
			mediafacts.ErrCoreInvalidResponse)
	case wireCoveragePSIVideo:
		return mediafacts.ParseResult{}, fmt.Errorf(
			"%w: peer claims psi-video coverage, which is not valid in protocol v6",
			mediafacts.ErrCoreInvalidResponse)
	default:
		return mediafacts.ParseResult{}, fmt.Errorf("%w: coverage %d",
			mediafacts.ErrCoreInvalidResponse, rawCoverage)
	}

	through, ok := r.int64()
	if !ok {
		return mediafacts.ParseResult{}, short("processed-through offset")
	}
	if through < 0 {
		return mediafacts.ParseResult{}, fmt.Errorf("%w: negative processed-through offset %d",
			mediafacts.ErrCoreInvalidResponse, through)
	}
	parsed.ProcessedThroughOffset = through

	sectionCount, ok := r.uint16()
	if !ok {
		return mediafacts.ParseResult{}, short("section count")
	}

	var seenEvents, seenFacts, seenPSI, seenTiming bool

	for i := 0; i < int(sectionCount); i++ {
		secType, ok := r.uint8()
		if !ok {
			return mediafacts.ParseResult{}, short("section type")
		}
		secVersion, ok := r.uint8()
		if !ok {
			return mediafacts.ParseResult{}, short("section version")
		}
		secFlags, ok := r.uint16()
		if !ok {
			return mediafacts.ParseResult{}, short("section flags")
		}
		secLen, ok := r.uint32()
		if !ok {
			return mediafacts.ParseResult{}, short("section length")
		}
		if int64(secLen) > int64(r.left()) {
			return mediafacts.ParseResult{}, fmt.Errorf("%w: section %d length %d exceeds remaining bytes %d",
				mediafacts.ErrCoreInvalidResponse, secType, secLen, r.left())
		}
		secBytes, ok := r.bytes(int(secLen))
		if !ok {
			return mediafacts.ParseResult{}, short("section payload")
		}
		secReader := &reader{b: secBytes}

		isCritical := (secFlags & SectionFlagCritical) != 0

		switch secType {
		case SectionEvents, SectionFacts, SectionActivePSI, SectionTiming:
			if secFlags != SectionFlagCritical {
				return mediafacts.ParseResult{}, fmt.Errorf("%w: section %d flags must be 0x0001 (got 0x%04x)",
					mediafacts.ErrCoreInvalidResponse, secType, secFlags)
			}
		}

		switch secType {
		case SectionEvents:
			if seenEvents {
				return mediafacts.ParseResult{}, fmt.Errorf("%w: duplicate SectionEvents", mediafacts.ErrCoreInvalidResponse)
			}
			if secVersion != SectionVersionV1 {
				return mediafacts.ParseResult{}, fmt.Errorf("%w: SectionEvents version %d", mediafacts.ErrCoreInvalidResponse, secVersion)
			}
			var err error
			if parsed.Events, err = decodeEvents(secReader); err != nil {
				return mediafacts.ParseResult{}, err
			}
			if secReader.left() != 0 {
				return mediafacts.ParseResult{}, fmt.Errorf("%w: %d trailing bytes in SectionEvents", mediafacts.ErrCoreInvalidResponse, secReader.left())
			}
			seenEvents = true

		case SectionFacts:
			if seenFacts {
				return mediafacts.ParseResult{}, fmt.Errorf("%w: duplicate SectionFacts", mediafacts.ErrCoreInvalidResponse)
			}
			if secVersion != SectionVersionV1 {
				return mediafacts.ParseResult{}, fmt.Errorf("%w: SectionFacts version %d", mediafacts.ErrCoreInvalidResponse, secVersion)
			}
			var err error
			if parsed.Facts, err = decodeFacts(secReader); err != nil {
				return mediafacts.ParseResult{}, err
			}
			if secReader.left() != 0 {
				return mediafacts.ParseResult{}, fmt.Errorf("%w: %d trailing bytes in SectionFacts", mediafacts.ErrCoreInvalidResponse, secReader.left())
			}
			seenFacts = true

		case SectionActivePSI:
			if seenPSI {
				return mediafacts.ParseResult{}, fmt.Errorf("%w: duplicate SectionActivePSI", mediafacts.ErrCoreInvalidResponse)
			}
			if secVersion != SectionVersionV1 {
				return mediafacts.ParseResult{}, fmt.Errorf("%w: SectionActivePSI version %d", mediafacts.ErrCoreInvalidResponse, secVersion)
			}
			var err error
			if parsed.PSI, err = decodeActivePSI(secReader); err != nil {
				return mediafacts.ParseResult{}, err
			}
			if secReader.left() != 0 {
				return mediafacts.ParseResult{}, fmt.Errorf("%w: %d trailing bytes in SectionActivePSI", mediafacts.ErrCoreInvalidResponse, secReader.left())
			}
			seenPSI = true

		case SectionTiming:
			if seenTiming {
				return mediafacts.ParseResult{}, fmt.Errorf("%w: duplicate SectionTiming", mediafacts.ErrCoreInvalidResponse)
			}
			if secVersion != SectionVersionV1 {
				return mediafacts.ParseResult{}, fmt.Errorf("%w: SectionTiming version %d", mediafacts.ErrCoreInvalidResponse, secVersion)
			}
			var err error
			if parsed.Timing, err = decodeTiming(secReader); err != nil {
				return mediafacts.ParseResult{}, err
			}
			if secReader.left() != 0 {
				return mediafacts.ParseResult{}, fmt.Errorf("%w: %d trailing bytes in SectionTiming", mediafacts.ErrCoreInvalidResponse, secReader.left())
			}
			seenTiming = true

		default:
			if isCritical {
				return mediafacts.ParseResult{}, fmt.Errorf("%w: unknown critical section type %d", mediafacts.ErrCoreInvalidResponse, secType)
			}
			// Non-critical unknown sections are safely skipped.
		}
	}

	if !seenEvents || !seenFacts || !seenPSI || !seenTiming {
		return mediafacts.ParseResult{}, fmt.Errorf("%w: missing required critical sections (events=%v, facts=%v, psi=%v, timing=%v)",
			mediafacts.ErrCoreInvalidResponse, seenEvents, seenFacts, seenPSI, seenTiming)
	}

	if r.left() != 0 {
		return mediafacts.ParseResult{}, fmt.Errorf("%w: %d bytes after the result",
			mediafacts.ErrCoreInvalidResponse, r.left())
	}
	return parsed, nil
}

func decodeTiming(r *reader) (mediafacts.TimingResult, error) {
	hasActiveEpoch, ok := r.uint8()
	if !ok {
		return mediafacts.TimingResult{}, short("has active epoch flag")
	}
	if hasActiveEpoch > 1 {
		return mediafacts.TimingResult{}, fmt.Errorf("%w: invalid has_active_epoch flag %d", mediafacts.ErrCoreInvalidResponse, hasActiveEpoch)
	}

	activeEpoch, ok := r.uint64()
	if !ok {
		return mediafacts.TimingResult{}, short("active epoch")
	}
	if hasActiveEpoch == 0 && activeEpoch != 0 {
		return mediafacts.TimingResult{}, fmt.Errorf("%w: active epoch must be 0 when has_active_epoch is false, got %d", mediafacts.ErrCoreInvalidResponse, activeEpoch)
	}

	recordCount, err := boundedCount(r, wireTimingRecordMinSize, "timing records")
	if err != nil {
		return mediafacts.TimingResult{}, err
	}

	var records []mediafacts.TimingRecord
	if recordCount > 0 {
		records = make([]mediafacts.TimingRecord, 0, recordCount)
	}

	for i := 0; i < recordCount; i++ {
		recType, ok := r.uint8()
		if !ok {
			return mediafacts.TimingResult{}, short("timing record type")
		}

		switch recType {
		case TimingRecordPES:
			point, err := decodeTimingPoint(r, "PES")
			if err != nil {
				return mediafacts.TimingResult{}, err
			}
			records = append(records, mediafacts.TimingRecord{Type: mediafacts.TimingRecordTypePES, PES: point})

		case TimingRecordRAP:
			point, err := decodeTimingPoint(r, "RAP")
			if err != nil {
				return mediafacts.TimingResult{}, err
			}
			records = append(records, mediafacts.TimingRecord{Type: mediafacts.TimingRecordTypeRandomAccessPoint, RAP: point})

		case TimingRecordPCR:
			epoch, ok := r.uint64()
			if !ok {
				return mediafacts.TimingResult{}, short("timing PCR epoch")
			}
			pcrPID, ok := r.uint16()
			if !ok {
				return mediafacts.TimingResult{}, short("timing PCR PID")
			}
			if pcrPID > 0x1FFF {
				return mediafacts.TimingResult{}, fmt.Errorf("%w: timing PCR PID %d exceeds 13-bit limit (0x1FFF)", mediafacts.ErrCoreInvalidResponse, pcrPID)
			}
			observedAt, ok := r.int64()
			if !ok {
				return mediafacts.TimingResult{}, short("timing PCR observed_at")
			}
			if observedAt < 0 {
				return mediafacts.TimingResult{}, fmt.Errorf("%w: timing PCR negative observed_at %d", mediafacts.ErrCoreInvalidResponse, observedAt)
			}
			extendedPCR27m, ok := r.int64()
			if !ok {
				return mediafacts.TimingResult{}, short("timing PCR extended_pcr_27m")
			}

			records = append(records, mediafacts.TimingRecord{
				Type: mediafacts.TimingRecordTypePCR,
				PCR: mediafacts.PCRPoint{
					Epoch:          mediafacts.TimelineEpoch(epoch),
					PCRPID:         pcrPID,
					ObservedAt:     observedAt,
					ExtendedPCR27m: extendedPCR27m,
				},
			})

		case TimingRecordDiscontinuity:
			scope, ok := r.uint8()
			if !ok {
				return mediafacts.TimingResult{}, short("timing discontinuity scope")
			}
			var discScope mediafacts.DiscontinuityScope
			switch scope {
			case TimingDiscontinuityScopeProgram:
				discScope = mediafacts.DiscontinuityScopeProgram
			case TimingDiscontinuityScopeTrack:
				discScope = mediafacts.DiscontinuityScopeTrack
			default:
				return mediafacts.TimingResult{}, fmt.Errorf("%w: unknown timing discontinuity scope %d", mediafacts.ErrCoreInvalidResponse, scope)
			}

			trackPID, ok := r.uint16()
			if !ok {
				return mediafacts.TimingResult{}, short("timing discontinuity track PID")
			}
			if scope == TimingDiscontinuityScopeProgram && trackPID != 0 {
				return mediafacts.TimingResult{}, fmt.Errorf("%w: timing discontinuity track PID must be 0 for Program scope, got %d", mediafacts.ErrCoreInvalidResponse, trackPID)
			}
			if scope == TimingDiscontinuityScopeTrack && (trackPID == 0 || trackPID >= 0x1FFF) {
				return mediafacts.TimingResult{}, fmt.Errorf("%w: timing discontinuity track PID must be a valid non-null TS PID (1..8191), got %d", mediafacts.ErrCoreInvalidResponse, trackPID)
			}

			reason, ok := r.uint8()
			if !ok {
				return mediafacts.TimingResult{}, short("timing discontinuity reason")
			}
			var discReason mediafacts.DiscontinuityReason
			switch reason {
			case TimingDiscontinuityReasonProgramIdentityChanged:
				discReason = mediafacts.DiscontinuityReasonProgramIdentityChanged
			case TimingDiscontinuityReasonPCRPIDChanged:
				discReason = mediafacts.DiscontinuityReasonPCRPIDChanged
			case TimingDiscontinuityReasonPCRDiscontinuity:
				discReason = mediafacts.DiscontinuityReasonPCRDiscontinuityIndicator
			case TimingDiscontinuityReasonTransportTimingLoss:
				discReason = mediafacts.DiscontinuityReasonTransportTimingLoss
			default:
				return mediafacts.TimingResult{}, fmt.Errorf("%w: unknown timing discontinuity reason %d", mediafacts.ErrCoreInvalidResponse, reason)
			}

			observedAt, ok := r.int64()
			if !ok {
				return mediafacts.TimingResult{}, short("timing discontinuity observed_at")
			}
			if observedAt < 0 {
				return mediafacts.TimingResult{}, fmt.Errorf("%w: timing discontinuity negative observed_at %d", mediafacts.ErrCoreInvalidResponse, observedAt)
			}

			flags, ok := r.uint8()
			if !ok {
				return mediafacts.TimingResult{}, short("timing discontinuity flags")
			}
			if flags&^(TimingDiscontinuityFlagHasEpochBefore|TimingDiscontinuityFlagHasEpochAfter) != 0 {
				return mediafacts.TimingResult{}, fmt.Errorf("%w: timing discontinuity flags %#02x", mediafacts.ErrCoreInvalidResponse, flags)
			}

			epochBefore, ok := r.uint64()
			if !ok {
				return mediafacts.TimingResult{}, short("timing discontinuity epoch_before")
			}
			epochAfter, ok := r.uint64()
			if !ok {
				return mediafacts.TimingResult{}, short("timing discontinuity epoch_after")
			}

			// Canonical zero rules
			if flags&TimingDiscontinuityFlagHasEpochBefore == 0 && epochBefore != 0 {
				return mediafacts.TimingResult{}, fmt.Errorf("%w: timing discontinuity epoch_before must be 0 when has_epoch_before is false, got %d", mediafacts.ErrCoreInvalidResponse, epochBefore)
			}
			if flags&TimingDiscontinuityFlagHasEpochAfter == 0 && epochAfter != 0 {
				return mediafacts.TimingResult{}, fmt.Errorf("%w: timing discontinuity epoch_after must be 0 when has_epoch_after is false, got %d", mediafacts.ErrCoreInvalidResponse, epochAfter)
			}

			records = append(records, mediafacts.TimingRecord{
				Type: mediafacts.TimingRecordTypeDiscontinuity,
				Discontinuity: mediafacts.DiscontinuityRecord{
					Scope:          discScope,
					TrackPID:       trackPID,
					Reason:         discReason,
					ObservedAt:     observedAt,
					HasEpochBefore: flags&TimingDiscontinuityFlagHasEpochBefore != 0,
					EpochBefore:    mediafacts.TimelineEpoch(epochBefore),
					HasEpochAfter:  flags&TimingDiscontinuityFlagHasEpochAfter != 0,
					EpochAfter:     mediafacts.TimelineEpoch(epochAfter),
				},
			})

		default:
			return mediafacts.TimingResult{}, fmt.Errorf("%w: unknown timing record type %d", mediafacts.ErrCoreInvalidResponse, recType)
		}
	}

	return mediafacts.TimingResult{
		Authority:      mediafacts.TimingAuthorityCanonical,
		HasActiveEpoch: hasActiveEpoch != 0,
		ActiveEpoch:    mediafacts.TimelineEpoch(activeEpoch),
		Records:        records,
	}, nil
}

func decodeEvents(r *reader) ([]mediafacts.Event, error) {
	n, err := boundedCount(r, wireEventSize, "events")
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	out := make([]mediafacts.Event, 0, n)
	for i := 0; i < n; i++ {
		kind, ok := r.uint8()
		if !ok {
			return nil, short("event kind")
		}
		offset, ok := r.uint64()
		if !ok {
			return nil, short("event offset")
		}
		if offset > math.MaxInt64 {
			return nil, fmt.Errorf("%w: event offset %d", mediafacts.ErrCoreInvalidResponse, offset)
		}
		flags, ok := r.uint8()
		if !ok {
			return nil, short("event flags")
		}
		switch kind {
		case wireEventProgramIdentityChanged:
			if offset != 0 || flags != 0 {
				return nil, fmt.Errorf("%w: program identity changed event has offset %d flags %#02x",
					mediafacts.ErrCoreInvalidResponse, offset, flags)
			}
			out = append(out, mediafacts.Event{
				Kind: mediafacts.EventProgramIdentityChanged,
			})
		case wireEventRandomAccessPoint:
			if flags&^wireEventJoinable != 0 {
				return nil, fmt.Errorf("%w: random access point event flags %#02x",
					mediafacts.ErrCoreInvalidResponse, flags)
			}
			out = append(out, mediafacts.Event{
				Kind:     mediafacts.EventRandomAccessPoint,
				Offset:   int64(offset),
				Joinable: flags&wireEventJoinable != 0,
			})
		case wireEventRandomAccessPointInvalidated:
			if flags != 0 {
				return nil, fmt.Errorf("%w: random access point invalidated event flags %#02x",
					mediafacts.ErrCoreInvalidResponse, flags)
			}
			out = append(out, mediafacts.Event{
				Kind:   mediafacts.EventRandomAccessPointInvalidated,
				Offset: int64(offset),
			})
		default:
			return nil, fmt.Errorf("%w: event kind %d", mediafacts.ErrCoreInvalidResponse, kind)
		}
	}
	return out, nil
}

func decodeFacts(r *reader) (mediafacts.Facts, error) {
	var f mediafacts.Facts

	flags, ok := r.uint8()
	if !ok {
		return f, short("facts flags")
	}
	if flags&^(wireFactHasPAT|wireFactHasPMT) != 0 {
		return f, fmt.Errorf("%w: facts flags %#02x", mediafacts.ErrCoreInvalidResponse, flags)
	}
	f.HasPAT = flags&wireFactHasPAT != 0
	f.HasPMT = flags&wireFactHasPMT != 0

	if f.PMTVersion, ok = r.uint8(); !ok {
		return f, short("PMT version")
	}
	if f.ProgramNumber, ok = r.uint16(); !ok {
		return f, short("program number")
	}
	if f.PMTPID, ok = r.uint16(); !ok {
		return f, short("PMT PID")
	}
	if f.VideoPID, ok = r.uint16(); !ok {
		return f, short("video PID")
	}

	rawCodec, ok := r.uint8()
	if !ok {
		return f, short("video codec")
	}
	switch rawCodec {
	case wireVideoCodecUnknown:
		f.VideoCodec = mediafacts.CodecUnknown
	case wireVideoCodecH264:
		f.VideoCodec = mediafacts.CodecH264
	case wireVideoCodecH265:
		f.VideoCodec = mediafacts.CodecH265
	case wireVideoCodecMPEG2:
		f.VideoCodec = mediafacts.CodecMPEG2
	default:
		return f, fmt.Errorf("%w: video codec %d", mediafacts.ErrCoreInvalidResponse, rawCodec)
	}

	var err error
	if f.AudioPIDs, err = decodeAudioPIDs(r); err != nil {
		return f, err
	}
	f.Scrambling.AudioPIDs = append([]uint16(nil), f.AudioPIDs...)
	if f.AudioTracks, err = decodeAudioTracks(r); err != nil {
		return f, err
	}

	// 81-byte Video Facts Block
	videoFlags, ok := r.uint8()
	if !ok {
		return f, short("video facts flags")
	}
	if videoFlags&^(wireVideoFactParameterSetsSeen|wireVideoFactScrambledConfirmed) != 0 {
		return f, fmt.Errorf("%w: video facts flags %#02x", mediafacts.ErrCoreInvalidResponse, videoFlags)
	}
	f.ParameterSetsSeen = videoFlags&wireVideoFactParameterSetsSeen != 0
	f.ScrambledVideoConfirmed = videoFlags&wireVideoFactScrambledConfirmed != 0

	if f.CleanEntryPoints, ok = r.uint64(); !ok {
		return f, short("clean entry points")
	}
	if f.CleanAccessUnits, ok = r.uint64(); !ok {
		return f, short("clean access units")
	}
	if f.RandomAccess.IRAPPoints, ok = r.uint64(); !ok {
		return f, short("IRAP points")
	}
	if f.RandomAccess.IntraPoints, ok = r.uint64(); !ok {
		return f, short("intra points")
	}
	if f.RandomAccess.RecoveryPointSEIs, ok = r.uint64(); !ok {
		return f, short("recovery point SEIs")
	}
	if f.RandomAccess.PredictedRejected, ok = r.uint64(); !ok {
		return f, short("predicted rejected")
	}
	if f.RandomAccess.UnreadableSlices, ok = r.uint64(); !ok {
		return f, short("unreadable slices")
	}
	if f.Scrambling.VideoScrambled, ok = r.uint64(); !ok {
		return f, short("video scrambled packets")
	}
	if f.Scrambling.VideoClear, ok = r.uint64(); !ok {
		return f, short("video clear packets")
	}
	if f.Scrambling.VideoClearRun, ok = r.uint64(); !ok {
		return f, short("video clear run")
	}

	// 24-byte Audio Scrambling Block
	if f.Scrambling.AudioScrambled, ok = r.uint64(); !ok {
		return f, short("audio scrambled packets")
	}
	if f.Scrambling.AudioClear, ok = r.uint64(); !ok {
		return f, short("audio clear packets")
	}
	if f.Scrambling.AudioClearRun, ok = r.uint64(); !ok {
		return f, short("audio clear run")
	}

	return f, nil
}

func decodeAudioPIDs(r *reader) ([]uint16, error) {
	n, err := boundedCount(r, wireAudioPIDSize, "audio PIDs")
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	out := make([]uint16, 0, n)
	for i := 0; i < n; i++ {
		pid, ok := r.uint16()
		if !ok {
			return nil, short("audio PID")
		}
		out = append(out, pid)
	}
	return out, nil
}

func decodeAudioTracks(r *reader) ([]mediafacts.AudioTrackInfo, error) {
	n, err := boundedCount(r, wireAudioTrackSize, "audio tracks")
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	out := make([]mediafacts.AudioTrackInfo, 0, n)
	for i := 0; i < n; i++ {
		var t mediafacts.AudioTrackInfo
		var ok bool
		if t.PID, ok = r.uint16(); !ok {
			return nil, short("track PID")
		}
		if t.StreamType, ok = r.uint8(); !ok {
			return nil, short("track stream type")
		}
		rawCodec, ok := r.uint8()
		if !ok {
			return nil, short("track codec")
		}
		switch rawCodec {
		case wireAudioCodecUnknown:
			t.Codec = "unknown"
		case wireAudioCodecMP2:
			t.Codec = "mp2"
		case wireAudioCodecAAC:
			t.Codec = "aac"
		case wireAudioCodecAC3:
			t.Codec = "ac3"
		case wireAudioCodecEAC3:
			t.Codec = "eac3"
		case wireAudioCodecDTS:
			t.Codec = "dts"
		default:
			return nil, fmt.Errorf("%w: audio codec %d", mediafacts.ErrCoreInvalidResponse, rawCodec)
		}

		// Three bytes, always. A length prefix here would be a length the peer
		// chooses for a field whose length the syntax already fixes: a language
		// is the three from the descriptor, or the three of "und".
		lang, ok := r.bytes(wireLanguageSize)
		if !ok {
			return nil, short("track language")
		}
		t.Language = string(lang)

		channels, ok := r.uint8()
		if !ok {
			return nil, short("track channel count")
		}
		t.Declared.Channels = int(channels)

		flags, ok := r.uint8()
		if !ok {
			return nil, short("track flags")
		}
		if flags&^(wireTrackMultichannel|wireTrackHasComponentType) != 0 {
			return nil, fmt.Errorf("%w: track flags %#02x", mediafacts.ErrCoreInvalidResponse, flags)
		}
		t.Declared.Multichannel = flags&wireTrackMultichannel != 0
		t.Declared.HasComponentType = flags&wireTrackHasComponentType != 0

		if t.Declared.ComponentType, ok = r.uint8(); !ok {
			return nil, short("track component type")
		}

		// 11-byte Audio Observation: channels (1), flags (1), acmod (1), frames (8)
		obsChannels, ok := r.uint8()
		if !ok {
			return nil, short("track observed channels")
		}
		t.Observed.Channels = int(obsChannels)

		obsFlags, ok := r.uint8()
		if !ok {
			return nil, short("track observed flags")
		}
		if obsFlags&^(wireObsFlagLFE|wireObsFlagHasACMod|wireObsFlagDependentSubstream) != 0 {
			return nil, fmt.Errorf("%w: track observed flags %#02x", mediafacts.ErrCoreInvalidResponse, obsFlags)
		}
		t.Observed.LFE = obsFlags&wireObsFlagLFE != 0
		t.Observed.HasAcmod = obsFlags&wireObsFlagHasACMod != 0
		t.Observed.DependentSubstream = obsFlags&wireObsFlagDependentSubstream != 0

		obsAcmod, ok := r.uint8()
		if !ok {
			return nil, short("track observed acmod")
		}
		t.Observed.Acmod = obsAcmod

		obsFrames, ok := r.uint64()
		if !ok {
			return nil, short("track observed frames")
		}
		t.Observed.Frames = obsFrames

		out = append(out, t)
	}
	return out, nil
}

func decodeActivePSI(r *reader) (mediafacts.ActivePSI, error) {
	var psi mediafacts.ActivePSI
	var err error
	if psi.PATSections, err = decodeSections(r, "PAT"); err != nil {
		return mediafacts.ActivePSI{}, err
	}
	if psi.PMTSections, err = decodeSections(r, "PMT"); err != nil {
		return mediafacts.ActivePSI{}, err
	}
	return psi, nil
}

// decodeSections reads one table, held to the bounds R8 derived.
//
// A section is at most 1024 bytes because ISO/IEC 13818-1 caps section_length at
// 1021, and a table has at most 256 sections because section_number is a byte.
// Both are checked before anything is copied, and the running total is checked
// as it grows: three separate claims a failing peer could make, each refused on
// its own terms rather than by whatever happens to run out first.
func decodeSections(r *reader, which string) ([][]byte, error) {
	rawCount, ok := r.uint16()
	if !ok {
		return nil, short(which + " section count")
	}
	n := int(rawCount)
	if n > mediafacts.MaxSectionsPerTable {
		return nil, fmt.Errorf("%w: %d %s sections, more than a table may have",
			mediafacts.ErrCoreInvalidResponse, n, which)
	}
	if n*wireSectionMinSize > r.left() {
		return nil, fmt.Errorf("%w: %d %s sections do not fit in %d remaining bytes",
			mediafacts.ErrCoreInvalidResponse, n, which, r.left())
	}
	if n == 0 {
		return nil, nil
	}

	out := make([][]byte, 0, n)
	total := 0
	for i := 0; i < n; i++ {
		rawLen, ok := r.uint16()
		if !ok {
			return nil, short(which + " section length")
		}
		length := int(rawLen)
		if length < wireSectionHeaderBytes || length > mediafacts.MaxSectionBytes {
			return nil, fmt.Errorf("%w: a %s section of %d bytes, which no section may be",
				mediafacts.ErrCoreInvalidResponse, which, length)
		}
		total += length
		if total > maxActiveTableBytes {
			return nil, fmt.Errorf("%w: %s sections total %d bytes, past what a table may be",
				mediafacts.ErrCoreInvalidResponse, which, total)
		}
		section, ok := r.bytes(length)
		if !ok {
			return nil, short(which + " section")
		}
		// Copied: the frame buffer is reused between requests, and a table the
		// caller holds must not change when the next answer arrives.
		out = append(out, append([]byte(nil), section...))
	}
	return out, nil
}

// decodeTimingPoint reads the body shared by PES and RAP timing records: epoch,
// pid, flags, observed_at, subject_at, pts, dts. kind names the record in errors.
func decodeTimingPoint(r *reader, kind string) (mediafacts.TimingPoint, error) {
	epoch, ok := r.uint64()
	if !ok {
		return mediafacts.TimingPoint{}, short("timing " + kind + " epoch")
	}
	pid, ok := r.uint16()
	if !ok {
		return mediafacts.TimingPoint{}, short("timing " + kind + " PID")
	}
	if pid > 0x1FFF {
		return mediafacts.TimingPoint{}, fmt.Errorf("%w: timing %s PID %d exceeds 13-bit limit (0x1FFF)", mediafacts.ErrCoreInvalidResponse, kind, pid)
	}
	flags, ok := r.uint8()
	if !ok {
		return mediafacts.TimingPoint{}, short("timing " + kind + " flags")
	}
	if flags&^(TimingPesFlagHasPTS|TimingPesFlagHasDTS) != 0 {
		return mediafacts.TimingPoint{}, fmt.Errorf("%w: timing %s flags %#02x", mediafacts.ErrCoreInvalidResponse, kind, flags)
	}
	observedAt, ok := r.int64()
	if !ok {
		return mediafacts.TimingPoint{}, short("timing " + kind + " observed_at")
	}
	if observedAt < 0 {
		return mediafacts.TimingPoint{}, fmt.Errorf("%w: timing %s negative observed_at %d", mediafacts.ErrCoreInvalidResponse, kind, observedAt)
	}
	subjectAt, ok := r.int64()
	if !ok {
		return mediafacts.TimingPoint{}, short("timing " + kind + " subject_at")
	}
	if subjectAt < 0 {
		return mediafacts.TimingPoint{}, fmt.Errorf("%w: timing %s negative subject_at %d", mediafacts.ErrCoreInvalidResponse, kind, subjectAt)
	}
	pts90k, ok := r.int64()
	if !ok {
		return mediafacts.TimingPoint{}, short("timing " + kind + " pts_90k")
	}
	dts90k, ok := r.int64()
	if !ok {
		return mediafacts.TimingPoint{}, short("timing " + kind + " dts_90k")
	}

	// Canonical zero rules
	if flags&TimingPesFlagHasPTS == 0 && pts90k != 0 {
		return mediafacts.TimingPoint{}, fmt.Errorf("%w: timing %s pts_90k must be 0 when has_pts is false, got %d", mediafacts.ErrCoreInvalidResponse, kind, pts90k)
	}
	if flags&TimingPesFlagHasDTS == 0 && dts90k != 0 {
		return mediafacts.TimingPoint{}, fmt.Errorf("%w: timing %s dts_90k must be 0 when has_dts is false, got %d", mediafacts.ErrCoreInvalidResponse, kind, dts90k)
	}

	return mediafacts.TimingPoint{
		Epoch:      mediafacts.TimelineEpoch(epoch),
		PID:        pid,
		HasPTS:     flags&TimingPesFlagHasPTS != 0,
		PTS90k:     pts90k,
		HasDTS:     flags&TimingPesFlagHasDTS != 0,
		DTS90k:     dts90k,
		ObservedAt: observedAt,
		SubjectAt:  subjectAt,
	}, nil
}
