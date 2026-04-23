package decision

import (
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/normalize"
)

const (
	defaultWebAACBitrateKbps = 256
	defaultWebAACChannels    = 2
	defaultWebAACSampleRate  = 48000
	qualityWebAACBitrateKbps = 320
	repairWebAACBitrateKbps  = 192
	hlsSegmentContainerTS    = "mpegts"
)

type targetProfileResolution struct {
	profile              *playbackprofile.TargetPlaybackProfile
	requestedIntent      playbackprofile.PlaybackIntent
	resolvedIntent       playbackprofile.PlaybackIntent
	qualityRung          playbackprofile.QualityRung
	audioQualityRung     playbackprofile.QualityRung
	videoQualityRung     playbackprofile.QualityRung
	degradedFrom         playbackprofile.PlaybackIntent
	forcedIntent         playbackprofile.PlaybackIntent
	maxQualityRung       playbackprofile.QualityRung
	hostPressureBand     playbackprofile.HostPressureBand
	operatorOverrideUsed bool
	hostOverrideApplied  bool
}

func buildTargetProfile(mode Mode, pred Predicates, input DecisionInput) targetProfileResolution {
	requestedIntent, effectiveIntent, forcedIntent, maxQualityRung, hostPressureBand, operatorOverrideUsed, hostOverrideApplied := resolvePlaybackIntent(mode, pred, input)

	switch mode {
	case ModeDirectPlay:
		profile := playbackprofile.CanonicalizeTarget(playbackprofile.TargetPlaybackProfile{
			Container: normalizedContainer(input.Source.Container),
			Packaging: packagingFromContainer(input.Source.Container),
			Video: playbackprofile.VideoTarget{
				Mode:  playbackprofile.MediaModeCopy,
				Codec: input.Source.VideoCodec,
			},
			Audio: playbackprofile.AudioTarget{
				Mode:  playbackprofile.MediaModeCopy,
				Codec: input.Source.AudioCodec,
			},
			HWAccel: playbackprofile.HWAccelNone,
		})
		return targetProfileResolution{
			profile:              &profile,
			requestedIntent:      requestedIntent,
			resolvedIntent:       playbackprofile.IntentDirect,
			qualityRung:          playbackprofile.RungDirectCopy,
			forcedIntent:         forcedIntent,
			maxQualityRung:       maxQualityRung,
			hostPressureBand:     hostPressureBand,
			operatorOverrideUsed: operatorOverrideUsed,
			hostOverrideApplied:  hostOverrideApplied,
		}
	case ModeDirectStream:
		profile := playbackprofile.CanonicalizeTarget(playbackprofile.TargetPlaybackProfile{
			Container: hlsSegmentContainerTS,
			Packaging: playbackprofile.PackagingTS,
			Video: playbackprofile.VideoTarget{
				Mode:  playbackprofile.MediaModeCopy,
				Codec: input.Source.VideoCodec,
			},
			Audio: playbackprofile.AudioTarget{
				Mode:  playbackprofile.MediaModeCopy,
				Codec: input.Source.AudioCodec,
			},
			HLS: playbackprofile.HLSTarget{
				Enabled:          true,
				SegmentContainer: hlsSegmentContainerTS,
			},
			HWAccel: playbackprofile.HWAccelNone,
		})
		resolution := targetProfileResolution{
			profile:              &profile,
			requestedIntent:      requestedIntent,
			resolvedIntent:       playbackprofile.IntentCompatible,
			qualityRung:          playbackprofile.RungCompatibleHLSTS,
			forcedIntent:         forcedIntent,
			maxQualityRung:       maxQualityRung,
			hostPressureBand:     hostPressureBand,
			operatorOverrideUsed: operatorOverrideUsed,
			hostOverrideApplied:  hostOverrideApplied,
		}
		if requestedIntent == playbackprofile.IntentDirect || (hostOverrideApplied && requestedIntent != playbackprofile.IntentUnknown && requestedIntent != resolution.resolvedIntent) {
			resolution.degradedFrom = requestedIntent
		}
		return resolution
	case ModeTranscode:
		resolvedIntent, degradedFrom := resolveTranscodeIntent(effectiveIntent)
		video := playbackprofile.VideoTarget{Mode: playbackprofile.MediaModeCopy}
		videoQualityRung := playbackprofile.RungUnknown
		if pred.CanVideo && !pred.VideoRepairRequired && normalize.Token(input.Source.VideoCodec) != "" {
			video.Codec = input.Source.VideoCodec
		} else {
			video = transcodeVideoTarget(resolvedIntent, input.Source)
			videoQualityRung = videoRungForTranscodeIntent(resolvedIntent)
		}

		audio := playbackprofile.AudioTarget{
			Mode: playbackprofile.MediaModeCopy,
		}
		audioQualityRung := playbackprofile.RungUnknown
		if pred.CanAudio && normalize.Token(input.Source.AudioCodec) != "" {
			audio.Codec = input.Source.AudioCodec
		} else {
			audio = transcodeAudioTarget(resolvedIntent)
			audioQualityRung = audioRungForTranscodeIntent(resolvedIntent)
		}

		if video.Mode != playbackprofile.MediaModeTranscode && audio.Mode != playbackprofile.MediaModeTranscode {
			audio = transcodeAudioTarget(resolvedIntent)
			audioQualityRung = audioRungForTranscodeIntent(resolvedIntent)
		}
		qualityRung := legacyQualityRung(videoQualityRung, audioQualityRung)

		profile := playbackprofile.CanonicalizeTarget(playbackprofile.TargetPlaybackProfile{
			Container: hlsSegmentContainerTS,
			Packaging: playbackprofile.PackagingTS,
			Video:     video,
			Audio:     audio,
			HLS: playbackprofile.HLSTarget{
				Enabled:          true,
				SegmentContainer: hlsSegmentContainerTS,
			},
			HWAccel: playbackprofile.HWAccelNone,
		})
		if degradedFrom == playbackprofile.IntentUnknown && hostOverrideApplied && requestedIntent != playbackprofile.IntentUnknown && requestedIntent != resolvedIntent {
			degradedFrom = requestedIntent
		}
		return targetProfileResolution{
			profile:              &profile,
			requestedIntent:      requestedIntent,
			resolvedIntent:       resolvedIntent,
			qualityRung:          qualityRung,
			audioQualityRung:     audioQualityRung,
			videoQualityRung:     videoQualityRung,
			degradedFrom:         degradedFrom,
			forcedIntent:         forcedIntent,
			maxQualityRung:       maxQualityRung,
			hostPressureBand:     hostPressureBand,
			operatorOverrideUsed: operatorOverrideUsed,
			hostOverrideApplied:  hostOverrideApplied,
		}
	default:
		return targetProfileResolution{
			requestedIntent:      requestedIntent,
			forcedIntent:         forcedIntent,
			maxQualityRung:       maxQualityRung,
			hostPressureBand:     hostPressureBand,
			operatorOverrideUsed: operatorOverrideUsed,
			hostOverrideApplied:  hostOverrideApplied,
		}
	}
}

