package runtimepolicy

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

func TestEvaluateConfidence_HardDecodeRiskStaysHard(t *testing.T) {
	snapshot := EvaluateConfidence(ConfidenceSnapshot{}, WindowFeatures{
		HardDecodeFails:      1,
		HostPressureBand:     playbackprofile.HostPressureNormal,
		HostPerformanceClass: "medium",
		SourceTruthFreshness: "fresh",
	}, time.Date(2026, 4, 18, 10, 0, 0, 0, time.UTC))

	if snapshot.State != ConfidenceLow {
		t.Fatalf("expected low confidence, got %q", snapshot.State)
	}
	if snapshot.Score >= 0 {
		t.Fatalf("expected negative score, got %d", snapshot.Score)
	}
	if !contains(snapshot.Reasons, ReasonDecodeRiskHigh) {
		t.Fatalf("expected decode risk reason, got %#v", snapshot.Reasons)
	}
	if !contains(snapshot.PolicyConstraints, ConstraintDecodeRiskHard) {
		t.Fatalf("expected hard decode constraint, got %#v", snapshot.PolicyConstraints)
	}
	if !contains(snapshot.PolicyConstraints, ConstraintNoProbeUp) {
		t.Fatalf("expected no-probe constraint, got %#v", snapshot.PolicyConstraints)
	}
}

func TestEvaluateConfidence_CleanRecoveredWindowBuildsStableTrust(t *testing.T) {
	now := time.Date(2026, 4, 18, 10, 0, 0, 0, time.UTC)
	snapshot := EvaluateConfidence(ConfidenceSnapshot{}, WindowFeatures{
		RecoveryBuffer:          2,
		CleanPlayingMS:          10_000,
		HostPressureBand:        playbackprofile.HostPressureNormal,
		HostPerformanceClass:    "high",
		HostBenchmarkClass:      "strong",
		SourceBitrateConfidence: "high",
		SourceTruthFreshness:    "fresh",
	}, now)

	if snapshot.State != ConfidenceStable {
		t.Fatalf("expected stable confidence for initial high-trust window, got %q", snapshot.State)
	}
	if snapshot.Score <= 0 {
		t.Fatalf("expected positive score, got %d", snapshot.Score)
	}
	if snapshot.StateSince != now {
		t.Fatalf("expected state since to initialize at current window time, got %v", snapshot.StateSince)
	}
	if !contains(snapshot.Reasons, ReasonBufferingRecovered) {
		t.Fatalf("expected buffering recovery reason, got %#v", snapshot.Reasons)
	}
	if !contains(snapshot.Reasons, ReasonHeadroomGood) {
		t.Fatalf("expected headroom reason, got %#v", snapshot.Reasons)
	}
}

func TestEvaluateConfidence_ProbeConfirmationBuildsConfidence(t *testing.T) {
	now := time.Date(2026, 4, 18, 10, 0, 0, 0, time.UTC)
	snapshot := EvaluateConfidence(ConfidenceSnapshot{
		Score:      42,
		State:      ConfidenceStable,
		StateSince: now.Add(-45 * time.Second),
	}, WindowFeatures{
		ProbeWindowConfirmed:    1,
		CleanPlayingMS:          10_000,
		HostPressureBand:        playbackprofile.HostPressureNormal,
		HostPerformanceClass:    "high",
		HostBenchmarkClass:      "strong",
		SourceBitrateConfidence: "high",
		SourceTruthFreshness:    "fresh",
	}, now)

	if snapshot.Score <= 42 {
		t.Fatalf("expected probe confirmation to raise confidence, got %d", snapshot.Score)
	}
	if !contains(snapshot.Reasons, ReasonProbeWindowConfirmed) {
		t.Fatalf("expected probe confirmation reason, got %#v", snapshot.Reasons)
	}
	if snapshot.ProbeSuccessStreak != 1 || snapshot.ProbeFailureStreak != 0 {
		t.Fatalf("expected persisted probe success state, got success=%d failure=%d", snapshot.ProbeSuccessStreak, snapshot.ProbeFailureStreak)
	}
	if snapshot.LastProbeEventAt != now {
		t.Fatalf("expected last probe event timestamp at current window time, got %v", snapshot.LastProbeEventAt)
	}
}

