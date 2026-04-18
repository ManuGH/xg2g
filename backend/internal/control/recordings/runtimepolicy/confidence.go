package runtimepolicy

import (
	"sort"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/normalize"
)

const (
	confidenceMinScore       = -100
	confidenceMaxScore       = 100
	cooldownLockCurrent      = 20 * time.Second
	cooldownProbeUp          = 15 * time.Second
	cooldownDegrade          = 30 * time.Second
	cooldownHardRisk         = 45 * time.Second
	holdLowBeforeRecovery    = 20 * time.Second
	holdRecoveryBeforeStable = 20 * time.Second
	holdStableBeforeHigh     = 40 * time.Second
	holdHighBeforeProbeUp    = 10 * time.Second
	probeTrustWindow         = 2 * time.Minute
)

func EvaluateConfidence(prev ConfidenceSnapshot, win WindowFeatures, now time.Time) ConfidenceSnapshot {
	score := decayConfidenceScore(prev.Score)
	reasons := make(map[string]struct{}, 8)
	constraints := make(map[string]struct{}, 6)
	probeSuccessStreak, probeFailureStreak, lastProbeEventAt := evolveProbeTrust(prev, win, now)

	addReason := func(reason string) {
		reason = strings.TrimSpace(reason)
		if reason == "" {
			return
		}
		reasons[reason] = struct{}{}
	}
	addConstraint := func(constraint string) {
		constraint = strings.TrimSpace(constraint)
		if constraint == "" {
			return
		}
		constraints[constraint] = struct{}{}
	}

	if win.HardDecodeFails > 0 {
		score -= 45 * win.HardDecodeFails
		addReason(ReasonDecodeRiskHigh)
		addConstraint(ConstraintDecodeRiskHard)
		addConstraint(ConstraintNoProbeUp)
	}
	if win.HardStallFails > 0 {
		score -= 35 * win.HardStallFails
		addReason(ReasonStallRecent)
		addConstraint(ConstraintNoProbeUp)
	}
	if win.DecodeWarnings > 0 {
		score -= 18 * win.DecodeWarnings
		addReason(ReasonDecodeWarningRecent)
		addConstraint(ConstraintNoProbeUp)
	}
	if win.NetworkWarnings > 0 {
		score -= 10 * win.NetworkWarnings
		addReason(ReasonNetworkRecentlyUnstable)
	}
	if win.BufferWarnings > 0 {
		score -= 8 * win.BufferWarnings
		addReason(ReasonBufferingRecent)
	}

	if win.RecoveryBuffer > 0 {
		score += minInt(16, 8*win.RecoveryBuffer)
		addReason(ReasonBufferingRecovered)
	}
	if win.RecoveryNetwork > 0 {
		score += minInt(16, 8*win.RecoveryNetwork)
		addReason(ReasonNetworkRecovered)
	}
	if win.RecoveryDecode > 0 {
		score += minInt(8, 4*win.RecoveryDecode)
		addReason(ReasonDecoderRecovered)
	}
	if win.ProbeWindowStarted > 0 {
		addConstraint(ConstraintNoProbeUp)
	}
	if win.ProbeWindowConfirmed > 0 {
		score += minInt(18, 10*win.ProbeWindowConfirmed)
		addReason(ReasonProbeWindowConfirmed)
	}
	if win.ProbeWindowRegressed > 0 {
		score -= minInt(30, 16*win.ProbeWindowRegressed)
		addReason(ReasonProbeWindowRegressed)
		addConstraint(ConstraintNoProbeUp)
	}
	if probeSuccessStreak > 0 && isRecentProbeTrust(lastProbeEventAt, now) {
		addReason(ReasonProbeRecentlyConfirmed)
	}
	if probeFailureStreak > 0 && isRecentProbeTrust(lastProbeEventAt, now) {
		addReason(ReasonProbeRecentlyRegressed)
		addConstraint(ConstraintNoProbeUp)
	}

	switch {
	case win.CleanPlayingMS >= 8_000:
		score += 12
		addReason(ReasonCleanPlaybackWindow)
	case win.CleanPlayingMS >= 4_000:
		score += 6
		addReason(ReasonCleanPlaybackWindow)
	}

	switch playbackprofile.NormalizeHostPressureBand(string(win.HostPressureBand)) {
	case playbackprofile.HostPressureCritical:
		score -= 16
		addReason(ReasonHostPressureHigh)
		addConstraint(ConstraintNoProbeUp)
	case playbackprofile.HostPressureConstrained:
		score -= 12
		addReason(ReasonHostPressureHigh)
		addConstraint(ConstraintNoProbeUp)
	case playbackprofile.HostPressureElevated:
		score -= 6
		addReason(ReasonHostPressureHigh)
	case playbackprofile.HostPressureNormal:
		score += 4
		addReason(ReasonHeadroomGood)
	}

	switch normalize.Token(win.HostPerformanceClass) {
	case "low":
		score -= 6
	case "medium":
	case "high":
		score += 4
		addReason(ReasonHeadroomGood)
	case "ultra":
		score += 8
		addReason(ReasonHeadroomGood)
	}

	switch normalize.Token(win.HostBenchmarkClass) {
	case "weak":
		score -= 8
	case "moderate":
	case "strong":
		score += 6
		addReason(ReasonHeadroomGood)
	}

	switch normalize.Token(win.SourceBitrateConfidence) {
	case "low":
		score -= 4
		addReason(ReasonSourceTruthLowConfidence)
	case "high":
		score += 4
	}

	switch normalize.Token(win.SourceTruthFreshness) {
	case "stale":
		score -= 12
		addReason(ReasonSourceTruthStale)
	case "partial":
		score -= 4
	case "fresh":
		score += 4
	}

	score = clampConfidenceScore(score)
	targetState := deriveConfidenceState(score)
	state, stateSince := resolveConfidenceState(prev, targetState, win, score, now)

	if prev.CooldownUntil.After(now) {
		addConstraint(ConstraintCooldownActive)
		addConstraint(ConstraintNoProbeUp)
	}

	if state == ConfidenceRecovery && win.HardDecodeFails == 0 && win.DecodeWarnings == 0 && (win.BufferWarnings > 0 || win.NetworkWarnings > 0) {
		addConstraint(ConstraintLockCurrentRung)
		addConstraint(ConstraintNoProbeUp)
	}

	if state == ConfidenceLow {
		addConstraint(ConstraintNoProbeUp)
	}

	return ConfidenceSnapshot{
		Score:              score,
		State:              state,
		StateSince:         stateSince,
		WindowCount:        prev.WindowCount + 1,
		CooldownUntil:      prev.CooldownUntil,
		ProbeSuccessStreak: probeSuccessStreak,
		ProbeFailureStreak: probeFailureStreak,
		LastProbeEventAt:   lastProbeEventAt,
		PolicyConstraints:  sortedKeys(constraints),
		Reasons:            sortedKeys(reasons),
	}
}

