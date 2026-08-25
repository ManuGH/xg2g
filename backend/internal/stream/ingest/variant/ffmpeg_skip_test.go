// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package variant

import (
	"os/exec"
	"testing"
)

// skipIfNoFFmpeg skips a test that shells out to ffmpeg when the binary is not
// installed.
//
// These tests decode a real capture and are worth keeping strict where ffmpeg
// exists — a developer machine, and the scheduled deep run that installs it.
// The PR gate deliberately does not install it ("scoped + deterministic, no
// ffmpeg binary needed"), so without this they failed the gate for the absence
// of a tool rather than for anything about the code.
//
// Skipping, not softening: where ffmpeg is present the assertions are unchanged.
func skipIfNoFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not found in PATH, skipping decode-verification test")
	}
}
