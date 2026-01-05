package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildRemuxArgs_HEVC(t *testing.T) {
	// HEVC is Chrome-incompatible (Gate 4 decision)
	// Should select transcode strategy
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

	decision := buildRemuxArgs(info, "input.ts", "output.mp4")

	assert.Equal(t, StrategyTranscode, decision.Strategy)
	assert.Contains(t, decision.Reason, "HEVC")
	assert.Contains(t, decision.Args, "-c:v")
	assert.Contains(t, decision.Args, "libx264")
}

func TestBuildRemuxArgs_H264_10bit(t *testing.T) {
	// 10-bit H.264 is Chrome-incompatible (Gate 2 + Gate 4)
	// Should select transcode strategy
	info := &StreamInfo{
		Video: VideoStreamInfo{
			CodecName: "h264",
			PixFmt:    "yuv420p10le",
			BitDepth:  10,
		},
		Audio: AudioStreamInfo{
			CodecName:  "aac",
			SampleRate: 48000,
			Channels:   2,
		},
	}

	decision := buildRemuxArgs(info, "input.ts", "output.mp4")

	assert.Equal(t, StrategyTranscode, decision.Strategy)
	assert.Contains(t, decision.Reason, "10-bit")
}

func TestBuildRemuxArgs_H264_8bit_AAC(t *testing.T) {
	// H.264 8-bit + AAC: Happy path
	// Should select default remux (copy video, transcode audio for safety)
	info := &StreamInfo{
		Video: VideoStreamInfo{
			CodecName: "h264",
			PixFmt:    "yuv420p",
			BitDepth:  8,
		},
		Audio: AudioStreamInfo{
			CodecName:  "aac",
			SampleRate: 48000,
			Channels:   2,
		},
	}

	decision := buildRemuxArgs(info, "input.ts", "output.mp4")

	assert.Equal(t, StrategyDefault, decision.Strategy)
	assert.Contains(t, decision.Args, "-c:v")
	assert.Contains(t, decision.Args, "copy")
	// Audio should be transcoded (current policy)
	assert.Contains(t, decision.Args, "-c:a")
	assert.Contains(t, decision.Args, "aac")
}

func TestBuildRemuxArgs_H264_AC3(t *testing.T) {
	// H.264 8-bit + AC3: Audio needs transcode (Chrome incompatible)
	info := &StreamInfo{
		Video: VideoStreamInfo{
			CodecName: "h264",
			PixFmt:    "yuv420p",
			BitDepth:  8,
		},
		Audio: AudioStreamInfo{
			CodecName:  "ac3",
			SampleRate: 48000,
			Channels:   2,
		},
	}

	decision := buildRemuxArgs(info, "input.ts", "output.mp4")

	assert.Equal(t, StrategyDefault, decision.Strategy)
	assert.Contains(t, decision.Args, "-c:v")
	assert.Contains(t, decision.Args, "copy")
	assert.Contains(t, decision.Args, "-c:a")
	assert.Contains(t, decision.Args, "aac")
	assert.Contains(t, decision.Reason, "transcode")
}

func TestBuildRemuxArgs_MPEG2(t *testing.T) {
	// MPEG2: Browser compatibility concern (Gate 4 decision)
	info := &StreamInfo{
		Video: VideoStreamInfo{
			CodecName: "mpeg2video",
			PixFmt:    "yuv420p",
		},
		Audio: AudioStreamInfo{
			CodecName: "mp2",
		},
	}

	decision := buildRemuxArgs(info, "input.ts", "output.mp4")

	assert.Equal(t, StrategyTranscode, decision.Strategy)
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
	args := buildDefaultRemuxArgs("input.ts", "output.mp4", true)

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
	args := buildTranscodeArgs("input.ts", "output.mp4")

	// Should contain transcode elements
	assert.Contains(t, args, "-c:v")
	assert.Contains(t, args, "libx264")
	assert.Contains(t, args, "-pix_fmt")
	assert.Contains(t, args, "yuv420p") // Force 8-bit
	assert.Contains(t, args, "-c:a")
	assert.Contains(t, args, "aac")
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
