// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package hardware

import (
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

// AV1QualityCalibration returns the hardware/vendor-calibrated AV1 rate control parameters
// for a given GPU vendor, output encoder, and domain intent.
//
// Calibration is ONLY active when:
// 1. GPU vendor is Intel (GPUVendorIntel)
// 2. Output encoder is "av1_vaapi"
// 3. ICQ preflight rate control mode is verified on the active driver
// 4. Intent is IntentCinema or IntentQuality
func AV1QualityCalibration(
	vendor GPUVendor,
	encoder string,
	intent playbackprofile.PlaybackIntent,
) (mode string, qualityVal int, calibrated bool) {
	if vendor != GPUVendorIntel {
		return "", 0, false
	}
	if encoder != "av1_vaapi" {
		return "", 0, false
	}
	if !VAAPIRateControlVerified("av1_vaapi", RateControlICQ) {
		return "", 0, false
	}

	switch intent {
	case playbackprofile.IntentCinema:
		return RateControlICQ, 22, true
	case playbackprofile.IntentQuality:
		return RateControlICQ, 24, true
	default:
		return "", 0, false
	}
}
