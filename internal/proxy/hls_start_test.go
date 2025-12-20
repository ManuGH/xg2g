// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package proxy

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestHLSStreamer_Start_FailsFastOnFFmpegExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ffmpeg shim test uses shell scripts")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not found")
	}

	binDir := t.TempDir()
	ffmpegPath := filepath.Join(binDir, "ffmpeg")
	script := "#!/bin/sh\n" +
		"sleep 0.2\n" +
		"exit 1\n"
	// #nosec G306 -- test helper script needs to be executable
	if err := os.WriteFile(ffmpegPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write ffmpeg shim: %v", err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	stream := &HLSStreamer{
		serviceRef:      "ref:hls:exit",
		targetURL:       "http://fake/stream",
		outputDir:       t.TempDir(),
		logger:          zerolog.New(io.Discard),
		playlistTimeout: 30 * time.Second,
	}

	start := time.Now()
	err := stream.Start()
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected Start() to fail, but it succeeded")
	}
	if !strings.Contains(err.Error(), "ffmpeg exited before playlist ready") {
		t.Fatalf("expected early-exit error, got: %v", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("Start() took too long (%v), expected fast failure after ffmpeg exit", elapsed)
	}
}
