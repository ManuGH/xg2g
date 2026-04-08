package hardware

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
)

func resetVaapiState(t *testing.T) {
	t.Helper()
	vaapiMu.Lock()
	vaapiChecked = false
	vaapiPassed = false
	vaapiRuntimeFailures = 0
	vaapiMu.Unlock()

	vaapiEncMu.Lock()
	vaapiEncChecked = false
	vaapiEncCaps = nil
	vaapiEncMu.Unlock()

	nvencMu.Lock()
	nvencChecked = false
	nvencPassed = false
	nvencRuntimeFailures = 0
	nvencMu.Unlock()

	nvencEncMu.Lock()
	nvencEncChecked = false
	nvencEncCaps = nil
	nvencEncMu.Unlock()

	t.Cleanup(func() {
		vaapiMu.Lock()
		vaapiChecked = false
		vaapiPassed = false
		vaapiRuntimeFailures = 0
		vaapiMu.Unlock()

		vaapiEncMu.Lock()
		vaapiEncChecked = false
		vaapiEncCaps = nil
		vaapiEncMu.Unlock()

		nvencMu.Lock()
		nvencChecked = false
		nvencPassed = false
		nvencRuntimeFailures = 0
		nvencMu.Unlock()

		nvencEncMu.Lock()
		nvencEncChecked = false
		nvencEncCaps = nil
		nvencEncMu.Unlock()
	})
}

func TestIsVAAPIReady_DefaultFalse(t *testing.T) {
	resetVaapiState(t)

	if IsVAAPIReady() {
		t.Fatal("IsVAAPIReady must be false before preflight runs (fail-closed)")
	}
}

func TestIsVAAPIReady_AfterPassedPreflight(t *testing.T) {
	resetVaapiState(t)

	SetVAAPIPreflightResult(true)

	if !IsVAAPIReady() {
		t.Fatal("IsVAAPIReady must be true after preflight passed")
	}
}

func TestIsVAAPIReady_AfterFailedPreflight(t *testing.T) {
	resetVaapiState(t)

	SetVAAPIPreflightResult(false)

	if IsVAAPIReady() {
		t.Fatal("IsVAAPIReady must be false after preflight failed")
	}
}

func TestIsVAAPIEncoderReady_FailClosed(t *testing.T) {
	resetVaapiState(t)

	if IsVAAPIEncoderReady("av1_vaapi") {
		t.Fatal("IsVAAPIEncoderReady must be false before encoder preflight runs (fail-closed)")
	}
}

func TestIsVAAPIEncoderReady_AfterSet(t *testing.T) {
	resetVaapiState(t)

	SetVAAPIEncoderPreflight(map[string]bool{
		"h264_vaapi": true,
		"hevc_vaapi": false,
		"av1_vaapi":  true,
	})

	if !IsVAAPIEncoderReady("h264_vaapi") {
		t.Fatal("expected h264_vaapi to be ready")
	}
	if IsVAAPIEncoderReady("hevc_vaapi") {
		t.Fatal("expected hevc_vaapi to be not ready")
	}
	if !IsVAAPIEncoderReady("av1_vaapi") {
		t.Fatal("expected av1_vaapi to be ready")
	}
}

func TestIsVAAPIEncoderAutoEligible_UsesMeasuredCapability(t *testing.T) {
	resetVaapiState(t)

	SetVAAPIEncoderCapabilities(map[string]VAAPIEncoderCapability{
		"h264_vaapi": {
			Verified:     true,
			ProbeElapsed: 120,
			AutoEligible: true,
		},
		"hevc_vaapi": {
			Verified:     true,
			ProbeElapsed: 250,
			AutoEligible: false,
		},
	})

	if !IsVAAPIEncoderAutoEligible("h264_vaapi") {
		t.Fatal("expected h264_vaapi to be auto-eligible")
	}
	if IsVAAPIEncoderAutoEligible("hevc_vaapi") {
		t.Fatal("expected hevc_vaapi to be not auto-eligible")
	}
	cap, ok := VAAPIEncoderCapabilityFor("h264_vaapi")
	if !ok {
		t.Fatal("expected h264_vaapi capability to be available")
	}
	if !cap.Verified || !cap.AutoEligible || cap.ProbeElapsed != 120 {
		t.Fatalf("unexpected capability snapshot: %#v", cap)
	}
}

