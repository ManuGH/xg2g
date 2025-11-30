//go:build !cgo || !transcoder

package transcoder

import (
	"strings"
	"testing"
)

// TestNewRustAudioRemuxer_Stub verifies that the stub returns an appropriate error
func TestNewRustAudioRemuxer_Stub(t *testing.T) {
	remuxer, err := NewRustAudioRemuxer(48000, 2, 192000)
	if err == nil {
		t.Fatal("expected error from stub NewRustAudioRemuxer, got nil")
	}

	if remuxer != nil {
		t.Errorf("expected nil remuxer from stub, got %v", remuxer)
	}

	// Error should mention CGO and transcoder tag
	errMsg := err.Error()
	if !strings.Contains(errMsg, "CGO_ENABLED=1") {
		t.Errorf("error should mention CGO_ENABLED=1, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "-tags=transcoder") {
		t.Errorf("error should mention -tags=transcoder, got: %s", errMsg)
	}
}

// TestRustAudioRemuxer_Process_Stub verifies Process returns error
func TestRustAudioRemuxer_Process_Stub(t *testing.T) {
	remuxer := &RustAudioRemuxer{}

	output, err := remuxer.Process([]byte{0x47, 0x00})
	if err == nil {
		t.Fatal("expected error from stub Process, got nil")
	}

	if output != nil {
		t.Errorf("expected nil output from stub, got %v", output)
	}

	if !strings.Contains(err.Error(), "not available") {
		t.Errorf("error should mention 'not available', got: %s", err.Error())
	}
}

// TestRustAudioRemuxer_Close_Stub verifies Close doesn't panic
func TestRustAudioRemuxer_Close_Stub(t *testing.T) {
	remuxer := &RustAudioRemuxer{}

	err := remuxer.Close()
	if err != nil {
		t.Errorf("stub Close returned unexpected error: %v", err)
	}

	// Multiple closes should also be safe
	err = remuxer.Close()
	if err != nil {
		t.Errorf("stub Close (second call) returned unexpected error: %v", err)
	}
}

// TestRustAudioRemuxer_Config_Stub verifies Config returns zeros
func TestRustAudioRemuxer_Config_Stub(t *testing.T) {
	remuxer := &RustAudioRemuxer{}

	sampleRate, channels, bitrate := remuxer.Config()

	if sampleRate != 0 {
		t.Errorf("expected sampleRate 0 from stub, got %d", sampleRate)
	}
	if channels != 0 {
		t.Errorf("expected channels 0 from stub, got %d", channels)
	}
	if bitrate != 0 {
		t.Errorf("expected bitrate 0 from stub, got %d", bitrate)
	}
}

// TestVersion_Stub verifies Version returns unavailable message
func TestVersion_Stub(t *testing.T) {
	version := Version()

	if version == "" {
		t.Error("Version() returned empty string")
	}

	if !strings.Contains(version, "unavailable") {
		t.Errorf("expected version to contain 'unavailable', got: %s", version)
	}

	expectedVersion := "unavailable (CGO disabled)"
	if version != expectedVersion {
		t.Errorf("expected version '%s', got '%s'", expectedVersion, version)
	}
}

// TestLastError_Stub verifies LastError returns empty string
func TestLastError_Stub(t *testing.T) {
	lastErr := LastError()

	if lastErr != "" {
		t.Errorf("expected empty string from stub LastError, got: %s", lastErr)
	}
}

// TestStubBehaviorConsistency ensures stub behavior is consistent
func TestStubBehaviorConsistency(t *testing.T) {
	// Multiple calls should return consistent errors
	_, err1 := NewRustAudioRemuxer(48000, 2, 192000)
	_, err2 := NewRustAudioRemuxer(44100, 1, 128000)

	if err1 == nil || err2 == nil {
		t.Fatal("expected errors from stub, got nil")
	}

	// Both errors should be the same
	if err1.Error() != err2.Error() {
		t.Errorf("stub errors should be consistent:\n  err1: %s\n  err2: %s", err1, err2)
	}
}
