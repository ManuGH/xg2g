package clientpolicy

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

func TestResolveProfileUserAgent_NativeHLSIOSSafariBypassesUA(t *testing.T) {
	got := ResolveProfileUserAgent("native_hls", playbackprofile.ClientIOSSafariNative, "ua-string")
	if got != "" {
		t.Fatalf("ResolveProfileUserAgent() = %q, want empty string", got)
	}
}

func TestResolveProfileUserAgent_DefaultModeKeepsUA(t *testing.T) {
	got := ResolveProfileUserAgent("", playbackprofile.ClientIOSSafariNative, "ua-string")
	if got != "ua-string" {
		t.Fatalf("ResolveProfileUserAgent() = %q, want %q", got, "ua-string")
	}
}

func TestApplyStartPackagingPolicy_IOSAV1ForcesFMP4(t *testing.T) {
	spec := ApplyStartPackagingPolicy(
		playbackprofile.ClientIOSSafariNative,
		profiles.ProfileAV1HW,
		model.ProfileSpec{Container: "mpegts"},
	)
	if spec.Container != "fmp4" {
		t.Fatalf("ApplyStartPackagingPolicy() container = %q, want %q", spec.Container, "fmp4")
	}
}

func TestApplyStartPackagingPolicy_DesktopSafariKeepsExperimentalAV1TS(t *testing.T) {
	spec := ApplyStartPackagingPolicy(
		playbackprofile.ClientSafariNative,
		profiles.ProfileAV1HW,
		model.ProfileSpec{Container: "mpegts"},
	)
	if spec.Container != "mpegts" {
		t.Fatalf("ApplyStartPackagingPolicy() container = %q, want %q", spec.Container, "mpegts")
	}
}

func TestWantsFMP4Packaging_ClientFamilyFallback(t *testing.T) {
	if !WantsFMP4Packaging("", playbackprofile.ClientIOSSafariNative) {
		t.Fatal("expected ios_safari_native to prefer fmp4 packaging")
	}
	if WantsFMP4Packaging("", "chromium_hlsjs") {
		t.Fatal("did not expect chromium_hlsjs to prefer fmp4 packaging by default")
	}
}

func TestAllowExperimentalNativeAV1TransportStream_DesktopSafariOnly(t *testing.T) {
	t.Setenv("XG2G_EXPERIMENTAL_AV1_MPEGTS_ENABLED", "true")

	target := playbackprofile.TargetPlaybackProfile{
		Container: "mpegts",
		Packaging: playbackprofile.PackagingTS,
		Video: playbackprofile.VideoTarget{
			Codec: "av1",
		},
		HLS: playbackprofile.HLSTarget{
			Enabled:          true,
			SegmentContainer: "mpegts",
		},
	}

	if !AllowExperimentalNativeAV1TransportStream(capabilities.PlaybackCapabilities{
		ClientFamilyFallback: playbackprofile.ClientSafariNative,
		ClientCapsSource:     capabilities.ClientCapsSourceRuntime,
		VideoCodecs:          []string{"av1", "hevc", "h264"},
		Containers:           []string{"ts", "fmp4"},
	}, "av1", target) {
		t.Fatal("expected desktop Safari runtime AV1 TS experiment to stay enabled")
	}

	if AllowExperimentalNativeAV1TransportStream(capabilities.PlaybackCapabilities{
		ClientFamilyFallback: playbackprofile.ClientIOSSafariNative,
		ClientCapsSource:     capabilities.ClientCapsSourceRuntime,
		VideoCodecs:          []string{"av1", "hevc", "h264"},
		Containers:           []string{"ts", "fmp4"},
	}, "av1", target) {
		t.Fatal("did not expect iOS Safari to keep the AV1 TS experiment")
	}
}