func TestRecordVAAPIRuntimeFailure_DemotesAfterThreshold(t *testing.T) {
	resetVaapiState(t)

	SetVAAPIPreflightResult(true)
	SetVAAPIEncoderPreflight(map[string]bool{
		"h264_vaapi": true,
	})

	for i := 1; i < vaapiRuntimeFailureThreshold; i++ {
		failures, demoted := RecordVAAPIRuntimeFailure()
		if failures != i {
			t.Fatalf("expected failures=%d, got %d", i, failures)
		}
		if demoted {
			t.Fatalf("unexpected demotion at failures=%d", failures)
		}
		if !IsVAAPIReady() {
			t.Fatalf("gpu must stay ready before threshold, failures=%d", failures)
		}
	}

	failures, demoted := RecordVAAPIRuntimeFailure()
	if failures != vaapiRuntimeFailureThreshold {
		t.Fatalf("expected failures=%d, got %d", vaapiRuntimeFailureThreshold, failures)
	}
	if !demoted {
		t.Fatal("expected VAAPI demotion at threshold")
	}
	if IsVAAPIReady() {
		t.Fatal("VAAPI must be not-ready after runtime demotion")
	}
	if IsVAAPIEncoderReady("h264_vaapi") {
		t.Fatal("encoder readiness must be cleared after runtime demotion")
	}
}

func TestRuntimeVAAPIDemotion_FallbacksToCPUProfile(t *testing.T) {
	resetVaapiState(t)

	SetVAAPIPreflightResult(true)
	SetVAAPIEncoderPreflight(map[string]bool{
		"h264_vaapi": true,
	})

	capInterlaced := &scan.Capability{Interlaced: true}
	specGPU := profiles.Resolve("safari", "Safari/17.0", 0, capInterlaced, profiles.GPUBackendVAAPI, profiles.HWAccelAuto)
	if specGPU.HWAccel != "vaapi" {
		t.Fatalf("expected GPU profile before runtime failure, got hwaccel=%q", specGPU.HWAccel)
	}

	for i := 0; i < vaapiRuntimeFailureThreshold; i++ {
		_, _ = RecordVAAPIRuntimeFailure()
	}

	specCPU := profiles.Resolve("safari", "Safari/17.0", 0, capInterlaced, profiles.GPUBackendNone, profiles.HWAccelAuto)
	if specCPU.HWAccel != "" {
		t.Fatalf("expected CPU fallback profile after runtime demotion, got hwaccel=%q", specCPU.HWAccel)
	}
	if specCPU.VideoCodec != "libx264" {
		t.Fatalf("expected CPU fallback codec libx264, got %q", specCPU.VideoCodec)
	}
}

func TestPreferredGPUBackendForCodec_PrefersFasterVerifiedBackend(t *testing.T) {
	resetVaapiState(t)

	SetVAAPIPreflightResult(true)
	SetVAAPIEncoderCapabilities(map[string]VAAPIEncoderCapability{
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 120},
	})
	SetNVENCPreflightResult(true)
	SetNVENCEncoderCapabilities(map[string]NVENCEncoderCapability{
		"h264_nvenc": {Verified: true, AutoEligible: true, ProbeElapsed: 80},
	})

	if got := PreferredGPUBackendForCodec("h264"); got != profiles.GPUBackendNVENC {
		t.Fatalf("expected NVENC to win for h264, got %q", got)
	}
	if got := PreferredGPUBackend(); got != profiles.GPUBackendNVENC {
		t.Fatalf("expected PreferredGPUBackend to follow the fastest common codec, got %q", got)
	}
}
