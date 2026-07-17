// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package profiles

import (
	"strings"

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

func resolveSafariDirtyHWMode(backend GPUBackend, hwaccelMode HWAccelMode, cfg ConfigSnapshot) string {
	switch hwaccelMode {
	case HWAccelOff:
		return safariDirtyHWModeNone
	case HWAccelForce:
		return safariDirtyHWModeFull
	}

	mode := cfg.SafariDirtyHWAccelMode
	switch mode {
	case safariDirtyHWModeNone, safariDirtyHWModeEncodeOnly, safariDirtyHWModeFull:
		if backend != GPUBackendNone {
			return mode
		}
		return safariDirtyHWModeNone
	case "":
		if backend != GPUBackendNone && cfg.SafariDirtyUseGPU {
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

func applyH264GPUSettings(
	spec *model.ProfileSpec,
	hwaccel string,
	qp int,
	maxRateK int,
	bufSizeK int,
) {
	spec.HWAccel = hwaccel
	spec.VideoCodec = "h264"
	spec.VideoQP = qp
	spec.VideoMaxRateK = maxRateK
	spec.VideoBufSizeK = bufSizeK
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

// Resolver binds all profile decisions to one immutable configuration snapshot.
// Production code should construct it at a composition root and pass it down.
type Resolver struct {
	config      ConfigSnapshot
	initialized bool
}

func NewResolver(config ConfigSnapshot) Resolver {
	return Resolver{config: config.clone(), initialized: true}
}

func LoadResolver() Resolver {
	return NewResolver(LoadConfigSnapshot())
}

// IsInitialized reports whether the resolver was bound to an explicit
// immutable snapshot. Composition roots use this to reject accidental
// zero-value injection instead of silently discarding operator policy.
func (r Resolver) IsInitialized() bool {
	return r.initialized
}

func (r Resolver) Resolve(requested, userAgent string, dvrWindowSec int, cap *scan.Capability, gpuBackend GPUBackend, hwaccelMode HWAccelMode) model.ProfileSpec {
	return ResolveWithConfig(requested, userAgent, dvrWindowSec, cap, gpuBackend, hwaccelMode, r.ConfigSnapshot())
}

// ConfigSnapshot returns the immutable profile-policy snapshot bound to the
// resolver. The zero value deliberately exposes environment-independent
// defaults, matching Resolve's compatibility behavior.
func (r Resolver) ConfigSnapshot() ConfigSnapshot {
	if !r.initialized {
		return DefaultConfigSnapshot()
	}
	return r.config.clone()
}

// Resolve is the compatibility facade for tests and out-of-tree consumers.
// Production call sites use an injected Resolver so planning never depends on
// package-init environment state.
func Resolve(requested, userAgent string, dvrWindowSec int, cap *scan.Capability, gpuBackend GPUBackend, hwaccelMode HWAccelMode) model.ProfileSpec {
	return Resolver{}.Resolve(requested, userAgent, dvrWindowSec, cap, gpuBackend, hwaccelMode)
}

// ResolveWithConfig maps a requested profile to a concrete ProfileSpec using
// only the supplied immutable configuration snapshot.
func ResolveWithConfig(requested, userAgent string, dvrWindowSec int, cap *scan.Capability, gpuBackend GPUBackend, hwaccelMode HWAccelMode, cfg ConfigSnapshot) model.ProfileSpec {
	isSafari := isSafariUA(userAgent)
	canonical := resolveCanonicalProfile(requested, isSafari)

	spec := newResolvedSpec(canonical)

	// Carry the verified source height so downstream bitrate budgeting can scale
	// with resolution (SD sources get a lower ceiling than HD).
	if cap != nil && cap.Height > 0 {
		spec.VideoSourceHeight = cap.Height
	}

	// 1. Resolve base orthogonal axes (Video copy/transcode, Audio copy/transcode, Container ts/fmp4, Policy mode)
	axes := resolveProfileAxes(canonical, isSafari, cap, cfg)
	spec.PolicyModeHint = axes.PolicyModeHint
	spec.Container = axes.Container
	spec.AudioBitrateK = axes.AudioBitrateK
	spec.TranscodeVideo = (axes.Video != VideoActionCopy)

	// 2. Apply video codec & quality/hardware overlays
	applyVideoQualityOverlay(&spec, axes, canonical, cap, gpuBackend, hwaccelMode, cfg)

	// 3. Apply global post-resolution constraints
	spec.LLHLS = false
	if spec.PolicyModeHint == ports.RuntimeModeUnknown {
		spec.PolicyModeHint = RuntimeModeHintFromProfile(spec)
	}

	// 4. Apply DVR window semantics
	applyDVROverlay(&spec, canonical, dvrWindowSec)

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

func applyH264VideoLadder(spec *model.ProfileSpec, rung playbackprofile.QualityRung) {
	spec.VideoCRF = playbackprofile.VideoCRFForRung(rung)
	spec.Preset = playbackprofile.VideoPresetForRung(rung)
}
