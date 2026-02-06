package ffmpeg

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildArgs_UsesOptionalVideoMap(t *testing.T) {
	adapter := NewLocalAdapter(
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
		6,
		0,
		0,
	)

	spec := ports.StreamSpec{
		SessionID: "sid-1",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityStandard,
		Source: ports.StreamSource{
			ID:   "http://example.com/stream",
			Type: ports.SourceURL,
		},
	}

	args, err := adapter.buildArgs(context.Background(), spec, spec.Source.ID)
	require.NoError(t, err)
	assert.Contains(t, args, "0:v:0?", "video map should be optional for audio-only inputs")
	assert.Contains(t, args, "0:a:0?", "audio map should remain optional")
}
