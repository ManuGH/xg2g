package v3

import (
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

type playbackFeedbackFallbackPlanID string

const (
	playbackFeedbackFallbackPlanRepairFMP4      playbackFeedbackFallbackPlanID = "repair_fmp4"
	playbackFeedbackFallbackPlanSafariDirty     playbackFeedbackFallbackPlanID = "safari_dirty"
	playbackFeedbackFallbackPlanSafariBrowserTS playbackFeedbackFallbackPlanID = "safari_browser_ts"
	playbackFeedbackFallbackPlanSafariRepairTS  playbackFeedbackFallbackPlanID = "safari_repair_ts"
)

type playbackFeedbackFallbackPlanReason string

const (
	playbackFeedbackFallbackReasonDefaultRepair                playbackFeedbackFallbackPlanReason = "default_repair_escalation"
	playbackFeedbackFallbackReasonSafariGeneralFirstFailure    playbackFeedbackFallbackPlanReason = "safari_general_first_failure"
	playbackFeedbackFallbackReasonSafariForceCopyFirstFailure  playbackFeedbackFallbackPlanReason = "safari_force_copy_allowlist_first_failure"
	playbackFeedbackFallbackReasonSafariForceCopyRepeatFailure playbackFeedbackFallbackPlanReason = "safari_force_copy_allowlist_repeat_failure"
)

type playbackFeedbackFallbackPlan struct {
	id      playbackFeedbackFallbackPlanID
	reason  playbackFeedbackFallbackPlanReason
	profile model.ProfileSpec
}

func nextPlaybackFeedbackPlanWithResolver(current model.ProfileSpec, serviceRef string, profileResolver profiles.Resolver) playbackFeedbackFallbackPlan {
	switch current.Name {
	case profiles.ProfileSafari:
		return nextSafariFeedbackPlanWithResolver(current, serviceRef, profileResolver)
	default:
		return buildPlaybackFeedbackRepairPlan(current)
	}
}

func nextSafariFeedbackPlanWithResolver(current model.ProfileSpec, serviceRef string, profileResolver profiles.Resolver) playbackFeedbackFallbackPlan {
	preferTS := shouldPreferSafariTSFallbackForServiceRefWithSnapshot(serviceRef, profileResolver.ConfigSnapshot().SafariForceCopyServiceRefs)
	if preferTS && !current.DisableSafariForceCopy {
		return buildSafariFeedbackBrowserTSPlan(current, profileResolver)
	}

	if preferTS {
		return buildSafariFeedbackRepairTSPlan(current, profileResolver)
	}

	return buildSafariFeedbackDirtyPlan(current, profileResolver)
}

func buildPlaybackFeedbackRepairPlan(current model.ProfileSpec) playbackFeedbackFallbackPlan {
	next := current
	next.Name = profiles.ProfileRepair
	next.PolicyModeHint = ports.RuntimeModeSafe
	next.EffectiveModeSource = ports.RuntimeModeSourceFeedbackFallback
	next.TranscodeVideo = true
	next.Deinterlace = false
	next.HWAccel = ""
	next.VideoCodec = "libx264"
	next.VideoCRF = 24
	next.VideoMaxWidth = 1280
	next.Preset = "veryfast"
	next.Container = "fmp4"
	return playbackFeedbackFallbackPlan{
		id:      playbackFeedbackFallbackPlanRepairFMP4,
		reason:  playbackFeedbackFallbackReasonDefaultRepair,
		profile: next,
	}
}

func buildSafariFeedbackBrowserTSPlan(current model.ProfileSpec, profileResolver profiles.Resolver) playbackFeedbackFallbackPlan {
	next := resolvePlaybackFeedbackProfile(profiles.ProfileSafari, safariFallbackBrowserUA, current.DVRWindowSec, profileResolver)
	next.DisableSafariForceCopy = true
	return playbackFeedbackFallbackPlan{
		id:      playbackFeedbackFallbackPlanSafariBrowserTS,
		reason:  playbackFeedbackFallbackReasonSafariForceCopyFirstFailure,
		profile: next,
	}
}

func buildSafariFeedbackRepairTSPlan(current model.ProfileSpec, profileResolver profiles.Resolver) playbackFeedbackFallbackPlan {
	next := resolvePlaybackFeedbackProfile(profiles.ProfileRepair, safariFallbackBrowserUA, current.DVRWindowSec, profileResolver)
	next.Container = "mpegts"
	next.Deinterlace = true
	next.HWAccel = ""
	next.VideoCodec = "libx264"
	next.VideoCRF = 24
	next.VideoMaxWidth = 1280
	next.Preset = "veryfast"
	next.AudioBitrateK = 192
	next.PolicyModeHint = ports.RuntimeModeSafe
	return playbackFeedbackFallbackPlan{
		id:      playbackFeedbackFallbackPlanSafariRepairTS,
		reason:  playbackFeedbackFallbackReasonSafariForceCopyRepeatFailure,
		profile: next,
	}
}

func buildSafariFeedbackDirtyPlan(current model.ProfileSpec, profileResolver profiles.Resolver) playbackFeedbackFallbackPlan {
	return playbackFeedbackFallbackPlan{
		id:      playbackFeedbackFallbackPlanSafariDirty,
		reason:  playbackFeedbackFallbackReasonSafariGeneralFirstFailure,
		profile: resolvePlaybackFeedbackProfile(profiles.ProfileSafariDirty, "", current.DVRWindowSec, profileResolver),
	}
}

func resolvePlaybackFeedbackProfile(profileID, userAgent string, dvrWindowSec int, profileResolver profiles.Resolver) model.ProfileSpec {
	next := profileResolver.Resolve(profileID, userAgent, dvrWindowSec, nil, profiles.GPUBackendNone, profiles.HWAccelOff)
	next.EffectiveModeSource = ports.RuntimeModeSourceFeedbackFallback
	return next
}
