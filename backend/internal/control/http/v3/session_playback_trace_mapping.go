package v3

import (
	v3sessions "github.com/ManuGH/xg2g/internal/control/http/v3/sessions"
	"github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	pipelineapi "github.com/ManuGH/xg2g/internal/pipeline/api"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"strings"
	"time"
)

func mapPlaybackTraceHLSDebug(trace *model.PlaybackTrace) *PlaybackTraceHlsDebug {
	if trace == nil || trace.HLS == nil {
		return nil
	}
	hls := trace.HLS
	dto := &PlaybackTraceHlsDebug{}
	hasValue := false

	if hls.PlaylistRequestCount > 0 {
		dto.PlaylistRequestCount = optionalIntPtr(hls.PlaylistRequestCount)
		hasValue = true
	}
	if hls.LastPlaylistAtUnix > 0 {
		dto.LastPlaylistAtMs = optionalIntPtr(int(hls.LastPlaylistAtUnix * 1000))
		hasValue = true
	}
	if hls.LastPlaylistIntervalMs > 0 {
		dto.LastPlaylistIntervalMs = optionalIntPtr(hls.LastPlaylistIntervalMs)
		hasValue = true
	}
	if hls.SegmentRequestCount > 0 {
		dto.SegmentRequestCount = optionalIntPtr(hls.SegmentRequestCount)
		hasValue = true
	}
	if hls.LastSegmentAtUnix > 0 {
		// LastSegmentAtUnix is stored as UnixMilli for sub-second precision.
		dto.LastSegmentAtMs = optionalIntPtr(int(hls.LastSegmentAtUnix))
		hasValue = true
	}
	if lastSegmentName := strings.TrimSpace(hls.LastSegmentName); lastSegmentName != "" {
		dto.LastSegmentName = &lastSegmentName
		hasValue = true
	}
	if hls.LastSegmentGapMs > 0 {
		dto.LastSegmentGapMs = optionalIntPtr(hls.LastSegmentGapMs)
		hasValue = true
	}
	if hls.LatestSegmentLagMs > 0 {
		dto.LatestSegmentLagMs = optionalIntPtr(hls.LatestSegmentLagMs)
		hasValue = true
	}
	if stallHint := strings.TrimSpace(hls.StallRisk); stallHint != "" {
		dto.StallHint = &stallHint
		hasValue = true
	}
	if startupMode := strings.TrimSpace(hls.StartupMode); startupMode != "" {
		dto.StartupMode = &startupMode
		hasValue = true
	}
	if hls.StartupHeadroomSec > 0 {
		dto.StartupHeadroomSec = optionalIntPtr(hls.StartupHeadroomSec)
		hasValue = true
	}
	if len(hls.StartupReasons) > 0 {
		reasons := append([]string(nil), hls.StartupReasons...)
		dto.StartupReasons = &reasons
		hasValue = true
	}
	if hasValue {
		assessment := pipelineapi.DeriveSessionPlaybackHealth(trace, pipelineapi.SessionPlaybackHealthContext{})
		if health := strings.TrimSpace(string(assessment.Health)); health != "" {
			dto.Health = &health
		}
		if len(assessment.ReasonCodes) > 0 {
			reasons := append([]string(nil), assessment.ReasonCodes...)
			dto.HealthReasons = &reasons
		}
	}

	if !hasValue {
		return nil
	}
	return dto
}

