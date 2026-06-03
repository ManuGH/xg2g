package hardware

import "testing"

// TestEightBitOnlyAV1PreservedButNotAdmitted locks the behavior of the
// SetVAAPIEncoderCapabilities filter (keep records that are Verified OR
// Verified8Bit OR Verified10Bit): an AV1 record verified only at 8-bit must be
// PRESERVED in the registry so fleet visibility (B3) can report "AV1 8-bit but
// not 10-bit", yet must NOT be admitted — admission gates on cap.Verified, which
// is false for an 8-bit-only AV1 (production drives AV1 at 10-bit).
func TestEightBitOnlyAV1PreservedButNotAdmitted(t *testing.T) {
	SetVAAPIEncoderCapabilities(map[string]HardwareEncoderCapability{
		"av1_vaapi":  {Verified: false, Verified8Bit: true, Verified10Bit: false},
		"h264_vaapi": {Verified: true, Verified8Bit: true, AutoEligible: true},
	})
	SetVAAPIPreflightResult(true)
	t.Cleanup(func() { SetVAAPIEncoderCapabilities(nil) })

	// Preserved for B3 observability (direct registry read, package-internal).
	if got, ok := vaapiEncCaps["av1_vaapi"]; !ok || !got.Verified8Bit || got.Verified10Bit {
		t.Fatalf("8-bit-only av1 record must be preserved, got %+v ok=%v", got, ok)
	}
	// But NOT admitted — admission gates on cap.Verified.
	if IsHardwareEncoderReady("av1") {
		t.Fatal("8-bit-only av1 (Verified=false) must NOT be hardware-ready")
	}
	if !IsHardwareEncoderReady("h264") {
		t.Fatal("h264 (Verified=true) should be hardware-ready")
	}
}
