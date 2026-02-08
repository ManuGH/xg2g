package hardware

import "testing"

func TestIsVAAPIReady_DefaultFalse(t *testing.T) {
	// Reset global state for test isolation
	vaapiMu.Lock()
	vaapiChecked = false
	vaapiPassed = false
	vaapiMu.Unlock()

	if IsVAAPIReady() {
		t.Fatal("IsVAAPIReady must be false before preflight runs (fail-closed)")
	}
}

func TestIsVAAPIReady_AfterPassedPreflight(t *testing.T) {
	vaapiMu.Lock()
	vaapiChecked = false
	vaapiPassed = false
	vaapiMu.Unlock()

	SetVAAPIPreflightResult(true)

	if !IsVAAPIReady() {
		t.Fatal("IsVAAPIReady must be true after preflight passed")
	}

	// cleanup
	vaapiMu.Lock()
	vaapiChecked = false
	vaapiPassed = false
	vaapiMu.Unlock()
}

func TestIsVAAPIReady_AfterFailedPreflight(t *testing.T) {
	vaapiMu.Lock()
	vaapiChecked = false
	vaapiPassed = false
	vaapiMu.Unlock()

	SetVAAPIPreflightResult(false)

	if IsVAAPIReady() {
		t.Fatal("IsVAAPIReady must be false after preflight failed")
	}

	// cleanup
	vaapiMu.Lock()
	vaapiChecked = false
	vaapiPassed = false
	vaapiMu.Unlock()
}
