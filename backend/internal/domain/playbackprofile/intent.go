// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package playbackprofile

import "github.com/ManuGH/xg2g/internal/normalize"

type PlaybackIntent string

const (
	IntentUnknown    PlaybackIntent = ""
	IntentDirect     PlaybackIntent = "direct"
	IntentCompatible PlaybackIntent = "compatible"
	IntentQuality    PlaybackIntent = "quality"
	IntentRepair     PlaybackIntent = "repair"
)

type QualityRung string

const (
	RungUnknown                     QualityRung = ""
	RungDirectCopy                  QualityRung = "direct_copy"
	RungCompatibleAudioAAC256Stereo QualityRung = "compatible_audio_aac_256_stereo"
	RungQualityAudioAAC320Stereo    QualityRung = "quality_audio_aac_320_stereo"
	RungRepairAudioAAC192Stereo     QualityRung = "repair_audio_aac_192_stereo"
	RungCompatibleVideoH264CRF23    QualityRung = "compatible_video_h264_crf23_fast"
	RungQualityVideoH264CRF20       QualityRung = "quality_video_h264_crf20_slow"
	RungRepairVideoH264CRF28        QualityRung = "repair_video_h264_crf28_veryfast"
	RungCompatibleHLSTS             QualityRung = "compatible_hls_ts"
	RungCompatibleHLSFMP4           QualityRung = "compatible_hls_fmp4"
	RungRepairH264AAC               QualityRung = "repair_h264_aac"
)

func NormalizeRequestedIntent(raw string) PlaybackIntent {
	switch normalize.Token(raw) {
	case "direct", "copy", "passthrough":
		return IntentDirect
	case "compatible", "high":
		return IntentCompatible
	case "quality":
		return IntentQuality
	case "repair":
		return IntentRepair
	default:
		return IntentUnknown
	}
}

func PublicIntentName(intent PlaybackIntent) string {
	switch intent {
	case IntentDirect:
		return string(IntentDirect)
	case IntentCompatible:
		return string(IntentCompatible)
	case IntentQuality:
		return string(IntentQuality)
	case IntentRepair:
		return string(IntentRepair)
	default:
		return ""
	}
}

func IsKnownIntent(raw string) bool {
	return NormalizeRequestedIntent(raw) != IntentUnknown
}

func NormalizeQualityRung(raw string) QualityRung {
	switch normalize.Token(raw) {
	case string(RungDirectCopy):
		return RungDirectCopy
	case string(RungCompatibleAudioAAC256Stereo):
		return RungCompatibleAudioAAC256Stereo
	case string(RungQualityAudioAAC320Stereo):
		return RungQualityAudioAAC320Stereo
	case string(RungRepairAudioAAC192Stereo):
		return RungRepairAudioAAC192Stereo
	case string(RungCompatibleVideoH264CRF23):
		return RungCompatibleVideoH264CRF23
	case string(RungQualityVideoH264CRF20):
		return RungQualityVideoH264CRF20
	case string(RungRepairVideoH264CRF28):
		return RungRepairVideoH264CRF28
	case string(RungCompatibleHLSTS):
		return RungCompatibleHLSTS
	case string(RungCompatibleHLSFMP4):
		return RungCompatibleHLSFMP4
	case string(RungRepairH264AAC):
		return RungRepairH264AAC
	default:
		return RungUnknown
	}
}

func MaxAudioBitrateForRung(rung QualityRung) int {
	switch NormalizeQualityRung(string(rung)) {
	case RungQualityAudioAAC320Stereo:
		return 320
	case RungCompatibleAudioAAC256Stereo, RungCompatibleHLSTS, RungCompatibleHLSFMP4:
		return 256
	case RungRepairAudioAAC192Stereo, RungRepairH264AAC:
		return 192
	default:
		return 0
	}
}

func ClampIntentToMaxQualityRung(intent PlaybackIntent, maxRung QualityRung) PlaybackIntent {
	switch NormalizeQualityRung(string(maxRung)) {
	case RungRepairAudioAAC192Stereo, RungRepairH264AAC:
		switch intent {
		case IntentQuality, IntentCompatible:
			return IntentRepair
		default:
			return intent
		}
	case RungCompatibleAudioAAC256Stereo, RungCompatibleVideoH264CRF23, RungCompatibleHLSTS, RungCompatibleHLSFMP4:
		switch intent {
		case IntentQuality:
			return IntentCompatible
		default:
			return intent
		}
	case RungRepairVideoH264CRF28:
		switch intent {
		case IntentQuality, IntentCompatible:
			return IntentRepair
		default:
			return intent
		}
	default:
		return intent
	}
}

func VideoCRFForRung(rung QualityRung) int {
	switch NormalizeQualityRung(string(rung)) {
	case RungQualityVideoH264CRF20:
		return 20
	case RungRepairVideoH264CRF28:
		return 28
	case RungCompatibleVideoH264CRF23:
		return 23
	default:
		return 0
	}
}

func VideoPresetForRung(rung QualityRung) string {
	switch NormalizeQualityRung(string(rung)) {
	case RungQualityVideoH264CRF20:
		return "slow"
	case RungRepairVideoH264CRF28:
		return "veryfast"
	case RungCompatibleVideoH264CRF23:
		return "fast"
	default:
		return ""
	}
}

func VideoRungForIntent(intent PlaybackIntent) QualityRung {
	switch intent {
	case IntentQuality:
		return RungQualityVideoH264CRF20
	case IntentRepair:
		return RungRepairVideoH264CRF28
	default:
		return RungCompatibleVideoH264CRF23
	}
}
