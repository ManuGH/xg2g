package recordings

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/http/v3/autocodec"
	"github.com/ManuGH/xg2g/internal/control/http/v3/clientpolicy"
	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/control/playbackshadow"
	domainrecordings "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
	"github.com/ManuGH/xg2g/internal/domain/playbackplanner"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

// ResolvePlaybackInfo resolves the truthful playback inputs up to the decision engine boundary.
func (s *Service) ResolvePlaybackInfo(ctx context.Context, req PlaybackInfoRequest) (PlaybackInfoResult, *PlaybackInfoError) {
	svc := s.deps.RecordingsService()
	if svc == nil {
		return PlaybackInfoResult{}, &PlaybackInfoError{
			Kind:    PlaybackInfoErrorUnavailable,
			Message: "Recordings service is not initialized",
		}
	}

	req = s.applyCapabilityRegistryFallback(ctx, req)

	sourceRef, truth, liveTruth, err := s.resolveSubjectTruth(ctx, req, svc)
	if err != nil {
		return PlaybackInfoResult{}, err
	}

	cfg := s.deps.Config()
	operatorPolicy := cfg.Playback.Operator
	operatorRuleName := ""
	operatorRuleScope := ""
	if req.SubjectKind == PlaybackSubjectRecording {
		effectiveOperator, matchedRule := profiles.ResolveEffectivePlaybackOperatorConfig(cfg.Playback.Operator, profiles.OperatorRuleModeRecording, sourceRef)
		operatorPolicy = effectiveOperator
		if matchedRule != nil {
			operatorRuleName = strings.TrimSpace(matchedRule.Name)
			operatorRuleScope = profiles.NormalizeOperatorRuleMode(matchedRule.Mode)
		}
	}

	capabilityContract := domainrecordings.ResolveCapabilityContract(
		ctx,
		req.PrincipalID,
		req.APIVersion,
		req.RequestedProfile,
		req.Headers,
		req.Capabilities,
		string(req.SubjectKind),
		req.ClientProfile,
	)
	resolvedCaps := capabilityContract.Effective
	hostContext := s.buildRequestHostContext(ctx)
	hostPressure := s.deps.HostPressure(ctx)
	operatorPolicy, runtimeFeedbackPolicy := s.applyPlaybackFeedbackPolicy(ctx, sourceRef, req, truth, resolvedCaps, hostContext, hostPressure, operatorPolicy)

	if req.SchemaType == "live" {
		plannerEval, err := s.buildPlannerEvaluation(req, sourceRef, truth, liveTruth, resolvedCaps, capabilityContract.PolicyVersion, hostContext, hostPressure, operatorPolicy)
		if err != nil {
			return PlaybackInfoResult{}, classifyPlannerError(err)
		}
		hostFingerprint, deviceFingerprint, sourceFingerprint := s.rememberCapabilitySnapshots(ctx, hostContext, sourceRef, req, truth, liveTruth, resolvedCaps)

		s.recordCapabilityObservation(ctx, sourceRef, req, truth, resolvedCaps, nil, hostFingerprint, deviceFingerprint, sourceFingerprint, plannerEval)

		return PlaybackInfoResult{
			SourceRef:                 sourceRef,
			Truth:                     truth,
			CapabilityContract:        capabilityContract,
			ResolvedCapabilities:      resolvedCaps,
			Decision:                  nil,
			ClientProfile:             req.ClientProfile,
			OperatorRuleName:          operatorRuleName,
			OperatorRuleScope:         operatorRuleScope,
			RuntimePolicyAction:       playbackPolicyActionName(runtimeFeedbackPolicy),
			RuntimePolicyPhase:        playbackPolicyPhaseName(runtimeFeedbackPolicy),
			RuntimeProbeCandidate:     playbackPolicyProbeCandidateName(runtimeFeedbackPolicy),
			RuntimePolicyReasons:      playbackPolicyReasonNames(runtimeFeedbackPolicy),
			RuntimePolicyConstraints:  playbackPolicyConstraintNames(runtimeFeedbackPolicy),
			RuntimeProbeSuccessStreak: playbackPolicyProbeSuccessStreak(runtimeFeedbackPolicy),
			RuntimeProbeFailureStreak: playbackPolicyProbeFailureStreak(runtimeFeedbackPolicy),
			PlannerEvaluation:         plannerEval,
		}, nil
	}

	input := buildDecisionInput(
		req,
		truth,
		resolvedCaps,
		cfg,
		operatorPolicy,
		hostPressure,
		hostContext.Snapshot.Runtime,
	)

	ctx = decision.WithShadowCollector(ctx, decision.NewShadowCollector())
	_, dec, prob := decision.Decide(ctx, input, req.SchemaType)
	if prob != nil {
		return PlaybackInfoResult{}, &PlaybackInfoError{
			Kind: PlaybackInfoErrorProblem,
			Problem: &PlaybackInfoProblem{
				Status: prob.Status,
				Type:   prob.Type,
				Title:  prob.Title,
				Code:   prob.Code,
				Detail: prob.Detail,
			},
		}
	}

	alignAutoCodecDecisionWithPolicy(req, resolvedCaps, hostContext.Snapshot.Runtime, s.profileResolver, s.clientAV1Disabled, s.iosNativeHEVCHWMode, dec)
	applyPlaybackTransportPolicyWithPolicy(req, resolvedCaps, dec, s.profileResolver.ConfigSnapshot().ExperimentalAV1MPEGTS)
	hostFingerprint, deviceFingerprint, sourceFingerprint := s.rememberCapabilitySnapshots(ctx, hostContext, sourceRef, req, truth, liveTruth, resolvedCaps)
	s.recordDecisionAudit(ctx, hostContext, sourceRef, req, resolvedCaps, input, dec)

	var plannerEval *PlannerEvaluation
	if s.observer != nil && PlaybackInfoRequestContext(req) != PlaybackInfoContextEpgBadge {
		plannerEval, _ = s.buildPlannerEvaluation(req, sourceRef, truth, liveTruth, resolvedCaps, capabilityContract.PolicyVersion, hostContext, hostPressure, operatorPolicy)
		if plannerEval != nil {
			s.submitPlannerShadowObservation(plannerEval.Evidence, dec, req)
		}
	}
	s.recordCapabilityObservation(ctx, sourceRef, req, truth, resolvedCaps, dec, hostFingerprint, deviceFingerprint, sourceFingerprint, plannerEval)

	return PlaybackInfoResult{
		SourceRef:                 sourceRef,
		Truth:                     truth,
		CapabilityContract:        capabilityContract,
		ResolvedCapabilities:      resolvedCaps,
		Decision:                  dec,
		ClientProfile:             req.ClientProfile,
		OperatorRuleName:          operatorRuleName,
		OperatorRuleScope:         operatorRuleScope,
		RuntimePolicyAction:       playbackPolicyActionName(runtimeFeedbackPolicy),
		RuntimePolicyPhase:        playbackPolicyPhaseName(runtimeFeedbackPolicy),
		RuntimeProbeCandidate:     playbackPolicyProbeCandidateName(runtimeFeedbackPolicy),
		RuntimePolicyReasons:      playbackPolicyReasonNames(runtimeFeedbackPolicy),
		RuntimePolicyConstraints:  playbackPolicyConstraintNames(runtimeFeedbackPolicy),
		RuntimeProbeSuccessStreak: playbackPolicyProbeSuccessStreak(runtimeFeedbackPolicy),
		RuntimeProbeFailureStreak: playbackPolicyProbeFailureStreak(runtimeFeedbackPolicy),
		PlannerEvaluation:         plannerEval,
	}, nil
}

