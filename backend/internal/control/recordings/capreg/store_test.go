package capreg

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/stretchr/testify/require"
)

func TestHostSnapshotDecisionFingerprint_UsesStableHostClass(t *testing.T) {
	base := HostSnapshot{
		Identity: HostIdentity{
			Hostname:     "host-a",
			OSName:       "Linux",
			OSVersion:    "6.9 ",
			Architecture: "amd64",
		},
		EncoderCapabilities: []EncoderCapability{
			{Codec: "hevc", Verified: true, AutoEligible: false, ProbeElapsedMS: 51},
			{Codec: "h264", Verified: false, AutoEligible: true, ProbeElapsedMS: 12},
		},
		UpdatedAt: time.Unix(1_700_000_000, 0).UTC(),
	}

	changedRuntime := base
	changedRuntime.Identity.Hostname = "host-b"
	changedRuntime.UpdatedAt = time.Unix(1_800_000_000, 0).UTC()
	changedRuntime.EncoderCapabilities = []EncoderCapability{
		{Codec: "h264", Verified: true, AutoEligible: false, ProbeElapsedMS: 999},
		{Codec: "hevc", Verified: false, AutoEligible: true, ProbeElapsedMS: 1},
	}

	baseFingerprint := base.DecisionFingerprint()
	require.NotEmpty(t, baseFingerprint)
	require.Contains(t, baseFingerprint, "df4:")
	require.Equal(t, baseFingerprint, changedRuntime.DecisionFingerprint())
}

func TestHostSnapshotDecisionFingerprint_ChangesWhenHostClassChanges(t *testing.T) {
	base := HostSnapshot{
		Identity: HostIdentity{
			OSName:       "linux",
			OSVersion:    "6.9",
			Architecture: "amd64",
		},
		EncoderCapabilities: []EncoderCapability{
			{Codec: "hevc"},
			{Codec: "h264"},
		},
	}

	withAV1 := base
	withAV1.EncoderCapabilities = append(withAV1.EncoderCapabilities, EncoderCapability{Codec: "av1"})

	require.NotEqual(t, base.DecisionFingerprint(), withAV1.DecisionFingerprint())
}

func TestHostSnapshotDecisionFingerprint_ChangesWhenPerformanceClassChanges(t *testing.T) {
	base := HostSnapshot{
		Identity: HostIdentity{
			OSName:       "linux",
			OSVersion:    "6.9",
			Architecture: "amd64",
		},
		Runtime: playbackprofile.HostRuntimeSnapshot{
			PerformanceClass: "medium",
		},
		EncoderCapabilities: []EncoderCapability{
			{Codec: "h264"},
		},
	}

	upgraded := base
	upgraded.Runtime.PerformanceClass = "high"

	require.NotEqual(t, base.DecisionFingerprint(), upgraded.DecisionFingerprint())
}

func TestHostSnapshotDecisionFingerprint_ChangesWhenBenchmarkClassChanges(t *testing.T) {
	base := HostSnapshot{
		Identity: HostIdentity{
			OSName:       "linux",
			OSVersion:    "6.9",
			Architecture: "amd64",
		},
		Runtime: playbackprofile.HostRuntimeSnapshot{
			PerformanceClass: "high",
			Benchmark: playbackprofile.HostBenchmarkSnapshot{
				Class: "moderate",
			},
		},
		EncoderCapabilities: []EncoderCapability{
			{Codec: "h264"},
		},
	}

	upgraded := base
	upgraded.Runtime.Benchmark.Class = "strong"

	require.NotEqual(t, base.DecisionFingerprint(), upgraded.DecisionFingerprint())
}

func TestHostSnapshotDecisionFingerprint_ChangesWhenH264BenchmarkClassChanges(t *testing.T) {
	base := HostSnapshot{
		Identity: HostIdentity{
			OSName:       "linux",
			OSVersion:    "6.9",
			Architecture: "amd64",
		},
		Runtime: playbackprofile.HostRuntimeSnapshot{
			PerformanceClass: "high",
			Benchmark: playbackprofile.HostBenchmarkSnapshot{
				Class: "strong",
				Codecs: []playbackprofile.HostCodecBenchmark{
					{Codec: "h264", Class: "moderate"},
					{Codec: "hevc", Class: "strong"},
				},
			},
		},
		EncoderCapabilities: []EncoderCapability{
			{Codec: "h264"},
		},
	}

	upgraded := base
	upgraded.Runtime.Benchmark.Codecs = []playbackprofile.HostCodecBenchmark{
		{Codec: "h264", Class: "weak"},
		{Codec: "hevc", Class: "strong"},
	}

	require.NotEqual(t, base.DecisionFingerprint(), upgraded.DecisionFingerprint())
}

