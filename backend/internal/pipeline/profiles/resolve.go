// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package profiles

import (
	"strconv"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/normalize"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
)

const (
	ProfileAuto            = "auto"
	ProfileHigh            = "high"
	ProfileLow             = "low"
	ProfileDVR             = "dvr"
	ProfileSafari          = "safari"
	ProfileSafariDirty     = "safari_dirty"
	ProfileSafariDVR       = "safari_dvr"
	ProfileSafariHEVC      = "safari_hevc"
	ProfileSafariHEVCHW    = "safari_hevc_hw"    // GPU-accelerated HEVC
	ProfileSafariHEVCHWLL  = "safari_hevc_hw_ll" // GPU-accelerated HEVC + LL-HLS
	ProfileSafariRuntimeHQ = "safari_runtime_hq" // Internal runtime hardening override, not a requested profile
	ProfileAV1HW           = "av1_hw"            // GPU-accelerated AV1 (VAAPI only)
	ProfileH264FMP4        = "h264_fmp4"         // Always transcode H.264 + fMP4 (optional VAAPI)
	ProfileAndroid         = "android"           // Android native: video copy + AAC + mpegts
	ProfileCopy            = "copy"
	ProfileRepair          = "repair" // High + Transcode (Rescue Mode)

	PublicProfileCompatible = string(playbackprofile.IntentCompatible)
	PublicProfileBandwidth  = "bandwidth" // Deprecated legacy alias; quality ladder migration removes this later.
	PublicProfileDirect     = string(playbackprofile.IntentDirect)
	PublicProfileQuality    = string(playbackprofile.IntentQuality)
	PublicProfileRepair     = string(playbackprofile.IntentRepair)
)

var aliasMap = map[string]string{
	"":                  ProfileAuto,
	"default":           ProfileAuto,
	"auto":              ProfileAuto,
	"hd":                ProfileHigh,
	"high":              ProfileHigh,
	"compatible":        ProfileHigh,
	"quality":           ProfileHigh,
	"web_opt":           ProfileHigh,
	"standard":          ProfileHigh,
	"live":              ProfileHigh,
	"mobile":            ProfileLow,
	"low":               ProfileLow,
	"bandwidth":         ProfileLow,
	"dvr":               ProfileDVR,
	"safari":            ProfileSafari,
	"safari_dirty":      ProfileSafariDirty,
	"safari_dvr":        ProfileSafariDVR,
	"safari_hevc":       ProfileSafariHEVC,
	"safari_hevc_hw":    ProfileSafariHEVCHW,
	"safari_hevc_hw_ll": ProfileSafariHEVCHWLL,
	"safari_runtime_hq": ProfileSafariRuntimeHQ,
	"safari_hq":         ProfileSafariRuntimeHQ, // Legacy internal name kept for persisted sessions and traces.
	"av1_hw":            ProfileAV1HW,
	"h264_fmp4":         ProfileH264FMP4,
	"android":           ProfileAndroid,
	"android_native":    ProfileAndroid,
	"android_tv_native": ProfileAndroid,
	"copy":              ProfileCopy,
	"direct":            ProfileCopy,
	"passthrough":       ProfileCopy,
	"repair":            ProfileRepair,
}

type HWAccelMode string

const (
	HWAccelAuto  HWAccelMode = "auto"  // Server decides based on GPU availability
	HWAccelForce HWAccelMode = "force" // Force GPU (fail if unavailable)
	HWAccelOff   HWAccelMode = "off"   // Force CPU
)

type GPUBackend string

const (
	GPUBackendNone  GPUBackend = ""
	GPUBackendVAAPI GPUBackend = "vaapi"
	GPUBackendNVENC GPUBackend = "nvenc"
)

const (
	profileHWAccelVAAPI           = "vaapi"
	profileHWAccelVAAPIEncodeOnly = "vaapi_encode_only"
	profileHWAccelNVENC           = "nvenc"

	safariDirtyHWModeNone       = "none"
	safariDirtyHWModeEncodeOnly = "encode_only"
	safariDirtyHWModeFull       = "full"
)

