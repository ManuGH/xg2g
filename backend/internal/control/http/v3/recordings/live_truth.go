package recordings

import (
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
)

type liveTruthState string

const (
	liveTruthStateVerified          liveTruthState = "verified"
	liveTruthStatePartial           liveTruthState = "partial"
	liveTruthStateFailed            liveTruthState = "failed"
	liveTruthStateInactiveEventFeed liveTruthState = "inactive_event_feed"
	liveTruthStateUnverified        liveTruthState = "unverified"
)

const (
	liveTruthOriginScan       = "live_scan"
	liveTruthOriginUnverified = "live_unverified"
)

const liveTruthFreshnessWindow = 2 * time.Hour

type liveTruthResolution struct {
	Truth        playback.MediaTruth
	Origin       string
	State        liveTruthState
	Reason       string
	ProblemFlags []string
}

func (r liveTruthResolution) Verified() bool {
	return r.State == liveTruthStateVerified
}

func resolveLiveTruthState(serviceRef string, source ChannelTruthSource) liveTruthResolution {
	now := time.Now().UTC()
	if source == nil {
		return unverifiedLiveTruth(serviceRef, liveTruthStateUnverified, "scanner_unavailable", scan.Capability{}, []string{
			"live_truth_unverified",
			"scanner_unavailable",
		})
	}

	cap, found := source.GetCapability(serviceRef)
	return resolveLiveTruthCapability(serviceRef, cap, found, now)
}

func resolveLiveTruthCapability(serviceRef string, cap scan.Capability, found bool, now time.Time) liveTruthResolution {
	if !found {
		return unverifiedLiveTruth(serviceRef, liveTruthStateUnverified, "missing_scan_truth", scan.Capability{}, []string{
			"live_truth_unverified",
			"missing_scan_truth",
		})
	}

	normalized := cap.Normalized()
	if normalized.IsInactiveEventFeed() {
		return unverifiedLiveTruth(serviceRef, liveTruthStateInactiveEventFeed, "inactive_event_feed", normalized, []string{
			"live_truth_unverified",
			"inactive_event_feed",
		})
	}
	if normalized.HasMediaTruth() {
		if !liveTruthFreshEnough(normalized, now) {
			return unverifiedLiveTruth(serviceRef, liveTruthStateUnverified, "stale_scan_truth", normalized, []string{
				"live_truth_unverified",
				"stale_scan_truth",
			})
		}
		metrics.IncLiveTruthSource("scan", "cache_hit")
		log.L().Debug().
			Str("event", "live.truth_verified").
			Str("serviceRef", serviceRef).
			Str("container", normalized.Container).
			Str("videoCodec", normalized.VideoCodec).
			Str("audioCodec", normalized.AudioCodec).
			Msg("Using persisted scan truth for live playback")
		return liveTruthResolution{
			Truth:  liveTruthFromCapability(normalized, playback.MediaStatusReady),
			Origin: liveTruthOriginScan,
			State:  liveTruthStateVerified,
			Reason: "cache_hit",
		}
	}

	state := liveTruthStateUnverified
	reason := "incomplete_scan_truth"
	flags := []string{"live_truth_unverified", "incomplete_scan_truth"}
	switch normalized.State {
	case scan.CapabilityStatePartial:
		state = liveTruthStatePartial
		reason = "partial_scan_truth"
		flags = append(flags, "partial_scan_truth")
	case scan.CapabilityStateFailed:
		state = liveTruthStateFailed
		reason = "failed_scan_truth"
		flags = append(flags, "failed_scan_truth")
	}
	if strings.TrimSpace(normalized.FailureReason) != "" {
		flags = append(flags, "scan_failure_reason_set")
	}
	return unverifiedLiveTruth(serviceRef, state, reason, normalized, flags)
}

func liveTruthFromCapability(cap scan.Capability, status playback.MediaStatus) playback.MediaTruth {
	normalized := cap.Normalized()
	return playback.MediaTruth{
		Status:              status,
		Container:           normalized.Container,
		VideoCodec:          normalized.VideoCodec,
		AudioCodec:          normalized.AudioCodec,
		BitrateKbps:         normalized.StableBitrateKbps(),
		BitrateObservedKbps: normalized.BitrateKbps,
		BitratePeakKbps:     normalized.BitratePeakKbps,
		BitrateSamples:      normalized.BitrateSamples,
		BitrateConfidence:   normalized.BitrateConfidence(),
		Width:               normalized.Width,
		Height:              normalized.Height,
		FPS:                 normalized.FPS,
		SignalFPS:           normalized.SignalFPS,
		Interlaced:          normalized.Interlaced,
		FieldOrder:          normalized.FieldOrder,
		AudioChannels:       normalized.AudioChannels,
		AudioBitrateKbps:    normalized.AudioBitrateKbps,
		AudioSampleRate:     normalized.AudioSampleRate,
		AudioChannelLayout:  normalized.AudioChannelLayout,
	}
}

func liveTruthFreshEnough(cap scan.Capability, now time.Time) bool {
	anchor := cap.LastSuccess
	if anchor.IsZero() {
		anchor = cap.LastScan
	}
	if anchor.IsZero() {
		return true
	}
	return now.Sub(anchor) <= liveTruthFreshnessWindow
}

func unverifiedLiveTruth(serviceRef string, state liveTruthState, reason string, cap scan.Capability, flags []string) liveTruthResolution {
	metrics.IncLiveTruthSource("unverified", reason)
	log.L().Info().
		Str("event", "live.truth_unverified").
		Str("serviceRef", serviceRef).
		Str("state", string(state)).
		Str("reason", reason).
		Str("scanState", string(cap.State)).
		Str("scanContainer", cap.Container).
		Str("scanVideoCodec", cap.VideoCodec).
		Str("scanAudioCodec", cap.AudioCodec).
		Str("scanResolution", cap.Resolution).
		Str("scanCodec", cap.Codec).
		Msg("Live playback truth is unavailable or incomplete")
	return liveTruthResolution{
		Truth:        liveTruthFromCapability(cap, playback.MediaStatusUnverified),
		Origin:       liveTruthOriginUnverified,
		State:        state,
		Reason:       reason,
		ProblemFlags: append([]string(nil), flags...),
	}
}
