package recordings

import (
	"context"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/control/recordings/capreg"
	"github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/normalize"
)

const (
	playbackFeedbackFreshnessWindow  = 6 * time.Hour
	playbackFeedbackSampleLimit      = 8
	playbackConfidenceWindow         = 10 * time.Second
	playbackConfidenceLookback       = 60 * time.Second
	playbackConfidenceSampleLimit    = 24
	playbackConfidenceStateFreshness = 60 * time.Second
)

type playbackFeedbackPolicy struct {
	MaxQualityRung playbackprofile.QualityRung
	Summary        capreg.FeedbackSummary
	Confidence     runtimepolicy.ConfidenceSnapshot
	Policy         runtimepolicy.PolicyDecision
}

type playbackConfidenceBucket struct {
	start   time.Time
	feature runtimepolicy.WindowFeatures
}

func resetPlaybackConfidenceFeature(base runtimepolicy.WindowFeatures) runtimepolicy.WindowFeatures {
	feature := base
	feature.CleanPlayingMS = 0
	feature.HardDecodeFails = 0
	feature.HardStallFails = 0
	feature.BufferWarnings = 0
	feature.NetworkWarnings = 0
	feature.DecodeWarnings = 0
	feature.RecoveryBuffer = 0
	feature.RecoveryNetwork = 0
	feature.RecoveryDecode = 0
	feature.ProbeWindowStarted = 0
	feature.ProbeWindowConfirmed = 0
	feature.ProbeWindowRegressed = 0
	return feature
}

func (s *Service) applyPlaybackFeedbackPolicy(ctx context.Context, sourceRef string, req PlaybackInfoRequest, truth playback.MediaTruth, resolved capabilities.PlaybackCapabilities, hostContext requestHostContext, hostPressure playbackprofile.HostPressureAssessment, operator config.PlaybackOperatorConfig) (config.PlaybackOperatorConfig, *playbackFeedbackPolicy) {
	if playbackprofile.NormalizeRequestedIntent(operator.ForceIntent) != playbackprofile.IntentUnknown {
		return operator, nil
	}

	feedbackPolicy, ok := s.lookupPlaybackFeedbackPolicy(ctx, sourceRef, req, truth, resolved, hostContext, hostPressure)
	if !ok {
		return operator, nil
	}

	s.rememberPlaybackPolicyState(ctx, sourceRef, req, truth, resolved, hostContext, feedbackPolicy)

	if feedbackPolicy.MaxQualityRung == playbackprofile.RungUnknown {
		return operator, &feedbackPolicy
	}

	current := playbackprofile.NormalizeQualityRung(operator.MaxQualityRung)
	merged := stricterMaxQualityRung(current, feedbackPolicy.MaxQualityRung)
	if merged == playbackprofile.RungUnknown || merged == current {
		return operator, &feedbackPolicy
	}

	operator.MaxQualityRung = string(merged)
	log.L().Info().
		Str("requestId", req.RequestID).
		Str("sourceRef", sourceRef).
		Str("subjectKind", string(req.SubjectKind)).
		Str("feedbackClamp", string(merged)).
		Int("confidenceScore", feedbackPolicy.Confidence.Score).
		Str("confidenceState", string(feedbackPolicy.Confidence.State)).
		Str("policyAction", string(feedbackPolicy.Policy.Action)).
		Strs("policyConstraints", feedbackPolicy.Policy.PolicyConstraints).
		Strs("confidenceReasons", feedbackPolicy.Confidence.Reasons).
		Str("bitrateConfidence", normalizeBitrateConfidence(truth.BitrateConfidence)).
		Int("recentSamples", feedbackPolicy.Summary.SampleCount).
		Int("consecutiveWarnings", feedbackPolicy.Summary.ConsecutiveWarnings).
		Int("consecutiveBufferWarnings", feedbackPolicy.Summary.ConsecutiveBufferWarnings).
		Int("consecutiveDecodeWarnings", feedbackPolicy.Summary.ConsecutiveDecodeWarnings).
		Int("consecutiveNetworkWarnings", feedbackPolicy.Summary.ConsecutiveNetworkWarnings).
		Int("consecutiveFailures", feedbackPolicy.Summary.ConsecutiveFailures).
		Int("consecutiveDecodeFailures", feedbackPolicy.Summary.ConsecutiveDecodeFailures).
		Int("consecutiveStallFailures", feedbackPolicy.Summary.ConsecutiveStallFailures).
		Int("priorStartedStreak", feedbackPolicy.Summary.PriorStartedStreak).
		Int("priorRecoveredStartStreak", feedbackPolicy.Summary.PriorRecoveredStartStreak).
		Int("priorRecoveryStartCode", feedbackPolicy.Summary.PriorRecoveryStartCode).
		Msg("playback feedback clamp applied")
	return operator, &feedbackPolicy
}

