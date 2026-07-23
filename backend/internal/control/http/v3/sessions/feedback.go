// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package sessions

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/control/recordings/capreg"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/rs/zerolog"
)

const (
	playbackErrorCodeDecode              = 3
	playbackErrorCodeHlsStalled          = 4
	playbackInfoCodeProbeWindowConfirmed = 221
	playbackInfoCodeNativeRenderStable   = 231
	playbackInfoCodeNativeRenderRecover  = 232
	playbackInfoCodeHLSJSRenderStable    = 241
	playbackInfoCodeHLSJSRenderBlack     = 242
)

// ReportPlaybackFeedback processes playback feedback reports, records observations,
// and triggers fallback profile restarts if a decode error is reported.
func (s *Service) ReportPlaybackFeedback(ctx context.Context, sessionID string, req PlaybackFeedbackInput) (PlaybackFeedbackResult, error) {
	if s == nil || s.deps == nil {
		return PlaybackFeedbackResult{Unavailable: true, ErrorMessage: "sessions service not available"}, nil
	}
	bus := s.deps.Bus()
	store := s.deps.SessionStore()
	if bus == nil || store == nil {
		return PlaybackFeedbackResult{Unavailable: true, ErrorMessage: "sessions store or bus not available"}, nil
	}

	sess, err := store.GetSession(ctx, sessionID)
	isDecodeError := shouldTriggerPlaybackFallback(req, sess)
	if err == nil && sess != nil {
		s.recordPlaybackFeedbackObservation(ctx, sess, req)
	}
	s.logPlaybackFeedback(sessionID, sess, req, isDecodeError)

	if !isDecodeError {
		return PlaybackFeedbackResult{Accepted: true}, nil
	}

	if err != nil {
		return PlaybackFeedbackResult{Unavailable: true, ErrorMessage: "session lookup failed"}, err
	}
	if sess == nil {
		return PlaybackFeedbackResult{NotFound: true, ErrorMessage: "session not found"}, nil
	}
	if sess.State != model.SessionReady && sess.State != model.SessionDraining {
		log.L().Warn().
			Str("sessionId", sessionID).
			Str("state", string(sess.State)).
			Msg("ignoring playback feedback before session reached serving state")
		return PlaybackFeedbackResult{Accepted: true}, nil
	}
	if !SessionHasFirstFrameArtifact(s.deps.Config().HLS.Root, sess.SessionID) {
		log.L().Warn().
			Str("sessionId", sessionID).
			Str("state", string(sess.State)).
			Msg("ignoring playback fallback request before first video frame was observed")
		return PlaybackFeedbackResult{Accepted: true}, nil
	}
	if sess.PlaybackTrace != nil && sess.PlaybackTrace.Operator != nil && sess.PlaybackTrace.Operator.ClientFallbackDisabled {
		log.L().Warn().
			Str("sessionId", sessionID).
			Msg("ignoring playback feedback because client fallback is disabled by persisted operator override")
		return PlaybackFeedbackResult{Accepted: true}, nil
	}

	profileResolver := s.deps.ProfileResolver()
	var changed bool
	updatedSess, err := store.UpdateSession(ctx, sessionID, func(rec *model.SessionRecord) error {
		if rec.Profile.Name == profiles.ProfileRepair {
			return nil
		}
		now := time.Now()
		trace := EnsureSessionPlaybackTrace(rec)
		if trace.RequestProfile == "" {
			trace.RequestProfile = profiles.PublicProfileName(rec.Profile.Name)
		}
		if trace.ClientPath == "" && rec.ContextData != nil {
			trace.ClientPath = strings.TrimSpace(rec.ContextData[model.CtxKeyClientPath])
		}
		if trace.InputKind == "" && rec.ContextData != nil {
			trace.InputKind = strings.TrimSpace(rec.ContextData[model.CtxKeySourceType])
		}
		if trace.FirstFrameAtUnix == 0 {
			trace.FirstFrameAtUnix = SessionFirstFrameUnix(s.deps.Config().HLS.Root, rec.SessionID)
		}
		fromTarget := model.TraceTargetProfileFromProfile(rec.Profile)
		fromHash := ""
		if fromTarget != nil {
			fromHash = fromTarget.Hash()
		}

		fallbackPlan := NextPlaybackFeedbackPlanWithResolver(rec.Profile, rec.ServiceRef, profileResolver)
		rec.Profile = fallbackPlan.profile

		codeVal := 0
		if req.Code != nil {
			codeVal = *req.Code
		}
		rec.FallbackReason = fmt.Sprintf("client_report:code=%d", codeVal)
		rec.FallbackAtUnix = now.Unix()
		rec.State = model.SessionStarting
		rec.PipelineState = model.PipeStopRequested
		rec.StopReason = ""
		toTarget := model.TraceTargetProfileFromProfile(rec.Profile)
		toHash := ""
		if toTarget != nil {
			toHash = toTarget.Hash()
		}
		trace.PolicyModeHint = rec.Profile.PolicyModeHint
		trace.EffectiveModeSource = rec.Profile.EffectiveModeSource
		trace.TargetProfile = toTarget
		trace.TargetProfileHash = toHash
		trace.StopReason = ""
		trace.StopClass = model.PlaybackStopClassNone
		trace.Fallbacks = append(trace.Fallbacks, model.PlaybackFallbackTrace{
			AtUnix:          now.Unix(),
			Trigger:         "client_feedback",
			Reason:          rec.FallbackReason,
			PlanID:          string(fallbackPlan.id),
			PlanReason:      string(fallbackPlan.reason),
			FromProfileHash: fromHash,
			ToProfileHash:   toHash,
		})
		changed = true
		return nil
	})

	if err != nil {
		log.L().Error().Err(err).Msg("failed to update session for fallback")
		return PlaybackFeedbackResult{InternalError: true, ErrorMessage: "store update failed"}, err
	}

	if !changed {
		log.L().Info().Str("sessionId", sessionID).Msg("fallback already active, ignoring request")
		return PlaybackFeedbackResult{Accepted: true}, nil
	}

	sess = updatedSess
	log.L().
		Warn().
		Str("sessionId", sess.SessionID).
		Str("fallback_plan", SessionFallbackPlanID(sess)).
		Str("fallback_plan_reason", SessionFallbackPlanReason(sess)).
		Str("fallback_profile", sess.Profile.Name).
		Str("fallback_container", sess.Profile.Container).
		Msg("activating safari fallback due to client error")

	stopEvt := model.StopSessionEvent{
		Type:          model.EventStopSession,
		SessionID:     sess.SessionID,
		Reason:        model.RClientStop,
		CorrelationID: sess.CorrelationID,
		RequestedAtUN: time.Now().Unix(),
	}
	if err := bus.Publish(ctx, string(model.EventStopSession), stopEvt); err != nil {
		log.L().Error().Err(err).Msg("failed to publish stop event during fallback")
	}

	s.ScheduleFallbackRestart(sess)
	return PlaybackFeedbackResult{Accepted: true}, nil
}

