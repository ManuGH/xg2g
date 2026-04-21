package profiles

import (
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

// RuntimeModeHintFromProfile maps the coarse resolved profile onto the current
// runtime-mode vocabulary without considering later FFmpeg-side overrides.
func RuntimeModeHintFromProfile(profile model.ProfileSpec) ports.RuntimeMode {
	switch NormalizeRequestedProfileID(profile.Name) {
	case ProfileCopy, ProfileAndroid:
		return ports.RuntimeModeCopy
	case ProfileSafariDirty, ProfileRepair:
		return ports.RuntimeModeSafe
	case ProfileSafariRuntimeHQ:
		return ports.RuntimeModeHQ25
	}

	if !profile.TranscodeVideo {
		return ports.RuntimeModeCopy
	}
	return ports.RuntimeModeHQ25
}
