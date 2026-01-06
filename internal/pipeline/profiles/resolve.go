// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package profiles

import (
	"strings"

	"github.com/ManuGH/xg2g/internal/pipeline/model"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
)

const (
	ProfileAuto           = "auto"
	ProfileHigh           = "high"
	ProfileLow            = "low"
	ProfileDVR            = "dvr"
	ProfileSafari         = "safari"
	ProfileSafariDVR      = "safari_dvr"
	ProfileSafariHEVC     = "safari_hevc"
	ProfileSafariHEVCHW   = "safari_hevc_hw"    // GPU-accelerated HEVC
	ProfileSafariHEVCHWLL = "safari_hevc_hw_ll" // GPU-accelerated HEVC + LL-HLS
	ProfileCopy           = "copy"
	ProfileRepair         = "repair" // High + Transcode (Rescue Mode)
)

var aliasMap = map[string]string{
	"":                  ProfileAuto,
	"default":           ProfileAuto,
	"auto":              ProfileAuto,
	"hd":                ProfileHigh,
	"high":              ProfileHigh,
	"web_opt":           ProfileHigh,
	"standard":          ProfileHigh,
	"live":              ProfileHigh,
	"mobile":            ProfileLow,
	"low":               ProfileLow,
	"dvr":               ProfileDVR,
	"safari":            ProfileSafari,
	"safari_dvr":        ProfileSafariDVR,
	"safari_hevc":       ProfileSafariHEVC,
	"safari_hevc_hw":    ProfileSafariHEVCHW,
	"safari_hevc_hw_ll": ProfileSafariHEVCHWLL,
	"copy":              ProfileCopy,
}

type HWAccelMode string

const (
	HWAccelAuto  HWAccelMode = "auto"  // Server decides based on GPU availability
	HWAccelForce HWAccelMode = "force" // Force GPU (fail if unavailable)
	HWAccelOff   HWAccelMode = "off"   // Force CPU
)

// shouldUseGPU determines whether to use GPU acceleration based on availability and user override
func shouldUseGPU(hasGPU bool, mode HWAccelMode) bool {
	switch mode {
	case HWAccelForce:
		return true // Force GPU (will fail later if unavailable)
	case HWAccelOff:
		return false // Force CPU
	case HWAccelAuto:
		return hasGPU // Auto: use GPU if available
	default:
		return hasGPU
	}
}

