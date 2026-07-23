package bootstrap

import (
	"os/exec"
	"testing"
)

func skipIfNoFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not found in PATH, skipping preflight-dependent test")
	}
}
