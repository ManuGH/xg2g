package decision

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

func TestDecide_DirectPlayEmitsCopyTargetProfile(t *testing.T) {
	trueVal := true
	input := DecisionInput{
		Source: Source{
			Container:  "mp4",
			VideoCodec: "h264",
			AudioCodec: "aac",
		},
		Capabilities: Capabilities{
			Version:       1,
			Containers:    []string{"mp4"},
			VideoCodecs:   []string{"h264"},
			AudioCodecs:   []string{"aac"},
			SupportsHLS:   true,
			SupportsRange: &trueVal,
		},
		Policy:     Policy{AllowTranscode: true},
		APIVersion: "v3",
	}

	_, dec, prob := Decide(t.Context(), input, "test")
	if prob != nil || dec == nil {
		t.Fatalf("expected decision, got problem=%v", prob)
	}
	if dec.Mode != ModeDirectPlay {
		t.Fatalf("expected direct play, got %s", dec.Mode)
	}
	if dec.TargetProfile == nil {
		t.Fatal("expected target profile")
	}
	if dec.TargetProfile.Video.Mode != "copy" || dec.TargetProfile.Audio.Mode != "copy" {
		t.Fatalf("expected copy/copy target profile, got %#v", dec.TargetProfile)
	}
	if dec.TargetProfile.HLS.Enabled {
		t.Fatalf("direct play target profile must not enable hls: %#v", dec.TargetProfile)
	}
}

func TestDecide_DirectStreamEmitsHLSTargetProfile(t *testing.T) {
	input := DecisionInput{
		Source: Source{
			Container:  "mkv",
			VideoCodec: "h264",
			AudioCodec: "aac",
		},
		Capabilities: Capabilities{
			Version:       1,
			Containers:    []string{"mp4"},
			VideoCodecs:   []string{"h264"},
			AudioCodecs:   []string{"aac"},
			SupportsHLS:   true,
			SupportsRange: nil,
		},
		Policy:     Policy{AllowTranscode: true},
		APIVersion: "v3",
	}

	_, dec, prob := Decide(t.Context(), input, "test")
	if prob != nil || dec == nil {
		t.Fatalf("expected decision, got problem=%v", prob)
	}
	if dec.Mode != ModeDirectStream {
		t.Fatalf("expected direct stream, got %s", dec.Mode)
	}
	if dec.TargetProfile == nil {
		t.Fatal("expected target profile")
	}
	if !dec.TargetProfile.HLS.Enabled || dec.TargetProfile.HLS.SegmentContainer != "mpegts" {
		t.Fatalf("expected hls ts target profile, got %#v", dec.TargetProfile)
	}
	if dec.TargetProfile.Video.Mode != "copy" || dec.TargetProfile.Audio.Mode != "copy" {
		t.Fatalf("expected direct stream to copy audio and video, got %#v", dec.TargetProfile)
	}
}

func TestDecide_TranscodeAudioEmitsConcreteTargetProfile(t *testing.T) {
	input := DecisionInput{
		Source: Source{
			Container:  "mp4",
			VideoCodec: "h264",
			AudioCodec: "ac3",
		},
		Capabilities: Capabilities{
			Version:       1,
			Containers:    []string{"mp4"},
			VideoCodecs:   []string{"h264"},
			AudioCodecs:   []string{"aac"},
			SupportsHLS:   true,
			SupportsRange: nil,
		},
		Policy:     Policy{AllowTranscode: true},
		APIVersion: "v3",
	}

	_, dec, prob := Decide(t.Context(), input, "test")
	if prob != nil || dec == nil {
		t.Fatalf("expected decision, got problem=%v", prob)
	}
	if dec.Mode != ModeTranscode {
		t.Fatalf("expected transcode, got %s", dec.Mode)
	}
	if dec.TargetProfile == nil {
		t.Fatal("expected target profile")
	}
	if dec.TargetProfile.Video.Mode != "copy" || dec.TargetProfile.Video.Codec != "h264" {
		t.Fatalf("expected copy-video target, got %#v", dec.TargetProfile)
	}
	if dec.TargetProfile.Audio.Mode != "transcode" || dec.TargetProfile.Audio.Codec != "aac" {
		t.Fatalf("expected aac audio transcode target, got %#v", dec.TargetProfile)
	}
	if dec.TargetProfile.Audio.BitrateKbps != 256 || dec.TargetProfile.Audio.Channels != 2 || dec.TargetProfile.Audio.SampleRate != 48000 {
		t.Fatalf("expected concrete AAC stereo target, got %#v", dec.TargetProfile)
	}
	if dec.Trace.ResolvedIntent != string(playbackprofile.IntentCompatible) {
		t.Fatalf("expected compatible resolved intent, got %#v", dec.Trace)
	}
	if dec.Trace.QualityRung != string(playbackprofile.RungCompatibleAudioAAC256Stereo) {
		t.Fatalf("expected compatible ladder rung, got %#v", dec.Trace)
	}
	if dec.Trace.AudioQualityRung != string(playbackprofile.RungCompatibleAudioAAC256Stereo) || dec.Trace.VideoQualityRung != "" {
		t.Fatalf("expected audio-only ladder trace, got %#v", dec.Trace)
	}
}

