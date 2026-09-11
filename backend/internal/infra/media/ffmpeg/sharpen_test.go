// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package ffmpeg

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTranscodeSharpenFilter(t *testing.T) {
	t.Run("default is a clean luma unsharp", func(t *testing.T) {
		assert.Equal(t, "unsharp=5:5:2.00:5:5:0.0", transcodeSharpenFilter(LoadAdapterConfig("", "").TranscodeSharpen))
	})

	t.Run("zero disables sharpening", func(t *testing.T) {
		t.Setenv("XG2G_TRANSCODE_SHARPEN", "0")
		assert.Equal(t, "", transcodeSharpenFilter(LoadAdapterConfig("", "").TranscodeSharpen))
	})

	t.Run("tunable amount", func(t *testing.T) {
		t.Setenv("XG2G_TRANSCODE_SHARPEN", "0.7")
		assert.Equal(t, "unsharp=5:5:0.70:5:5:0.0", transcodeSharpenFilter(LoadAdapterConfig("", "").TranscodeSharpen))
	})

	t.Run("clamped to 3.0", func(t *testing.T) {
		t.Setenv("XG2G_TRANSCODE_SHARPEN", "5")
		assert.Equal(t, "unsharp=5:5:3.00:5:5:0.0", transcodeSharpenFilter(LoadAdapterConfig("", "").TranscodeSharpen))
	})
}

func TestVaapiSharpnessFilter(t *testing.T) {
	t.Run("intel default maps 2.0 to hardware 44", func(t *testing.T) {
		assert.Equal(t, "sharpness_vaapi=sharpness=44", vaapiSharpnessFilter(2.0, "intel"))
	})

	t.Run("zero disables sharpening", func(t *testing.T) {
		assert.Equal(t, "", vaapiSharpnessFilter(0, "intel"))
	})

	t.Run("negative amount disables sharpening", func(t *testing.T) {
		assert.Equal(t, "", vaapiSharpnessFilter(-0.5, "intel"))
	})

	t.Run("tunable amount", func(t *testing.T) {
		assert.Equal(t, "sharpness_vaapi=sharpness=22", vaapiSharpnessFilter(1.0, "intel"))
	})

	t.Run("clamped to 64", func(t *testing.T) {
		assert.Equal(t, "sharpness_vaapi=sharpness=64", vaapiSharpnessFilter(3.0, "intel"))
		assert.Equal(t, "sharpness_vaapi=sharpness=64", vaapiSharpnessFilter(5.0, "intel"))
	})

	t.Run("amd disables sharpness_vaapi because driver lacks VPP sharpening", func(t *testing.T) {
		assert.Equal(t, "", vaapiSharpnessFilter(2.0, "amd"))
	})

	t.Run("unknown vendor disables sharpness_vaapi", func(t *testing.T) {
		assert.Equal(t, "", vaapiSharpnessFilter(2.0, "unknown"))
	})
}

func TestVaapiDenoiseFilter(t *testing.T) {
	t.Run("intel default maps 0.6 to hardware 12", func(t *testing.T) {
		assert.Equal(t, "denoise_vaapi=denoise=12", vaapiDenoiseFilter(0.6, "intel"))
	})

	t.Run("staging 0.5 maps to hardware 10", func(t *testing.T) {
		assert.Equal(t, "denoise_vaapi=denoise=10", vaapiDenoiseFilter(0.5, "intel"))
	})

	t.Run("zero disables denoise", func(t *testing.T) {
		assert.Equal(t, "", vaapiDenoiseFilter(0, "intel"))
	})

	t.Run("negative amount disables denoise", func(t *testing.T) {
		assert.Equal(t, "", vaapiDenoiseFilter(-0.5, "intel"))
	})

	t.Run("tunable amount", func(t *testing.T) {
		assert.Equal(t, "denoise_vaapi=denoise=20", vaapiDenoiseFilter(1.0, "intel"))
	})

	t.Run("clamped to 64", func(t *testing.T) {
		assert.Equal(t, "denoise_vaapi=denoise=30", vaapiDenoiseFilter(1.5, "intel"))
		assert.Equal(t, "denoise_vaapi=denoise=64", vaapiDenoiseFilter(5.0, "intel"))
	})

	t.Run("amd disables denoise_vaapi because driver lacks VPP denoise", func(t *testing.T) {
		assert.Equal(t, "", vaapiDenoiseFilter(0.6, "amd"))
	})

	t.Run("unknown vendor disables denoise_vaapi", func(t *testing.T) {
		assert.Equal(t, "", vaapiDenoiseFilter(0.6, "unknown"))
	})
}

func TestTranscodeDenoiseFilter(t *testing.T) {
	t.Run("default scales the conservative base", func(t *testing.T) {
		// default 0.6 * base 4:3:6:4
		assert.Equal(t, "hqdn3d=2.4:1.8:3.6:2.4", transcodeDenoiseFilter(LoadAdapterConfig("", "").TranscodeDenoise))
	})

	t.Run("zero disables", func(t *testing.T) {
		t.Setenv("XG2G_TRANSCODE_DENOISE", "0")
		assert.Equal(t, "", transcodeDenoiseFilter(LoadAdapterConfig("", "").TranscodeDenoise))
	})

	t.Run("full strength is the base", func(t *testing.T) {
		t.Setenv("XG2G_TRANSCODE_DENOISE", "1.0")
		assert.Equal(t, "hqdn3d=4.0:3.0:6.0:4.0", transcodeDenoiseFilter(LoadAdapterConfig("", "").TranscodeDenoise))
	})

	t.Run("clamped to 1.5", func(t *testing.T) {
		t.Setenv("XG2G_TRANSCODE_DENOISE", "5")
		assert.Equal(t, "hqdn3d=6.0:4.5:9.0:6.0", transcodeDenoiseFilter(LoadAdapterConfig("", "").TranscodeDenoise))
	})
}

func TestTranscodeDebandFilter(t *testing.T) {
	t.Run("default on", func(t *testing.T) {
		assert.Equal(t, "deband", transcodeDebandFilter(LoadAdapterConfig("", "").TranscodeDeband))
	})

	t.Run("disable via env", func(t *testing.T) {
		t.Setenv("XG2G_TRANSCODE_DEBAND", "false")
		assert.Equal(t, "", transcodeDebandFilter(LoadAdapterConfig("", "").TranscodeDeband))
	})
}