func TestEvaluateConfidence_ProbeRegressionBlocksFurtherProbe(t *testing.T) {
	now := time.Date(2026, 4, 18, 10, 0, 0, 0, time.UTC)
	snapshot := EvaluateConfidence(ConfidenceSnapshot{
		Score:              78,
		State:              ConfidenceHigh,
		StateSince:         now.Add(-20 * time.Second),
		ProbeSuccessStreak: 1,
		LastProbeEventAt:   now.Add(-30 * time.Second),
	}, WindowFeatures{
		ProbeWindowStarted:      1,
		ProbeWindowRegressed:    1,
		NetworkWarnings:         1,
		HostPressureBand:        playbackprofile.HostPressureNormal,
		HostPerformanceClass:    "high",
		HostBenchmarkClass:      "strong",
		SourceBitrateConfidence: "high",
		SourceTruthFreshness:    "fresh",
	}, now)

	if snapshot.Score >= 78 {
		t.Fatalf("expected regressed probe to lower confidence, got %d", snapshot.Score)
	}
	if !contains(snapshot.Reasons, ReasonProbeWindowRegressed) {
		t.Fatalf("expected probe regression reason, got %#v", snapshot.Reasons)
	}
	if !contains(snapshot.PolicyConstraints, ConstraintNoProbeUp) {
		t.Fatalf("expected regressed probe to block further probing, got %#v", snapshot.PolicyConstraints)
	}
	if snapshot.ProbeSuccessStreak != 0 || snapshot.ProbeFailureStreak != 1 {
		t.Fatalf("expected persisted probe regression state, got success=%d failure=%d", snapshot.ProbeSuccessStreak, snapshot.ProbeFailureStreak)
	}
}

func TestDecidePolicy_HardDecodeRiskDegradesToCompatible(t *testing.T) {
	decision := DecidePolicy(PolicyInput{}, ConfidenceSnapshot{
		Score:             -40,
		State:             ConfidenceLow,
		PolicyConstraints: []string{ConstraintDecodeRiskHard},
		Reasons:           []string{ReasonDecodeRiskHigh},
	}, time.Date(2026, 4, 18, 10, 0, 0, 0, time.UTC))

	if decision.Action != PolicyDegrade {
		t.Fatalf("expected degrade action, got %q", decision.Action)
	}
	if decision.MaxQualityRung != playbackprofile.RungCompatibleVideoH264CRF23 {
		t.Fatalf("expected compatible max quality, got %q", decision.MaxQualityRung)
	}
	if !contains(decision.PolicyConstraints, ConstraintMaxQualityCompatible) {
		t.Fatalf("expected compatible constraint, got %#v", decision.PolicyConstraints)
	}
}

func TestDecidePolicy_LowConfidenceWithRecentStallsDegradesToRepair(t *testing.T) {
	decision := DecidePolicy(PolicyInput{}, ConfidenceSnapshot{
		Score:   -70,
		State:   ConfidenceLow,
		Reasons: []string{ReasonStallRecent},
	}, time.Date(2026, 4, 18, 10, 0, 0, 0, time.UTC))

	if decision.Action != PolicyDegrade {
		t.Fatalf("expected degrade action, got %q", decision.Action)
	}
	if decision.MaxQualityRung != playbackprofile.RungRepairH264AAC {
		t.Fatalf("expected repair max quality, got %q", decision.MaxQualityRung)
	}
	if !contains(decision.PolicyConstraints, ConstraintMaxQualityRepair) {
		t.Fatalf("expected repair constraint, got %#v", decision.PolicyConstraints)
	}
}

func TestApplyPolicy_SetsCooldownForHardRisk(t *testing.T) {
	now := time.Date(2026, 4, 18, 10, 0, 0, 0, time.UTC)
	snapshot := ApplyPolicy(ConfidenceSnapshot{}, PolicyDecision{
		Action:            PolicyDegrade,
		MaxQualityRung:    playbackprofile.RungCompatibleVideoH264CRF23,
		PolicyConstraints: []string{ConstraintDecodeRiskHard},
	}, now)

	if !snapshot.CooldownUntil.After(now) {
		t.Fatalf("expected cooldown to be set")
	}
	if !contains(snapshot.PolicyConstraints, ConstraintCooldownActive) {
		t.Fatalf("expected cooldown constraint, got %#v", snapshot.PolicyConstraints)
	}
}