// Resolve maps a requested profile and user agent to a concrete ProfileSpec.
// dvrWindowSec controls the DVR window for DVR profiles; <=0 disables DVR.
// hwaccelMode allows explicit GPU/CPU override (default: auto).
func Resolve(requested, userAgent string, dvrWindowSec int, cap *scan.Capability, hasGPU bool, hwaccelMode HWAccelMode) model.ProfileSpec {
	requested = strings.ToLower(strings.TrimSpace(requested))
	canonical, ok := aliasMap[requested]
	if !ok {
		canonical = ProfileAuto
	}

	isSafari := isSafariUA(userAgent)
	if canonical == ProfileAuto {
		if isSafari {
			// Safari browser does NOT support HEVC in MSE/HLS.js
			// Use H.264 (safari profile) for browser compatibility
			// safari_hevc is opt-in only for testing/native apps
			canonical = ProfileSafari
		} else {
			canonical = ProfileHigh
		}
	}

	// REMOVED: Server-side Safari profile override
	// Frontend now controls profile switching explicitly based on fullscreen state
	// This ensures inline playback uses custom controls, fullscreen uses native DVR

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
		spec.AudioBitrateK = 192    // FORCE AAC: Browsers cannot decode MP2/AC3 natively
		if dvrWindowSec > 0 {
			spec.DVRWindowSec = dvrWindowSec
		}
	case ProfileSafari:
		// Smart Profile Logic
		if cap != nil && !cap.Interlaced {
			// Progressive -> Direct Remux (Original Quality)
			spec.TranscodeVideo = false
			spec.Container = "fmp4"
			spec.AudioBitrateK = 192
			// HWAccel disabled for passthrough
		} else {
			// Interlaced or Unknown -> Transcode + Deinterlace
			spec.TranscodeVideo = true
			spec.Deinterlace = true
			spec.Container = "fmp4"
			spec.AudioBitrateK = 192

			// HWAccel Decision (respects override)
			useGPU := shouldUseGPU(hasGPU, hwaccelMode)

			if useGPU {
				// GPU Acceleration (High Quality)
				spec.HWAccel = "vaapi"
				spec.VideoCodec = "h264"
				spec.VideoCRF = 16
				spec.VideoMaxRateK = 20000
				spec.VideoBufSizeK = 40000
			} else {
				// CPU Fallback (Safe Quality)
				spec.VideoCodec = "libx264"
				spec.VideoCRF = 18
				spec.VideoMaxRateK = 8000
				spec.VideoBufSizeK = 16000
				spec.Preset = "veryfast"
			}
		}

		spec.LLHLS = false
		if dvrWindowSec > 0 {
			spec.DVRWindowSec = dvrWindowSec
		}
	case ProfileSafariDVR:
		spec.TranscodeVideo = true
		spec.Deinterlace = true
		spec.LLHLS = true
		spec.VideoCRF = 23
		spec.AudioBitrateK = 192
		if dvrWindowSec > 0 {
			spec.DVRWindowSec = dvrWindowSec
		}
	case ProfileDVR:
		spec.TranscodeVideo = true
		spec.Deinterlace = true
		spec.VideoCRF = 23
		if dvrWindowSec > 0 {
			spec.DVRWindowSec = dvrWindowSec
		}
	case ProfileSafariHEVC:
		// Experimental: HEVC Live Transcoding (CPU)
		// Strict constraints for Apple HLS compatibility (fMP4 implied by args builder)
		spec.TranscodeVideo = true
		spec.VideoCodec = "hevc"
		spec.Deinterlace = true
		spec.VideoCRF = 22        // Conservative start for x265
		spec.VideoMaxRateK = 5000 // Strict VBV Cap
		spec.VideoBufSizeK = 10000
		spec.BFrames = 2 // B-Frames now work with FFmpeg master (sdtp bug fixed)
		spec.AudioBitrateK = 192
	case ProfileSafariHEVCHW:
		// GPU-Accelerated HEVC (VAAPI) - Recommended for multi-stream
		// 10x faster than CPU, ~10% CPU usage per stream
		spec.TranscodeVideo = true
		spec.VideoCodec = "hevc"
		spec.Deinterlace = true
		spec.VideoMaxRateK = 5000 // VBV Cap
		spec.VideoBufSizeK = 10000
		spec.AudioBitrateK = 192
		// Note: VAAPI doesn't use CRF, uses constant quality mode instead

		// HWAccel Decision (respects override)
		if shouldUseGPU(hasGPU, hwaccelMode) {
			spec.HWAccel = "vaapi"
		} else {
			// CPU Fallback: x265
			spec.VideoCodec = "hevc"
			spec.VideoCRF = 22
		}

	case ProfileSafariHEVCHWLL:
		// GPU-Accelerated HEVC with LL-HLS - Ultra-low latency streaming
		// Combines GPU encoding (~10% CPU) with LL-HLS (<3s latency)
		spec.TranscodeVideo = true
		spec.VideoCodec = "hevc"
		spec.Deinterlace = true
		spec.LLHLS = true // Enable Low-Latency HLS with 0.5s part-segments
		spec.VideoMaxRateK = 5000
		spec.VideoBufSizeK = 10000
		spec.AudioBitrateK = 192

		// HWAccel Decision (respects override)
		if shouldUseGPU(hasGPU, hwaccelMode) {
			spec.HWAccel = "vaapi"
		} else {
			// CPU Fallback: x265 with LL-HLS
			spec.VideoCodec = "hevc"
			spec.VideoCRF = 22
		}

		if dvrWindowSec > 0 {
			spec.DVRWindowSec = dvrWindowSec
		}
	case ProfileRepair:
		// RESCUE MODE: Force Transcode to repair timestamps/GOP
		spec.TranscodeVideo = true
		spec.Deinterlace = false // Keep simple unless needed
		spec.VideoCRF = 24       // Slightly lower qual for speed?
		spec.VideoMaxWidth = 1280
		spec.AudioBitrateK = 192 // Ensure audio is clean too
	}

	return spec
}

func isSafariUA(ua string) bool {
	if ua == "" {
		return false
	}
	ua = strings.ToLower(ua)
	if strings.Contains(ua, "iphone") || strings.Contains(ua, "ipad") || strings.Contains(ua, "ipod") {
		return true
	}
	// Chrome also includes "Safari", so check for "Safari" AND NOT "Chrome".
	return strings.Contains(ua, "safari") &&
		!strings.Contains(ua, "chrome") &&
		!strings.Contains(ua, "chromium") &&
		!strings.Contains(ua, "crios") &&
		!strings.Contains(ua, "fxios") &&
		!strings.Contains(ua, "edgios")
}