func (s *Service) recordPlaybackFeedbackObservation(ctx context.Context, sess *model.SessionRecord, req PlaybackFeedbackInput) {
	if s == nil || sess == nil {
		return
	}
	observedAt := time.Now().UTC()
	codeVal := 0
	if req.Code != nil {
		codeVal = *req.Code
	}
	msgVal := ""
	if req.Message != nil {
		msgVal = *req.Message
	}

	if isSoftStartupPlaybackWarning(req) && SessionInPlaybackStartupWarmup(sess, s.deps.Config().HLS.Root, PlaybackStartupSoftWarningWarmup, observedAt) {
		log.L().Info().
			Str("sessionId", sess.SessionID).
			Str("event", string(req.Event)).
			Int("code", codeVal).
			Str("msg", msgVal).
			Time("startupWarmupUntil", SessionPlaybackStartupWarmupUntil(sess, s.deps.Config().HLS.Root, PlaybackStartupSoftWarningWarmup)).
			Msg("ignoring soft startup playback warning during warmup")
		return
	}

	registry := s.deps.CapabilityRegistry()
	if registry == nil {
		return
	}

	decisionRequestID := ""
	if sess.ContextData != nil {
		decisionRequestID = strings.TrimSpace(sess.ContextData[model.CtxKeyDecisionRequest])
	}

	linked := capreg.PlaybackObservation{}
	if decisionRequestID != "" {
		var (
			ok  bool
			err error
		)
		linked, ok, err = registry.LookupDecisionObservation(ctx, decisionRequestID)
		if err != nil {
			log.L().Warn().Err(err).Str("sessionId", sess.SessionID).Str("decisionRequestId", decisionRequestID).Msg("capability registry decision lookup failed")
		}
		if !ok {
			linked = capreg.PlaybackObservation{}
		}
	}

	trace := sess.PlaybackTrace
	target := traceTargetProfileForFeedback(sess)
	observation := capreg.PlaybackObservation{
		ObservedAt:         observedAt,
		RequestID:          decisionRequestID,
		ObservationKind:    "feedback",
		Outcome:            playbackFeedbackOutcome(req.Event, codeVal),
		SessionID:          strings.TrimSpace(sess.SessionID),
		SourceRef:          firstNonEmptyFeedback(strings.TrimSpace(sess.ServiceRef), linked.SourceRef),
		SourceFingerprint:  linked.SourceFingerprint,
		SubjectKind:        firstNonEmptyFeedback(feedbackSubjectKind(sess), linked.SubjectKind),
		RequestedIntent:    firstNonEmptyFeedback(feedbackTraceIntent(trace, true), linked.RequestedIntent),
		ResolvedIntent:     firstNonEmptyFeedback(feedbackTraceIntent(trace, false), linked.ResolvedIntent),
		Mode:               feedbackMode(linked.Mode, target),
		SelectedContainer:  feedbackContainer(linked.SelectedContainer, target),
		SelectedVideoCodec: feedbackVideoCodec(linked.SelectedVideoCodec, target),
		SelectedAudioCodec: feedbackAudioCodec(linked.SelectedAudioCodec, target),
		SourceWidth:        linked.SourceWidth,
		SourceHeight:       linked.SourceHeight,
		SourceFPS:          linked.SourceFPS,
		HostFingerprint:    linked.HostFingerprint,
		DeviceFingerprint:  linked.DeviceFingerprint,
		ClientCapsHash:     linked.ClientCapsHash,
		Network:            linked.Network,
		FeedbackEvent:      string(req.Event),
		FeedbackCode:       codeVal,
		FeedbackMessage:    msgVal,
	}

	if err := registry.RecordObservation(ctx, observation); err != nil {
		log.L().Warn().Err(err).Str("sessionId", sess.SessionID).Str("decisionRequestId", decisionRequestID).Msg("capability registry feedback observation failed")
	}
}

