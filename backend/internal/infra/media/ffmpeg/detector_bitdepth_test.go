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

// TestApplyEncoderVerdicts_AdmissionIsProductionBitDepth locks the single-source
// invariant B3 depends on: cap.Verified (the field every gate reads and the only
// field B3 may export as "admitted") IS the production-bit-depth verdict —
// derived, never set independently. For AV1 that is Verified10Bit; Verified8Bit
// is recorded but gates nothing. This is what guarantees export-source ==
// gate-source.
func TestApplyEncoderVerdicts_AdmissionIsProductionBitDepth(t *testing.T) {
	mkAV1 := func(v8, v10 bool) hardware.VAAPIEncoderCapability {
		caps := map[string]hardware.VAAPIEncoderCapability{"av1_vaapi": {AutoEligible: true}}
		v := hardware.VerdictVerified
		if !v10 {
			v = hardware.VerdictWithheld
		}
		applyEncoderVerdicts(caps, []string{"av1_vaapi"},
			map[string]hardware.EncoderVerdict{"av1_vaapi": v}, map[string]string{},
			map[string]bool{"av1_vaapi": v8}, map[string]bool{"av1_vaapi": v10})
		return caps["av1_vaapi"]
	}

	if c := mkAV1(true, true); !c.Verified || c.Verified != c.Verified10Bit {
		t.Fatalf("av1 8+10 verified: Verified=%v must equal Verified10Bit=%v (export==gate)", c.Verified, c.Verified10Bit)
	}
	if c := mkAV1(true, false); c.Verified {
		t.Fatal("av1 8-bit-only (10-bit withheld) must NOT be admitted — Verified8Bit gates nothing")
	}
	if c := mkAV1(true, false); !c.Verified8Bit {
		t.Fatal("Verified8Bit must still record the 8-bit result for B3 observability")
	}
	if c := mkAV1(false, true); !c.Verified {
		t.Fatal("av1 10-bit verified must be admitted even if the 8-bit probe was not verified")
	}

	// Non-AV1 admission == 8-bit verdict.
	hevc := map[string]hardware.VAAPIEncoderCapability{"hevc_vaapi": {}}
	applyEncoderVerdicts(hevc, []string{"hevc_vaapi"},
		map[string]hardware.EncoderVerdict{"hevc_vaapi": hardware.VerdictVerified},
		map[string]string{}, map[string]bool{"hevc_vaapi": true}, map[string]bool{})
	if c := hevc["hevc_vaapi"]; !c.Verified || c.Verified != c.Verified8Bit {
		t.Fatalf("hevc admission must equal Verified8Bit: Verified=%v 8bit=%v", c.Verified, c.Verified8Bit)
	}
}
