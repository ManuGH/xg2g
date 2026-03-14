package decision

import "testing"

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
}
