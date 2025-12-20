// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// SPDX-License-Identifier: MIT

package proxy

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	streamprofile "github.com/ManuGH/xg2g/internal/core/profile"
	"github.com/rs/zerolog"
)

// TestSafariDVR_Stop_Idempotent_NoDoubleWaitDeadlock ensures that calling Stop()
// multiple times is safe and does not cause panics or double-wait errors.
func TestSafariDVR_Stop_Idempotent_NoDoubleWaitDeadlock(t *testing.T) {
	logger := zerolog.New(io.Discard)
	tmpDir := t.TempDir()

	config := streamprofile.SafariDVRConfig{
		SegmentDuration: 2,
		DVRWindowSize:   60,
	}

	dummyFFmpeg := filepath.Join(tmpDir, "ffmpeg_dummy.sh")
	script := "#!/bin/sh\n" +
		"sleep 5\n"
	// #nosec G306 -- test helper script needs to be executable
	if err := os.WriteFile(dummyFFmpeg, []byte(script), 0755); err != nil {
		t.Fatalf("failed to create dummy ffmpeg: %v", err)
	}
	config.FFmpegPath = dummyFFmpeg

	profile, err := NewSafariDVRProfile("ref:1:0:1", "http://fake/stream", tmpDir, logger, config)
	if err != nil {
		t.Fatalf("NewSafariDVRProfile failed: %v", err)
	}

	// Start the profile
	// It will try to start the dummy script.
	if err := profile.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Wait a bit to ensure waiter is running
	time.Sleep(100 * time.Millisecond)

	var wg sync.WaitGroup
	wg.Add(2)

	// Call Stop twice concurrently
	go func() {
		defer wg.Done()
		profile.Stop()
	}()
	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)
		profile.Stop()
	}()

	// Wait for stops to complete (should be fast due to SIGTERM/KILL)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(3 * time.Second):
		t.Fatal("Stop() deadlock or timeout")
	}

	// Double check internal state
	profile.mu.RLock()
	if profile.started {
		t.Error("profile should be marked stopped")
	}
	profile.mu.RUnlock()
}

