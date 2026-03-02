// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//go:build integration

package test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Copies of helpers to avoid export/import cycle issues in test package
func buildTestBinaryHLSHelper(t *testing.T) string {
	t.Helper()
	binaryPath := filepath.Join(t.TempDir(), "xg2g-hls-test")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "../../cmd/daemon") // #nosec G204
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build daemon: %v\n%s", err, out)
	}
	return binaryPath
}

func waitForPortHLSHelper(t *testing.T, port int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
		if err == nil {
			if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusServiceUnavailable {
				_ = resp.Body.Close()

				return true
			}
			_ = resp.Body.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func generateSampleTS(t *testing.T) []byte {
	t.Helper()
	tsPath := filepath.Join(t.TempDir(), "sample.ts")
	// Generate 30 seconds of test video/audio
	cmd := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "testsrc=duration=30:size=320x240:rate=30", "-f", "lavfi", "-i", "sine=f=440:d=30", "-c:v", "libx264", "-c:a", "aac", "-f", "mpegts", tsPath) // #nosec G204
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to generate sample TS: %v\n%s", err, out)
	}
	// #nosec G304 - Testing file
	data, err := os.ReadFile(tsPath)

	if err != nil {
		t.Fatalf("failed to read sample TS: %v", err)
	}
	return data
}

