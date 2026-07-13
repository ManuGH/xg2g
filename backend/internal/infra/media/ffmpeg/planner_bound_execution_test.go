package ffmpeg

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/domain/vod"
)

func TestFinalizePlanDoesNotMutatePlannerBoundProfile(t *testing.T) {
	t.Setenv("XG2G_SAFARI_FORCE_COPY_SERVICE_REFS", "1:0:19:11:6:85:C00000:0:0:0:")
	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)
	adapter.streamProbeFn = func(context.Context, string) (*vod.StreamInfo, error) {
		t.Fatal("planner-bound profiles must not be probed or re-planned")
		return nil, nil
	}
	profile := model.ProfileSpec{
		Name:             "safari",
		PlannerBound:     true,
		TranscodeVideo:   true,
		VideoCodec:       "libx264",
		AudioMode:        "transcode",
		AudioCodec:       "aac",
		Container:        "fmp4",
		VideoTargetRateK: 8000,
		VideoMaxRateK:    16000,
		VideoBufSizeK:    32000,
	}
	spec := ports.StreamSpec{
		SessionID: "planner-bound",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Profile:   profile,
		Source: ports.StreamSource{
			ID:   "1:0:19:11:6:85:C00000:0:0:0:",
			Type: ports.SourceTuner,
		},
	}

	finalized := adapter.FinalizePlan(context.Background(), spec, spec.Source.ID)
	require.Equal(t, profile.TranscodeVideo, finalized.Profile.TranscodeVideo)
	require.Equal(t, profile.VideoCodec, finalized.Profile.VideoCodec)
	require.Equal(t, profile.AudioMode, finalized.Profile.AudioMode)
	require.Equal(t, profile.Container, finalized.Profile.Container)
	require.Equal(t, profile.VideoTargetRateK, finalized.Profile.VideoTargetRateK)
	require.Equal(t, profile.VideoMaxRateK, finalized.Profile.VideoMaxRateK)
}

func TestPlannerBoundRateControlPreservesTargetAndMaximum(t *testing.T) {
	profile := model.ProfileSpec{
		PlannerBound:     true,
		TranscodeVideo:   true,
		VideoTargetRateK: 8000,
		VideoMaxRateK:    16000,
		VideoBufSizeK:    32000,
	}

	vaapi := appendVaapiRateControlArgs(nil, profile, "av1", LoadAdapterConfig("", ""))
	value, ok := valueAfter(vaapi, "-b:v")
	require.True(t, ok)
	require.Equal(t, "8000k", value)
	value, ok = valueAfter(vaapi, "-maxrate")
	require.True(t, ok)
	require.Equal(t, "16000k", value)

	nvenc := appendNVENCRateControlArgs(nil, profile)
	value, ok = valueAfter(nvenc, "-b:v")
	require.True(t, ok)
	require.Equal(t, "8000k", value)
	value, ok = valueAfter(nvenc, "-maxrate")
	require.True(t, ok)
	require.Equal(t, "16000k", value)

	adapter := &LocalAdapter{Logger: zerolog.Nop()}
	cpu := adapter.buildCPUVideoArgs(nil, ports.StreamSpec{Profile: profile}, "h264", 50, 2)
	value, ok = valueAfter(cpu, "-b:v")
	require.True(t, ok)
	require.Equal(t, "8000k", value)
	require.NotContains(t, cpu, "-crf")
}

func TestPlannerBoundUnsetTargetPreservesHEVCQualityModeAndCeiling(t *testing.T) {
	profile := model.ProfileSpec{
		PlannerBound:   true,
		TranscodeVideo: true,
		VideoCodec:     "hevc",
		VideoQP:        20,
		VideoMaxRateK:  5000,
		VideoBufSizeK:  10000,
	}

	args := appendVaapiRateControlArgs(nil, profile, "hevc", LoadAdapterConfig("", ""))
	value, ok := valueAfter(args, "-rc_mode")
	require.True(t, ok)
	require.Equal(t, "CQP", value)
	value, ok = valueAfter(args, "-qp")
	require.True(t, ok)
	require.Equal(t, "20", value)
	_, ok = valueAfter(args, "-b:v")
	require.False(t, ok, "unset planner target must not replace CQP with bitrate mode")
	value, ok = valueAfter(args, "-maxrate")
	require.True(t, ok)
	require.Equal(t, "5000k", value)
	value, ok = valueAfter(args, "-bufsize")
	require.True(t, ok)
	require.Equal(t, "10000k", value)
}

func TestPlannerBoundScaleWidthReachesEveryEncoderPath(t *testing.T) {
	adapter := &LocalAdapter{Logger: zerolog.Nop()}
	spec := ports.StreamSpec{Profile: model.ProfileSpec{
		PlannerBound:   true,
		TranscodeVideo: true,
		VideoMaxWidth:  640,
	}}

	fullVAAPI := adapter.buildVaapiVideoArgs(nil, spec, "h264", 50, 2)
	filter, ok := valueAfter(fullVAAPI, "-vf")
	require.True(t, ok)
	require.Equal(t, "scale_vaapi=w=640:h=-2:format=nv12:out_color_matrix=bt709:out_color_primaries=bt709:out_color_transfer=bt709", filter)
	require.NotContains(t, fullVAAPI, "-color_primaries")

	encodeOnlyVAAPI := adapter.buildVaapiEncodeOnlyVideoArgs(nil, spec, "h264", 50, 2)
	filter, ok = valueAfter(encodeOnlyVAAPI, "-vf")
	require.True(t, ok)
	require.Equal(t, "scale=w=640:h=-2:flags=lanczos,format=nv12,hwupload", filter)

	nvenc := adapter.buildNVENCVideoArgs(nil, spec, "h264", 50, 2)
	filter, ok = valueAfter(nvenc, "-vf")
	require.True(t, ok)
	require.Equal(t, "scale=w=640:h=-2:flags=lanczos", filter)

	cpu := adapter.buildCPUVideoArgs(nil, spec, "h264", 50, 2)
	filter, ok = valueAfter(cpu, "-vf")
	require.True(t, ok)
	require.Equal(t, "scale=w=640:h=-2:flags=lanczos", filter)
}
