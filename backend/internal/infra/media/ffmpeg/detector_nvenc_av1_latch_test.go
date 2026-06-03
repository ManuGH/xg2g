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
	// Post-derive state on an NVIDIA host: exit-0 "verified" everything.
	derived := map[string]hardware.NVENCEncoderCapability{
		"h264_nvenc": {Verified: true, AutoEligible: true, ProbeElapsed: 10 * time.Millisecond},
		"av1_nvenc":  {Verified: true, AutoEligible: true, ProbeElapsed: 20 * time.Millisecond},
	}
	nvencEncoders := map[string]bool{"h264_nvenc": true, "av1_nvenc": true}

	t.Cleanup(func() { hardware.SetNVENCEncoderCapabilities(nil) })

	// BUG DEMONSTRATION: pre-latch, av1_nvenc is admitted at the real global gate.
	hardware.SetNVENCEncoderCapabilities(derived)
	hardware.SetNVENCPreflightResult(true)
	if !hardware.IsNVENCEncoderReady("av1_nvenc") {
		t.Fatal("precondition: pre-latch, exit-0 av1_nvenc IS admitted — the false-verified gap this latch closes")
	}

	// APPLY THE LATCH.
	clampUnverifiedNVENCAV1(derived, nvencEncoders)

	// Detector-side bool map (plan_codec gate via NVENCEncoderVerified): dropped.
	if nvencEncoders["av1_nvenc"] {
		t.Error("av1_nvenc must be removed from the detector nvencEncoders map (plan_codec admission path)")
	}
	// Cap: unverifiable, not verified, not auto-eligible; ProbeElapsed kept for the gauge.
	got := derived["av1_nvenc"]
	if got.Verdict != hardware.VerdictUnverifiable {
		t.Errorf("av1_nvenc verdict = %q, want unverifiable", got.Verdict)
	}
	if got.Verified || got.AutoEligible {
		t.Errorf("av1_nvenc must be fail-closed: Verified=%v AutoEligible=%v, want both false", got.Verified, got.AutoEligible)
	}
	if got.ProbeElapsed != 20*time.Millisecond {
		t.Errorf("av1_nvenc ProbeElapsed should be preserved for observability, got %v", got.ProbeElapsed)
	}
	// Surgical: h264_nvenc untouched on both paths (latch is av1-only; NVIDIA host keeps HW H264/HEVC).
	if h := derived["h264_nvenc"]; !h.Verified || !h.AutoEligible {
		t.Error("h264_nvenc must stay verified+auto-eligible (latch is av1-only)")
	}
	if !nvencEncoders["h264_nvenc"] {
		t.Error("h264_nvenc must stay in the detector nvencEncoders map (latch is av1-only)")
	}

	// REAL global admission gate after the latch: av1 closed, h264 open.
	hardware.SetNVENCEncoderCapabilities(derived)
	if hardware.IsNVENCEncoderReady("av1_nvenc") {
		t.Error("post-latch: av1_nvenc must NOT be admitted at the global NVENC gate")
	}
	if !hardware.IsNVENCEncoderReady("h264_nvenc") {
		t.Error("post-latch: h264_nvenc must remain admitted")
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
