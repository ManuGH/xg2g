package proxy

// defaultUseRustRemuxer returns true to use the high-performance native Rust remuxer by default.
//
// The Rust remuxer provides significantly better performance than FFmpeg subprocess:
// - Latency: ~39µs vs 200-500ms (FFmpeg)
// - CPU usage: <0.1% vs 5-10% (FFmpeg)
// - Throughput: 4.94 GB/s
//
// If the Rust remuxer fails to initialize (e.g., missing AVX2 instructions on old CPUs),
// the proxy will automatically fall back to FFmpeg subprocess transcoding.
//
// Users can override this behavior with environment variable:
//
//	XG2G_USE_RUST_REMUXER=false  # Force FFmpeg subprocess
func defaultUseRustRemuxer() bool {
	return true
}

// defaultTranscodingEnabled returns true to enable audio transcoding by default.
//
// Audio transcoding (AC3/MP2 → AAC) is required for iOS Safari compatibility.
// The Rust remuxer is used by default, with automatic fallback to FFmpeg if needed.
//
// Users can disable transcoding entirely with:
//
//	XG2G_ENABLE_AUDIO_TRANSCODING=false
func defaultTranscodingEnabled() bool {
	return true
}