func TestHostSnapshotDecisionFingerprint_ChangesWhenH264ProfileBenchmarkClassChanges(t *testing.T) {
	base := HostSnapshot{
		Identity: HostIdentity{
			OSName:       "linux",
			OSVersion:    "6.9",
			Architecture: "amd64",
		},
		Runtime: playbackprofile.HostRuntimeSnapshot{
			PerformanceClass: "high",
			Benchmark: playbackprofile.HostBenchmarkSnapshot{
				Class: "strong",
				Codecs: []playbackprofile.HostCodecBenchmark{
					{Codec: "h264", Class: "strong"},
				},
				Profiles: []playbackprofile.HostProfileBenchmark{
					{ProfileID: playbackprofile.BenchmarkProfileVideoH2641080I, Class: "moderate"},
				},
			},
		},
		EncoderCapabilities: []EncoderCapability{
			{Codec: "h264"},
		},
	}

	upgraded := base
	upgraded.Runtime.Benchmark.Profiles = []playbackprofile.HostProfileBenchmark{
		{ProfileID: playbackprofile.BenchmarkProfileVideoH2641080I, Class: "weak"},
	}

	require.NotEqual(t, base.DecisionFingerprint(), upgraded.DecisionFingerprint())
}

func TestSourceSnapshotFingerprint_ChangesWhenBitrateConfidenceChanges(t *testing.T) {
	base := SourceSnapshot{
		SubjectKind:       "live",
		Origin:            "live_scan",
		Container:         "ts",
		VideoCodec:        "hevc",
		AudioCodec:        "ac3",
		BitrateConfidence: "low",
		BitrateBucket:     "9m_18m",
		Width:             3840,
		Height:            2160,
		FPS:               25,
		SignalFPS:         50,
		Interlaced:        true,
		ProblemFlags:      []string{"interlaced"},
		UpdatedAt:         time.Unix(1_700_000_000, 0).UTC(),
	}

	upgraded := base
	upgraded.BitrateConfidence = "high"
	upgraded.UpdatedAt = time.Unix(1_800_000_000, 0).UTC()

	require.NotEqual(t, base.Fingerprint(), upgraded.Fingerprint())
}

func TestSourceSnapshotFingerprint_ChangesWhenSignalFPSOrBitrateBucketChanges(t *testing.T) {
	base := SourceSnapshot{
		SubjectKind:       "live",
		Origin:            "live_scan",
		Container:         "ts",
		VideoCodec:        "hevc",
		AudioCodec:        "ac3",
		BitrateConfidence: "high",
		BitrateBucket:     "9m_18m",
		Width:             3840,
		Height:            2160,
		FPS:               25,
		SignalFPS:         50,
		Interlaced:        true,
		ProblemFlags:      []string{"interlaced"},
		UpdatedAt:         time.Unix(1_700_000_000, 0).UTC(),
	}

	withLowerSignalFPS := base
	withLowerSignalFPS.SignalFPS = 25

	withHigherBitrateBucket := base
	withHigherBitrateBucket.BitrateBucket = "18m_plus"

	require.NotEqual(t, base.Fingerprint(), withLowerSignalFPS.Fingerprint())
	require.NotEqual(t, base.Fingerprint(), withHigherBitrateBucket.Fingerprint())
}

func TestSummarizeFeedbackObservations_TreatsBlackRenderAsDecodeWarning(t *testing.T) {
	summary := summarizeFeedbackObservations([]PlaybackObservation{
		{
			ObservedAt:         time.Unix(1_700_000_200, 0).UTC(),
			ObservationKind:    "feedback",
			Outcome:            "warning",
			FeedbackEvent:      "info",
			FeedbackCode:       242,
			FeedbackMessage:    "black_suspect",
			SelectedVideoCodec: "av1",
		},
		{
			ObservedAt:      time.Unix(1_700_000_100, 0).UTC(),
			ObservationKind: "feedback",
			Outcome:         "started",
			FeedbackEvent:   "info",
			FeedbackCode:    200,
		},
	})

	require.Equal(t, 2, summary.SampleCount)
	require.Equal(t, 1, summary.WarningCount)
	require.Equal(t, 1, summary.ConsecutiveWarnings)
	require.Equal(t, 1, summary.ConsecutiveDecodeWarnings)
	require.Equal(t, 0, summary.ConsecutiveBufferWarnings)
	require.Equal(t, 0, summary.ConsecutiveNetworkWarnings)
}

func TestSummarizeFeedbackObservations_TreatsRenderStableAsRecoveryStart(t *testing.T) {
	summary := summarizeFeedbackObservations([]PlaybackObservation{
		{
			ObservedAt:      time.Unix(1_700_000_300, 0).UTC(),
			ObservationKind: "feedback",
			Outcome:         "warning",
			FeedbackEvent:   "warning",
			FeedbackCode:    101,
		},
		{
			ObservedAt:      time.Unix(1_700_000_200, 0).UTC(),
			ObservationKind: "feedback",
			Outcome:         "started",
			FeedbackEvent:   "info",
			FeedbackCode:    241,
		},
		{
			ObservedAt:      time.Unix(1_700_000_100, 0).UTC(),
			ObservationKind: "feedback",
			Outcome:         "started",
			FeedbackEvent:   "info",
			FeedbackCode:    221,
		},
	})

	require.Equal(t, 3, summary.SampleCount)
	require.Equal(t, 1, summary.WarningCount)
	require.Equal(t, 1, summary.ConsecutiveWarnings)
	require.Equal(t, 2, summary.PriorRecoveredStartStreak)
	require.Equal(t, 241, summary.PriorRecoveryStartCode)
}
