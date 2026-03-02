package recordings

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveDurationTruth_DeterministicPriority(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		in              DurationTruthResolveInput
		wantSource      DurationTruthSource
		wantConfidence  DurationTruthConfidence
		wantDurationMs  *int64
		wantHasReason   DurationReasonCode
		wantPrimaryCode DurationReasonCode
	}{
		{
			name: "primary_metadata_wins",
			in: DurationTruthResolveInput{
				PrimaryDurationSeconds:   3600,
				SecondaryDurationSeconds: 1800,
				SecondarySource:          DurationTruthSourceFFProbe,
				AllowHeuristic:           true,
				HeuristicDurationSeconds: 1000,
			},
			wantSource:      DurationTruthSourceMetadata,
			wantConfidence:  DurationTruthConfidenceHigh,
			wantDurationMs:  int64Ptr(3_600_000),
			wantHasReason:   DurationReasonFromSourceMetadata,
			wantPrimaryCode: DurationReasonFromSourceMetadata,
		},
		{
			name: "secondary_ffprobe_when_primary_missing",
			in: DurationTruthResolveInput{
				PrimaryDurationSeconds:   0,
				SecondaryDurationSeconds: 2400,
				SecondarySource:          DurationTruthSourceFFProbe,
				AllowHeuristic:           true,
				HeuristicDurationSeconds: 3000,
			},
			wantSource:      DurationTruthSourceFFProbe,
			wantConfidence:  DurationTruthConfidenceMedium,
			wantDurationMs:  int64Ptr(2_400_000),
			wantHasReason:   DurationReasonFromFFProbe,
			wantPrimaryCode: DurationReasonPrimaryMissing,
		},
		{
			name: "heuristic_used_after_probe_failure",
			in: DurationTruthResolveInput{
				PrimaryDurationSeconds:   0,
				SecondaryDurationSeconds: 0,
				SecondarySource:          DurationTruthSourceFFProbe,
				SecondaryFailed:          true,
				AllowHeuristic:           true,
				HeuristicDurationSeconds: 1200,
			},
			wantSource:      DurationTruthSourceHeuristic,
			wantConfidence:  DurationTruthConfidenceLow,
			wantDurationMs:  int64Ptr(1_200_000),
			wantHasReason:   DurationReasonFromHeuristic,
			wantPrimaryCode: DurationReasonProbeFailed,
		},
		{
			name: "unknown_when_all_paths_fail",
			in: DurationTruthResolveInput{
				PrimaryDurationSeconds:   -1,
				SecondaryDurationSeconds: 0,
				SecondarySource:          DurationTruthSourceFFProbe,
				SecondaryFailed:          true,
				AllowHeuristic:           true,
				HeuristicDurationSeconds: 0,
			},
			wantSource:      DurationTruthSourceUnknown,
			wantConfidence:  DurationTruthConfidenceLow,
			wantDurationMs:  nil,
			wantHasReason:   DurationReasonUnknownDeniedSeek,
			wantPrimaryCode: DurationReasonUnknownDeniedSeek,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveDurationTruth(tc.in)
			assert.Equal(t, tc.wantSource, got.Source)
			assert.Equal(t, tc.wantConfidence, got.Confidence)
			assert.Equal(t, tc.wantDurationMs, got.DurationMs)
			assert.Contains(t, got.Reasons, tc.wantHasReason)
			assert.Equal(t, tc.wantPrimaryCode, DurationReasonPrimaryFrom(got.Reasons))
		})
	}
}

func TestResolveDurationTruth_ClampAndSanity(t *testing.T) {
	t.Parallel()

	t.Run("primary_negative_ignored", func(t *testing.T) {
		got := ResolveDurationTruth(DurationTruthResolveInput{
			PrimaryDurationSeconds:   -10,
			SecondaryDurationSeconds: 100,
			SecondarySource:          DurationTruthSourceContainer,
		})
		require.NotNil(t, got.DurationMs)
		assert.Equal(t, int64(100_000), *got.DurationMs)
		assert.Equal(t, DurationTruthSourceContainer, got.Source)
		assert.Contains(t, got.Reasons, DurationReasonPrimaryMissing)
	})

	t.Run("overflow_clamped", func(t *testing.T) {
		got := ResolveDurationTruth(DurationTruthResolveInput{
			PrimaryDurationSeconds: math.MaxInt64/1000 + 1,
		})
		require.NotNil(t, got.DurationMs)
		assert.Equal(t, int64(30*24*60*60*1000), *got.DurationMs)
		assert.Contains(t, got.Reasons, DurationReasonInconsistentClamp)
	})

	t.Run("duration_seconds_helper", func(t *testing.T) {
		ms := int64(90_000)
		truth := DurationTruth{DurationMs: &ms}
		sec := truth.DurationSeconds()
		require.NotNil(t, sec)
		assert.Equal(t, int64(90), *sec)
	})
}

func int64Ptr(v int64) *int64 {
	return &v
}