func (s *Service) logPlaybackFeedback(sessionID string, sess *model.SessionRecord, req PlaybackFeedbackInput, decodeError bool) {
	var event *zerolog.Event
	switch req.Event {
	case PlaybackFeedbackEventError:
		event = log.L().Warn()
	default:
		event = log.L().Info()
	}

	codeVal := 0
	if req.Code != nil {
		codeVal = *req.Code
	}
	msgVal := ""
	if req.Message != nil {
		msgVal = *req.Message
	}

	event.
		Str("sessionId", strings.TrimSpace(sessionID)).
		Str("event", string(req.Event)).
		Int("code", codeVal).
		Str("msg", msgVal).
		Bool("decodeError", decodeError)

	appendPlaybackContextFields(event, req.Context)

	if sess == nil {
		event.Msg("playback feedback received")
		return
	}

	event.
		Str("state", string(sess.State)).
		Str("serviceRef", strings.TrimSpace(sess.ServiceRef)).
		Str("profile", strings.TrimSpace(sess.Profile.Name)).
		Str("profileContainer", strings.TrimSpace(sess.Profile.Container)).
		Str("profileVideoCodec", strings.TrimSpace(sess.Profile.VideoCodec)).
		Bool("firstFrameSeen", SessionHasFirstFrameArtifact(s.deps.Config().HLS.Root, sess.SessionID))

	if sess.ContextData != nil {
		if requestID := strings.TrimSpace(sess.ContextData[model.CtxKeyDecisionRequest]); requestID != "" {
			event.Str("decisionRequestId", requestID)
		}
		if requestedPath := strings.TrimSpace(sess.ContextData[model.CtxKeyClientPath]); requestedPath != "" {
			event.Str("contextClientPath", requestedPath)
		}
	}

	trace := sess.PlaybackTrace
	if trace == nil {
		event.Msg("playback feedback received")
		return
	}

	event.
		Str("requestProfile", strings.TrimSpace(trace.RequestProfile)).
		Str("requestedIntent", strings.TrimSpace(trace.RequestedIntent)).
		Str("resolvedIntent", strings.TrimSpace(trace.ResolvedIntent)).
		Str("clientPath", strings.TrimSpace(trace.ClientPath)).
		Str("inputKind", strings.TrimSpace(trace.InputKind)).
		Str("autoCodecPolicy", strings.TrimSpace(trace.AutoCodecPolicy)).
		Str("autoCodecRequested", strings.TrimSpace(trace.AutoCodecRequested)).
		Str("autoCodecSelected", strings.TrimSpace(trace.AutoCodecSelected)).
		Str("autoCodecPerformanceClass", strings.TrimSpace(trace.AutoCodecHostClass)).
		Str("autoCodecBenchmarkClass", strings.TrimSpace(trace.AutoCodecBenchClass)).
		Str("ffmpegPackaging", traceFFmpegPlanValue(trace, func(plan *model.FFmpegPlanTrace) string { return strings.TrimSpace(plan.Packaging) })).
		Str("ffmpegContainer", traceFFmpegPlanValue(trace, func(plan *model.FFmpegPlanTrace) string { return strings.TrimSpace(plan.Container) })).
		Str("ffmpegVideoMode", traceFFmpegPlanValue(trace, func(plan *model.FFmpegPlanTrace) string { return strings.TrimSpace(plan.VideoMode) })).
		Str("ffmpegVideoCodec", traceFFmpegPlanValue(trace, func(plan *model.FFmpegPlanTrace) string { return strings.TrimSpace(plan.VideoCodec) })).
		Str("ffmpegAudioMode", traceFFmpegPlanValue(trace, func(plan *model.FFmpegPlanTrace) string { return strings.TrimSpace(plan.AudioMode) })).
		Str("ffmpegAudioCodec", traceFFmpegPlanValue(trace, func(plan *model.FFmpegPlanTrace) string { return strings.TrimSpace(plan.AudioCodec) })).
		Str("clientFamily", traceClientValue(trace, func(client *model.PlaybackClientSnapshot) string { return strings.TrimSpace(client.ClientFamily) })).
		Str("preferredHlsEngine", traceClientValue(trace, func(client *model.PlaybackClientSnapshot) string { return strings.TrimSpace(client.PreferredHLSEngine) }))

	if trace.TargetProfile != nil {
		event.
			Str("targetContainer", strings.TrimSpace(trace.TargetProfile.Container)).
			Str("targetPackaging", strings.TrimSpace(string(trace.TargetProfile.Packaging))).
			Str("targetVideoCodec", strings.TrimSpace(trace.TargetProfile.Video.Codec)).
			Str("targetAudioCodec", strings.TrimSpace(trace.TargetProfile.Audio.Codec))
	}

	event.Msg("playback feedback received")
}

