package ffmpeg

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/domain/vod"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFinalizePlan_SafariForceCopyAllowlistUsesFMP4CopyMode(t *testing.T) {
	t.Setenv("XG2G_SAFARI_FORCE_COPY_SERVICE_REFS", "1:0:19:11:6:85:C00000:0:0:0:")

	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)
	adapter.streamProbeFn = func(ctx context.Context, inputURL string) (*vod.StreamInfo, error) {
		t.Fatal("force-copy allowlist should bypass the runtime probe")
		return nil, nil
	}

	spec := ports.StreamSpec{
		SessionID: "finalize-safari-force-copy",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityStandard,
		Profile: model.ProfileSpec{
			Name:           "safari",
			TranscodeVideo: true,
			Container:      "fmp4",
			AudioBitrateK:  192,
		},
		Source: ports.StreamSource{
			ID:   "1:0:19:11:6:85:C00000:0:0:0:",
			Type: ports.SourceTuner,
		},
	}

	finalized := adapter.FinalizePlan(context.Background(), spec, "http://127.0.0.1:17999/1:0:19:11:6:85:C00000:0:0:0:")
	assert.False(t, finalized.Profile.TranscodeVideo)
	assert.Equal(t, "h264", finalized.Profile.VideoCodec)
	assert.Equal(t, "fmp4", finalized.Profile.Container)
	assert.Equal(t, ports.RuntimeModeCopy, finalized.Profile.EffectiveRuntimeMode)
	assert.Equal(t, ports.RuntimeModeSourceEnvOverride, finalized.Profile.EffectiveModeSource)
}

func TestFinalizePlan_SafariHQAllowlistOverridesCopyAllowlist(t *testing.T) {
	t.Setenv("XG2G_SAFARI_FORCE_COPY_SERVICE_REFS", "1:0:19:132F:3EF:1:C00000:0:0:0:")
	t.Setenv("XG2G_SAFARI_HQ_SERVICE_REFS", "1:0:19:132F:3EF:1:C00000:0:0:0:")

	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)
	adapter.streamProbeFn = func(ctx context.Context, inputURL string) (*vod.StreamInfo, error) {
		t.Fatal("service-ref allowlists should not depend on the runtime probe")
		return nil, nil
	}

	spec := ports.StreamSpec{
		SessionID: "finalize-safari-hq-precedence",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityStandard,
		Profile: model.ProfileSpec{
			Name:           "safari",
			PolicyModeHint: ports.RuntimeModeCopy,
			TranscodeVideo: true,
			Container:      "fmp4",
			AudioBitrateK:  192,
		},
		Source: ports.StreamSource{
			ID:   "1:0:19:132F:3EF:1:C00000:0:0:0:",
			Type: ports.SourceTuner,
		},
	}

	finalized := adapter.FinalizePlan(context.Background(), spec, "http://127.0.0.1:17999/1:0:19:132F:3EF:1:C00000:0:0:0:")
	assert.Equal(t, profiles.ProfileSafariRuntimeHQ, finalized.Profile.Name)
	assert.True(t, finalized.Profile.TranscodeVideo)
	assert.Equal(t, "libx264", finalized.Profile.VideoCodec)
	assert.Equal(t, "mpegts", finalized.Profile.Container)
	assert.Equal(t, ports.RuntimeModeHQ50, finalized.Profile.EffectiveRuntimeMode)
	assert.Equal(t, ports.RuntimeModeSourceEnvOverride, finalized.Profile.EffectiveModeSource)
}

