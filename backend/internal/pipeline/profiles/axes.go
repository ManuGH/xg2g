// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package profiles

import (
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
)

// VideoCodecAction defines the target video encoding strategy for the profile.
type VideoCodecAction string

const (
	VideoActionCopy VideoCodecAction = "copy"
	VideoActionH264 VideoCodecAction = "h264"
	VideoActionHEVC VideoCodecAction = "hevc"
	VideoActionAV1  VideoCodecAction = "av1"
)

// ProfileAxes represents the four orthogonal decision dimensions of any stream profile:
// 1. Video Action (Copy vs Transcode Codec)
// 2. Audio Action (Copy vs Transcode Bitrate)
// 3. Container Format (mpegts vs fmp4 vs default)
// 4. Runtime Mode Policy Hint (Copy vs HQ25 vs Safe)
type ProfileAxes struct {
	Video          VideoCodecAction
	AudioBitrateK  int
	Container      string
	PolicyModeHint ports.RuntimeMode
}

// resolveProfileAxes maps a canonical profile to its base orthogonal axes.
func resolveProfileAxes(canonical string, isSafari bool, cap *scan.Capability, cfg ConfigSnapshot) ProfileAxes {
	switch canonical {
	case ProfileCopy:
		return ProfileAxes{
			Video:          VideoActionCopy,
			AudioBitrateK:  0,
			Container:      "mpegts",
			PolicyModeHint: ports.RuntimeModeCopy,
		}
	case ProfileLow:
		return ProfileAxes{
			Video:          VideoActionH264,
			AudioBitrateK:  160,
			Container:      "",
			PolicyModeHint: ports.RuntimeModeHQ25,
		}
	case ProfileHigh:
		return ProfileAxes{
			Video:          VideoActionCopy,
			AudioBitrateK:  192,
			Container:      "",
			PolicyModeHint: ports.RuntimeModeCopy,
		}
	case ProfileAndroid:
		return ProfileAxes{
			Video:          VideoActionCopy,
			AudioBitrateK:  192,
			Container:      "mpegts",
			PolicyModeHint: ports.RuntimeModeCopy,
		}
	case ProfileSafari:
		if cap != nil && !cap.Interlaced {
			return ProfileAxes{
				Video:          VideoActionCopy,
				AudioBitrateK:  192,
				Container:      safariFamilyContainer(isSafari),
				PolicyModeHint: ports.RuntimeModeCopy,
			}
		}
		return ProfileAxes{
			Video:          VideoActionH264,
			AudioBitrateK:  192,
			Container:      safariFamilyContainer(isSafari),
			PolicyModeHint: ports.RuntimeModeHQ25,
		}
	case ProfileSafariDirty:
		return ProfileAxes{
			Video:          VideoActionH264,
			AudioBitrateK:  cfg.SafariDirtyAudioBitrateK,
			Container:      safariFamilyContainer(isSafari),
			PolicyModeHint: ports.RuntimeModeSafe,
		}
	case ProfileDVR:
		return ProfileAxes{
			Video:          VideoActionH264,
			AudioBitrateK:  0,
			Container:      "",
			PolicyModeHint: ports.RuntimeModeHQ25,
		}
	case ProfileSafariDVR:
		return ProfileAxes{
			Video:          VideoActionH264,
			AudioBitrateK:  192,
			Container:      "",
			PolicyModeHint: ports.RuntimeModeHQ25,
		}
	case ProfileH264FMP4:
		return ProfileAxes{
			Video:          VideoActionH264,
			AudioBitrateK:  192,
			Container:      "fmp4",
			PolicyModeHint: ports.RuntimeModeHQ25,
		}
	case ProfileSafariHEVC, ProfileSafariHEVCHW, ProfileSafariHEVCHWLL:
		return ProfileAxes{
			Video:          VideoActionHEVC,
			AudioBitrateK:  192,
			Container:      "fmp4",
			PolicyModeHint: ports.RuntimeModeHQ25,
		}
	case ProfileAV1HW:
		container := "fmp4"
		if cfg.ExperimentalAV1MPEGTS {
			container = "mpegts"
		}
		return ProfileAxes{
			Video:          VideoActionAV1,
			AudioBitrateK:  192,
			Container:      container,
			PolicyModeHint: ports.RuntimeModeHQ25,
		}
	case ProfileRepair:
		return ProfileAxes{
			Video:          VideoActionH264,
			AudioBitrateK:  192,
			Container:      "",
			PolicyModeHint: ports.RuntimeModeSafe,
		}
	default:
		return ProfileAxes{
			Video:          VideoActionCopy,
			AudioBitrateK:  0,
			Container:      "",
			PolicyModeHint: ports.RuntimeModeUnknown,
		}
	}
}