func mapSessionPlaybackTrace(requestID string, session *model.SessionRecord, hlsRoot string) *PlaybackTrace {
	if session == nil {
		return nil
	}
	dto := &PlaybackTrace{
		RequestId: requestID,
	}
	if sid := strings.TrimSpace(session.SessionID); sid != "" {
		dto.SessionId = &sid
	}

	trace := session.PlaybackTrace
	if trace == nil {
		trace = &model.PlaybackTrace{}
	}
	runtimeState := v3sessions.LoadSessionRuntimePolicyState(session)
	runtimeTimeline := v3sessions.LoadSessionRuntimeTimeline(session)
	runtimeReplay := v3sessions.LoadSessionRuntimeReplay(session)
	if runtimeReplay == nil {
		runtimeReplay = v3sessions.BuildSessionRuntimePolicyReplay(session)
	}

	requestProfile := strings.TrimSpace(trace.RequestProfile)
	if requestProfile == "" {
		requestProfile = profiles.PublicProfileName(session.Profile.Name)
	}
	if requestProfile != "" {
		dto.RequestProfile = &requestProfile
	}

	requestedIntent := strings.TrimSpace(trace.RequestedIntent)
	if requestedIntent == "" {
		requestedIntent = requestProfile
	}
	if requestedIntent != "" {
		dto.RequestedIntent = &requestedIntent
	}

	resolvedIntent := strings.TrimSpace(trace.ResolvedIntent)
	if resolvedIntent == "" {
		resolvedIntent = profiles.PublicProfileName(session.Profile.Name)
	}
	if resolvedIntent != "" {
		dto.ResolvedIntent = &resolvedIntent
	}

	policyModeHint := strings.TrimSpace(string(trace.PolicyModeHint))
	if policyModeHint != "" {
		dto.PolicyModeHint = &policyModeHint
	}
	effectiveRuntimeMode := strings.TrimSpace(string(trace.EffectiveRuntimeMode))
	if effectiveRuntimeMode != "" {
		dto.EffectiveRuntimeMode = &effectiveRuntimeMode
	}
	effectiveModeSource := strings.TrimSpace(string(trace.EffectiveModeSource))
	if effectiveModeSource != "" {
		dto.EffectiveModeSource = &effectiveModeSource
	}

	qualityRung := strings.TrimSpace(trace.QualityRung)
	if qualityRung != "" {
		dto.QualityRung = &qualityRung
	}
	audioQualityRung := strings.TrimSpace(trace.AudioQualityRung)
	if audioQualityRung != "" {
		dto.AudioQualityRung = &audioQualityRung
	}
	videoQualityRung := strings.TrimSpace(trace.VideoQualityRung)
	if videoQualityRung != "" {
		dto.VideoQualityRung = &videoQualityRung
	}

	degradedFrom := strings.TrimSpace(trace.DegradedFrom)
	if degradedFrom != "" {
		dto.DegradedFrom = &degradedFrom
	}
	hostPressureBand := strings.TrimSpace(trace.HostPressureBand)
	if hostPressureBand != "" {
		dto.HostPressureBand = &hostPressureBand
	}
	if autoCodecPolicy := strings.TrimSpace(trace.AutoCodecPolicy); autoCodecPolicy != "" {
		dto.AutoCodecPolicy = &autoCodecPolicy
	}
	if autoCodecRequested := strings.TrimSpace(trace.AutoCodecRequested); autoCodecRequested != "" {
		dto.AutoCodecRequestedCodecs = &autoCodecRequested
	}
	if autoCodecSelected := strings.TrimSpace(trace.AutoCodecSelected); autoCodecSelected != "" {
		dto.AutoCodecSelectedCodec = &autoCodecSelected
	}
	if autoCodecHostClass := strings.TrimSpace(trace.AutoCodecHostClass); autoCodecHostClass != "" {
		dto.AutoCodecPerformanceClass = &autoCodecHostClass
	}
	if autoCodecBenchClass := strings.TrimSpace(trace.AutoCodecBenchClass); autoCodecBenchClass != "" {
		dto.AutoCodecBenchmarkClass = &autoCodecBenchClass
	}
	if trace.HostOverrideApplied {
		hostOverrideApplied := true
		dto.HostOverrideApplied = &hostOverrideApplied
	}
	clientSnapshot := sessionPlaybackClientSnapshot(session)
	if clientSnapshot != nil {
		dto.Client = mapPlaybackClientSnapshot(clientSnapshot)
		if clientFamily := strings.TrimSpace(clientSnapshot.ClientFamily); clientFamily != "" {
			dto.ClientFamily = &clientFamily
		}
		if clientCapsSource := strings.TrimSpace(clientSnapshot.ClientCapsSource); clientCapsSource != "" {
			dto.ClientCapsSource = &clientCapsSource
		}
	}

	if trace.Source != nil {
		dto.Source = mapSourceProfile(trace.Source)
	}

	clientPath := strings.TrimSpace(trace.ClientPath)
	if clientPath == "" && session.ContextData != nil {
		clientPath = strings.TrimSpace(session.ContextData[model.CtxKeyClientPath])
	}
	if clientPath != "" {
		dto.ClientPath = &clientPath
	}

	inputKind := strings.TrimSpace(trace.InputKind)
	if inputKind == "" && session.ContextData != nil {
		inputKind = strings.TrimSpace(session.ContextData[model.CtxKeySourceType])
	}
	if inputKind != "" {
		dto.InputKind = &inputKind
	}
	preflightReason := strings.TrimSpace(trace.PreflightReason)
	if preflightReason != "" {
		dto.PreflightReason = &preflightReason
	}
	preflightDetail := strings.TrimSpace(trace.PreflightDetail)
	if preflightDetail != "" {
		dto.PreflightDetail = &preflightDetail
	}

	// Statistics never lie: the displayed output profile must reflect what
	// ffmpeg actually runs (session.Profile, kept in sync through recovery and
	// runtime transitions), NOT the decision engine's earlier prediction that
	// may still sit on the trace. Derive the live target + hash from the
	// executed profile so the panel can never report a transcode it isn't doing.
	target := model.TraceTargetProfileFromProfile(session.Profile)
	if target == nil {
		target = trace.TargetProfile
	}
	if target != nil {
		canonical := playbackprofile.CanonicalizeTarget(*target)
		dto.TargetProfile = mapTargetProfile(&canonical)
		if hash := playbackprofile.HashTarget(canonical); hash != "" {
			dto.TargetProfileHash = &hash
		}
	}

	operator := PlaybackTraceOperator{}
	hasOperator := false
	if trace.Operator != nil {
		operator = PlaybackTraceOperator{
			ClientFallbackDisabled: boolPtr(trace.Operator.ClientFallbackDisabled),
			OverrideApplied:        boolPtr(trace.Operator.OverrideApplied),
		}
		hasOperator = trace.Operator.ClientFallbackDisabled || trace.Operator.OverrideApplied
		if forcedIntent := strings.TrimSpace(trace.Operator.ForcedIntent); forcedIntent != "" {
			operator.ForcedIntent = &forcedIntent
			hasOperator = true
		}
		if maxQualityRung := strings.TrimSpace(trace.Operator.MaxQualityRung); maxQualityRung != "" {
			operator.MaxQualityRung = &maxQualityRung
			hasOperator = true
		}
		if ruleName := strings.TrimSpace(trace.Operator.RuleName); ruleName != "" {
			operator.RuleName = &ruleName
			hasOperator = true
		}
		if ruleScope := strings.TrimSpace(trace.Operator.RuleScope); ruleScope != "" {
			operator.RuleScope = &ruleScope
			hasOperator = true
		}
	}
	if runtimeAction := strings.TrimSpace(string(runtimeState.LastAction)); runtimeAction != "" {
		operator.RuntimePolicyAction = &runtimeAction
		hasOperator = true
	}
	if runtimePhase := strings.TrimSpace(sessionRuntimePolicyPhaseName(runtimeState, time.Now().UTC())); runtimePhase != "" {
		operator.RuntimePolicyPhase = &runtimePhase
		hasOperator = true
	}
	if len(runtimeState.PolicyConstraints) > 0 {
		constraints := append([]string(nil), runtimeState.PolicyConstraints...)
		operator.RuntimePolicyConstraints = &constraints
		hasOperator = true
	}
	if len(runtimeState.Reasons) > 0 {
		reasons := append([]string(nil), runtimeState.Reasons...)
		operator.RuntimePolicyReasons = &reasons
		hasOperator = true
	}
	if mappedReplay := mapRuntimePolicyReplay(runtimeReplay); mappedReplay != nil {
		operator.RuntimePolicyReplay = mappedReplay
		hasOperator = true
	}
	if runtimeCandidate := strings.TrimSpace(string(runtimeState.ProbeStep)); runtimeCandidate != "" {
		operator.RuntimeProbeCandidate = &runtimeCandidate
		hasOperator = true
	}
	if mappedTimeline := mapRuntimePolicyTimeline(runtimeTimeline); mappedTimeline != nil {
		operator.RuntimePolicyTimeline = mappedTimeline
		hasOperator = true
	}
	if hasOperator {
		dto.Operator = &operator
	}

	if trace.FFmpegPlan != nil {
		dto.FfmpegPlan = &PlaybackTraceFfmpegPlan{
			InputKind:  strPtr(trace.FFmpegPlan.InputKind),
			Container:  strPtr(trace.FFmpegPlan.Container),
			Packaging:  strPtr(trace.FFmpegPlan.Packaging),
			HwAccel:    strPtr(trace.FFmpegPlan.HWAccel),
			VideoMode:  strPtr(trace.FFmpegPlan.VideoMode),
			VideoCodec: strPtr(trace.FFmpegPlan.VideoCodec),
			AudioMode:  strPtr(trace.FFmpegPlan.AudioMode),
			AudioCodec: strPtr(trace.FFmpegPlan.AudioCodec),
		}
	}
	if trace.RuntimeDiagnostics != nil && !trace.RuntimeDiagnostics.IsZero() {
		dto.RuntimeDiagnostics = mapPlaybackRuntimeDiagnostics(*trace.RuntimeDiagnostics)
	}

	if hlsDebug := mapPlaybackTraceHLSDebug(trace); hlsDebug != nil {
		dto.HlsDebug = hlsDebug
	}

	firstFrameAtUnix := trace.FirstFrameAtUnix
	if firstFrameAtUnix == 0 {
		firstFrameAtUnix = v3sessions.SessionFirstFrameUnix(hlsRoot, session.SessionID)
	}
	if firstFrameAtUnix > 0 {
		firstFrameAtMs := int(firstFrameAtUnix * 1000)
		dto.FirstFrameAtMs = &firstFrameAtMs
	}

	fallbackCount := len(trace.Fallbacks)
	lastFallbackReason := ""
	if fallbackCount > 0 {
		lastFallbackReason = strings.TrimSpace(trace.Fallbacks[fallbackCount-1].Reason)
	} else if strings.TrimSpace(session.FallbackReason) != "" {
		fallbackCount = 1
		lastFallbackReason = strings.TrimSpace(session.FallbackReason)
	}
	if fallbackCount > 0 {
		dto.FallbackCount = &fallbackCount
	}
	if lastFallbackReason != "" {
		dto.LastFallbackReason = &lastFallbackReason
	}

	stopReason := strings.TrimSpace(trace.StopReason)
	if stopReason == "" && session.Reason != "" && session.State.IsTerminal() {
		stopReason = string(session.Reason)
	}
	if stopReason != "" {
		dto.StopReason = &stopReason
	}

	stopClass := strings.TrimSpace(string(trace.StopClass))
	if stopClass == "" && session.State.IsTerminal() && session.Reason != "" {
		stopClass = strings.TrimSpace(string(model.TraceStopClassFromReason(session.Reason)))
	}
	if stopClass != "" {
		dto.StopClass = &stopClass
	}

	return dto
}

