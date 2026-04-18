package recordings

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/control/recordings/capreg"
	"github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

func TestFeedbackClampMaxQualityRung_LowBitrateConfidenceDelaysGenericFailureClamp(t *testing.T) {
	truth := playback.MediaTruth{BitrateConfidence: "low"}

	if got := feedbackClampMaxQualityRung(capreg.FeedbackSummary{ConsecutiveFailures: 2}, truth); got != playbackprofile.RungUnknown {
		t.Fatalf("expected no clamp after two generic failures on low-confidence truth, got %q", got)
	}
	if got := feedbackClampMaxQualityRung(capreg.FeedbackSummary{ConsecutiveFailures: 3}, truth); got != playbackprofile.RungCompatibleVideoH264CRF23 {
		t.Fatalf("expected compatible clamp after three generic failures on low-confidence truth, got %q", got)
	}
	if got := feedbackClampMaxQualityRung(capreg.FeedbackSummary{ConsecutiveFailures: 4}, truth); got != playbackprofile.RungRepairH264AAC {
		t.Fatalf("expected repair clamp after four generic failures on low-confidence truth, got %q", got)
	}
}

func TestFeedbackClampMaxQualityRung_LowBitrateConfidenceKeepsSpecificFailureClamps(t *testing.T) {
	truth := playback.MediaTruth{BitrateConfidence: "low"}

	if got := feedbackClampMaxQualityRung(capreg.FeedbackSummary{
		ConsecutiveFailures:       2,
		ConsecutiveDecodeFailures: 2,
	}, truth); got != playbackprofile.RungCompatibleVideoH264CRF23 {
		t.Fatalf("expected decode failures to keep compatible clamp on low-confidence truth, got %q", got)
	}
	if got := feedbackClampMaxQualityRung(capreg.FeedbackSummary{
		ConsecutiveFailures:      2,
		ConsecutiveStallFailures: 2,
	}, truth); got != playbackprofile.RungRepairH264AAC {
		t.Fatalf("expected stall failures to keep repair clamp on low-confidence truth, got %q", got)
	}
}

func TestFeedbackClampMaxQualityRung_PriorHealthyStartsDelayGenericFailureClamp(t *testing.T) {
	truth := playback.MediaTruth{BitrateConfidence: "high"}

	if got := feedbackClampMaxQualityRung(capreg.FeedbackSummary{
		ConsecutiveFailures: 2,
		PriorStartedStreak:  2,
	}, truth); got != playbackprofile.RungUnknown {
		t.Fatalf("expected no clamp after two generic failures when preceded by a healthy start streak, got %q", got)
	}
	if got := feedbackClampMaxQualityRung(capreg.FeedbackSummary{
		ConsecutiveFailures: 3,
		PriorStartedStreak:  2,
	}, truth); got != playbackprofile.RungCompatibleVideoH264CRF23 {
		t.Fatalf("expected compatible clamp after three generic failures when preceded by a healthy start streak, got %q", got)
	}
	if got := feedbackClampMaxQualityRung(capreg.FeedbackSummary{
		ConsecutiveFailures: 4,
		PriorStartedStreak:  2,
	}, truth); got != playbackprofile.RungRepairH264AAC {
		t.Fatalf("expected repair clamp after four generic failures when preceded by a healthy start streak, got %q", got)
	}
}

func TestFeedbackClampMaxQualityRung_RepeatedBufferWarningsClampToCompatible(t *testing.T) {
	truth := playback.MediaTruth{BitrateConfidence: "high"}

	if got := feedbackClampMaxQualityRung(capreg.FeedbackSummary{
		ConsecutiveWarnings:       2,
		ConsecutiveBufferWarnings: 2,
	}, truth); got != playbackprofile.RungUnknown {
		t.Fatalf("expected no clamp for two buffer warnings, got %q", got)
	}
	if got := feedbackClampMaxQualityRung(capreg.FeedbackSummary{
		ConsecutiveWarnings:       3,
		ConsecutiveBufferWarnings: 3,
	}, truth); got != playbackprofile.RungCompatibleVideoH264CRF23 {
		t.Fatalf("expected compatible clamp for three buffer warnings, got %q", got)
	}
}

func TestFeedbackClampMaxQualityRung_RepeatedDecodeWarningsClampToCompatible(t *testing.T) {
	truth := playback.MediaTruth{BitrateConfidence: "high"}

	if got := feedbackClampMaxQualityRung(capreg.FeedbackSummary{
		ConsecutiveWarnings:       1,
		ConsecutiveDecodeWarnings: 1,
	}, truth); got != playbackprofile.RungUnknown {
		t.Fatalf("expected no clamp for one decode warning, got %q", got)
	}
	if got := feedbackClampMaxQualityRung(capreg.FeedbackSummary{
		ConsecutiveWarnings:       2,
		ConsecutiveDecodeWarnings: 2,
	}, truth); got != playbackprofile.RungCompatibleVideoH264CRF23 {
		t.Fatalf("expected compatible clamp for two decode warnings, got %q", got)
	}
}