func TestFinalizePlan_HQ50ServiceRefOverridesAV1HQ25Runtime(t *testing.T) {
	t.Setenv("XG2G_SAFARI_HQ50_SERVICE_REFS", "1:0:19:91:4:85:C00000:0:0:0:")

	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)
	adapter.streamProbeFn = func(ctx context.Context, inputURL string) (*vod.StreamInfo, error) {
		return &vod.StreamInfo{
			Container: "ts",
			Video: vod.VideoStreamInfo{
				CodecName:  "h264",
				Interlaced: true,
			},
		}, nil
	}

	spec := ports.StreamSpec{
		SessionID: "finalize-av1-hq50-service-ref",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityHigh,
		Profile: model.ProfileSpec{
			Name:           "av1_hw",
			PolicyModeHint: ports.RuntimeModeHQ25,
			TranscodeVideo: true,
			HWAccel:        "vaapi",
			VideoCodec:     "av1",
			Deinterlace:    true,
			Container:      "fmp4",
			VideoMaxRateK:  6000,
			VideoBufSizeK:  12000,
			AudioBitrateK:  192,
		},
		Source: ports.StreamSource{
			ID:   "1:0:19:91:4:85:C00000:0:0:0:",
			Type: ports.SourceTuner,
		},
	}

	finalized := adapter.FinalizePlan(context.Background(), spec, "http://127.0.0.1:17999/1:0:19:91:4:85:C00000:0:0:0:")
	assert.Equal(t, "av1_hw", finalized.Profile.Name)
	assert.Equal(t, "av1", finalized.Profile.VideoCodec)
	assert.True(t, finalized.Profile.TranscodeVideo)
	assert.True(t, finalized.Profile.Deinterlace)
	assert.False(t, finalized.Profile.ForceSafariHQ25)
	assert.Equal(t, ports.RuntimeModeHQ50, finalized.Profile.PolicyModeHint)
	assert.Equal(t, ports.RuntimeModeHQ50, finalized.Profile.EffectiveRuntimeMode)
	assert.Equal(t, ports.RuntimeModeSourceEnvOverride, finalized.Profile.EffectiveModeSource)
	assert.Equal(t, 12000, finalized.Profile.VideoMaxRateK)
	assert.Equal(t, 24000, finalized.Profile.VideoBufSizeK)
}

func TestFinalizePlan_HQ50ServiceRefHonorsSharpnessBudgetEnv(t *testing.T) {
	t.Setenv("XG2G_SAFARI_HQ50_SERVICE_REFS", "1:0:19:91:4:85:C00000:0:0:0:")
	t.Setenv("XG2G_SAFARI_HQ50_MAXRATE_K", "9000")
	t.Setenv("XG2G_SAFARI_HQ50_BUFSIZE_K", "18000")

	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)

	spec := ports.StreamSpec{
		SessionID: "finalize-av1-hq50-service-ref-sharpness-env",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityHigh,
		Profile: model.ProfileSpec{
			Name:           "av1_hw",
			PolicyModeHint: ports.RuntimeModeHQ25,
			TranscodeVideo: true,
			HWAccel:        "vaapi",
			VideoCodec:     "av1",
			Deinterlace:    true,
			Container:      "fmp4",
			VideoMaxRateK:  6000,
			VideoBufSizeK:  12000,
			AudioBitrateK:  192,
		},
		Source: ports.StreamSource{
			ID:   "1:0:19:91:4:85:C00000:0:0:0:",
			Type: ports.SourceTuner,
		},
	}

	finalized := adapter.FinalizePlan(context.Background(), spec, "http://127.0.0.1:17999/1:0:19:91:4:85:C00000:0:0:0:")
	assert.Equal(t, ports.RuntimeModeHQ50, finalized.Profile.EffectiveRuntimeMode)
	assert.Equal(t, ports.RuntimeModeSourceEnvOverride, finalized.Profile.EffectiveModeSource)
	assert.Equal(t, 9000, finalized.Profile.VideoMaxRateK)
	assert.Equal(t, 18000, finalized.Profile.VideoBufSizeK)
}

