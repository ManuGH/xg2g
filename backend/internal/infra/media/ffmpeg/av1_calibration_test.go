// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAV1IntentCalibrationAndNonLeakage(t *testing.T) {
	// Enable VAAPI ICQ preflight probe
	hardware.SetVAAPIRateControlCapabilities(map[string]map[string]bool{
		"av1_vaapi": {"ICQ": true},
	})

	t.Run("Intel + IntentCinema emits -rc_mode ICQ -global_quality 22 without bitrate caps", func(t *testing.T) {
		prof := ports.ProfileSpec{
			Name:          profiles.ProfileAV1HW,
			Intent:        playbackprofile.IntentCinema,
			VideoMaxRateK: 32000,
		}
		cfg := AdapterConfig{GPUVendor: string(hardware.GPUVendorIntel)}
		args := appendVaapiRateControlArgs(nil, prof, "av1", cfg)

		require.Contains(t, args, "-rc_mode")
		require.Contains(t, args, "ICQ")
		require.Contains(t, args, "-global_quality")
		require.Contains(t, args, "22")
		assert.NotContains(t, args, "-b:v")
		assert.NotContains(t, args, "-maxrate")
		assert.NotContains(t, args, "-bufsize")
	})

	t.Run("Intel + IntentQuality emits -rc_mode ICQ -global_quality 24 without bitrate caps", func(t *testing.T) {
		prof := ports.ProfileSpec{
			Name:          profiles.ProfileAV1HW,
			Intent:        playbackprofile.IntentQuality,
			VideoMaxRateK: 32000,
		}
		cfg := AdapterConfig{GPUVendor: string(hardware.GPUVendorIntel)}
		args := appendVaapiRateControlArgs(nil, prof, "av1", cfg)

		require.Contains(t, args, "-rc_mode")
		require.Contains(t, args, "ICQ")
		require.Contains(t, args, "-global_quality")
		require.Contains(t, args, "24")
		assert.NotContains(t, args, "-b:v")
		assert.NotContains(t, args, "-maxrate")
		assert.NotContains(t, args, "-bufsize")
	})

	t.Run("AMD vendor NEVER emits Intel ICQ Q22 or Q24", func(t *testing.T) {
		prof := ports.ProfileSpec{
			Name:          profiles.ProfileAV1HW,
			Intent:        playbackprofile.IntentCinema,
			VideoMaxRateK: 32000,
		}
		cfg := AdapterConfig{GPUVendor: string(hardware.GPUVendorAMD)}
		args := appendVaapiRateControlArgs(nil, prof, "av1", cfg)

		assert.NotContains(t, args, "ICQ")
		for i, arg := range args {
			if arg == "-global_quality" && i+1 < len(args) {
				assert.NotEqual(t, "22", args[i+1])
				assert.NotEqual(t, "24", args[i+1])
			}
		}
	})

	t.Run("NVIDIA vendor NEVER receives constqp 22 or 24", func(t *testing.T) {
		prof := ports.ProfileSpec{
			Name:          profiles.ProfileAV1HW,
			Intent:        playbackprofile.IntentCinema,
			VideoMaxRateK: 32000,
		}
		args := appendNVENCRateControlArgs(nil, prof)

		assert.NotContains(t, args, "constqp")
		for i, arg := range args {
			if arg == "-qp" && i+1 < len(args) {
				assert.NotEqual(t, "22", args[i+1])
				assert.NotEqual(t, "24", args[i+1])
			}
		}
	})

	t.Run("Generic ProfileAV1HW has VideoQP == 0", func(t *testing.T) {
		spec := profiles.Resolve(profiles.ProfileAV1HW, "", 0, nil, profiles.GPUBackendVAAPI, profiles.HWAccelAuto)
		assert.Equal(t, 0, spec.VideoQP)
	})
}