func DecidePolicy(input PolicyInput, conf ConfidenceSnapshot, now time.Time) PolicyDecision {
	constraints := make(map[string]struct{}, len(conf.PolicyConstraints))
	for _, constraint := range conf.PolicyConstraints {
		if strings.TrimSpace(constraint) != "" {
			constraints[constraint] = struct{}{}
		}
	}
	reasons := make(map[string]struct{}, len(conf.Reasons))
	for _, reason := range conf.Reasons {
		if strings.TrimSpace(reason) != "" {
			reasons[reason] = struct{}{}
		}
	}
	addReason := func(reason string) {
		reason = strings.TrimSpace(reason)
		if reason == "" {
			return
		}
		reasons[reason] = struct{}{}
	}
	addConstraint := func(constraint string) {
		constraint = strings.TrimSpace(constraint)
		if constraint == "" {
			return
		}
		constraints[constraint] = struct{}{}
	}
	if conf.ProbeSuccessStreak > 0 && isRecentProbeTrust(conf.LastProbeEventAt, now) {
		addReason(ReasonProbeRecentlyConfirmed)
	}
	if conf.ProbeFailureStreak > 0 && isRecentProbeTrust(conf.LastProbeEventAt, now) {
		addReason(ReasonProbeRecentlyRegressed)
		addConstraint(ConstraintNoProbeUp)
	}

	decision := PolicyDecision{
		Action:            PolicyHold,
		MaxQualityRung:    playbackprofile.RungUnknown,
		PolicyConstraints: sortedKeys(constraints),
		Reasons:           sortedKeys(reasons),
	}

	if conf.CooldownUntil.After(now) || hasKey(constraints, ConstraintCooldownActive) {
		addConstraint(ConstraintCooldownActive)
		addConstraint(ConstraintNoProbeUp)
		decision.Action = PolicyCooldown
		decision.MaxQualityRung = playbackprofile.NormalizeQualityRung(string(input.CurrentMaxQualityRung))
		decision.PolicyConstraints = sortedKeys(constraints)
		return decision
	}

	if hasKey(constraints, ConstraintDecodeRiskHard) {
		addConstraint(ConstraintMaxQualityCompatible)
		addConstraint(ConstraintNoProbeUp)
		decision.Action = PolicyDegrade
		decision.MaxQualityRung = playbackprofile.RungCompatibleVideoH264CRF23
		decision.PolicyConstraints = sortedKeys(constraints)
		return decision
	}

	switch conf.State {
	case ConfidenceLow:
		addConstraint(ConstraintNoProbeUp)
		decision.Action = PolicyDegrade
		if hasKey(reasons, ReasonStallRecent) || conf.Score <= -60 {
			addConstraint(ConstraintMaxQualityRepair)
			decision.MaxQualityRung = playbackprofile.RungRepairH264AAC
		} else {
			addConstraint(ConstraintMaxQualityCompatible)
			decision.MaxQualityRung = playbackprofile.RungCompatibleVideoH264CRF23
		}
	case ConfidenceRecovery:
		if hasKey(constraints, ConstraintLockCurrentRung) {
			addConstraint(ConstraintNoProbeUp)
			decision.Action = PolicyLockCurrent
		}
	case ConfidenceStable:
		decision.Action = PolicyHold
	case ConfidenceHigh:
		if candidate, ok := nextProbeUpRung(input.CurrentMaxQualityRung); ok && canProbeUp(conf, constraints, now) {
			addReason(ReasonProbeUpReady)
			decision.Action = PolicyProbeUp
			decision.MaxQualityRung = candidate
			decision.ProbeCandidate = candidate
			break
		}
		decision.Action = PolicyHold
	default:
		decision.Action = PolicyHold
	}

	decision.PolicyConstraints = sortedKeys(constraints)
	decision.Reasons = sortedKeys(reasons)
	return decision
}

