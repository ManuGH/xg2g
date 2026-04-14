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

	sourceRef, truth, err := s.resolveSubjectTruth(ctx, req, svc)
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

	input := buildDecisionInput(
		req,
		truth,
		resolvedCaps,
		cfg,
		operatorPolicy,
		s.deps.HostPressure(ctx),
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
	hostFingerprint, deviceFingerprint, sourceFingerprint := s.rememberCapabilitySnapshots(ctx, hostContext, sourceRef, req, truth, resolvedCaps)
	s.recordDecisionAudit(ctx, hostContext, sourceRef, req, resolvedCaps, input, dec)
	s.recordCapabilityObservation(ctx, sourceRef, req, truth, resolvedCaps, dec, hostFingerprint, deviceFingerprint, sourceFingerprint)

	return PlaybackInfoResult{
		SourceRef:            sourceRef,
		Truth:                truth,
		ResolvedCapabilities: resolvedCaps,
		Decision:             dec,
		ClientProfile:        req.ClientProfile,
		OperatorRuleName:     operatorRuleName,
		OperatorRuleScope:    operatorRuleScope,
	}, nil
}

func (s *Service) resolveSubjectTruth(ctx context.Context, req PlaybackInfoRequest, svc RecordingsService) (string, playback.MediaTruth, *PlaybackInfoError) {
	subjectID := strings.TrimSpace(req.SubjectID)
	if subjectID == "" {
		return "", playback.MediaTruth{}, &PlaybackInfoError{
			Kind:    PlaybackInfoErrorInvalidInput,
			Message: "subject id is required",
		}
	}

	switch req.SubjectKind {
	case PlaybackSubjectLive:
		if err := domainrecordings.ValidateLiveRef(subjectID); err != nil {
			return "", playback.MediaTruth{}, &PlaybackInfoError{
				Kind:    PlaybackInfoErrorInvalidInput,
				Message: "serviceRef must be a valid live Enigma2 reference",
				Cause:   err,
			}
		}
		source := s.deps.ChannelTruthSource()
		truthResolution := resolveLiveTruthState(subjectID, source)
		if !truthResolution.Verified() && truthResolution.Reason == "missing_scan_truth" {
			if probeSource, ok := source.(channelTruthProbeSource); ok {
				probedCap, found, probeErr := probeSource.ProbeCapability(ctx, subjectID)
				if probeErr != nil {
					log.L().Warn().
						Err(probeErr).
						Str("serviceRef", subjectID).
						Msg("live playback truth probe failed")
				}
				truthResolution = resolveLiveTruthCapability(subjectID, probedCap, found)
			}
		}
		if !truthResolution.Verified() {
			return "", playback.MediaTruth{}, playbackInfoErrorForLiveTruth(truthResolution)
		}
		return subjectID, truthResolution.Truth, nil
	case PlaybackSubjectRecording:
		sourceRef, ok := domainrecordings.DecodeRecordingID(subjectID)
		if !ok {
			return "", playback.MediaTruth{}, &PlaybackInfoError{
				Kind:    PlaybackInfoErrorInvalidInput,
				Message: "Invalid recording ID format",
			}
		}

		truth, err := svc.GetMediaTruth(ctx, subjectID)
		if err != nil {
			return "", playback.MediaTruth{}, classifyPlaybackInfoError(err)
		}

		switch truth.Status {
		case playback.MediaStatusPreparing:
			return "", playback.MediaTruth{}, &PlaybackInfoError{
				Kind:              PlaybackInfoErrorPreparing,
				Message:           "Retry shortly.",
				RetryAfterSeconds: truth.RetryAfter,
				ProbeState:        string(truth.ProbeState),
			}
		case playback.MediaStatusUpstreamUnavailable:
			return "", playback.MediaTruth{}, &PlaybackInfoError{
				Kind:    PlaybackInfoErrorUpstreamUnavailable,
				Message: "Retry later.",
			}
		case playback.MediaStatusNotFound:
			return "", playback.MediaTruth{}, &PlaybackInfoError{
				Kind:    PlaybackInfoErrorNotFound,
				Message: "recording not found",
			}
		}
		return sourceRef, truth, nil
	default:
		return "", playback.MediaTruth{}, &PlaybackInfoError{
			Kind:    PlaybackInfoErrorInvalidInput,
			Message: "unsupported playback subject kind",
		}
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

func alignAutoCodecDecision(req PlaybackInfoRequest, resolvedCaps capabilities.PlaybackCapabilities, dec *decision.Decision) {
	if req.Capabilities == nil || dec == nil || dec.Mode != decision.ModeTranscode || !shouldApplyAutoCodecDecision(req.RequestedProfile) {
		return
	}

	profileID := autocodec.PickProfileForCapabilities(resolvedCaps, profiles.HWAccelAuto)
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

func alignRecordingNativePackaging(req PlaybackInfoRequest, resolvedCaps capabilities.PlaybackCapabilities, dec *decision.Decision) {
	if req.SubjectKind != PlaybackSubjectRecording || dec == nil || dec.TargetProfile == nil {
		return
	}
	if shouldPreferNativeDirectStream(dec) {
		if recordingClientSupportsDirectTransportStream(req.RequestedProfile, resolvedCaps.ClientFamilyFallback) {
			return
		}
		rewriteDecisionToDirectStream(dec)
	}
	if shouldPreserveNativeSafariRecordingTransport(resolvedCaps) {
		return
	}
	if !clientWantsFMP4Packaging(req.RequestedProfile, resolvedCaps.ClientFamilyFallback) {
		return
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

func shouldPreserveNativeSafariRecordingTransport(resolvedCaps capabilities.PlaybackCapabilities) bool {
	if !strings.EqualFold(strings.TrimSpace(resolvedCaps.PreferredHLSEngine), "native") {
		return false
	}
	switch recordingPlatformClass(resolvedCaps) {
	case "ios_webkit", "ipados_webkit", "tvos_native_host":
		return true
	case "macos_safari":
		return false
	}
	if strings.ToLower(strings.TrimSpace(resolvedCaps.ClientFamilyFallback)) != "ios_safari_native" {
		return false
	}
	if resolvedCaps.DeviceContext == nil {
		return true
	}

	switch strings.ToLower(strings.TrimSpace(resolvedCaps.DeviceContext.OSName)) {
	case "", "ios", "ipados":
		return true
	default:
		return false
	}
}

func recordingPlatformClass(resolvedCaps capabilities.PlaybackCapabilities) string {
	if resolvedCaps.DeviceContext == nil {
		return ""
	}
	return normalize.Token(resolvedCaps.DeviceContext.PlatformClass)
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
) decision.DecisionInput {
	serverCanTranscode := strings.TrimSpace(cfg.FFmpeg.Bin) != "" && strings.TrimSpace(cfg.HLS.Root) != ""
	clientAllowsTranscode := resolvedCaps.AllowTranscode == nil || *resolvedCaps.AllowTranscode
	allowTranscode := serverCanTranscode && clientAllowsTranscode

	return decision.DecisionInput{
		RequestID:       req.RequestID,
		RequestedIntent: playbackprofile.NormalizeRequestedIntent(req.RequestedProfile),
		APIVersion:      req.APIVersion,
		Source: decision.Source{
			Container:  truth.Container,
			VideoCodec: truth.VideoCodec,
			AudioCodec: truth.AudioCodec,
			Width:      truth.Width,
			Height:     truth.Height,
			FPS:        truth.FPS,
			Interlaced: truth.Interlaced,
		},
		Capabilities: decision.FromCapabilities(resolvedCaps),
		Policy: decision.Policy{
			AllowTranscode: allowTranscode,
			Operator: decision.OperatorPolicy{
				ForceIntent:    playbackprofile.NormalizeRequestedIntent(operatorPolicy.ForceIntent),
				MaxQualityRung: playbackprofile.NormalizeQualityRung(operatorPolicy.MaxQualityRung),
			},
			Host: decision.HostPolicy{
				PressureBand: playbackprofile.NormalizeHostPressureBand(string(hostPressure.EffectiveBand)),
			},
		},
	}
}
