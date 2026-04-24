package intents

import (
	"context"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/control/recordings/capreg"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/normalize"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

const startPlaybackPolicyStateFreshness = 15 * time.Minute

func (s *Service) clampRequestedStartProfileFromPlaybackPolicy(ctx context.Context, intent Intent, requestedPlaybackMode, profileID string) string {
	state, observation, ok := s.lookupStartPlaybackPolicyState(ctx, intent)
	if !ok {
		return profileID
	}

	targetIntent := playbackprofile.ClampIntentToMaxQualityRung(playbackprofile.IntentQuality, state.MaxQualityRung)
	if targetIntent == playbackprofile.IntentUnknown || targetIntent == playbackprofile.IntentQuality {
		return profileID
	}

	clampedProfileID := clampAggressiveStartProfile(profileID, requestedPlaybackMode, clientFamilyForIntent(intent), targetIntent)
	if clampedProfileID == profileID {
		return profileID
	}

	intent.Logger.Info().
		Str("decisionTrace", strings.TrimSpace(intent.DecisionTrace)).
		Str("decisionRequestId", strings.TrimSpace(observation.RequestID)).
		Str("serviceRef", strings.TrimSpace(intent.ServiceRef)).
		Str("feedbackClamp", string(state.MaxQualityRung)).
		Str("confidenceState", string(state.Confidence.State)).
		Int("confidenceScore", state.Confidence.Score).
		Str("requestedPlaybackMode", strings.TrimSpace(requestedPlaybackMode)).
		Str("profileFrom", profileID).
		Str("profileTo", clampedProfileID).
		Msg("start profile clamped from playback feedback")
	return clampedProfileID
}

func (s *Service) lookupStartPlaybackPolicyState(ctx context.Context, intent Intent) (capreg.PlaybackPolicyState, capreg.PlaybackObservation, bool) {
	registry := s.deps.CapabilityRegistry()
	if registry == nil {
		return capreg.PlaybackPolicyState{}, capreg.PlaybackObservation{}, false
	}

	decisionRequestID := strings.TrimSpace(intent.DecisionTrace)
	if decisionRequestID == "" {
		decisionRequestID = strings.TrimSpace(intent.Params[model.CtxKeyDecisionRequest])
	}
	if decisionRequestID == "" {
		return capreg.PlaybackPolicyState{}, capreg.PlaybackObservation{}, false
	}

	observation, found, err := registry.LookupDecisionObservation(ctx, decisionRequestID)
	if err != nil {
		intent.Logger.Warn().
			Err(err).
			Str("decisionTrace", decisionRequestID).
			Str("serviceRef", strings.TrimSpace(intent.ServiceRef)).
			Msg("startup playback decision lookup failed")
		return capreg.PlaybackPolicyState{}, capreg.PlaybackObservation{}, false
	}
	if !found {
		return capreg.PlaybackPolicyState{}, capreg.PlaybackObservation{}, false
	}
	if normalize.Token(observation.ObservationKind) != "decision" || normalize.Token(observation.SubjectKind) != "live" {
		return capreg.PlaybackPolicyState{}, capreg.PlaybackObservation{}, false
	}

	serviceRef := strings.TrimSpace(intent.ServiceRef)
	if serviceRef != "" && strings.TrimSpace(observation.SourceRef) != "" && strings.TrimSpace(observation.SourceRef) != serviceRef {
		return capreg.PlaybackPolicyState{}, capreg.PlaybackObservation{}, false
	}
	if strings.TrimSpace(observation.SourceFingerprint) == "" || strings.TrimSpace(observation.DeviceFingerprint) == "" || strings.TrimSpace(observation.HostFingerprint) == "" {
		return capreg.PlaybackPolicyState{}, capreg.PlaybackObservation{}, false
	}

	stateLookup, ok := registry.(capreg.PlaybackPolicyStateLookup)
	if !ok {
		return capreg.PlaybackPolicyState{}, capreg.PlaybackObservation{}, false
	}

	state, found, err := stateLookup.LookupPlaybackPolicyState(ctx, capreg.PlaybackPolicyStateQuery{
		SubjectKind:       observation.SubjectKind,
		SourceFingerprint: observation.SourceFingerprint,
		DeviceFingerprint: observation.DeviceFingerprint,
		HostFingerprint:   observation.HostFingerprint,
	})
	if err != nil {
		intent.Logger.Warn().
			Err(err).
			Str("decisionTrace", decisionRequestID).
			Str("serviceRef", strings.TrimSpace(intent.ServiceRef)).
			Msg("startup playback policy state lookup failed")
		return capreg.PlaybackPolicyState{}, capreg.PlaybackObservation{}, false
	}
	if !found {
		return capreg.PlaybackPolicyState{}, capreg.PlaybackObservation{}, false
	}

	state.MaxQualityRung = playbackprofile.NormalizeQualityRung(string(state.MaxQualityRung))
	if state.MaxQualityRung == playbackprofile.RungUnknown {
		return capreg.PlaybackPolicyState{}, capreg.PlaybackObservation{}, false
	}

	if state.UpdatedAt.IsZero() || !state.UpdatedAt.After(time.Now().UTC().Add(-startPlaybackPolicyStateFreshness)) {
		return capreg.PlaybackPolicyState{}, capreg.PlaybackObservation{}, false
	}

	return state, observation, true
}

func clampAggressiveStartProfile(profileID, requestedPlaybackMode, clientFamily string, targetIntent playbackprofile.PlaybackIntent) string {
	switch profiles.NormalizeRequestedProfileID(profileID) {
	case profiles.ProfileAV1HW, profiles.ProfileSafariHEVC, profiles.ProfileSafariHEVCHW, profiles.ProfileSafariHEVCHWLL:
		switch targetIntent {
		case playbackprofile.IntentCompatible:
			return feedbackCompatibleStartProfile(requestedPlaybackMode, clientFamily)
		case playbackprofile.IntentRepair:
			return feedbackRepairStartProfile(requestedPlaybackMode, clientFamily)
		}
	}
	return profiles.NormalizeRequestedProfileID(profileID)
}

func feedbackCompatibleStartProfile(requestedPlaybackMode, clientFamily string) string {
	switch normalize.Token(requestedPlaybackMode) {
	case "native_hls":
		return profiles.ProfileSafari
	case "hlsjs":
		return profiles.ProfileHigh
	case "transcode":
		return profiles.ProfileH264FMP4
	}

	switch normalize.Token(clientFamily) {
	case playbackprofile.ClientSafariNative, playbackprofile.ClientIOSSafariNative:
		return profiles.ProfileSafari
	default:
		return profiles.ProfileHigh
	}
}

func feedbackRepairStartProfile(requestedPlaybackMode, clientFamily string) string {
	switch normalize.Token(requestedPlaybackMode) {
	case "native_hls":
		return profiles.ProfileSafariDirty
	case "hlsjs", "transcode":
		return profiles.ProfileH264FMP4
	}

	switch normalize.Token(clientFamily) {
	case playbackprofile.ClientSafariNative, playbackprofile.ClientIOSSafariNative:
		return profiles.ProfileSafariDirty
	default:
		return profiles.ProfileH264FMP4
	}
}