func resolvePlaybackIntent(mode Mode, pred Predicates, input DecisionInput) (playbackprofile.PlaybackIntent, playbackprofile.PlaybackIntent, playbackprofile.PlaybackIntent, playbackprofile.QualityRung, playbackprofile.HostPressureBand, bool, bool) {
	requestedIntent := playbackprofile.NormalizeRequestedIntent(string(input.RequestedIntent))
	effectiveIntent := requestedIntent
	forcedIntent := playbackprofile.NormalizeRequestedIntent(string(input.Policy.Operator.ForceIntent))
	maxQualityRung := playbackprofile.NormalizeQualityRung(string(input.Policy.Operator.MaxQualityRung))
	hostPressureBand := playbackprofile.NormalizeHostPressureBand(string(input.Policy.Host.PressureBand))
	operatorOverrideApplied := false
	operatorActive := forcedIntent != playbackprofile.IntentUnknown || maxQualityRung != playbackprofile.RungUnknown

	if forcedIntent != playbackprofile.IntentUnknown {
		operatorOverrideApplied = operatorOverrideApplied || forcedIntent != effectiveIntent
		effectiveIntent = forcedIntent
	}

	clampedIntent := playbackprofile.ClampIntentToMaxQualityRung(effectiveIntent, maxQualityRung)
	operatorOverrideApplied = operatorOverrideApplied || clampedIntent != effectiveIntent
	effectiveIntent = clampedIntent

	hostOverrideApplied := false
	if !operatorActive {
		if hostIntent := applyHostPressureIntent(effectiveIntent, hostPressureBand); hostIntent != effectiveIntent {
			effectiveIntent = hostIntent
			hostOverrideApplied = true
		}
		if hostIntent := applyHostPerformanceIntent(effectiveIntent, input.Policy.Host, input.Source, mode, pred); hostIntent != effectiveIntent {
			effectiveIntent = hostIntent
			hostOverrideApplied = true
		}
	}

	return requestedIntent, effectiveIntent, forcedIntent, maxQualityRung, hostPressureBand, operatorOverrideApplied, hostOverrideApplied
}

func resolveTranscodeIntent(requested playbackprofile.PlaybackIntent) (playbackprofile.PlaybackIntent, playbackprofile.PlaybackIntent) {
	switch requested {
	case playbackprofile.IntentQuality:
		return playbackprofile.IntentQuality, playbackprofile.IntentUnknown
	case playbackprofile.IntentRepair:
		return playbackprofile.IntentRepair, playbackprofile.IntentUnknown
	case playbackprofile.IntentDirect:
		return playbackprofile.IntentCompatible, playbackprofile.IntentDirect
	default:
		return playbackprofile.IntentCompatible, playbackprofile.IntentUnknown
	}
}

func applyHostPressureIntent(intent playbackprofile.PlaybackIntent, band playbackprofile.HostPressureBand) playbackprofile.PlaybackIntent {
	switch playbackprofile.NormalizeHostPressureBand(string(band)) {
	case playbackprofile.HostPressureConstrained, playbackprofile.HostPressureCritical:
		if intent == playbackprofile.IntentQuality {
			return playbackprofile.IntentCompatible
		}
	}
	return intent
}

