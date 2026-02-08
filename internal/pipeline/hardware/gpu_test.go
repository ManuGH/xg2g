package hardware

import "testing"

func resetVaapiState(t *testing.T) {
	t.Helper()
	vaapiMu.Lock()
	vaapiChecked = false
	vaapiPassed = false
	vaapiMu.Unlock()

	t.Cleanup(func() {
		vaapiMu.Lock()
		vaapiChecked = false
		vaapiPassed = false
		vaapiMu.Unlock()
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
