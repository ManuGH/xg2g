// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"errors"
	"io"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
	"github.com/ManuGH/xg2g/internal/stream/timeline"
)

const (
	// MaxWindowBytes limits the maximum transport stream payload that can be extracted
	// in a single ExtractWindow call under MasterRing.mu, protecting against unbounded allocations
	// and lock contention.
	MaxWindowBytes = 16 * 1024 * 1024 // 16 MiB
)

var (
	ErrNoTimeline                       = errors.New("timeline index not enabled on ring")
	ErrEpochNotFound                    = errors.New("requested epoch not found in ring")
	ErrEpochNoTiming                    = errors.New("requested epoch has no timing records")
	ErrPTSOutOfRange                    = errors.New("requested PTS is outside retained presentation range")
	ErrNoMatchingRAP                    = errors.New("no matching random access point found")
	ErrTopologyUnresolved               = errors.New("active stream topology is unresolved")
	ErrHistoricalProgramSeekUnsupported = errors.New("seeking into historical program identity is unsupported")
	ErrNoCanonicalEndBoundary           = errors.New("no canonical end boundary found in epoch")
	ErrWindowTooLarge                   = errors.New("window extraction exceeds maximum byte limit")
	ErrWindowSpansMultipleEpochs        = errors.New("window extraction spans multiple epochs or program discontinuity")
	ErrInvalidWindowRange               = errors.New("invalid window PTS range")
)

// WindowRequest defines a live window extraction request.
type WindowRequest struct {
	Epoch           mediafacts.TimelineEpoch
	StartPTS        int64
	EndPTS          int64
	IncludePreamble bool
}

// WindowSlice contains the packet-aligned raw transport stream slice between two RAPs.
type WindowSlice struct {
	Epoch             mediafacts.TimelineEpoch
	RequestedStartPTS int64
	RequestedEndPTS   int64
	ActualStartPTS90k int64
	ActualEndPTS90k   int64
	StartOffset       int64
	EndOffset         int64
	StartRAP          timeline.RAPEntry
	EndRAP            timeline.RAPEntry
	Preamble          []byte
	Data              []byte
	PacketCount       int
}

// ExtractWindow atomically reads a contiguous window of TS packets between StartRAP and EndRAP.
// It returns a packet-aligned raw transport stream slice [StartRAP.Offset, EndRAP.Offset).
// It fails closed if active topology is unresolved when preamble is requested, if EndRAP is missing,
// or if limits are exceeded.
func (r *MasterRing) ExtractWindow(req WindowRequest) (WindowSlice, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.isClosed {
		return WindowSlice{}, ErrRingClosed
	}
	if r.timelineIndex == nil {
		return WindowSlice{}, ErrNoTimeline
	}

	if req.IncludePreamble {
		if !r.facts.HasPMT {
			return WindowSlice{}, ErrTopologyUnresolved
		}
		preamble := r.patpmtPreambleLocked()
		if len(preamble) == 0 {
			return WindowSlice{}, ErrTopologyUnresolved
		}
	}

	if req.StartPTS > req.EndPTS {
		return WindowSlice{}, ErrInvalidWindowRange
	}

	tail := r.store.tailOffset()
	head := r.store.headOffset()

	// 1. Resolve StartRAP (joinable, preceding StartPTS in req.Epoch)
	startRAP, ok := r.timelineIndex.FindRAPByTime(req.Epoch, req.StartPTS, timeline.SeekOptions{
		Mode:         timeline.SeekModePreceding,
		JoinableOnly: true,
	})
	if !ok {
		// Distinguish out-of-range PTS from general no RAP
		raps := r.timelineIndex.RAPsBetween(tail, head-1)
		var earliestPTS, latestPTS int64
		var hasAny bool
		for _, rap := range raps {
			if rap.Epoch == req.Epoch && rap.HasPTS {
				if !hasAny {
					hasAny = true
					earliestPTS = rap.PTS90k
					latestPTS = rap.PTS90k
				} else {
					if rap.PTS90k < earliestPTS {
						earliestPTS = rap.PTS90k
					}
					if rap.PTS90k > latestPTS {
						latestPTS = rap.PTS90k
					}
				}
			}
		}
		if hasAny && (req.StartPTS < earliestPTS || req.StartPTS > latestPTS) {
			return WindowSlice{}, ErrPTSOutOfRange
		}
		return WindowSlice{}, ErrNoMatchingRAP
	}

	if startRAP.Offset < tail || startRAP.Offset >= head {
		return WindowSlice{}, ErrNoMatchingRAP
	}

	// 2. Resolve EndRAP strictly in monotonic byte order
	allRAPs := r.timelineIndex.RAPsBetween(startRAP.Offset+1, head)
	var endRAP timeline.RAPEntry
	var foundEnd bool
	for _, rap := range allRAPs {
		if rap.Epoch == req.Epoch && rap.Offset > startRAP.Offset && rap.HasPTS && rap.PTS90k >= req.EndPTS {
			endRAP = rap
			foundEnd = true
			break
		}
	}

	if !foundEnd {
		return WindowSlice{}, ErrNoCanonicalEndBoundary
	}

	if endRAP.Offset > head {
		return WindowSlice{}, ErrNoCanonicalEndBoundary
	}

	// 3. Verify no program discontinuity exists strictly between StartRAP and EndRAP
	if endRAP.Offset > startRAP.Offset+1 {
		discs := r.timelineIndex.DiscontinuitiesBetween(startRAP.Offset+1, endRAP.Offset-1)
		for _, d := range discs {
			if d.Scope == mediafacts.DiscontinuityScopeProgram {
				return WindowSlice{}, ErrWindowSpansMultipleEpochs
			}
		}
	}

	// 4. Memory ceiling check
	byteLen := endRAP.Offset - startRAP.Offset
	if byteLen <= 0 {
		return WindowSlice{}, ErrInvalidWindowRange
	}
	if byteLen > MaxWindowBytes {
		return WindowSlice{}, ErrWindowTooLarge
	}

	// 5. Infallible read from packetStore under r.mu
	buf := make([]byte, byteLen)
	n, _, err := r.store.readAt(buf, startRAP.Offset)
	if err != nil {
		return WindowSlice{}, err
	}
	if int64(n) != byteLen {
		return WindowSlice{}, io.ErrUnexpectedEOF
	}

	res := WindowSlice{
		Epoch:             req.Epoch,
		RequestedStartPTS: req.StartPTS,
		RequestedEndPTS:   req.EndPTS,
		ActualStartPTS90k: startRAP.PTS90k,
		ActualEndPTS90k:   endRAP.PTS90k,
		StartOffset:       startRAP.Offset,
		EndOffset:         endRAP.Offset,
		StartRAP:          startRAP,
		EndRAP:            endRAP,
		Data:              buf,
		PacketCount:       len(buf) / TSPacketSize,
	}

	if req.IncludePreamble {
		res.Preamble = r.patpmtPreambleLocked()
	}

	return res, nil
}