// shouldUseGPU determines whether to use GPU acceleration based on availability and user override
func shouldUseGPU(backend GPUBackend, mode HWAccelMode) bool {
	switch mode {
	case HWAccelForce:
		return true // Force GPU (will fail later if unavailable)
	case HWAccelOff:
		return false // Force CPU
	case HWAccelAuto:
		return backend != GPUBackendNone // Auto: use GPU if available
	default:
		return backend != GPUBackendNone
	}
}

// IsGPUBackedProfile reports whether the resolved profile uses VAAPI anywhere in the pipeline.
func IsGPUBackedProfile(hwaccel string) bool {
	switch strings.TrimSpace(hwaccel) {
	case profileHWAccelVAAPI, profileHWAccelVAAPIEncodeOnly, profileHWAccelNVENC:
		return true
	default:
		return false
	}
}

// IsFullVAAPIProfile reports whether both decode and encode use the VAAPI path.
func IsFullVAAPIProfile(hwaccel string) bool {
	return strings.TrimSpace(hwaccel) == profileHWAccelVAAPI
}

func resolveSafariDirtyHWMode(backend GPUBackend, hwaccelMode HWAccelMode) string {
	switch hwaccelMode {
	case HWAccelOff:
		return safariDirtyHWModeNone
	case HWAccelForce:
		return safariDirtyHWModeFull
	}

	mode := normalize.Token(config.ParseString("XG2G_SAFARI_DIRTY_HWACCEL_MODE", ""))
	switch mode {
	case safariDirtyHWModeNone, safariDirtyHWModeEncodeOnly, safariDirtyHWModeFull:
		if backend != GPUBackendNone {
			return mode
		}
		return safariDirtyHWModeNone
	case "":
		if backend != GPUBackendNone && envBool("XG2G_SAFARI_DIRTY_USE_GPU", false) {
			return safariDirtyHWModeFull
		}
		return safariDirtyHWModeNone
	default:
		return safariDirtyHWModeNone
	}
}

func hwAccelProfileForBackend(backend GPUBackend) string {
	switch backend {
	case GPUBackendNVENC:
		return profileHWAccelNVENC
	case GPUBackendVAAPI:
		return profileHWAccelVAAPI
	default:
		return ""
	}
}

func requestedHWAccelProfile(backend GPUBackend, hwaccelMode HWAccelMode) string {
	if profile := hwAccelProfileForBackend(backend); profile != "" {
		return profile
	}
	if hwaccelMode == HWAccelForce {
		return profileHWAccelVAAPI
	}
	return ""
}

func requestedEncodeOnlyHWAccelProfile(backend GPUBackend, hwaccelMode HWAccelMode) string {
	switch backend {
	case GPUBackendNVENC:
		return profileHWAccelNVENC
	case GPUBackendVAAPI:
		return profileHWAccelVAAPIEncodeOnly
	case GPUBackendNone:
		if hwaccelMode == HWAccelForce {
			return profileHWAccelVAAPIEncodeOnly
		}
	}
	return ""
}

func newResolvedSpec(name string) model.ProfileSpec {
	return model.ProfileSpec{
		Name:                name,
		PolicyModeHint:      ports.RuntimeModeUnknown,
		EffectiveModeSource: ports.RuntimeModeSourceResolve,
	}
}

func applyDVRWindow(spec *model.ProfileSpec, dvrWindowSec int) {
	if dvrWindowSec > 0 {
		spec.DVRWindowSec = dvrWindowSec
	}
}

func safariFamilyContainer(isSafari bool) string {
	if isSafari {
		return "mpegts"
	}
	return "fmp4"
}

func interlacedOrUnknown(cap *scan.Capability) bool {
	return cap == nil || cap.Interlaced || isLikelyPALSDInterlaced(cap)
}

// isLikelyPALSDInterlaced guards against scanner false-negatives on PAL SD
// MPEG-2 sources: 720x576 @ ~25 fps mpeg2video is interlaced 576i50 in practice
// across all DVB-S/T/C broadcast SD channels. A single-shot ffprobe sample can
// occasionally report `progressive` (e.g. caught between IDR boundaries), which
// would otherwise disable deinterlacing and stall AV1 encode paths that cannot
// consume interlaced frames.
func isLikelyPALSDInterlaced(cap *scan.Capability) bool {
	if cap == nil {
		return false
	}
	codec := cap.VideoCodec
	if codec == "" {
		codec = cap.Codec
	}
	if codec != "mpeg2video" {
		return false
	}
	if cap.Width != 720 || cap.Height != 576 {
		return false
	}
	fps := cap.FPS
	if fps <= 0 {
		fps = cap.SignalFPS
	}
	return fps > 24.5 && fps < 25.5
}

