package hardware

import "testing"

func TestSnapshotTranscodeCapabilities_ReflectsVerifiedEncoders(t *testing.T) {
	resetVaapiState(t)

	SetVAAPIPreflightResult(true)
	SetVAAPIEncoderPreflight(map[string]bool{
		"hevc_vaapi": true,
		"h264_vaapi": true,
		"av1_vaapi":  false,
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
	if len(got.HardwareVideoCodec) != 2 || got.HardwareVideoCodec[0] != "h264" || got.HardwareVideoCodec[1] != "hevc" {
		t.Fatalf("unexpected hardware codec snapshot: %#v", got.HardwareVideoCodec)
	}
}
