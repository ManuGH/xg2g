// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package profiles

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestHWAccelForceWithoutGPU verifies that force mode with no GPU available
// is handled gracefully (in this case, by the handler rejecting the request).
// The resolver still returns HWAccel="vaapi" which will fail at FFmpeg init,
// but the handler should prevent this from reaching FFmpeg.
func TestHWAccelForceWithoutGPU(t *testing.T) {
	spec := Resolve("safari", "Safari/17.0", 10800, nil, false, HWAccelForce)

	// Resolver allows force even without GPU (handler validates)
	assert.True(t, spec.TranscodeVideo, "Safari interlaced should transcode")
	assert.Equal(t, "vaapi", spec.HWAccel, "force mode sets vaapi even without GPU")

	// Handler is responsible for validation (see handlers_v3.go:218-224)
	// This test documents that the resolver doesn't validate availability
}

// TestHWAccelOffAlwaysCPU verifies that off mode always uses CPU encoding,
// regardless of GPU availability.
func TestHWAccelOffAlwaysCPU(t *testing.T) {
	tests := []struct {
		name   string
		hasGPU bool
	}{
		{"off+gpu → cpu", true},
		{"off+no-gpu → cpu", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := Resolve("safari", "Safari/17.0", 10800, nil, tt.hasGPU, HWAccelOff)

			assert.True(t, spec.TranscodeVideo, "Safari interlaced should transcode")
			assert.Empty(t, spec.HWAccel, "off mode must disable HWAccel")
			assert.Equal(t, "libx264", spec.VideoCodec, "off mode uses CPU encoder")
		})
	}
}

// TestHWAccelAutoRespectGPU verifies that auto mode uses GPU when available,
// CPU when not available.
func TestHWAccelAutoRespectGPU(t *testing.T) {
	tests := []struct {
		name            string
		hasGPU          bool
		expectedHWAccel string
		expectedCodec   string
	}{
		{"auto+gpu → vaapi", true, "vaapi", "h264"},
		{"auto+no-gpu → cpu", false, "", "libx264"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := Resolve("safari", "Safari/17.0", 10800, nil, tt.hasGPU, HWAccelAuto)

			assert.True(t, spec.TranscodeVideo, "Safari interlaced should transcode")
			assert.Equal(t, tt.expectedHWAccel, spec.HWAccel, "auto mode respects GPU availability")
			assert.Equal(t, tt.expectedCodec, spec.VideoCodec, "codec matches hwaccel mode")
		})
	}
}

// TestHWAccelHEVCProfiles verifies that HEVC profiles respect hwaccel override
func TestHWAccelHEVCProfiles(t *testing.T) {
	tests := []struct {
		name            string
		profile         string
		hwaccel         HWAccelMode
		hasGPU          bool
		expectedHWAccel string
		expectedCodec   string
	}{
		{"safari_hevc_hw+auto+gpu → vaapi", "safari_hevc_hw", HWAccelAuto, true, "vaapi", "hevc"},
		{"safari_hevc_hw+auto+no-gpu → x265", "safari_hevc_hw", HWAccelAuto, false, "", "hevc"},
		{"safari_hevc_hw+off+gpu → x265", "safari_hevc_hw", HWAccelOff, true, "", "hevc"},
		{"safari_hevc_hw_ll+force+gpu → vaapi+llhls", "safari_hevc_hw_ll", HWAccelForce, true, "vaapi", "hevc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := Resolve(tt.profile, "Safari/17.0", 10800, nil, tt.hasGPU, tt.hwaccel)

			assert.Equal(t, tt.expectedHWAccel, spec.HWAccel, "hwaccel mode")
			assert.Equal(t, tt.expectedCodec, spec.VideoCodec, "codec")

			if tt.profile == "safari_hevc_hw_ll" {
				assert.True(t, spec.LLHLS, "LL-HLS must be enabled for safari_hevc_hw_ll")
			}
		})
	}
}

// TestHWAccelPassthroughIgnored verifies that hwaccel is ignored for passthrough (copy) mode
func TestHWAccelPassthroughIgnored(t *testing.T) {
	spec := Resolve("copy", "Safari/17.0", 10800, nil, true, HWAccelForce)

	assert.False(t, spec.TranscodeVideo, "copy profile is passthrough")
	assert.Empty(t, spec.HWAccel, "passthrough ignores hwaccel")
}

// TestHWAccelDeterminism verifies that same inputs produce same outputs (reproducibility)
func TestHWAccelDeterminism(t *testing.T) {
	// Same inputs → same outputs (deterministic)
	spec1 := Resolve("safari", "Safari/17.0", 10800, nil, false, HWAccelOff)
	spec2 := Resolve("safari", "Safari/17.0", 10800, nil, false, HWAccelOff)

	assert.Equal(t, spec1.HWAccel, spec2.HWAccel, "deterministic hwaccel")
	assert.Equal(t, spec1.VideoCodec, spec2.VideoCodec, "deterministic codec")
	assert.Equal(t, spec1.VideoCRF, spec2.VideoCRF, "deterministic quality")
}
