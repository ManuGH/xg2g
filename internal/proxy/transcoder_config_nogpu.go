//go:build !gpu
// +build !gpu

package proxy

// defaultUseRustRemuxer returns false for nogpu builds since Rust FFI is not available.
func defaultUseRustRemuxer() bool {
	return false
}
