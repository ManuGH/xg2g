package ffmpeg

import (
	"context"
	"io"
	"path/filepath"
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

// TestABR_RegressionGuard_SingleRendition performs a STRICT full frozen element-by-element
// []string slice comparison using assert.Equal to guarantee zero regression on single-rendition streams.
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

	// Complete frozen []string slice up to dynamic output paths
	expectedPrefix := []string{
		"-fflags", "+genpts+discardcorrupt+flush_packets",
		"-err_detect", "ignore_err",
		"-max_error_rate", "1.0",
		"-ignore_unknown",
		"-avoid_negative_ts", "make_zero",
		"-headers", "Icy-MetaData: 1\r\n",
		"-analyzeduration", "1000000",
		"-probesize", "1M",
		"-protocol_whitelist", "crypto,http,https,tcp,tls",
		"-reconnect", "1",
		"-reconnect_at_eof", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "2",
		"-reconnect_on_network_error", "1",
		"-reconnect_on_http_error", "4xx,5xx",
		"-i", "http://localhost:8080/stream",
		"-progress", "pipe:2",
		"-map", "0:v:0?",
		"-map", "0:a:0?",
		"-r", "25",
		"-c:v", "libx264",
		"-preset", "superfast",
		"-tune", "zerolatency",
		"-crf", "20",
		"-x264-params", "keyint=50:min-keyint=50:scenecut=0",
		"-g", "50",
		"-sc_threshold", "0",
		"-force_key_frames", "expr:gte(t,n_forced*2)",
		"-flags", "+cgop",
		"-pix_fmt", "yuv420p",
		"-profile:v", "main",
		"-c:a", "aac",
		"-b:a", "320k",
		"-ac", "2",
		"-ar", "48000",
		"-sn",
		"-f", "hls",
		"-hls_time", "2",
		"-hls_list_size", "30",
		"-hls_flags", "delete_segments+append_list+program_date_time+independent_segments+temp_file",
		"-hls_segment_type", "mpegts",
	}

	require.Len(t, plan.args, len(expectedPrefix)+3)
	assert.Equal(t, expectedPrefix, plan.args[:len(expectedPrefix)])
	assert.Equal(t, "-hls_segment_filename", plan.args[len(expectedPrefix)])
	assert.Equal(t, "seg_%06d.ts", filepath.Base(plan.args[len(expectedPrefix)+1]))
	assert.Equal(t, "index.m3u8", filepath.Base(plan.args[len(expectedPrefix)+2]))
}

// TestABR_RegressionGuard_DirectCopy performs a STRICT full frozen element-by-element
// []string slice comparison using assert.Equal for Direct Copy sessions.
func TestABR_RegressionGuard_DirectCopy(t *testing.T) {
	adapter := newABRTestAdapter(t)

	spec := ports.StreamSpec{
		SessionID: "sess-direct-copy-1",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Source:    ports.StreamSource{Type: ports.SourceURL, ID: "stream1"},
		Profile: ports.ProfileSpec{
			Name:           "passthrough",
			TranscodeVideo: false, // Direct Copy
			EnableABR:      true,  // Must be ignored for direct copy
		},
	}

	plan, err := adapter.buildArgsWithPlan(context.Background(), spec, "http://localhost:8080/stream")
	require.NoError(t, err)
	assert.Equal(t, "index.m3u8", plan.primaryPlaylist)

	expectedPrefix := []string{
		"-fflags", "+genpts+discardcorrupt+flush_packets",
		"-err_detect", "ignore_err",
		"-max_error_rate", "1.0",
		"-ignore_unknown",
		"-avoid_negative_ts", "make_zero",
		"-headers", "Icy-MetaData: 1\r\n",
		"-analyzeduration", "1000000",
		"-probesize", "1M",
		"-protocol_whitelist", "crypto,http,https,tcp,tls",
		"-reconnect", "1",
		"-reconnect_at_eof", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "2",
		"-reconnect_on_network_error", "1",
		"-reconnect_on_http_error", "4xx,5xx",
		"-i", "http://localhost:8080/stream",
		"-progress", "pipe:2",
		"-map", "0:v:0?",
		"-map", "0:a:0?",
		"-c:v", "copy",
		"-enc_time_base:v", "demux",
		"-bsf:v", "dump_extra=freq=keyframe",
		"-c:a", "copy",
		"-sn",
		"-f", "hls",
		"-hls_time", "2",
		"-hls_list_size", "30",
		"-hls_flags", "delete_segments+append_list+program_date_time+temp_file",
		"-hls_segment_type", "mpegts",
	}

	require.Len(t, plan.args, len(expectedPrefix)+3)
	assert.Equal(t, expectedPrefix, plan.args[:len(expectedPrefix)])
	assert.Equal(t, "-hls_segment_filename", plan.args[len(expectedPrefix)])
	assert.Equal(t, "seg_%06d.ts", filepath.Base(plan.args[len(expectedPrefix)+1]))
	assert.Equal(t, "index.m3u8", filepath.Base(plan.args[len(expectedPrefix)+2]))
}

