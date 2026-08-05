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

	// Ensure no separate external audio group in master playlist
	// Note: FFmpeg's HLS muxer emits #EXT-X-INDEPENDENT-SEGMENTS in each variant media playlist (verified below), while master index.m3u8 omits it.
	assert.NotContains(t, masterContent, "#EXT-X-MEDIA:TYPE=AUDIO", "Master playlist must not contain external audio group tags")

	// Block-wise Master Playlist Parsing: Map #EXT-X-STREAM-INF attributes directly to following variant URI
	type variantStreamInf struct {
		bandwidth  int
		resolution string
		codecs     string
	}
	variantBlocks := make(map[string]variantStreamInf)

	lines := strings.Split(masterContent, "\n")
	var lastStreamInf string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			lastStreamInf = line
		} else if line != "" && !strings.HasPrefix(line, "#") && lastStreamInf != "" {
			var info variantStreamInf
			if idx := strings.Index(lastStreamInf, "BANDWIDTH="); idx >= 0 {
				rest := lastStreamInf[idx+len("BANDWIDTH="):]
				if endIdx := strings.IndexAny(rest, ", \r\n"); endIdx >= 0 {
					rest = rest[:endIdx]
				}
				info.bandwidth, _ = strconv.Atoi(rest)
			}
			if idx := strings.Index(lastStreamInf, "RESOLUTION="); idx >= 0 {
				rest := lastStreamInf[idx+len("RESOLUTION="):]
				if endIdx := strings.IndexAny(rest, ", \r\n"); endIdx >= 0 {
					rest = rest[:endIdx]
				}
				info.resolution = rest
			}
			if idx := strings.Index(lastStreamInf, "CODECS="); idx >= 0 {
				rest := lastStreamInf[idx+len("CODECS="):]
				if strings.HasPrefix(rest, `"`) {
					rest = rest[1:]
					if endIdx := strings.Index(rest, `"`); endIdx >= 0 {
						rest = rest[:endIdx]
					}
				} else if endIdx := strings.IndexAny(rest, ", \r\n"); endIdx >= 0 {
					rest = rest[:endIdx]
				}
				info.codecs = rest
			}
			variantBlocks[line] = info
			lastStreamInf = ""
		}
	}

	require.Contains(t, variantBlocks, "variant_high/index.m3u8", "master playlist must map block to variant_high/index.m3u8")
	require.Contains(t, variantBlocks, "variant_low/index.m3u8", "master playlist must map block to variant_low/index.m3u8")

	highBlock := variantBlocks["variant_high/index.m3u8"]
	lowBlock := variantBlocks["variant_low/index.m3u8"]

	assert.Equal(t, "1280x720", highBlock.resolution, "high variant block resolution mismatch")
	assert.Equal(t, "640x360", lowBlock.resolution, "low variant block resolution mismatch")

	assert.Contains(t, highBlock.codecs, "avc1", "high variant codecs must include avc1")
	assert.Contains(t, highBlock.codecs, "mp4a.40.2", "high variant codecs must include mp4a.40.2")
	assert.Contains(t, lowBlock.codecs, "avc1", "low variant codecs must include avc1")
	assert.Contains(t, lowBlock.codecs, "mp4a.40.2", "low variant codecs must include mp4a.40.2")

	assert.True(t, highBlock.bandwidth >= 600000 && highBlock.bandwidth <= 2500000, "high variant bandwidth out of range: %d", highBlock.bandwidth)
	assert.True(t, lowBlock.bandwidth >= 200000 && lowBlock.bandwidth <= 1000000, "low variant bandwidth out of range: %d", lowBlock.bandwidth)
	assert.True(t, highBlock.bandwidth > lowBlock.bandwidth, "high variant bandwidth (%d) must exceed low variant bandwidth (%d)", highBlock.bandwidth, lowBlock.bandwidth)

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

	for _, v := range variants {
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
		bw := variantBlocks[v.relPath].bandwidth
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
	require.Equal(t, len(highDurs), len(lowDurs), "High and low variant playlists must contain equal number of #EXTINF segments")

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
	require.Equal(t, len(highSegs), len(lowSegs), "variant_high and variant_low must generate equal segment count (%d vs %d)", len(highSegs), len(lowSegs))

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

		// Assert PTS alignment of first video packet across variants strictly
		hPts, errH := strconv.ParseFloat(hFirst.PtsTime, 64)
		require.NoError(t, errH, "high segment %s must expose valid first-video PTS (got %q)", hPath, hFirst.PtsTime)
		lPts, errL := strconv.ParseFloat(lFirst.PtsTime, 64)
		require.NoError(t, errL, "low segment %s must expose valid first-video PTS (got %q)", lPath, lFirst.PtsTime)

		ptsDiff := math.Abs(hPts - lPts)
		require.True(t, ptsDiff < 0.05, "Segment %d start PTS drift between high (%f) and low (%f) exceeds 0.05s", i, hPts, lPts)
	}
}