func (s *Service) lookupPlaybackFeedbackPolicy(ctx context.Context, sourceRef string, req PlaybackInfoRequest, truth playback.MediaTruth, resolved capabilities.PlaybackCapabilities, hostContext requestHostContext, hostPressure playbackprofile.HostPressureAssessment) (playbackFeedbackPolicy, bool) {
	registry := s.deps.CapabilityRegistry()
	if registry == nil {
		return playbackFeedbackPolicy{}, false
	}
	hostFingerprint := strings.TrimSpace(hostContext.Snapshot.Identity.Fingerprint())
	deviceFingerprint := deviceIdentityForRequest(req, resolved).Fingerprint()
	sourceFingerprint := s.sourceSnapshotForRequest(ctx, sourceRef, req, truth).Fingerprint()
	if hostFingerprint == "" || deviceFingerprint == "" || sourceFingerprint == "" {
		return playbackFeedbackPolicy{}, false
	}

	now := time.Now().UTC()
	summaryQuery := capreg.FeedbackSummaryQuery{
		SubjectKind:       string(req.SubjectKind),
		SourceFingerprint: sourceFingerprint,
		DeviceFingerprint: deviceFingerprint,
		HostFingerprint:   hostFingerprint,
		Since:             now.Add(-playbackFeedbackFreshnessWindow),
		Limit:             playbackFeedbackSampleLimit,
	}
	var (
		summary      capreg.FeedbackSummary
		summaryFound bool
	)
	if lookup, ok := registry.(capreg.FeedbackSummaryLookup); ok {
		loadedSummary, found, err := lookup.LookupRecentFeedbackSummary(ctx, summaryQuery)
		if err != nil {
			log.L().Warn().Err(err).Str("requestId", req.RequestID).Str("sourceRef", sourceRef).Msg("playback feedback summary lookup failed")
			return playbackFeedbackPolicy{}, false
		}
		summary = loadedSummary
		summaryFound = found
	}

	initialConfidence := runtimepolicy.ConfidenceSnapshot{}
	currentMaxQualityRung := playbackprofile.RungUnknown
	observationsSince := now.Add(-playbackConfidenceLookback)
	stateFound := false

	if stateLookup, ok := registry.(capreg.PlaybackPolicyStateLookup); ok {
		state, found, err := stateLookup.LookupPlaybackPolicyState(ctx, capreg.PlaybackPolicyStateQuery{
			SubjectKind:       string(req.SubjectKind),
			SourceFingerprint: sourceFingerprint,
			DeviceFingerprint: deviceFingerprint,
			HostFingerprint:   hostFingerprint,
		})
		if err != nil {
			log.L().Warn().Err(err).Str("requestId", req.RequestID).Str("sourceRef", sourceRef).Msg("playback policy state lookup failed")
		} else if found && state.UpdatedAt.After(now.Add(-playbackConfidenceStateFreshness)) {
			initialConfidence = state.Confidence
			currentMaxQualityRung = state.MaxQualityRung
			stateFound = true
			nextSince := state.UpdatedAt.Add(time.Millisecond)
			if nextSince.After(observationsSince) {
				observationsSince = nextSince
			}
		}
	}
	if !summaryFound && !stateFound {
		return playbackFeedbackPolicy{}, false
	}

	confidence := initialConfidence
	evaluatedFromObservations := false
	if obsLookup, ok := registry.(capreg.FeedbackObservationLookup); ok {
		observations, err := obsLookup.LookupRecentFeedbackObservations(ctx, capreg.FeedbackSummaryQuery{
			SubjectKind:       summaryQuery.SubjectKind,
			SourceFingerprint: summaryQuery.SourceFingerprint,
			DeviceFingerprint: summaryQuery.DeviceFingerprint,
			HostFingerprint:   summaryQuery.HostFingerprint,
			Since:             observationsSince,
			Limit:             playbackConfidenceSampleLimit,
		})
		if err != nil {
			log.L().Warn().Err(err).Str("requestId", req.RequestID).Str("sourceRef", sourceRef).Msg("playback feedback observation lookup failed")
		} else if len(observations) > 0 {
			baseWindow := buildPlaybackConfidenceWindow(summary, truth, req.SubjectKind, hostPressure, hostContext.Snapshot.Runtime)
			windows := buildPlaybackConfidenceWindowsFromObservations(observations, baseWindow, now)
			confidence = evaluatePlaybackConfidenceTimeline(initialConfidence, windows, now)
			evaluatedFromObservations = true
		}
	}
	if !evaluatedFromObservations && initialConfidence.WindowCount == 0 {
		baseWindow := buildPlaybackConfidenceWindow(summary, truth, req.SubjectKind, hostPressure, hostContext.Snapshot.Runtime)
		confidence = runtimepolicy.EvaluateConfidence(initialConfidence, baseWindow, now)
	}
	policy := runtimepolicy.DecidePolicy(runtimepolicy.PolicyInput{CurrentMaxQualityRung: currentMaxQualityRung}, confidence, now)
	confidence = runtimepolicy.ApplyPolicy(confidence, policy, now)

	maxQualityRung := stricterMaxQualityRung(feedbackClampMaxQualityRung(summary, truth), policy.MaxQualityRung)
	return playbackFeedbackPolicy{
		MaxQualityRung: maxQualityRung,
		Summary:        summary,
		Confidence:     confidence,
		Policy:         policy,
	}, true
}