// TestSafariDVR_TerminatesProcessGroup verifies that Stop() terminates child processes.
func TestSafariDVR_TerminatesProcessGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process groups are unix-specific")
	}

	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not found")
	}
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep not found")
	}

	tmpDir := t.TempDir()
	dummyBin := filepath.Join(tmpDir, "group_sleep.sh")
	script2 := "#!/bin/sh\n" +
		"sleep 10 &\n" +
		"echo $! > child.pid\n" +
		"wait\n"
	// #nosec G306 -- test helper script needs to be executable
	if err := os.WriteFile(dummyBin, []byte(script2), 0755); err != nil {
		t.Fatalf("failed to create dummy script: %v", err)
	}

	logger := zerolog.New(io.Discard)
	config := streamprofile.SafariDVRConfig{
		SegmentDuration: 2,
		DVRWindowSize:   60,
		FFmpegPath:      dummyBin,
	}

	profile, err := NewSafariDVRProfile("ref:test:group", "http://fake/stream", tmpDir, logger, config)
	if err != nil {
		t.Fatalf("NewSafariDVRProfile failed: %v", err)
	}

	start := time.Now()
	if err := profile.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	childPIDPath := filepath.Join(profile.outputDir, "child.pid")
	var childPID int
	deadline := time.Now().Add(2 * time.Second)
	for {
		// #nosec G304 -- test reads a pid file from a temp dir created in this test
		data, err := os.ReadFile(childPIDPath)
		if err == nil {
			if pid, convErr := strconv.Atoi(strings.TrimSpace(string(data))); convErr == nil && pid > 0 {
				childPID = pid
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("child pid file not created: %s", childPIDPath)
		}
		time.Sleep(25 * time.Millisecond)
	}

	profile.Stop()

	duration := time.Since(start)
	if duration > 5*time.Second {
		t.Errorf("Stop() took too long (%v), potentially waiting for full 10s sleep", duration)
	}

	// Verify the child process is gone (best-effort: allow brief reaping delay).
	killDeadline := time.Now().Add(2 * time.Second)
	for {
		err := syscall.Kill(childPID, 0)
		if err != nil {
			break
		}
		if time.Now().After(killDeadline) {
			_ = syscall.Kill(childPID, syscall.SIGKILL)
			t.Fatalf("child process %d still alive after Stop()", childPID)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// TestSafariDVR_FFmpegCrash_HandlesGracefully verifies that the profile handles ffmpeg crashes gracefully.
func TestSafariDVR_FFmpegCrash_HandlesGracefully(t *testing.T) {
	logger := zerolog.New(io.Discard)
	tmpDir := t.TempDir()

	config := streamprofile.SafariDVRConfig{
		SegmentDuration: 2,
		DVRWindowSize:   60,
	}

	// Create a script that crashes immediately
	dummyFFmpeg := filepath.Join(tmpDir, "ffmpeg_crash.sh")
	script := "#!/bin/sh\n" +
		"echo 'Starting...' >&2\n" +
		"sleep 0.5\n" +
		"echo 'Error: Simulated ffmpeg crash' >&2\n" +
		"exit 1\n"
	// #nosec G306 -- test helper script needs to be executable
	if err := os.WriteFile(dummyFFmpeg, []byte(script), 0755); err != nil {
		t.Fatalf("failed to create dummy ffmpeg: %v", err)
	}
	config.FFmpegPath = dummyFFmpeg

	profile, err := NewSafariDVRProfile("ref:1:0:crash", "http://fake/stream", tmpDir, logger, config)
	if err != nil {
		t.Fatalf("NewSafariDVRProfile failed: %v", err)
	}

	// Start the profile - should succeed initially
	if err := profile.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Wait for process to crash
	time.Sleep(1 * time.Second)

	// Verify profile detected the crash and marked itself as not started
	profile.mu.RLock()
	started := profile.started
	profile.mu.RUnlock()

	if started {
		t.Error("profile should have detected ffmpeg crash and marked itself as stopped")
	}

	// Calling Stop should be safe even after crash
	profile.Stop()

	// Verify final state
	profile.mu.RLock()
	if profile.started {
		t.Error("profile should be stopped after explicit Stop()")
	}
	profile.mu.RUnlock()
}

// TestSafariDVR_Start_WhenStopping_ReturnsError verifies that Start() fails when Stop() is in progress.
func TestSafariDVR_Start_WhenStopping_ReturnsError(t *testing.T) {
	logger := zerolog.New(io.Discard)
	tmpDir := t.TempDir()

	config := streamprofile.SafariDVRConfig{
		SegmentDuration: 2,
		DVRWindowSize:   60,
	}

	// Create a long-running script
	dummyFFmpeg := filepath.Join(tmpDir, "ffmpeg_long.sh")
	script := "#!/bin/sh\n" +
		"sleep 30\n"
	// #nosec G306 -- test helper script needs to be executable
	if err := os.WriteFile(dummyFFmpeg, []byte(script), 0755); err != nil {
		t.Fatalf("failed to create dummy ffmpeg: %v", err)
	}
	config.FFmpegPath = dummyFFmpeg

	profile, err := NewSafariDVRProfile("ref:1:0:stopping", "http://fake/stream", tmpDir, logger, config)
	if err != nil {
		t.Fatalf("NewSafariDVRProfile failed: %v", err)
	}

	// Start the profile
	if err := profile.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Manually set stopping flag to simulate Stop() in progress
	profile.mu.Lock()
	profile.stopping = true
	profile.mu.Unlock()

	// Try to start again - should fail
	err = profile.Start()
	if err == nil {
		t.Fatal("expected Start() to fail when stopping=true, but it succeeded")
	}

	expectedMsg := "safari dvr profile is stopping"
	if !strings.Contains(err.Error(), expectedMsg) {
		t.Errorf("expected error message to contain %q, got: %v", expectedMsg, err)
	}

	// Cleanup
	profile.mu.Lock()
	profile.stopping = false
	profile.mu.Unlock()
	profile.Stop()
}

// TestSafariDVR_InvalidOutputPath_ReturnsError verifies that invalid paths are rejected.
func TestSafariDVR_InvalidOutputPath_ReturnsError(t *testing.T) {
	logger := zerolog.New(io.Discard)

	config := streamprofile.SafariDVRConfig{
		SegmentDuration: 2,
		DVRWindowSize:   60,
		FFmpegPath:      "/usr/bin/ffmpeg",
	}

	// Try to create profile with path traversal attempt
	_, err := NewSafariDVRProfile("../../etc/passwd", "http://fake/stream", "/tmp", logger, config)
	if err == nil {
		t.Fatal("expected NewSafariDVRProfile to reject path traversal, but it succeeded")
	}
}
