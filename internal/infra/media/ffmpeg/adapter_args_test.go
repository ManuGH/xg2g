package ffmpeg

import (
	"context"
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
