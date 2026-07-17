package intents

import (
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

// TestRequestedCodecsForIntent_AppleAV1DecodeGate clamps the real av1-service
// gate (requestedCodecsForIntent) for Apple devices in BOTH directions — the
// asymmetry that matters: an Apple device WITHOUT a hardware AV1 decoder (M2 Mac,
// iPhone 14/A16) must NOT get av1 in its requested-codec list (serving it would
// be a guaranteed black screen — Apple has no software AV1 decoder), and one WITH
// a decoder (M3 Mac, iPhone 16) MUST get av1. The existing both-direction tests
// cover Android TV/Shield only; this covers the "Safari is reference" devices.
func TestRequestedCodecsForIntent_AppleAV1DecodeGate(t *testing.T) {
	smooth := true
	mk := func(fam, os, osver, deviceModel, dtype string) Intent {
		return Intent{
			Params: map[string]string{
				"playback_mode":          "native_hls",
				model.CtxKeyClientFamily: fam,
			},
			ClientCaps: &capabilities.PlaybackCapabilities{
				ClientFamilyFallback: fam,
				ClientCapsSource:     capabilities.ClientCapsSourceRuntime,
				Containers:           []string{"mp4", "ts", "fmp4"},
				VideoCodecs:          []string{"av1", "hevc", "h264"},
				SupportsHLS:          true,
				RuntimeProbeUsed:     true,
				RuntimeProbeVersion:  2,
				DeviceType:           dtype,
				DeviceContext:        &capabilities.DeviceContext{OSName: os, OSVersion: osver, Model: deviceModel},
				VideoCodecSignals: []capabilities.VideoCodecSignal{
					{Codec: "av1", Supported: true, Smooth: &smooth},
					{Codec: "hevc", Supported: true, Smooth: &smooth},
				},
			},
		}
	}
	cases := []struct {
		name    string
		intent  Intent
		wantAV1 bool
	}{
		{"mac m2 (no hw av1) excluded", mk(playbackprofile.ClientSafariNative, "macos", "14.4", "MacBook Pro M2", ""), false},
		{"mac m3 (hw av1) included", mk(playbackprofile.ClientSafariNative, "macos", "14.4", "MacBook Pro M3", ""), true},
		{"iphone 14 a16 excluded", mk(playbackprofile.ClientIOSSafariNative, "ios", "18.1", "iPhone 14 Pro A16", "iphone"), false},
		{"iphone 16 included", mk(playbackprofile.ClientIOSSafariNative, "ios", "18.1", "iPhone 16 Pro", "iphone"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := requestedCodecsForIntentWithPolicy(tc.intent, "native_hls", false)
			hasAV1 := strings.Contains(got, "av1")
			t.Logf("%s -> requested codecs %q", tc.name, got)
			if hasAV1 != tc.wantAV1 {
				t.Fatalf("av1 present=%v, want %v (codecs %q)", hasAV1, tc.wantAV1, got)
			}
		})
	}
}