func TestDecidePolicy_HighConfidenceProbesRepairUpToCompatible(t *testing.T) {
	now := time.Date(2026, 4, 18, 10, 3, 0, 0, time.UTC)
	decision := DecidePolicy(PolicyInput{
		CurrentMaxQualityRung: playbackprofile.RungRepairH264AAC,
	}, ConfidenceSnapshot{
		Score:       72,
		State:       ConfidenceHigh,
		StateSince:  now.Add(-12 * time.Second),
		WindowCount: 5,
		Reasons:     []string{ReasonHeadroomGood, ReasonCleanPlaybackWindow},
	}, now)

	if decision.Action != PolicyProbeUp {
		t.Fatalf("expected probe-up action, got %q", decision.Action)
	}
	if decision.MaxQualityRung != playbackprofile.RungCompatibleVideoH264CRF23 {
		t.Fatalf("expected repair probe-up to target compatible, got %q", decision.MaxQualityRung)
	}
	if decision.ProbeCandidate != playbackprofile.RungCompatibleVideoH264CRF23 {
		t.Fatalf("expected probe candidate to target compatible, got %q", decision.ProbeCandidate)
	}
	if !contains(decision.Reasons, ReasonProbeUpReady) {
		t.Fatalf("expected probe-up ready reason, got %#v", decision.Reasons)
	}
}

func TestDecidePolicy_HighConfidenceProbesCompatibleUpToQuality(t *testing.T) {
	now := time.Date(2026, 4, 18, 10, 3, 30, 0, time.UTC)
	decision := DecidePolicy(PolicyInput{
		CurrentMaxQualityRung: playbackprofile.RungCompatibleVideoH264CRF23,
	}, ConfidenceSnapshot{
		Score:       80,
		State:       ConfidenceHigh,
		StateSince:  now.Add(-15 * time.Second),
		WindowCount: 6,
		Reasons:     []string{ReasonHeadroomGood, ReasonCleanPlaybackWindow},
	}, now)

	if decision.Action != PolicyProbeUp {
		t.Fatalf("expected probe-up action, got %q", decision.Action)
	}
	if decision.MaxQualityRung != playbackprofile.RungQualityVideoH264CRF20 {
		t.Fatalf("expected compatible probe-up to target quality, got %q", decision.MaxQualityRung)
	}
}

func TestDecidePolicy_HighConfidenceProbeBlockedByGuardrails(t *testing.T) {
	now := time.Date(2026, 4, 18, 10, 3, 45, 0, time.UTC)
	decision := DecidePolicy(PolicyInput{
		CurrentMaxQualityRung: playbackprofile.RungRepairH264AAC,
	}, ConfidenceSnapshot{
		Score:             78,
		State:             ConfidenceHigh,
		StateSince:        now.Add(-5 * time.Second),
		WindowCount:       6,
		PolicyConstraints: []string{ConstraintNoProbeUp},
	}, now)

	if decision.Action != PolicyHold {
		t.Fatalf("expected probe-up to be blocked, got %q", decision.Action)
	}
	if decision.MaxQualityRung != playbackprofile.RungUnknown {
		t.Fatalf("expected no max-quality change when probe-up is blocked, got %q", decision.MaxQualityRung)
	}
}

func TestDecidePolicy_RecentProbeRegressionBlocksProbeUp(t *testing.T) {
	now := time.Date(2026, 4, 18, 10, 3, 45, 0, time.UTC)
	decision := DecidePolicy(PolicyInput{
		CurrentMaxQualityRung: playbackprofile.RungRepairH264AAC,
	}, ConfidenceSnapshot{
		Score:              82,
		State:              ConfidenceHigh,
		StateSince:         now.Add(-15 * time.Second),
		WindowCount:        6,
		ProbeFailureStreak: 1,
		LastProbeEventAt:   now.Add(-30 * time.Second),
	}, now)

	if decision.Action != PolicyHold {
		t.Fatalf("expected recent probe regression to block probe-up, got %q", decision.Action)
	}
	if !contains(decision.Reasons, ReasonProbeRecentlyRegressed) {
		t.Fatalf("expected recent probe regression reason, got %#v", decision.Reasons)
	}
	if !contains(decision.PolicyConstraints, ConstraintNoProbeUp) {
		t.Fatalf("expected no-probe constraint, got %#v", decision.PolicyConstraints)
	}
}

