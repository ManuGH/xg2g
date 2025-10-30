//go:build gpu
// +build gpu

package proxy

// defaultTranscodingEnabled returns true for gpu builds for iOS Safari compatibility.
// Rust FFI is available and provides high-performance transcoding.
func defaultTranscodingEnabled() bool {
	return true
}
