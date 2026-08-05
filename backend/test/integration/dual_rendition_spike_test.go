package test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type FFprobeStream struct {
	Index     int    `json:"index"`
	CodecName string `json:"codec_name"`
	CodecType string `json:"codec_type"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
}

type FFprobeOutput struct {
	Streams []FFprobeStream `json:"streams"`
}

func TestDualRenditionHLSSpike(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg binary not found in PATH, skipping dual-rendition HLS spike test")
	}
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe binary not found in PATH, skipping dual-rendition HLS spike test")
	}

	repoRoot, err := filepath.Abs("../../")
	require.NoError(t, err)
	scriptPath := filepath.Join(repoRoot, "scripts", "spikes", "dual_rendition_spike.sh")
	_, err = os.Stat(scriptPath)
	require.NoError(t, err, "dual_rendition_spike.sh script must exist at %s", scriptPath)

	outDir := t.TempDir()

	// Execute Bash script (single source of FFmpeg execution truth)
	cmd := exec.Command(scriptPath, "--output-dir", outDir, "--ffmpeg", ffmpegPath, "--ffprobe", ffprobePath, "--duration", "10")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "dual_rendition_spike.sh failed: %s", string(output))

	// =========================================================================
	// Level 1 Validation: FFmpeg Log Mapping Evidence
	// =========================================================================
	logPath := filepath.Join(outDir, "logs", "ffmpeg_stderr.log")
	logBytes, err := os.ReadFile(logPath)
	require.NoError(t, err, "FFmpeg stderr log file must exist")
	logContent := string(logBytes)
	assert.Contains(t, logContent, "Stream mapping:", "FFmpeg log should record Stream mapping header")

	// =========================================================================
	// Level 2 Validation: Master Playlist (index.m3u8) Content & Structure
	// =========================================================================
	masterPath := filepath.Join(outDir, "index.m3u8")
	masterBytes, err := os.ReadFile(masterPath)
	require.NoError(t, err, "Master playlist index.m3u8 must exist")
	masterContent := string(masterBytes)

	// Check STREAM-INF count
	streamInfCount := strings.Count(masterContent, "#EXT-X-STREAM-INF:")
	assert.Equal(t, 2, streamInfCount, "Master playlist must contain exactly 2 #EXT-X-STREAM-INF entries")

	// Check variant URIs
	assert.Contains(t, masterContent, "variant_high/index.m3u8", "Master playlist must reference variant_high/index.m3u8")
	assert.Contains(t, masterContent, "variant_low/index.m3u8", "Master playlist must reference variant_low/index.m3u8")

	// Check resolutions
	assert.Contains(t, masterContent, "RESOLUTION=1280x720", "Master playlist must specify 1280x720 for high variant")
	assert.Contains(t, masterContent, "RESOLUTION=640x360", "Master playlist must specify 640x360 for low variant")

	// Ensure no separate external audio group
	assert.NotContains(t, masterContent, "#EXT-X-MEDIA:TYPE=AUDIO", "Master playlist must not contain external audio group tags")

	// Parse bandwidths and verify distinctness
	lines := strings.Split(masterContent, "\n")
	var bandwidths []int
	for _, line := range lines {
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			if idx := strings.Index(line, "BANDWIDTH="); idx >= 0 {
				rest := line[idx+len("BANDWIDTH="):]
				endIdx := strings.IndexAny(rest, ", \r\n")
				if endIdx >= 0 {
					rest = rest[:endIdx]
				}
				bw, err := strconv.Atoi(rest)
				if err == nil {
					bandwidths = append(bandwidths, bw)
				}
			}
		}
	}
	require.Len(t, bandwidths, 2, "Must extract 2 BANDWIDTH values from master playlist")
	assert.NotEqual(t, bandwidths[0], bandwidths[1], "High and low variants must have distinct BANDWIDTH values")
	assert.True(t, bandwidths[0] > bandwidths[1], "High variant bandwidth (%d) should be greater than low variant bandwidth (%d)", bandwidths[0], bandwidths[1])

	// =========================================================================
	// Level 3 Validation: ffprobe Per-Variant Stream Inspection
	// =========================================================================
	variants := []struct {
		name       string
		relPath    string
		expWidth   int
		expHeight  int
		minBw      int
	}{
		{name: "high", relPath: "variant_high/index.m3u8", expWidth: 1280, expHeight: 720, minBw: 800000},
		{name: "low", relPath: "variant_low/index.m3u8", expWidth: 640, expHeight: 360, minBw: 400000},
	}

	for _, v := range variants {
		variantPlPath := filepath.Join(outDir, v.relPath)
		plBytes, err := os.ReadFile(variantPlPath)
		require.NoError(t, err, "Variant playlist %s must exist", v.relPath)
		plContent := string(plBytes)

		// Assert independent segments tag
		assert.Contains(t, plContent, "#EXT-X-INDEPENDENT-SEGMENTS", "Variant playlist %s must include #EXT-X-INDEPENDENT-SEGMENTS", v.relPath)

		// Execute ffprobe on variant playlist
		probeCmd := exec.Command(ffprobePath, "-v", "error", "-show_entries", "stream=index,codec_type,codec_name,width,height", "-of", "json", variantPlPath)
		probeOut, err := probeCmd.CombinedOutput()
		require.NoError(t, err, "ffprobe failed for %s: %s", v.relPath, string(probeOut))

		var probeResult FFprobeOutput
		err = json.Unmarshal(probeOut, &probeResult)
		require.NoError(t, err, "ffprobe JSON unmarshal failed for %s", v.relPath)

		videoCount := 0
		audioCount := 0
		for _, st := range probeResult.Streams {
			switch st.CodecType {
			case "video":
				videoCount++
				assert.Equal(t, "h264", st.CodecName, "%s video codec should be h264", v.name)
				assert.Equal(t, v.expWidth, st.Width, "%s width mismatch", v.name)
				assert.Equal(t, v.expHeight, st.Height, "%s height mismatch", v.name)
			case "audio":
				audioCount++
				assert.Equal(t, "aac", st.CodecName, "%s audio codec should be aac", v.name)
			}
		}

		assert.Equal(t, 1, videoCount, "%s variant must contain exactly 1 video stream", v.name)
		assert.Equal(t, 1, audioCount, "%s variant must contain exactly 1 audio stream", v.name)
	}

	// =========================================================================
	// Level 3 Alignment Validation: Segment Count & Temporal Boundary Parity
	// =========================================================================
	highSegs, err := filepath.Glob(filepath.Join(outDir, "variant_high", "seg_*.ts"))
	require.NoError(t, err)
	lowSegs, err := filepath.Glob(filepath.Join(outDir, "variant_low", "seg_*.ts"))
	require.NoError(t, err)

	assert.NotEmpty(t, highSegs, "variant_high must contain generated .ts segments")
	assert.Equal(t, len(highSegs), len(lowSegs), "variant_high and variant_low must generate equal segment count (%d vs %d)", len(highSegs), len(lowSegs))

	for i := 0; i < len(highSegs); i++ {
		hInfo, err := os.Stat(highSegs[i])
		require.NoError(t, err)
		lInfo, err := os.Stat(lowSegs[i])
		require.NoError(t, err)

		assert.True(t, hInfo.Size() > 0, "segment %s size must be > 0", highSegs[i])
		assert.True(t, lInfo.Size() > 0, "segment %s size must be > 0", lowSegs[i])
	}
}
