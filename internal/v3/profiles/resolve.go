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
	case ProfileLow:
		spec.TranscodeVideo = true
	case ProfileSafari:
		spec.TranscodeVideo = true
		spec.LLHLS = true
	case ProfileSafariDVR:
		spec.TranscodeVideo = true
		spec.LLHLS = true
		if dvrWindowSec > 0 {
			spec.DVRWindowSec = dvrWindowSec
		}
	case ProfileDVR:
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
