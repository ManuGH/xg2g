package ffmpeg

import (
	"io"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
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

func TestLocalAdapter_StartTimeoutForSpec_ExtendsSafariCPUTranscode(t *testing.T) {
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
		2,
		15*time.Second,
		30*time.Second,
		"",
	)

	spec := ports.StreamSpec{
		Source: ports.StreamSource{Type: ports.SourceTuner},
		Profile: ports.ProfileSpec{
			Name:           "safari",
			TranscodeVideo: true,
			VideoCodec:     "libx264",
			Container:      "fmp4",
		},
	}

	assert.Equal(t, 30*time.Second, adapter.startTimeoutForSpec(spec))

	spec.Profile.HWAccel = "vaapi"
	assert.Equal(t, 15*time.Second, adapter.startTimeoutForSpec(spec))
}

func TestLocalAdapter_StartTimeoutForProfile_ExtendsSafariHQ50Transcode(t *testing.T) {
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
		2,
		15*time.Second,
		30*time.Second,
		"",
	)

	profile := ports.ProfileSpec{
		Name:                 "safari_hq",
		TranscodeVideo:       true,
		VideoCodec:           "libx264",
		Container:            "mpegts",
		EffectiveRuntimeMode: ports.RuntimeModeHQ50,
	}

	assert.Equal(t, 60*time.Second, adapter.startTimeoutForProfile(ports.SourceTuner, profile))
}