func mapPlaybackRuntimeDiagnostics(d ports.RuntimeDiagnostics) *PlaybackTraceRuntimeDiagnostics {
	out := &PlaybackTraceRuntimeDiagnostics{}
	if d.FrameCount > 0 {
		out.FrameCount = &d.FrameCount
	}
	if d.FPS > 0 {
		value := float32(d.FPS)
		out.Fps = &value
	}
	out.DropFrames = &d.DropFrames
	out.DupFrames = &d.DupFrames
	if d.Speed > 0 {
		value := float32(d.Speed)
		out.Speed = &value
	}
	if d.CorruptDecodedFrames > 0 {
		out.CorruptDecodedFrames = &d.CorruptDecodedFrames
	}
	if warning := strings.TrimSpace(d.LastWarning); warning != "" {
		out.LastWarning = &warning
	}
	if d.UpdatedAtUnix > 0 {
		updatedAtUnix := int(d.UpdatedAtUnix)
		out.UpdatedAtUnix = &updatedAtUnix
	}
	return out
}

func sessionRuntimePolicyPhaseName(state runtimepolicy.SessionLoopState, now time.Time) string {
	if state.ProbeState == runtimepolicy.ProbeLifecycleScheduled || state.ProbeState == runtimepolicy.ProbeLifecycleObserving {
		return "probing"
	}
	switch strings.TrimSpace(string(state.LastAction)) {
	case "probe_up":
		return "probing"
	case "cooldown":
		return "cooldown"
	case "degrade", "step_down":
		return "degraded"
	case "lock_current":
		return "recovering"
	}
	if hasString(state.Reasons, runtimepolicy.ReasonProbeRecentlyRegressed, runtimepolicy.ReasonProbeWindowRegressed) || state.ProbeState == runtimepolicy.ProbeLifecycleAborted {
		return "probe_regressed"
	}
	if !state.CooldownUntil.IsZero() && state.CooldownUntil.After(now) {
		return "cooldown"
	}
	switch state.ConfidenceState {
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

func mapRuntimePolicyTimeline(timeline []runtimepolicy.TickTrace) *[]PlaybackTraceRuntimeTick {
	if len(timeline) == 0 {
		return nil
	}
	out := make([]PlaybackTraceRuntimeTick, 0, len(timeline))
	for _, tick := range timeline {
		dto := PlaybackTraceRuntimeTick{
			TickAt:             tick.TickAt,
			ConfidenceScore:    toIntPtr(tick.ConfidenceScore),
			ConfidenceState:    strPtr(string(tick.ConfidenceState)),
			PolicyAction:       strPtr(string(tick.PolicyAction)),
			PlannedTransition:  strPtr(string(tick.PlannedTransition)),
			ExecutedTransition: strPtr(string(tick.ExecutedTransition)),
			ActiveStep:         strPtr(string(tick.ActiveStep)),
			TargetStep:         strPtr(string(tick.TargetStep)),
			ProbeStep:          strPtr(string(tick.ProbeStep)),
			ProbeState:         strPtr(string(tick.ProbeState)),
		}
		if !tick.CooldownUntil.IsZero() {
			cooldown := tick.CooldownUntil
			dto.CooldownUntil = &cooldown
		}
		if len(tick.Blockers) > 0 {
			blockers := append([]string(nil), tick.Blockers...)
			dto.Blockers = &blockers
		}
		if len(tick.Reasons) > 0 {
			reasons := append([]string(nil), tick.Reasons...)
			dto.Reasons = &reasons
		}
		out = append(out, dto)
	}
	return &out
}

func mapRuntimePolicyReplay(replay *runtimepolicy.RuntimePolicyReplay) *PlaybackTraceRuntimeReplay {
	if replay == nil {
		return nil
	}
	out := &PlaybackTraceRuntimeReplay{
		Metadata:     mapRuntimePolicyReplayMetadata(replay.Metadata),
		InitialState: mapRuntimePolicyReplayState(replay.InitialState),
		FinalState:   mapRuntimePolicyReplayState(replay.FinalState),
	}
	if len(replay.Ticks) > 0 {
		ticks := make([]PlaybackTraceRuntimeReplayTick, 0, len(replay.Ticks))
		for _, tick := range replay.Ticks {
			ticks = append(ticks, PlaybackTraceRuntimeReplayTick{
				Input:    mapRuntimePolicyReplayTickInput(tick.Input),
				Expected: mapRuntimePolicyReplayTickExpected(tick.Expected),
			})
		}
		out.Ticks = &ticks
	}
	return out
}

func mapRuntimePolicyReplayMetadata(metadata runtimepolicy.ReplayMetadata) *PlaybackTraceRuntimeReplayMetadata {
	if metadata.SessionID == "" && metadata.ServiceRef == "" && metadata.ClientPath == "" && metadata.SourceType == "" && metadata.InitialTarget == runtimepolicy.PlaybackStepUnknown {
		return nil
	}
	return &PlaybackTraceRuntimeReplayMetadata{
		SessionId:     strPtr(metadata.SessionID),
		ServiceRef:    strPtr(metadata.ServiceRef),
		ClientPath:    strPtr(metadata.ClientPath),
		SourceType:    strPtr(metadata.SourceType),
		InitialTarget: strPtr(string(metadata.InitialTarget)),
	}
}

func mapRuntimePolicyReplayState(state runtimepolicy.SessionLoopState) *PlaybackTraceRuntimeReplayState {
	if state.CurrentStep == runtimepolicy.PlaybackStepUnknown &&
		state.TargetStep == runtimepolicy.PlaybackStepUnknown &&
		state.ProbeStep == runtimepolicy.PlaybackStepUnknown &&
		state.ProbeState == runtimepolicy.ProbeLifecycleNone &&
		state.ConfidenceScore == 0 &&
		state.ConfidenceState == "" &&
		state.CooldownUntil.IsZero() &&
		state.LastAction == "" &&
		len(state.PolicyConstraints) == 0 &&
		len(state.Reasons) == 0 {
		return nil
	}
	out := &PlaybackTraceRuntimeReplayState{
		ConfidenceScore: toIntPtr(state.ConfidenceScore),
		ConfidenceState: strPtr(string(state.ConfidenceState)),
		CurrentStep:     strPtr(string(state.CurrentStep)),
		LastAction:      strPtr(string(state.LastAction)),
		TargetStep:      strPtr(string(state.TargetStep)),
		ProbeStep:       strPtr(string(state.ProbeStep)),
		ProbeState:      strPtr(string(state.ProbeState)),
	}
	if !state.CooldownUntil.IsZero() {
		cooldown := state.CooldownUntil
		out.CooldownUntil = &cooldown
	}
	if len(state.PolicyConstraints) > 0 {
		constraints := append([]string(nil), state.PolicyConstraints...)
		out.PolicyConstraints = &constraints
	}
	if len(state.Reasons) > 0 {
		reasons := append([]string(nil), state.Reasons...)
		out.Reasons = &reasons
	}
	return out
}

func mapRuntimePolicyReplayTickInput(input runtimepolicy.ReplayTickInput) *PlaybackTraceRuntimeReplayTickInput {
	out := &PlaybackTraceRuntimeReplayTickInput{
		ObservedStep: strPtr(string(input.ObservedStep)),
		TargetStep:   strPtr(string(input.TargetStep)),
		TickAt:       input.TickAt,
	}
	out.Confidence = mapRuntimePolicyReplayTickInputConfidence(input.Confidence)
	return out
}

func mapRuntimePolicyReplayTickInputConfidence(snapshot runtimepolicy.ConfidenceSnapshot) *PlaybackTraceRuntimeReplayTickInputConfidence {
	out := &PlaybackTraceRuntimeReplayTickInputConfidence{
		Score:       toIntPtr(snapshot.Score),
		State:       strPtr(string(snapshot.State)),
		WindowCount: toIntPtr(snapshot.WindowCount),
	}
	if !snapshot.StateSince.IsZero() {
		stateSince := snapshot.StateSince
		out.StateSince = &stateSince
	}
	if !snapshot.CooldownUntil.IsZero() {
		cooldown := snapshot.CooldownUntil
		out.CooldownUntil = &cooldown
	}
	if len(snapshot.PolicyConstraints) > 0 {
		constraints := append([]string(nil), snapshot.PolicyConstraints...)
		out.PolicyConstraints = &constraints
	}
	if len(snapshot.Reasons) > 0 {
		reasons := append([]string(nil), snapshot.Reasons...)
		out.Reasons = &reasons
	}
	return out
}

func mapRuntimePolicyReplayTickExpected(expected runtimepolicy.ReplayTickExpectation) *PlaybackTraceRuntimeReplayTickExpected {
	out := &PlaybackTraceRuntimeReplayTickExpected{
		Action:             strPtr(string(expected.Action)),
		ActiveStep:         strPtr(string(expected.ActiveStep)),
		PlannedTransition:  strPtr(string(expected.PlannedTransition)),
		ExecutedTransition: strPtr(string(expected.ExecutedTransition)),
		ProbeStep:          strPtr(string(expected.ProbeStep)),
		ProbeState:         strPtr(string(expected.ProbeState)),
		RuntimePhase:       strPtr(expected.RuntimePhase),
	}
	if len(expected.Blockers) > 0 {
		blockers := append([]string(nil), expected.Blockers...)
		out.Blockers = &blockers
	}
	if len(expected.Reasons) > 0 {
		reasons := append([]string(nil), expected.Reasons...)
		out.Reasons = &reasons
	}
	return out
}

func toIntPtr(i int) *int { return &i }