func feedbackClampMaxQualityRung(summary capreg.FeedbackSummary, truth playback.MediaTruth) playbackprofile.QualityRung {
	genericFailurePenalty := feedbackClampGenericFailurePenalty(summary, truth)
	switch {
	case summary.ConsecutiveStallFailures >= 2:
		return playbackprofile.RungRepairH264AAC
	case summary.ConsecutiveFailures >= 3+genericFailurePenalty:
		return playbackprofile.RungRepairH264AAC
	case summary.ConsecutiveDecodeFailures >= 2:
		return playbackprofile.RungCompatibleVideoH264CRF23
	case summary.ConsecutiveDecodeWarnings >= 2:
		return playbackprofile.RungCompatibleVideoH264CRF23
	case summary.ConsecutiveNetworkWarnings >= 3+feedbackClampWarningPenalty(summary, 212):
		return playbackprofile.RungCompatibleVideoH264CRF23
	case summary.ConsecutiveFailures >= 2+genericFailurePenalty:
		return playbackprofile.RungCompatibleVideoH264CRF23
	case summary.ConsecutiveBufferWarnings >= 3+feedbackClampWarningPenalty(summary, 211):
		return playbackprofile.RungCompatibleVideoH264CRF23
	default:
		return playbackprofile.RungUnknown
	}
}

