// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package playbackprofile

import "testing"

func TestNormalizeRequestedIntent(t *testing.T) {
	t.Run("maps aliases", func(t *testing.T) {
		cases := map[string]PlaybackIntent{
			"direct":      IntentDirect,
			"copy":        IntentDirect,
			"passthrough": IntentDirect,
			"compatible":  IntentCompatible,
			"high":        IntentCompatible,
			"quality":     IntentQuality,
			"repair":      IntentRepair,
			"unknown":     IntentUnknown,
			"":            IntentUnknown,
		}

		for raw, want := range cases {
			if got := NormalizeRequestedIntent(raw); got != want {
				t.Fatalf("NormalizeRequestedIntent(%q) = %q, want %q", raw, got, want)
			}
		}
	})
}

func TestNormalizeClientRequestedIntent(t *testing.T) {
	for _, raw := range []string{"repair", " REPAIR "} {
		if got := NormalizeClientRequestedIntent(raw); got != "repair" {
			t.Fatalf("NormalizeClientRequestedIntent(%q) = %q, want repair", raw, got)
		}
	}
	for _, raw := range []string{"", "auto", "quality", "compatible", "direct", "bandwidth", "av1_hw", "safari_hevc_hw"} {
		if got := NormalizeClientRequestedIntent(raw); got != "" {
			t.Fatalf("NormalizeClientRequestedIntent(%q) = %q, want auto/empty", raw, got)
		}
	}
}

func TestUsesAutoCodecSelection(t *testing.T) {
	if !UsesAutoCodecSelection("") || !UsesAutoCodecSelection("auto") || !UsesAutoCodecSelection("quality") {
		t.Fatal("automatic and quality planning must use codec selection")
	}
	if UsesAutoCodecSelection("repair") || UsesAutoCodecSelection("bandwidth") {
		t.Fatal("repair and rolling-upgrade bandwidth callers must preserve their fixed codec policy")
	}
}

func TestPublicIntentName(t *testing.T) {
	cases := map[PlaybackIntent]string{
		IntentDirect:     "direct",
		IntentCompatible: "compatible",
		IntentQuality:    "quality",
		IntentRepair:     "repair",
		IntentUnknown:    "",
	}

	for intent, want := range cases {
		if got := PublicIntentName(intent); got != want {
			t.Fatalf("PublicIntentName(%q) = %q, want %q", intent, got, want)
		}
	}
}

func TestIsKnownIntent(t *testing.T) {
	if !IsKnownIntent("quality") {
		t.Fatal("expected quality to be known")
	}
	if IsKnownIntent("mystery") {
		t.Fatal("expected mystery to be unknown")
	}
}

func TestNormalizeQualityRung(t *testing.T) {
	cases := map[string]QualityRung{
		"quality_audio_aac_320_stereo":    RungQualityAudioAAC320Stereo,
		"compatible_audio_aac_256_stereo": RungCompatibleAudioAAC256Stereo,
		"repair_audio_aac_192_stereo":     RungRepairAudioAAC192Stereo,
		"compatible_hls_ts":               RungCompatibleHLSTS,
		"compatible_hls_fmp4":             RungCompatibleHLSFMP4,
		"repair_h264_aac":                 RungRepairH264AAC,
		"unknown":                         RungUnknown,
	}

	for raw, want := range cases {
		if got := NormalizeQualityRung(raw); got != want {
			t.Fatalf("NormalizeQualityRung(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestClampIntentToMaxQualityRung(t *testing.T) {
	if got := ClampIntentToMaxQualityRung(IntentQuality, RungCompatibleAudioAAC256Stereo); got != IntentCompatible {
		t.Fatalf("expected quality to clamp to compatible, got %q", got)
	}
	if got := ClampIntentToMaxQualityRung(IntentCompatible, RungRepairAudioAAC192Stereo); got != IntentRepair {
		t.Fatalf("expected compatible to clamp to repair, got %q", got)
	}
	if got := ClampIntentToMaxQualityRung(IntentDirect, RungRepairAudioAAC192Stereo); got != IntentDirect {
		t.Fatalf("expected direct to remain direct, got %q", got)
	}
}
