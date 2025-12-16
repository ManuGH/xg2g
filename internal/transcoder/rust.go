//go:build cgo && transcoder

// Package transcoder provides Go bindings for the Rust audio remuxer via CGO/FFI.
//
// This package wraps the native Rust audio remuxer, allowing Go code to perform
// low-latency MP2/AC3 → AAC audio remuxing without spawning external ffmpeg processes.
//
// # Build Requirements
//
// CGO must be enabled, the Rust library must be built, and the 'transcoder' build tag must be specified:
//
//	cd transcoder && cargo build --release
//	CGO_ENABLED=1 go build -tags=transcoder
//	CGO_ENABLED=1 go test -tags=transcoder ./...
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
// #cgo linux LDFLAGS: -ldl -lm -lpthread -lavcodec -lavformat -lavutil -lswresample -lswscale
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
// The input must be valid MPEG-TS data. The function writes the remuxed MPEG-TS
// stream with AAC audio into the provided output buffer.
//
// Returns the number of bytes written to the output buffer.
//
// This function is safe for concurrent use by multiple goroutines on the same
// RustAudioRemuxer instance (the Rust implementation handles synchronization),
// provided that input and output buffers are not shared or are sychronized by the caller.
//
// Performance: This function performs NO heap allocations if the output buffer is provided.
func (r *RustAudioRemuxer) Process(input []byte, output []byte) (int, error) {
	if r.isClosed {
		return 0, ErrTranscoderUnavailable
	}

	if len(input) == 0 {
		return 0, ErrInvalidInput
	}

	if len(output) == 0 {
		return 0, ErrOutputTooSmall
	}

	// Check for overlap
	// Basic check: start addresses. A full range check would be &input[0] < &output[len] && &output[0] < &input[len]
	// But simply checking if they share the same backing array at the start is a good first step,
	// or we can be stricter.
	// Since Go GC moves pointers, using unsafe.Pointer for comparison is tricky but safe within a function if pinned?
	// Actually, just checking if we are writing to input:
	// If the user passes the same slice, &input[0] == &output[0].
	if len(input) > 0 && len(output) > 0 && &input[0] == &output[0] {
		return 0, fmt.Errorf("%w: input and output buffers must not overlap", ErrInvalidInput)
	}

	// Lock OS thread to ensure thread-local error storage works correctly across CGO calls
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Keep input and output alive during C call
	runtime.KeepAlive(input)
	runtime.KeepAlive(output)

	// Call Rust FFI function
	written := C.xg2g_audio_remux_process(
		r.handle,
		(*C.uint8_t)(unsafe.Pointer(&input[0])),
		C.size_t(len(input)),
		(*C.uint8_t)(unsafe.Pointer(&output[0])),
		C.size_t(len(output)),
	)

	// Success
	if written >= 0 {
		return int(written), nil
	}

	// Handle errors
	// -2 indicates buffer too small (see ffi.rs)
	if written == -2 {
		return 0, ErrOutputTooSmall
	}

	// Other errors
	lastErr := lastError()
	if lastErr != "" {
		return 0, fmt.Errorf("remuxing failed: %s", lastErr)
	}
	return 0, fmt.Errorf("remuxing failed (error code: %d)", written)
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
		_ = r.Close() // Ignore error in finalizer - nothing we can do here
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

// lastError returns the last error message from the Rust library, if any.
//
// Returns an empty string if there is no error.
//
// Note: This function must be called from the same OS thread that generated the error.
// The caller is responsible for ensuring thread safety (e.g., using runtime.LockOSThread).
func lastError() string {
	cError := C.xg2g_last_error()
	if cError == nil {
		return ""
	}
	defer C.xg2g_free_string(cError)
	return C.GoString(cError)
}
