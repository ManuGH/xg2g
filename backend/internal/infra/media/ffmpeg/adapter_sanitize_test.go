package ffmpeg

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestSanitizeFFmpegLogLine_RemovesCredentialsFromEmbeddedURL(t *testing.T) {
	line := "Input #0, mpegts, from 'http://root:Kiddy99@10.10.55.64:17999/1:0:19:132F:3EF:1:C00000:0:0:0':"

	sanitized := sanitizeFFmpegLogLine(line)

	assert.Equal(t, "Input #0, mpegts, from 'http://10.10.55.64:17999/1:0:19:132F:3EF:1:C00000:0:0:0':", sanitized)
}

func TestFFmpegLogLevel_ClassifiesProgressAsDebug(t *testing.T) {
	level := ffmpegLogLevel("frame=6472 fps=51.35 bitrate=N/A speed=1.03x")
	assert.Equal(t, zerolog.DebugLevel, level)
}

func TestFFmpegLogLevel_ClassifiesSegmentWritesAsDebug(t *testing.T) {
	level := ffmpegLogLevel("[hls @ 0x123] Opening '/var/lib/xg2g/hls/sessions/sid/seg_000020.ts' for writing")
	assert.Equal(t, zerolog.DebugLevel, level)
}

func TestFFmpegLogLevel_ClassifiesFailuresAsWarn(t *testing.T) {
	level := ffmpegLogLevel("[h264 @ 0x123] non-existing PPS 0 referenced")
	assert.Equal(t, zerolog.WarnLevel, level)
}

func TestFFmpegLogLevel_ClassifiesPreambleAsInfo(t *testing.T) {
	level := ffmpegLogLevel("Input #0, mpegts, from 'http://10.10.55.64:17999/stream':")
	assert.Equal(t, zerolog.InfoLevel, level)
}

func TestSummarizeFFmpegFailureLine_ClassifiesMissingCodecParamsHeaderFailure(t *testing.T) {
	detail := summarizeFFmpegFailureLine("[out#0/hls @ 0x9cec31200] Could not write header (incorrect codec parameters ?): Invalid argument")
	assert.Equal(t, "copy output missing codec parameters", detail)
}

func TestSummarizeFFmpegFailureLine_ClassifiesMissingCodecParamsFromPPS(t *testing.T) {
	detail := summarizeFFmpegFailureLine("[h264 @ 0x123] non-existing PPS 0 referenced")
	assert.Equal(t, "copy output missing codec parameters", detail)
}

func TestSummarizeFFmpegFailureLine_ClassifiesMissingCodecParamsFromUnspecifiedSize(t *testing.T) {
	detail := summarizeFFmpegFailureLine("[mpegts @ 0x123] Could not find codec parameters for stream 1 (Video: h264, none): unspecified size")
	assert.Equal(t, "copy output missing codec parameters", detail)
}

func TestProcessDetailPriority_PrefersCodecParamsOverPrematureStreamEnd(t *testing.T) {
	assert.Greater(t,
		processDetailPriority("copy output missing codec parameters"),
		processDetailPriority("upstream stream ended prematurely"),
	)
}

func TestRecordRuntimeDiagnostics_ParsesProgressAndSourceWarnings(t *testing.T) {
	adapter := &LocalAdapter{runtimeDiagnostics: make(map[ports.RunHandle]ports.RuntimeDiagnostics)}
	handle := ports.RunHandle("session-1-123")

	adapter.recordRuntimeDiagnostics(handle, "frame=6472 fps=51.35 drop_frames=0 dup_frames=52 speed=1.03x", "")
	adapter.recordRuntimeDiagnostics(handle, "[mpegts @ 0x123] corrupt decoded frame in stream 0", "[mpegts @ 0x123] corrupt decoded frame in stream 0")

	status := adapter.Health(nil, handle)
	assert.False(t, status.Healthy)
	assert.Equal(t, 6472, status.Diagnostics.FrameCount)
	assert.Equal(t, 51.35, status.Diagnostics.FPS)
	assert.Equal(t, 0, status.Diagnostics.DropFrames)
	assert.Equal(t, 52, status.Diagnostics.DupFrames)
	assert.Equal(t, 1.03, status.Diagnostics.Speed)
	assert.Equal(t, 1, status.Diagnostics.CorruptDecodedFrames)
	assert.Contains(t, status.Diagnostics.LastWarning, "corrupt decoded frame")
}
