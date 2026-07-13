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

	case "transcode":
		plan.Video = TrackPlan{Mode: "transcode", Codec: "h264"} // Default
		plan.Audio = TrackPlan{Mode: "transcode", Codec: "aac"}
		plan.Packaging = Packaging{Container: "mpegts"}

		if isVideoCodecCompatible(ev) && !requiresInterlaceRepair(ev) && ev.SourceTruth.VideoCodec != "" {
			plan.Video = TrackPlan{Mode: "copy", Codec: ev.SourceTruth.VideoCodec}
		} else {
			if requiresInterlaceRepair(ev) {
				plan.Filters.Deinterlace = true
			}

			isChromium := strings.Contains(strings.ToLower(ev.ClientEvidence.Family), "chromium") ||
				strings.Contains(strings.ToLower(ev.ClientEvidence.Family), "chrome")

			isSafari := strings.Contains(strings.ToLower(ev.ClientEvidence.Family), "safari") ||
				strings.Contains(strings.ToLower(ev.ClientEvidence.Family), "ios") ||
				ev.ClientEvidence.Family == "safari_hevc" ||
				ev.ClientEvidence.Family == "safari_hevc_hw"

			if isSafari && !isChromium && contains(ev.ClientEvidence.SupportedVideoCodecs, "hevc") {
				plan.Video.Codec = "hevc"
			}
		}

		if isAudioCodecCompatible(ev) && ev.SourceTruth.AudioCodec != "" {
			plan.Audio = TrackPlan{Mode: "copy", Codec: ev.SourceTruth.AudioCodec}
		}
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
	targetKbps := 0
	
	if plan.Video.Mode == "transcode" {
		// Default conservative limits for transcode
		targetKbps = 8000
		plan.RateControl.TargetVideoBitrateKbps = targetKbps
		plan.RateControl.MaxVideoBitrateKbps = 16000
		
		// If we know network is constrained
		if ev.NetworkEvidence.DownlinkKbps > 0 && ev.NetworkEvidence.DownlinkKbps < 5000 {
			plan.RateControl.TargetVideoBitrateKbps = 3000
			plan.RateControl.MaxVideoBitrateKbps = 6000
		}
	} else if plan.Video.Mode == "copy" {
		// If we are copying, target bitrate matches source
		if ev.SourceTruth.BitrateKbps > 0 {
			targetKbps = ev.SourceTruth.BitrateKbps
			plan.RateControl.TargetVideoBitrateKbps = targetKbps
			plan.RateControl.MaxVideoBitrateKbps = targetKbps
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
