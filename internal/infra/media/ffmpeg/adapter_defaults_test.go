package ffmpeg

import (
	"io"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestNewLocalAdapter_DefaultSegmentSecondsMatchesRegistry(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg",
		"",
		t.TempDir(),
		nil,
		zerolog.New(io.Discard),
		"",
		"",
		0,
		0,
		false,
		2*time.Second,
		0, // unset
		0,
		0,
		"",
	)

	assert.Equal(t, config.DefaultHLSSegmentSeconds, adapter.SegmentSeconds)
}