func TestDecide_TranscodeAudioQualityIntentUsesHigherAACBitrate(t *testing.T) {
	input := DecisionInput{
		RequestedIntent: playbackprofile.IntentQuality,
		Source: Source{
			Container:  "mp4",
			VideoCodec: "h264",
			AudioCodec: "ac3",
		},
		Capabilities: Capabilities{
			Version:       1,
			Containers:    []string{"mp4"},
			VideoCodecs:   []string{"h264"},
			AudioCodecs:   []string{"aac"},
			SupportsHLS:   true,
			SupportsRange: nil,
		},
		Policy:     Policy{AllowTranscode: true},
		APIVersion: "v3",
	}

	_, dec, prob := Decide(t.Context(), input, "test")
	if prob != nil || dec == nil {
		t.Fatalf("expected decision, got problem=%v", prob)
	}
	if dec.TargetProfile == nil {
		t.Fatal("expected target profile")
	}
	if dec.TargetProfile.Audio.BitrateKbps != 320 {
		t.Fatalf("expected quality audio bitrate, got %#v", dec.TargetProfile.Audio)
	}
	if dec.Trace.RequestedIntent != string(playbackprofile.IntentQuality) || dec.Trace.ResolvedIntent != string(playbackprofile.IntentQuality) {
		t.Fatalf("expected quality intent trace, got %#v", dec.Trace)
	}
	if dec.Trace.QualityRung != string(playbackprofile.RungQualityAudioAAC320Stereo) {
		t.Fatalf("expected quality ladder rung, got %#v", dec.Trace)
	}
	if dec.Trace.AudioQualityRung != string(playbackprofile.RungQualityAudioAAC320Stereo) || dec.Trace.VideoQualityRung != "" {
		t.Fatalf("expected quality audio ladder trace, got %#v", dec.Trace)
	}
}

func TestDecide_TranscodeAudioRepairIntentUsesSaferAACBitrate(t *testing.T) {
	input := DecisionInput{
		RequestedIntent: playbackprofile.IntentRepair,
		Source: Source{
			Container:  "mp4",
			VideoCodec: "h264",
			AudioCodec: "ac3",
		},
		Capabilities: Capabilities{
			Version:       1,
			Containers:    []string{"mp4"},
			VideoCodecs:   []string{"h264"},
			AudioCodecs:   []string{"aac"},
			SupportsHLS:   true,
			SupportsRange: nil,
		},
		Policy:     Policy{AllowTranscode: true},
		APIVersion: "v3",
	}

	_, dec, prob := Decide(t.Context(), input, "test")
	if prob != nil || dec == nil {
		t.Fatalf("expected decision, got problem=%v", prob)
	}
	if dec.TargetProfile == nil {
		t.Fatal("expected target profile")
	}
	if dec.TargetProfile.Audio.BitrateKbps != 192 {
		t.Fatalf("expected repair audio bitrate, got %#v", dec.TargetProfile.Audio)
	}
	if dec.Trace.RequestedIntent != string(playbackprofile.IntentRepair) || dec.Trace.ResolvedIntent != string(playbackprofile.IntentRepair) {
		t.Fatalf("expected repair intent trace, got %#v", dec.Trace)
	}
	if dec.Trace.QualityRung != string(playbackprofile.RungRepairAudioAAC192Stereo) {
		t.Fatalf("expected repair ladder rung, got %#v", dec.Trace)
	}
	if dec.Trace.AudioQualityRung != string(playbackprofile.RungRepairAudioAAC192Stereo) || dec.Trace.VideoQualityRung != "" {
		t.Fatalf("expected repair audio ladder trace, got %#v", dec.Trace)
	}
}

