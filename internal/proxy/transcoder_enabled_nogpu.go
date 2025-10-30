//go:build !gpu
// +build !gpu

package proxy

// defaultTranscodingEnabled returns false for nogpu builds since neither
// Rust FFI nor FFmpeg are available in CI/test environments.
// Users can explicitly enable it with XG2G_ENABLE_AUDIO_TRANSCODING=true if they have FFmpeg.
func defaultTranscodingEnabled() bool {
	return false
}
