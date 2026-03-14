package capabilities

import "github.com/ManuGH/xg2g/internal/domain/playbackprofile"

// ToClientPlaybackProfile adapts legacy playback capabilities to the shared playback-profile model.
func ToClientPlaybackProfile(c PlaybackCapabilities) playbackprofile.ClientPlaybackProfile {
	c = CanonicalizeCapabilities(c)

	out := playbackprofile.ClientPlaybackProfile{
		DeviceType:    c.DeviceType,
		Containers:    c.Containers,
		VideoCodecs:   c.VideoCodecs,
		AudioCodecs:   c.AudioCodecs,
		SupportsHLS:   c.SupportsHLS,
		SupportsRange: c.SupportsRange != nil && *c.SupportsRange,
	}
	if c.AllowTranscode != nil {
		v := *c.AllowTranscode
		out.AllowTranscode = &v
	}
	if c.MaxVideo != nil {
		out.MaxVideo = &playbackprofile.VideoConstraints{
			Width:  c.MaxVideo.Width,
			Height: c.MaxVideo.Height,
			FPS:    float64(c.MaxVideo.Fps),
		}
	}

	return playbackprofile.CanonicalizeClient(out)
}
