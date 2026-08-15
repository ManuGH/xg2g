// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package hardware

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/stretchr/testify/assert"
)

func TestAV1QualityCalibration(t *testing.T) {
	// Set preflight verified for av1_vaapi ICQ
	SetVAAPIRateControlCapabilities(map[string]map[string]bool{
		"av1_vaapi": {"ICQ": true},
	})

	t.Run("Intel AV1 VAAPI Cinema resolves ICQ Q22", func(t *testing.T) {
		mode, q, ok := AV1QualityCalibration(GPUVendorIntel, "av1_vaapi", playbackprofile.IntentCinema)
		assert.True(t, ok)
		assert.Equal(t, RateControlICQ, mode)
		assert.Equal(t, 22, q)
	})

	t.Run("Intel AV1 VAAPI Quality resolves ICQ Q24", func(t *testing.T) {
		mode, q, ok := AV1QualityCalibration(GPUVendorIntel, "av1_vaapi", playbackprofile.IntentQuality)
		assert.True(t, ok)
		assert.Equal(t, RateControlICQ, mode)
		assert.Equal(t, 24, q)
	})

	t.Run("AMD vendor never inherits Intel ICQ values", func(t *testing.T) {
		mode, q, ok := AV1QualityCalibration(GPUVendorAMD, "av1_vaapi", playbackprofile.IntentCinema)
		assert.False(t, ok)
		assert.Equal(t, "", mode)
		assert.Equal(t, 0, q)
	})

	t.Run("NVIDIA vendor never inherits Intel ICQ values", func(t *testing.T) {
		mode, q, ok := AV1QualityCalibration(GPUVendorNVIDIA, "av1_nvenc", playbackprofile.IntentCinema)
		assert.False(t, ok)
		assert.Equal(t, "", mode)
		assert.Equal(t, 0, q)
	})

	t.Run("Unverified preflight blocks ICQ calibration", func(t *testing.T) {
		SetVAAPIRateControlCapabilities(map[string]map[string]bool{
			"av1_vaapi": {"ICQ": false},
		})
		mode, q, ok := AV1QualityCalibration(GPUVendorIntel, "av1_vaapi", playbackprofile.IntentCinema)
		assert.False(t, ok)
		assert.Equal(t, "", mode)
		assert.Equal(t, 0, q)
	})
}