func appendPlaybackContextFields(event *zerolog.Event, ctx *PlaybackEngineErrorContextInput) {
	if ctx == nil {
		return
	}
	if ctx.Engine != nil {
		event.Str("playbackEngine", string(*ctx.Engine))
	}
	if ctx.Phase != nil {
		event.Str("playbackPhase", string(*ctx.Phase))
	}
	if ctx.AttemptId != nil {
		event.Int("playbackAttemptId", *ctx.AttemptId)
	}
	if ctx.PlaybackEpoch != nil {
		event.Int("playbackEpoch", *ctx.PlaybackEpoch)
	}
	if ctx.RecoveryAttempt != nil {
		event.Int("playbackRecoveryAttempt", *ctx.RecoveryAttempt)
	}
}

func shouldTriggerPlaybackFallback(req PlaybackFeedbackInput, sess *model.SessionRecord) bool {
	if req.Event != PlaybackFeedbackEventError || req.Code == nil {
		return false
	}

	switch *req.Code {
	case playbackErrorCodeDecode:
		return true
	case playbackErrorCodeHlsStalled:
		return shouldEscalateIOSAV1HlsStall(sess)
	default:
		return false
	}
}

func shouldEscalateIOSAV1HlsStall(sess *model.SessionRecord) bool {
	if sess == nil {
		return false
	}

	if strings.TrimSpace(sessionClientFamilyForFeedback(sess)) != playbackprofile.ClientIOSSafariNative {
		return false
	}
	if strings.TrimSpace(sessionClientPathForFeedback(sess)) != "hlsjs" {
		return false
	}

	return strings.EqualFold(strings.TrimSpace(sessionTargetVideoCodecForFeedback(sess)), "av1")
}

func isSoftStartupPlaybackWarning(req PlaybackFeedbackInput) bool {
	if req.Event != PlaybackFeedbackEventWarning || req.Code == nil {
		return false
	}
	switch *req.Code {
	case 101, 102, 104:
		return true
	default:
		return false
	}
}

func sessionClientFamilyForFeedback(sess *model.SessionRecord) string {
	if sess == nil {
		return ""
	}
	if sess.ContextData != nil {
		if family := strings.TrimSpace(sess.ContextData[model.CtxKeyClientFamily]); family != "" {
			return family
		}
	}
	if sess.PlaybackTrace != nil && sess.PlaybackTrace.Client != nil {
		return strings.TrimSpace(sess.PlaybackTrace.Client.ClientFamily)
	}
	return ""
}

