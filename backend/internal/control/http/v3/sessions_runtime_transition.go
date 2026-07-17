package v3

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

type sessionRuntimeTransitionResult struct {
	Executed bool
	Restart  bool
	Blockers []string
}

func applySessionRuntimePolicyTransition(rec *model.SessionRecord, transition runtimepolicy.SessionTransition, now time.Time, profileResolver profiles.Resolver) (sessionRuntimeTransitionResult, error) {
	if rec == nil || transition.IsZero() {
		return sessionRuntimeTransitionResult{}, nil
	}
	if rec.State != model.SessionReady && rec.State != model.SessionDraining {
		return sessionRuntimeTransitionResult{Blockers: []string{runtimepolicy.BlockerSessionNotRestartable}}, nil
	}

	switch transition.Kind {
	case runtimepolicy.SessionTransitionScheduleStepDown:
		nextProfile, ok := sessionRuntimeProfileForStepWithResolver(rec.Profile, transition.ToStep, profileResolver)
		if !ok {
			return sessionRuntimeTransitionResult{Blockers: []string{runtimepolicy.BlockerProfileUnmapped}}, nil
		}
		currentTarget := model.TraceTargetProfileFromProfile(rec.Profile)
		nextTarget := model.TraceTargetProfileFromProfile(nextProfile)
		if currentTarget != nil && nextTarget != nil && currentTarget.Hash() == nextTarget.Hash() {
			return sessionRuntimeTransitionResult{Blockers: []string{runtimepolicy.BlockerAlreadyAtProfile}}, nil
		}
		applyRuntimeTransitionProfile(rec, nextProfile, transition, now)
		return sessionRuntimeTransitionResult{Executed: true, Restart: true}, nil
	case runtimepolicy.SessionTransitionScheduleProbeUp, runtimepolicy.SessionTransitionRevertProbe:
		nextProfile, ok := sessionRuntimeProfileForStepWithResolver(rec.Profile, transition.ToStep, profileResolver)
		if !ok {
			return sessionRuntimeTransitionResult{Blockers: []string{runtimepolicy.BlockerProfileUnmapped}}, nil
		}
		currentTarget := model.TraceTargetProfileFromProfile(rec.Profile)
		nextTarget := model.TraceTargetProfileFromProfile(nextProfile)
		if currentTarget != nil && nextTarget != nil && currentTarget.Hash() == nextTarget.Hash() {
			return sessionRuntimeTransitionResult{Blockers: []string{runtimepolicy.BlockerAlreadyAtProfile}}, nil
		}
		applyRuntimeTransitionProfile(rec, nextProfile, transition, now)
		return sessionRuntimeTransitionResult{Executed: true, Restart: true}, nil
	case runtimepolicy.SessionTransitionCommitProbe:
		return sessionRuntimeTransitionResult{Executed: true}, nil
	default:
		return sessionRuntimeTransitionResult{}, nil
	}
}

func applyRuntimeTransitionProfile(rec *model.SessionRecord, nextProfile model.ProfileSpec, transition runtimepolicy.SessionTransition, now time.Time) {
	fromTarget := model.TraceTargetProfileFromProfile(rec.Profile)
	fromHash := ""
	if fromTarget != nil {
		fromHash = fromTarget.Hash()
	}

	rec.Profile = nextProfile
	rec.FallbackReason = runtimeTransitionReason(transition)
	rec.FallbackAtUnix = now.Unix()
	rec.UpdatedAtUnix = now.Unix()
	rec.State = model.SessionStarting
	rec.PipelineState = model.PipeStopRequested
	rec.StopReason = ""

	trace := ensureSessionPlaybackTrace(rec)
	if trace.ClientPath == "" && rec.ContextData != nil {
		trace.ClientPath = strings.TrimSpace(rec.ContextData[model.CtxKeyClientPath])
	}
	if trace.InputKind == "" {
		trace.InputKind = sessionRuntimeInputKindFromRecord(rec)
	}

	toTarget := model.TraceTargetProfileFromProfile(nextProfile)
	toHash := ""
	if toTarget != nil {
		toHash = toTarget.Hash()
	}

	trace.PolicyModeHint = nextProfile.PolicyModeHint
	trace.EffectiveRuntimeMode = nextProfile.EffectiveRuntimeMode
	if trace.EffectiveRuntimeMode == "" || trace.EffectiveRuntimeMode == ports.RuntimeModeUnknown {
		trace.EffectiveRuntimeMode = nextProfile.PolicyModeHint
	}
	trace.EffectiveModeSource = nextProfile.EffectiveModeSource
	trace.TargetProfile = toTarget
	trace.TargetProfileHash = toHash
	trace.FFmpegPlan = model.TraceFFmpegPlanFromProfile(nextProfile, trace.InputKind, 0)
	trace.VideoQualityRung = model.TraceVideoQualityRungFromProfile(nextProfile)
	trace.QualityRung = trace.VideoQualityRung
	trace.StopReason = ""
	trace.StopClass = model.PlaybackStopClassNone
	trace.Fallbacks = append(trace.Fallbacks, model.PlaybackFallbackTrace{
		AtUnix:          now.Unix(),
		Trigger:         "runtime_policy",
		Reason:          rec.FallbackReason,
		FromProfileHash: fromHash,
		ToProfileHash:   toHash,
	})
}