func TestFeedbackClampMaxQualityRung_RepeatedNetworkWarningsClampToCompatible(t *testing.T) {
	truth := playback.MediaTruth{BitrateConfidence: "high"}

	if got := feedbackClampMaxQualityRung(capreg.FeedbackSummary{
		ConsecutiveWarnings:        2,
		ConsecutiveNetworkWarnings: 2,
	}, truth); got != playbackprofile.RungUnknown {
		t.Fatalf("expected no clamp for two network warnings, got %q", got)
	}
	if got := feedbackClampMaxQualityRung(capreg.FeedbackSummary{
		ConsecutiveWarnings:        3,
		ConsecutiveNetworkWarnings: 3,
	}, truth); got != playbackprofile.RungCompatibleVideoH264CRF23 {
		t.Fatalf("expected compatible clamp for three network warnings, got %q", got)
	}
}

func TestFeedbackClampMaxQualityRung_PriorRecoveryDelaysSoftWarningClamp(t *testing.T) {
	truth := playback.MediaTruth{BitrateConfidence: "high"}

	if got := feedbackClampMaxQualityRung(capreg.FeedbackSummary{
		ConsecutiveWarnings:       3,
		ConsecutiveBufferWarnings: 3,
		PriorRecoveredStartStreak: 1,
		PriorRecoveryStartCode:    211,
	}, truth); got != playbackprofile.RungUnknown {
		t.Fatalf("expected no clamp for three buffer warnings after a recent recovery start, got %q", got)
	}
	if got := feedbackClampMaxQualityRung(capreg.FeedbackSummary{
		ConsecutiveWarnings:       4,
		ConsecutiveBufferWarnings: 4,
		PriorRecoveredStartStreak: 1,
		PriorRecoveryStartCode:    211,
	}, truth); got != playbackprofile.RungCompatibleVideoH264CRF23 {
		t.Fatalf("expected compatible clamp for four buffer warnings after a recent recovery start, got %q", got)
	}
	if got := feedbackClampMaxQualityRung(capreg.FeedbackSummary{
		ConsecutiveWarnings:       3,
		ConsecutiveBufferWarnings: 3,
		PriorRecoveredStartStreak: 1,
		PriorRecoveryStartCode:    212,
	}, truth); got != playbackprofile.RungCompatibleVideoH264CRF23 {
		t.Fatalf("expected mismatched recovery type to keep the normal buffer clamp, got %q", got)
	}
}

func TestFeedbackClampMaxQualityRung_MatchingRecoveryTrustBuildsFurtherBufferHeadroom(t *testing.T) {
	truth := playback.MediaTruth{BitrateConfidence: "high"}

	if got := feedbackClampMaxQualityRung(capreg.FeedbackSummary{
		ConsecutiveWarnings:       4,
		ConsecutiveBufferWarnings: 4,
		PriorRecoveredStartStreak: 2,
		PriorRecoveryStartCode:    211,
	}, truth); got != playbackprofile.RungUnknown {
		t.Fatalf("expected no clamp for four buffer warnings after two matching recovery starts, got %q", got)
	}
	if got := feedbackClampMaxQualityRung(capreg.FeedbackSummary{
		ConsecutiveWarnings:       5,
		ConsecutiveBufferWarnings: 5,
		PriorRecoveredStartStreak: 2,
		PriorRecoveryStartCode:    211,
	}, truth); got != playbackprofile.RungCompatibleVideoH264CRF23 {
		t.Fatalf("expected compatible clamp for five buffer warnings after two matching recovery starts, got %q", got)
	}
}

func TestBuildPlaybackConfidenceWindowsFromObservations_AllocatesCleanPlaybackAcrossWindows(t *testing.T) {
	now := time.Date(2026, 4, 18, 18, 0, 0, 0, time.UTC)
	base := runtimepolicy.WindowFeatures{
		WindowKind:              "live",
		HostPressureBand:        playbackprofile.HostPressureNormal,
		HostPerformanceClass:    "high",
		HostBenchmarkClass:      "strong",
		SourceBitrateConfidence: "high",
		SourceTruthFreshness:    "fresh",
	}

	windows := buildPlaybackConfidenceWindowsFromObservations([]capreg.PlaybackObservation{
		{
			ObservedAt:   now.Add(-35 * time.Second),
			Outcome:      "started",
			FeedbackCode: 200,
		},
		{
			ObservedAt:   now.Add(-5 * time.Second),
			Outcome:      "warning",
			FeedbackCode: 101,
		},
	}, base, now)

	var totalClean int64
	var bufferWarnings int
	for _, window := range windows {
		totalClean += window.CleanPlayingMS
		bufferWarnings += window.BufferWarnings
	}

	if totalClean < 29_000 || totalClean > 31_000 {
		t.Fatalf("expected about 30s of clean playback, got %dms", totalClean)
	}
	if bufferWarnings != 1 {
		t.Fatalf("expected one buffered warning across windows, got %d", bufferWarnings)
	}
}