func sessionClientPathForFeedback(sess *model.SessionRecord) string {
	if sess == nil {
		return ""
	}
	if sess.ContextData != nil {
		if clientPath := strings.TrimSpace(sess.ContextData[model.CtxKeyClientPath]); clientPath != "" {
			return clientPath
		}
	}
	if sess.PlaybackTrace != nil {
		return strings.TrimSpace(sess.PlaybackTrace.ClientPath)
	}
	return ""
}

func sessionTargetVideoCodecForFeedback(sess *model.SessionRecord) string {
	if sess == nil {
		return ""
	}
	if sess.PlaybackTrace != nil {
		if sess.PlaybackTrace.TargetProfile != nil {
			if codec := strings.TrimSpace(sess.PlaybackTrace.TargetProfile.Video.Codec); codec != "" {
				return codec
			}
		}
		if sess.PlaybackTrace.FFmpegPlan != nil {
			if codec := strings.TrimSpace(sess.PlaybackTrace.FFmpegPlan.VideoCodec); codec != "" {
				return codec
			}
		}
		if codec := strings.TrimSpace(sess.PlaybackTrace.AutoCodecSelected); codec != "" {
			return codec
		}
	}
	return strings.TrimSpace(sess.Profile.VideoCodec)
}

func traceFFmpegPlanValue(trace *model.PlaybackTrace, fn func(plan *model.FFmpegPlanTrace) string) string {
	if trace == nil || trace.FFmpegPlan == nil {
		return ""
	}
	return fn(trace.FFmpegPlan)
}

func traceClientValue(trace *model.PlaybackTrace, fn func(client *model.PlaybackClientSnapshot) string) string {
	if trace == nil || trace.Client == nil {
		return ""
	}
	return fn(trace.Client)
}

func playbackFeedbackOutcome(event PlaybackFeedbackEvent, code int) string {
	if code == playbackInfoCodeHLSJSRenderBlack {
		return "warning"
	}
	switch event {
	case PlaybackFeedbackEventError:
		return "failed"
	case PlaybackFeedbackEventWarning:
		return "warning"
	default:
		return "started"
	}
}

func feedbackSubjectKind(sess *model.SessionRecord) string {
	if sess == nil || sess.ContextData == nil {
		return "live"
	}
	switch strings.ToLower(strings.TrimSpace(sess.ContextData[model.CtxKeyMode])) {
	case "recording":
		return "recording"
	default:
		return "live"
	}
}

func feedbackTraceIntent(trace *model.PlaybackTrace, requested bool) string {
	if trace == nil {
		return ""
	}
	if requested {
		return strings.TrimSpace(trace.RequestedIntent)
	}
	return strings.TrimSpace(trace.ResolvedIntent)
}

func traceTargetProfileForFeedback(sess *model.SessionRecord) *playbackprofile.TargetPlaybackProfile {
	if sess == nil {
		return nil
	}
	if sess.PlaybackTrace != nil && sess.PlaybackTrace.TargetProfile != nil {
		return sess.PlaybackTrace.TargetProfile
	}
	return model.TraceTargetProfileFromProfile(sess.Profile)
}

func feedbackMode(existing string, target *playbackprofile.TargetPlaybackProfile) string {
	if existing = strings.TrimSpace(existing); existing != "" {
		return existing
	}
	if target == nil {
		return ""
	}
	if target.Video.Mode == playbackprofile.MediaModeCopy && target.Audio.Mode == playbackprofile.MediaModeCopy {
		if target.HLS.Enabled {
			return "direct_stream"
		}
		return "direct_play"
	}
	return "transcode"
}

func feedbackContainer(existing string, target *playbackprofile.TargetPlaybackProfile) string {
	if existing = strings.TrimSpace(existing); existing != "" {
		return existing
	}
	if target == nil {
		return ""
	}
	return strings.TrimSpace(target.Container)
}

func feedbackVideoCodec(existing string, target *playbackprofile.TargetPlaybackProfile) string {
	if existing = strings.TrimSpace(existing); existing != "" {
		return existing
	}
	if target == nil {
		return ""
	}
	return strings.TrimSpace(target.Video.Codec)
}

func feedbackAudioCodec(existing string, target *playbackprofile.TargetPlaybackProfile) string {
	if existing = strings.TrimSpace(existing); existing != "" {
		return existing
	}
	if target == nil {
		return ""
	}
	return strings.TrimSpace(target.Audio.Codec)
}

func firstNonEmptyFeedback(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
