// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package sessions

import (
	"strings"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

const SafariFallbackBrowserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.4 Safari/605.1.15"

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

// NextPlaybackFeedbackPlanWithResolver computes the next fallback profile plan based on the current profile.
func NextPlaybackFeedbackPlanWithResolver(current model.ProfileSpec, serviceRef string, profileResolver profiles.Resolver) playbackFeedbackFallbackPlan {
	switch current.Name {
	case profiles.ProfileSafari:
		return nextSafariFeedbackPlanWithResolver(current, serviceRef, profileResolver)
	default:
		return buildPlaybackFeedbackRepairPlan(current)
	}
}

func nextSafariFeedbackPlanWithResolver(current model.ProfileSpec, serviceRef string, profileResolver profiles.Resolver) playbackFeedbackFallbackPlan {
	preferTS := ShouldPreferSafariTSFallbackForServiceRefWithSnapshot(serviceRef, profileResolver.ConfigSnapshot().SafariForceCopyServiceRefs)
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
	next := resolvePlaybackFeedbackProfile(profiles.ProfileSafari, SafariFallbackBrowserUA, current.DVRWindowSec, profileResolver)
	next.DisableSafariForceCopy = true
	return playbackFeedbackFallbackPlan{
		id:      playbackFeedbackFallbackPlanSafariBrowserTS,
		reason:  playbackFeedbackFallbackReasonSafariForceCopyFirstFailure,
		profile: next,
	}
}

func buildSafariFeedbackRepairTSPlan(current model.ProfileSpec, profileResolver profiles.Resolver) playbackFeedbackFallbackPlan {
	next := resolvePlaybackFeedbackProfile(profiles.ProfileRepair, SafariFallbackBrowserUA, current.DVRWindowSec, profileResolver)
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

// ShouldPreferSafariTSFallbackForServiceRefWithSnapshot checks if a service reference is in the configured allowlist.
func ShouldPreferSafariTSFallbackForServiceRefWithSnapshot(serviceRef string, configuredRefs []string) bool {
	targetRef := NormalizeServiceRefForEnv(serviceRef)
	if targetRef == "" {
		return false
	}
	for _, candidate := range configuredRefs {
		if NormalizeServiceRefForEnv(candidate) == targetRef {
			return true
		}
	}
	return false
}

// NormalizeServiceRefForEnv normalizes a service reference string.
func NormalizeServiceRefForEnv(raw string) string {
	ref := strings.TrimSpace(raw)
	if ref == "" {
		return ""
	}
	ref = strings.TrimRight(ref, ":")
	if isHexColonServiceRefForEnv(ref) {
		return strings.ToUpper(ref)
	}
	return ref
}

func isHexColonServiceRefForEnv(ref string) bool {
	if ref == "" || !strings.Contains(ref, ":") {
		return false
	}
	for _, ch := range ref {
		switch {
		case ch == ':':
		case ch >= '0' && ch <= '9':
		case ch >= 'a' && ch <= 'f':
		case ch >= 'A' && ch <= 'F':
		default:
			return false
		}
	}
	return true
}

// SessionFallbackPlanID retrieves the last fallback plan ID from the session trace.
func SessionFallbackPlanID(sess *model.SessionRecord) string {
	if sess == nil || sess.PlaybackTrace == nil || len(sess.PlaybackTrace.Fallbacks) == 0 {
		return ""
	}
	return strings.TrimSpace(sess.PlaybackTrace.Fallbacks[len(sess.PlaybackTrace.Fallbacks)-1].PlanID)
}

// SessionFallbackPlanReason retrieves the last fallback plan reason from the session trace.
func SessionFallbackPlanReason(sess *model.SessionRecord) string {
	if sess == nil || sess.PlaybackTrace == nil || len(sess.PlaybackTrace.Fallbacks) == 0 {
		return ""
	}
	return strings.TrimSpace(sess.PlaybackTrace.Fallbacks[len(sess.PlaybackTrace.Fallbacks)-1].PlanReason)
}