func TestFinalizePlan_AdaptiveTranscodeQualityPromotesAV1FMP4ToHQ50(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)

	spec := ports.StreamSpec{
		SessionID: "finalize-adaptive-av1-quality",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityHigh,
		Profile: model.ProfileSpec{
			Name:           "av1_hw",
			PolicyModeHint: ports.RuntimeModeHQ25,
			TranscodeVideo: true,
			HWAccel:        "vaapi_encode_only",
			VideoCodec:     "av1",
			Deinterlace:    true,
			Container:      "fmp4",
			VideoMaxRateK:  6000,
			VideoBufSizeK:  12000,
			AudioBitrateK:  192,
		},
		Source: ports.StreamSource{
			ID:   "1:0:19:EF75:3F9:1:C00000:0:0:0:",
			Type: ports.SourceTuner,
		},
	}

	finalized := adapter.FinalizePlan(context.Background(), spec, "http://127.0.0.1:17999/1:0:19:EF75:3F9:1:C00000:0:0:0:")
	assert.Equal(t, "av1_hw", finalized.Profile.Name)
	assert.Equal(t, "av1", finalized.Profile.VideoCodec)
	assert.Equal(t, "fmp4", finalized.Profile.Container)
	assert.Equal(t, ports.RuntimeModeHQ50, finalized.Profile.PolicyModeHint)
	assert.Equal(t, ports.RuntimeModeHQ50, finalized.Profile.EffectiveRuntimeMode)
	assert.Equal(t, ports.RuntimeModeSourceRuntimeHardening, finalized.Profile.EffectiveModeSource)
	assert.False(t, finalized.Profile.ForceSafariHQ25)
	assert.Equal(t, 14000, finalized.Profile.VideoMaxRateK)
	assert.Equal(t, 28000, finalized.Profile.VideoBufSizeK)
}

func TestFinalizePlan_AdaptiveTranscodeQualityHonorsAV1BudgetEnv(t *testing.T) {
	t.Setenv("XG2G_ADAPTIVE_AV1_MAXRATE_K", "16000")
	t.Setenv("XG2G_ADAPTIVE_AV1_BUFSIZE_K", "32000")

	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)

	spec := ports.StreamSpec{
		SessionID: "finalize-adaptive-av1-quality-env",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityHigh,
		Profile: model.ProfileSpec{
			Name:           "av1_hw",
			PolicyModeHint: ports.RuntimeModeHQ25,
			TranscodeVideo: true,
			HWAccel:        "vaapi_encode_only",
			VideoCodec:     "av1",
			Container:      "fmp4",
			VideoMaxRateK:  6000,
			VideoBufSizeK:  12000,
			AudioBitrateK:  192,
		},
		Source: ports.StreamSource{
			ID:   "1:0:19:EF75:3F9:1:C00000:0:0:0:",
			Type: ports.SourceTuner,
		},
	}

	finalized := adapter.FinalizePlan(context.Background(), spec, "http://127.0.0.1:17999/1:0:19:EF75:3F9:1:C00000:0:0:0:")
	assert.Equal(t, ports.RuntimeModeHQ50, finalized.Profile.EffectiveRuntimeMode)
	assert.Equal(t, 16000, finalized.Profile.VideoMaxRateK)
	assert.Equal(t, 32000, finalized.Profile.VideoBufSizeK)
}

func TestFinalizePlan_AdaptiveTranscodeQualityPromotesHEVCFMP4ToHQ50(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)

	spec := ports.StreamSpec{
		SessionID: "finalize-adaptive-hevc-quality",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityHigh,
		Profile: model.ProfileSpec{
			Name:           "safari_hevc_hw",
			PolicyModeHint: ports.RuntimeModeHQ25,
			TranscodeVideo: true,
			HWAccel:        "vaapi",
			VideoCodec:     "hevc",
			Container:      "fmp4",
			VideoMaxRateK:  5000,
			VideoBufSizeK:  10000,
			AudioBitrateK:  192,
		},
		Source: ports.StreamSource{
			ID:   "1:0:19:EF75:3F9:1:C00000:0:0:0:",
			Type: ports.SourceTuner,
		},
	}

	finalized := adapter.FinalizePlan(context.Background(), spec, "http://127.0.0.1:17999/1:0:19:EF75:3F9:1:C00000:0:0:0:")
	assert.Equal(t, "hevc", finalized.Profile.VideoCodec)
	assert.Equal(t, ports.RuntimeModeHQ50, finalized.Profile.PolicyModeHint)
	assert.Equal(t, ports.RuntimeModeHQ50, finalized.Profile.EffectiveRuntimeMode)
	assert.Equal(t, ports.RuntimeModeSourceRuntimeHardening, finalized.Profile.EffectiveModeSource)
	assert.Equal(t, 14000, finalized.Profile.VideoMaxRateK)
	assert.Equal(t, 28000, finalized.Profile.VideoBufSizeK)
}

