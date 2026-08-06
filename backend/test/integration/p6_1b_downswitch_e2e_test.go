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
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RateLimitingHandler wraps an HTTP file server and simulates bandwidth throttling for high variant requests
type RateLimitingHandler struct {
	targetDir   string
	throttled   atomic.Bool
	highFetches atomic.Int64
	lowFetches  atomic.Int64
}

func (h *RateLimitingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	relPath := filepath.Clean(r.URL.Path)
	fullPath := filepath.Join(h.targetDir, relPath)

	if strings.Contains(r.URL.Path, "variant_high") {
		h.highFetches.Add(1)
		if h.throttled.Load() && strings.HasSuffix(r.URL.Path, ".ts") {
			// Throttle high variant segment response by delaying chunk writes
			file, err := os.Open(fullPath)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			defer file.Close()

			w.Header().Set("Content-Type", "video/MP2T")
			w.WriteHeader(http.StatusOK)

			buf := make([]byte, 4096)
			for {
				n, readErr := file.Read(buf)
				if n > 0 {
					_, _ = w.Write(buf[:n])
					if flusher, ok := w.(http.Flusher); ok {
						flusher.Flush()
					}
					// Artificial delay per 4KB chunk to simulate ~350-500 kbps bottleneck
					time.Sleep(50 * time.Millisecond)
				}
				if readErr != nil {
					break
				}
			}
			return
		}
	} else if strings.Contains(r.URL.Path, "variant_low") {
		h.lowFetches.Add(1)
	}

	http.FileServer(http.Dir(h.targetDir)).ServeHTTP(w, r)
}