func TestDecide_TranscodeDirectIntentDegradesToCompatible(t *testing.T) {
	input := DecisionInput{
		RequestedIntent: playbackprofile.IntentDirect,
		Source: Source{
			Container:  "mp4",
			VideoCodec: "h264",
			AudioCodec: "ac3",
		},
		Capabilities: Capabilities{
			Version:       1,
			Containers:    []string{"mp4"},
			VideoCodecs:   []string{"h264"},
			AudioCodecs:   []string{"aac"},
			SupportsHLS:   true,
			SupportsRange: nil,
		},
		Policy:     Policy{AllowTranscode: true},
		APIVersion: "v3",
	}

	_, dec, prob := Decide(t.Context(), input, "test")
	if prob != nil || dec == nil {
		t.Fatalf("expected decision, got problem=%v", prob)
	}
	if dec.TargetProfile == nil {
		t.Fatal("expected target profile")
	}
	if dec.Trace.RequestedIntent != string(playbackprofile.IntentDirect) {
		t.Fatalf("expected direct requested intent, got %#v", dec.Trace)
	}
	if dec.Trace.ResolvedIntent != string(playbackprofile.IntentCompatible) {
		t.Fatalf("expected compatible resolved intent, got %#v", dec.Trace)
	}
	if dec.Trace.DegradedFrom != string(playbackprofile.IntentDirect) {
		t.Fatalf("expected degradedFrom=direct, got %#v", dec.Trace)
	}
}

func TestDecide_OperatorForceRepairOverridesDirectPlayToTranscode(t *testing.T) {
	trueVal := true
	input := DecisionInput{
		Source: Source{
			Container:  "mp4",
			VideoCodec: "h264",
			AudioCodec: "aac",
		},
		Capabilities: Capabilities{
			Version:       1,
			Containers:    []string{"mp4"},
			VideoCodecs:   []string{"h264"},
			AudioCodecs:   []string{"aac"},
			SupportsHLS:   true,
			SupportsRange: &trueVal,
		},
		Policy: Policy{
			AllowTranscode: true,
			Operator: OperatorPolicy{
				ForceIntent: playbackprofile.IntentRepair,
			},
		},
		APIVersion: "v3",
	}

	_, dec, prob := Decide(t.Context(), input, "test")
	if prob != nil || dec == nil {
		t.Fatalf("expected decision, got problem=%v", prob)
	}
	if dec.Mode != ModeTranscode {
		t.Fatalf("expected forced repair to choose transcode, got %s", dec.Mode)
	}
	if dec.Trace.ForcedIntent != string(playbackprofile.IntentRepair) || !dec.Trace.OverrideApplied {
		t.Fatalf("expected operator override trace, got %#v", dec.Trace)
	}
}

func TestDecide_OperatorMaxQualityRungCapsQualityIntent(t *testing.T) {
	input := DecisionInput{
		RequestedIntent: playbackprofile.IntentQuality,
		Source: Source{
			Container:  "mp4",
			VideoCodec: "h264",
			AudioCodec: "ac3",
		},
		Capabilities: Capabilities{
			Version:       1,
			Containers:    []string{"mp4"},
			VideoCodecs:   []string{"h264"},
			AudioCodecs:   []string{"aac"},
			SupportsHLS:   true,
			SupportsRange: nil,
		},
		Policy: Policy{
			AllowTranscode: true,
			Operator: OperatorPolicy{
				MaxQualityRung: playbackprofile.RungCompatibleAudioAAC256Stereo,
			},
		},
		APIVersion: "v3",
	}

	_, dec, prob := Decide(t.Context(), input, "test")
	if prob != nil || dec == nil {
		t.Fatalf("expected decision, got problem=%v", prob)
	}
	if dec.TargetProfile == nil {
		t.Fatal("expected target profile")
	}
	if dec.TargetProfile.Audio.BitrateKbps != 256 {
		t.Fatalf("expected quality cap to reduce bitrate to 256, got %#v", dec.TargetProfile.Audio)
	}
	if dec.Trace.MaxQualityRung != string(playbackprofile.RungCompatibleAudioAAC256Stereo) || !dec.Trace.OverrideApplied {
		t.Fatalf("expected operator max quality rung trace, got %#v", dec.Trace)
	}
}

