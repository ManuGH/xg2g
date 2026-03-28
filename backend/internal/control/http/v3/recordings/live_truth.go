package recordings

import (
	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
)

// resolveLiveTruth prefers persisted scan truth and falls back to the legacy
// structural assumption when that truth is unavailable or incomplete.
func resolveLiveTruth(serviceRef string, source ChannelTruthSource) playback.MediaTruth {
	if source == nil {
		metrics.IncLiveTruthSource("fallback", "scanner_unavailable")
		return fallbackLiveTruth(serviceRef, "scanner_unavailable", scan.Capability{})
	}

	cap, found := source.GetCapability(serviceRef)
	if !found {
		metrics.IncLiveTruthSource("fallback", "missing_scan_truth")
		return fallbackLiveTruth(serviceRef, "missing_scan_truth", scan.Capability{})
	}
	if cap.IsInactiveEventFeed() {
		metrics.IncLiveTruthSource("fallback", "inactive_event_feed")
		return fallbackLiveTruth(serviceRef, "inactive_event_feed", cap)
	}
	if !cap.HasMediaTruth() {
		metrics.IncLiveTruthSource("fallback", "incomplete_scan_truth")
		return fallbackLiveTruth(serviceRef, "incomplete_scan_truth", cap)
	}

	metrics.IncLiveTruthSource("scan", "cache_hit")
	log.L().Debug().
		Str("event", "live.truth_from_scan").
		Str("serviceRef", serviceRef).
		Str("container", cap.Container).
		Str("videoCodec", cap.VideoCodec).
		Str("audioCodec", cap.AudioCodec).
		Msg("Using persisted scan truth for live playback")

	return playback.MediaTruth{
		Status:     playback.MediaStatusReady,
		Container:  cap.Container,
		VideoCodec: cap.VideoCodec,
		AudioCodec: cap.AudioCodec,
		Width:      cap.Width,
		Height:     cap.Height,
		FPS:        cap.FPS,
		Interlaced: cap.Interlaced,
	}
}

// fallbackLiveTruth keeps the old live assumption, but marks it explicitly in
// logs and metrics so operators can see where scan truth is still missing.
func fallbackLiveTruth(serviceRef string, reason string, cap scan.Capability) playback.MediaTruth {
	log.L().Info().
		Str("event", "live.truth_fallback").
		Str("serviceRef", serviceRef).
		Str("reason", reason).
		Str("scanState", string(cap.State)).
		Str("scanContainer", cap.Container).
		Str("scanVideoCodec", cap.VideoCodec).
		Str("scanAudioCodec", cap.AudioCodec).
		Str("scanResolution", cap.Resolution).
		Str("scanCodec", cap.Codec).
		Msg("Falling back to legacy live truth assumption")

	return playback.MediaTruth{
		Status:     playback.MediaStatusReady,
		Container:  "mpegts",
		VideoCodec: "h264",
		AudioCodec: "aac",
	}
}