func applyEnvH264GPUSettings(
	spec *model.ProfileSpec,
	hwaccel string,
	qpKey string,
	maxRateKey string,
	bufSizeKey string,
	defaultQP int,
	defaultMaxRateK int,
	defaultBufSizeK int,
) {
	spec.HWAccel = hwaccel
	spec.VideoCodec = "h264"
	spec.VideoQP = envIntBounded(qpKey, defaultQP, 10, 40)
	spec.VideoMaxRateK = envIntBounded(maxRateKey, defaultMaxRateK, 4000, 60000)
	spec.VideoBufSizeK = envIntBounded(bufSizeKey, defaultBufSizeK, 8000, 120000)
}

// NormalizeRequestedProfileID maps known aliases to the stable internal profile IDs
// without collapsing unknown inputs to auto.
func NormalizeRequestedProfileID(requested string) string {
	requested = normalize.Token(requested)
	if canonical, ok := aliasMap[requested]; ok {
		return canonical
	}
	return requested
}

// PrefersNativeFMP4Packaging reports whether the requested internal/public
// profile carries an explicit native HLS fMP4 packaging bias. Client-family
// fallback belongs in higher-level policy layers.
func PrefersNativeFMP4Packaging(profile string) bool {
	switch NormalizeRequestedProfileID(profile) {
	case ProfileSafari,
		ProfileSafariDVR,
		ProfileSafariDirty,
		ProfileSafariHEVC,
		ProfileSafariHEVCHW,
		ProfileSafariHEVCHWLL,
		ProfileAV1HW,
		ProfileH264FMP4,
		ProfileAndroid:
		return true
	default:
		return false
	}
}

// PublicProfileName returns a clearer public-facing label for legacy internal
// profile identifiers while preserving unknown values as-is.
func PublicProfileName(profile string) string {
	switch playbackprofile.NormalizeRequestedIntent(profile) {
	case playbackprofile.IntentDirect:
		return PublicProfileDirect
	case playbackprofile.IntentCompatible:
		return PublicProfileCompatible
	case playbackprofile.IntentQuality:
		return PublicProfileQuality
	case playbackprofile.IntentRepair:
		return PublicProfileRepair
	}

	switch NormalizeRequestedProfileID(profile) {
	case ProfileAuto:
		return PublicProfileCompatible
	case ProfileHigh:
		return PublicProfileCompatible
	case ProfileLow:
		return PublicProfileBandwidth
	case ProfileDVR:
		return PublicProfileCompatible
	case ProfileAndroid:
		return PublicProfileCompatible
	case ProfileSafari:
		return PublicProfileCompatible
	case ProfileSafariRuntimeHQ:
		return PublicProfileCompatible
	case ProfileSafariDVR:
		return PublicProfileCompatible
	case ProfileSafariHEVC:
		return PublicProfileQuality
	case ProfileSafariHEVCHW:
		return PublicProfileQuality
	case ProfileSafariHEVCHWLL:
		return PublicProfileQuality
	case ProfileAV1HW:
		return PublicProfileQuality
	case ProfileH264FMP4:
		return PublicProfileRepair
	case ProfileCopy:
		return PublicProfileDirect
	case ProfileSafariDirty:
		return PublicProfileRepair
	case ProfileRepair:
		return PublicProfileRepair
	default:
		switch normalize.Token(profile) {
		case "generic":
			return PublicProfileCompatible
		case "universal":
			return PublicProfileCompatible
		default:
			return normalize.Token(profile)
		}
	}
}

