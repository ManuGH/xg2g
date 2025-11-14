//go:build !cgo
// +build !cgo

package transcoder

import "errors"

// RustAudioRemuxer stub for non-CGO builds.
type RustAudioRemuxer struct{}

// NewRustAudioRemuxer returns an error when CGO is disabled.
//
// The Rust audio remuxer requires CGO to be enabled. When building without CGO
// (CGO_ENABLED=0), this function returns an error indicating that the feature
// is unavailable.
//
// The proxy will automatically fall back to FFmpeg subprocess transcoding when
// this error is returned.
func NewRustAudioRemuxer(_ int, _ int, _ int) (*RustAudioRemuxer, error) {
	return nil, errors.New("rust audio remuxer not available: build requires CGO_ENABLED=1")
}

// Process stub for non-CGO builds.
func (r *RustAudioRemuxer) Process(_ []byte) ([]byte, error) {
	return nil, errors.New("rust audio remuxer not available")
}

// Close stub for non-CGO builds.
func (r *RustAudioRemuxer) Close() error {
	return nil
}

// Config stub for non-CGO builds.
func (r *RustAudioRemuxer) Config() (sampleRate, channels, bitrate int) {
	return 0, 0, 0
}

// Version returns a placeholder version string for non-CGO builds.
func Version() string {
	return "unavailable (CGO disabled)"
}

// LastError returns an empty string for non-CGO builds.
func LastError() string {
	return ""
}
