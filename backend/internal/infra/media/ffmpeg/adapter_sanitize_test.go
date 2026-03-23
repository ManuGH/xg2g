package ffmpeg

import (
	"testing"

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
