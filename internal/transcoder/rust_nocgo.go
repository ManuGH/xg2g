//go:build !cgo || !transcoder
// +build !cgo !transcoder

package transcoder

import "errors"

// RustAudioRemuxer stub for non-CGO or non-transcoder builds.
type RustAudioRemuxer struct{}

// NewRustAudioRemuxer returns an error when CGO is disabled or transcoder tag is not set.
//
// The Rust audio remuxer requires CGO to be enabled and the 'transcoder' build tag.
// When building without these requirements, this function returns an error.
//
// To enable the transcoder:
//   - Build Rust library: cd transcoder && cargo build --release
//   - Build with tag: CGO_ENABLED=1 go build -tags=transcoder
//
// The proxy will automatically fall back to FFmpeg subprocess transcoding when
// this error is returned.
func NewRustAudioRemuxer(_ int, _ int, _ int) (*RustAudioRemuxer, error) {
	return nil, errors.New("rust audio remuxer not available: build requires CGO_ENABLED=1 and -tags=transcoder")
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
