package capabilities

import "testing"

func TestToClientPlaybackProfile_MapsLegacyCapabilities(t *testing.T) {
	allow := false
	supportsRange := true
	in := PlaybackCapabilities{
		CapabilitiesVersion: 1,
		Containers:          []string{" ts ", "mp4", "ts"},
		VideoCodecs:         []string{" h264 ", "hevc"},
		AudioCodecs:         []string{" ac3 ", "aac"},
		SupportsHLS:         true,
		DeviceType:          " Safari ",
		HLSEngines:          []string{" Native "},
		PreferredHLSEngine:  "NATIVE",
		AllowTranscode:      &allow,
		SupportsRange:       &supportsRange,
		MaxVideo: &MaxVideo{
			Width:  1920,
			Height: 1080,
			Fps:    60,
		},
	}

	got := ToClientPlaybackProfile(in)

	if got.DeviceType != "safari" {
		t.Fatalf("expected normalized device type, got %q", got.DeviceType)
	}
	if got.PlaybackEngine != "native_hls" {
		t.Fatalf("expected playback engine to be derived from preferred hls engine, got %q", got.PlaybackEngine)
	}
	if !got.SupportsHLS || !got.SupportsRange {
		t.Fatalf("expected supports flags to be preserved: %#v", got)
	}
	if got.AllowTranscode == nil || *got.AllowTranscode {
		t.Fatalf("expected allowTranscode=false to be preserved: %#v", got.AllowTranscode)
	}
	if len(got.Containers) != 2 || got.Containers[0] != "mp4" || got.Containers[1] != "mpegts" {
		t.Fatalf("unexpected canonical containers: %#v", got.Containers)
	}
	if len(got.HLSPackaging) != 1 || got.HLSPackaging[0] != "ts" {
		t.Fatalf("unexpected hls packaging mapping: %#v", got.HLSPackaging)
	}
	if got.MaxVideo == nil || got.MaxVideo.Width != 1920 || got.MaxVideo.Height != 1080 || got.MaxVideo.FPS != 60 {
		t.Fatalf("unexpected maxVideo mapping: %#v", got.MaxVideo)
	}
}
