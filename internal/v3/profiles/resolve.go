// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package profiles

import (
	"strings"

	"github.com/ManuGH/xg2g/internal/v3/model"
)

const (
	ProfileAuto      = "auto"
	ProfileHigh      = "high"
	ProfileLow       = "low"
	ProfileDVR       = "dvr"
	ProfileSafari    = "safari"
	ProfileSafariDVR = "safari_dvr"
	ProfileCopy      = "copy"
)

var aliasMap = map[string]string{
	"":           ProfileAuto,
	"default":    ProfileAuto,
	"auto":       ProfileAuto,
	"hd":         ProfileHigh,
	"high":       ProfileHigh,
	"web_opt":    ProfileHigh,
	"standard":   ProfileHigh,
	"live":       ProfileHigh,
	"mobile":     ProfileLow,
	"low":        ProfileLow,
	"dvr":        ProfileDVR,
	"safari":     ProfileSafari,
	"safari_dvr": ProfileSafariDVR,
	"copy":       ProfileCopy,
}

// Resolve maps a requested profile and user agent to a concrete ProfileSpec.
// dvrWindowSec controls the DVR window for DVR profiles; <=0 disables DVR.
func Resolve(requested, userAgent string, dvrWindowSec int) model.ProfileSpec {
	requested = strings.ToLower(strings.TrimSpace(requested))
	canonical, ok := aliasMap[requested]
	if !ok {
		canonical = ProfileAuto
	}

	isSafari := isSafariUA(userAgent)
	if canonical == ProfileAuto {
		if isSafari {
			canonical = ProfileSafari
		} else {
			canonical = ProfileHigh
		}
	}

	if isSafari {
		if canonical == ProfileDVR || canonical == ProfileSafariDVR {
			canonical = ProfileSafariDVR
		} else if canonical != ProfileSafari {
			canonical = ProfileSafari
		}
	}

	spec := model.ProfileSpec{
		Name: canonical,
	}

	switch canonical {
	case ProfileCopy:
		return spec
	case ProfileLow:
		spec.TranscodeVideo = true
		spec.VideoCRF = 26
		spec.VideoMaxWidth = 1280
		spec.AudioBitrateK = 160
	case ProfileHigh:
		spec.TranscodeVideo = false // Default to copy (passthrough) for original quality
		if dvrWindowSec > 0 {
			spec.DVRWindowSec = dvrWindowSec
		}
	case ProfileSafari:
		// Revert to "High" profile (Stream Copy) as requested by user.
		// WARNING: This may cause decode errors on Safari if stream is broken.
		spec.TranscodeVideo = false
		spec.LLHLS = false
		if dvrWindowSec > 0 {
			spec.DVRWindowSec = dvrWindowSec
		}
	case ProfileSafariDVR:
		spec.TranscodeVideo = true
		spec.LLHLS = true
		spec.VideoCRF = 23
		if dvrWindowSec > 0 {
			spec.DVRWindowSec = dvrWindowSec
		}
	case ProfileDVR:
		spec.TranscodeVideo = true
		spec.VideoCRF = 23
		if dvrWindowSec > 0 {
			spec.DVRWindowSec = dvrWindowSec
		}
	}

	return spec
}

func isSafariUA(ua string) bool {
	if ua == "" {
		return false
	}
	// Chrome also includes "Safari", so check for "Safari" AND NOT "Chrome".
	return strings.Contains(ua, "Safari") &&
		!strings.Contains(ua, "Chrome") &&
		!strings.Contains(ua, "Chromium")
}