func TestFinalizePlan_AdaptiveTranscodeQualityPromotesH264ToHQ50(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)

	spec := ports.StreamSpec{
		SessionID: "finalize-adaptive-h264-quality",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityHigh,
		Profile: model.ProfileSpec{
			Name:           "h264_fmp4",
			PolicyModeHint: ports.RuntimeModeHQ25,
			TranscodeVideo: true,
			VideoCodec:     "libx264",
			Container:      "mpegts",
			VideoMaxRateK:  8000,
			VideoBufSizeK:  16000,
			AudioBitrateK:  192,
		},
		Source: ports.StreamSource{
			ID:   "1:0:19:EF75:3F9:1:C00000:0:0:0:",
			Type: ports.SourceTuner,
		},
	}

	finalized := adapter.FinalizePlan(context.Background(), spec, "http://127.0.0.1:17999/1:0:19:EF75:3F9:1:C00000:0:0:0:")
	assert.Equal(t, "libx264", finalized.Profile.VideoCodec)
	assert.Equal(t, ports.RuntimeModeHQ50, finalized.Profile.PolicyModeHint)
	assert.Equal(t, ports.RuntimeModeHQ50, finalized.Profile.EffectiveRuntimeMode)
	assert.Equal(t, ports.RuntimeModeSourceRuntimeHardening, finalized.Profile.EffectiveModeSource)
	assert.Equal(t, 16000, finalized.Profile.VideoMaxRateK)
	assert.Equal(t, 32000, finalized.Profile.VideoBufSizeK)
}

func TestFinalizePlan_AdaptiveTranscodeQualityRespectsExplicitHQ25Cap(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)

	spec := ports.StreamSpec{
		SessionID: "finalize-adaptive-av1-quality-hq25-cap",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityHigh,
		Profile: model.ProfileSpec{
			Name:            "av1_hw",
			PolicyModeHint:  ports.RuntimeModeHQ25,
			ForceSafariHQ25: true,
			TranscodeVideo:  true,
			HWAccel:         "vaapi_encode_only",
			VideoCodec:      "av1",
			Container:       "fmp4",
			VideoMaxRateK:   6000,
			VideoBufSizeK:   12000,
			AudioBitrateK:   192,
		},
		Source: ports.StreamSource{
			ID:   "1:0:19:EF75:3F9:1:C00000:0:0:0:",
			Type: ports.SourceTuner,
		},
	}

	finalized := adapter.FinalizePlan(context.Background(), spec, "http://127.0.0.1:17999/1:0:19:EF75:3F9:1:C00000:0:0:0:")
	assert.Equal(t, ports.RuntimeModeHQ25, finalized.Profile.EffectiveRuntimeMode)
	assert.Equal(t, ports.RuntimeModeSourceResolve, finalized.Profile.EffectiveModeSource)
	assert.Equal(t, 6000, finalized.Profile.VideoMaxRateK)
	assert.Equal(t, 12000, finalized.Profile.VideoBufSizeK)
}

