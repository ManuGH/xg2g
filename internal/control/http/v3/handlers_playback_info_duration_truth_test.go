package v3

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/hls"
	"github.com/ManuGH/xg2g/internal/pipeline/resume"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapPlaybackInfoV2_DurationTruthDTO(t *testing.T) {
	t.Parallel()

	srv := &Server{}
	dec := durationTruthDecision("req-duration-truth-1")

	rawTruth := playback.MediaTruth{
		Duration:           3600,
		DurationSource:     string(recordings.DurationTruthSourceMetadata),
		DurationConfidence: string(recordings.DurationTruthConfidenceHigh),
		DurationReasons: []string{
			string(recordings.DurationReasonFromSourceMetadata),
		},
		Container:  "ts",
		VideoCodec: "h264",
		AudioCodec: "aac",
	}

	info := srv.mapPlaybackInfoV2(context.Background(), "rec-1", dec, nil, nil, nil, false, rawTruth)

	require.NotNil(t, info.DurationMs)
	assert.Equal(t, int64(3_600_000), *info.DurationMs)
	require.NotNil(t, info.DurationSeconds)
	assert.Equal(t, int64(3600), *info.DurationSeconds)
	require.NotNil(t, info.DurationSource)
	assert.Equal(t, PlaybackInfoDurationSourceSourceMetadata, *info.DurationSource)
	require.NotNil(t, info.DurationConfidence)
	assert.Equal(t, High, *info.DurationConfidence)
	require.NotNil(t, info.DurationReasons)
	assert.Contains(t, *info.DurationReasons, DurationFromSourceMetadata)
	require.NotNil(t, info.IsSeekable)
	assert.True(t, *info.IsSeekable)
}

func TestMapPlaybackInfoV2_UnknownDurationDisablesSeek(t *testing.T) {
	t.Parallel()

	srv := &Server{}
	dec := durationTruthDecision("req-duration-truth-2")

	rawTruth := playback.MediaTruth{
		Container:  "ts",
		VideoCodec: "h264",
		AudioCodec: "aac",
	}

	info := srv.mapPlaybackInfoV2(context.Background(), "rec-2", dec, nil, nil, nil, false, rawTruth)

	assert.Nil(t, info.DurationMs)
	assert.Nil(t, info.DurationSeconds)
	require.NotNil(t, info.DurationReasons)
	assert.Contains(t, *info.DurationReasons, DurationUnknownDeniedSeek)
	require.NotNil(t, info.IsSeekable)
	assert.False(t, *info.IsSeekable)
	require.NotNil(t, info.Seekable)
	assert.False(t, *info.Seekable)
}

func TestMapPlaybackInfoV2_ResumeClampAddsReason(t *testing.T) {
	t.Parallel()

	srv := &Server{}
	dec := durationTruthDecision("req-duration-truth-3")
	resumeState := &resume.State{
		PosSeconds:      5000,
		DurationSeconds: 0,
		Finished:        false,
	}

	rawTruth := playback.MediaTruth{
		Duration:           3600,
		DurationSource:     string(recordings.DurationTruthSourceMetadata),
		DurationConfidence: string(recordings.DurationTruthConfidenceHigh),
		DurationReasons: []string{
			string(recordings.DurationReasonFromSourceMetadata),
		},
		Container:  "ts",
		VideoCodec: "h264",
		AudioCodec: "aac",
	}

	info := srv.mapPlaybackInfoV2(context.Background(), "rec-3", dec, nil, resumeState, nil, false, rawTruth)

	require.NotNil(t, info.Resume)
	assert.Equal(t, int64(3600), info.Resume.PosSeconds)
	require.NotNil(t, info.Resume.DurationSeconds)
	assert.Equal(t, int64(3600), *info.Resume.DurationSeconds)
	require.NotNil(t, info.DurationReasons)
	assert.Contains(t, *info.DurationReasons, ResumeClampedToDuration)
}

func TestMapPlaybackInfoV2_HeuristicDurationLowConfidenceDisablesSeek(t *testing.T) {
	t.Parallel()

	srv := &Server{}
	dec := durationTruthDecision("req-duration-truth-4")
	segmentTruth := &hls.SegmentTruth{
		TotalDuration: 120 * time.Second,
		IsVOD:         true,
	}

	rawTruth := playback.MediaTruth{
		Container:  "ts",
		VideoCodec: "h264",
		AudioCodec: "aac",
	}

	info := srv.mapPlaybackInfoV2(context.Background(), "rec-4", dec, nil, nil, segmentTruth, true, rawTruth)

	require.NotNil(t, info.DurationMs)
	assert.Equal(t, int64(120_000), *info.DurationMs)
	require.NotNil(t, info.DurationSource)
	assert.Equal(t, PlaybackInfoDurationSourceHeuristic, *info.DurationSource)
	require.NotNil(t, info.DurationConfidence)
	assert.Equal(t, Low, *info.DurationConfidence)
	require.NotNil(t, info.IsSeekable)
	assert.False(t, *info.IsSeekable)
}

func durationTruthDecision(requestID string) *decision.Decision {
	return &decision.Decision{
		Mode: decision.ModeDirectStream,
		Selected: decision.SelectedFormats{
			Container:  "ts",
			VideoCodec: "h264",
			AudioCodec: "aac",
		},
		Outputs: []decision.Output{
			{Kind: "hls", URL: "placeholder://recording"},
		},
		Reasons:            []decision.ReasonCode{decision.ReasonDirectStreamMatch},
		Trace:              decision.Trace{RequestID: requestID},
		SelectedOutputURL:  "placeholder://recording",
		SelectedOutputKind: "hls",
	}
}
