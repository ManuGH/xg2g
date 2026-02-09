package hardware

import "testing"

func resetVaapiState(t *testing.T) {
	t.Helper()
	vaapiMu.Lock()
	vaapiChecked = false
	vaapiPassed = false
	vaapiMu.Unlock()

	vaapiEncMu.Lock()
	vaapiEncChecked = false
	vaapiEncVerified = nil
	vaapiEncMu.Unlock()

	t.Cleanup(func() {
		vaapiMu.Lock()
		vaapiChecked = false
		vaapiPassed = false
		vaapiMu.Unlock()

		vaapiEncMu.Lock()
		vaapiEncChecked = false
		vaapiEncVerified = nil
		vaapiEncMu.Unlock()
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
