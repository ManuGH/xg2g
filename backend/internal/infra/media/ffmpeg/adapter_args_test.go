package ffmpeg

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func indexOf(args []string, target string) int {
	for i, a := range args {
		if a == target {
			return i
		}
	}
	return -1
}

func valueAfter(args []string, flag string) (string, bool) {
	idx := indexOf(args, flag)
	if idx < 0 || idx+1 >= len(args) {
		return "", false
	}
	return args[idx+1], true
}

func TestBuildArgs_UsesOptionalVideoMap(t *testing.T) {
	adapter := NewLocalAdapter(
		"",
		"",
		t.TempDir(),
		nil,
		zerolog.New(io.Discard),
		"",
		"",
		0,
		0,
		false,
		2*time.Second,
		6,
		0,
		0,
		"",
	)

	spec := ports.StreamSpec{
		SessionID: "sid-1",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityStandard,
		Source: ports.StreamSource{
			ID:   "http://example.com/stream",
			Type: ports.SourceURL,
		},
	}

	args, err := adapter.buildArgs(context.Background(), spec, spec.Source.ID)
	require.NoError(t, err)
	assert.Contains(t, args, "0:v:0?", "video map should be optional for audio-only inputs")
	assert.Contains(t, args, "0:a:0?", "audio map should remain optional")
}

func TestBuildArgs_EmptyProfileLegacy(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)

	spec := ports.StreamSpec{
		SessionID: "legacy-1",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityStandard,
		// Profile is zero-valued -> legacy path
		Source: ports.StreamSource{
			ID:   "http://example.com/stream",
			Type: ports.SourceURL,
		},
	}

	args, err := adapter.buildArgs(context.Background(), spec, spec.Source.ID)
	require.NoError(t, err)
	assert.Contains(t, args, "libx264", "legacy path must use libx264")
	assert.Contains(t, args, "yadif", "legacy path must use yadif deinterlace")
	assert.Contains(t, args, "-crf", "legacy path must use CRF")
	assert.Contains(t, args, "20", "legacy path CRF=20")
	assert.Contains(t, args, "-preset", "legacy path must have preset")
	assert.Contains(t, args, "ultrafast", "legacy path preset=ultrafast")
	assert.NotContains(t, args, "h264_vaapi", "legacy path must not contain VAAPI")
	assert.NotContains(t, args, "-vaapi_device", "legacy path must not contain vaapi_device")
}

func TestBuildArgs_VaapiH264Deinterlace(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "/dev/dri/renderD128",
	)
	adapter.vaapiEncoders = map[string]bool{"h264_vaapi": true, "hevc_vaapi": true} // simulate passed preflight

	spec := ports.StreamSpec{
		SessionID: "vaapi-1",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityStandard,
		Profile: model.ProfileSpec{
			TranscodeVideo: true,
			HWAccel:        "vaapi",
			VideoCodec:     "h264",
			Deinterlace:    true,
			VideoMaxRateK:  20000,
			VideoBufSizeK:  40000,
			AudioBitrateK:  192,
		},
		Source: ports.StreamSource{
			ID:   "http://example.com/stream",
			Type: ports.SourceURL,
		},
	}

	args, err := adapter.buildArgs(context.Background(), spec, spec.Source.ID)
	require.NoError(t, err)

	// VAAPI device init must appear before -i
	iIdx := indexOf(args, "-i")
	require.True(t, iIdx > 0)

	vaapiDevIdx := indexOf(args, "-vaapi_device")
	require.True(t, vaapiDevIdx >= 0 && vaapiDevIdx < iIdx, "vaapi_device must come before -i")
	assert.Equal(t, "/dev/dri/renderD128", args[vaapiDevIdx+1])

	assert.Contains(t, args, "-hwaccel")
	assert.Contains(t, args, "-hwaccel_output_format")

	// Encoder
	assert.Contains(t, args, "h264_vaapi")

	// Deinterlace in GPU memory (NOT CPU yadif)
	assert.Contains(t, args, "deinterlace_vaapi")
	assert.NotContains(t, args, "yadif")

	// VBR rate control (NOT CRF)
	assert.Contains(t, args, "-b:v")
	assert.Contains(t, args, "20000k")
	assert.Contains(t, args, "-maxrate")
	assert.Contains(t, args, "-bufsize")
	assert.Contains(t, args, "40000k")
	assert.NotContains(t, args, "-crf")

	// No CPU-specific flags
	assert.NotContains(t, args, "-preset")
	assert.NotContains(t, args, "-tune")
	assert.NotContains(t, args, "-pix_fmt")
}

