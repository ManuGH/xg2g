package artifacts

import (
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

func recordingTargetProfile(profile string) *playbackprofile.TargetPlaybackProfile {
	return recservice.RecordingTargetProfile(profile)
}
