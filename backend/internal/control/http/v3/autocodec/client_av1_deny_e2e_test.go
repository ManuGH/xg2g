package autocodec

import (
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

// TestPickNativeHLSProfile_ExcludedClientGetsNoAV1 is the END-TO-END proof that an
// Apple device without a hardware AV1 decoder (M2 Mac, iPhone 14) is not merely
// denied by the policy gate but actually receives a NON-AV1 profile from the
// decision-engine entry — even though it declared av1 first and emitted a runtime
// av1 signal. Serving AV1 to such a device is a guaranteed black screen (Apple
// ships no software AV1 decoder), so this is the highest-risk path for the
// "Safari is reference" stance.
func TestPickNativeHLSProfile_ExcludedClientGetsNoAV1(t *testing.T) {
	smooth := true
	base := func() *capabilities.PlaybackCapabilities {
		return &capabilities.PlaybackCapabilities{
			Containers:           []string{"mp4", "ts", "fmp4"},
			VideoCodecs:          []string{"av1", "hevc", "h264"},
			RuntimeProbeUsed:     true,
			ClientCapsSource:     capabilities.ClientCapsSourceRuntimePlusFam,
			ClientFamilyFallback: playbackprofile.ClientChromiumHLSJS,
			VideoCodecSignals:    []capabilities.VideoCodecSignal{{Codec: "av1", Supported: true, Smooth: &smooth}},
		}
	}

	excluded := []struct {
		name   string
		family string
		dev    *capabilities.DeviceContext
		dtype  string
	}{
		{"mac m2 (no hw av1 decode)", playbackprofile.ClientSafariNative,
			&capabilities.DeviceContext{OSName: "macos", OSVersion: "14.4", Model: "MacBook Pro M2"}, ""},
		{"iphone 14 a16 (no hw av1 decode)", playbackprofile.ClientIOSSafariNative,
			&capabilities.DeviceContext{OSName: "ios", OSVersion: "18.1", Model: "iPhone 14 Pro A16"}, "iphone"},
	}
	for _, tc := range excluded {
		t.Run(tc.name, func(t *testing.T) {
			caps := base()
			caps.DeviceContext = tc.dev
			caps.DeviceType = tc.dtype

			// Precondition: the policy gate denies AV1.
			if ClientAV1PlaybackAllowed(*caps, tc.family) {
				t.Fatalf("precondition: %s must be denied AV1 by the policy gate", tc.name)
			}
			// END-TO-END: the decision-engine entry returns a NON-AV1 profile.
			got := PickNativeHLSProfileForClientAndHost("av1,hevc,h264", tc.family, caps, profiles.HWAccelAuto, playbackprofile.HostRuntimeSnapshot{})
			t.Logf("%s -> profile %q", tc.name, got)
			if got == profiles.ProfileAV1HW || strings.Contains(strings.ToLower(got), "av1") {
				t.Fatalf("%s must NOT receive an AV1 profile (black-screen risk); got %q", tc.name, got)
			}
		})
	}

	// Contrast: an M3 Mac (hardware AV1 decode) is NOT stripped by the gate.
	m3 := base()
	m3.DeviceContext = &capabilities.DeviceContext{OSName: "macos", OSVersion: "14.4", Model: "MacBook Pro M3"}
	if !ClientAV1PlaybackAllowed(*m3, playbackprofile.ClientSafariNative) {
		t.Fatal("contrast: M3 Mac SHOULD be allowed AV1 by the policy gate")
	}
}
