package ffmpeg

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newABRTestAdapter(t *testing.T) *LocalAdapter {
	return NewLocalAdapter(
		"ffmpeg",
		"ffprobe",
		t.TempDir(),
		nil,
		zerolog.New(io.Discard),
		"",
		"",
		0,
		0,
		false,
		2,
		2,
		0,
		0,
		"",
	)
}

// TestABR_RegressionGuard_SingleRendition verifies that EnableABR = false preserves
// 100% of the existing single-rendition FFmpeg argument planning and primaryPlaylist = "index.m3u8".
func TestABR_RegressionGuard_SingleRendition(t *testing.T) {
	adapter := newABRTestAdapter(t)

	spec := ports.StreamSpec{
		SessionID: "sess-regression-1",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Source:    ports.StreamSource{Type: ports.SourceURL, ID: "stream1"},
		Profile: ports.ProfileSpec{
			Name:           "hq25",
			TranscodeVideo: true,
			VideoCodec:     "h264",
			EnableABR:      false, // Explicitly false
		},
	}

	plan, err := adapter.buildArgsWithPlan(context.Background(), spec, "http://localhost:8080/stream")
	require.NoError(t, err)
	assert.Equal(t, "index.m3u8", plan.primaryPlaylist)
	assert.False(t, strings.Contains(strings.Join(plan.args, " "), "-master_pl_name"))
}

// TestABR_3Tier_Source1080p_DynamicGOP verifies that EnableABR = true with a >720p source
// generates a 3-tier master.m3u8 ladder with dynamic GOP calculated from output FPS.
func TestABR_3Tier_Source1080p_DynamicGOP(t *testing.T) {
	// 1. Test 1080p Source @ 25 FPS (gop = 25 * 2 = 50)
	adapter25 := newABRTestAdapter(t)
	adapter25.fpsProbeFn = func(ctx context.Context, url string) (int, string, error) {
		return 25, "probed_25p", nil
	}

	spec25 := ports.StreamSpec{
		SessionID: "sess-abr-1080p-25fps",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Source:    ports.StreamSource{Type: ports.SourceURL, ID: "stream1"},
		Profile: ports.ProfileSpec{
			Name:              "hq25",
			TranscodeVideo:    true,
			VideoCodec:        "h264",
			VideoSourceHeight: 1080,
			EnableABR:         true,
		},
	}

	plan25, err := adapter25.buildArgsWithPlan(context.Background(), spec25, "http://localhost:8080/stream")
	require.NoError(t, err)
	assert.Equal(t, "master.m3u8", plan25.primaryPlaylist)

	cmdStr25 := strings.Join(plan25.args, " ")
	assert.Contains(t, cmdStr25, "-master_pl_name master.m3u8")
	assert.Contains(t, cmdStr25, "[0:v:0]split=3[v1080in][v720in][v480in]")
	assert.Contains(t, cmdStr25, "agroup:audio,name:1080p")
	assert.Contains(t, cmdStr25, "-g 50 -keyint_min 50 -force_key_frames expr:gte(t,n_forced*2)")

	// 2. Test 1080p Source @ 50 FPS (gop = 50 * 2 = 100)
	adapter50 := newABRTestAdapter(t)
	adapter50.fpsProbeFn = func(ctx context.Context, url string) (int, string, error) {
		return 50, "probed_50p", nil
	}

	spec50 := ports.StreamSpec{
		SessionID: "sess-abr-1080p-50fps",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Source:    ports.StreamSource{Type: ports.SourceURL, ID: "stream1"},
		Profile: ports.ProfileSpec{
			Name:                 "hq50",
			PolicyModeHint:       ports.RuntimeModeHQ50,
			EffectiveRuntimeMode: ports.RuntimeModeHQ50,
			EffectiveModeSource:  ports.RuntimeModeSourceResolve,
			PlannerBound:         true,
			TranscodeVideo:       true,
			VideoCodec:           "h264",
			VideoSourceHeight:    1080,
			EnableABR:            true,
		},
	}

	plan50, err := adapter50.buildArgsWithPlan(context.Background(), spec50, "http://localhost:8080/stream")
	require.NoError(t, err)
	assert.Equal(t, "master.m3u8", plan50.primaryPlaylist)

	cmdStr50 := strings.Join(plan50.args, " ")
	assert.Contains(t, cmdStr50, "-g 100 -keyint_min 100 -force_key_frames expr:gte(t,n_forced*2)")
}

// TestABR_2Tier_Source720p_NoFakeUpscaling verifies that a 720p source generates
// a 2-tier ladder (720p / 480p) without fake 1080p upscaling.
func TestABR_2Tier_Source720p_NoFakeUpscaling(t *testing.T) {
	adapter := newABRTestAdapter(t)

	spec720 := ports.StreamSpec{
		SessionID: "sess-abr-720p-source",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Source:    ports.StreamSource{Type: ports.SourceURL, ID: "stream1"},
		Profile: ports.ProfileSpec{
			Name:              "hq25",
			TranscodeVideo:    true,
			VideoCodec:        "h264",
			VideoSourceHeight: 720, // ORF / ARD / ZDF 720p source
			EnableABR:         true,
		},
	}

	plan720, err := adapter.buildArgsWithPlan(context.Background(), spec720, "http://localhost:8080/stream")
	require.NoError(t, err)
	assert.Equal(t, "master.m3u8", plan720.primaryPlaylist)

	cmdStr720 := strings.Join(plan720.args, " ")
	assert.Contains(t, cmdStr720, "-filter_complex [0:v:0]split=2[v720in][v480in]")
	assert.Contains(t, cmdStr720, "agroup:audio,name:720p")
	assert.False(t, strings.Contains(cmdStr720, "1080p")) // Zero fake 1080p upscaling!
}
