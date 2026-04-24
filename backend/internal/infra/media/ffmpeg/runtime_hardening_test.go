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

func TestFinalizePlan_SafariForceCopyAllowlistAppliesCopyHardenedMode(t *testing.T) {
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
	assert.Equal(t, "mpegts", finalized.Profile.Container)
	assert.Equal(t, ports.RuntimeModeCopyHardened, finalized.Profile.EffectiveRuntimeMode)
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
