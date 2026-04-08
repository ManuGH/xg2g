package hardware

import "github.com/ManuGH/xg2g/internal/domain/playbackprofile"

// SnapshotTranscodeCapabilities returns a read-only host capability snapshot for playback targeting.
func SnapshotTranscodeCapabilities(ffmpegAvailable, hlsAvailable bool) playbackprofile.ServerTranscodeCapabilities {
	return playbackprofile.CanonicalizeServerCapabilities(playbackprofile.ServerTranscodeCapabilities{
		FFmpegAvailable:    ffmpegAvailable,
		HLSAvailable:       hlsAvailable,
		HasVAAPI:           HasVAAPI(),
		VAAPIReady:         IsVAAPIReady(),
		HasNVENC:           HasNVENC(),
		NVENCReady:         IsNVENCReady(),
		HardwareVideoCodec: SupportedHardwareCodecs(),
	})
}