func ApplyPolicy(snapshot ConfidenceSnapshot, decision PolicyDecision, now time.Time) ConfidenceSnapshot {
	if snapshot.CooldownUntil.Before(now) {
		snapshot.CooldownUntil = time.Time{}
	}

	switch decision.Action {
	case PolicyLockCurrent:
		snapshot.CooldownUntil = maxTime(snapshot.CooldownUntil, now.Add(cooldownLockCurrent))
	case PolicyProbeUp:
		if decision.MaxQualityRung != playbackprofile.RungUnknown {
			snapshot.CooldownUntil = maxTime(snapshot.CooldownUntil, now.Add(cooldownProbeUp))
		}
	case PolicyDegrade:
		if hasString(decision.PolicyConstraints, ConstraintDecodeRiskHard) || decision.MaxQualityRung == playbackprofile.RungRepairH264AAC {
			snapshot.CooldownUntil = maxTime(snapshot.CooldownUntil, now.Add(cooldownHardRisk))
		} else {
			snapshot.CooldownUntil = maxTime(snapshot.CooldownUntil, now.Add(cooldownDegrade))
		}
	case PolicyCooldown:
		snapshot.CooldownUntil = maxTime(snapshot.CooldownUntil, now.Add(cooldownDegrade))
	}

	if snapshot.CooldownUntil.After(now) {
		merged := make(map[string]struct{}, len(snapshot.PolicyConstraints)+1)
		for _, constraint := range snapshot.PolicyConstraints {
			if strings.TrimSpace(constraint) != "" {
				merged[constraint] = struct{}{}
			}
		}
		merged[ConstraintCooldownActive] = struct{}{}
		snapshot.PolicyConstraints = sortedKeys(merged)
	}

	return snapshot
}

func deriveConfidenceState(score int) ConfidenceState {
	switch {
	case score <= -30:
		return ConfidenceLow
	case score >= 50:
		return ConfidenceHigh
	case score >= 10:
		return ConfidenceStable
	default:
		return ConfidenceRecovery
	}
}

func resolveConfidenceState(prev ConfidenceSnapshot, target ConfidenceState, win WindowFeatures, score int, now time.Time) (ConfidenceState, time.Time) {
	if shouldForceLowConfidence(win, score) {
		if prev.State == ConfidenceLow && !prev.StateSince.IsZero() {
			return ConfidenceLow, prev.StateSince
		}
		return ConfidenceLow, now
	}

	if prev.State == "" {
		if target == ConfidenceHigh {
			return ConfidenceStable, now
		}
		return target, now
	}

	current := prev.State
	stateSince := prev.StateSince
	if stateSince.IsZero() {
		stateSince = now
	}

	targetRank := confidenceStateRank(target)
	currentRank := confidenceStateRank(current)

	switch {
	case targetRank < currentRank:
		return target, now
	case targetRank == currentRank:
		return current, stateSince
	}

	next := current
	var hold time.Duration
	switch current {
	case ConfidenceLow:
		next = ConfidenceRecovery
		hold = holdLowBeforeRecovery
	case ConfidenceRecovery:
		next = ConfidenceStable
		hold = holdRecoveryBeforeStable
	case ConfidenceStable:
		next = ConfidenceHigh
		hold = holdStableBeforeHigh
	case ConfidenceHigh:
		return ConfidenceHigh, stateSince
	default:
		return target, now
	}

	if now.Sub(stateSince) < hold {
		return current, stateSince
	}
	return next, now
}

