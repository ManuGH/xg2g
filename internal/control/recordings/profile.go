package recordings

import (
	"context"

	"github.com/ManuGH/xg2g/internal/control/playback"
)

// ProfileResolver maps request context (headers) to client capabilities.
// It implements playback.ClientProfileResolver.
type ProfileResolver struct{}

// NewProfileResolver creates a new ProfileResolver.
func NewProfileResolver() *ProfileResolver {
	return &ProfileResolver{}
}

func (h *ProfileResolver) Resolve(ctx context.Context, headers map[string]string) (playback.PlaybackCapabilities, error) {
	// ADR-P7: Delegate to the Single Source of Truth
	requestedProfile := headers["X-Playback-Profile"]
	caps := ResolveCapabilities(ctx, "", "v3.1", requestedProfile, headers, nil)

	// Map domain capabilities to playback capabilities
	return playback.PlaybackCapabilities{
		CapabilitiesVersion: caps.CapabilitiesVersion,
		Containers:          caps.Containers,
		VideoCodecs:         caps.VideoCodecs,
		AudioCodecs:         caps.AudioCodecs,
		SupportsHLS:         caps.SupportsHLS,
		DeviceType:          caps.DeviceType,
	}, nil
}
