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
		// DeliveryEngine is already set to "direct"

	case "remux":
		plan.Video = TrackPlan{Mode: "copy", Codec: ev.SourceTruth.VideoCodec}
		plan.Audio = TrackPlan{Mode: "copy", Codec: ev.SourceTruth.AudioCodec}
		plan.Packaging = Packaging{Container: "mpegts"}
		// Safari usually wants fmp4 or TS for HLS. The core decision historically used "mpegts" for remux HLS.
		// If ClientEvidence.Family specifically implies fmp4 preference, we could set it here.
		// For now, keep it "mpegts" for strict parity with legacy 'hlsSegmentContainerTS'.

	case "transcode":
		plan.Video = TrackPlan{Mode: "transcode", Codec: "h264"} // Default
		plan.Audio = TrackPlan{Mode: "transcode", Codec: "aac"}
		plan.Packaging = Packaging{Container: "mpegts"}

		// If video doesn't need repair and the client supports it, we can copy it even in transcode mode.
		if isVideoCodecCompatible(ev) && !requiresInterlaceRepair(ev) && ev.SourceTruth.VideoCodec != "" {
			plan.Video = TrackPlan{Mode: "copy", Codec: ev.SourceTruth.VideoCodec}
		} else {
			// Video needs transcode
			if requiresInterlaceRepair(ev) {
				plan.Filters.Deinterlace = true
			}

			// Chromium / Chrome logic: Usually prefers H.264 over HEVC for HLS
			isChromium := strings.Contains(strings.ToLower(ev.ClientEvidence.Family), "chromium") ||
				strings.Contains(strings.ToLower(ev.ClientEvidence.Family), "chrome")

			// Safari / iOS HEVC 4K logic
			isSafari := strings.Contains(strings.ToLower(ev.ClientEvidence.Family), "safari") ||
				strings.Contains(strings.ToLower(ev.ClientEvidence.Family), "ios") ||
				ev.ClientEvidence.Family == "safari_hevc" ||
				ev.ClientEvidence.Family == "safari_hevc_hw"

			if isSafari && !isChromium && contains(ev.ClientEvidence.SupportedVideoCodecs, "hevc") {
				plan.Video.Codec = "hevc"
			}
		}

		// If audio doesn't need repair and the client supports it, we can copy it.
		if isAudioCodecCompatible(ev) && ev.SourceTruth.AudioCodec != "" {
			plan.Audio = TrackPlan{Mode: "copy", Codec: ev.SourceTruth.AudioCodec}
		}

		// DeliveryEngine is already "hls"
	}
}
