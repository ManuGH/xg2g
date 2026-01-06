package v3

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildRemuxArgs_HEVC(t *testing.T) {
	// Test case 1: HEVC (unsupported in VOD) -> Transcode
	info := &StreamInfo{
		Video: VideoStreamInfo{
			CodecName: "hevc",
			PixFmt:    "yuv420p",
			BitDepth:  8,
		},
		Audio: AudioStreamInfo{
			CodecName:  "aac",
			SampleRate: 48000,
			Channels:   2,
		},
	}
	decision := buildRemuxArgs(info, "input.ts", "output.mp4", "1")

	assert.Equal(t, StrategyTranscode, decision.Strategy)
	assert.Contains(t, decision.Reason, "HEVC")
	assert.Contains(t, decision.Args, "-c:v")
	assert.Contains(t, decision.Args, "h264_vaapi")
}

func TestBuildRemuxArgs_H264_10bit(t *testing.T) {
	// Test case 2: H.264 10-bit -> Transcode
	info := &StreamInfo{Video: VideoStreamInfo{CodecName: "h264", PixFmt: "yuv420p10le", BitDepth: 10}}
	decision := buildRemuxArgs(info, "in", "out", "1")
	if decision.Strategy != StrategyTranscode {
		t.Errorf("expected Transcode for 10-bit, got %s", decision.Strategy)
	}
	assert.Contains(t, decision.Reason, "10-bit")
}

func TestBuildRemuxArgs_H264_8bit_AAC(t *testing.T) {
	// Test case 3: H.264 8-bit + AAC (Standard) -> Default (Copy)
	info := &StreamInfo{
		Video: VideoStreamInfo{CodecName: "h264", PixFmt: "yuv420p", BitDepth: 8},
		Audio: AudioStreamInfo{CodecName: "aac", SampleRate: 48000, Channels: 2},
	}
	decision := buildRemuxArgs(info, "in", "out", "1")
	if decision.Strategy != StrategyDefault {
		t.Errorf("expected Default for H.264 8-bit AAC, got %s", decision.Strategy)
	}
	assert.Contains(t, decision.Args, "-c:v")
	assert.Contains(t, decision.Args, "copy")
	assert.Contains(t, decision.Args, "-c:a")
	assert.Contains(t, decision.Args, "aac")
}

func TestBuildRemuxArgs_H264_AC3(t *testing.T) {
	// Test case 4: H.264 8-bit + AC3 -> Default (Copy video, transcode audio)
	info := &StreamInfo{
		Video: VideoStreamInfo{CodecName: "h264", PixFmt: "yuv420p", BitDepth: 8},
		Audio: AudioStreamInfo{CodecName: "ac3", SampleRate: 48000, Channels: 2},
	}
	decision := buildRemuxArgs(info, "in", "out", "1")
	if decision.Strategy != StrategyDefault {
		t.Errorf("expected Default for H.264 AC3, got %s", decision.Strategy)
	}
	assert.Contains(t, decision.Args, "-c:v")
	assert.Contains(t, decision.Args, "copy")
	assert.Contains(t, decision.Args, "-c:a")
	assert.Contains(t, decision.Args, "aac")
	// Policy: Audio always transcoded to AAC, but reason might say "Safe for Smart Copy" (video focus)
	assert.Contains(t, decision.Reason, "detected")
}

func TestBuildRemuxArgs_MPEG2(t *testing.T) {
	// Test case 5: MPEG2 -> Transcode
	info := &StreamInfo{
		Video: VideoStreamInfo{CodecName: "mpeg2video", PixFmt: "yuv420p"},
		Audio: AudioStreamInfo{CodecName: "mp2"},
	}
	decision := buildRemuxArgs(info, "in", "out", "1")
	if decision.Strategy != StrategyTranscode {
		t.Errorf("expected Transcode for MPEG2, got %s", decision.Strategy)
	}
	assert.Contains(t, decision.Reason, "MPEG2")
}

func TestClassifyRemuxError_NonMonotonousDTS(t *testing.T) {
	// Gate 3: Non-monotonous DTS pattern
	stderr := `[mp4 @ 0x...] Non-monotonous DTS in output stream 0:0; previous: 123, current: 122`

	err := classifyRemuxError(stderr, 1)

	assert.ErrorIs(t, err, ErrNonMonotonousDTS)
	assert.True(t, shouldRetryWithFallback(err))
}

func TestClassifyRemuxError_InvalidDuration(t *testing.T) {
	// Gate 3: Invalid duration pattern (HIGH SEVERITY - breaks Resume)
	stderr := `[mp4 @ 0x...] Packet with invalid duration -1 in stream 0`

	err := classifyRemuxError(stderr, 1)

	assert.ErrorIs(t, err, ErrInvalidDuration)
	// Should NOT retry with fallback (high severity)
	assert.False(t, shouldRetryWithFallback(err))
}

