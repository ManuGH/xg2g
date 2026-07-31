// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package ffmpeg

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/stretchr/testify/assert"
)

// vendorConfig loads the adapter config and pins the GPU vendor, so rate-control
// expectations do not depend on whatever silicon the test machine happens to have.
func vendorConfig(vendor string) AdapterConfig {
	cfg := LoadAdapterConfig("", "")
	cfg.GPUVendor = vendor
	return cfg
}

func TestAppendVaapiRateControlArgs_AV1QVBR(t *testing.T) {
	prof := ports.ProfileSpec{VideoMaxRateK: 8000, VideoBufSizeK: 16000}

	t.Run("AMD: AV1 emits QVBR with b:v + maxrate cap + quality target", func(t *testing.T) {
		args := appendVaapiRateControlArgs(nil, prof, "av1", vendorConfig("amd"))

		rc, ok := valueAfter(args, "-rc_mode")
		assert.True(t, ok)
		assert.Equal(t, "QVBR", rc)

		// QVBR requires -b:v; keep the 75% target headroom.
		bv, ok := valueAfter(args, "-b:v")
		assert.True(t, ok, "QVBR must carry -b:v (else 'Bitrate must be set for QVBR RC mode')")
		assert.Equal(t, "6000k", bv)

		maxrate, _ := valueAfter(args, "-maxrate")
		assert.Equal(t, "8000k", maxrate, "maxrate stays the hard ceiling")

		gq, ok := valueAfter(args, "-global_quality")
		assert.True(t, ok)
		assert.Equal(t, "90", gq, "default quality target")

		assert.Contains(t, args, "-async_depth")
	})

	t.Run("XG2G_AV1_QVBR=false falls back to implicit VBR", func(t *testing.T) {
		t.Setenv("XG2G_AV1_QVBR", "false")
		args := appendVaapiRateControlArgs(nil, prof, "av1", vendorConfig("amd"))

		assert.NotContains(t, args, "-rc_mode", "disabled QVBR must not set an explicit rc_mode")
		assert.NotContains(t, args, "-global_quality")
		bv, _ := valueAfter(args, "-b:v")
		assert.Equal(t, "6000k", bv, "VBR fallback keeps the 75% target")
		maxrate, _ := valueAfter(args, "-maxrate")
		assert.Equal(t, "8000k", maxrate)
	})

	t.Run("XG2G_AV1_QVBR_QUALITY tunes the quality target", func(t *testing.T) {
		t.Setenv("XG2G_AV1_QVBR_QUALITY", "90")
		args := appendVaapiRateControlArgs(nil, prof, "av1", vendorConfig("amd"))
		gq, _ := valueAfter(args, "-global_quality")
		assert.Equal(t, "90", gq)
	})

	t.Run("non-AV1 (h264) is unaffected — no QVBR, no quality target", func(t *testing.T) {
		args := appendVaapiRateControlArgs(nil, prof, "h264", vendorConfig("amd"))
		assert.NotContains(t, args, "-rc_mode")
		assert.NotContains(t, args, "-global_quality")
		bv, _ := valueAfter(args, "-b:v")
		assert.Equal(t, "8000k", bv, "h264 keeps b:v == maxrate (no AV1 ring-stall workaround)")
	})

	t.Run("explicit VideoQP keeps the CQP branch (QVBR not applied)", func(t *testing.T) {
		cqp := ports.ProfileSpec{VideoQP: 110, VideoMaxRateK: 8000}
		args := appendVaapiRateControlArgs(nil, cqp, "av1", vendorConfig("amd"))
		rc, _ := valueAfter(args, "-rc_mode")
		assert.Equal(t, "CQP", rc)
		assert.NotContains(t, args, "-b:v", "CQP branch does not emit -b:v")
	})

	// Regression: an Intel iHD box rejects QVBR at encoder-open
	// ("Driver does not support QVBR RC mode (supported modes: CQP, CBR, VBR, ICQ)"),
	// which killed every AV1 session before the first frame.
	t.Run("Intel: AV1 must not request QVBR", func(t *testing.T) {
		args := appendVaapiRateControlArgs(nil, prof, "av1", vendorConfig("intel"))

		assert.NotContains(t, args, "-rc_mode", "iHD rejects QVBR and fails encoder-open")
		assert.NotContains(t, args, "-global_quality")
		bv, _ := valueAfter(args, "-b:v")
		assert.Equal(t, "6000k", bv, "VBR fallback keeps the 75% target headroom")
		maxrate, _ := valueAfter(args, "-maxrate")
		assert.Equal(t, "8000k", maxrate)
	})

	t.Run("unknown vendor stays on the safe VBR path", func(t *testing.T) {
		args := appendVaapiRateControlArgs(nil, prof, "av1", vendorConfig("unknown"))
		assert.NotContains(t, args, "-rc_mode", "never guess a vendor-specific RC mode")
	})
}

// The full-GPU AV1 chain was pinned off for every host because of an AMD output
// bug. On an Intel iGPU that cost 1.6x vs 7.6x realtime for 1080i25 -> 50p,
// since the software filters, not the encoder, are the bottleneck.
func TestAV1NeedsSoftwareNormalization(t *testing.T) {
	hd := ports.StreamSpec{Profile: ports.ProfileSpec{VideoSourceHeight: 1080}}
	sd := ports.StreamSpec{Profile: ports.ProfileSpec{VideoSourceHeight: 576}}
	unknownHeight := ports.StreamSpec{Profile: ports.ProfileSpec{}}

	t.Run("AMD keeps the software geometry normalization", func(t *testing.T) {
		assert.True(t, av1NeedsSoftwareNormalization(hd, "amd"))
	})

	t.Run("unknown vendor is treated like AMD", func(t *testing.T) {
		assert.True(t, av1NeedsSoftwareNormalization(hd, "unknown"))
		assert.True(t, av1NeedsSoftwareNormalization(hd, ""))
	})

	t.Run("Intel and NVIDIA may run the full GPU chain on HD", func(t *testing.T) {
		assert.False(t, av1NeedsSoftwareNormalization(hd, "intel"))
		assert.False(t, av1NeedsSoftwareNormalization(hd, "nvidia"))
	})

	// Apple's M-series AV1 decoder shows black video for SD-resolution AV1, and
	// the upscale that avoids it lives in the software filter chain.
	t.Run("sub-720p sources keep the software upscale on every vendor", func(t *testing.T) {
		assert.True(t, av1NeedsSoftwareNormalization(sd, "intel"))
		assert.True(t, av1NeedsSoftwareNormalization(sd, "nvidia"))
	})

	t.Run("unknown source height does not block the GPU chain", func(t *testing.T) {
		assert.False(t, av1NeedsSoftwareNormalization(unknownHeight, "intel"))
	})
}
