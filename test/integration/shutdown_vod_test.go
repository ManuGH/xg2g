// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//go:build integration

package test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func writeFakeFFmpeg(t *testing.T, pidFile string) string {
	t.Helper()

	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "fake-ffmpeg.sh")
	script := "#!/bin/sh\n" +
		"set -e\n" +
		"if [ -z \"$FAKE_FFMPEG_PIDFILE\" ]; then\n" +
		"  echo 'FAKE_FFMPEG_PIDFILE missing' >&2\n" +
		"  exit 1\n" +
		"fi\n" +
		"echo $$ > \"$FAKE_FFMPEG_PIDFILE\"\n" +
		"trap 'exit 0' INT TERM\n" +
		"while true; do sleep 1; done\n"

	if err := os.WriteFile(binPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake ffmpeg script: %v", err)
	}

	return binPath
}

func readPID(t *testing.T, pidFile string) int {
	t.Helper()

	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("failed to read pid file: %v", err)
	}

	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		t.Fatalf("invalid pid %q: %v", pidStr, err)
	}
	return pid
}

func waitForPIDFile(t *testing.T, pidFile string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(pidFile); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for pid file: %s", pidFile)
}

func waitForProcessExit(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()

	if _, err := os.Stat("/proc"); err != nil {
		t.Skip("/proc not available for process checks")
	}

	deadline := time.Now().Add(timeout)
	procPath := filepath.Join("/proc", strconv.Itoa(pid))
	for time.Now().Before(deadline) {
		if _, err := os.Stat(procPath); os.IsNotExist(err) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("process %d still running after timeout", pid)
}

func TestShutdownKillsRecordingFFmpeg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping shutdown VOD test in short mode")
	}

	binaryPath := filepath.Join(t.TempDir(), "xg2g-test")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "../../cmd/daemon")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build daemon: %v\n%s", err, out)
	}

	dataDir := t.TempDir()
	localRoot := filepath.Join(dataDir, "recordings")
	if err := os.MkdirAll(localRoot, 0750); err != nil {
		t.Fatalf("failed to create local recordings dir: %v", err)
	}

	recordingFile := filepath.Join(localRoot, "test.ts")
	if err := os.WriteFile(recordingFile, []byte("dummy"), 0644); err != nil {
		t.Fatalf("failed to write recording file: %v", err)
	}

	// Ensure the file is stable for playback checks.
	time.Sleep(50 * time.Millisecond)

	pidFile := filepath.Join(t.TempDir(), "ffmpeg.pid")
	fakeFFmpeg := writeFakeFFmpeg(t, pidFile)

	port := getFreeTCPPort(t)
	apiToken := "test-token"
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/recordings/test.ts"
	recordingID := base64.RawURLEncoding.EncodeToString([]byte(serviceRef))

	env := []string{
		"XG2G_DATA=" + dataDir,
		"XG2G_LISTEN=:" + strconv.Itoa(port),
		"XG2G_EPG_ENABLED=false",
		"XG2G_API_TOKEN=" + apiToken,
		"XG2G_API_TOKEN_SCOPES=v3:read,v3:write",
		"XG2G_V3_WORKER_ENABLED=false",
		"XG2G_V3_HLS_ROOT=" + filepath.Join(dataDir, "v3-hls"),
		"XG2G_RECORDINGS_MAP=/recordings=" + localRoot,
		"XG2G_RECORDING_PLAYBACK_POLICY=local_only",
		"XG2G_RECORDING_STABLE_WINDOW=10ms",
		"XG2G_V3_FFMPEG_BIN=" + fakeFFmpeg,
		"FAKE_FFMPEG_PIDFILE=" + pidFile,
		"PATH=" + os.Getenv("PATH"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath)
	cmd.Env = env
	var outputBuffer strings.Builder
	cmd.Stdout = &outputBuffer
	cmd.Stderr = &outputBuffer

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start daemon: %v", err)
	}

	ready := false
	for i := 0; i < 50; i++ {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		_ = cmd.Process.Kill()
		t.Fatalf("daemon did not become ready. Output:\n%s", outputBuffer.String())
	}

	req, err := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/api/v3/recordings/%s/playlist.m3u8", port, recordingID), nil)
	if err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("failed to build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("failed to trigger recording build: %v", err)
	}
	_ = resp.Body.Close()

	waitForPIDFile(t, pidFile, 5*time.Second)
	pid := readPID(t, pidFile)

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("failed to send SIGTERM: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) && exitErr.ExitCode() != 0 {
				t.Fatalf("daemon exited with code %d", exitErr.ExitCode())
			}
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("daemon did not shut down within timeout")
	}

	waitForProcessExit(t, pid, 5*time.Second)
}