func TestClassifyRemuxError_TimestampUnset(t *testing.T) {
	// Gate 3: Timestamp unset pattern
	stderr := `[mp4 @ 0x...] timestamps are unset in a packet for stream 0`

	err := classifyRemuxError(stderr, 1)

	assert.ErrorIs(t, err, ErrTimestampUnset)
	assert.True(t, shouldRetryWithFallback(err))
}

func TestClassifyRemuxError_Success(t *testing.T) {
	// Exit code 0 = success
	stderr := `[some warnings that are OK]`

	err := classifyRemuxError(stderr, 0)

	assert.NoError(t, err)
}

func TestShouldRetryWithFallback_HighSeverity(t *testing.T) {
	// Invalid duration is HIGH severity (breaks Resume/Continue Watching)
	// Should NOT retry with fallback - must fail fast
	assert.False(t, shouldRetryWithFallback(ErrInvalidDuration))
}

func TestShouldRetryWithFallback_MediumSeverity(t *testing.T) {
	// Non-monotonous DTS can be fixed with fallback flags
	assert.True(t, shouldRetryWithFallback(ErrNonMonotonousDTS))
	assert.True(t, shouldRetryWithFallback(ErrTimestampUnset))
}

func TestBuildDefaultRemuxArgs_Structure(t *testing.T) {
	// Verify arg structure (will be replaced with Gate 1 exact command)
	args := buildDefaultRemuxArgs("input.ts", "output.mp4", true, "1", 0)

	// Should contain core remux elements
	assert.Contains(t, args, "-i")
	assert.Contains(t, args, "input.ts")
	assert.Contains(t, args, "output.mp4")
	assert.Contains(t, args, "-c:v")
	assert.Contains(t, args, "copy")
	assert.Contains(t, args, "-c:a")
	assert.Contains(t, args, "aac")
	assert.Contains(t, args, "-movflags")
	assert.Contains(t, args, "+faststart")
	assert.Contains(t, args, "-sn") // Strip subtitles
	assert.Contains(t, args, "-dn") // Strip data streams
}

func TestBuildTranscodeArgs_Structure(t *testing.T) {
	// Verify transcode arg structure (will be replaced with Gate 1 exact command)
	args := buildTranscodeArgs("input.ts", "output.mp4", "1", 0)

	// Should contain transcode elements
	assert.Contains(t, args, "-c:v")
	assert.Contains(t, args, "h264_vaapi")
	assert.Contains(t, args, "-filter:v")
	// Check for format=nv12,hwupload as part of the filter string
	foundHW := false
	for _, a := range args {
		if strings.Contains(a, "format=nv12,hwupload") {
			foundHW = true
			break
		}
	}
	assert.True(t, foundHW, "args should contain format=nv12,hwupload filter")
	assert.Contains(t, args, "-c:a")
	assert.Contains(t, args, "aac")
	assert.Contains(t, args, "-ss")
	assert.Contains(t, args, "1") // Start time cut
}

// TestRemuxDecisionLogging ensures we can log decisions for observability
func TestRemuxDecisionLogging(t *testing.T) {
	decision := &RemuxDecision{
		Strategy: StrategyDefault,
		Reason:   "H.264 8-bit detected",
		Args:     []string{"-c:v", "copy"},
	}

	// Should not panic
	assert.NotPanics(t, func() {
		logRemuxDecision(decision, "test-recording-id")
	})
}

// TestInferBitDepthFromPixFmt validates robust bit-depth detection
func TestInferBitDepthFromPixFmt(t *testing.T) {
	tests := []struct {
		pixFmt   string
		expected int
	}{
		// 8-bit formats (default)
		{"yuv420p", 8},
		{"yuv422p", 8},
		{"yuv444p", 8},
		{"nv12", 8},
		{"", 0}, // empty should return 0 (caller will default to 8)

		// 10-bit formats (critical for Chrome compatibility)
		{"yuv420p10le", 10},
		{"yuv420p10be", 10},
		{"yuv420p10", 10}, // no endianness suffix
		{"yuv422p10le", 10},
		{"yuv444p10le", 10},
		{"YUV420P10LE", 10}, // case insensitive

		// 12-bit formats
		{"yuv420p12le", 12},
		{"yuv422p12le", 12},

		// 16-bit formats
		{"yuv420p16le", 16},
		{"yuv444p16le", 16},
	}

	for _, tc := range tests {
		t.Run(tc.pixFmt, func(t *testing.T) {
			result := inferBitDepthFromPixFmt(tc.pixFmt)
			assert.Equal(t, tc.expected, result, "inferBitDepthFromPixFmt(%q) = %d, want %d", tc.pixFmt, result, tc.expected)
		})
	}
}

// TestTruncateForLog ensures we don't blow up logs with huge stderr
func TestTruncateForLog(t *testing.T) {
	short := "short message"
	assert.Equal(t, short, truncateForLog(short, 100))

	long := string(make([]byte, 1000))
	truncated := truncateForLog(long, 500)
	assert.Contains(t, truncated, "truncated")
	assert.Contains(t, truncated, "1000 bytes")
	assert.True(t, len(truncated) < 600) // Should be around 500 + message
}
