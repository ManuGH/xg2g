package decision

import "github.com/ManuGH/xg2g/internal/domain/playbackprofile"

// SourceToPlaybackProfile adapts decision source truth to the shared playback-profile model.
func SourceToPlaybackProfile(src Source) playbackprofile.SourceProfile {
	return playbackprofile.CanonicalizeSource(playbackprofile.SourceProfile{
		Container:   src.Container,
		VideoCodec:  src.VideoCodec,
		AudioCodec:  src.AudioCodec,
		BitrateKbps: src.BitrateKbps,
		Width:       src.Width,
		Height:      src.Height,
		FPS:         src.FPS,
	})
}

// CapabilitiesToPlaybackProfile adapts decision capabilities to the shared playback-profile model.
func CapabilitiesToPlaybackProfile(c Capabilities) playbackprofile.ClientPlaybackProfile {
	out := playbackprofile.ClientPlaybackProfile{
		DeviceType:    c.DeviceType,
		Containers:    c.Containers,
		VideoCodecs:   c.VideoCodecs,
		AudioCodecs:   c.AudioCodecs,
		SupportsHLS:   c.SupportsHLS,
		SupportsRange: c.SupportsRange != nil && *c.SupportsRange,
	}
	if c.MaxVideo != nil {
		out.MaxVideo = &playbackprofile.VideoConstraints{
			Width:  c.MaxVideo.Width,
			Height: c.MaxVideo.Height,
		}
	}

	return playbackprofile.CanonicalizeClient(out)
}