func TestBuildArgs_VaapiHEVC(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "/dev/dri/renderD128",
	)
	adapter.vaapiEncoders = map[string]bool{"h264_vaapi": true, "hevc_vaapi": true}

	spec := ports.StreamSpec{
		SessionID: "vaapi-hevc",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityStandard,
		Profile: model.ProfileSpec{
			TranscodeVideo: true,
			HWAccel:        "vaapi",
			VideoCodec:     "hevc",
			Deinterlace:    false,
			VideoMaxRateK:  5000,
			VideoBufSizeK:  10000,
			AudioBitrateK:  192,
		},
		Source: ports.StreamSource{
			ID:   "http://example.com/stream",
			Type: ports.SourceURL,
		},
	}

	args, err := adapter.buildArgs(context.Background(), spec, spec.Source.ID)
	require.NoError(t, err)
	assert.Contains(t, args, "hevc_vaapi")
	assert.NotContains(t, args, "h264_vaapi")
	assert.NotContains(t, args, "deinterlace_vaapi", "progressive source: no deinterlace filter")
	assert.NotContains(t, args, "yadif")
}

func TestBuildArgs_VaapiNoPreflightFails(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "/dev/dri/renderD128",
	)
	// vaapiEncoders is nil (preflight not run)

	spec := ports.StreamSpec{
		SessionID: "vaapi-nopreflight",
		Mode:      ports.ModeLive,
		Profile: model.ProfileSpec{
			TranscodeVideo: true,
			HWAccel:        "vaapi",
			VideoCodec:     "h264",
		},
		Source: ports.StreamSource{
			ID:   "http://example.com/stream",
			Type: ports.SourceURL,
		},
	}

	_, err := adapter.buildArgs(context.Background(), spec, spec.Source.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not verified by preflight")
}

func TestBuildArgs_VaapiNoDeviceFails(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "", // no device
	)

	spec := ports.StreamSpec{
		SessionID: "vaapi-nodevice",
		Mode:      ports.ModeLive,
		Profile: model.ProfileSpec{
			TranscodeVideo: true,
			HWAccel:        "vaapi",
			VideoCodec:     "h264",
		},
		Source: ports.StreamSource{
			ID:   "http://example.com/stream",
			Type: ports.SourceURL,
		},
	}

	_, err := adapter.buildArgs(context.Background(), spec, spec.Source.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no vaapi device configured")
}

func TestBuildArgs_VaapiDefaultQuality(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "/dev/dri/renderD128",
	)
	adapter.vaapiEncoders = map[string]bool{"h264_vaapi": true, "hevc_vaapi": true}

	spec := ports.StreamSpec{
		SessionID: "vaapi-default-q",
		Mode:      ports.ModeLive,
		Profile: model.ProfileSpec{
			TranscodeVideo: true,
			HWAccel:        "vaapi",
			VideoCodec:     "h264",
			// VideoMaxRateK is 0 (not set)
		},
		Source: ports.StreamSource{
			ID:   "http://example.com/stream",
			Type: ports.SourceURL,
		},
	}

	args, err := adapter.buildArgs(context.Background(), spec, spec.Source.ID)
	require.NoError(t, err)
	assert.Contains(t, args, "-global_quality")
	assert.Contains(t, args, "23")
	assert.NotContains(t, args, "-b:v", "no rate when VideoMaxRateK=0")
}

func TestBuildArgs_CPUProfileDriven(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)

	spec := ports.StreamSpec{
		SessionID: "cpu-profile",
		Mode:      ports.ModeLive,
		Profile: model.ProfileSpec{
			TranscodeVideo: true,
			VideoCodec:     "libx264",
			Deinterlace:    true,
			VideoCRF:       18,
			VideoMaxRateK:  8000,
			VideoBufSizeK:  16000,
			Preset:         "veryfast",
			AudioBitrateK:  192,
		},
		Source: ports.StreamSource{
			ID:   "http://example.com/stream",
			Type: ports.SourceURL,
		},
	}

	args, err := adapter.buildArgs(context.Background(), spec, spec.Source.ID)
	require.NoError(t, err)
	assert.Contains(t, args, "libx264")
	assert.Contains(t, args, "yadif")
	assert.Contains(t, args, "veryfast")
	assert.Contains(t, args, "18")
	assert.Contains(t, args, "-maxrate")
	assert.Contains(t, args, "8000k")
	assert.NotContains(t, args, "h264_vaapi")
	assert.NotContains(t, args, "-vaapi_device")
}