func classifyPlannerError(err error) *PlaybackInfoError {
	if err == nil {
		return nil
	}
	if errors.Is(err, playbackplanner.ErrRuleNotImplemented) {
		return &PlaybackInfoError{Kind: PlaybackInfoErrorUnsupported, Message: err.Error(), Cause: err}
	}
	if errors.Is(err, playbackplanner.ErrInvalidEvidence) {
		return &PlaybackInfoError{Kind: PlaybackInfoErrorUnverified, Message: err.Error(), Cause: err}
	}
	return &PlaybackInfoError{Kind: PlaybackInfoErrorInternal, Message: err.Error(), Cause: err}
}

func playbackPolicyActionName(policy *playbackFeedbackPolicy) string {
	if policy == nil {
		return ""
	}
	return strings.TrimSpace(string(policy.Policy.Action))
}

func playbackPolicyPhaseName(policy *playbackFeedbackPolicy) string {
	if policy == nil {
		return ""
	}

	action := strings.TrimSpace(string(policy.Policy.Action))
	switch action {
	case "probe_up":
		return "probing"
	case "cooldown":
		return "cooldown"
	case "degrade":
		return "degraded"
	case "lock_current":
		return "recovering"
	}

	if playbackPolicyHasReason(policy, runtimepolicy.ReasonProbeRecentlyRegressed, runtimepolicy.ReasonProbeWindowRegressed) {
		return "probe_regressed"
	}

	switch policy.Confidence.State {
	case runtimepolicy.ConfidenceRecovery:
		return "recovering"
	case runtimepolicy.ConfidenceLow:
		return "degraded"
	case runtimepolicy.ConfidenceStable, runtimepolicy.ConfidenceHigh:
		return "stable"
	default:
		return ""
	}
}

func playbackPolicyProbeCandidateName(policy *playbackFeedbackPolicy) string {
	if policy == nil {
		return ""
	}
	return strings.TrimSpace(string(policy.Policy.ProbeCandidate))
}

