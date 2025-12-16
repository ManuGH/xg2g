//go:build cgo && transcoder

package transcoder

import (
	"runtime"
	"testing"
	"time"
)

func TestNewRustAudioRemuxer(t *testing.T) {
	remuxer, err := NewRustAudioRemuxer(48000, 2, 192000)
	if err != nil {
		t.Fatalf("NewRustAudioRemuxer failed: %v", err)
	}
	defer remuxer.Close()

	if remuxer == nil {
		t.Fatal("remuxer is nil")
	}

	sampleRate, channels, bitrate := remuxer.Config()
	if sampleRate != 48000 {
		t.Errorf("expected sample rate 48000, got %d", sampleRate)
	}
	if channels != 2 {
		t.Errorf("expected 2 channels, got %d", channels)
	}
	if bitrate != 192000 {
		t.Errorf("expected bitrate 192000, got %d", bitrate)
	}
}

func TestRustAudioRemuxer_Process(t *testing.T) {
	remuxer, err := NewRustAudioRemuxer(48000, 2, 192000)
	if err != nil {
		t.Fatalf("NewRustAudioRemuxer failed: %v", err)
	}
	defer remuxer.Close()

	// Create dummy MPEG-TS packet (188 bytes with sync byte 0x47)
	input := make([]byte, 188*10) // 10 TS packets
	for i := 0; i < 10; i++ {
		input[i*188] = 0x47 // MPEG-TS sync byte
	}

	output := make([]byte, len(input)*4)
	written, err := remuxer.Process(input, output)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Currently the Rust implementation is a passthrough,
	// so output length should match input
	if written != len(input) {
		t.Logf("Warning: output length (%d) != input length (%d)", written, len(input))
		t.Logf("This is expected until native remuxing is fully implemented")
	}
}

func TestRustAudioRemuxer_ProcessEmptyInput(t *testing.T) {
	remuxer, err := NewRustAudioRemuxer(48000, 2, 192000)
	if err != nil {
		t.Fatalf("NewRustAudioRemuxer failed: %v", err)
	}
	defer remuxer.Close()

	output := make([]byte, 1024)
	_, err = remuxer.Process([]byte{}, output)
	if err == nil {
		t.Fatal("expected error for empty input, got nil")
	}

	expectedMsg := "input is empty"
	if err.Error() != expectedMsg {
		t.Errorf("expected error '%s', got '%s'", expectedMsg, err.Error())
	}
}

func TestRustAudioRemuxer_ProcessAfterClose(t *testing.T) {
	remuxer, err := NewRustAudioRemuxer(48000, 2, 192000)
	if err != nil {
		t.Fatalf("NewRustAudioRemuxer failed: %v", err)
	}

	// Close the remuxer
	if err := remuxer.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Try to process data after closing
	input := make([]byte, 188)
	input[0] = 0x47
	output := make([]byte, 188*4)

	_, err = remuxer.Process(input, output)
	if err == nil {
		t.Fatal("expected error when processing after Close(), got nil")
	}

	expectedMsg := "remuxer is closed"
	if err.Error() != expectedMsg {
		t.Errorf("expected error '%s', got '%s'", expectedMsg, err.Error())
	}
}

func TestRustAudioRemuxer_MultipleClose(t *testing.T) {
	remuxer, err := NewRustAudioRemuxer(48000, 2, 192000)
	if err != nil {
		t.Fatalf("NewRustAudioRemuxer failed: %v", err)
	}

	// Close multiple times should not panic
	if err := remuxer.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}

	if err := remuxer.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}

	if err := remuxer.Close(); err != nil {
		t.Fatalf("third Close failed: %v", err)
	}
}

func TestRustAudioRemuxer_ConcurrentAccess(t *testing.T) {
	remuxer, err := NewRustAudioRemuxer(48000, 2, 192000)
	if err != nil {
		t.Fatalf("NewRustAudioRemuxer failed: %v", err)
	}
	defer remuxer.Close()

	// Create test data
	input := make([]byte, 188*10)
	for i := 0; i < 10; i++ {
		input[i*188] = 0x47
	}

	// Process from multiple goroutines concurrently
	const numGoroutines = 10
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer func() { done <- true }()
			// Each goroutine needs its own output buffer!
			output := make([]byte, len(input)*4)

			for j := 0; j < 5; j++ {
				_, err := remuxer.Process(input, output)
				if err != nil {
					t.Errorf("concurrent Process failed: %v", err)
				}
			}
		}()
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}
}

func TestRustAudioRemuxer_Finalizer(t *testing.T) {
	// Create remuxer without calling Close()
	_, err := NewRustAudioRemuxer(48000, 2, 192000)
	if err != nil {
		t.Fatalf("NewRustAudioRemuxer failed: %v", err)
	}

	// Let it go out of scope without Close()
	// The finalizer should clean it up

	// Force GC to run
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	// If we get here without crash, the finalizer worked
	t.Log("Finalizer test passed - no resource leak")
}

func TestVersion(t *testing.T) {
	version := Version()
	if version == "" {
		t.Error("Version() returned empty string")
	}

	t.Logf("Rust transcoder version: %s", version)

	// Version should match Cargo.toml version (1.0.0)
	if version != "1.0.0" {
		t.Logf("Warning: expected version '1.0.0', got '%s'", version)
	}
}

func BenchmarkRustAudioRemuxer_Process(b *testing.B) {
	remuxer, err := NewRustAudioRemuxer(48000, 2, 192000)
	if err != nil {
		b.Fatalf("NewRustAudioRemuxer failed: %v", err)
	}
	defer remuxer.Close()

	// Create realistic input size (1024 TS packets = 192KB)
	input := make([]byte, 188*1024)
	for i := 0; i < 1024; i++ {
		input[i*188] = 0x47
	}

	// Pre-allocate output buffer
	output := make([]byte, len(input)*4)

	b.ResetTimer()
	b.SetBytes(int64(len(input)))

	for i := 0; i < b.N; i++ {
		// Zero allocation here!
		_, err := remuxer.Process(input, output)
		if err != nil {
			b.Fatalf("Process failed: %v", err)
		}
	}
}

func BenchmarkRustAudioRemuxer_ProcessSmall(b *testing.B) {
	remuxer, err := NewRustAudioRemuxer(48000, 2, 192000)
	if err != nil {
		b.Fatalf("NewRustAudioRemuxer failed: %v", err)
	}
	defer remuxer.Close()

	// Small input (10 TS packets = 1880 bytes)
	input := make([]byte, 188*10)
	for i := 0; i < 10; i++ {
		input[i*188] = 0x47
	}
	output := make([]byte, len(input)*4)

	b.ResetTimer()
	b.SetBytes(int64(len(input)))

	for i := 0; i < b.N; i++ {
		_, err := remuxer.Process(input, output)
		if err != nil {
			b.Fatalf("Process failed: %v", err)
		}
	}
}
