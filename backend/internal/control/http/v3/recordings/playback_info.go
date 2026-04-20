package recordings

import (
	"context"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/http/v3/autocodec"
	"github.com/ManuGH/xg2g/internal/control/playback"
	domainrecordings "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/normalize"
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

	resolvedCaps := domainrecordings.ResolveCapabilities(
		ctx,
		req.PrincipalID,
		req.APIVersion,
		req.RequestedProfile,
		req.Headers,
		req.Capabilities,
	)
	hostContext := s.buildRequestHostContext(ctx)
	hostPressure := s.deps.HostPressure(ctx)
	operatorPolicy, runtimeFeedbackPolicy := s.applyPlaybackFeedbackPolicy(ctx, sourceRef, req, truth, resolvedCaps, hostContext, hostPressure, operatorPolicy)

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

	alignAutoCodecDecision(req, resolvedCaps, dec)
	alignLiveNativePackaging(req, resolvedCaps, dec)
	alignRecordingNativePackaging(req, resolvedCaps, dec)
	hostFingerprint, deviceFingerprint, sourceFingerprint := s.rememberCapabilitySnapshots(ctx, hostContext, sourceRef, req, truth, liveTruth, resolvedCaps)
	s.recordDecisionAudit(ctx, hostContext, sourceRef, req, resolvedCaps, input, dec)
	s.recordCapabilityObservation(ctx, sourceRef, req, truth, resolvedCaps, dec, hostFingerprint, deviceFingerprint, sourceFingerprint)

	return PlaybackInfoResult{
		SourceRef:                 sourceRef,
		Truth:                     truth,
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
	}, nil
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
		truthResolution := resolveLiveTruthState(subjectID, source)
		if !truthResolution.Verified() && shouldProbeLiveTruth(truthResolution) {
			if probeSource, ok := source.(channelTruthProbeSource); ok {
				cachedResolution := truthResolution
				probedCap, found, probeErr := probeSource.ProbeCapability(ctx, subjectID)
				if probeErr != nil {
					log.L().Warn().
						Err(probeErr).
						Str("serviceRef", subjectID).
						Msg("live playback truth probe failed")
				}
				if found || cachedResolution.Reason == "missing_scan_truth" {
					truthResolution = resolveLiveTruthCapability(subjectID, probedCap, found, time.Now().UTC())
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

func shouldProbeLiveTruth(resolution liveTruthResolution) bool {
	switch resolution.Reason {
	case "missing_scan_truth", "partial_scan_truth", "incomplete_scan_truth", "failed_scan_truth", "stale_scan_truth":
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
	case "stale_scan_truth":
		message = "Live media truth stale"
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

func alignAutoCodecDecision(req PlaybackInfoRequest, resolvedCaps capabilities.PlaybackCapabilities, dec *decision.Decision) {
	if req.Capabilities == nil || dec == nil || dec.Mode != decision.ModeTranscode || !shouldApplyAutoCodecDecision(req.RequestedProfile) {
		return
	}

	profileID := pickPlaybackInfoAutoProfile(resolvedCaps)
	if profileID == "" {
		return
	}

	profileSpec := profiles.Resolve(profileID, "", 0, nil, resolvePlaybackInfoGPUBackend(profileID), profiles.HWAccelAuto)
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
}

func pickPlaybackInfoAutoProfile(resolvedCaps capabilities.PlaybackCapabilities) string {
	if profileID := autocodec.PickNativeHLSProfile("", resolvedCaps.ClientFamilyFallback, &resolvedCaps, profiles.HWAccelAuto); profileID != "" {
		return profileID
	}
	return autocodec.PickProfileForCapabilities(resolvedCaps, profiles.HWAccelAuto)
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

func alignLiveNativePackaging(req PlaybackInfoRequest, resolvedCaps capabilities.PlaybackCapabilities, dec *decision.Decision) {
	if req.SubjectKind != PlaybackSubjectLive || dec == nil || dec.TargetProfile == nil {
		return
	}
	if !clientWantsFMP4Packaging(req.RequestedProfile, resolvedCaps.ClientFamilyFallback) {
		return
	}
	if shouldKeepExperimentalNativeAV1TransportStream(resolvedCaps, dec) {
		return
	}
	if shouldPreferNativeDirectStream(dec) {
		rewriteDecisionToDirectStream(dec)
	}
	if dec.SelectedOutputKind != "hls" || !dec.TargetProfile.HLS.Enabled {
		return
	}

	target := *dec.TargetProfile
	target.Container = "fmp4"
	target.Packaging = playbackprofile.PackagingFMP4
	target.HLS.Enabled = true
	target.HLS.SegmentContainer = "fmp4"
	canonical := playbackprofile.CanonicalizeTarget(target)
	dec.TargetProfile = &canonical
	dec.Selected.Container = "fmp4"
	if dec.Mode == decision.ModeDirectStream {
		dec.Trace.QualityRung = string(playbackprofile.RungCompatibleHLSFMP4)
	}
}

func shouldKeepExperimentalNativeAV1TransportStream(resolvedCaps capabilities.PlaybackCapabilities, dec *decision.Decision) bool {
	if dec == nil || dec.TargetProfile == nil {
		return false
	}
	if !config.ParseBool("XG2G_EXPERIMENTAL_AV1_MPEGTS_ENABLED", false) {
		return false
	}
	switch normalize.Token(resolvedCaps.ClientFamilyFallback) {
	case playbackprofile.ClientSafariNative:
	default:
		return false
	}
	switch normalize.Token(resolvedCaps.ClientCapsSource) {
	case "runtime", "runtime_plus_family":
	default:
		return false
	}
	if !playbackInfoCapsHasToken(resolvedCaps.VideoCodecs, "av1") {
		return false
	}
	if !playbackInfoCapsHasToken(resolvedCaps.Containers, "ts") && !playbackInfoCapsHasToken(resolvedCaps.Containers, "mpegts") {
		return false
	}
	if !decisionTargetsVideoCodec(dec, "av1") {
		return false
	}
	target := playbackprofile.CanonicalizeTarget(*dec.TargetProfile)
	if target.Container != "mpegts" && target.Packaging != playbackprofile.PackagingTS && target.HLS.SegmentContainer != "mpegts" {
		return false
	}
	return true
}

func alignRecordingNativePackaging(req PlaybackInfoRequest, resolvedCaps capabilities.PlaybackCapabilities, dec *decision.Decision) {
	if req.SubjectKind != PlaybackSubjectRecording || dec == nil || dec.TargetProfile == nil {
		return
	}
	if !clientWantsFMP4Packaging(req.RequestedProfile, resolvedCaps.ClientFamilyFallback) {
		return
	}
	if shouldPreferNativeDirectStream(dec) {
		if recordingClientSupportsDirectTransportStream(req.RequestedProfile, resolvedCaps.ClientFamilyFallback) {
			return
		}
		rewriteDecisionToDirectStream(dec)
	}
	if dec.SelectedOutputKind != "hls" || !dec.TargetProfile.HLS.Enabled {
		return
	}

	target := *dec.TargetProfile
	target.Container = "mp4"
	target.Packaging = playbackprofile.PackagingFMP4
	target.HLS.Enabled = true
	target.HLS.SegmentContainer = "fmp4"
	canonical := playbackprofile.CanonicalizeTarget(target)
	dec.TargetProfile = &canonical
	if dec.Mode == decision.ModeDirectStream {
		dec.Trace.QualityRung = string(playbackprofile.RungCompatibleHLSFMP4)
	}
}

func shouldPreferNativeDirectStream(dec *decision.Decision) bool {
	if dec == nil || dec.Mode != decision.ModeDirectPlay || dec.TargetProfile == nil {
		return false
	}

	target := playbackprofile.CanonicalizeTarget(*dec.TargetProfile)
	if target.Video.Mode != playbackprofile.MediaModeCopy || target.Audio.Mode != playbackprofile.MediaModeCopy {
		return false
	}

	switch normalize.Token(target.Container) {
	case "ts", "mpegts":
		return true
	}
	return target.Packaging == playbackprofile.PackagingTS
}

func rewriteDecisionToDirectStream(dec *decision.Decision) {
	if dec == nil || dec.TargetProfile == nil {
		return
	}

	target := playbackprofile.CanonicalizeTarget(*dec.TargetProfile)
	target.HLS.Enabled = true
	target.HLS.SegmentContainer = "mpegts"
	canonical := playbackprofile.CanonicalizeTarget(target)

	dec.Mode = decision.ModeDirectStream
	dec.Outputs = []decision.Output{
		{
			Kind: "hls",
			URL:  "placeholder://direct-stream.m3u8",
		},
	}
	dec.TargetProfile = &canonical
	dec.Reasons = []decision.ReasonCode{decision.ReasonDirectStreamMatch}
	dec.SelectedOutputKind = "hls"
	dec.SelectedOutputURL = "placeholder://direct-stream.m3u8"
	dec.Trace.ResolvedIntent = playbackprofile.PublicIntentName(playbackprofile.IntentCompatible)
	dec.Trace.QualityRung = string(playbackprofile.RungCompatibleHLSTS)
	dec.Trace.AudioQualityRung = ""
	dec.Trace.VideoQualityRung = ""
	if dec.Trace.RequestedIntent == string(playbackprofile.IntentDirect) {
		dec.Trace.DegradedFrom = string(playbackprofile.IntentDirect)
	} else {
		dec.Trace.DegradedFrom = ""
	}
	dec.Trace.Why = []decision.Reason{
		{
			Code: decision.ReasonDirectStreamMatch,
		},
	}
}

func clientWantsFMP4Packaging(requestedProfile string, clientFamily string) bool {
	switch strings.ToLower(strings.TrimSpace(requestedProfile)) {
	case "android_native", "android_tv_native", "safari", "safari_dvr", "safari_dirty", "safari_hevc", "safari_hevc_hw", "safari_hevc_hw_ll", "h264_fmp4":
		return true
	}

	switch strings.ToLower(strings.TrimSpace(clientFamily)) {
	case "android_native", "android_tv_native", "safari_native", "ios_safari_native":
		return true
	default:
		return false
	}
}

func playbackInfoCapsHasToken(values []string, want string) bool {
	want = normalize.Token(want)
	if want == "" {
		return false
	}
	for _, value := range values {
		if normalize.Token(value) == want {
			return true
		}
	}
	return false
}

func decisionTargetsVideoCodec(dec *decision.Decision, want string) bool {
	want = normalize.Token(want)
	if want == "" || dec == nil {
		return false
	}
	if normalize.Token(dec.Selected.VideoCodec) == want {
		return true
	}
	if dec.TargetProfile == nil {
		return false
	}
	return normalize.Token(dec.TargetProfile.Video.Codec) == want
}

func recordingClientSupportsDirectTransportStream(requestedProfile string, clientFamily string) bool {
	switch strings.ToLower(strings.TrimSpace(requestedProfile)) {
	case "android_native", "android_tv_native":
		return true
	}

	switch strings.ToLower(strings.TrimSpace(clientFamily)) {
	case "android_native", "android_tv_native":
		return true
	default:
		return false
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
	videoBenchmarkClass := benchmarkClassForPlaybackPath(hostRuntime.Benchmark, truth, mappedCaps)

	return decision.DecisionInput{
		RequestID:       req.RequestID,
		RequestedIntent: playbackprofile.NormalizeRequestedIntent(req.RequestedProfile),
		APIVersion:      req.APIVersion,
		Source: decision.Source{
			Container:         truth.Container,
			VideoCodec:        truth.VideoCodec,
			AudioCodec:        truth.AudioCodec,
			BitrateKbps:       truth.BitrateKbps,
			BitrateConfidence: truth.BitrateConfidence,
			Width:             truth.Width,
			Height:            truth.Height,
			FPS:               truth.FPS,
			Interlaced:        truth.Interlaced,
		},
		Capabilities: mappedCaps,
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

func benchmarkClassForPlaybackPath(snapshot playbackprofile.HostBenchmarkSnapshot, truth playback.MediaTruth, caps decision.Capabilities) string {
	if profileID := benchmarkProfileForPlaybackPath(truth, caps); profileID != "" {
		if class := playbackprofile.BenchmarkClassForProfile(snapshot, profileID); class != "" {
			return class
		}
	}
	return playbackprofile.BenchmarkClassForCodec(snapshot, "h264")
}

func benchmarkProfileForPlaybackPath(truth playback.MediaTruth, caps decision.Capabilities) string {
	if benchmarkProfileIsAudioOnly(truth, caps) {
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

func benchmarkProfileIsAudioOnly(truth playback.MediaTruth, caps decision.Capabilities) bool {
	if benchmarkSourceRequiresVideoRepair(truth) {
		return false
	}
	canVideo := benchmarkContains(caps.VideoCodecs, truth.VideoCodec) && benchmarkWithinMaxVideo(truth, caps.MaxVideo)
	canAudio := benchmarkContains(caps.AudioCodecs, truth.AudioCodec)
	return canVideo && !canAudio
}

func benchmarkSourceRequiresVideoRepair(truth playback.MediaTruth) bool {
	return truth.Interlaced || truth.Width <= 0 || truth.Height <= 0 || truth.FPS <= 0
}

func benchmarkContains(values []string, value string) bool {
	want := normalize.Token(value)
	if want == "" {
		return false
	}
	for _, candidate := range values {
		if normalize.Token(candidate) == want {
			return true
		}
	}
	return false
}

func benchmarkWithinMaxVideo(truth playback.MediaTruth, maxVideo *decision.MaxVideoDimensions) bool {
	if maxVideo == nil {
		return true
	}
	if maxVideo.Width > 0 && truth.Width > 0 && truth.Width > maxVideo.Width {
		return false
	}
	if maxVideo.Height > 0 && truth.Height > 0 && truth.Height > maxVideo.Height {
		return false
	}
	if maxVideo.FPS > 0 && truth.FPS > 0 && truth.FPS > float64(maxVideo.FPS) {
		return false
	}
	return true
}