// Resolve maps a requested profile and user agent to a concrete ProfileSpec.
// dvrWindowSec controls the DVR window for DVR profiles; <=0 disables DVR.
// hwaccelMode allows explicit GPU/CPU override (default: auto).
func Resolve(requested, userAgent string, dvrWindowSec int, cap *scan.Capability, gpuBackend GPUBackend, hwaccelMode HWAccelMode) model.ProfileSpec {
	isSafari := isSafariUA(userAgent)
	canonical := resolveCanonicalProfile(requested, isSafari)

	// REMOVED: Server-side Safari profile override
	// Frontend now controls profile switching explicitly based on fullscreen state
	// This ensures inline playback uses custom controls, fullscreen uses native DVR

	spec := newResolvedSpec(canonical)

	// Carry the verified source height so downstream bitrate budgeting can scale
	// with resolution (SD sources get a lower ceiling than HD).
	if cap != nil && cap.Height > 0 {
		spec.VideoSourceHeight = cap.Height
	}

	switch canonical {
	case ProfileCopy:
		spec.PolicyModeHint = ports.RuntimeModeCopy
		return spec
	case ProfileLow:
		spec.PolicyModeHint = ports.RuntimeModeHQ25
		spec.TranscodeVideo = true
		spec.VideoCRF = 26
		spec.VideoMaxWidth = 1280
		spec.AudioBitrateK = 160
	case ProfileHigh:
		spec.PolicyModeHint = ports.RuntimeModeCopy
		spec.TranscodeVideo = false // Default to copy (passthrough) for original quality
		spec.AudioBitrateK = 192    // FORCE AAC: Browsers cannot decode MP2/AC3 natively
		applyDVRWindow(&spec, dvrWindowSec)
	case ProfileAndroid:
		spec.PolicyModeHint = ports.RuntimeModeCopy
		// Android native player (ExoPlayer): video copy + AAC transcode.
		// Always use mpegts — fMP4 copy fails when SPS/PPS arrive late.
		spec.TranscodeVideo = false
		spec.Container = "mpegts"
		spec.AudioBitrateK = 192
		applyDVRWindow(&spec, dvrWindowSec)
	case ProfileSafari:
		configureSafariSpec(&spec, cap, isSafari, gpuBackend, hwaccelMode)
		applyDVRWindow(&spec, dvrWindowSec)
	case ProfileSafariDirty:
		configureSafariDirtySpec(&spec, isSafari, gpuBackend, hwaccelMode)
		applyDVRWindow(&spec, dvrWindowSec)
	case ProfileSafariDVR:
		spec.PolicyModeHint = ports.RuntimeModeHQ25
		spec.TranscodeVideo = true
		spec.Deinterlace = true
		spec.LLHLS = false // classic HLS is served; the ffmpeg layer does not emit LL-HLS partials
		applyH264VideoLadder(&spec, playbackprofile.VideoRungForIntent(playbackprofile.IntentCompatible))
		spec.AudioBitrateK = 192
		applyDVRWindow(&spec, dvrWindowSec)
	case ProfileH264FMP4:
		spec.PolicyModeHint = ports.RuntimeModeHQ25
		// Always transcode to H.264 with fMP4 segments.
		// Useful for explicit client capability negotiation: "h264" means "make it H.264".
		spec.TranscodeVideo = true
		spec.Container = "fmp4"
		spec.AudioBitrateK = 192

		if interlacedOrUnknown(cap) {
			spec.Deinterlace = true
		}

		// HWAccel Decision (respects override)
		useGPU := shouldUseGPU(gpuBackend, hwaccelMode)
		if useGPU {
			spec.HWAccel = requestedHWAccelProfile(gpuBackend, hwaccelMode)
			spec.VideoCodec = "h264"
			spec.VideoCRF = 16
			spec.VideoMaxRateK = 20000
			spec.VideoBufSizeK = 40000
		} else {
			spec.VideoCodec = "libx264"
			applyH264VideoLadder(&spec, playbackprofile.VideoRungForIntent(playbackprofile.IntentRepair))
			spec.VideoMaxRateK = 8000
			spec.VideoBufSizeK = 16000
		}

		applyDVRWindow(&spec, dvrWindowSec)
	case ProfileDVR:
		spec.PolicyModeHint = ports.RuntimeModeHQ25
		spec.TranscodeVideo = true
		spec.Deinterlace = true
		applyH264VideoLadder(&spec, playbackprofile.VideoRungForIntent(playbackprofile.IntentCompatible))
		applyDVRWindow(&spec, dvrWindowSec)
	case ProfileSafariHEVC:
		spec.PolicyModeHint = ports.RuntimeModeHQ25
		// HEVC live output must use fMP4/CMAF packaging for native WebKit HLS.
		// HEVC in MPEG-TS is a fragile path and can surface as black video while
		// the playlist and segments are still delivered successfully.
		spec.TranscodeVideo = true
		spec.VideoCodec = "hevc"
		spec.Container = "fmp4"
		spec.Deinterlace = interlacedOrUnknown(cap)
		spec.VideoCRF = 22        // Conservative start for x265
		spec.VideoMaxRateK = 5000 // Strict VBV Cap
		spec.VideoBufSizeK = 10000
		spec.AudioBitrateK = 192
	case ProfileSafariHEVCHW:
		spec.PolicyModeHint = ports.RuntimeModeHQ25
		// GPU-Accelerated HEVC (VAAPI) - Recommended for multi-stream
		// 10x faster than CPU, ~10% CPU usage per stream
		// Keep WebKit on fMP4/CMAF; HEVC in MPEG-TS can be served successfully
		// by the backend but still fail to present video in native Safari.
		spec.TranscodeVideo = true
		spec.VideoCodec = "hevc"
		spec.Container = "fmp4"
		// Respect existing scan truth so progressive HEVC candidates do not enter
		// the conservative interlaced path and get downgraded to H.264 before launch.
		spec.Deinterlace = interlacedOrUnknown(cap)
		spec.VideoQP = envIntBounded("XG2G_SAFARI_HEVC_VAAPI_QP", 20, 10, 40)
		spec.VideoMaxRateK = 5000 // VBV Cap
		spec.VideoBufSizeK = 10000
		spec.AudioBitrateK = 192
		// Note: VAAPI doesn't use CRF, uses constant quality mode instead

		// HWAccel Decision (respects override)
		if shouldUseGPU(gpuBackend, hwaccelMode) {
			spec.HWAccel = requestedHWAccelProfile(gpuBackend, hwaccelMode)
		} else {
			// CPU Fallback: x265
			spec.VideoCodec = "hevc"
			spec.VideoCRF = 22
		}

	case ProfileSafariHEVCHWLL:
		spec.PolicyModeHint = ports.RuntimeModeHQ25
		// GPU-Accelerated HEVC with LL-HLS - Ultra-low latency streaming
		// Combines GPU encoding (~10% CPU) with LL-HLS (<3s latency)
		spec.TranscodeVideo = true
		spec.VideoCodec = "hevc"
		spec.Container = "fmp4"
		spec.Deinterlace = interlacedOrUnknown(cap)
		spec.LLHLS = false // LL-HLS part-segments are not emitted yet; do not advertise what we do not serve
		spec.VideoQP = envIntBounded("XG2G_SAFARI_HEVC_VAAPI_QP", 20, 10, 40)
		spec.VideoMaxRateK = 5000
		spec.VideoBufSizeK = 10000
		spec.AudioBitrateK = 192

		// HWAccel Decision (respects override)
		if shouldUseGPU(gpuBackend, hwaccelMode) {
			spec.HWAccel = requestedHWAccelProfile(gpuBackend, hwaccelMode)
		} else {
			// CPU Fallback: x265 with LL-HLS
			spec.VideoCodec = "hevc"
			spec.VideoCRF = 22
		}

		applyDVRWindow(&spec, dvrWindowSec)
	case ProfileAV1HW:
		spec.PolicyModeHint = ports.RuntimeModeHQ25
		// GPU-Accelerated AV1 (VAAPI).
		// Default to fMP4. MPEG-TS is available only as an explicit experimental opt-in.
		spec.TranscodeVideo = true
		spec.VideoCodec = "av1"
		if config.ParseBool("XG2G_EXPERIMENTAL_AV1_MPEGTS_ENABLED", false) {
			spec.Container = "mpegts"
		} else {
			spec.Container = "fmp4"
		}
		// Respect existing scan truth so progressive services do not enter the
		// conservative interlaced startup path and get downgraded before launch.
		spec.Deinterlace = interlacedOrUnknown(cap)
		spec.VideoMaxRateK = 6000
		spec.VideoBufSizeK = 12000
		spec.AudioBitrateK = 192

		if shouldUseGPU(gpuBackend, hwaccelMode) {
			// Keep AV1 on the VAAPI encode-only path for live playback. This leaves
			// frame geometry in system memory long enough to normalize the input
			// before hwupload, which avoids malformed 1080p AV1 output on current
			// AMD VAAPI stacks.
			spec.HWAccel = requestedEncodeOnlyHWAccelProfile(gpuBackend, hwaccelMode)
		}

		applyDVRWindow(&spec, dvrWindowSec)
	case ProfileRepair:
		spec.PolicyModeHint = ports.RuntimeModeSafe
		// RESCUE MODE: Force Transcode to repair timestamps/GOP
		spec.TranscodeVideo = true
		spec.Deinterlace = false // Keep simple unless needed
		applyH264VideoLadder(&spec, playbackprofile.VideoRungForIntent(playbackprofile.IntentRepair))
		spec.VideoMaxWidth = 1280
		spec.AudioBitrateK = 192 // Ensure audio is clean too
	}

	if spec.PolicyModeHint == ports.RuntimeModeUnknown {
		spec.PolicyModeHint = RuntimeModeHintFromProfile(spec)
	}

	return spec
}

