// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package ffmpeg

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/stretchr/testify/assert"
)

func TestTranscodeSharpenFilter(t *testing.T) {
	t.Run("default is a clean luma unsharp", func(t *testing.T) {
		assert.Equal(t, "unsharp=5:5:1.50:5:5:0.0", transcodeSharpenFilter())
	})

	t.Run("zero disables sharpening", func(t *testing.T) {
		t.Setenv("XG2G_TRANSCODE_SHARPEN", "0")
		assert.Equal(t, "", transcodeSharpenFilter())
	})

	t.Run("tunable amount", func(t *testing.T) {
		t.Setenv("XG2G_TRANSCODE_SHARPEN", "0.7")
		assert.Equal(t, "unsharp=5:5:0.70:5:5:0.0", transcodeSharpenFilter())
	})

	t.Run("clamped to 3.0", func(t *testing.T) {
		t.Setenv("XG2G_TRANSCODE_SHARPEN", "5")
		assert.Equal(t, "unsharp=5:5:3.00:5:5:0.0", transcodeSharpenFilter())
	})
}

func TestTranscodeDenoiseFilter(t *testing.T) {
	t.Run("default scales the conservative base", func(t *testing.T) {
		// default 0.6 * base 4:3:6:4
		assert.Equal(t, "hqdn3d=2.4:1.8:3.6:2.4", transcodeDenoiseFilter())
	})

	t.Run("zero disables", func(t *testing.T) {
		t.Setenv("XG2G_TRANSCODE_DENOISE", "0")
		assert.Equal(t, "", transcodeDenoiseFilter())
	})

	t.Run("full strength is the base", func(t *testing.T) {
		t.Setenv("XG2G_TRANSCODE_DENOISE", "1.0")
		assert.Equal(t, "hqdn3d=4.0:3.0:6.0:4.0", transcodeDenoiseFilter())
	})

	t.Run("clamped to 1.5", func(t *testing.T) {
		t.Setenv("XG2G_TRANSCODE_DENOISE", "5")
		assert.Equal(t, "hqdn3d=6.0:4.5:9.0:6.0", transcodeDenoiseFilter())
	})
}

func TestTranscodeDebandFilter(t *testing.T) {
	t.Run("default on", func(t *testing.T) {
		assert.Equal(t, "deband", transcodeDebandFilter())
	})

	t.Run("disable via env", func(t *testing.T) {
		t.Setenv("XG2G_TRANSCODE_DEBAND", "false")
		assert.Equal(t, "", transcodeDebandFilter())
	})
}

func TestSourceAwareTranscodeFilters(t *testing.T) {
	t.Run("known 1080p preserves native detail", func(t *testing.T) {
		profile := ports.ProfileSpec{VideoSourceHeight: 1080}
		assert.Empty(t, transcodeDenoiseFilterForProfile(profile))
		assert.Equal(t, "unsharp=5:5:0.35:5:5:0.0", transcodeSharpenFilterForProfile(profile))
		assert.Empty(t, transcodeDebandFilterForProfile(profile))
	})

	t.Run("720p uses only mild cleanup", func(t *testing.T) {
		profile := ports.ProfileSpec{VideoSourceHeight: 720}
		assert.Equal(t, "hqdn3d=1.2:0.9:1.8:1.2", transcodeDenoiseFilterForProfile(profile))
		assert.Equal(t, "unsharp=5:5:0.75:5:5:0.0", transcodeSharpenFilterForProfile(profile))
		assert.Empty(t, transcodeDebandFilterForProfile(profile))
	})

	t.Run("SD retains broadcast cleanup", func(t *testing.T) {
		profile := ports.ProfileSpec{VideoSourceHeight: 576}
		assert.Equal(t, "hqdn3d=2.4:1.8:3.6:2.4", transcodeDenoiseFilterForProfile(profile))
		assert.Equal(t, "unsharp=5:5:1.00:5:5:0.0", transcodeSharpenFilterForProfile(profile))
		assert.Equal(t, "deband", transcodeDebandFilterForProfile(profile))
	})

	t.Run("unknown source preserves previous defaults", func(t *testing.T) {
		profile := ports.ProfileSpec{}
		assert.Equal(t, "hqdn3d=2.4:1.8:3.6:2.4", transcodeDenoiseFilterForProfile(profile))
		assert.Equal(t, "unsharp=5:5:1.50:5:5:0.0", transcodeSharpenFilterForProfile(profile))
		assert.Equal(t, "deband", transcodeDebandFilterForProfile(profile))
	})

	t.Run("explicit operator values override source defaults", func(t *testing.T) {
		t.Setenv("XG2G_TRANSCODE_DENOISE", "0.2")
		t.Setenv("XG2G_TRANSCODE_SHARPEN", "0.9")
		t.Setenv("XG2G_TRANSCODE_DEBAND", "true")
		profile := ports.ProfileSpec{VideoSourceHeight: 1080}
		assert.Equal(t, "hqdn3d=0.8:0.6:1.2:0.8", transcodeDenoiseFilterForProfile(profile))
		assert.Equal(t, "unsharp=5:5:0.90:5:5:0.0", transcodeSharpenFilterForProfile(profile))
		assert.Equal(t, "deband", transcodeDebandFilterForProfile(profile))
	})
}
