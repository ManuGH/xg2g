// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

type SessionPlaybackHealth string

const (
	SessionPlaybackHealthHealthy              SessionPlaybackHealth = "healthy"
	SessionPlaybackHealthEdgeFragile          SessionPlaybackHealth = "edge_fragile"
	SessionPlaybackHealthProducerSlow         SessionPlaybackHealth = "producer_slow"
	SessionPlaybackHealthSegmentStale         SessionPlaybackHealth = "segment_stale"
	SessionPlaybackHealthConsumerStalled      SessionPlaybackHealth = "consumer_stalled"
	SessionPlaybackHealthInsufficientEvidence SessionPlaybackHealth = "insufficient_evidence"
)

const (
	sessionHealthReasonPlaylistProgressObserved    = "playlist_progress_observed"
	sessionHealthReasonSegmentProgressObserved     = "segment_progress_observed"
	sessionHealthReasonPlaylistAdvancingNoSegments = "playlist_advancing_without_segments"
	sessionHealthReasonHighSegmentLag              = "high_segment_lag"
	sessionHealthReasonSegmentGapObserved          = "segment_gap_observed"
	sessionHealthReasonLastSegmentStale            = "last_segment_stale"
	sessionHealthReasonConsumerProgressMissing     = "consumer_progress_missing"
	sessionHealthReasonLowStartupHeadroom          = "low_startup_headroom"
	sessionHealthReasonClientFamilyWebkit          = "client_family_webkit"
	sessionHealthReasonInsufficientTraceDepth      = "insufficient_trace_depth"
)

const (
	sessionHealthLagWarningThreshold     = 4 * time.Second
	sessionHealthGapWarningThreshold     = 3 * time.Second
	sessionHealthMinTraceObservations    = 2
	sessionHealthConsumerConfidencePolls = 3
)

type SessionPlaybackHealthContext struct {
	ObservedAt   time.Time
	ClientFamily string
	SessionMode  string
}

type SessionPlaybackHealthEvidence struct {
	PlaylistRequests        int
	LastPlaylistAtMs        int64
	SegmentRequests         int
	LastSegmentAtMs         int64
	LastSegmentName         string
	LatestSegmentLagMs      int64
	LastSegmentGapMs        int64
	PlaylistAdvanced        bool
	SegmentProgressObserved bool
	StartupMode             string
	StartupHeadroomSec      int
	ClientFamily            string
}

type SessionPlaybackHealthAssessment struct {
	Health      SessionPlaybackHealth
	ReasonCodes []string
	Evidence    SessionPlaybackHealthEvidence
}

func DeriveSessionPlaybackHealth(
	trace *model.PlaybackTrace,
	ctx SessionPlaybackHealthContext,
) SessionPlaybackHealthAssessment {
	evidence := deriveSessionPlaybackHealthEvidence(trace, ctx)
	reasons := deriveSessionPlaybackHealthReasons(evidence)

	if hasInsufficientTraceDepth(evidence) {
		reasons = appendUniqueReason(reasons, sessionHealthReasonInsufficientTraceDepth)
		return SessionPlaybackHealthAssessment{
			Health:      SessionPlaybackHealthInsufficientEvidence,
			ReasonCodes: reasons,
			Evidence:    evidence,
		}
	}

	if isSegmentStaleHealth(evidence) {
		reasons = appendUniqueReason(reasons, sessionHealthReasonLastSegmentStale)
		return SessionPlaybackHealthAssessment{
			Health:      SessionPlaybackHealthSegmentStale,
			ReasonCodes: reasons,
			Evidence:    evidence,
		}
	}

	if isProducerSlowHealth(evidence) {
		return SessionPlaybackHealthAssessment{
			Health:      SessionPlaybackHealthProducerSlow,
			ReasonCodes: reasons,
			Evidence:    evidence,
		}
	}

	if isConsumerStalledHealth(evidence) {
		reasons = appendUniqueReason(reasons, sessionHealthReasonConsumerProgressMissing)
		return SessionPlaybackHealthAssessment{
			Health:      SessionPlaybackHealthConsumerStalled,
			ReasonCodes: reasons,
			Evidence:    evidence,
		}
	}

	if isEdgeFragileHealth(evidence) {
		return SessionPlaybackHealthAssessment{
			Health:      SessionPlaybackHealthEdgeFragile,
			ReasonCodes: reasons,
			Evidence:    evidence,
		}
	}

	if evidence.PlaylistAdvanced && evidence.SegmentProgressObserved {
		return SessionPlaybackHealthAssessment{
			Health:      SessionPlaybackHealthHealthy,
			ReasonCodes: reasons,
			Evidence:    evidence,
		}
	}

	reasons = appendUniqueReason(reasons, sessionHealthReasonInsufficientTraceDepth)
	return SessionPlaybackHealthAssessment{
		Health:      SessionPlaybackHealthInsufficientEvidence,
		ReasonCodes: reasons,
		Evidence:    evidence,
	}
}