func TestBuildPlaybackConfidenceWindowsFromObservations_ProbeConfirmationKeepsCleanPlayback(t *testing.T) {
	now := time.Date(2026, 4, 18, 18, 0, 0, 0, time.UTC)
	base := runtimepolicy.WindowFeatures{
		WindowKind:              "live",
		HostPressureBand:        playbackprofile.HostPressureNormal,
		HostPerformanceClass:    "high",
		HostBenchmarkClass:      "strong",
		SourceBitrateConfidence: "high",
		SourceTruthFreshness:    "fresh",
	}

	windows := buildPlaybackConfidenceWindowsFromObservations([]capreg.PlaybackObservation{
		{
			ObservedAt:   now.Add(-25 * time.Second),
			Outcome:      "started",
			FeedbackCode: 220,
		},
		{
			ObservedAt:   now.Add(-15 * time.Second),
			Outcome:      "started",
			FeedbackCode: 221,
		},
		{
			ObservedAt:   now.Add(-5 * time.Second),
			Outcome:      "warning",
			FeedbackCode: 101,
		},
	}, base, now)

	var (
		totalClean     int64
		probeStarted   int
		probeConfirmed int
		probeRegressed int
		bufferWarnings int
	)
	for _, window := range windows {
		totalClean += window.CleanPlayingMS
		probeStarted += window.ProbeWindowStarted
		probeConfirmed += window.ProbeWindowConfirmed
		probeRegressed += window.ProbeWindowRegressed
		bufferWarnings += window.BufferWarnings
	}

	if totalClean < 19_000 || totalClean > 21_000 {
		t.Fatalf("expected about 20s of clean playback across the probe window, got %dms", totalClean)
	}
	if probeStarted != 1 || probeConfirmed != 1 {
		t.Fatalf("expected one started and one confirmed probe window, got started=%d confirmed=%d", probeStarted, probeConfirmed)
	}
	if probeRegressed != 0 {
		t.Fatalf("expected no probe regression after confirmation, got %d", probeRegressed)
	}
	if bufferWarnings != 1 {
		t.Fatalf("expected one buffer warning after the confirmed probe, got %d", bufferWarnings)
	}
}

func TestBuildPlaybackConfidenceWindowsFromObservations_ProbeRegressionCountsBeforeConfirm(t *testing.T) {
	now := time.Date(2026, 4, 18, 18, 0, 0, 0, time.UTC)
	base := runtimepolicy.WindowFeatures{
		WindowKind:              "live",
		HostPressureBand:        playbackprofile.HostPressureNormal,
		HostPerformanceClass:    "high",
		HostBenchmarkClass:      "strong",
		SourceBitrateConfidence: "high",
		SourceTruthFreshness:    "fresh",
	}

	windows := buildPlaybackConfidenceWindowsFromObservations([]capreg.PlaybackObservation{
		{
			ObservedAt:   now.Add(-15 * time.Second),
			Outcome:      "started",
			FeedbackCode: 220,
		},
		{
			ObservedAt:   now.Add(-10 * time.Second),
			Outcome:      "warning",
			FeedbackCode: 104,
		},
	}, base, now)

	var (
		probeStarted   int
		probeConfirmed int
		probeRegressed int
		networkWarns   int
	)
	for _, window := range windows {
		probeStarted += window.ProbeWindowStarted
		probeConfirmed += window.ProbeWindowConfirmed
		probeRegressed += window.ProbeWindowRegressed
		networkWarns += window.NetworkWarnings
	}

	if probeStarted != 1 {
		t.Fatalf("expected one started probe window, got %d", probeStarted)
	}
	if probeConfirmed != 0 {
		t.Fatalf("expected no confirmed probe window before the warning, got %d", probeConfirmed)
	}
	if probeRegressed != 1 {
		t.Fatalf("expected one regressed probe window, got %d", probeRegressed)
	}
	if networkWarns != 1 {
		t.Fatalf("expected one network warning during the regressed probe, got %d", networkWarns)
	}
}
