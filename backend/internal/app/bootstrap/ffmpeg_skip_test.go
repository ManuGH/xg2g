package bootstrap_test

import (
	"os/exec"
	"testing"
)

// skipIfNoFFmpeg skips tests that exercise WireServices' startup preflight,
// which requires a real ffmpeg binary on PATH. The required PR gate is
// intentionally ffmpeg-free (see ci.yml); these tests still run for real
// wherever ffmpeg is available (local dev, Go Coverage Report workflow).
func skipIfNoFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not found in PATH, skipping preflight-dependent test")
	}
}
