package recordings

import (
	"strings"
	"sync/atomic"
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

// defaultLiveTruthFreshnessWindow is the maximum age of a scan-probed capability
// entry before live playback truth considers it stale and re-verifies it. Media
// truth (container/codecs/resolution/fps) is stable per channel for weeks, so the
// previous 2h window forced a slow synchronous re-probe on essentially every cold
// start; on stream-relay / pay-TV channels that probe can take ~20s+ (it races the
// cold tune through several failing fallbacks) and then falls back to this very
// truth anyway, producing a stuttery, autoplay-killing start. 7 days re-verifies
// stale truth at a sane cadence while keeping warm cold starts fast.
const defaultLiveTruthFreshnessWindow = 7 * 24 * time.Hour

// liveTruthFreshnessWindow is the live-truth staleness threshold
// (see defaultLiveTruthFreshnessWindow). Overridable via SetLiveTruthFreshnessWindow.
var liveTruthFreshnessWindow atomic.Int64

func init() {
	liveTruthFreshnessWindow.Store(int64(defaultLiveTruthFreshnessWindow))
}

// SetLiveTruthFreshnessWindow allows operators to override the stale scan
// truth threshold at startup (e.g. from config/env). Must be called before
// the first live playback info request.
func SetLiveTruthFreshnessWindow(d time.Duration) {
	if d <= 0 {
		log.L().Warn().Dur("attempted", d).Msg("live_truth: ignoring non-positive freshness window, keeping default")
		return
	}
	liveTruthFreshnessWindow.Store(int64(d))
	log.L().Info().Dur("freshness_window", d).Msg("live_truth: stale scan truth threshold updated")
}

type liveTruthResolution struct {
	Truth        playback.MediaTruth
	Origin       string
	State        liveTruthState
	Reason       string
	ProblemFlags []string
	// Stale marks a verified resolution that was served from a scan-truth entry
	// older than the freshness window (stale-while-revalidate). The caller is
	// expected to trigger a detached background re-probe for interactive requests.
	Stale bool
}

func (r liveTruthResolution) Verified() bool {
	return r.State == liveTruthStateVerified
}

func resolveLiveTruthState(serviceRef string, source ChannelTruthSource, requestContext string) liveTruthResolution {
	now := time.Now().UTC()
	if source == nil {
		return unverifiedLiveTruth(serviceRef, liveTruthStateUnverified, "scanner_unavailable", scan.Capability{}, []string{
			"live_truth_unverified",
			"scanner_unavailable",
		}, requestContext)
	}

	cap, found := source.GetCapability(serviceRef)
	return resolveLiveTruthCapability(serviceRef, cap, found, now, requestContext)
}

func resolveLiveTruthCapability(serviceRef string, cap scan.Capability, found bool, now time.Time, requestContext string) liveTruthResolution {
	if !found {
		return unverifiedLiveTruth(serviceRef, liveTruthStateUnverified, "missing_scan_truth", scan.Capability{}, []string{
			"live_truth_unverified",
			"missing_scan_truth",
		}, requestContext)
	}

	normalized := cap.Normalized()
	if normalized.IsInactiveEventFeed() {
		return unverifiedLiveTruth(serviceRef, liveTruthStateInactiveEventFeed, "inactive_event_feed", normalized, []string{
			"live_truth_unverified",
			"inactive_event_feed",
		}, requestContext)
	}
	if normalized.HasMediaTruth() {
		if !liveTruthFreshEnough(normalized, now) {
			// Stale-while-revalidate: complete media truth (container/codecs/
			// resolution) is stable per channel for weeks, and the on-demand relay
			// probe can fail exactly when playback would succeed (a cold tune
			// yields ffprobe I/O errors until the descrambler locks). Blocking the
			// player behind a 503 here is therefore strictly worse than serving
			// the cached truth. Serve it immediately and mark the resolution
			// Stale; interactive callers revalidate via a detached background
			// probe (refreshLiveTruthAsync).
			metrics.IncLiveTruthSource("scan", "stale_cache_hit")
			evt := log.L().Info().
				Str("event", "live.truth_stale_served").
				Str("serviceRef", serviceRef).
				Str("container", normalized.Container).
				Str("videoCodec", normalized.VideoCodec).
				Str("audioCodec", normalized.AudioCodec)
			if requestContext != "" {
				evt = evt.Str("request_context", requestContext)
			}
			evt.Msg("Serving stale scan truth for live playback; revalidating in background")
			return liveTruthResolution{
				Truth:  liveTruthFromCapability(normalized, playback.MediaStatusReady),
				Origin: liveTruthOriginScan,
				State:  liveTruthStateVerified,
				Reason: "stale_cache_hit",
				Stale:  true,
			}
		}
		metrics.IncLiveTruthSource("scan", "cache_hit")
		evt := log.L().Debug().
			Str("event", "live.truth_verified").
			Str("serviceRef", serviceRef).
			Str("container", normalized.Container).
			Str("videoCodec", normalized.VideoCodec).
			Str("audioCodec", normalized.AudioCodec)
		if requestContext != "" {
			evt = evt.Str("request_context", requestContext)
		}
		if requestContext == PlaybackInfoContextEpgBadge {
			evt.Msg("Using persisted scan truth for live playback preview")
		} else {
			evt.Msg("Using persisted scan truth for live playback")
		}
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
	return unverifiedLiveTruth(serviceRef, state, reason, normalized, flags, requestContext)
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
	return now.Sub(anchor) <= time.Duration(liveTruthFreshnessWindow.Load())
}

func unverifiedLiveTruth(serviceRef string, state liveTruthState, reason string, cap scan.Capability, flags []string, requestContext string) liveTruthResolution {
	metrics.IncLiveTruthSource("unverified", reason)
	evt := log.L().Info().
		Str("event", "live.truth_unverified").
		Str("serviceRef", serviceRef).
		Str("state", string(state)).
		Str("reason", reason).
		Str("scanState", string(cap.State)).
		Str("scanContainer", cap.Container).
		Str("scanVideoCodec", cap.VideoCodec).
		Str("scanAudioCodec", cap.AudioCodec).
		Str("scanResolution", cap.Resolution).
		Str("scanCodec", cap.Codec)
	if requestContext != "" {
		evt = evt.Str("request_context", requestContext)
	}
	if requestContext == PlaybackInfoContextEpgBadge {
		evt.Msg("Live playback preview truth is unavailable or incomplete")
	} else {
		evt.Msg("Live playback truth is unavailable or incomplete")
	}
	return liveTruthResolution{
		Truth:        liveTruthFromCapability(cap, playback.MediaStatusUnverified),
		Origin:       liveTruthOriginUnverified,
		State:        state,
		Reason:       reason,
		ProblemFlags: append([]string(nil), flags...),
	}
}
