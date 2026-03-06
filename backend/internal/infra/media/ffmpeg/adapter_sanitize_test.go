package ffmpeg

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeFFmpegLogLine_RemovesCredentialsFromEmbeddedURL(t *testing.T) {
	line := "Input #0, mpegts, from 'http://root:Kiddy99@10.10.55.64:17999/1:0:19:132F:3EF:1:C00000:0:0:0':"

	sanitized := sanitizeFFmpegLogLine(line)

	assert.Equal(t, "Input #0, mpegts, from 'http://10.10.55.64:17999/1:0:19:132F:3EF:1:C00000:0:0:0':", sanitized)
}
