// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

// Detail strings for a failed attach. The reason taxonomy in ports is bounded and
// already part of the v3 contract, so these carry the finer truth in the result's
// Detail field rather than growing the enum.
const (
	detailSharedIngestUnavailable = "shared_ingest_unavailable"
	detailTopologyNotReady        = "topology_not_ready"
	detailNotJoinable             = "not_joinable"
	detailAttachTimeout           = "attach_timeout"
)

// preflightResultFromFacts explains a failed live attach in the taxonomy the
// playback trace already publishes.
//
// The old TS preflight opened its own connection to answer two questions - is
// this stream descrambled, and is there something a decoder could start on - and
// answered them by classifying a sample: a window size, a threshold, a fraction.
// The ring answers the same questions by counting what it has already seen, so
// what used to be an inference is now an observation. The reasons below are
// therefore fewer and each is meant literally.
//
// Reason values are reused, never invented: PreflightReason is a bounded taxonomy
// that reaches clients through PlaybackTrace.PreflightReason, and this is a change
// of how a value is derived, not of which values exist.
func preflightResultFromFacts(facts ports.LiveSourceFacts, attachErr error) ports.PreflightResult {
	result := ports.PreflightResult{OK: false}

	switch {
	case facts.ScrambledVideoPackets > 0 && facts.CleanAccessUnits == 0:
		// Measured, not inferred: not one complete picture has arrived without an
		// encrypted packet in it. That is the receiver failing to descramble the
		// service, which no amount of waiting on this side fixes.
		result.Reason = ports.PreflightReasonScrambled
		result.Detail = fmt.Sprintf("no clean access unit in %d scrambled and %d clear video packets",
			facts.ScrambledVideoPackets, facts.ClearVideoPackets)
		result.Scramble = ports.ScrambleEvidence{}

	case !facts.HasPAT || !facts.HasPMT:
		// Without PAT and PMT the stream has not said what it contains, so there is
		// nothing yet to call valid or invalid.
		result.Reason = ports.PreflightReasonInvalidTS
		result.Detail = detailTopologyNotReady

	case facts.VideoPID == 0:
		result.Reason = ports.PreflightReasonNoVideo
		result.Detail = "pmt names no video elementary stream"

	case !facts.Joinable():
		// The topology is known but nothing a decoder could be started on has
		// arrived: either the parameter sets have not been seen, or every entry
		// point so far carried a scrambled packet.
		result.Reason = ports.PreflightReasonInvalidTS
		result.Detail = joinabilityDetail(facts)

	case errors.Is(attachErr, context.DeadlineExceeded):
		result.Reason = ports.PreflightReasonTimeout
		result.Detail = detailAttachTimeout

	default:
		// The facts describe a stream that should have been joinable, so the attach
		// failed for a reason the ring cannot see - the upstream never started, or
		// it ended while waiting.
		result.Reason = ports.PreflightReasonUnreachable
		result.Detail = detailSharedIngestUnavailable
	}

	if attachErr != nil {
		if msg := strings.TrimSpace(attachErr.Error()); msg != "" {
			result.Detail = result.Detail + ": " + msg
		}
	}
	return result.Normalized()
}

// joinabilityDetail names which of the joinability criteria is missing, so a
// trace says what the stream is still waiting for rather than only that it is.
func joinabilityDetail(facts ports.LiveSourceFacts) string {
	missing := make([]string, 0, 2)
	if !facts.ParameterSetsSeen {
		missing = append(missing, "no parameter sets")
	}
	if facts.CleanEntryPoints == 0 {
		missing = append(missing, "no clean entry point")
	}
	if len(missing) == 0 {
		return detailNotJoinable
	}
	return detailNotJoinable + ": " + strings.Join(missing, ", ")
}

// preflightErrorFromFacts wraps the result so it reaches the orchestrator on the
// path that already maps a structured preflight failure into the playback trace.
func preflightErrorFromFacts(facts ports.LiveSourceFacts, attachErr error) error {
	return ports.NewPreflightError(preflightResultFromFacts(facts, attachErr))
}