func feedbackClampGenericFailurePenalty(summary capreg.FeedbackSummary, truth playback.MediaTruth) int {
	penalty := 0
	if normalizeBitrateConfidence(truth.BitrateConfidence) == "low" {
		penalty = 1
	}
	// A short, healthy run right before the current failure streak should buy
	// one retry step for generic failures, but not for explicit decode/stall cases.
	if summary.PriorStartedStreak >= 2 {
		penalty = 1
	}
	return penalty
}

func feedbackClampWarningPenalty(summary capreg.FeedbackSummary, expectedRecoveryCode int) int {
	if summary.PriorRecoveryStartCode != expectedRecoveryCode && summary.PriorRecoveryStartCode != 201 {
		return 0
	}
	switch {
	case summary.PriorRecoveredStartStreak >= 2:
		return 2
	case summary.PriorRecoveredStartStreak >= 1:
		return 1
	default:
		return 0
	}
}

func normalizeBitrateConfidence(raw string) string {
	switch normalize.Token(raw) {
	case "low", "medium", "high":
		return normalize.Token(raw)
	default:
		return ""
	}
}

func stricterMaxQualityRung(current, candidate playbackprofile.QualityRung) playbackprofile.QualityRung {
	current = playbackprofile.NormalizeQualityRung(string(current))
	candidate = playbackprofile.NormalizeQualityRung(string(candidate))
	if candidate == playbackprofile.RungUnknown {
		return current
	}
	if current == playbackprofile.RungUnknown {
		return candidate
	}
	if qualityClampSeverity(candidate) > qualityClampSeverity(current) {
		return candidate
	}
	return current
}

func qualityClampSeverity(rung playbackprofile.QualityRung) int {
	switch playbackprofile.ClampIntentToMaxQualityRung(playbackprofile.IntentQuality, rung) {
	case playbackprofile.IntentRepair:
		return 2
	case playbackprofile.IntentCompatible:
		return 1
	default:
		return 0
	}
}

func buildPlaybackConfidenceWindow(summary capreg.FeedbackSummary, truth playback.MediaTruth, subjectKind PlaybackSubjectKind, hostPressure playbackprofile.HostPressureAssessment, hostRuntime playbackprofile.HostRuntimeSnapshot) runtimepolicy.WindowFeatures {
	window := runtimepolicy.WindowFeatures{
		HardDecodeFails:         summary.ConsecutiveDecodeFailures,
		HardStallFails:          summary.ConsecutiveStallFailures,
		BufferWarnings:          summary.ConsecutiveBufferWarnings,
		NetworkWarnings:         summary.ConsecutiveNetworkWarnings,
		DecodeWarnings:          summary.ConsecutiveDecodeWarnings,
		CleanPlayingMS:          confidenceCleanPlayingMS(summary),
		WindowKind:              confidenceWindowKind(subjectKind),
		HostPressureBand:        playbackprofile.NormalizeHostPressureBand(string(hostPressure.EffectiveBand)),
		HostPerformanceClass:    hostRuntime.PerformanceClass,
		HostBenchmarkClass:      playbackprofile.BenchmarkClassForCodec(hostRuntime.Benchmark, "h264"),
		SourceBitrateConfidence: normalizeBitrateConfidence(truth.BitrateConfidence),
		SourceTruthFreshness:    confidenceSourceTruthFreshness(truth),
	}

	switch summary.PriorRecoveryStartCode {
	case 211:
		window.RecoveryBuffer = summary.PriorRecoveredStartStreak
	case 212:
		window.RecoveryNetwork = summary.PriorRecoveredStartStreak
	case 213:
		window.RecoveryDecode = summary.PriorRecoveredStartStreak
	}

	return window
}

func confidenceCleanPlayingMS(summary capreg.FeedbackSummary) int64 {
	switch {
	case summary.PriorRecoveredStartStreak > 0:
		return 8_000
	case summary.PriorStartedStreak >= 2:
		return 8_000
	default:
		return 0
	}
}

