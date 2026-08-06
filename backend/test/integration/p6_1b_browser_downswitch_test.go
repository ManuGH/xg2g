package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type AccessLogRecord struct {
	Timestamp  time.Time `json:"timestamp"`
	Path       string    `json:"path"`
	Variant    string    `json:"variant"`
	Segment    string    `json:"segment"`
	Bytes      int64     `json:"bytes"`
	DurationMS int64     `json:"duration_ms"`
	Throttled  bool      `json:"throttled"`
}

type P61bServerHandler struct {
	targetDir   string
	throttled   atomic.Bool
	highStarted atomic.Bool
	mu          sync.Mutex
	accessLogs  []AccessLogRecord
}

func (h *P61bServerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// CORS Headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "*")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Control Signal Endpoint from Browser Harness
	if r.URL.Path == "/api/signal_high_started" && r.Method == http.MethodPost {
		h.highStarted.Store(true)
		h.throttled.Store(true)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"high_started_acknowledged"}`))
		return
	}

	start := time.Now()
	relPath := filepath.Clean(r.URL.Path)
	fullPath := filepath.Join(h.targetDir, relPath)

	variant := "master"
	if strings.Contains(r.URL.Path, "variant_high") {
		variant = "high"
	} else if strings.Contains(r.URL.Path, "variant_low") {
		variant = "low"
	}

	segment := ""
	if strings.HasSuffix(r.URL.Path, ".ts") {
		segment = filepath.Base(r.URL.Path)
	}

	// Set correct HLS MIME types
	if strings.HasSuffix(r.URL.Path, ".m3u8") {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	} else if strings.HasSuffix(r.URL.Path, ".ts") {
		w.Header().Set("Content-Type", "video/mp2t")
	}

	isThrottledReq := false
	var bytesWritten int64

	if variant == "high" && strings.HasSuffix(r.URL.Path, ".ts") && h.throttled.Load() {
		isThrottledReq = true
		file, err := os.Open(fullPath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer file.Close()

		w.WriteHeader(http.StatusOK)
		buf := make([]byte, 4096)
		for {
			n, readErr := file.Read(buf)
			if n > 0 {
				nw, _ := w.Write(buf[:n])
				bytesWritten += int64(nw)
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
				time.Sleep(35 * time.Millisecond)
			}
			if readErr != nil {
				break
			}
		}
	} else {
		// Unthrottled delivery
		info, err := os.Stat(fullPath)
		if err == nil && !info.IsDir() {
			bytesWritten = info.Size()
		}
		http.FileServer(http.Dir(h.targetDir)).ServeHTTP(w, r)
	}

	dur := time.Since(start)

	h.mu.Lock()
	h.accessLogs = append(h.accessLogs, AccessLogRecord{
		Timestamp:  start,
		Path:       r.URL.Path,
		Variant:    variant,
		Segment:    segment,
		Bytes:      bytesWritten,
		DurationMS: dur.Milliseconds(),
		Throttled:  isThrottledReq,
	})
	h.mu.Unlock()
}

type NodeHarnessResult struct {
	Success              bool    `json:"success"`
	Error                string  `json:"error"`
	HighLevelIndex       int     `json:"highLevelIndex"`
	LowLevelIndex        int     `json:"lowLevelIndex"`
	HighStartedSignaled  bool    `json:"highStartedSignaled"`
	DownswitchedObserved bool    `json:"downswitchedObserved"`
	SampleCount          int     `json:"sampleCount"`
	StartTime            float64 `json:"startTime"`
	EndTime              float64 `json:"endTime"`
	ProgressSeconds      float64 `json:"progressSeconds"`
	FatalErrorCount      int     `json:"fatalErrorCount"`
}

func TestP6_1b_BrowserDownswitchAgainstLiveSpike(t *testing.T) {
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node binary not found in PATH, skipping browser downswitch E2E test")
	}
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg binary not found in PATH, skipping browser downswitch E2E test")
	}
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe binary not found in PATH, skipping browser downswitch E2E test")
	}

	repoRoot, err := filepath.Abs("../../")
	require.NoError(t, err)

	scriptPath := filepath.Join(repoRoot, "scripts", "spikes", "dual_rendition_spike.sh")
	_, err = os.Stat(scriptPath)
	require.NoError(t, err, "dual_rendition_spike.sh must exist at %s", scriptPath)

	hlsJsPath := filepath.Join(repoRoot, "..", "apps", "webui", "node_modules", "hls.js", "dist", "hls.min.js")
	hlsJsPath, err = filepath.Abs(hlsJsPath)
	require.NoError(t, err)
	if _, err := os.Stat(hlsJsPath); os.IsNotExist(err) {
		t.Skipf("local hls.min.js not found at %s, skipping test", hlsJsPath)
	}

	nodeHarnessPath := filepath.Join(repoRoot, "e2e", "p6_1b_player_downswitch.mjs")
	if _, err := os.Stat(nodeHarnessPath); os.IsNotExist(err) {
		t.Skipf("Playwright harness script not found at %s, skipping test", nodeHarnessPath)
	}

	outDir := t.TempDir()

	// 1. Launch Live Continuous FFmpeg Dual-Rendition Stream
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, scriptPath, "--output-dir", outDir, "--ffmpeg", ffmpegPath, "--ffprobe", ffprobePath, "--live")
	err = cmd.Start()
	require.NoError(t, err, "Failed to start live dual_rendition_spike.sh process")

	initialPID := cmd.Process.Pid
	t.Logf("Started live FFmpeg process with PID: %d", initialPID)

	// Ensure cleanup of live process on test exit
	defer func() {
		cancel()
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				_ = cmd.Process.Kill()
			}
		}
	}()

	// 2. Wait for Initial HLS Stream Readiness
	t.Log("==> Waiting for HLS stream readiness...")
	masterPath := filepath.Join(outDir, "index.m3u8")
	highPath := filepath.Join(outDir, "variant_high", "index.m3u8")
	lowPath := filepath.Join(outDir, "variant_low", "index.m3u8")

	readyDeadline := time.Now().Add(10 * time.Second)
	ready := false
	for time.Now().Before(readyDeadline) {
		if fileExistsNonEmpty(masterPath) && fileExistsNonEmpty(highPath) && fileExistsNonEmpty(lowPath) {
			ready = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	require.True(t, ready, "HLS playlists must exist and be non-empty within timeout")

	// 3. Start Rate-Limiting HTTP Test Server
	handler := &P61bServerHandler{targetDir: outDir}
	ts := httptest.NewServer(handler)
	defer ts.Close()

	streamURL := fmt.Sprintf("%s/index.m3u8", ts.URL)
	signalURL := fmt.Sprintf("%s/api/signal_high_started", ts.URL)

	// 4. Run Playwright Headless Browser Harness Script
	t.Log("==> Executing Playwright Chromium hls.js Browser Harness...")
	nodeCmd := exec.Command(nodeBin, nodeHarnessPath, "--stream-url", streamURL, "--signal-url", signalURL, "--hls-js-path", hlsJsPath)
	nodeCmd.Dir = filepath.Join(repoRoot, "e2e")
	nodeOut, err := nodeCmd.CombinedOutput()
	t.Logf("Node Harness Output:\n%s", string(nodeOut))
	require.NoError(t, err, "Node Playwright harness failed: %s", string(nodeOut))

	var harnessRes NodeHarnessResult
	err = json.Unmarshal(nodeOut, &harnessRes)
	require.NoError(t, err, "Failed to parse Node harness JSON output: %s", string(nodeOut))

	// 5. Invariant Assertions on Browser ABR Results
	t.Log("==> Verifying Browser ABR Downswitch Assertions...")
	require.True(t, harnessRes.Success, "Browser harness must succeed: %s", harnessRes.Error)
	assert.True(t, harnessRes.HighStartedSignaled, "Browser must have signaled High level start")
	assert.True(t, harnessRes.DownswitchedObserved, "Browser hls.js must have observed LEVEL_SWITCHED to Low")
	assert.Equal(t, 0, harnessRes.FatalErrorCount, "Must have 0 fatal hls.js errors")
	assert.True(t, harnessRes.ProgressSeconds >= 1.5, "Playback currentTime must progress at least 1.5 seconds (actual: %.2f)", harnessRes.ProgressSeconds)

	// 6. Access Log Audit Assertions
	t.Log("==> Auditing Server HTTP Access Logs...")
	handler.mu.Lock()
	logs := make([]AccessLogRecord, len(handler.accessLogs))
	copy(logs, handler.accessLogs)
	handler.mu.Unlock()

	hasPreShapingHigh := false
	hasPostShapingThrottledHigh := false
	hasPostShapingLow := false

	for _, log := range logs {
		if log.Variant == "high" && strings.HasSuffix(log.Path, ".ts") {
			if log.Throttled {
				hasPostShapingThrottledHigh = true
			} else {
				hasPreShapingHigh = true
			}
		}
		if log.Variant == "low" && strings.HasSuffix(log.Path, ".ts") {
			hasPostShapingLow = true
		}
	}

	assert.True(t, hasPreShapingHigh, "Access logs must confirm High segment requests before shaping")
	assert.True(t, hasPostShapingThrottledHigh, "Access logs must confirm throttled High segment request")
	assert.True(t, hasPostShapingLow, "Access logs must confirm browser requested Low segments after shaping")

	// 7. Verify Process Liveness & PID Stability
	t.Log("==> Verifying FFmpeg Process Liveness & PID Stability...")
	assert.Equal(t, initialPID, cmd.Process.Pid, "FFmpeg OS process PID must remain 100% constant")
	err = syscall.Kill(initialPID, 0)
	require.NoError(t, err, "FFmpeg OS process (PID %d) must still be alive after downswitch", initialPID)
}

func fileExistsNonEmpty(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Size() > 0
}
