package hardware

import "github.com/ManuGH/xg2g/internal/domain/playbackprofile"

// SnapshotTranscodeCapabilities returns a read-only host capability snapshot for playback targeting.
func SnapshotTranscodeCapabilities(ffmpegAvailable, hlsAvailable bool) playbackprofile.ServerTranscodeCapabilities {
	hwCodecs := make([]string, 0, 3)
	if IsVAAPIEncoderReady("h264_vaapi") {
		hwCodecs = append(hwCodecs, "h264")
	}
	if IsVAAPIEncoderReady("hevc_vaapi") {
		hwCodecs = append(hwCodecs, "hevc")
	}
	if IsVAAPIEncoderReady("av1_vaapi") {
		hwCodecs = append(hwCodecs, "av1")
	}

	return playbackprofile.CanonicalizeServerCapabilities(playbackprofile.ServerTranscodeCapabilities{
		FFmpegAvailable:    ffmpegAvailable,
		HLSAvailable:       hlsAvailable,
		HasVAAPI:           HasVAAPI(),
		VAAPIReady:         IsVAAPIReady(),
		HardwareVideoCodec: hwCodecs,
	})
}