func sessionRuntimeProfileForStepWithResolver(current model.ProfileSpec, step runtimepolicy.PlaybackLadderStep, profileResolver profiles.Resolver) (model.ProfileSpec, bool) {
	build := func(profileID string) model.ProfileSpec {
		next := profileResolver.Resolve(profileID, "", current.DVRWindowSec, nil, profiles.GPUBackendNone, profiles.HWAccelOff)
		next.DVRWindowSec = current.DVRWindowSec
		next.VOD = current.VOD
		next.DisableSafariForceCopy = current.DisableSafariForceCopy
		next.ForceSafariHQ25 = current.ForceSafariHQ25
		next.LLHLS = current.LLHLS
		if strings.TrimSpace(current.Container) != "" {
			next.Container = current.Container
		}
		if current.Deinterlace {
			next.Deinterlace = true
		}
		next.EffectiveModeSource = ports.RuntimeModeSourceRuntimeHardening
		if next.EffectiveRuntimeMode == "" || next.EffectiveRuntimeMode == ports.RuntimeModeUnknown {
			next.EffectiveRuntimeMode = next.PolicyModeHint
		}
		return next
	}

	switch step {
	case runtimepolicy.PlaybackStepDirectCopy:
		return build(profiles.ProfileCopy), true
	case runtimepolicy.PlaybackStepVideoCopyAudioAAC:
		return build(profiles.ProfileHigh), true
	case runtimepolicy.PlaybackStepAV11080p:
		next := profileResolver.Resolve(profiles.ProfileAV1HW, "", current.DVRWindowSec, nil, profiles.GPUBackendVAAPI, profiles.HWAccelForce)
		next.DVRWindowSec = current.DVRWindowSec
		next.VOD = current.VOD
		next.DisableSafariForceCopy = current.DisableSafariForceCopy
		next.ForceSafariHQ25 = current.ForceSafariHQ25
		next.LLHLS = current.LLHLS
		if current.Deinterlace {
			next.Deinterlace = true
		}
		next.EffectiveModeSource = ports.RuntimeModeSourceRuntimeHardening
		if next.EffectiveRuntimeMode == "" || next.EffectiveRuntimeMode == ports.RuntimeModeUnknown {
			next.EffectiveRuntimeMode = next.PolicyModeHint
		}
		return next, true
	case runtimepolicy.PlaybackStepH2641080p:
		next := build(profiles.ProfileHigh)
		next.TranscodeVideo = true
		next.VideoCodec = "libx264"
		next.HWAccel = ""
		next.VideoCRF = 20
		next.VideoMaxWidth = 1920
		next.Preset = "slow"
		if next.AudioBitrateK <= 0 {
			next.AudioBitrateK = 192
		}
		return next, true
	case runtimepolicy.PlaybackStepH264720p:
		return build(profiles.ProfileLow), true
	case runtimepolicy.PlaybackStepRepairLow:
		return build(profiles.ProfileRepair), true
	default:
		return model.ProfileSpec{}, false
	}
}

func runtimeTransitionReason(transition runtimepolicy.SessionTransition) string {
	return fmt.Sprintf("runtime_policy:%s:%s->%s", transition.Kind, transition.FromStep, transition.ToStep)
}

func sessionRuntimeInputKindFromRecord(rec *model.SessionRecord) string {
	if rec == nil || rec.ContextData == nil {
		return ""
	}
	return strings.TrimSpace(rec.ContextData[model.CtxKeySourceType])
}

func (s *Server) publishSessionRuntimeTransition(ctx context.Context, store SessionStateStore, sess *model.SessionRecord, transition runtimepolicy.SessionTransition) {
	if s == nil || store == nil || sess == nil || transition.IsZero() {
		return
	}
	eventBus := s.sessionsModuleDeps().bus
	if eventBus == nil {
		return
	}

	stopEvt := model.StopSessionEvent{
		Type:          model.EventStopSession,
		SessionID:     sess.SessionID,
		Reason:        model.RClientStop,
		CorrelationID: sess.CorrelationID,
		RequestedAtUN: time.Now().Unix(),
	}
	if err := eventBus.Publish(ctx, string(model.EventStopSession), stopEvt); err != nil {
		log.L().Error().Err(err).Str("sessionId", sess.SessionID).Str("transition", string(transition.Kind)).Msg("failed to publish runtime policy stop event")
		return
	}

	log.L().
		Warn().
		Str("sessionId", sess.SessionID).
		Str("transition", string(transition.Kind)).
		Str("from_step", string(transition.FromStep)).
		Str("to_step", string(transition.ToStep)).
		Str("profile", sess.Profile.Name).
		Msg("runtime policy transition scheduled session restart")

	s.scheduleSessionRestart(eventBus, store, sess)
}