func TestFinalizePlan_AdaptiveTranscodeQualityCanBeDisabled(t *testing.T) {
	t.Setenv("XG2G_ADAPTIVE_QUALITY_ENABLED", "false")

	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)

	spec := ports.StreamSpec{
		SessionID: "finalize-adaptive-av1-quality-disabled",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityHigh,
		Profile: model.ProfileSpec{
			Name:           "av1_hw",
			PolicyModeHint: ports.RuntimeModeHQ25,
			TranscodeVideo: true,
			HWAccel:        "vaapi_encode_only",
			VideoCodec:     "av1",
			Container:      "fmp4",
			VideoMaxRateK:  6000,
			VideoBufSizeK:  12000,
			AudioBitrateK:  192,
		},
		Source: ports.StreamSource{
			ID:   "1:0:19:EF75:3F9:1:C00000:0:0:0:",
			Type: ports.SourceTuner,
		},
	}

	finalized := adapter.FinalizePlan(context.Background(), spec, "http://127.0.0.1:17999/1:0:19:EF75:3F9:1:C00000:0:0:0:")
	assert.Equal(t, ports.RuntimeModeHQ25, finalized.Profile.EffectiveRuntimeMode)
	assert.Equal(t, ports.RuntimeModeSourceResolve, finalized.Profile.EffectiveModeSource)
	assert.Equal(t, 6000, finalized.Profile.VideoMaxRateK)
	assert.Equal(t, 12000, finalized.Profile.VideoBufSizeK)
}

func TestFinalizePlan_AdaptiveTranscodeQualityHonorsLegacyAV1DisableEnv(t *testing.T) {
	t.Setenv("XG2G_ADAPTIVE_AV1_QUALITY_ENABLED", "false")

	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)

	spec := ports.StreamSpec{
		SessionID: "finalize-adaptive-av1-quality-legacy-disabled",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityHigh,
		Profile: model.ProfileSpec{
			Name:           "av1_hw",
			PolicyModeHint: ports.RuntimeModeHQ25,
			TranscodeVideo: true,
			HWAccel:        "vaapi_encode_only",
			VideoCodec:     "av1",
			Container:      "fmp4",
			VideoMaxRateK:  6000,
			VideoBufSizeK:  12000,
			AudioBitrateK:  192,
		},
		Source: ports.StreamSource{
			ID:   "1:0:19:EF75:3F9:1:C00000:0:0:0:",
			Type: ports.SourceTuner,
		},
	}

	finalized := adapter.FinalizePlan(context.Background(), spec, "http://127.0.0.1:17999/1:0:19:EF75:3F9:1:C00000:0:0:0:")
	assert.Equal(t, ports.RuntimeModeHQ25, finalized.Profile.EffectiveRuntimeMode)
	assert.Equal(t, ports.RuntimeModeSourceResolve, finalized.Profile.EffectiveModeSource)
	assert.Equal(t, 6000, finalized.Profile.VideoMaxRateK)
	assert.Equal(t, 12000, finalized.Profile.VideoBufSizeK)
}

func TestFinalizePlan_HQ25ServiceRefWinsOverHQ50ServiceRef(t *testing.T) {
	t.Setenv("XG2G_SAFARI_HQ25_SERVICE_REFS", "1:0:19:91:4:85:C00000:0:0:0:")
	t.Setenv("XG2G_SAFARI_HQ50_SERVICE_REFS", "1:0:19:91:4:85:C00000:0:0:0:")

	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)
	adapter.streamProbeFn = func(ctx context.Context, inputURL string) (*vod.StreamInfo, error) {
		t.Fatal("service-ref runtime override should not depend on the runtime probe")
		return nil, nil
	}

	spec := ports.StreamSpec{
		SessionID: "finalize-hq25-precedence",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityStandard,
		Profile: model.ProfileSpec{
			Name:           "safari",
			PolicyModeHint: ports.RuntimeModeCopy,
			TranscodeVideo: true,
			Container:      "fmp4",
			AudioBitrateK:  192,
		},
		Source: ports.StreamSource{
			ID:   "1:0:19:91:4:85:C00000:0:0:0:",
			Type: ports.SourceTuner,
		},
	}

	finalized := adapter.FinalizePlan(context.Background(), spec, "http://127.0.0.1:17999/1:0:19:91:4:85:C00000:0:0:0:")
	assert.Equal(t, profiles.ProfileSafariRuntimeHQ, finalized.Profile.Name)
	assert.True(t, finalized.Profile.ForceSafariHQ25)
	assert.Equal(t, ports.RuntimeModeHQ25, finalized.Profile.EffectiveRuntimeMode)
	assert.Equal(t, ports.RuntimeModeSourceEnvOverride, finalized.Profile.EffectiveModeSource)
}