func confidenceWindowKind(subjectKind PlaybackSubjectKind) string {
	switch subjectKind {
	case PlaybackSubjectLive:
		return "live"
	case PlaybackSubjectRecording:
		return "vod"
	default:
		return ""
	}
}

func confidenceSourceTruthFreshness(truth playback.MediaTruth) string {
	switch {
	case truth.Status == playback.MediaStatusUnverified:
		return "stale"
	case strings.TrimSpace(truth.Container) == "" || strings.TrimSpace(truth.VideoCodec) == "" || strings.TrimSpace(truth.AudioCodec) == "":
		return "partial"
	default:
		return "fresh"
	}
}

func evaluatePlaybackConfidenceTimeline(initial runtimepolicy.ConfidenceSnapshot, windows []runtimepolicy.WindowFeatures, now time.Time) runtimepolicy.ConfidenceSnapshot {
	snapshot := initial
	for _, window := range windows {
		snapshot = runtimepolicy.EvaluateConfidence(snapshot, window, now)
	}
	return snapshot
}

func buildPlaybackConfidenceWindowsFromObservations(observations []capreg.PlaybackObservation, base runtimepolicy.WindowFeatures, now time.Time) []runtimepolicy.WindowFeatures {
	if len(observations) == 0 {
		return []runtimepolicy.WindowFeatures{base}
	}

	lookbackStart := now.Add(-playbackConfidenceLookback)
	ordered := append([]capreg.PlaybackObservation(nil), observations...)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].ObservedAt.Before(ordered[j].ObservedAt)
	})

	buckets := make(map[int64]*playbackConfidenceBucket, len(ordered))
	getBucket := func(ts time.Time) *playbackConfidenceBucket {
		start := ts.UTC().Truncate(playbackConfidenceWindow)
		key := start.Unix()
		if existing, ok := buckets[key]; ok {
			return existing
		}
		entry := &playbackConfidenceBucket{start: start, feature: resetPlaybackConfidenceFeature(base)}
		buckets[key] = entry
		return entry
	}

	probePending := false
	for _, observation := range ordered {
		ts := observation.ObservedAt.UTC()
		if ts.Before(lookbackStart) || ts.After(now) {
			continue
		}
		current := getBucket(ts)
		switch observation.Outcome {
		case "failed":
			if probePending {
				current.feature.ProbeWindowRegressed++
				probePending = false
			}
			switch observation.FeedbackCode {
			case 3:
				current.feature.HardDecodeFails++
			case 4:
				current.feature.HardStallFails++
			}
		case "warning":
			if probePending {
				current.feature.ProbeWindowRegressed++
				probePending = false
			}
			if confidenceIsBufferingWarningCode(observation.FeedbackCode) {
				current.feature.BufferWarnings++
			}
			if confidenceIsDecodeWarningCode(observation.FeedbackCode) {
				current.feature.DecodeWarnings++
			}
			if confidenceIsNetworkWarningCode(observation.FeedbackCode) {
				current.feature.NetworkWarnings++
			}
		case "started":
			switch observation.FeedbackCode {
			case 211:
				current.feature.RecoveryBuffer++
			case 212:
				current.feature.RecoveryNetwork++
			case 213:
				current.feature.RecoveryDecode++
			case 220:
				current.feature.ProbeWindowStarted++
				probePending = true
			case 221:
				current.feature.ProbeWindowConfirmed++
				probePending = false
			}
		}
	}

	playingSince := time.Time{}
	for _, observation := range ordered {
		ts := observation.ObservedAt.UTC()
		if ts.Before(lookbackStart) || ts.After(now) {
			continue
		}
		switch observation.Outcome {
		case "started":
			if confidenceIsProbeConfirmedCode(observation.FeedbackCode) && !playingSince.IsZero() {
				continue
			}
			playingSince = ts
		case "warning", "failed":
			if !playingSince.IsZero() {
				allocatePlaybackConfidenceCleanMS(buckets, base, playingSince, ts)
				playingSince = time.Time{}
			}
		}
	}
	if !playingSince.IsZero() {
		allocatePlaybackConfidenceCleanMS(buckets, base, playingSince, now)
	}

	starts := make([]int64, 0, len(buckets))
	for key := range buckets {
		starts = append(starts, key)
	}
	slices.Sort(starts)

	windows := make([]runtimepolicy.WindowFeatures, 0, len(starts))
	for _, key := range starts {
		windows = append(windows, buckets[key].feature)
	}
	if len(windows) == 0 {
		return []runtimepolicy.WindowFeatures{base}
	}
	return windows
}

