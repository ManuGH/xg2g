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
		"", "",
	)
	if spec.Container != "fmp4" {
		t.Fatalf("ApplyStartPackagingPolicy() container = %q, want %q", spec.Container, "fmp4")
	}
}

func TestApplyStartPackagingPolicy_DesktopSafariAV1ForcesFMP4(t *testing.T) {
	spec := ApplyStartPackagingPolicy(
		playbackprofile.ClientSafariNative,
		profiles.ProfileAV1HW,
		model.ProfileSpec{Container: "mpegts"},
		"", "",
	)
	if spec.Container != "fmp4" {
		t.Fatalf("ApplyStartPackagingPolicy() container = %q, want %q", spec.Container, "fmp4")
	}
}

func TestApplyStartPackagingPolicy_DesktopSafariHEVCForcesFMP4(t *testing.T) {
	spec := ApplyStartPackagingPolicy(
		playbackprofile.ClientSafariNative,
		profiles.ProfileSafariHEVCHW,
		model.ProfileSpec{Container: "mpegts"},
		"", "",
	)
	if spec.Container != "fmp4" {
		t.Fatalf("ApplyStartPackagingPolicy() container = %q, want %q", spec.Container, "fmp4")
	}
}

func TestApplyStartPackagingPolicy_NonWebKitHEVCKeepsContainer(t *testing.T) {
	spec := ApplyStartPackagingPolicy(
		"chromium_hlsjs",
		profiles.ProfileSafariHEVCHW,
		model.ProfileSpec{Container: "mpegts"},
		"", "",
	)
	if spec.Container != "mpegts" {
		t.Fatalf("ApplyStartPackagingPolicy() container = %q, want %q", spec.Container, "mpegts")
	}
}

// Native-HEVC-copy branch: an HEVC source copied for a Safari-native client that
// asked for the native engine must flip mpegts -> fmp4 so the hvc1 path engages.
func TestApplyStartPackagingPolicy_SafariNativeHEVCCopyForcesFMP4(t *testing.T) {
	spec := ApplyStartPackagingPolicy(
		playbackprofile.ClientSafariNative,
		profiles.ProfileSafari,
		model.ProfileSpec{Name: "safari", Container: "mpegts", TranscodeVideo: false},
		"hevc", "native",
	)
	if spec.Container != "fmp4" {
		t.Fatalf("ApplyStartPackagingPolicy() container = %q, want %q", spec.Container, "fmp4")
	}
	if spec.VideoCodec != "hevc" {
		t.Fatalf("ApplyStartPackagingPolicy() videoCodec = %q, want %q (so the hvc1 tag engages)", spec.VideoCodec, "hevc")
	}
	if spec.TranscodeVideo {
		t.Fatalf("ApplyStartPackagingPolicy() must stay a copy (TranscodeVideo=false)")
	}
}

// iOS Safari native resolves to fMP4 already; the HEVC copy must still get
// VideoCodec=hevc so the hvc1 tag engages (else hev1 + per-keyframe flash).
func TestApplyStartPackagingPolicy_IOSNativeHEVCCopyAlreadyFMP4PinsHevc(t *testing.T) {
	spec := ApplyStartPackagingPolicy(
		playbackprofile.ClientIOSSafariNative,
		profiles.ProfileSafari,
		model.ProfileSpec{Name: "safari", Container: "fmp4", TranscodeVideo: false},
		"hevc", "native",
	)
	if spec.Container != "fmp4" {
		t.Fatalf("container = %q, want fmp4", spec.Container)
	}
	if spec.VideoCodec != "hevc" {
		t.Fatalf("videoCodec = %q, want hevc (so the hvc1 tag engages on iOS)", spec.VideoCodec)
	}
}

// Same shape but the client did NOT request native (hls.js) -> stays mpegts.
func TestApplyStartPackagingPolicy_HEVCCopyHlsjsKeepsMpegts(t *testing.T) {
	spec := ApplyStartPackagingPolicy(
		playbackprofile.ClientSafariNative,
		profiles.ProfileSafari,
		model.ProfileSpec{Container: "mpegts", TranscodeVideo: false},
		"hevc", "hlsjs",
	)
	if spec.Container != "mpegts" {
		t.Fatalf("ApplyStartPackagingPolicy() container = %q, want %q", spec.Container, "mpegts")
	}
}

// H.264 copy with native requested -> stays mpegts (HEVC-only gate).
func TestApplyStartPackagingPolicy_H264CopyNativeKeepsMpegts(t *testing.T) {
	spec := ApplyStartPackagingPolicy(
		playbackprofile.ClientSafariNative,
		profiles.ProfileSafari,
		model.ProfileSpec{Container: "mpegts", TranscodeVideo: false},
		"h264", "native",
	)
	if spec.Container != "mpegts" {
		t.Fatalf("ApplyStartPackagingPolicy() container = %q, want %q", spec.Container, "mpegts")
	}
}

// HEVC copy + native but non-Safari client -> early return, stays mpegts.
func TestApplyStartPackagingPolicy_HEVCCopyNonSafariKeepsMpegts(t *testing.T) {
	spec := ApplyStartPackagingPolicy(
		"chromium_hlsjs",
		profiles.ProfileSafari,
		model.ProfileSpec{Container: "mpegts", TranscodeVideo: false},
		"hevc", "native",
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

func TestAllowExperimentalNativeAV1TransportStream_DisablesNativeWebKitAV1TS(t *testing.T) {
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

	if AllowExperimentalNativeAV1TransportStream(capabilities.PlaybackCapabilities{
		ClientFamilyFallback: playbackprofile.ClientSafariNative,
		ClientCapsSource:     capabilities.ClientCapsSourceRuntime,
		VideoCodecs:          []string{"av1", "hevc", "h264"},
		Containers:           []string{"ts", "fmp4"},
	}, "av1", target) {
		t.Fatal("did not expect desktop Safari to keep the AV1 TS experiment")
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
