// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"fmt"
	"math"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// The PSI result envelope, protocol v3.
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

// wireEventProgramIdentityChanged is the only event kind a PSI-only core may
// report. A random access point is a statement about video payload, which is not
// in this coverage; a peer sending one is describing something it did not read.
const wireEventProgramIdentityChanged uint8 = 1

const (
	wireFactHasPAT uint8 = 1 << 0
	wireFactHasPMT uint8 = 1 << 1

	wireEventJoinable uint8 = 1 << 0

	wireTrackMultichannel     uint8 = 1 << 0
	wireTrackHasComponentType uint8 = 1 << 1
)

// Fixed sizes, named so the bound checks below read as arithmetic about the
// format rather than as magic numbers.
const (
	wireLanguageSize       = 3
	wireSectionHeaderBytes = 3
	wireEventSize          = 1 + 8 + 1
	wireAudioPIDSize       = 2
	wireAudioTrackSize     = 2 + 1 + 1 + wireLanguageSize + 1 + 1 + 1
	wireSectionMinSize     = 2
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

// decodePSIResult reads one v3 result envelope.
//
// The status byte is consumed here so the offsets in the layout above are the
// offsets in the frame. A body that does not end exactly where the last field
// ends is refused: trailing bytes are another message's shape, and a decoder
// that ignores them cannot tell a peer it agrees with from one it does not.
func decodePSIResult(body []byte) (mediafacts.ParseResult, error) {
	r := &reader{b: body}

	if _, ok := r.uint8(); !ok {
		return mediafacts.ParseResult{}, short("status")
	}

	rawCoverage, ok := r.uint8()
	if !ok {
		return mediafacts.ParseResult{}, short("coverage")
	}
	var parsed mediafacts.ParseResult
	switch rawCoverage {
	case wireCoveragePSIOnly:
		parsed.Coverage = mediafacts.ParseCoveragePSIOnly
	case wireCoverageComplete:
		// Refused rather than accepted. Nothing on the other side of this
		// protocol reads anything but PSI, so a peer claiming to cover the whole
		// stream is claiming something this build knows it cannot do - and that
		// claim is the one thing that would let the ring commit it.
		return mediafacts.ParseResult{}, fmt.Errorf(
			"%w: peer claims complete coverage, which no core on this protocol provides",
			mediafacts.ErrCoreInvalidResponse)
	default:
		return mediafacts.ParseResult{}, fmt.Errorf("%w: coverage %d",
			mediafacts.ErrCoreInvalidResponse, rawCoverage)
	}

	through, ok := r.uint64()
	if !ok {
		return mediafacts.ParseResult{}, short("processed-through offset")
	}
	if through > math.MaxInt64 {
		return mediafacts.ParseResult{}, fmt.Errorf("%w: answered with offset %d",
			mediafacts.ErrCoreInvalidResponse, through)
	}
	parsed.ProcessedThroughOffset = int64(through)

	var err error
	if parsed.Events, err = decodeEvents(r); err != nil {
		return mediafacts.ParseResult{}, err
	}
	if parsed.Facts, err = decodeFacts(r); err != nil {
		return mediafacts.ParseResult{}, err
	}
	if parsed.PSI, err = decodeActivePSI(r); err != nil {
		return mediafacts.ParseResult{}, err
	}

	if r.left() != 0 {
		return mediafacts.ParseResult{}, fmt.Errorf("%w: %d bytes after the result",
			mediafacts.ErrCoreInvalidResponse, r.left())
	}
	return parsed, nil
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
		if kind != wireEventProgramIdentityChanged {
			return nil, fmt.Errorf("%w: event kind %d", mediafacts.ErrCoreInvalidResponse, kind)
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
		if flags&^wireEventJoinable != 0 {
			return nil, fmt.Errorf("%w: event flags %#02x", mediafacts.ErrCoreInvalidResponse, flags)
		}
		out = append(out, mediafacts.Event{
			Kind:     mediafacts.EventProgramIdentityChanged,
			Offset:   int64(offset),
			Joinable: flags&wireEventJoinable != 0,
		})
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
	if f.AudioTracks, err = decodeAudioTracks(r); err != nil {
		return f, err
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