func TestFinalizePlan_ProgressiveAV1HardeningOnlyClearsDeinterlace(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)
	adapter.streamProbeFn = func(ctx context.Context, inputURL string) (*vod.StreamInfo, error) {
		return &vod.StreamInfo{
			Container: "ts",
			Video: vod.VideoStreamInfo{
				CodecName:  "h264",
				Interlaced: false,
			},
		}, nil
	}

	spec := ports.StreamSpec{
		SessionID: "finalize-av1-progressive-hardening",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityStandard,
		Profile: model.ProfileSpec{
			Name:           "av1_hw",
			TranscodeVideo: true,
			HWAccel:        "vaapi",
			VideoCodec:     "av1",
			Deinterlace:    true,
			Container:      "mpegts",
			VideoMaxRateK:  6000,
			VideoBufSizeK:  12000,
			AudioBitrateK:  192,
		},
		Source: ports.StreamSource{
			ID:   "http://example.com/stream",
			Type: ports.SourceURL,
		},
	}

	finalized := adapter.FinalizePlan(context.Background(), spec, spec.Source.ID)
	require.Equal(t, "av1", finalized.Profile.VideoCodec)
	assert.True(t, finalized.Profile.TranscodeVideo)
	assert.False(t, finalized.Profile.Deinterlace)
	assert.Equal(t, ports.RuntimeModeHQ25, finalized.Profile.EffectiveRuntimeMode)
	assert.Equal(t, ports.RuntimeModeSourceRuntimeHardening, finalized.Profile.EffectiveModeSource)
}

func TestFinalizePlan_ProgressiveAV1HardeningDoesNotRetryTransientProbeFailures(t *testing.T) {
	t.Setenv("XG2G_ADAPTIVE_QUALITY_ENABLED", "false")

	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)
	adapter.SafariRuntimeProbeTimeout = 2 * time.Second

	probeCalls := 0
	adapter.streamProbeFn = func(ctx context.Context, inputURL string) (*vod.StreamInfo, error) {
		probeCalls++
		return nil, errors.New("ffprobe failed: exit status 1 (stderr: [http @ 0x0] Stream ends prematurely at 0. Input/output error)")
	}

	spec := ports.StreamSpec{
		SessionID: "finalize-av1-progressive-hardening-no-retry",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityStandard,
		Profile: model.ProfileSpec{
			Name:           "av1_hw",
			TranscodeVideo: true,
			HWAccel:        "vaapi",
			VideoCodec:     "av1",
			Deinterlace:    true,
			Container:      "fmp4",
			VideoMaxRateK:  6000,
			VideoBufSizeK:  12000,
			AudioBitrateK:  192,
		},
		Source: ports.StreamSource{
			ID:   "1:0:19:1331:3EF:1:C00000:0:0:0:",
			Type: ports.SourceTuner,
		},
	}

	finalized := adapter.FinalizePlan(context.Background(), spec, "http://127.0.0.1:17999/1:0:19:1331:3EF:1:C00000:0:0:0:")
	require.Equal(t, 1, probeCalls, "progressive av1 hardening should fail open without startup retries")
	assert.True(t, finalized.Profile.Deinterlace, "transient probe failures must keep the safe deinterlace default")
	assert.Equal(t, ports.RuntimeModeHQ25, finalized.Profile.EffectiveRuntimeMode)
	assert.Equal(t, ports.RuntimeModeSourceResolve, finalized.Profile.EffectiveModeSource)
}