// resolveCanonicalProfile maps the requested profile to its canonical internal
// ID, resolving the auto profile to a Safari- or HD-leaning default based on the
// detected client family. Unknown requests collapse to auto.
func resolveCanonicalProfile(requested string, isSafari bool) string {
	requested = normalize.Token(requested)
	canonical, ok := aliasMap[requested]
	if !ok {
		canonical = ProfileAuto
	}

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

	return canonical
}

// configureSafariSpec applies the Safari ("smart") profile settings, choosing
// between a direct progressive remux and an interlaced transcode (with GPU/CPU
// fallback). The DVR window is applied by the caller.
func configureSafariSpec(spec *model.ProfileSpec, cap *scan.Capability, isSafari bool, gpuBackend GPUBackend, hwaccelMode HWAccelMode) {
	// Smart Profile Logic
	if cap != nil && !cap.Interlaced {
		// Progressive -> Direct Remux (Original Quality)
		// Browser Safari stays on classic HLS-TS because copied broadcast H.264
		// inside fMP4 caused black-video regressions there. Native clients that
		// reuse the safari family (for example Android native_hls) prefer fMP4.
		spec.TranscodeVideo = false
		spec.PolicyModeHint = ports.RuntimeModeCopy
		spec.Container = safariFamilyContainer(isSafari)
		spec.AudioBitrateK = 192
		// HWAccel disabled for passthrough
	} else {
		// Interlaced or Unknown -> Transcode + Deinterlace
		spec.PolicyModeHint = ports.RuntimeModeHQ25
		spec.TranscodeVideo = true
		spec.Deinterlace = true
		// Browser Safari has also shown black-video failures on live fMP4
		// transcode output. Keep classic MPEG-TS HLS there as the safer
		// browser-native HLS path; app-native or non-Safari clients that
		// resolve through the safari family can stay on fMP4.
		spec.Container = safariFamilyContainer(isSafari)
		spec.AudioBitrateK = 192

		// HWAccel Decision (respects override)
		useGPU := shouldUseGPU(gpuBackend, hwaccelMode)

		if useGPU {
			// GPU acceleration uses an explicit VAAPI QP target as the primary
			// quality knob. The bitrate fields remain available as optional
			// safety ceilings in the FFmpeg builder.
			applyEnvH264GPUSettings(
				spec,
				requestedHWAccelProfile(gpuBackend, hwaccelMode),
				"XG2G_SAFARI_VAAPI_QP",
				"XG2G_SAFARI_VAAPI_MAXRATE_K",
				"XG2G_SAFARI_VAAPI_BUFSIZE_K",
				20,
				20000,
				40000,
			)
		} else {
			// CPU fallback keeps the Safari-compatible H.264 live path, while
			// retaining the quality rung's CRF and overriding the preset to a
			// live-safe default. The old "slow" preset can stall startup on
			// 1080i relay inputs before HLS emits meaningful progress.
			spec.VideoCodec = "libx264"
			applyH264VideoLadder(spec, playbackprofile.VideoRungForIntent(playbackprofile.IntentQuality))
			spec.Preset = envPreset("XG2G_SAFARI_CPU_PRESET", "veryfast")
			spec.VideoMaxRateK = 8000
			spec.VideoBufSizeK = 16000
		}
	}

	spec.LLHLS = false
}

