package hardware

import (
	"slices"
	"testing"
)

func TestSnapshotTranscodeCapabilities_ReflectsVerifiedEncoders(t *testing.T) {
	resetVaapiState(t)

	SetVAAPIPreflightResult(true)
	SetVAAPIEncoderPreflight(map[string]bool{
		"hevc_vaapi": true,
		"h264_vaapi": true,
		"av1_vaapi":  false,
	})
	SetNVENCPreflightResult(true)
	SetNVENCEncoderCapabilities(map[string]NVENCEncoderCapability{
		"av1_nvenc": {Verified: true, AutoEligible: false},
	})

	got := SnapshotTranscodeCapabilities(true, true)

	if !got.FFmpegAvailable || !got.HLSAvailable {
		t.Fatalf("expected ffmpeg/hls availability to be preserved: %#v", got)
	}
	if !got.VAAPIReady {
		t.Fatalf("expected VAAPIReady after successful preflight: %#v", got)
	}
	if got.HasVAAPI != HasVAAPI() {
		t.Fatalf("expected HasVAAPI to mirror runtime probe: got=%v runtime=%v", got.HasVAAPI, HasVAAPI())
	}
	if !got.NVENCReady {
		t.Fatalf("expected NVENCReady after successful preflight: %#v", got)
	}
	if !slices.Equal(got.HardwareVideoCodec, []string{"av1", "h264", "hevc"}) {
		t.Fatalf("unexpected hardware codec snapshot: %#v", got.HardwareVideoCodec)
	}
}