func TestDecidePolicy_RecentProbeSuccessLowersProbeThreshold(t *testing.T) {
	now := time.Date(2026, 4, 18, 10, 3, 30, 0, time.UTC)
	decision := DecidePolicy(PolicyInput{
		CurrentMaxQualityRung: playbackprofile.RungCompatibleVideoH264CRF23,
	}, ConfidenceSnapshot{
		Score:              58,
		State:              ConfidenceHigh,
		StateSince:         now.Add(-15 * time.Second),
		WindowCount:        6,
		ProbeSuccessStreak: 1,
		LastProbeEventAt:   now.Add(-30 * time.Second),
		Reasons:            []string{ReasonHeadroomGood, ReasonCleanPlaybackWindow},
	}, now)

	if decision.Action != PolicyProbeUp {
		t.Fatalf("expected recent probe success to permit probe-up at reduced threshold, got %q", decision.Action)
	}
	if decision.MaxQualityRung != playbackprofile.RungQualityVideoH264CRF20 {
		t.Fatalf("expected compatible probe-up to target quality, got %q", decision.MaxQualityRung)
	}
	if !contains(decision.Reasons, ReasonProbeRecentlyConfirmed) {
		t.Fatalf("expected recent probe success reason, got %#v", decision.Reasons)
	}
}

func TestApplyPolicy_ProbeUpSetsCooldown(t *testing.T) {
	now := time.Date(2026, 4, 18, 10, 4, 0, 0, time.UTC)
	snapshot := ApplyPolicy(ConfidenceSnapshot{}, PolicyDecision{
		Action:         PolicyProbeUp,
		MaxQualityRung: playbackprofile.RungCompatibleVideoH264CRF23,
	}, now)

	if !snapshot.CooldownUntil.After(now) {
		t.Fatalf("expected probe-up cooldown to be set")
	}
	if !contains(snapshot.PolicyConstraints, ConstraintCooldownActive) {
		t.Fatalf("expected probe-up cooldown constraint, got %#v", snapshot.PolicyConstraints)
	}
}

func TestDecidePolicy_CooldownCarriesForwardCurrentMaxQualityRung(t *testing.T) {
	now := time.Date(2026, 4, 18, 10, 0, 0, 0, time.UTC)
	decision := DecidePolicy(PolicyInput{
		CurrentMaxQualityRung: playbackprofile.RungCompatibleVideoH264CRF23,
	}, ConfidenceSnapshot{
		CooldownUntil:     now.Add(10 * time.Second),
		PolicyConstraints: []string{ConstraintCooldownActive},
	}, now)

	if decision.Action != PolicyCooldown {
		t.Fatalf("expected cooldown action, got %q", decision.Action)
	}
	if decision.MaxQualityRung != playbackprofile.RungCompatibleVideoH264CRF23 {
		t.Fatalf("expected current max quality rung to carry through cooldown, got %q", decision.MaxQualityRung)
	}
}

func TestEvaluateConfidence_LowConfidenceHoldsBeforeRecovery(t *testing.T) {
	now := time.Date(2026, 4, 18, 10, 0, 30, 0, time.UTC)
	prevSince := now.Add(-10 * time.Second)
	snapshot := EvaluateConfidence(ConfidenceSnapshot{
		Score:      -48,
		State:      ConfidenceLow,
		StateSince: prevSince,
	}, WindowFeatures{
		CleanPlayingMS:       10_000,
		HostPressureBand:     playbackprofile.HostPressureNormal,
		SourceTruthFreshness: "fresh",
	}, now)

	if snapshot.State != ConfidenceLow {
		t.Fatalf("expected low confidence to hold before recovery threshold, got %q", snapshot.State)
	}
	if snapshot.StateSince != prevSince {
		t.Fatalf("expected low confidence hold to preserve state since, got %v", snapshot.StateSince)
	}
}