func TestDecide_TranscodeVideoQualityIntentUsesExplicitVideoLadder(t *testing.T) {
	input := DecisionInput{
		RequestedIntent: playbackprofile.IntentQuality,
		Source: Source{
			Container:  "mp4",
			VideoCodec: "hevc",
			AudioCodec: "aac",
			Width:      1920,
			Height:     1080,
			FPS:        25,
		},
		Capabilities: Capabilities{
			Version:       1,
			Containers:    []string{"mp4"},
			VideoCodecs:   []string{"h264"},
			AudioCodecs:   []string{"aac"},
			SupportsHLS:   true,
			SupportsRange: nil,
		},
		Policy:     Policy{AllowTranscode: true},
		APIVersion: "v3",
	}

	_, dec, prob := Decide(t.Context(), input, "test")
	if prob != nil || dec == nil {
		t.Fatalf("expected decision, got problem=%v", prob)
	}
	if dec.TargetProfile == nil {
		t.Fatal("expected target profile")
	}
	if dec.TargetProfile.Video.Mode != playbackprofile.MediaModeTranscode || dec.TargetProfile.Video.Codec != "h264" {
		t.Fatalf("expected h264 video transcode target, got %#v", dec.TargetProfile.Video)
	}
	if dec.TargetProfile.Video.CRF != 20 || dec.TargetProfile.Video.Preset != "slow" {
		t.Fatalf("expected quality video ladder crf/preset, got %#v", dec.TargetProfile.Video)
	}
	if dec.TargetProfile.Audio.Mode != playbackprofile.MediaModeCopy || dec.TargetProfile.Audio.Codec != "aac" {
		t.Fatalf("expected audio copy target, got %#v", dec.TargetProfile.Audio)
	}
	if dec.Trace.QualityRung != string(playbackprofile.RungQualityVideoH264CRF20) {
		t.Fatalf("expected legacy quality rung to follow video ladder, got %#v", dec.Trace)
	}
	if dec.Trace.VideoQualityRung != string(playbackprofile.RungQualityVideoH264CRF20) || dec.Trace.AudioQualityRung != "" {
		t.Fatalf("expected video-only ladder trace, got %#v", dec.Trace)
	}
}

func TestDecide_HostPressureDegradesQualityIntentToCompatible(t *testing.T) {
	input := DecisionInput{
		RequestedIntent: playbackprofile.IntentQuality,
		Source: Source{
			Container:  "mp4",
			VideoCodec: "h264",
			AudioCodec: "ac3",
		},
		Capabilities: Capabilities{
			Version:       1,
			Containers:    []string{"mp4"},
			VideoCodecs:   []string{"h264"},
			AudioCodecs:   []string{"aac"},
			SupportsHLS:   true,
			SupportsRange: nil,
		},
		Policy: Policy{
			AllowTranscode: true,
			Host: HostPolicy{
				PressureBand: playbackprofile.HostPressureConstrained,
			},
		},
		APIVersion: "v3",
	}

	_, dec, prob := Decide(t.Context(), input, "test")
	if prob != nil || dec == nil {
		t.Fatalf("expected decision, got problem=%v", prob)
	}
	if dec.TargetProfile == nil {
		t.Fatal("expected target profile")
	}
	if dec.TargetProfile.Audio.BitrateKbps != 256 {
		t.Fatalf("expected host pressure to clamp quality audio bitrate to 256, got %#v", dec.TargetProfile.Audio)
	}
	if dec.Trace.ResolvedIntent != string(playbackprofile.IntentCompatible) {
		t.Fatalf("expected compatible resolved intent under host pressure, got %#v", dec.Trace)
	}
	if dec.Trace.QualityRung != string(playbackprofile.RungCompatibleAudioAAC256Stereo) {
		t.Fatalf("expected compatible ladder rung under host pressure, got %#v", dec.Trace)
	}
	if dec.Trace.AudioQualityRung != string(playbackprofile.RungCompatibleAudioAAC256Stereo) || dec.Trace.VideoQualityRung != "" {
		t.Fatalf("expected host pressure to keep audio-only ladder trace, got %#v", dec.Trace)
	}
	if dec.Trace.DegradedFrom != string(playbackprofile.IntentQuality) {
		t.Fatalf("expected degradedFrom=quality under host pressure, got %#v", dec.Trace)
	}
	if dec.Trace.OverrideApplied {
		t.Fatalf("expected host pressure not to flip operator override trace, got %#v", dec.Trace)
	}
}
