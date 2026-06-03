package ffmpeg

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
)

// TestVaapiEncoder10BitVerified covers the input to the plan-time bit-depth gate:
// production drives AV1 at 10-bit (p010le), so plan_codec refuses the AV1 path
// unless the encoder was decode-verified at 10-bit, not merely at 8-bit.
func TestVaapiEncoder10BitVerified(t *testing.T) {
	d := &Detector{
		vaapiEncoderCaps: map[string]hardware.VAAPIEncoderCapability{
			// AV1 verified at both depths (e.g. AMD radeonsi that promotes p010).
			"av1_vaapi": {Verified: true, Verified8Bit: true, Verified10Bit: true},
			// HEVC verified at 8-bit only (the common case).
			"hevc_vaapi": {Verified: true, Verified8Bit: true, Verified10Bit: false},
		},
	}
	if !d.VaapiEncoder10BitVerified("av1_vaapi") {
		t.Error("av1_vaapi should be 10-bit verified")
	}
	if d.VaapiEncoder10BitVerified("hevc_vaapi") {
		t.Error("hevc_vaapi should NOT be 10-bit verified (8-bit only)")
	}
	if d.VaapiEncoder10BitVerified("av1_nvenc") {
		t.Error("absent encoder must not report 10-bit verified")
	}
}

// TestEncoderCapabilityBitDepthFields guards that the record carries both depths
// distinctly — what fleet visibility (B3) needs to tell "AV1 8-bit but not
// 10-bit" apart from "no AV1 at all".
func TestEncoderCapabilityBitDepthFields(t *testing.T) {
	c := hardware.HardwareEncoderCapability{Verified: true, Verified8Bit: true, Verified10Bit: false}
	if !c.Verified8Bit || c.Verified10Bit {
		t.Fatalf("expected 8-bit-only capability, got 8bit=%v 10bit=%v", c.Verified8Bit, c.Verified10Bit)
	}
}
