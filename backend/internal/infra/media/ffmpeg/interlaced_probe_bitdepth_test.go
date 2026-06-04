package ffmpeg

import (
	"strings"
	"testing"
)

// The interlaced path-correctness probe must verify the bit depth production
// actually serves: AV1 at p010le (10-bit), H.264/HEVC at nv12 (8-bit). Probing
// nv12 for AV1 "verifies" an 8-bit path that is never served (production uploads
// AV1 as p010le, see encode_args.go). Negative control: route AV1 back to nv12
// and these go red.
func TestVaapiProductionUploadFormat_MatchesProductionBitDepth(t *testing.T) {
	cases := map[string]string{
		"av1": "p010le", "av1_vaapi": "p010le", "libsvtav1": "p010le",
		"hevc": "nv12", "hevc_vaapi": "nv12", "h264": "nv12", "h264_vaapi": "nv12",
	}
	for enc, want := range cases {
		if got := vaapiProductionUploadFormat(enc); got != want {
			t.Errorf("vaapiProductionUploadFormat(%q) = %q, want %q", enc, got, want)
		}
	}
}

func TestInterlacedCorrectnessFilters_AV1UsesProductionP010(t *testing.T) {
	eo := vaapiEncodeOnlyInterlacedCorrectnessFilter("av1_vaapi")
	if !strings.Contains(eo, "format=p010le") || strings.Contains(eo, "format=nv12") {
		t.Errorf("encode-only AV1 interlaced filter must upload p010le, got %q", eo)
	}
	if h := vaapiEncodeOnlyInterlacedCorrectnessFilter("hevc_vaapi"); !strings.Contains(h, "format=nv12") {
		t.Errorf("encode-only HEVC interlaced filter must upload nv12, got %q", h)
	}
}