// TestABR_2Tier_VBVParameters explicitly validates all per-stream VBV flags in the 2-tier ladder,
// guaranteeing that -bufsize:v:1 2000k is properly mapped to v:1 (480p) and not v:2.
func TestABR_2Tier_VBVParameters(t *testing.T) {
	adapter := newABRTestAdapter(t)

	spec720 := ports.StreamSpec{
		SessionID: "sess-vbv-check",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Source:    ports.StreamSource{Type: ports.SourceURL, ID: "stream1"},
		Profile: ports.ProfileSpec{
			Name:              "hq25",
			TranscodeVideo:    true,
			VideoCodec:        "h264",
			VideoSourceHeight: 720,
			EnableABR:         true,
		},
	}

	plan, err := adapter.buildArgsWithPlan(context.Background(), spec720, "http://localhost:8080/stream")
	require.NoError(t, err)
	assert.Equal(t, "master.m3u8", plan.primaryPlaylist)

	args := plan.args

	// Check 720p (v:0) VBV parameters
	b0Idx := indexOf(args, "-b:v:0")
	require.Greater(t, b0Idx, -1)
	assert.Equal(t, "2000k", args[b0Idx+1])

	max0Idx := indexOf(args, "-maxrate:v:0")
	require.Greater(t, max0Idx, -1)
	assert.Equal(t, "2400k", args[max0Idx+1])

	buf0Idx := indexOf(args, "-bufsize:v:0")
	require.Greater(t, buf0Idx, -1)
	assert.Equal(t, "4000k", args[buf0Idx+1])

	// Check 480p (v:1) VBV parameters - strict index check!
	b1Idx := indexOf(args, "-b:v:1")
	require.Greater(t, b1Idx, -1)
	assert.Equal(t, "900k", args[b1Idx+1])

	max1Idx := indexOf(args, "-maxrate:v:1")
	require.Greater(t, max1Idx, -1)
	assert.Equal(t, "1100k", args[max1Idx+1])

	buf1Idx := indexOf(args, "-bufsize:v:1")
	require.Greater(t, buf1Idx, -1, "-bufsize:v:1 MUST exist for v:1 output")
	assert.Equal(t, "2000k", args[buf1Idx+1])

	// Must NOT contain -bufsize:v:2 in 2-tier mode
	buf2Idx := indexOf(args, "-bufsize:v:2")
	assert.Equal(t, -1, buf2Idx, "-bufsize:v:2 must NOT exist in 2-tier ladder")
}

// TestABR_UnknownSourceHeight_DefaultsTo2Tier verifies that when VideoSourceHeight == 0 (unknown/unspecified),
// the planner fail-safes conservatively to a 2-tier ladder (720p/480p) to prevent fake 1080p upscaling.
func TestABR_UnknownSourceHeight_DefaultsTo2Tier(t *testing.T) {
	adapter := newABRTestAdapter(t)

	specUnknown := ports.StreamSpec{
		SessionID: "sess-unknown-height",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Source:    ports.StreamSource{Type: ports.SourceURL, ID: "stream1"},
		Profile: ports.ProfileSpec{
			Name:              "hq25",
			TranscodeVideo:    true,
			VideoCodec:        "h264",
			VideoSourceHeight: 0, // Unknown/unspecified source height
			EnableABR:         true,
		},
	}

	plan, err := adapter.buildArgsWithPlan(context.Background(), specUnknown, "http://localhost:8080/stream")
	require.NoError(t, err)
	assert.Equal(t, "master.m3u8", plan.primaryPlaylist)

	cmdStr := strings.Join(plan.args, " ")
	assert.Contains(t, cmdStr, "[0:v:0]split=2[v720in][v480in]")
	assert.Contains(t, cmdStr, "agroup:audio,name:720p")
	assert.False(t, strings.Contains(cmdStr, "1080p")) // Fail-safe: zero fake 1080p upscaling!
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