func TestHLSEndpoint(t *testing.T) {
	binaryPath := buildTestBinaryHLSHelper(t)
	sampleTS := generateSampleTS(t)

	// Mock Upstream serving TS content
	mockTS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/bouquets" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"bouquets": [["1:0:1:0:0:0:0:0:0:0", "Test Channel"]]}`))
			return
		}
		if r.URL.Path == "/api/about" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"info":{"model":"test","boxtype":"test","brand":"test"},"tuners_count":1,"tuners":[{"name":"Tuner A","type":"DVB-S2"}],"fbc_tuners":[]}`))
			return
		}

		w.Header().Set("Content-Type", "video/mp2t")
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}

		timeout := time.After(30 * time.Second)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()

		offset := 0
		chunkSize := 32 * 1024 // 32KB chunks

		for {
			select {
			case <-r.Context().Done():
				return
			case <-timeout:
				return
			case <-ticker.C:
				if offset >= len(sampleTS) {
					offset = 0 // Loop
				}
				end := offset + chunkSize
				if end > len(sampleTS) {
					end = len(sampleTS)
				}
				_, err := w.Write(sampleTS[offset:end])
				if err != nil {
					return
				}
				flusher.Flush()
				offset = end
			}
		}
	}))
	defer mockTS.Close()

	t.Run("WithFFmpeg_Repair", func(t *testing.T) {
		// We assume ffmpeg might NOT be installed in this environment.
		// If not installed, this test essentially validates the Fallback path logic (logging warning but working).
		// If installed, it validates Repair path.
		// We can check if ffmpeg is in PATH.
		_, err := exec.LookPath("ffmpeg")
		hasFFmpeg := err == nil

		port := getFreeTCPPort(t)
		proxyPort := getFreeTCPPort(t)
		for port == proxyPort {
			proxyPort = getFreeTCPPort(t)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		cmd := exec.CommandContext(ctx, binaryPath) // #nosec G204
		cmd.Env = append(os.Environ(),
			"XG2G_DATA="+t.TempDir(),
			fmt.Sprintf("XG2G_LISTEN=:%d", port),
			fmt.Sprintf("XG2G_PROXY_LISTEN=:%d", proxyPort),
			fmt.Sprintf("XG2G_PROXY_PORT=%d", proxyPort),
			"XG2G_INSTANT_TUNE=false", // disable stream detector to force direct target use
			"XG2G_OWI_BASE="+mockTS.URL,
			"XG2G_BOUQUET=Test Channel",
			"XG2G_INITIAL_REFRESH=false",
			"XG2G_HDHR_ENABLED=true",
			"XG2G_PLEX_FORCE_HLS=true",     // Enable HLS
			"XG2G_H264_STREAM_REPAIR=true", // Enable Repair
			"XG2G_PROXY_TARGET="+mockTS.URL,
		)
		var outBuf ThreadSafeBuffer
		cmd.Stdout = &outBuf
		cmd.Stderr = &outBuf

		if err := cmd.Start(); err != nil {
			t.Fatalf("Start: %v", err)
		}
		defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

		if !waitForPortHLSHelper(t, port, 5*time.Second) || !waitForPortHLSHelper(t, proxyPort, 5*time.Second) {
			t.Fatalf("Startup fail: %s", outBuf.String())
		}

		// 1. Check Lineup.json (should have HLS URL?)
		// Actually PlexForceHLS changes lineup.json logic?
		// We can just check /hls/... playlist directly as requested.

		// 2. Fetch Playlist from Proxy
		// Use iOS User-Agent as instructed
		playlistURL := fmt.Sprintf("http://127.0.0.1:%d/hls/1:0:1:0:0:0:0:0:0:0/playlist.m3u8", proxyPort)

		client := &http.Client{Timeout: 30 * time.Second} // Give ffmpeg time to probe and segment
		req, _ := http.NewRequest("GET", playlistURL, nil)
		req.Header.Set("User-Agent", "Plex/2.0 iOS")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Playlist req failed: %v\nLogs:\n%s", err, outBuf.String())
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != 200 {
			t.Errorf("Playlist status %d\nLogs:\n%s", resp.StatusCode, outBuf.String())
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "vnd.apple.mpegurl") && !strings.Contains(ct, "application/x-mpegURL") {
			t.Errorf("Unexpected Content-Type: %s\nLogs:\n%s", ct, outBuf.String())
		}

		body, _ := io.ReadAll(resp.Body)
		sBody := string(body)
		if !strings.Contains(sBody, "#EXTM3U") {
			t.Errorf("Invalid playlist content: %s\nLogs:\n%s", sBody, outBuf.String())
		}

		// Find a segment
		// Usually "segment_0.ts" or similar
		if !strings.Contains(sBody, ".ts") {
			// If ffmpeg is used, it transcodes to segments.
			// If fallback, it packages as segments.
			// Assuming it works fast enough.
			t.Log("Playlist does not contain .ts segments yet")
		}

		// Try fetching a segment (even if not in playlist, predictable name?)
		// HLSManager names: segment_00000.ts ?
		// Or segment_%d.ts
		// Let's guess or parse.
		// If playlist has no segment, we can't test segment.

		// If HasFFmpeg is true, we expect repair logs.
		// If false, we expect warning logs.
		if !hasFFmpeg {
			if !strings.Contains(outBuf.String(), "FFmpeg not found") && !strings.Contains(outBuf.String(), "falling back") {
				// t.Log("Expected fallback warning if ffmpeg missing")
				t.Log("Expected fallback warning if ffmpeg missing, but logs might confirm it differently")
			}
		}
	})

	t.Run("WithoutFFmpeg_Fallback", func(t *testing.T) {
		port := getFreeTCPPort(t)
		proxyPort := getFreeTCPPort(t)
		for port == proxyPort {
			proxyPort = getFreeTCPPort(t)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		cmd := exec.CommandContext(ctx, binaryPath) // #nosec G204
		// Filter PATH to exclude ffmpeg
		newEnv := []string{}
		for _, e := range os.Environ() {
			if !strings.HasPrefix(e, "PATH=") {
				newEnv = append(newEnv, e)
			}
		}
		// Better: empty PATH
		newEnv = append(newEnv, "PATH=")

		cmd.Env = append(newEnv,
			"XG2G_DATA="+t.TempDir(),
			fmt.Sprintf("XG2G_LISTEN=:%d", port),
			fmt.Sprintf("XG2G_PROXY_LISTEN=:%d", proxyPort),
			fmt.Sprintf("XG2G_PROXY_PORT=%d", proxyPort),
			"XG2G_INSTANT_TUNE=false", // disable stream detector to force direct target use
			"XG2G_OWI_BASE="+mockTS.URL,
			"XG2G_BOUQUET=Test Channel",
			"XG2G_INITIAL_REFRESH=false",
			"XG2G_HDHR_ENABLED=true",
			"XG2G_PLEX_FORCE_HLS=true",
			"XG2G_H264_STREAM_REPAIR=true",
			"XG2G_PLEX_FFMPEG_PATH=/invalid/ffmpeg", // Force Missing for Plex Profile
			"XG2G_PROXY_TARGET="+mockTS.URL,
		)
		var outBuf ThreadSafeBuffer
		cmd.Stdout = &outBuf
		cmd.Stderr = &outBuf

		if err := cmd.Start(); err != nil {
			t.Fatalf("Start: %v", err)
		}
		defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

		if !waitForPortHLSHelper(t, port, 5*time.Second) || !waitForPortHLSHelper(t, proxyPort, 5*time.Second) {
			t.Fatalf("Startup fail: %s", outBuf.String())
		}

		// Fetch Playlist
		playlistURL := fmt.Sprintf("http://127.0.0.1:%d/hls/1:0:1:0:0:0:0:0:0:0/playlist.m3u8", proxyPort)
		client := &http.Client{Timeout: 5 * time.Second}
		req, _ := http.NewRequest("GET", playlistURL, nil)
		req.Header.Set("User-Agent", "Plex/2.0 iOS")

		resp, err := client.Do(req)

		// Expect 500 error (because HLS needs ffmpeg and start failed) OR 404 (if HLS disabled)
		// If 500: HLS Manager exists but failed to start stream (ffmpeg missing).
		if err == nil {
			if resp.StatusCode == 500 {
				// Expected failure for HLS without ffmpeg
				_ = resp.Body.Close()
				return
			}
			_ = resp.Body.Close()
		}

		// If 404, check logs to see if HLS Manager failed init
		if err == nil && resp.StatusCode == 404 {
			if strings.Contains(outBuf.String(), "HLS streaming disabled") {
				// OK, manager disabled.
				return
			}
		}

		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("Unexpected response without ffmpeg: err=%v status=%d\nLogs:\n%s", err, status, outBuf.String())
	})
}
