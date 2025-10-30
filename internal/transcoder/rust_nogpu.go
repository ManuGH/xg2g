//go:build !gpu
// +build !gpu

// Package transcoder provides stub implementations when GPU/FFI is disabled.
package transcoder

import "errors"

// RustAudioRemuxer is a stub when built without GPU support.
type RustAudioRemuxer struct{}

// NewRustAudioRemuxer returns an error when built without GPU support.
func NewRustAudioRemuxer(sampleRate, channels, bitrate int) (*RustAudioRemuxer, error) {
	return nil, errors.New("Rust audio remuxer not available: build with -tags=gpu")
}

// Process returns an error (stub implementation).
func (r *RustAudioRemuxer) Process(input []byte) ([]byte, error) {
	return nil, errors.New("Rust audio remuxer not available: build with -tags=gpu")
}

// Close is a no-op stub.
func (r *RustAudioRemuxer) Close() error {
	return nil
}

// Config returns zero values (stub implementation).
func (r *RustAudioRemuxer) Config() (sampleRate, channels, bitrate int) {
	return 0, 0, 0
}

// Version returns a stub version string.
func Version() string {
	return "nogpu-stub"
}

// LastError returns empty string (stub implementation).
func LastError() string {
	return ""
}