func TestBuildArgs_IngestFlagsBeforeInput(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)

	spec := ports.StreamSpec{
		SessionID: "ingest-flags",
		Mode:      ports.ModeLive,
		Profile: model.ProfileSpec{
			TranscodeVideo: true,
			VideoCodec:     "libx264",
		},
		Source: ports.StreamSource{
			ID:   "http://example.com/stream",
			Type: ports.SourceURL,
		},
	}

	args, err := adapter.buildArgs(context.Background(), spec, spec.Source.ID)
	require.NoError(t, err)

	iIdx := indexOf(args, "-i")
	require.True(t, iIdx > 0)

	fflagsIdx := indexOf(args, "-fflags")
	require.True(t, fflagsIdx >= 0 && fflagsIdx < iIdx, "fflags must be before -i")
	fflags, ok := valueAfter(args, "-fflags")
	require.True(t, ok)
	assert.Contains(t, fflags, "+genpts")
	assert.Contains(t, fflags, "+discardcorrupt")
	assert.Contains(t, fflags, "+igndts")

	errDetectIdx := indexOf(args, "-err_detect")
	require.True(t, errDetectIdx >= 0 && errDetectIdx < iIdx, "err_detect must be before -i")
	errDetect, ok := valueAfter(args, "-err_detect")
	require.True(t, ok)
	assert.Equal(t, "ignore_err", errDetect)

	analyzeIdx := indexOf(args, "-analyzeduration")
	require.True(t, analyzeIdx >= 0 && analyzeIdx < iIdx, "analyzeduration must be before -i")
	analyze, ok := valueAfter(args, "-analyzeduration")
	require.True(t, ok)
	assert.Equal(t, "1000000", analyze)

	probeIdx := indexOf(args, "-probesize")
	require.True(t, probeIdx >= 0 && probeIdx < iIdx, "probesize must be before -i")
	probe, ok := valueAfter(args, "-probesize")
	require.True(t, ok)
	assert.Equal(t, "1M", probe)
	assert.NotContains(t, fflags, "nobuffer")
}

func TestBuildFPSProbeArgs_DefaultAndRetry(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg", "ffprobe", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)

	args := adapter.buildFPSProbeArgs("http://example.com/stream", false)
	fflags, ok := valueAfter(args, "-fflags")
	require.True(t, ok)
	assert.Equal(t, "+genpts+discardcorrupt+igndts", fflags)

	errDetect, ok := valueAfter(args, "-err_detect")
	require.True(t, ok)
	assert.Equal(t, "ignore_err", errDetect)

	analyze, ok := valueAfter(args, "-analyzeduration")
	require.True(t, ok)
	assert.Equal(t, "2000000", analyze)

	probe, ok := valueAfter(args, "-probesize")
	require.True(t, ok)
	assert.Equal(t, "5M", probe)

	retryArgs := adapter.buildFPSProbeArgs("http://example.com/stream", true)
	retryAnalyze, ok := valueAfter(retryArgs, "-analyzeduration")
	require.True(t, ok)
	assert.Equal(t, "10000000", retryAnalyze)

	retryProbe, ok := valueAfter(retryArgs, "-probesize")
	require.True(t, ok)
	assert.Equal(t, "20M", retryProbe)
}

func TestBuildArgs_ResilientIngestToggleOff(t *testing.T) {
	t.Setenv("XG2G_RESILIENT_INGEST", "false")
	adapter := NewLocalAdapter(
		"ffmpeg", "ffprobe", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)

	spec := ports.StreamSpec{
		SessionID: "ingest-toggle-off",
		Mode:      ports.ModeLive,
		Profile: model.ProfileSpec{
			TranscodeVideo: true,
			VideoCodec:     "libx264",
		},
		Source: ports.StreamSource{
			ID:   "http://example.com/stream",
			Type: ports.SourceURL,
		},
	}

	args, err := adapter.buildArgs(context.Background(), spec, spec.Source.ID)
	require.NoError(t, err)

	fflags, ok := valueAfter(args, "-fflags")
	require.True(t, ok)
	assert.Contains(t, fflags, "+genpts")
	assert.Contains(t, fflags, "+igndts")
	assert.NotContains(t, fflags, "discardcorrupt")
	assert.Equal(t, -1, indexOf(args, "-err_detect"))

	probeArgs := adapter.buildFPSProbeArgs("http://example.com/stream", false)
	probeFFlags, ok := valueAfter(probeArgs, "-fflags")
	require.True(t, ok)
	assert.Equal(t, "+genpts+igndts", probeFFlags)
	assert.Equal(t, -1, indexOf(probeArgs, "-err_detect"))
}

