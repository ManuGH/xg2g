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

func (h *ProfileResolver) Resolve(ctx context.Context, headers map[string]string) (playback.ClientProfile, error) {
	// Extract profile alias from synthetic header
	profileName := headers["X-Playback-Profile"]

	var p playback.ClientProfile
	p.Name = profileName
	// p.UserAgent = headers["User-Agent"] // Explicitly ignored to avoid contract drift per strict review

	// Map Profile Name to Capabilities
	switch PlaybackProfile(profileName) {
	case ProfileSafari:
		p.Name = "safari_native"
		p.IsSafari = true
		p.SupportsNativeHLS = true
		p.SupportsH264 = true
		p.SupportsAAC = true
		p.SupportsAC3 = true
	case ProfileTVOS:
		p.Name = "tvos"
		p.IsSafari = true
		p.SupportsNativeHLS = true
		p.SupportsH264 = true
		p.SupportsAAC = true
		p.SupportsAC3 = true
		p.CanPlayTS = true
	case ProfileGeneric:
		p.Name = "mse_hlsjs"
		p.SupportsMSE = true
		p.SupportsH264 = true
		p.SupportsAAC = true
		p.IsChrome = true
	default:
		p.Name = "unknown"
		p.SupportsMSE = true
		p.SupportsH264 = true
	}
	return p, nil
}
