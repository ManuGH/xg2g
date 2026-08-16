package playbackplanner

import (
	"strings"
)

// resolveMediaTargets populates Video, Audio, Packaging, Filters, and RateControl based on the selected Mode.
func resolveMediaTargets(plan *PlaybackPlan, ev PlaybackEvidence) {
	switch plan.Mode {
	case "copy":
		plan.Video = TrackPlan{Mode: "copy", Codec: ev.SourceTruth.VideoCodec}
		plan.Audio = TrackPlan{Mode: "copy", Codec: ev.SourceTruth.AudioCodec}
		plan.Packaging = Packaging{Container: ev.SourceTruth.Container}

	case "remux":
		plan.Video = TrackPlan{Mode: "copy", Codec: ev.SourceTruth.VideoCodec}
		plan.Audio = TrackPlan{Mode: "copy", Codec: ev.SourceTruth.AudioCodec}
		plan.Packaging = Packaging{Container: "mpegts"}
		if ev.ClientEvidence.PrefersFMP4 || strings.EqualFold(strings.TrimSpace(ev.ClientEvidence.Family), "ios_safari_native") {
			plan.Packaging.Container = "fmp4"
		}

	case "transcode":
		plan.Video = TrackPlan{Mode: "transcode", Codec: "h264"} // Default
		plan.Audio = TrackPlan{Mode: "transcode", Codec: "aac", BitrateKbps: 192, Channels: 2, SampleRate: 48000}
		autoTranscodeProfile := false
		plan.Packaging = Packaging{Container: "mpegts"}
		if ev.ClientEvidence.PrefersFMP4 || strings.EqualFold(strings.TrimSpace(ev.ClientEvidence.Family), "ios_safari_native") {
			plan.Packaging.Container = "fmp4"
		}

		if requiresInterlaceRepair(ev) {
			plan.Filters.Deinterlace = true
		}
		if ev.ClientEvidence.MaxVideoWidth > 0 && ev.SourceTruth.Width > ev.ClientEvidence.MaxVideoWidth {
			plan.Filters.ScaleWidth = ev.ClientEvidence.MaxVideoWidth
		}

		isABRIntent := isABRRequested(ev)
		if isABRIntent {
			plan.Video = TrackPlan{Mode: "transcode", Codec: "h264", EnableABR: true}
		} else if codec, ok := selectAutoTranscodeVideoCodec(ev); ok {
			plan.Video.Codec = codec
			autoTranscodeProfile = true
			if codec == "av1" && ev.OperatorPolicy.ExperimentalAV1MPEGTS && !nativeWebKitClient(ev.ClientEvidence.Family) {
				plan.Packaging.Container = "mpegts"
			} else {
				plan.Packaging.Container = "fmp4"
			}
		} else if isVideoCodecCompatible(ev) && !requiresInterlaceRepair(ev) && !exceedsMaxVideoLimits(ev) && ev.SourceTruth.VideoCodec != "" && !requiresVideoNormalization(ev) {
			plan.Video = TrackPlan{Mode: "copy", Codec: ev.SourceTruth.VideoCodec}
		} else {
			isChromium := strings.Contains(strings.ToLower(ev.ClientEvidence.Family), "chromium") ||
				strings.Contains(strings.ToLower(ev.ClientEvidence.Family), "chrome")

			isSafari := strings.Contains(strings.ToLower(ev.ClientEvidence.Family), "safari") ||
				strings.Contains(strings.ToLower(ev.ClientEvidence.Family), "ios") ||
				ev.ClientEvidence.Family == "safari_hevc" ||
				ev.ClientEvidence.Family == "safari_hevc_hw"

			isHLSJS := strings.EqualFold(ev.ClientEvidence.PreferredEngine, "hlsjs")

			if isSafari && !isChromium && !isHLSJS &&
				explicitlyRequestsHEVCProfile(ev.RequestedIntent) &&
				contains(ev.ClientEvidence.SupportedVideoCodecs, "hevc") {
				plan.Video.Codec = "hevc"
			}
		}

		// Legacy auto-codec profiles are complete execution profiles: when video
		// is re-encoded they also normalize audio to AAC, even if source audio is
		// independently copy-compatible. Explicit repair/copy paths retain the
		// track-by-track behavior above.
		if !autoTranscodeProfile && isAudioCodecCompatible(ev) && ev.SourceTruth.AudioCodec != "" {
			plan.Audio = TrackPlan{Mode: "copy", Codec: ev.SourceTruth.AudioCodec}
		}
		if requiresPlannedTranscode(ev) && plan.Video.Mode == "copy" && plan.Audio.Mode == "copy" {
			// Legacy repair/forced-transcode mode is track-aware. Preserve a
			// compatible video bitstream, but ensure the mode is not a no-op by
			// normalizing audio to AAC when both tracks were otherwise copyable.
			plan.Audio = TrackPlan{Mode: "transcode", Codec: "aac", BitrateKbps: 192, Channels: 2, SampleRate: 48000}
		}
		if plan.Video.Mode == "transcode" {
			plan.RateControl.MaxVideoBitrateKbps = transcodeMaxVideoBitrateKbps(plan.Video.Codec, ev)
		}
	}

	// fMP4 guard for copied broadcast video: DVB H.264 uses open GOPs, and
	// ffmpeg's HLS fMP4 muxer clamps the leading B-frame's negative CTS offset
	// at every segment boundary — producing a duplicate PTS plus a 40ms hole
	// per segment, which players render as a periodic visible judder (verified
	// against a raw capture: source PTS were perfect, segments were not).
	// MPEG-TS segments carry the source timestamps faithfully for most clients.
	// Android TV native is the narrow exception: the MediaTek TS parser on Fire
	// TV corrupts NAL alignment at segment boundaries, while the same decoder is
	// stable through ExoPlayer's FragmentedMp4Extractor. Keep the client's fMP4
	// preference for that family only; video still remains bit-for-bit copy.
	if plan.Video.Mode == "copy" && plan.Packaging.Container == "fmp4" && !copyCodecRequiresFMP4(plan.Video.Codec) && !allowsCopiedH264FMP4(ev.ClientEvidence) {
		plan.Packaging.Container = "mpegts"
	}
}

