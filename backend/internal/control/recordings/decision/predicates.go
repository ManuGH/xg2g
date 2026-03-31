package decision

import (
	"context"

	"github.com/ManuGH/xg2g/internal/log"
	mediacodec "github.com/ManuGH/xg2g/internal/media/codec"
)

// computePredicates evaluates all compatibility predicates (Section 6.2) while
// optionally emitting shadow-path telemetry for the typed migration.
func computePredicates(ctx context.Context, source Source, caps Capabilities, policy Policy) Predicates {
	// Element-wise compatibility checks
	// ADR-009.1 §1 Scope Cut: codec compatibility is string-only (no profile/level).
	// Exit condition: TruthProvider provides profile/level or RFC-6381, and Capabilities can express them.
	canContainer := contains(caps.Containers, source.Container)
	canVideo := contains(caps.VideoCodecs, source.VideoCodec) && withinMaxVideo(source, caps.MaxVideo)
	canAudio := contains(caps.AudioCodecs, source.AudioCodec)
	videoRepairRequired := sourceRequiresVideoRepair(source, caps.MaxVideo)
	shadowEvaluateVideoCompatibility(ctx, source, caps, canVideo, videoRepairRequired)

	// Direct play: client can play source container+codecs directly via static MP4/TS
	// AND Client MUST support Range requests (for seeking/progressive)
	// FIX R2-001: Normalize container to match contains() behavior
	containerNorm := robustNorm(source.Container)
	isDirectPlayableContainer := containerNorm == "mp4" || containerNorm == "mov" || containerNorm == "m4v" || containerNorm == "mpegts" || containerNorm == "ts"
	hasRange := caps.SupportsRange != nil && *caps.SupportsRange
	directPlayPossible := canContainer && canVideo && canAudio && isDirectPlayableContainer && hasRange && !videoRepairRequired

	// Direct stream: no re-encode, but may remux/package to HLS
	// Requires: HLS support + compatible codecs (container may differ)
	directStreamPossible := caps.SupportsHLS && canVideo && canAudio && !videoRepairRequired

	// Transcode needed: any incompatibility OR protocol gap (neither DP nor DS possible)
	transcodeNeeded := videoRepairRequired || !canVideo || !canAudio || (!directPlayPossible && !directStreamPossible)

	// Transcode possible: policy-gated + client must accept HLS output
	transcodePossible := policy.AllowTranscode && caps.SupportsHLS

	return Predicates{
		CanContainer:         canContainer,
		CanVideo:             canVideo,
		CanAudio:             canAudio,
		VideoRepairRequired:  videoRepairRequired,
		DirectPlayPossible:   directPlayPossible,
		DirectStreamPossible: directStreamPossible,
		TranscodeNeeded:      transcodeNeeded,
		TranscodePossible:    transcodePossible,
	}
}

func shadowEvaluateVideoCompatibility(ctx context.Context, source Source, caps Capabilities, legacyCanVideo bool, legacyVideoRepairRequired bool) {
	newResult := EvaluateVideoCompatibility(
		sourceToVideoCapability(source),
		clientToVideoCapabilityForSource(caps, source),
	)
	legacyCompatibleWithoutRepair := legacyCanVideo && !legacyVideoRepairRequired
	newCompatible := newResult.Compatible()
	if legacyCompatibleWithoutRepair == newCompatible {
		return
	}
	divergence := ShadowDivergence{
		Predicate:                     "video",
		LegacyCanVideo:                legacyCanVideo,
		LegacyVideoRepairRequired:     legacyVideoRepairRequired,
		LegacyCompatibleWithoutRepair: legacyCompatibleWithoutRepair,
		NewCompatible:                 newCompatible,
		NewReasons:                    compatibilityReasonStrings(newResult.Reasons),
	}
	if collector := shadowCollectorFromContext(ctx); collector != nil {
		collector.Add(divergence)
	}

	event := log.L().Warn().
		Str("event", "decision.predicate.video_divergence").
		Bool("legacyCanVideo", legacyCanVideo).
		Bool("legacyVideoRepairRequired", legacyVideoRepairRequired).
		Bool("legacyCompatibleWithoutRepair", legacyCompatibleWithoutRepair).
		Bool("newCompatible", newCompatible).
		Str("sourceContainer", source.Container).
		Str("sourceVideoCodec", source.VideoCodec).
		Int("sourceWidth", source.Width).
		Int("sourceHeight", source.Height).
		Float64("sourceFPS", source.FPS).
		Bool("sourceInterlaced", source.Interlaced).
		Strs("clientVideoCodecs", caps.VideoCodecs)

	if caps.MaxVideo != nil {
		event = event.
			Int("clientMaxWidth", caps.MaxVideo.Width).
			Int("clientMaxHeight", caps.MaxVideo.Height).
			Int("clientMaxFPS", caps.MaxVideo.FPS)
	}
	event.Strs("newReasons", divergence.NewReasons).
		Msg("typed video compatibility shadow path diverged from legacy predicate")
}

func compatibilityReasonStrings(reasons []mediacodec.CompatibilityReason) []string {
	if len(reasons) == 0 {
		return nil
	}
	out := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		if reason == "" {
			continue
		}
		out = append(out, string(reason))
	}
	return out
}

func sourceRequiresVideoRepair(source Source, maxVideo *MaxVideoDimensions) bool {
	if source.Interlaced {
		return true
	}
	if maxVideo == nil {
		return false
	}
	return source.Width <= 0 || source.Height <= 0 || source.FPS <= 0
}

// contains checks if a slice contains a specific string (case-insensitive).
func contains(slice []string, item string) bool {
	item = robustNorm(item)
	for _, s := range slice {
		if robustNorm(s) == item {
			return true
		}
	}
	return false
}

func withinMaxVideo(source Source, maxVideo *MaxVideoDimensions) bool {
	if maxVideo == nil {
		return true
	}
	if maxVideo.Width > 0 && source.Width > 0 && source.Width > maxVideo.Width {
		return false
	}
	if maxVideo.Height > 0 && source.Height > 0 && source.Height > maxVideo.Height {
		return false
	}
	if maxVideo.FPS > 0 && source.FPS > 0 && source.FPS > float64(maxVideo.FPS) {
		return false
	}
	return true
}
