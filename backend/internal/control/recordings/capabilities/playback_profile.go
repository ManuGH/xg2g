package capabilities

import "github.com/ManuGH/xg2g/internal/domain/playbackprofile"

// ToClientPlaybackProfile adapts legacy playback capabilities to the shared playback-profile model.
func ToClientPlaybackProfile(c PlaybackCapabilities) playbackprofile.ClientPlaybackProfile {
	c = CanonicalizeCapabilities(c)

	packaging := make([]string, 0, len(c.Containers))
	containers := make([]string, 0, len(c.Containers))
	for _, container := range c.Containers {
		switch container {
		case "ts", "mpegts":
			containers = append(containers, "mpegts")
			packaging = append(packaging, "ts")
		case "fmp4":
			packaging = append(packaging, "fmp4")
		default:
			containers = append(containers, container)
		}
	}

	playbackEngine := ""
	switch c.PreferredHLSEngine {
	case "native":
		playbackEngine = "native_hls"
	case "hlsjs":
		playbackEngine = "hls_js"
	}

	out := playbackprofile.ClientPlaybackProfile{
		DeviceType:     c.DeviceType,
		PlaybackEngine: playbackEngine,
		Containers:     containers,
		VideoCodecs:    c.VideoCodecs,
		AudioCodecs:    c.AudioCodecs,
		HLSPackaging:   packaging,
		SupportsHLS:    c.SupportsHLS,
		SupportsRange:  c.SupportsRange != nil && *c.SupportsRange,
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