func allocatePlaybackConfidenceCleanMS(buckets map[int64]*playbackConfidenceBucket, base runtimepolicy.WindowFeatures, start, end time.Time) {
	if end.Before(start) || end.Equal(start) {
		return
	}
	current := start.UTC()
	limit := end.UTC()
	for current.Before(limit) {
		windowStart := current.Truncate(playbackConfidenceWindow)
		windowEnd := windowStart.Add(playbackConfidenceWindow)
		segmentEnd := windowEnd
		if segmentEnd.After(limit) {
			segmentEnd = limit
		}
		key := windowStart.Unix()
		entry, ok := buckets[key]
		if !ok {
			entry = &playbackConfidenceBucket{start: windowStart, feature: resetPlaybackConfidenceFeature(base)}
			buckets[key] = entry
		}
		entry.feature.CleanPlayingMS += segmentEnd.Sub(current).Milliseconds()
		current = segmentEnd
	}
}

func confidenceIsBufferingWarningCode(code int) bool {
	switch code {
	case 101, 102:
		return true
	default:
		return false
	}
}

func confidenceIsDecodeWarningCode(code int) bool {
	// Keep in lock-step with capreg.isDecodeWarningCode. 103 is a generic decode
	// warning; 242 is the HLS.js black-render code (playbackInfoCodeHLSJSRenderBlack)
	// — a decode failure surfaced by the player. The windowed confidence engine
	// must count it as a decode warning so repeated black-screening sets
	// ConstraintNoProbeUp instead of letting the engine probe up.
	switch code {
	case 103, 242:
		return true
	default:
		return false
	}
}

func confidenceIsNetworkWarningCode(code int) bool {
	return code == 104
}

func confidenceIsProbeConfirmedCode(code int) bool {
	return code == 221
}

func (s *Service) rememberPlaybackPolicyState(ctx context.Context, sourceRef string, req PlaybackInfoRequest, truth playback.MediaTruth, resolved capabilities.PlaybackCapabilities, hostContext requestHostContext, policy playbackFeedbackPolicy) {
	registry := s.deps.CapabilityRegistry()
	if registry == nil {
		return
	}
	store, ok := registry.(capreg.PlaybackPolicyStateStore)
	if !ok {
		return
	}

	hostFingerprint := strings.TrimSpace(hostContext.Snapshot.Identity.Fingerprint())
	deviceFingerprint := deviceIdentityForRequest(req, resolved).Fingerprint()
	sourceFingerprint := s.sourceSnapshotForRequest(ctx, sourceRef, req, truth).Fingerprint()
	if hostFingerprint == "" || deviceFingerprint == "" || sourceFingerprint == "" {
		return
	}

	if err := store.RememberPlaybackPolicyState(ctx, capreg.PlaybackPolicyState{
		SubjectKind:       string(req.SubjectKind),
		SourceFingerprint: sourceFingerprint,
		DeviceFingerprint: deviceFingerprint,
		HostFingerprint:   hostFingerprint,
		MaxQualityRung:    policy.MaxQualityRung,
		Confidence:        policy.Confidence,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		log.L().Warn().Err(err).Str("requestId", req.RequestID).Str("sourceRef", sourceRef).Msg("playback policy state persist failed")
	}
}
