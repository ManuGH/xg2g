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