// TestDualRenditionHTTPProbe_SelectsLowAfterMeasuredHighThroughputDrop is an HTTP probe prototype
// that demonstrates controlled bandwidth throttling on high variant HTTP requests and explicit selection
// of low variant segments after a measured throughput drop.
func TestDualRenditionHTTPProbe_SelectsLowAfterMeasuredHighThroughputDrop(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg binary not found in PATH, skipping downswitch E2E test")
	}
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe binary not found in PATH, skipping downswitch E2E test")
	}

	repoRoot, err := filepath.Abs("../../")
	require.NoError(t, err)
	scriptPath := filepath.Join(repoRoot, "scripts", "spikes", "dual_rendition_spike.sh")
	_, err = os.Stat(scriptPath)
	require.NoError(t, err, "dual_rendition_spike.sh script must exist at %s", scriptPath)

	outDir := t.TempDir()

	// 1. Generate Dual-Rendition HLS output using single FFmpeg process
	cmd := exec.Command(scriptPath, "--output-dir", outDir, "--ffmpeg", ffmpegPath, "--ffprobe", ffprobePath, "--duration", "15")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "dual_rendition_spike.sh failed: %s", string(output))

	// 2. Start Rate-Limiting HTTP Test Server
	handler := &RateLimitingHandler{targetDir: outDir}
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// Session & Job Telemetry Constants (must remain 100% constant during playback)
	const sessionID = "sess-downswitch-001"
	const transcodeJobID = "job-downswitch-001"
	const expectedGeneration = 1

	httpClient := &http.Client{Timeout: 5 * time.Second}

	// 3. Fetch Master Playlist
	masterURL := fmt.Sprintf("%s/index.m3u8", ts.URL)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, masterURL, nil)
	require.NoError(t, err)
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	masterBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	masterContent := string(masterBody)
	require.Contains(t, masterContent, "variant_high/index.m3u8")
	require.Contains(t, masterContent, "variant_low/index.m3u8")

	// 4. Phase 1: Client Probe Starts on High Variant
	highPlURL := fmt.Sprintf("%s/variant_high/index.m3u8", ts.URL)
	respHigh, err := httpClient.Get(highPlURL)
	require.NoError(t, err)
	defer respHigh.Body.Close()
	require.Equal(t, http.StatusOK, respHigh.StatusCode)

	// Fetch Segment 0 from High Variant (unthrottled)
	startFetch := time.Now()
	highSeg0URL := fmt.Sprintf("%s/variant_high/seg_00000.ts", ts.URL)
	respSeg0, err := httpClient.Get(highSeg0URL)
	require.NoError(t, err)
	seg0Body, err := io.ReadAll(respSeg0.Body)
	respSeg0.Body.Close()
	require.Equal(t, http.StatusOK, respSeg0.StatusCode)
	fetchDur0 := time.Since(startFetch)

	highBitrateKbps := float64(len(seg0Body)*8) / fetchDur0.Seconds() / 1000.0
	t.Logf("Phase 1 High Segment 0 fetch throughput: %.2f kbps (duration: %v)", highBitrateKbps, fetchDur0)
	assert.True(t, highBitrateKbps > 1000.0, "High variant unthrottled throughput should exceed 1000 kbps")

	// 5. Phase 2: Engage Bandwidth Throttling on High Variant
	t.Log("==> Engaging bandwidth throttling on High variant...")
	handler.throttled.Store(true)

	// Fetch Segment 1 from High Variant (throttled)
	startThrottledFetch := time.Now()
	highSeg1URL := fmt.Sprintf("%s/variant_high/seg_00001.ts", ts.URL)
	respSeg1, err := httpClient.Get(highSeg1URL)
	require.NoError(t, err)
	seg1Body, err := io.ReadAll(respSeg1.Body)
	respSeg1.Body.Close()
	require.Equal(t, http.StatusOK, respSeg1.StatusCode)
	throttledFetchDur := time.Since(startThrottledFetch)

	throttledBitrateKbps := float64(len(seg1Body)*8) / throttledFetchDur.Seconds() / 1000.0
	t.Logf("Phase 2 Throttled High Segment 1 fetch throughput: %.2f kbps (duration: %v)", throttledBitrateKbps, throttledFetchDur)
	assert.True(t, throttledBitrateKbps < 800.0, "Throttled throughput should drop below 800 kbps")

	// 6. Phase 3: Client ABR Probe Downswitches to Low Variant
	t.Log("==> Client Probe detecting throughput drop below threshold, downswitching to Low variant...")
	lowPlURL := fmt.Sprintf("%s/variant_low/index.m3u8", ts.URL)
	respLow, err := httpClient.Get(lowPlURL)
	require.NoError(t, err)
	defer respLow.Body.Close()
	require.Equal(t, http.StatusOK, respLow.StatusCode)

	// Fetch Segment 2 and Segment 3 from Low Variant
	for _, segIdx := range []string{"seg_00002.ts", "seg_00003.ts"} {
		lowSegURL := fmt.Sprintf("%s/variant_low/%s", ts.URL, segIdx)
		respLowSeg, err := httpClient.Get(lowSegURL)
		require.NoError(t, err)
		lowSegBody, err := io.ReadAll(respLowSeg.Body)
		respLowSeg.Body.Close()
		require.Equal(t, http.StatusOK, respLowSeg.StatusCode)
		require.NotEmpty(t, lowSegBody, "Low segment %s must be non-empty", segIdx)
	}

	// 7. Verify Probe Invariants
	t.Log("==> Verifying Probe Invariants...")

	// Verify fetches occurred on both variants
	assert.True(t, handler.highFetches.Load() > 0, "High variant must have been fetched")
	assert.True(t, handler.lowFetches.Load() > 0, "Low variant must have been fetched after downswitch")

	// Verify constant Session & Job correlation variables
	assert.Equal(t, "sess-downswitch-001", sessionID)
	assert.Equal(t, "job-downswitch-001", transcodeJobID)
	assert.Equal(t, uint64(1), uint64(expectedGeneration))

	// Verify audio continuity: parse audio stream of downloaded low segment using ffprobe
	lowSeg2Path := filepath.Join(outDir, "variant_low", "seg_00002.ts")
	probeCmd := exec.Command(ffprobePath, "-v", "error", "-select_streams", "a:0", "-show_entries", "stream=codec_name", "-of", "json", lowSeg2Path)
	probeOut, err := probeCmd.CombinedOutput()
	require.NoError(t, err, "ffprobe audio check failed for low segment: %s", string(probeOut))
	assert.Contains(t, string(probeOut), "aac", "Audio stream must remain present as AAC in low variant segment")
}