func deriveSessionPlaybackHealthEvidence(
	trace *model.PlaybackTrace,
	ctx SessionPlaybackHealthContext,
) SessionPlaybackHealthEvidence {
	evidence := SessionPlaybackHealthEvidence{
		ClientFamily: strings.TrimSpace(ctx.ClientFamily),
	}
	if trace == nil || trace.HLS == nil {
		return evidence
	}

	hls := trace.HLS
	evidence.PlaylistRequests = hls.PlaylistRequestCount
	evidence.LastPlaylistAtMs = hls.LastPlaylistAtUnix * 1000
	evidence.SegmentRequests = hls.SegmentRequestCount
	evidence.LastSegmentAtMs = hls.LastSegmentAtUnix * 1000
	evidence.LastSegmentName = strings.TrimSpace(hls.LastSegmentName)
	evidence.LatestSegmentLagMs = int64(hls.LatestSegmentLagMs)
	evidence.LastSegmentGapMs = int64(hls.LastSegmentGapMs)
	evidence.PlaylistAdvanced = hls.PlaylistRequestCount > 0 && hls.LastPlaylistAtUnix > 0
	evidence.SegmentProgressObserved = hls.SegmentRequestCount > 0 && hls.LastSegmentAtUnix > 0
	evidence.StartupMode = strings.TrimSpace(hls.StartupMode)
	evidence.StartupHeadroomSec = hls.StartupHeadroomSec

	if evidence.ClientFamily == "" && trace.Client != nil {
		evidence.ClientFamily = strings.TrimSpace(trace.Client.ClientFamily)
	}
	return evidence
}

func deriveSessionPlaybackHealthReasons(evidence SessionPlaybackHealthEvidence) []string {
	reasons := make([]string, 0, 8)

	if evidence.PlaylistAdvanced {
		reasons = appendUniqueReason(reasons, sessionHealthReasonPlaylistProgressObserved)
	}
	if evidence.SegmentProgressObserved {
		reasons = appendUniqueReason(reasons, sessionHealthReasonSegmentProgressObserved)
	}
	if isPlaylistAdvancingWithoutSegments(evidence) {
		reasons = appendUniqueReason(reasons, sessionHealthReasonPlaylistAdvancingNoSegments)
	}
	if evidence.LastSegmentGapMs >= int64(sessionHealthGapWarningThreshold/time.Millisecond) {
		reasons = appendUniqueReason(reasons, sessionHealthReasonSegmentGapObserved)
	}
	if evidence.LatestSegmentLagMs >= int64(sessionHealthLagWarningThreshold/time.Millisecond) {
		reasons = appendUniqueReason(reasons, sessionHealthReasonHighSegmentLag)
	}
	if hasLowStartupHeadroom(evidence) {
		reasons = appendUniqueReason(reasons, sessionHealthReasonLowStartupHeadroom)
	}
	if isWebkitClientFamily(evidence.ClientFamily) {
		reasons = appendUniqueReason(reasons, sessionHealthReasonClientFamilyWebkit)
	}

	return reasons
}

func hasInsufficientTraceDepth(evidence SessionPlaybackHealthEvidence) bool {
	if evidence.PlaylistRequests+evidence.SegmentRequests < sessionHealthMinTraceObservations {
		return true
	}
	return !evidence.PlaylistAdvanced && !evidence.SegmentProgressObserved
}

func isPlaylistAdvancingWithoutSegments(evidence SessionPlaybackHealthEvidence) bool {
	return evidence.PlaylistRequests >= hlsPlaylistOnlyPollThreshold && evidence.SegmentRequests == 0
}

func isProducerSlowHealth(evidence SessionPlaybackHealthEvidence) bool {
	return evidence.LatestSegmentLagMs >= int64(hlsProducerSlowLag/time.Millisecond) &&
		evidence.LastSegmentGapMs < int64(hlsSegmentStaleAfter/time.Millisecond)
}

func isSegmentStaleHealth(evidence SessionPlaybackHealthEvidence) bool {
	return evidence.LastSegmentGapMs >= int64(hlsSegmentStaleAfter/time.Millisecond)
}

func isConsumerStalledHealth(evidence SessionPlaybackHealthEvidence) bool {
	return evidence.PlaylistRequests >= sessionHealthConsumerConfidencePolls &&
		isPlaylistAdvancingWithoutSegments(evidence) &&
		evidence.LatestSegmentLagMs < int64(sessionHealthLagWarningThreshold/time.Millisecond) &&
		evidence.LastSegmentGapMs < int64(hlsSegmentStaleAfter/time.Millisecond)
}

func isEdgeFragileHealth(evidence SessionPlaybackHealthEvidence) bool {
	if !evidence.PlaylistAdvanced {
		return false
	}
	if evidence.SegmentProgressObserved &&
		(evidence.LastSegmentGapMs >= int64(sessionHealthGapWarningThreshold/time.Millisecond) ||
			evidence.LatestSegmentLagMs >= int64(sessionHealthLagWarningThreshold/time.Millisecond) ||
			(isWebkitClientFamily(evidence.ClientFamily) && hasLowStartupHeadroom(evidence))) {
		return true
	}
	return isPlaylistAdvancingWithoutSegments(evidence) &&
		evidence.PlaylistRequests >= hlsPlaylistOnlyPollThreshold &&
		evidence.PlaylistRequests < sessionHealthConsumerConfidencePolls
}

func hasLowStartupHeadroom(evidence SessionPlaybackHealthEvidence) bool {
	if evidence.StartupHeadroomSec <= 0 {
		return false
	}
	if isWebkitClientFamily(evidence.ClientFamily) {
		return evidence.StartupHeadroomSec <= 8
	}
	return evidence.StartupHeadroomSec <= 6
}

func isWebkitClientFamily(clientFamily string) bool {
	clientFamily = strings.ToLower(strings.TrimSpace(clientFamily))
	return strings.Contains(clientFamily, "safari") || strings.Contains(clientFamily, "webkit")
}

func appendUniqueReason(reasons []string, code string) []string {
	code = strings.TrimSpace(code)
	if code == "" {
		return reasons
	}
	for _, existing := range reasons {
		if existing == code {
			return reasons
		}
	}
	return append(reasons, code)
}