// applyVideoQualityOverlay applies codec ladder targets, hardware acceleration, and scan-rule overlays.
func applyVideoQualityOverlay(spec *model.ProfileSpec, axes ProfileAxes, canonical string, cap *scan.Capability, gpuBackend GPUBackend, hwaccelMode HWAccelMode, cfg ConfigSnapshot) {
	if axes.Video == VideoActionCopy {
		return
	}

	useGPU := shouldUseGPU(gpuBackend, hwaccelMode)

	switch axes.Video {
	case VideoActionH264:
		switch canonical {
		case ProfileLow:
			spec.VideoCRF = 26
			spec.VideoMaxWidth = 1280
			spec.VideoMaxRateK = 3000
			spec.VideoBufSizeK = 6000
		case ProfileSafari:
			spec.Deinterlace = true
			if useGPU {
				applyH264GPUSettings(
					spec,
					requestedHWAccelProfile(gpuBackend, hwaccelMode),
					cfg.SafariVAAPIQP,
					cfg.SafariVAAPIMaxRateK,
					cfg.SafariVAAPIBufSizeK,
				)
			} else {
				spec.VideoCodec = "libx264"
				applyH264VideoLadder(spec, playbackprofile.VideoRungForIntent(playbackprofile.IntentQuality))
				spec.Preset = cfg.SafariCPUPreset
				spec.VideoMaxRateK = 8000
				spec.VideoBufSizeK = 16000
			}
		case ProfileSafariDirty:
			spec.Deinterlace = true
			switch resolveSafariDirtyHWMode(gpuBackend, hwaccelMode, cfg) {
			case safariDirtyHWModeFull:
				applyH264GPUSettings(
					spec,
					requestedHWAccelProfile(gpuBackend, hwaccelMode),
					cfg.SafariDirtyGPUQP,
					cfg.SafariDirtyGPUMaxRateK,
					cfg.SafariDirtyGPUBufSizeK,
				)
			case safariDirtyHWModeEncodeOnly:
				applyH264GPUSettings(
					spec,
					requestedEncodeOnlyHWAccelProfile(gpuBackend, hwaccelMode),
					cfg.SafariDirtyGPUQP,
					cfg.SafariDirtyGPUMaxRateK,
					cfg.SafariDirtyGPUBufSizeK,
				)
			default:
				spec.VideoCodec = "libx264"
				spec.VideoCRF = cfg.SafariDirtyCPUCRF
				spec.VideoMaxRateK = cfg.SafariDirtyCPUMaxRateK
				spec.VideoBufSizeK = cfg.SafariDirtyCPUBufSizeK
				spec.Preset = cfg.SafariDirtyCPUPreset
			}
		case ProfileSafariDVR, ProfileDVR:
			spec.Deinterlace = true
			applyH264VideoLadder(spec, playbackprofile.VideoRungForIntent(playbackprofile.IntentCompatible))
		case ProfileH264FMP4:
			if interlacedOrUnknown(cap) {
				spec.Deinterlace = true
			}
			if useGPU {
				spec.HWAccel = requestedHWAccelProfile(gpuBackend, hwaccelMode)
				spec.VideoCodec = "h264"
				spec.VideoCRF = 16
				spec.VideoMaxRateK = 20000
				spec.VideoBufSizeK = 40000
			} else {
				spec.VideoCodec = "libx264"
				applyH264VideoLadder(spec, playbackprofile.VideoRungForIntent(playbackprofile.IntentRepair))
				spec.VideoMaxRateK = 8000
				spec.VideoBufSizeK = 16000
			}
		case ProfileRepair:
			spec.Deinterlace = false
			applyH264VideoLadder(spec, playbackprofile.VideoRungForIntent(playbackprofile.IntentRepair))
			spec.VideoMaxWidth = 1280
		}

	case VideoActionHEVC:
		spec.VideoCodec = "hevc"
		spec.Deinterlace = interlacedOrUnknown(cap)
		spec.VideoMaxRateK = 5000
		spec.VideoBufSizeK = 10000
		switch canonical {
		case ProfileSafariHEVC:
			spec.VideoCRF = 22
		case ProfileSafariHEVCHW, ProfileSafariHEVCHWLL:
			spec.VideoQP = cfg.SafariHEVCVAAPIQP
			if useGPU {
				spec.HWAccel = requestedHWAccelProfile(gpuBackend, hwaccelMode)
			} else {
				spec.VideoCRF = 22
			}
		}

	case VideoActionAV1:
		spec.VideoCodec = "av1"
		spec.Deinterlace = interlacedOrUnknown(cap)
		spec.VideoMaxRateK = 6000
		spec.VideoBufSizeK = 12000
		if useGPU {
			spec.HWAccel = requestedEncodeOnlyHWAccelProfile(gpuBackend, hwaccelMode)
		}
	}
}

// applyDVROverlay applies DVR window semantics where appropriate for the profile.
func applyDVROverlay(spec *model.ProfileSpec, canonical string, dvrWindowSec int) {
	switch canonical {
	case ProfileHigh, ProfileAndroid, ProfileSafari, ProfileSafariDirty, ProfileSafariDVR, ProfileH264FMP4, ProfileDVR, ProfileSafariHEVCHWLL, ProfileAV1HW:
		applyDVRWindow(spec, dvrWindowSec)
	}
}
