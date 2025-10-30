//go:build gpu
// +build gpu

package proxy

// defaultUseRustRemuxer returns true for gpu builds to use high-performance Rust remuxer.
func defaultUseRustRemuxer() bool {
	return true
}
