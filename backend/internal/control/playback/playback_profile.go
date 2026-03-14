package playback

import "github.com/ManuGH/xg2g/internal/domain/playbackprofile"

// MediaTruthToPlaybackProfile adapts media truth to the shared playback-profile model.
func MediaTruthToPlaybackProfile(truth MediaTruth) playbackprofile.SourceProfile {
	return playbackprofile.CanonicalizeSource(playbackprofile.SourceProfile{
		Container:  truth.Container,
		VideoCodec: truth.VideoCodec,
		AudioCodec: truth.AudioCodec,
		Width:      truth.Width,
		Height:     truth.Height,
		FPS:        truth.FPS,
		Interlaced: truth.Interlaced,
	})
}

// CapabilitiesToPlaybackProfile adapts legacy playback capabilities to the shared playback-profile model.
func CapabilitiesToPlaybackProfile(c PlaybackCapabilities) playbackprofile.ClientPlaybackProfile {
	out := playbackprofile.ClientPlaybackProfile{
		DeviceType:    c.DeviceType,
		Containers:    c.Containers,
		VideoCodecs:   c.VideoCodecs,
		AudioCodecs:   c.AudioCodecs,
		SupportsHLS:   c.SupportsHLS,
		SupportsRange: false,
	}
	if c.AllowTranscode != nil {
		v := *c.AllowTranscode
		out.AllowTranscode = &v
	}
	if c.MaxVideo != nil {
		out.MaxVideo = &playbackprofile.VideoConstraints{
			Width:  c.MaxVideo.Width,
			Height: c.MaxVideo.Height,
			FPS:    c.MaxVideo.FPS,
		}
	}

	return playbackprofile.CanonicalizeClient(out)
}