// configureSafariDirtySpec applies the Safari "dirty" recovery profile for
// degraded DVB inputs, selecting among full-VAAPI, encode-only-VAAPI and CPU
// fallback paths. The DVR window is applied by the caller.
func configureSafariDirtySpec(spec *model.ProfileSpec, isSafari bool, gpuBackend GPUBackend, hwaccelMode HWAccelMode) {
	spec.PolicyModeHint = ports.RuntimeModeSafe
	// Robust recovery profile for dirty DVB inputs.
	spec.TranscodeVideo = true
	spec.Deinterlace = true
	// Browser Safari has shown black-video / freeze regressions on dirty live
	// transcodes packaged as fMP4. Keep TS HLS there; other MSE clients can
	// stay on fMP4.
	spec.Container = safariFamilyContainer(isSafari)
	spec.AudioBitrateK = envIntBounded("XG2G_SAFARI_DIRTY_AUDIO_BITRATE_K", 192, 96, 384)

	// Dirty DVB sources need finer-grained control than a simple CPU/GPU split.
	// none        -> CPU decode + CPU deinterlace + CPU encode
	// encode_only -> CPU decode + CPU deinterlace + VAAPI encode
	// full        -> full VAAPI decode/deinterlace/encode path
	switch resolveSafariDirtyHWMode(gpuBackend, hwaccelMode) {
	case safariDirtyHWModeFull:
		applyEnvH264GPUSettings(
			spec,
			requestedHWAccelProfile(gpuBackend, hwaccelMode),
			"XG2G_SAFARI_DIRTY_VAAPI_QP",
			"XG2G_SAFARI_DIRTY_MAXRATE_K",
			"XG2G_SAFARI_DIRTY_BUFSIZE_K",
			20,
			20000,
			40000,
		)
	case safariDirtyHWModeEncodeOnly:
		applyEnvH264GPUSettings(
			spec,
			requestedEncodeOnlyHWAccelProfile(gpuBackend, hwaccelMode),
			"XG2G_SAFARI_DIRTY_VAAPI_QP",
			"XG2G_SAFARI_DIRTY_MAXRATE_K",
			"XG2G_SAFARI_DIRTY_BUFSIZE_K",
			20,
			20000,
			40000,
		)
	default:
		// CPU fallback prioritizes startup stability and sustained playback over
		// "best quality" defaults. Dirty 1080i feeds can stall badly on hosts
		// without GPU acceleration when we keep the older CRF16/fast/14M ladder.
		spec.VideoCodec = "libx264"
		spec.VideoCRF = envIntBounded("XG2G_SAFARI_DIRTY_CRF", 18, 12, 30)
		spec.VideoMaxRateK = envIntBounded("XG2G_SAFARI_DIRTY_MAXRATE_K", 8000, 4000, 60000)
		spec.VideoBufSizeK = envIntBounded("XG2G_SAFARI_DIRTY_BUFSIZE_K", 16000, 8000, 120000)
		spec.Preset = envPreset("XG2G_SAFARI_DIRTY_PRESET", "veryfast")
	}

	spec.LLHLS = false
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

func envBool(key string, defaultValue bool) bool {
	raw := strings.TrimSpace(strings.ToLower(config.ParseString(key, "")))
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return defaultValue
	}
}

func envIntBounded(key string, defaultValue, minValue, maxValue int) int {
	raw := strings.TrimSpace(config.ParseString(key, ""))
	if raw == "" {
		return defaultValue
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return defaultValue
	}
	if n < minValue {
		return minValue
	}
	if n > maxValue {
		return maxValue
	}
	return n
}

func envPreset(key, defaultValue string) string {
	raw := strings.ToLower(strings.TrimSpace(config.ParseString(key, "")))
	if raw == "" {
		return defaultValue
	}
	switch raw {
	case "slow", "medium", "fast", "veryfast", "faster", "superfast", "ultrafast":
		return raw
	default:
		return defaultValue
	}
}

func applyH264VideoLadder(spec *model.ProfileSpec, rung playbackprofile.QualityRung) {
	spec.VideoCRF = playbackprofile.VideoCRFForRung(rung)
	spec.Preset = playbackprofile.VideoPresetForRung(rung)
}
