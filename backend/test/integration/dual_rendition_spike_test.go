package test

import (
	"encoding/json"
	"math"
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

type FFprobePacket struct {
	PtsTime string `json:"pts_time"`
	DtsTime string `json:"dts_time"`
	Flags   string `json:"flags"`
}

type FFprobeOutput struct {
	Streams []FFprobeStream `json:"streams"`
	Packets []FFprobePacket `json:"packets"`
}

func parseExtinfDurations(content string) ([]float64, error) {
	var durations []float64
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#EXTINF:") {
			durStr := strings.TrimPrefix(line, "#EXTINF:")
			durStr = strings.TrimSuffix(durStr, ",")
			dur, err := strconv.ParseFloat(durStr, 64)
			if err != nil {
				return nil, err
			}
			durations = append(durations, dur)
		}
	}
	return durations, nil
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

	// Check CODECS attribute in Master Playlist
	assert.Contains(t, masterContent, "CODECS=", "Master playlist must specify CODECS attribute in #EXT-X-STREAM-INF")
	assert.Contains(t, masterContent, "avc1", "Master playlist CODECS attribute must include H.264 (avc1)")
	assert.Contains(t, masterContent, "mp4a.40.2", "Master playlist CODECS attribute must include AAC (mp4a.40.2)")

	// Ensure no separate external audio group
	assert.NotContains(t, masterContent, "#EXT-X-MEDIA:TYPE=AUDIO", "Master playlist must not contain external audio group tags")

	// Parse bandwidths and verify distinctness and plausible range bounds
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

	// Enforce plausible bandwidth bounds
	highBw := bandwidths[0]
	lowBw := bandwidths[1]
	assert.True(t, highBw >= 600000 && highBw <= 2500000, "High variant bandwidth (%d) out of plausible range [600k, 2.5M]", highBw)
	assert.True(t, lowBw >= 200000 && lowBw <= 1000000, "Low variant bandwidth (%d) out of plausible range [200k, 1M]", lowBw)

	// =========================================================================
	// Level 3 Validation: ffprobe Per-Variant Stream Inspection
	// =========================================================================
	variants := []struct {
		name      string
		relPath   string
		expWidth  int
		expHeight int
		minBw     int
		maxBw     int
	}{
		{name: "high", relPath: "variant_high/index.m3u8", expWidth: 1280, expHeight: 720, minBw: 600000, maxBw: 2500000},
		{name: "low", relPath: "variant_low/index.m3u8", expWidth: 640, expHeight: 360, minBw: 200000, maxBw: 1000000},
	}

	var variantDurations [][]float64

	for idx, v := range variants {
		variantPlPath := filepath.Join(outDir, v.relPath)
		plBytes, err := os.ReadFile(variantPlPath)
		require.NoError(t, err, "Variant playlist %s must exist", v.relPath)
		plContent := string(plBytes)

		// Assert independent segments tag
		assert.Contains(t, plContent, "#EXT-X-INDEPENDENT-SEGMENTS", "Variant playlist %s must include #EXT-X-INDEPENDENT-SEGMENTS", v.relPath)

		// Parse #EXTINF durations
		durs, err := parseExtinfDurations(plContent)
		require.NoError(t, err, "Failed to parse #EXTINF durations for %s", v.relPath)
		require.NotEmpty(t, durs, "Variant playlist %s must have segment #EXTINF durations", v.relPath)
		variantDurations = append(variantDurations, durs)

		// Assert bandwidth is within configured variant bounds
		bw := bandwidths[idx]
		assert.True(t, bw >= v.minBw && bw <= v.maxBw, "Variant %s bandwidth %d out of bounds [%d, %d]", v.name, bw, v.minBw, v.maxBw)

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

	// Compare #EXTINF segment durations across high and low variants
	require.Len(t, variantDurations, 2)
	highDurs := variantDurations[0]
	lowDurs := variantDurations[1]
	assert.Equal(t, len(highDurs), len(lowDurs), "High and low variant playlists must contain equal number of #EXTINF segments")

	for i := 0; i < len(highDurs); i++ {
		durDiff := math.Abs(highDurs[i] - lowDurs[i])
		assert.True(t, durDiff < 0.05, "Segment %d duration drift between high (%f) and low (%f) exceeds 0.05s", i, highDurs[i], lowDurs[i])
	}

	// =========================================================================
	// Level 3 Alignment Validation: Segment Keyframe (K-Flag) & PTS Alignment
	// =========================================================================
	highSegs, err := filepath.Glob(filepath.Join(outDir, "variant_high", "seg_*.ts"))
	require.NoError(t, err)
	lowSegs, err := filepath.Glob(filepath.Join(outDir, "variant_low", "seg_*.ts"))
	require.NoError(t, err)

	assert.NotEmpty(t, highSegs, "variant_high must contain generated .ts segments")
	assert.Equal(t, len(highSegs), len(lowSegs), "variant_high and variant_low must generate equal segment count (%d vs %d)", len(highSegs), len(lowSegs))

	for i := 0; i < len(highSegs); i++ {
		hPath := highSegs[i]
		lPath := lowSegs[i]

		hInfo, err := os.Stat(hPath)
		require.NoError(t, err)
		lInfo, err := os.Stat(lPath)
		require.NoError(t, err)

		assert.True(t, hInfo.Size() > 0, "segment %s size must be > 0", hPath)
		assert.True(t, lInfo.Size() > 0, "segment %s size must be > 0", lPath)

		// Inspect video packets of high segment using ffprobe
		hProbeCmd := exec.Command(ffprobePath, "-v", "error", "-select_streams", "v:0", "-show_packets", "-show_entries", "packet=pts_time,flags", "-of", "json", hPath)
		hOut, err := hProbeCmd.CombinedOutput()
		require.NoError(t, err, "ffprobe packet inspection failed for %s: %s", hPath, string(hOut))
		var hProbe FFprobeOutput
		require.NoError(t, json.Unmarshal(hOut, &hProbe))

		// Inspect video packets of low segment using ffprobe
		lProbeCmd := exec.Command(ffprobePath, "-v", "error", "-select_streams", "v:0", "-show_packets", "-show_entries", "packet=pts_time,flags", "-of", "json", lPath)
		lOut, err := lProbeCmd.CombinedOutput()
		require.NoError(t, err, "ffprobe packet inspection failed for %s: %s", lPath, string(lOut))
		var lProbe FFprobeOutput
		require.NoError(t, json.Unmarshal(lOut, &lProbe))

		// Assert first video packet in both segments has Keyframe flag ("K")
		require.NotEmpty(t, hProbe.Packets, "high segment %s must contain video packets", hPath)
		require.NotEmpty(t, lProbe.Packets, "low segment %s must contain video packets", lPath)

		hFirst := hProbe.Packets[0]
		lFirst := lProbe.Packets[0]

		assert.Contains(t, hFirst.Flags, "K", "high segment %s first video packet must be a Keyframe (flags=%s)", hPath, hFirst.Flags)
		assert.Contains(t, lFirst.Flags, "K", "low segment %s first video packet must be a Keyframe (flags=%s)", lPath, lFirst.Flags)

		// Assert PTS alignment of first video packet across variants
		hPts, errH := strconv.ParseFloat(hFirst.PtsTime, 64)
		lPts, errL := strconv.ParseFloat(lFirst.PtsTime, 64)
		if errH == nil && errL == nil {
			ptsDiff := math.Abs(hPts - lPts)
			assert.True(t, ptsDiff < 0.05, "Segment %d start PTS drift between high (%f) and low (%f) exceeds 0.05s", i, hPts, lPts)
		}
	}
}