func allowsCopiedH264FMP4(client ClientEvidence) bool {
	fam := strings.ToLower(strings.TrimSpace(client.Family))
	if fam == "ios_safari_native" {
		return true
	}
	return fam == "android_tv_native" && client.PrefersFMP4
}

func copyCodecRequiresFMP4(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "hevc", "h265", "av1":
		return true
	default:
		return false
	}
}

func explicitlyRequestsHEVCProfile(requestedIntent string) bool {
	switch strings.ToLower(strings.TrimSpace(requestedIntent)) {
	case "safari_hevc", "safari_hevc_hw", "safari_hevc_hw_ll", "hevc":
		return true
	default:
		return false
	}
}

func transcodeMaxVideoBitrateKbps(codec string, ev PlaybackEvidence) int {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	// Ceilings sized for 1080p50 broadcast, not for 24-30fps film: 50 fields per
	// second carry roughly twice the temporal information, and the old 6000/5000
	// were the visible quality limit once the pipeline stopped collapsing 50i to
	// 25p. These are ceilings, not targets - the encoder spends what the picture
	// needs, and applyPolicyModifiers still clamps them on a constrained link.
	case "av1":
		return 32000
	case "hevc", "h265":
		return 10000
	case "h264", "avc", "libx264":
		for _, encoder := range ev.HostSnapshot.EncoderCapabilities {
			if strings.EqualFold(strings.TrimSpace(encoder.Codec), "h264") && encoder.Verified && encoder.AutoEligible {
				return 20000
			}
		}
		return 8000
	default:
		return 8000
	}
}

// applyPolicyModifiers applies host pressure downgrades and operator limits to the plan.
func applyPolicyModifiers(plan *PlaybackPlan, ev PlaybackEvidence) {
	// If the host is under pressure, we might downgrade quality
	// In legacy, critical host pressure clamped intent to compatible or lower.
	// We can reflect this by enforcing max quality rungs.
	maxRung := ev.OperatorPolicy.MaxQualityRung
	if ev.HostSnapshot.PressureBand == "critical" || ev.HostSnapshot.PressureBand == "constrained" {
		if maxRung == "" || maxRung == "quality" {
			maxRung = "compatible"
		}
	}

	plan.Guardrails.MaxQualityRung = maxRung

	// Network caps: if network bandwidth is limited, apply it
	switch plan.Video.Mode {
	case "transcode":
		if plan.RateControl.MaxVideoBitrateKbps <= 0 {
			plan.RateControl.MaxVideoBitrateKbps = transcodeMaxVideoBitrateKbps(plan.Video.Codec, ev)
		}

		// If we know network is constrained
		if ev.NetworkEvidence.DownlinkKbps > 0 && ev.NetworkEvidence.DownlinkKbps < 5000 {
			plan.RateControl.TargetVideoBitrateKbps = 3000
			if plan.RateControl.MaxVideoBitrateKbps == 0 || plan.RateControl.MaxVideoBitrateKbps > 6000 {
				plan.RateControl.MaxVideoBitrateKbps = 6000
			}
		}
	case "copy":
		// If we are copying, target bitrate matches source
		if ev.SourceTruth.BitrateKbps > 0 {
			plan.RateControl.TargetVideoBitrateKbps = ev.SourceTruth.BitrateKbps
			plan.RateControl.MaxVideoBitrateKbps = ev.SourceTruth.BitrateKbps
		}
	}

	// Operator overrides
	if ev.OperatorPolicy.MaxGlobalBitrate > 0 {
		if plan.RateControl.MaxVideoBitrateKbps == 0 || plan.RateControl.MaxVideoBitrateKbps > ev.OperatorPolicy.MaxGlobalBitrate {
			plan.RateControl.MaxVideoBitrateKbps = ev.OperatorPolicy.MaxGlobalBitrate
			if plan.RateControl.TargetVideoBitrateKbps > ev.OperatorPolicy.MaxGlobalBitrate {
				plan.RateControl.TargetVideoBitrateKbps = ev.OperatorPolicy.MaxGlobalBitrate
			}
		}
	}
}

func isABRRequested(ev PlaybackEvidence) bool {
	requested := strings.ToLower(strings.TrimSpace(ev.OperatorPolicy.ForceIntent))
	if requested == "" {
		requested = strings.ToLower(strings.TrimSpace(ev.RequestedIntent))
	}
	switch requested {
	case "abr", "mobile_abr", "3tier", "2tier":
		return true
	default:
		return false
	}
}
