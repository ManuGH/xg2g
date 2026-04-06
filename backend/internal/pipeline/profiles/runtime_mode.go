package profiles

import (
	"strings"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

// RuntimeModeHintFromProfile maps the coarse resolved profile onto the current
// runtime-mode vocabulary without considering later FFmpeg-side overrides.
func RuntimeModeHintFromProfile(profile model.ProfileSpec) ports.RuntimeMode {
	switch strings.ToLower(strings.TrimSpace(profile.Name)) {
	case strings.ToLower(ProfileCopy), strings.ToLower(ProfileAndroid):
		return ports.RuntimeModeCopy
	case strings.ToLower(ProfileSafariDirty), strings.ToLower(ProfileRepair):
		return ports.RuntimeModeSafe
	case "safari_hq":
		return ports.RuntimeModeHQ25
	}

	if !profile.TranscodeVideo {
		return ports.RuntimeModeCopy
	}
	return ports.RuntimeModeHQ25
}