func TestBuildArgs_LiveInputOverridesDoNotAffectFileOrFPSProbe(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg", "ffprobe", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)

	fileSpec := ports.StreamSpec{
		SessionID: "file-input",
		Mode:      ports.ModeLive,
		Profile: model.ProfileSpec{
			TranscodeVideo: true,
			VideoCodec:     "libx264",
		},
		Source: ports.StreamSource{
			ID:   "/tmp/example.ts",
			Type: ports.SourceFile,
		},
	}

	fileArgs, err := adapter.buildArgs(context.Background(), fileSpec, fileSpec.Source.ID)
	require.NoError(t, err)
	analyze, ok := valueAfter(fileArgs, "-analyzeduration")
	require.True(t, ok)
	assert.Equal(t, "2000000", analyze)
	probe, ok := valueAfter(fileArgs, "-probesize")
	require.True(t, ok)
	assert.Equal(t, "5M", probe)
	fflags, ok := valueAfter(fileArgs, "-fflags")
	require.True(t, ok)
	assert.NotContains(t, fflags, "nobuffer")

	probeArgs := adapter.buildFPSProbeArgs("http://example.com/stream", false)
	probeAnalyze, ok := valueAfter(probeArgs, "-analyzeduration")
	require.True(t, ok)
	assert.Equal(t, "2000000", probeAnalyze)
	probeSize, ok := valueAfter(probeArgs, "-probesize")
	require.True(t, ok)
	assert.Equal(t, "5M", probeSize)
}

func TestBuildArgs_LiveInputNoBufferOptIn(t *testing.T) {
	t.Setenv("XG2G_LIVE_NOBUFFER", "true")
	adapter := NewLocalAdapter(
		"ffmpeg", "ffprobe", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)

	spec := ports.StreamSpec{
		SessionID: "live-nobuffer",
		Mode:      ports.ModeLive,
		Profile: model.ProfileSpec{
			TranscodeVideo: true,
			VideoCodec:     "libx264",
		},
		Source: ports.StreamSource{
			ID:   "http://example.com/stream",
			Type: ports.SourceURL,
		},
	}

	args, err := adapter.buildArgs(context.Background(), spec, spec.Source.ID)
	require.NoError(t, err)

	fflags, ok := valueAfter(args, "-fflags")
	require.True(t, ok)
	assert.Contains(t, fflags, "+nobuffer")
}

func TestBuildArgs_AV1HWFallbackWithoutProfileMutation(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "/dev/dri/renderD128",
	)
	// AV1 not verified, but H264/HEVC are.
	adapter.vaapiEncoders = map[string]bool{"h264_vaapi": true, "hevc_vaapi": true}

	spec := ports.StreamSpec{
		SessionID: "av1-fallback",
		Mode:      ports.ModeLive,
		Profile: model.ProfileSpec{
			Name:           "av1_hw",
			TranscodeVideo: true,
			HWAccel:        "vaapi",
			VideoCodec:     "av1",
			VideoMaxRateK:  6000,
			VideoBufSizeK:  12000,
			AudioBitrateK:  192,
		},
		Source: ports.StreamSource{
			ID:   "http://example.com/stream",
			Type: ports.SourceURL,
		},
	}

	args, err := adapter.buildArgs(context.Background(), spec, spec.Source.ID)
	require.NoError(t, err)
	assert.Contains(t, args, "hevc_vaapi", "av1_hw should fall back to a verified HW codec")
	assert.NotContains(t, args, "av1_vaapi", "must not emit av1_vaapi when av1 preflight failed")
	assert.Equal(t, "av1", spec.Profile.VideoCodec, "adapter must not mutate profile codec semantics")
}

func TestBuildArgs_UsesLastKnownFPSWhenProbeFails(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)
	adapter.fpsProbeFn = func(context.Context, string) (int, string, error) {
		return 0, "", errors.New("signal: killed")
	}

	spec := ports.StreamSpec{
		SessionID: "fps-cache-fallback",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityStandard,
		Profile: model.ProfileSpec{
			TranscodeVideo: true,
			VideoCodec:     "h264",
			Deinterlace:    false,
			VideoCRF:       20,
			Preset:         "veryfast",
		},
		Source: ports.StreamSource{
			ID:   "1:0:19:132F:3EF:1:C00000:0:0:0",
			Type: ports.SourceTuner,
		},
	}

	adapter.setLastKnownFPS(fpsCacheKey(spec.Source, "http://example.com/live"), 50)
	args, err := adapter.buildArgs(context.Background(), spec, "http://example.com/live")
	require.NoError(t, err)

	x264Params, ok := valueAfter(args, "-x264-params")
	require.True(t, ok, "x264 params should be present")
	assert.Contains(t, x264Params, "keyint=300:min-keyint=300:scenecut=0")
}

