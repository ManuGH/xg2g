package capabilities

import "testing"

func TestResolveRuntimeProbeCapabilities_PreservesRuntimePlusFamilyAcrossRepeatedNormalization(t *testing.T) {
	t.Parallel()

	raw := PlaybackCapabilities{
		CapabilitiesVersion:  3,
		Containers:           []string{"ts"},
		VideoCodecs:          []string{"h264"},
		AudioCodecs:          []string{"aac"},
		SupportsHLS:          true,
		SupportsHLSExplicit:  true,
		DeviceType:           "web",
		PreferredHLSEngine:   "hlsjs",
		RuntimeProbeUsed:     true,
		RuntimeProbeVersion:  2,
		ClientFamilyFallback: "chromium_hlsjs",
	}

	first := ResolveRuntimeProbeCapabilities(raw)
	if first.ClientCapsSource != ClientCapsSourceRuntimePlusFam {
		t.Fatalf("first normalization source = %q, want %q", first.ClientCapsSource, ClientCapsSourceRuntimePlusFam)
	}

	second := ResolveRuntimeProbeCapabilities(first)
	if second.ClientCapsSource != ClientCapsSourceRuntimePlusFam {
		t.Fatalf("second normalization source = %q, want %q", second.ClientCapsSource, ClientCapsSourceRuntimePlusFam)
	}
	if second.DeviceType != "chromium" {
		t.Fatalf("second normalization deviceType = %q, want %q", second.DeviceType, "chromium")
	}
}
