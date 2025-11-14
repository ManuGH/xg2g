//go:build cgo
// +build cgo

// Package transcoder provides Go bindings for the Rust audio remuxer via CGO/FFI.
//
// This package wraps the native Rust audio remuxer, allowing Go code to perform
// low-latency MP2/AC3 → AAC audio remuxing without spawning external ffmpeg processes.
//
// # Build Requirements
//
// CGO must be enabled and the Rust library must be built first:
//
//	cd transcoder && cargo build --release
//	CGO_ENABLED=1 go build
//
// # Usage Example
//
//	remuxer, err := transcoder.NewRustAudioRemuxer(48000, 2, 192000)
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer remuxer.Close()
//
//	output, err := remuxer.Process(inputTSData)
//	if err != nil {
//		log.Fatal(err)
//	}
package transcoder

// #cgo LDFLAGS: -L${SRCDIR}/../../transcoder/target/release -lxg2g_transcoder
// #cgo darwin LDFLAGS: -framework CoreFoundation -framework Security
// #cgo linux LDFLAGS: -ldl -lm -lpthread
// #include <stdlib.h>
// #include "rust_bindings.h"
import "C"
import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"
)

// RustAudioRemuxer wraps the Rust audio remuxer for safe Go usage.
//
// This struct manages the lifecycle of a Rust AudioRemuxer instance,
// providing memory-safe access to native audio remuxing functionality.
type RustAudioRemuxer struct {
	handle     C.xg2g_remuxer_handle
	sampleRate int
	channels   int
	bitrate    int
	isClosed   bool
}

// NewRustAudioRemuxer creates a new audio remuxer instance.
//
// Parameters:
//   - sampleRate: Audio sample rate in Hz (e.g., 48000)
//   - channels: Number of audio channels (e.g., 2 for stereo)
//   - bitrate: Target AAC bitrate in bits per second (e.g., 192000 for 192kbps)
//
// Returns an error if initialization fails.
//
// The caller must call Close() when done to prevent resource leaks.
func NewRustAudioRemuxer(sampleRate, channels, bitrate int) (*RustAudioRemuxer, error) {
	handle := C.xg2g_audio_remux_init(
		C.int(sampleRate),
		C.int(channels),
		C.int(bitrate),
	)

	if handle == nil {
		return nil, errors.New("failed to initialize Rust audio remuxer")
	}

	remuxer := &RustAudioRemuxer{
		handle:     handle,
		sampleRate: sampleRate,
		channels:   channels,
		bitrate:    bitrate,
		isClosed:   false,
	}

	// Set up finalizer to ensure cleanup even if Close() is not called
	runtime.SetFinalizer(remuxer, (*RustAudioRemuxer).finalize)

	return remuxer, nil
}

// Process remuxes MPEG-TS data containing MP2/AC3 audio to AAC.
//
// The input must be valid MPEG-TS data. The function returns a new byte slice
// containing the remuxed MPEG-TS stream with AAC audio.
//
// This function is safe for concurrent use by multiple goroutines on the same
// RustAudioRemuxer instance (the Rust implementation handles synchronization).
func (r *RustAudioRemuxer) Process(input []byte) ([]byte, error) {
	if r.isClosed {
		return nil, errors.New("remuxer is closed")
	}

	if len(input) == 0 {
		return nil, errors.New("input is empty")
	}

	// Allocate output buffer (20x input size for transcoding expansion)
	// AC3→AAC transcoding can expand significantly (e.g., 16 packets → 170 packets)
	outputCapacity := len(input) * 20
	output := make([]byte, outputCapacity)

	// Keep input alive during C call
	defer runtime.KeepAlive(input)

	// Call Rust FFI function
	written := C.xg2g_audio_remux_process(
		r.handle,
		(*C.uint8_t)(unsafe.Pointer(&input[0])),
		C.size_t(len(input)),
		(*C.uint8_t)(unsafe.Pointer(&output[0])),
		C.size_t(outputCapacity),
	)

	if written < 0 {
		return nil, fmt.Errorf("remuxing failed (error code: %d)", written)
	}

	// Return slice of actual written bytes
	return output[:int(written)], nil
}

// Close releases the Rust remuxer resources.
//
// After calling Close(), the remuxer cannot be used anymore.
// It is safe to call Close() multiple times.
func (r *RustAudioRemuxer) Close() error {
	if r.isClosed {
		return nil
	}

	if r.handle != nil {
		C.xg2g_audio_remux_free(r.handle)
		r.handle = nil
	}

	r.isClosed = true

	// Remove finalizer since we've cleaned up manually
	runtime.SetFinalizer(r, nil)

	return nil
}

// finalize is called by the garbage collector if Close() was not called.
// This provides a safety net to prevent resource leaks.
func (r *RustAudioRemuxer) finalize() {
	if !r.isClosed {
		r.Close()
	}
}

// Config returns the remuxer configuration.
func (r *RustAudioRemuxer) Config() (sampleRate, channels, bitrate int) {
	return r.sampleRate, r.channels, r.bitrate
}

// Version returns the Rust transcoder library version.
func Version() string {
	cVersion := C.xg2g_transcoder_version()
	return C.GoString(cVersion)
}

// LastError returns the last error message from the Rust library, if any.
//
// Returns an empty string if there is no error.
func LastError() string {
	cError := C.xg2g_last_error()
	if cError == nil {
		return ""
	}
	defer C.xg2g_free_string(cError)
	return C.GoString(cError)
}