func TestBuildArgs_CachesDetectedFPSAndReusesAfterProbeFailure(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 2, 0, 0, "",
	)
	probeCalls := 0
	adapter.fpsProbeFn = func(context.Context, string) (int, string, error) {
		probeCalls++
		if probeCalls == 1 {
			return 48, "r_frame_rate", nil
		}
		return 0, "", errors.New("signal: killed")
	}

	spec := ports.StreamSpec{
		SessionID: "fps-cache-reuse",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityStandard,
		Profile: model.ProfileSpec{
			TranscodeVideo: true,
			VideoCodec:     "h264",
			Deinterlace:    false,
			VideoCRF:       20,
			Preset:         "veryfast",
		},
		Source: ports.StreamSource{
			ID:   "1:0:19:6F:D:85:C00000:0:0:0",
			Type: ports.SourceTuner,
		},
	}

	args1, err := adapter.buildArgs(context.Background(), spec, "http://example.com/live2")
	require.NoError(t, err)
	x264Params1, ok := valueAfter(args1, "-x264-params")
	require.True(t, ok, "x264 params should be present in first run")
	assert.Contains(t, x264Params1, "keyint=96:min-keyint=96:scenecut=0")

	args2, err := adapter.buildArgs(context.Background(), spec, "http://example.com/live2")
	require.NoError(t, err)
	x264Params2, ok := valueAfter(args2, "-x264-params")
	require.True(t, ok, "x264 params should be present in second run")
	assert.Contains(t, x264Params2, "keyint=96:min-keyint=96:scenecut=0")
}

func TestBuildArgs_IgnoresOutOfRangeLastKnownFPS(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)
	adapter.fpsProbeFn = func(context.Context, string) (int, string, error) {
		return 0, "", errors.New("signal: killed")
	}

	spec := ports.StreamSpec{
		SessionID: "fps-cache-range",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityStandard,
		Profile: model.ProfileSpec{
			TranscodeVideo: true,
			VideoCodec:     "h264",
			Deinterlace:    false,
			VideoCRF:       20,
			Preset:         "veryfast",
		},
		Source: ports.StreamSource{
			ID:   "1:0:19:13E:6:85:C00000:0:0:0",
			Type: ports.SourceTuner,
		},
	}

	adapter.setLastKnownFPS(fpsCacheKey(spec.Source, "http://example.com/live3"), 240) // out of range for default FPSMax=120
	args, err := adapter.buildArgs(context.Background(), spec, "http://example.com/live3")
	require.NoError(t, err)

	x264Params, ok := valueAfter(args, "-x264-params")
	require.True(t, ok, "x264 params should be present")
	assert.Contains(t, x264Params, "keyint=150:min-keyint=150:scenecut=0")
}

func TestBuildArgs_SafariDirtyUsesShortStartupSegments(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 45*time.Minute, 0, false, 2*time.Second, 6, 0, 0, "",
	)
	adapter.fpsProbeFn = func(context.Context, string) (int, string, error) {
		return 50, "r_frame_rate", nil
	}

	spec := ports.StreamSpec{
		SessionID: "safari-dirty-startup",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityStandard,
		Profile: model.ProfileSpec{
			Name:           "safari_dirty",
			TranscodeVideo: true,
			VideoCodec:     "h264",
			Deinterlace:    true,
			VideoCRF:       18,
			Preset:         "fast",
		},
		Source: ports.StreamSource{
			ID:   "1:0:19:132F:3EF:1:C00000:0:0:0",
			Type: ports.SourceTuner,
		},
	}

	args, err := adapter.buildArgs(context.Background(), spec, "http://example.com/live")
	require.NoError(t, err)

	hlsTime, ok := valueAfter(args, "-hls_time")
	require.True(t, ok)
	assert.Equal(t, "2", hlsTime)

	hlsInitTime, ok := valueAfter(args, "-hls_init_time")
	require.True(t, ok)
	assert.Equal(t, "1", hlsInitTime)

	hlsListSize, ok := valueAfter(args, "-hls_list_size")
	require.True(t, ok)
	assert.Equal(t, "1350", hlsListSize)

	x264Params, ok := valueAfter(args, "-x264-params")
	require.True(t, ok)
	assert.Contains(t, x264Params, "keyint=100:min-keyint=100:scenecut=0")

	forceKeyFrames, ok := valueAfter(args, "-force_key_frames")
	require.True(t, ok)
	assert.Equal(t, "expr:gte(t,n_forced*2)", forceKeyFrames)
}
