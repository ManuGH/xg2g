package playbackprofile

import (
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile/ports"
)

// HashTarget returns the stable SHA-256 hash of the canonical target profile.
func HashTarget(p TargetPlaybackProfile) string {
	return ports.HashTarget(p)
}