func shouldForceLowConfidence(win WindowFeatures, score int) bool {
	return win.HardDecodeFails > 0 || win.HardStallFails > 0 || score <= -60
}

func confidenceStateRank(state ConfidenceState) int {
	switch state {
	case ConfidenceLow:
		return 0
	case ConfidenceRecovery:
		return 1
	case ConfidenceStable:
		return 2
	case ConfidenceHigh:
		return 3
	default:
		return -1
	}
}

func canProbeUp(conf ConfidenceSnapshot, constraints map[string]struct{}, now time.Time) bool {
	if conf.State != ConfidenceHigh {
		return false
	}
	if hasKey(constraints, ConstraintNoProbeUp) || hasKey(constraints, ConstraintCooldownActive) {
		return false
	}
	if conf.ProbeFailureStreak > 0 && isRecentProbeTrust(conf.LastProbeEventAt, now) {
		return false
	}
	scoreThreshold := 60
	if conf.ProbeSuccessStreak > 0 && isRecentProbeTrust(conf.LastProbeEventAt, now) {
		switch {
		case conf.ProbeSuccessStreak >= 2:
			scoreThreshold = 52
		default:
			scoreThreshold = 56
		}
	}
	if conf.Score < scoreThreshold || conf.WindowCount < 3 {
		return false
	}
	if conf.StateSince.IsZero() || now.Sub(conf.StateSince) < holdHighBeforeProbeUp {
		return false
	}
	return true
}

func evolveProbeTrust(prev ConfidenceSnapshot, win WindowFeatures, now time.Time) (int, int, time.Time) {
	successStreak := prev.ProbeSuccessStreak
	failureStreak := prev.ProbeFailureStreak
	lastProbeEventAt := prev.LastProbeEventAt

	if !isRecentProbeTrust(lastProbeEventAt, now) {
		successStreak = 0
		failureStreak = 0
		lastProbeEventAt = time.Time{}
	}

	switch {
	case win.ProbeWindowRegressed > 0:
		failureStreak = minInt(3, maxInt(1, failureStreak+win.ProbeWindowRegressed))
		successStreak = 0
		lastProbeEventAt = now
	case win.ProbeWindowConfirmed > 0:
		successStreak = minInt(3, maxInt(1, successStreak+win.ProbeWindowConfirmed))
		failureStreak = 0
		lastProbeEventAt = now
	}

	return successStreak, failureStreak, lastProbeEventAt
}

func isRecentProbeTrust(lastProbeEventAt, now time.Time) bool {
	return !lastProbeEventAt.IsZero() && !lastProbeEventAt.Before(now.Add(-probeTrustWindow))
}

func nextProbeUpRung(current playbackprofile.QualityRung) (playbackprofile.QualityRung, bool) {
	switch playbackprofile.NormalizeQualityRung(string(current)) {
	case playbackprofile.RungRepairAudioAAC192Stereo:
		return playbackprofile.RungCompatibleAudioAAC256Stereo, true
	case playbackprofile.RungRepairVideoH264CRF28, playbackprofile.RungRepairH264AAC:
		return playbackprofile.RungCompatibleVideoH264CRF23, true
	case playbackprofile.RungCompatibleAudioAAC256Stereo:
		return playbackprofile.RungQualityAudioAAC320Stereo, true
	case playbackprofile.RungCompatibleVideoH264CRF23:
		return playbackprofile.RungQualityVideoH264CRF20, true
	default:
		return playbackprofile.RungUnknown, false
	}
}

func decayConfidenceScore(score int) int {
	switch {
	case score > 0:
		score -= 4
		if score < 0 {
			return 0
		}
	case score < 0:
		score += 4
		if score > 0 {
			return 0
		}
	}
	return score
}

func clampConfidenceScore(score int) int {
	switch {
	case score < confidenceMinScore:
		return confidenceMinScore
	case score > confidenceMaxScore:
		return confidenceMaxScore
	default:
		return score
	}
}

func hasKey(set map[string]struct{}, key string) bool {
	_, ok := set[strings.TrimSpace(key)]
	return ok
}

func sortedKeys(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func hasString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
