package playbackprofile

import "testing"

func TestCanonicalizeClient_NormalizesTokensAndSlices(t *testing.T) {
	allow := true
	in := ClientPlaybackProfile{
		DeviceType:     "  Safari  ",
		PlaybackEngine: "  HLS_JS ",
		Containers:     []string{" TS ", "mp4", "ts", ""},
		VideoCodecs:    []string{" H264 ", "hevc", "H264"},
		AudioCodecs:    []string{" ac3 ", "AAC", "aac"},
		HLSPackaging:   []string{" FMP4 ", "ts", "fmp4"},
		SupportsHLS:    true,
		SupportsRange:  true,
		AllowTranscode: &allow,
		MaxVideo: &VideoConstraints{
			Width:  1920,
			Height: 1080,
			FPS:    60,
		},
	}

	got := CanonicalizeClient(in)

	if got.DeviceType != "safari" {
		t.Fatalf("expected normalized device type, got %q", got.DeviceType)
	}
	if got.PlaybackEngine != "hls_js" {
		t.Fatalf("expected normalized playback engine, got %q", got.PlaybackEngine)
	}
	if len(got.Containers) != 2 || got.Containers[0] != "mp4" || got.Containers[1] != "ts" {
		t.Fatalf("unexpected canonical containers: %#v", got.Containers)
	}
	if len(got.VideoCodecs) != 2 || got.VideoCodecs[0] != "h264" || got.VideoCodecs[1] != "hevc" {
		t.Fatalf("unexpected canonical video codecs: %#v", got.VideoCodecs)
	}
	if len(got.AudioCodecs) != 2 || got.AudioCodecs[0] != "aac" || got.AudioCodecs[1] != "ac3" {
		t.Fatalf("unexpected canonical audio codecs: %#v", got.AudioCodecs)
	}
	if len(got.HLSPackaging) != 2 || got.HLSPackaging[0] != "fmp4" || got.HLSPackaging[1] != "ts" {
		t.Fatalf("unexpected canonical hls packaging: %#v", got.HLSPackaging)
	}
	if got.AllowTranscode == nil || !*got.AllowTranscode {
		t.Fatal("expected allowTranscode to be preserved")
	}
}

func TestCanonicalizeTarget_NormalizesMediaAndHwFields(t *testing.T) {
	in := TargetPlaybackProfile{
		Container: " MPEGTS ",
		Packaging: Packaging(" TS "),
		Video: VideoTarget{
			Mode:  MediaMode(" TRANSCODE "),
			Codec: " H264 ",
		},
		Audio: AudioTarget{
			Mode:        MediaMode(" copy "),
			Codec:       " AAC ",
			Channels:    -2,
			BitrateKbps: -1,
			SampleRate:  -1,
		},
		HLS: HLSTarget{
			Enabled:          true,
			SegmentContainer: " MPEGTS ",
			SegmentSeconds:   -3,
		},
	}

	got := CanonicalizeTarget(in)

	if got.Container != "mpegts" {
		t.Fatalf("expected normalized container, got %q", got.Container)
	}
	if got.Packaging != PackagingTS {
		t.Fatalf("expected normalized packaging, got %q", got.Packaging)
	}
	if got.HWAccel != HWAccelNone {
		t.Fatalf("expected default hwaccel none, got %q", got.HWAccel)
	}
	if got.Video.Mode != MediaModeTranscode || got.Video.Codec != "h264" {
		t.Fatalf("unexpected canonical video target: %#v", got.Video)
	}
	if got.Audio.Mode != MediaModeCopy || got.Audio.Codec != "aac" {
		t.Fatalf("unexpected canonical audio target: %#v", got.Audio)
	}
	if got.Audio.Channels != 0 || got.Audio.BitrateKbps != 0 || got.Audio.SampleRate != 0 {
		t.Fatalf("expected negative audio values to normalize to zero: %#v", got.Audio)
	}
	if got.HLS.SegmentContainer != "mpegts" || got.HLS.SegmentSeconds != 0 {
		t.Fatalf("unexpected canonical hls target: %#v", got.HLS)
	}
}