func applyHostPerformanceIntent(intent playbackprofile.PlaybackIntent, host HostPolicy, source Source, mode Mode, pred Predicates) playbackprofile.PlaybackIntent {
	videoTranscodeRequired := mode == ModeTranscode && (!pred.CanVideo || pred.VideoRepairRequired)
	if !videoTranscodeRequired {
		return intent
	}

	cost := sourceTranscodeCostScore(source)
	performanceClass := normalizeHostPerformanceClass(host.PerformanceClass)
	benchmarkClass := normalizeHostBenchmarkClass(host.BenchmarkClass)
	switch performanceClass {
	case hostPerformanceClassLow:
		if cost >= 8 && intent == playbackprofile.IntentCompatible {
			return playbackprofile.IntentRepair
		}
		if cost >= 5 && intent == playbackprofile.IntentQuality {
			return playbackprofile.IntentCompatible
		}
	case hostPerformanceClassMedium:
		if cost >= 8 && intent == playbackprofile.IntentQuality {
			return playbackprofile.IntentCompatible
		}
	}

	if videoTranscodeRequired && benchmarkClass == hostBenchmarkClassWeak {
		if cost >= 9 && intent == playbackprofile.IntentCompatible {
			return playbackprofile.IntentRepair
		}
		if cost >= 6 && intent == playbackprofile.IntentQuality && performanceClass != hostPerformanceClassLow {
			return playbackprofile.IntentCompatible
		}
	}
	return intent
}

func sourceTranscodeCostScore(source Source) int {
	score := 0
	if source.Interlaced {
		score += 3
	}

	pixels := source.Width * source.Height
	switch {
	case pixels >= 3840*2160:
		score += 4
	case pixels >= 1920*1080:
		score += 2
	case pixels >= 1280*720:
		score++
	}

	switch {
	case source.FPS >= 50:
		score += 2
	case source.FPS >= 30:
		score++
	}

	score += bitrateCostScore(source)

	return score
}

func bitrateCostScore(source Source) int {
	score := 0
	switch {
	case source.BitrateKbps >= 18000:
		score += 3
	case source.BitrateKbps >= 9000:
		score += 2
	case source.BitrateKbps >= 5000:
		score++
	}

	// A single fresh probe is better than guessing, but still too noisy to let
	// bitrate alone push us into a more degraded ladder.
	if score > 0 && normalizeBitrateConfidence(source.BitrateConfidence) == bitrateConfidenceLow {
		score--
	}

	return score
}

func transcodeAudioTarget(intent playbackprofile.PlaybackIntent) playbackprofile.AudioTarget {
	return playbackprofile.AudioTarget{
		Mode:        playbackprofile.MediaModeTranscode,
		Codec:       "aac",
		Channels:    defaultWebAACChannels,
		BitrateKbps: bitrateForIntent(intent),
		SampleRate:  defaultWebAACSampleRate,
	}
}

func bitrateForIntent(intent playbackprofile.PlaybackIntent) int {
	switch intent {
	case playbackprofile.IntentQuality:
		return qualityWebAACBitrateKbps
	case playbackprofile.IntentRepair:
		return repairWebAACBitrateKbps
	default:
		return defaultWebAACBitrateKbps
	}
}

func transcodeVideoTarget(intent playbackprofile.PlaybackIntent, source Source) playbackprofile.VideoTarget {
	rung := playbackprofile.VideoRungForIntent(intent)
	return playbackprofile.VideoTarget{
		Mode:   playbackprofile.MediaModeTranscode,
		Codec:  "h264",
		CRF:    playbackprofile.VideoCRFForRung(rung),
		Preset: playbackprofile.VideoPresetForRung(rung),
		Width:  source.Width,
		Height: source.Height,
		FPS:    source.FPS,
	}
}

func audioRungForTranscodeIntent(intent playbackprofile.PlaybackIntent) playbackprofile.QualityRung {
	switch intent {
	case playbackprofile.IntentQuality:
		return playbackprofile.RungQualityAudioAAC320Stereo
	case playbackprofile.IntentRepair:
		return playbackprofile.RungRepairAudioAAC192Stereo
	default:
		return playbackprofile.RungCompatibleAudioAAC256Stereo
	}
}

func videoRungForTranscodeIntent(intent playbackprofile.PlaybackIntent) playbackprofile.QualityRung {
	return playbackprofile.VideoRungForIntent(intent)
}

func legacyQualityRung(video, audio playbackprofile.QualityRung) playbackprofile.QualityRung {
	if video != playbackprofile.RungUnknown {
		return video
	}
	return audio
}

func packagingFromContainer(container string) playbackprofile.Packaging {
	switch normalize.Token(container) {
	case "mp4", "mov", "m4v":
		return playbackprofile.PackagingMP4
	case "mpegts", "ts":
		return playbackprofile.PackagingTS
	default:
		return playbackprofile.PackagingUnknown
	}
}

func normalizedContainer(container string) string {
	return normalize.Token(container)
}