func playbackPolicyReasonNames(policy *playbackFeedbackPolicy) []string {
	if policy == nil || len(policy.Policy.Reasons) == 0 {
		return nil
	}
	out := make([]string, 0, len(policy.Policy.Reasons))
	for _, reason := range policy.Policy.Reasons {
		if strings.TrimSpace(reason) != "" {
			out = append(out, reason)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func playbackPolicyConstraintNames(policy *playbackFeedbackPolicy) []string {
	if policy == nil || len(policy.Policy.PolicyConstraints) == 0 {
		return nil
	}
	out := make([]string, 0, len(policy.Policy.PolicyConstraints))
	for _, constraint := range policy.Policy.PolicyConstraints {
		if strings.TrimSpace(constraint) != "" {
			out = append(out, constraint)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func playbackPolicyHasReason(policy *playbackFeedbackPolicy, reasons ...string) bool {
	if policy == nil || len(reasons) == 0 {
		return false
	}
	for _, candidate := range policy.Policy.Reasons {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		for _, reason := range reasons {
			if candidate == strings.TrimSpace(reason) {
				return true
			}
		}
	}
	return false
}

func playbackPolicyProbeSuccessStreak(policy *playbackFeedbackPolicy) int {
	if policy == nil {
		return 0
	}
	return policy.Confidence.ProbeSuccessStreak
}

func playbackPolicyProbeFailureStreak(policy *playbackFeedbackPolicy) int {
	if policy == nil {
		return 0
	}
	return policy.Confidence.ProbeFailureStreak
}

func (s *Service) resolveSubjectTruth(ctx context.Context, req PlaybackInfoRequest, svc RecordingsService) (string, playback.MediaTruth, *liveTruthResolution, *PlaybackInfoError) {
	subjectID := strings.TrimSpace(req.SubjectID)
	if subjectID == "" {
		return "", playback.MediaTruth{}, nil, &PlaybackInfoError{
			Kind:    PlaybackInfoErrorInvalidInput,
			Message: "subject id is required",
		}
	}

	switch req.SubjectKind {
	case PlaybackSubjectLive:
		if err := domainrecordings.ValidateLiveRef(subjectID); err != nil {
			return "", playback.MediaTruth{}, nil, &PlaybackInfoError{
				Kind:    PlaybackInfoErrorInvalidInput,
				Message: "serviceRef must be a valid live Enigma2 reference",
				Cause:   err,
			}
		}
		source := s.deps.ChannelTruthSource()
		requestContext := PlaybackInfoRequestContext(req)
		truthResolution := resolveLiveTruthState(subjectID, source, requestContext)
		if truthResolution.Verified() && truthResolution.Stale && probeAllowedForContext(requestContext) {
			if probeSource, ok := source.(channelTruthProbeSource); ok {
				s.refreshLiveTruthAsync(ctx, probeSource, subjectID)
			}
		}
		if !truthResolution.Verified() && shouldProbeLiveTruth(truthResolution) && probeAllowedForContext(requestContext) {
			if probeSource, ok := source.(channelTruthProbeSource); ok {
				cachedResolution := truthResolution
				probedCap, found, completed, probeErr := s.probeLiveTruthBounded(ctx, probeSource, subjectID)
				switch {
				case !completed:
					// persistent truth cache, so the client's retry is served from cache
					// instead of freezing here for ~20-33s.
					evt := log.L().Info().
						Str("serviceRef", subjectID).
						Dur("budget", liveInteractiveProbeBudget())
					if requestContext != "" {
						evt = evt.Str("request_context", requestContext)
					}
					evt.Msg("live playback truth probe exceeded interactive budget; continuing in background")
				default:
					if probeErr != nil {
						evt := log.L().Warn().
							Err(probeErr).
							Str("serviceRef", subjectID)
						if requestContext != "" {
							evt = evt.Str("request_context", requestContext)
						}
						evt.Msg("live playback truth probe failed")
					}
					if found || cachedResolution.Reason == "missing_scan_truth" {
						truthResolution = resolveLiveTruthCapability(subjectID, probedCap, found, time.Now().UTC(), requestContext)
					}
				}
			}
		}
		if !truthResolution.Verified() {
			return "", playback.MediaTruth{}, &truthResolution, playbackInfoErrorForLiveTruth(truthResolution)
		}
		return subjectID, truthResolution.Truth, &truthResolution, nil
	case PlaybackSubjectRecording:
		sourceRef, ok := domainrecordings.DecodeRecordingID(subjectID)
		if !ok {
			return "", playback.MediaTruth{}, nil, &PlaybackInfoError{
				Kind:    PlaybackInfoErrorInvalidInput,
				Message: "Invalid recording ID format",
			}
		}

		truth, err := svc.GetMediaTruth(ctx, subjectID)
		if err != nil {
			return "", playback.MediaTruth{}, nil, classifyPlaybackInfoError(err)
		}

		switch truth.Status {
		case playback.MediaStatusPreparing:
			return "", playback.MediaTruth{}, nil, &PlaybackInfoError{
				Kind:              PlaybackInfoErrorPreparing,
				Message:           "Retry shortly.",
				RetryAfterSeconds: truth.RetryAfter,
				ProbeState:        string(truth.ProbeState),
			}
		case playback.MediaStatusUpstreamUnavailable:
			return "", playback.MediaTruth{}, nil, &PlaybackInfoError{
				Kind:    PlaybackInfoErrorUpstreamUnavailable,
				Message: "Retry later.",
			}
		case playback.MediaStatusNotFound:
			return "", playback.MediaTruth{}, nil, &PlaybackInfoError{
				Kind:    PlaybackInfoErrorNotFound,
				Message: "recording not found",
			}
		}
		return sourceRef, truth, nil, nil
	default:
		return "", playback.MediaTruth{}, nil, &PlaybackInfoError{
			Kind:    PlaybackInfoErrorInvalidInput,
			Message: "unsupported playback subject kind",
		}
	}
}

// probeAllowedForContext gates the synchronous relay probe by request context.
// epg_badge requests are passive previews for the channel grid: fanning a cold relay
// probe out across the whole visible list storms the tuner/relay and starves the
// interactive play path (and the badge result is throwaway anyway). Badges are served
// from persisted scan truth when present and stay blank otherwise; the scan manager's
// background refresh warms their truth. Only interactive contexts (player_start and
// legacy/unknown) drive on-demand probing.
func probeAllowedForContext(requestContext string) bool {
	return requestContext != PlaybackInfoContextEpgBadge
}

func shouldProbeLiveTruth(resolution liveTruthResolution) bool {
	// stale_scan_truth is intentionally absent: stale-but-complete truth is served
	// from cache (stale-while-revalidate) and never reaches this unverified path.
	switch resolution.Reason {
	case "missing_scan_truth", "partial_scan_truth", "incomplete_scan_truth", "failed_scan_truth":
		return true
	default:
		return false
	}
}

func playbackInfoErrorForLiveTruth(resolution liveTruthResolution) *PlaybackInfoError {
	message := "Live media truth unavailable"
	switch resolution.Reason {
	case "scanner_unavailable":
		message = "Live scan truth unavailable"
	case "missing_scan_truth":
		message = "Live media truth missing"
	case "inactive_event_feed":
		message = "Live event feed inactive"
	case "partial_scan_truth", "incomplete_scan_truth":
		message = "Live media truth incomplete"
	case "failed_scan_truth":
		message = "Live media truth failed"
	}

	return &PlaybackInfoError{
		Kind:              PlaybackInfoErrorUnverified,
		Message:           message,
		RetryAfterSeconds: 5,
		TruthState:        string(resolution.State),
		TruthReason:       resolution.Reason,
		TruthOrigin:       resolution.Origin,
		ProblemFlags:      append([]string(nil), resolution.ProblemFlags...),
	}
}

func (s *Service) recordDecisionAudit(ctx context.Context, hostContext requestHostContext, sourceRef string, req PlaybackInfoRequest, resolvedCaps capabilities.PlaybackCapabilities, input decision.DecisionInput, dec *decision.Decision) {
	sink := s.deps.DecisionAuditSink()
	if sink == nil || dec == nil {
		return
	}

	clientFamily := strings.TrimSpace(resolvedCaps.ClientFamilyFallback)
	if clientFamily == "" {
		clientFamily = req.ClientProfile
	}

	event, err := decision.BuildEvent(decision.EventMetadata{
		ServiceRef:       sourceRef,
		SubjectKind:      string(req.SubjectKind),
		Origin:           decision.OriginRuntime,
		ClientFamily:     clientFamily,
		ClientCapsSource: resolvedCaps.ClientCapsSource,
		DeviceType:       resolvedCaps.DeviceType,
		HostFingerprint:  hostContext.DecisionFingerprint,
		DecidedAt:        time.Now().UTC(),
	}, input, dec)
	if err != nil {
		log.L().Warn().Err(err).Str("serviceRef", sourceRef).Msg("decision audit: failed to build event")
		return
	}
	if err := sink.Record(ctx, event); err != nil {
		log.L().Warn().Err(err).Str("serviceRef", sourceRef).Str("basisHash", event.BasisHash).Msg("decision audit: failed to persist event")
	}

	for _, divergence := range decision.ShadowDivergencesFromContext(ctx) {
		shadowEvent, err := decision.BuildShadowDivergenceEvent(event, divergence)
		if err != nil {
			log.L().Warn().Err(err).Str("serviceRef", sourceRef).Msg("decision audit: failed to build shadow divergence event")
			continue
		}
		if err := sink.Record(ctx, shadowEvent); err != nil {
			log.L().Warn().Err(err).Str("serviceRef", sourceRef).Str("basisHash", shadowEvent.BasisHash).Msg("decision audit: failed to persist shadow divergence event")
		}
	}
}

func alignAutoCodecDecisionWithPolicy(req PlaybackInfoRequest, resolvedCaps capabilities.PlaybackCapabilities, hostRuntime playbackprofile.HostRuntimeSnapshot, profileResolver profiles.Resolver, clientAV1Disabled bool, iosNativeHEVCHWMode string, dec *decision.Decision) {
	if req.Capabilities == nil || dec == nil || dec.Mode != decision.ModeTranscode || !shouldApplyAutoCodecDecision(req.RequestedProfile) {
		return
	}

	// Copy-first: if the decision engine already keeps the video on the copy
	// path (ModeTranscode driven only by the audio codec while the video stays
	// copy), do NOT let the auto-codec re-encode it. buildTargetProfile has
	// already produced video=copy + audio=transcode/AAC — exactly what
	// stream.start runs for this source — so the preview must report that copy.
	// Re-encoding a copyable source only loses quality and made the stats panel
	// lie (it predicted a phantom transcode the executed stream never performs).
	// Resolving a fresh profile here is wrong: ProfileSafari with a nil capability
	// snapshot falls through to its interlaced/unknown branch (TranscodeVideo=true)
	// and would clobber the copy. Just skip the auto-codec; it still applies below
	// when the video itself needs transcoding (video.Mode == transcode).
	if dec.TargetProfile != nil && dec.TargetProfile.Video.Mode == playbackprofile.MediaModeCopy {
		// Surface the audio transcode the target already specifies
		// (buildTargetProfile forces Audio -> transcode/AAC when the video stays
		// copy) so the selected formats match the executed stream. Leave the
		// copied video and container untouched.
		if audioCodec := strings.TrimSpace(dec.TargetProfile.Audio.Codec); audioCodec != "" {
			dec.Selected.AudioCodec = audioCodec
		}
		return
	}

	profileID := pickPlaybackInfoAutoProfileWithPolicy(resolvedCaps, hostRuntime, clientAV1Disabled, iosNativeHEVCHWMode)
	if profileID == "" {
		return
	}

	profileSpec := profileResolver.Resolve(profileID, "", 0, nil, resolvePlaybackInfoGPUBackend(profileID), profiles.HWAccelAuto)
	target := model.TraceTargetProfileFromProfile(profileSpec)
	if target != nil {
		dec.TargetProfile = target
		dec.Selected.Container = target.Container
		if strings.TrimSpace(target.Video.Codec) != "" {
			dec.Selected.VideoCodec = target.Video.Codec
		}
		if strings.TrimSpace(target.Audio.Codec) != "" {
			dec.Selected.AudioCodec = target.Audio.Codec
		}
	}

	if publicProfile := profiles.PublicProfileName(profileID); publicProfile != "" {
		dec.Trace.RequestedIntent = publicProfile
		dec.Trace.ResolvedIntent = publicProfile
	}
	selectionTrace := autocodec.DescribeSelection(strings.Join(autocodec.ResolveAutoTranscodeCodecsWithPolicy(resolvedCaps, clientAV1Disabled), ","), profileID, hostRuntime)
	dec.Trace.AutoCodecPolicy = selectionTrace.Policy
	dec.Trace.AutoCodecRequested = selectionTrace.RequestedCodecs
	dec.Trace.AutoCodecSelected = selectionTrace.SelectedCodec
	dec.Trace.AutoCodecHostClass = selectionTrace.PerformanceClass
	dec.Trace.AutoCodecBenchClass = selectionTrace.CodecBenchmarkClass
}

func pickPlaybackInfoAutoProfileWithPolicy(resolvedCaps capabilities.PlaybackCapabilities, hostRuntime playbackprofile.HostRuntimeSnapshot, clientAV1Disabled bool, iosNativeHEVCHWMode string) string {
	if profileID := autocodec.PickNativeHLSProfileForClientAndHostWithPolicy("", resolvedCaps.ClientFamilyFallback, &resolvedCaps, profiles.HWAccelAuto, hostRuntime, clientAV1Disabled); profileID != "" {
		return autocodec.ApplyClientCompatibilityProfileIDWithPolicy(resolvedCaps.ClientFamilyFallback, profileID, iosNativeHEVCHWMode)
	}
	return autocodec.ApplyClientCompatibilityProfileIDWithPolicy(
		resolvedCaps.ClientFamilyFallback,
		autocodec.PickProfileForCapabilitiesForClientAndHostWithPolicy(resolvedCaps, resolvedCaps.ClientFamilyFallback, profiles.HWAccelAuto, hostRuntime, clientAV1Disabled),
		iosNativeHEVCHWMode,
	)
}

func shouldApplyAutoCodecDecision(requestedProfile string) bool {
	switch strings.ToLower(strings.TrimSpace(requestedProfile)) {
	case "direct", "copy", "passthrough", "compatible", "high", "bandwidth", "low", "repair", "h264_fmp4", "safari_dirty":
		return false
	default:
		return true
	}
}

func resolvePlaybackInfoGPUBackend(profileID string) profiles.GPUBackend {
	switch profiles.NormalizeRequestedProfileID(profileID) {
	case profiles.ProfileAV1HW:
		return hardware.PreferredGPUBackendForCodec("av1")
	case profiles.ProfileSafariHEVCHW, profiles.ProfileSafariHEVCHWLL:
		return hardware.PreferredGPUBackendForCodec("hevc")
	case profiles.ProfileH264FMP4:
		return hardware.PreferredGPUBackendForCodec("h264")
	default:
		return hardware.PreferredGPUBackend()
	}
}

func classifyPlaybackInfoError(err error) *PlaybackInfoError {
	class := domainrecordings.Classify(err)
	msg := err.Error()

	switch class {
	case domainrecordings.ClassInvalidArgument:
		return &PlaybackInfoError{Kind: PlaybackInfoErrorInvalidInput, Message: msg, Cause: err}
	case domainrecordings.ClassForbidden:
		return &PlaybackInfoError{Kind: PlaybackInfoErrorForbidden, Message: msg, Cause: err}
	case domainrecordings.ClassNotFound:
		return &PlaybackInfoError{Kind: PlaybackInfoErrorNotFound, Message: msg, Cause: err}
	case domainrecordings.ClassPreparing:
		return &PlaybackInfoError{
			Kind:              PlaybackInfoErrorPreparing,
			Message:           msg,
			RetryAfterSeconds: 5,
			ProbeState:        string(playback.ProbeStateInFlight),
			Cause:             err,
		}
	case domainrecordings.ClassUnsupported:
		return &PlaybackInfoError{Kind: PlaybackInfoErrorUnsupported, Message: msg, Cause: err}
	case domainrecordings.ClassUpstream:
		return &PlaybackInfoError{Kind: PlaybackInfoErrorUpstreamUnavailable, Message: msg, Cause: err}
	default:
		return &PlaybackInfoError{
			Kind:    PlaybackInfoErrorInternal,
			Message: "An unexpected error occurred",
			Cause:   err,
		}
	}
}

func buildDecisionInput(
	req PlaybackInfoRequest,
	truth playback.MediaTruth,
	resolvedCaps capabilities.PlaybackCapabilities,
	cfg config.AppConfig,
	operatorPolicy config.PlaybackOperatorConfig,
	hostPressure playbackprofile.HostPressureAssessment,
	hostRuntime playbackprofile.HostRuntimeSnapshot,
) decision.DecisionInput {
	serverCanTranscode := strings.TrimSpace(cfg.FFmpeg.Bin) != "" && strings.TrimSpace(cfg.HLS.Root) != ""
	clientAllowsTranscode := resolvedCaps.AllowTranscode == nil || *resolvedCaps.AllowTranscode
	allowTranscode := serverCanTranscode && clientAllowsTranscode
	mappedCaps := decision.FromCapabilities(resolvedCaps)
	source := decisionSourceFromMediaTruth(truth)
	videoBenchmarkClass := benchmarkClassForPlaybackPath(hostRuntime.Benchmark, truth, source, mappedCaps)

	return decision.DecisionInput{
		RequestID:       req.RequestID,
		RequestedIntent: playbackprofile.NormalizeRequestedIntent(req.RequestedProfile),
		APIVersion:      req.APIVersion,
		Source:          source,
		Capabilities:    mappedCaps,
		Policy: decision.Policy{
			AllowTranscode: allowTranscode,
			Operator: decision.OperatorPolicy{
				ForceIntent:    playbackprofile.NormalizeRequestedIntent(operatorPolicy.ForceIntent),
				MaxQualityRung: playbackprofile.NormalizeQualityRung(operatorPolicy.MaxQualityRung),
			},
			Host: decision.HostPolicy{
				PressureBand:     playbackprofile.NormalizeHostPressureBand(string(hostPressure.EffectiveBand)),
				PerformanceClass: hostRuntime.PerformanceClass,
				BenchmarkClass:   videoBenchmarkClass,
			},
		},
	}
}

func decisionSourceFromMediaTruth(truth playback.MediaTruth) decision.Source {
	return decision.Source{
		Container:         truth.Container,
		VideoCodec:        truth.VideoCodec,
		AudioCodec:        truth.AudioCodec,
		BitrateKbps:       truth.BitrateKbps,
		BitrateConfidence: truth.BitrateConfidence,
		Width:             truth.Width,
		Height:            truth.Height,
		FPS:               truth.FPS,
		Interlaced:        truth.Interlaced,
	}
}

func benchmarkClassForPlaybackPath(snapshot playbackprofile.HostBenchmarkSnapshot, truth playback.MediaTruth, source decision.Source, caps decision.Capabilities) string {
	if profileID := benchmarkProfileForPlaybackPath(truth, source, caps); profileID != "" {
		if class := playbackprofile.BenchmarkClassForProfile(snapshot, profileID); class != "" {
			return class
		}
	}
	return playbackprofile.BenchmarkClassForCodec(snapshot, "h264")
}

func benchmarkProfileForPlaybackPath(truth playback.MediaTruth, source decision.Source, caps decision.Capabilities) string {
	if benchmarkProfileIsAudioOnly(source, caps) {
		return playbackprofile.BenchmarkProfileAudioAACStereo
	}
	return benchmarkProfileForTruth(truth)
}

func benchmarkProfileForTruth(truth playback.MediaTruth) string {
	pixels := truth.Width * truth.Height
	signalFPS := truth.SignalFPS
	if signalFPS <= 0 {
		signalFPS = truth.FPS
	}
	switch {
	case truth.Interlaced && pixels >= 1920*1080 && signalFPS >= 50:
		return playbackprofile.BenchmarkProfileVideoH2641080I50
	case truth.Interlaced && pixels >= 1920*1080:
		return playbackprofile.BenchmarkProfileVideoH2641080I
	case pixels >= 3840*2160 && signalFPS >= 50:
		return playbackprofile.BenchmarkProfileVideoH2642160P50
	case pixels >= 3840*2160:
		return playbackprofile.BenchmarkProfileVideoH2642160P
	case pixels >= 1920*1080:
		return playbackprofile.BenchmarkProfileVideoH2641080P
	default:
		return ""
	}
}

func benchmarkProfileIsAudioOnly(source decision.Source, caps decision.Capabilities) bool {
	return decision.CanKeepVideoCopy(source, caps) && !decision.CanKeepAudioCopy(source, caps)
}

func (s *Service) buildPlannerEvaluation(
	req PlaybackInfoRequest,
	sourceRef string,
	truth playback.MediaTruth,
	liveTruth *liveTruthResolution,
	resolvedCaps capabilities.PlaybackCapabilities,
	capabilityPolicyVersion string,
	hostContext requestHostContext,
	hostPressure playbackprofile.HostPressureAssessment,
	operatorPolicy config.PlaybackOperatorConfig,
) (*PlannerEvaluation, error) {
	scope := "recording"
	if req.SubjectKind == PlaybackSubjectLive {
		scope = "live"
	}
	dvrWindowSeconds := 0
	if scope == "live" {
		dvrWindowSeconds = int(s.deps.Config().HLS.DVRWindow.Seconds())
	}

	clientFamily := req.ClientProfile
	if strings.TrimSpace(resolvedCaps.ClientFamilyFallback) != "" {
		clientFamily = resolvedCaps.ClientFamilyFallback
	}

	provenance := "recordings"
	confidence := "ok"
	var observedAt, validUntil int64
	if liveTruth != nil {
		provenance = liveTruth.Origin
		if liveTruth.Stale {
			confidence = "stale"
		} else {
			confidence = string(liveTruth.State)
		}
		if !liveTruth.ObservedAt.IsZero() {
			observedAt = liveTruth.ObservedAt.UnixMilli()
		}
		if !liveTruth.ValidUntil.IsZero() {
			validUntil = liveTruth.ValidUntil.UnixMilli()
		}
	}

	allowTranscode := true
	if resolvedCaps.AllowTranscode != nil {
		allowTranscode = *resolvedCaps.AllowTranscode
	}

	maxVideoW, maxVideoH, maxVideoFPS := 0, 0, 0
	if resolvedCaps.MaxVideo != nil {
		maxVideoW = resolvedCaps.MaxVideo.Width
		maxVideoH = resolvedCaps.MaxVideo.Height
		maxVideoFPS = resolvedCaps.MaxVideo.Fps
	}

	now := time.Now()
	var availableEngines []string
	if hostContext.Snapshot.Runtime.Capabilities.HLSAvailable {
		availableEngines = append(availableEngines, "hls")
	}

	var downlink, rtt int
	var internetValidated bool
	if resolvedCaps.NetworkContext != nil {
		downlink = resolvedCaps.NetworkContext.DownlinkKbps
		if resolvedCaps.NetworkContext.InternetValidated != nil {
			internetValidated = *resolvedCaps.NetworkContext.InternetValidated
		}
	}

	autoTranscodeVideoCodecs := plannerAutoTranscodeVideoCodecs(req, resolvedCaps, s.clientAV1Disabled)
	profilePolicy := s.profileResolver.ConfigSnapshot()
	experimentalAV1MPEGTS := clientpolicy.AllowPlannerExperimentalAV1MPEGTS(
		resolvedCaps,
		profilePolicy.ExperimentalAV1MPEGTS,
	)
	networkCaptureTime := int64(0)
	if resolvedCaps.NetworkContext != nil {
		networkCaptureTime = now.UnixMilli()
	}
	legacyInput := playbackshadow.LegacyPlanningInput{
		EvaluatedAt:              now.UnixMilli(),
		Scope:                    scope,
		RequestedIntent:          req.RequestedProfile,
		SourceIdentity:           sourceRef,
		Provenance:               provenance,
		Confidence:               confidence,
		ObservedAt:               observedAt,
		ValidUntil:               validUntil,
		NetworkCaptureTime:       networkCaptureTime,
		PolicyVersion:            capabilityPolicyVersion,
		Container:                truth.Container,
		VideoCodec:               truth.VideoCodec,
		AudioCodec:               truth.AudioCodec,
		Width:                    truth.Width,
		Height:                   truth.Height,
		FPS:                      int(truth.FPS),
		Interlaced:               truth.Interlaced,
		BitrateKbps:              truth.BitrateKbps,
		BitrateConfidence:        string(truth.BitrateConfidence),
		ClientFamily:             clientFamily,
		DeviceType:               resolvedCaps.DeviceType,
		CapabilityVersion:        strconv.Itoa(resolvedCaps.CapabilitiesVersion),
		AllowTranscode:           allowTranscode,
		SupportedContainers:      resolvedCaps.Containers,
		SupportedVideoCodecs:     resolvedCaps.VideoCodecs,
		SupportedAudioCodecs:     resolvedCaps.AudioCodecs,
		AutoTranscodeVideoCodecs: autoTranscodeVideoCodecs,
		MaxVideoWidth:            maxVideoW,
		MaxVideoHeight:           maxVideoH,
		MaxVideoFPS:              maxVideoFPS,
		PreferredEngine:          resolvedCaps.PreferredHLSEngine,
		SupportedEngines:         resolvedCaps.HLSEngines,
		PrefersFMP4:              clientWantsFMP4(req, resolvedCaps, nil),
		SupportsHls:              resolvedCaps.SupportsHLS,
		SupportsRange:            resolvedCaps.SupportsRange,
		DownlinkKbps:             downlink,
		RTTMillis:                rtt,
		InternetValidated:        internetValidated,
		HostPressureBand:         string(hostPressure.EffectiveBand),
		PerformanceClass:         hostContext.Snapshot.Runtime.PerformanceClass,
		BenchmarkClass:           playbackprofile.BenchmarkClassForCodec(hostContext.Snapshot.Runtime.Benchmark, "hevc"),
		AvailableEngines:         availableEngines,
		HostEncoderCapabilities:  plannerHostEncoderCapabilities(hostContext),
		ForceIntent:              operatorPolicy.ForceIntent,
		MaxQualityRung:           operatorPolicy.MaxQualityRung,
		DisableTranscoding:       false,
		MaxGlobalBitrate:         0,
		StrictFreshness:          false,
		DVRWindowSeconds:         dvrWindowSeconds,
		ExperimentalAV1MPEGTS:    experimentalAV1MPEGTS,
	}

	ev, err := playbackshadow.BuildPlaybackEvidence(legacyInput)
	if err != nil {
		log.L().Debug().Err(err).Msg("failed to build playback evidence for planner")
		return nil, err
	}
	res, err := playbackplanner.Plan(ev)
	if err != nil {
		log.L().Debug().Err(err).Msg("failed to evaluate playback planner")
		return nil, err
	}
	return &PlannerEvaluation{
		Evidence: ev,
		Result:   res,
	}, nil
}

func (s *Service) submitPlannerShadowObservation(ev playbackplanner.PlaybackEvidence, dec *decision.Decision, req PlaybackInfoRequest) {
	if s.observer == nil || dec == nil || PlaybackInfoRequestContext(req) == PlaybackInfoContextEpgBadge {
		return
	}
	obs := playbackshadow.ShadowObservation{
		Evidence: ev,
		Legacy:   playbackshadow.ComparableFromLegacy(dec),
	}
	s.observer.TryObserve(obs)
}

func plannerAutoTranscodeVideoCodecs(req PlaybackInfoRequest, resolvedCaps capabilities.PlaybackCapabilities, clientAV1Disabled bool) []string {
	// Legacy auto-codec selection is request opt-in: family fallback may fill
	// compatibility fields, but it must not manufacture a codec negotiation for
	// an older request that supplied no capabilities object at all.
	if req.Capabilities == nil {
		return nil
	}

	codecs := autocodec.ResolveAutoTranscodeCodecsWithPolicy(resolvedCaps, clientAV1Disabled)
	family := strings.ToLower(strings.TrimSpace(resolvedCaps.ClientFamilyFallback))
	if family != playbackprofile.ClientSafariNative && family != playbackprofile.ClientIOSSafariNative {
		return codecs
	}
	if !stringSliceContainsFold(resolvedCaps.VideoCodecs, "hevc") || stringSliceContainsFold(codecs, "hevc") {
		return codecs
	}

	// Native WebKit can explicitly advertise HEVC without MediaCapabilities
	// smooth/power signals. This is sufficient on the legacy native-HLS path;
	// preserve the same evidence while keeping AV1 device-policy gated.
	insertAt := len(codecs)
	for i, codec := range codecs {
		if strings.EqualFold(strings.TrimSpace(codec), "h264") {
			insertAt = i
			break
		}
	}
	codecs = append(codecs, "")
	copy(codecs[insertAt+1:], codecs[insertAt:])
	codecs[insertAt] = "hevc"
	return codecs
}

func stringSliceContainsFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
}

func plannerHostEncoderCapabilities(hostContext requestHostContext) []playbackplanner.HostEncoderCapability {
	encoders := hostContext.Snapshot.EncoderCapabilities
	out := make([]playbackplanner.HostEncoderCapability, 0, len(encoders))
	for _, encoder := range encoders {
		out = append(out, playbackplanner.HostEncoderCapability{
			Codec:          encoder.Codec,
			Verified:       encoder.Verified,
			AutoEligible:   encoder.AutoEligible,
			ProbeElapsedMS: encoder.ProbeElapsedMS,
			BenchmarkClass: playbackprofile.BenchmarkClassForCodec(hostContext.Snapshot.Runtime.Benchmark, encoder.Codec),
		})
	}
	return out
}
