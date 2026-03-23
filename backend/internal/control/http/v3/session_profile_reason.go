package v3

import (
	"strings"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

const (
	sessionProfileReasonSafariCompatTranscode = "safari_compat_transcode"
	sessionProfileReasonRepairTranscode       = "repair_transcode"
	sessionProfileReasonTranscodeStartup      = "transcode_startup"
)

func sessionProfileReason(session *model.SessionRecord) *string {
	if session == nil || !sessionRequiresVideoTranscode(session) {
		return nil
	}

	switch strings.TrimSpace(session.Profile.Name) {
	case profiles.ProfileSafari:
		return toPtr(sessionProfileReasonSafariCompatTranscode)
	case profiles.ProfileSafariDirty:
		return toPtr(sessionProfileReasonRepairTranscode)
	default:
		return toPtr(sessionProfileReasonTranscodeStartup)
	}
}

func sessionRequiresVideoTranscode(session *model.SessionRecord) bool {
	if session == nil {
		return false
	}
	if session.Profile.TranscodeVideo {
		return true
	}
	if trace := session.PlaybackTrace; trace != nil {
		if trace.TargetProfile != nil && trace.TargetProfile.Video.Mode == playbackprofile.MediaModeTranscode {
			return true
		}
		if trace.FFmpegPlan != nil && strings.EqualFold(strings.TrimSpace(trace.FFmpegPlan.VideoMode), string(playbackprofile.MediaModeTranscode)) {
			return true
		}
	}
	return false
}
