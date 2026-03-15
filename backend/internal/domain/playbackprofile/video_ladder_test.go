package playbackprofile

import "testing"

func TestVideoLadder_NormalizeQualityRung(t *testing.T) {
	cases := map[string]QualityRung{
		"compatible_video_h264_crf23_fast": RungCompatibleVideoH264CRF23,
		"quality_video_h264_crf20_slow":    RungQualityVideoH264CRF20,
		"repair_video_h264_crf28_veryfast": RungRepairVideoH264CRF28,
		"compatible_video_h264_crf23_FAST": RungCompatibleVideoH264CRF23,
		"quality_video_h264_crf20_SLOW":    RungQualityVideoH264CRF20,
		"repair_video_h264_crf28_VERYFAST": RungRepairVideoH264CRF28,
		"mystery_video_h264_crf21_medium":  RungUnknown,
	}

	for raw, want := range cases {
		if got := NormalizeQualityRung(raw); got != want {
			t.Fatalf("NormalizeQualityRung(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestVideoLadder_ClampIntentToMaxQualityRung(t *testing.T) {
	if got := ClampIntentToMaxQualityRung(IntentQuality, RungCompatibleVideoH264CRF23); got != IntentCompatible {
		t.Fatalf("expected quality to clamp to compatible for compatible video rung, got %q", got)
	}
	if got := ClampIntentToMaxQualityRung(IntentCompatible, RungRepairVideoH264CRF28); got != IntentRepair {
		t.Fatalf("expected compatible to clamp to repair for repair video rung, got %q", got)
	}
	if got := ClampIntentToMaxQualityRung(IntentDirect, RungRepairVideoH264CRF28); got != IntentDirect {
		t.Fatalf("expected direct to remain direct, got %q", got)
	}
}

func TestVideoLadder_Defaults(t *testing.T) {
	cases := []struct {
		rung       QualityRung
		wantCRF    int
		wantPreset string
	}{
		{rung: RungCompatibleVideoH264CRF23, wantCRF: 23, wantPreset: "fast"},
		{rung: RungQualityVideoH264CRF20, wantCRF: 20, wantPreset: "slow"},
		{rung: RungRepairVideoH264CRF28, wantCRF: 28, wantPreset: "veryfast"},
		{rung: RungUnknown, wantCRF: 0, wantPreset: ""},
	}

	for _, tc := range cases {
		if got := VideoCRFForRung(tc.rung); got != tc.wantCRF {
			t.Fatalf("VideoCRFForRung(%q) = %d, want %d", tc.rung, got, tc.wantCRF)
		}
		if got := VideoPresetForRung(tc.rung); got != tc.wantPreset {
			t.Fatalf("VideoPresetForRung(%q) = %q, want %q", tc.rung, got, tc.wantPreset)
		}
	}
}

func TestVideoLadder_VideoRungForIntent(t *testing.T) {
	cases := map[PlaybackIntent]QualityRung{
		IntentQuality:    RungQualityVideoH264CRF20,
		IntentRepair:     RungRepairVideoH264CRF28,
		IntentCompatible: RungCompatibleVideoH264CRF23,
		IntentDirect:     RungCompatibleVideoH264CRF23,
		IntentUnknown:    RungCompatibleVideoH264CRF23,
	}

	for intent, want := range cases {
		if got := VideoRungForIntent(intent); got != want {
			t.Fatalf("VideoRungForIntent(%q) = %q, want %q", intent, got, want)
		}
	}
}

func TestVideoLadder_CanonicalizeTargetNormalizesVideoFields(t *testing.T) {
	got := CanonicalizeTarget(TargetPlaybackProfile{
		Video: VideoTarget{
			Mode:   MediaMode(" TRANSCODE "),
			Codec:  " H264 ",
			CRF:    -5,
			Preset: " FAST ",
		},
	})

	if got.Video.Mode != MediaModeTranscode {
		t.Fatalf("expected transcode mode, got %q", got.Video.Mode)
	}
	if got.Video.Codec != "h264" {
		t.Fatalf("expected h264 codec, got %q", got.Video.Codec)
	}
	if got.Video.CRF != 0 {
		t.Fatalf("expected negative crf to clamp to 0, got %d", got.Video.CRF)
	}
	if got.Video.Preset != "fast" {
		t.Fatalf("expected preset to normalize to fast, got %q", got.Video.Preset)
	}
}
