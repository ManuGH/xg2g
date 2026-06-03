package ffmpeg

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
)

// TestNVENCAV1FailClosed_NotAdmittedWithoutDecodeVerify proves the NVENC AV1
// fail-closed latch closes a FALSE-VERIFIED gap (the mirror image of the VAAPI
// false-withheld this branch's sibling PR fixes): NVENC has no decode-verify —
// testNVENCEncoder discards output to "-f null -" and trusts exit 0 — so
// DeriveHardwareEncoderCapabilities marks av1_nvenc Verified=true on "the encoder
// ran", not "the output decodes". Admitting that can serve corrupt/black AV1 to a
// client with no software AV1 fallback. The latch demotes av1_nvenc to unverifiable
// / not-admitted while leaving h264_nvenc untouched.
//
// The test first DEMONSTRATES the bug — pre-latch, the REAL global admission gate
// (IsNVENCEncoderReady) admits av1_nvenc — then applies the latch and asserts both
// admission paths close. It is its own negative control: make the latch a no-op and
// the post-latch assertions go red; over-reach to h264_nvenc and the surgical-scope
// assertions go red. No NVIDIA hardware required.
func TestNVENCAV1FailClosed_NotAdmittedWithoutDecodeVerify(t *testing.T) {
	// av1_nvenc admission is gated by THREE distinct stores; a latch that closes two
	// of three leaves a silent hole. This test drives all three real gates:
	//   1. global nvencEncCaps    -> hardware.IsNVENCEncoderReady (+ IsHardwareEncoderReady,
	//      the autocodec/intent/B3 funnel) — set from d.nvencEncoderCaps via Set, filtered on Verified.
	//   2. detector d.nvencEncoders -> d.NVENCEncoderVerified (the plan_codec gate).
	//   3. detector d.nvencEncoderCaps -> d.NVENCEncoderAutoEligible.
	// Post-derive state on an NVIDIA host: exit-0 "verified" everything in all three.
	d := &Detector{
		nvencEncoders: map[string]bool{"h264_nvenc": true, "av1_nvenc": true},
		nvencEncoderCaps: map[string]hardware.NVENCEncoderCapability{
			"h264_nvenc": {Verified: true, AutoEligible: true, ProbeElapsed: 10 * time.Millisecond},
			"av1_nvenc":  {Verified: true, AutoEligible: true, ProbeElapsed: 20 * time.Millisecond},
		},
	}
	t.Cleanup(func() { hardware.SetNVENCEncoderCapabilities(nil) })

	// BUG DEMONSTRATION: pre-latch, ALL THREE readers admit av1_nvenc.
	hardware.SetNVENCEncoderCapabilities(d.nvencEncoderCaps)
	hardware.SetNVENCPreflightResult(true)
	if !d.NVENCEncoderVerified("av1_nvenc") || !d.NVENCEncoderAutoEligible("av1_nvenc") || !hardware.IsNVENCEncoderReady("av1_nvenc") {
		t.Fatal("precondition: pre-latch, exit-0 av1_nvenc is admitted by all three readers — the false-verified gap this latch closes")
	}

	// APPLY THE LATCH, then mirror to the global store exactly as PreflightNVENC does.
	clampUnverifiedNVENCAV1(d.nvencEncoderCaps, d.nvencEncoders)
	hardware.SetNVENCEncoderCapabilities(d.nvencEncoderCaps)

	// ALL THREE readers must now refuse av1_nvenc — completeness, no silent third path.
	if d.NVENCEncoderVerified("av1_nvenc") {
		t.Error("store 2 (nvencEncoders / plan_codec gate): av1_nvenc still admitted")
	}
	if d.NVENCEncoderAutoEligible("av1_nvenc") {
		t.Error("store 3 (nvencEncoderCaps / NVENCEncoderAutoEligible): av1_nvenc still auto-eligible")
	}
	if hardware.IsNVENCEncoderReady("av1_nvenc") {
		t.Error("store 1 (global nvencEncCaps / IsNVENCEncoderReady+IsHardwareEncoderReady): av1_nvenc still admitted")
	}
	// Explicit three-state label retained for the gauge; ProbeElapsed preserved.
	if got := d.nvencEncoderCaps["av1_nvenc"]; got.Verdict != hardware.VerdictUnverifiable || got.Verified || got.AutoEligible {
		t.Errorf("av1_nvenc cap = %+v, want {Verdict:unverifiable, Verified:false, AutoEligible:false}", got)
	}
	if got := d.nvencEncoderCaps["av1_nvenc"].ProbeElapsed; got != 20*time.Millisecond {
		t.Errorf("av1_nvenc ProbeElapsed must be preserved for observability, got %v", got)
	}

	// SURGICAL: h264_nvenc untouched on every one of the three readers (latch is av1-only).
	if !d.NVENCEncoderVerified("h264_nvenc") || !d.NVENCEncoderAutoEligible("h264_nvenc") || !hardware.IsNVENCEncoderReady("h264_nvenc") {
		t.Error("h264_nvenc must remain admitted on all three readers — an NVIDIA host keeps HW H264/HEVC")
	}
}

// TestClampUnverifiedNVENCAV1_NoopWhenAbsent guards the latch on hosts/builds with
// no av1_nvenc at all: it must be a no-op, not a panic or a spurious entry.
func TestClampUnverifiedNVENCAV1_NoopWhenAbsent(t *testing.T) {
	caps := map[string]hardware.NVENCEncoderCapability{
		"h264_nvenc": {Verified: true, AutoEligible: true},
	}
	nvencEncoders := map[string]bool{"h264_nvenc": true}
	clampUnverifiedNVENCAV1(caps, nvencEncoders)
	if _, present := caps["av1_nvenc"]; present {
		t.Error("clamp must not invent an av1_nvenc entry when none was probed")
	}
	if !caps["h264_nvenc"].Verified || !nvencEncoders["h264_nvenc"] {
		t.Error("clamp must leave h264_nvenc untouched when av1_nvenc is absent")
	}
}
