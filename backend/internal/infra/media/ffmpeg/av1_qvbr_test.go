// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package ffmpeg

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/stretchr/testify/assert"
)

// withProbedQVBR publishes a rate-control probe result, so the expectations
// below depend on what a driver was measured to accept rather than on whatever
// silicon the test machine happens to have.
func withProbedQVBR(t *testing.T, supported bool) AdapterConfig {
	return withProbedQVBRForVendor(t, supported, "intel")
}

func withProbedQVBRForVendor(t *testing.T, supported bool, vendor string) AdapterConfig {
	t.Helper()
	hardware.SetVAAPIRateControlCapabilities(map[string]map[string]bool{
		"av1_vaapi":  {hardware.RateControlQVBR: supported},
		"hevc_vaapi": {hardware.RateControlQVBR: supported},
		"h264_vaapi": {hardware.RateControlQVBR: supported},
	})
	t.Cleanup(func() { hardware.SetVAAPIRateControlCapabilities(nil) })
	cfg := LoadAdapterConfig("", "")
	cfg.GPUVendor = vendor
	return cfg
}

func TestAppendVaapiRateControlArgs_AV1QVBR(t *testing.T) {
	prof := ports.ProfileSpec{VideoMaxRateK: 8000, VideoBufSizeK: 16000}

	t.Run("probed QVBR: AV1 emits it with b:v + maxrate cap + quality target", func(t *testing.T) {
		args := appendVaapiRateControlArgs(nil, prof, "av1", withProbedQVBR(t, true))

		rc, ok := valueAfter(args, "-rc_mode")
		assert.True(t, ok)
		assert.Equal(t, "QVBR", rc)

		// QVBR requires -b:v; keep the 75% target headroom.
		bv, ok := valueAfter(args, "-b:v")
		assert.True(t, ok, "QVBR must carry -b:v (else 'Bitrate must be set for QVBR RC mode')")
		assert.Equal(t, "8000k", bv, "off AMD the target is the ceiling, no ring-stall headroom")

		maxrate, _ := valueAfter(args, "-maxrate")
		assert.Equal(t, "8000k", maxrate, "maxrate stays the hard ceiling")

		gq, ok := valueAfter(args, "-global_quality")
		assert.True(t, ok)
		assert.Equal(t, "90", gq, "default quality target")

		assert.Contains(t, args, "-async_depth")
	})

	t.Run("XG2G_AV1_QVBR=false falls back to implicit VBR", func(t *testing.T) {
		t.Setenv("XG2G_AV1_QVBR", "false")
		args := appendVaapiRateControlArgs(nil, prof, "av1", withProbedQVBR(t, true))

		assert.NotContains(t, args, "-rc_mode", "disabled QVBR must not set an explicit rc_mode")
		assert.NotContains(t, args, "-global_quality")
		bv, _ := valueAfter(args, "-b:v")
		assert.Equal(t, "8000k", bv, "VBR fallback also spends the full ceiling off AMD")
		maxrate, _ := valueAfter(args, "-maxrate")
		assert.Equal(t, "8000k", maxrate)
	})

	t.Run("XG2G_AV1_QVBR_QUALITY tunes the quality target", func(t *testing.T) {
		t.Setenv("XG2G_AV1_QVBR_QUALITY", "90")
		args := appendVaapiRateControlArgs(nil, prof, "av1", withProbedQVBR(t, true))
		gq, _ := valueAfter(args, "-global_quality")
		assert.Equal(t, "90", gq)
	})

	t.Run("non-AV1 (h264) is unaffected — no QVBR, no quality target", func(t *testing.T) {
		args := appendVaapiRateControlArgs(nil, prof, "h264", withProbedQVBR(t, true))
		assert.NotContains(t, args, "-rc_mode")
		assert.NotContains(t, args, "-global_quality")
		bv, _ := valueAfter(args, "-b:v")
		assert.Equal(t, "8000k", bv, "h264 keeps b:v == maxrate (no AV1 ring-stall workaround)")
	})

	t.Run("explicit VideoQP keeps the CQP branch (QVBR not applied)", func(t *testing.T) {
		cqp := ports.ProfileSpec{VideoQP: 110, VideoMaxRateK: 8000}
		args := appendVaapiRateControlArgs(nil, cqp, "av1", withProbedQVBR(t, true))
		rc, _ := valueAfter(args, "-rc_mode")
		assert.Equal(t, "CQP", rc)
		assert.NotContains(t, args, "-b:v", "CQP branch does not emit -b:v")
	})

	// Regression: an Intel iHD box rejects QVBR at encoder-open
	// ("Driver does not support QVBR RC mode (supported modes: CQP, CBR, VBR, ICQ)"),
	// which killed every AV1 session before the first frame.
	t.Run("driver rejected QVBR: AV1 must not request it", func(t *testing.T) {
		args := appendVaapiRateControlArgs(nil, prof, "av1", withProbedQVBR(t, false))

		assert.NotContains(t, args, "-rc_mode", "a rejected mode fails encoder-open")
		assert.NotContains(t, args, "-global_quality")
		bv, _ := valueAfter(args, "-b:v")
		assert.Equal(t, "8000k", bv, "VBR fallback also spends the full ceiling off AMD")
		maxrate, _ := valueAfter(args, "-maxrate")
		assert.Equal(t, "8000k", maxrate)
	})

	// Fail-safe: without a probe result nothing is known, and an unproven mode
	// costs the viewer the whole stream rather than some quality.
	t.Run("unprobed driver stays on the safe VBR path", func(t *testing.T) {
		hardware.SetVAAPIRateControlCapabilities(nil)
		args := appendVaapiRateControlArgs(nil, prof, "av1", LoadAdapterConfig("", ""))
		assert.NotContains(t, args, "-rc_mode", "never emit an unproven RC mode")
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

// The 25% target headroom is an AMD VCN ring-stall workaround. Keeping it on
// other silicon handed back a quarter of the bitrate ceiling for free.
func TestAppendVaapiRateControlArgs_RingStallHeadroomIsAMDOnly(t *testing.T) {
	prof := ports.ProfileSpec{VideoMaxRateK: 8000, VideoBufSizeK: 16000}

	amd, _ := valueAfter(appendVaapiRateControlArgs(nil, prof, "av1", withProbedQVBRForVendor(t, true, "amd")), "-b:v")
	assert.Equal(t, "6000k", amd, "75% of the 8000k ceiling")

	intel, _ := valueAfter(appendVaapiRateControlArgs(nil, prof, "av1", withProbedQVBRForVendor(t, true, "intel")), "-b:v")
	assert.Equal(t, "8000k", intel, "no ring stall here, so no headroom to give away")
}