func TestEvaluateConfidence_LowConfidenceAdvancesToRecoveryAfterHold(t *testing.T) {
	now := time.Date(2026, 4, 18, 10, 0, 30, 0, time.UTC)
	snapshot := EvaluateConfidence(ConfidenceSnapshot{
		Score:      -48,
		State:      ConfidenceLow,
		StateSince: now.Add(-21 * time.Second),
	}, WindowFeatures{
		CleanPlayingMS:       10_000,
		HostPressureBand:     playbackprofile.HostPressureNormal,
		SourceTruthFreshness: "fresh",
	}, now)

	if snapshot.State != ConfidenceRecovery {
		t.Fatalf("expected low confidence to advance to recovery after hold, got %q", snapshot.State)
	}
	if snapshot.StateSince != now {
		t.Fatalf("expected recovery transition to reset state since, got %v", snapshot.StateSince)
	}
}

func TestEvaluateConfidence_RecoveryAdvancesToStableAfterHold(t *testing.T) {
	now := time.Date(2026, 4, 18, 10, 1, 0, 0, time.UTC)
	snapshot := EvaluateConfidence(ConfidenceSnapshot{
		Score:      12,
		State:      ConfidenceRecovery,
		StateSince: now.Add(-21 * time.Second),
	}, WindowFeatures{
		RecoveryBuffer:          1,
		CleanPlayingMS:          10_000,
		HostPressureBand:        playbackprofile.HostPressureNormal,
		HostPerformanceClass:    "high",
		HostBenchmarkClass:      "strong",
		SourceBitrateConfidence: "high",
		SourceTruthFreshness:    "fresh",
	}, now)

	if snapshot.State != ConfidenceStable {
		t.Fatalf("expected recovery to advance to stable after hold, got %q", snapshot.State)
	}
	if snapshot.StateSince != now {
		t.Fatalf("expected stable transition to reset state since, got %v", snapshot.StateSince)
	}
}

func TestEvaluateConfidence_StableConfidenceHoldsBeforeHigh(t *testing.T) {
	now := time.Date(2026, 4, 18, 10, 2, 0, 0, time.UTC)
	prevSince := now.Add(-30 * time.Second)
	snapshot := EvaluateConfidence(ConfidenceSnapshot{
		Score:      48,
		State:      ConfidenceStable,
		StateSince: prevSince,
	}, WindowFeatures{
		CleanPlayingMS:          10_000,
		HostPressureBand:        playbackprofile.HostPressureNormal,
		HostPerformanceClass:    "high",
		HostBenchmarkClass:      "strong",
		SourceBitrateConfidence: "high",
		SourceTruthFreshness:    "fresh",
	}, now)

	if snapshot.State != ConfidenceStable {
		t.Fatalf("expected stable confidence to hold before high threshold, got %q", snapshot.State)
	}
	if snapshot.StateSince != prevSince {
		t.Fatalf("expected stable confidence hold to preserve state since, got %v", snapshot.StateSince)
	}
}

func TestEvaluateConfidence_StableConfidenceAdvancesToHighAfterHold(t *testing.T) {
	now := time.Date(2026, 4, 18, 10, 2, 0, 0, time.UTC)
	snapshot := EvaluateConfidence(ConfidenceSnapshot{
		Score:      48,
		State:      ConfidenceStable,
		StateSince: now.Add(-41 * time.Second),
	}, WindowFeatures{
		CleanPlayingMS:          10_000,
		HostPressureBand:        playbackprofile.HostPressureNormal,
		HostPerformanceClass:    "high",
		HostBenchmarkClass:      "strong",
		SourceBitrateConfidence: "high",
		SourceTruthFreshness:    "fresh",
	}, now)

	if snapshot.State != ConfidenceHigh {
		t.Fatalf("expected stable confidence to advance to high after hold, got %q", snapshot.State)
	}
	if snapshot.StateSince != now {
		t.Fatalf("expected high transition to reset state since, got %v", snapshot.StateSince)
	}
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
