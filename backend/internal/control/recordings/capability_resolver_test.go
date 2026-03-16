package recordings

import (
	"context"
	"testing"

	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
)

func TestResolveCapabilities_RuntimeProbeUsesFamilyForIdentityOnly(t *testing.T) {
	supportsRange := true
	in := capabilities.PlaybackCapabilities{
		CapabilitiesVersion:  2,
		Containers:           []string{"mp4", "ts"},
		VideoCodecs:          []string{"hevc", "h264"},
		AudioCodecs:          []string{"aac", "mp3", "ac3"},
		SupportsHLS:          true,
		SupportsHLSExplicit:  true,
		SupportsRange:        &supportsRange,
		DeviceType:           "web",
		HLSEngines:           []string{"native"},
		PreferredHLSEngine:   "native",
		RuntimeProbeUsed:     true,
		RuntimeProbeVersion:  1,
		ClientFamilyFallback: "safari_native",
	}

	got := ResolveCapabilities(context.Background(), "", "v3.1", "", nil, &in)

	if got.DeviceType != "safari" {
		t.Fatalf("expected family fallback to tighten generic device type, got %q", got.DeviceType)
	}
	if got.ClientCapsSource != capabilities.ClientCapsSourceRuntimePlusFam {
		t.Fatalf("expected runtime_plus_family source, got %q", got.ClientCapsSource)
	}
	if got.PreferredHLSEngine != "native" {
		t.Fatalf("expected runtime preferred hls engine to win, got %q", got.PreferredHLSEngine)
	}
	if len(got.VideoCodecs) != 2 || got.VideoCodecs[0] != "h264" || got.VideoCodecs[1] != "hevc" {
		t.Fatalf("expected runtime video codecs to stay intact, got %#v", got.VideoCodecs)
	}
}

func TestResolveCapabilities_FamilyFallbackFillsMissingOptionalFields(t *testing.T) {
	in := capabilities.PlaybackCapabilities{
		CapabilitiesVersion:  2,
		Containers:           []string{"mp4", "ts"},
		VideoCodecs:          []string{"h264"},
		AudioCodecs:          []string{"aac", "mp3"},
		DeviceType:           "web",
		ClientFamilyFallback: "firefox_hlsjs",
	}

	got := ResolveCapabilities(context.Background(), "", "v3.1", "", nil, &in)

	if got.DeviceType != "firefox" {
		t.Fatalf("expected firefox device type from family fallback, got %q", got.DeviceType)
	}
	if got.ClientCapsSource != capabilities.ClientCapsSourceFamilyFallback {
		t.Fatalf("expected family_fallback source, got %q", got.ClientCapsSource)
	}
	if got.PreferredHLSEngine != "hlsjs" {
		t.Fatalf("expected preferred hlsjs engine from family fallback, got %q", got.PreferredHLSEngine)
	}
	if len(got.HLSEngines) != 1 || got.HLSEngines[0] != "hlsjs" {
		t.Fatalf("expected hls.js engine from family fallback, got %#v", got.HLSEngines)
	}
}

func TestResolveCapabilities_UnknownFamilyLeavesRuntimeTruthUntouched(t *testing.T) {
	in := capabilities.PlaybackCapabilities{
		CapabilitiesVersion:  2,
		Containers:           []string{"mp4", "ts"},
		VideoCodecs:          []string{"h264"},
		AudioCodecs:          []string{"aac"},
		DeviceType:           "web",
		RuntimeProbeUsed:     true,
		RuntimeProbeVersion:  1,
		ClientFamilyFallback: "unknown_browser",
	}

	got := ResolveCapabilities(context.Background(), "", "v3.1", "", nil, &in)

	if got.DeviceType != "web" {
		t.Fatalf("expected unknown family to leave device type unchanged, got %q", got.DeviceType)
	}
	if got.ClientCapsSource != capabilities.ClientCapsSourceRuntime {
		t.Fatalf("expected runtime source, got %q", got.ClientCapsSource)
	}
}

func TestResolveCapabilities_FamilyFallbackKeepsIOSDistinctFromDesktopSafari(t *testing.T) {
	in := capabilities.PlaybackCapabilities{
		CapabilitiesVersion:  2,
		Containers:           []string{"mp4", "ts"},
		VideoCodecs:          []string{"h264"},
		AudioCodecs:          []string{"aac", "mp3"},
		DeviceType:           "web",
		ClientFamilyFallback: "ios_safari_native",
	}

	got := ResolveCapabilities(context.Background(), "", "v3.1", "", nil, &in)

	if got.DeviceType != "ios_safari" {
		t.Fatalf("expected ios safari device identity from family fallback, got %q", got.DeviceType)
	}
	if got.ClientCapsSource != capabilities.ClientCapsSourceFamilyFallback {
		t.Fatalf("expected family_fallback source, got %q", got.ClientCapsSource)
	}
}

func TestResolveCapabilities_FullRuntimeProbeDoesNotGetExpandedByFamily(t *testing.T) {
	supportsRange := true
	in := capabilities.PlaybackCapabilities{
		CapabilitiesVersion:  2,
		Containers:           []string{"mp4", "ts", "fmp4"},
		VideoCodecs:          []string{"h264"},
		AudioCodecs:          []string{"aac", "mp3"},
		SupportsHLS:          true,
		SupportsHLSExplicit:  true,
		SupportsRange:        &supportsRange,
		DeviceType:           "firefox",
		HLSEngines:           []string{"hlsjs"},
		PreferredHLSEngine:   "hlsjs",
		RuntimeProbeUsed:     true,
		RuntimeProbeVersion:  1,
		ClientFamilyFallback: "chromium_hlsjs",
	}

	got := ResolveCapabilities(context.Background(), "", "v3.1", "", nil, &in)

	if got.DeviceType != "firefox" {
		t.Fatalf("expected explicit runtime device identity to win, got %q", got.DeviceType)
	}
	if got.ClientCapsSource != capabilities.ClientCapsSourceRuntimePlusFam {
		t.Fatalf("expected runtime_plus_family source when family fallback enriches the runtime probe, got %q", got.ClientCapsSource)
	}
	if len(got.VideoCodecs) != 1 || got.VideoCodecs[0] != "h264" {
		t.Fatalf("expected runtime codec set to remain conservative, got %#v", got.VideoCodecs)
	}
}
