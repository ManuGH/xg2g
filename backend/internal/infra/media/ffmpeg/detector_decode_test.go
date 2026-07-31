package ffmpeg

import (
	"context"
	"errors"
	"testing"

	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func decodeDetector(t *testing.T, probe func(context.Context, string) error) *Detector {
	t.Helper()
	d := newDetector("ffmpeg", zerolog.Nop(), "/dev/dri/renderD128", t.TempDir(), AdapterConfig{})
	d.decodeProbeFn = probe
	hardware.SetVAAPIDecodeCapabilities(nil)
	t.Cleanup(func() { hardware.SetVAAPIDecodeCapabilities(nil) })
	return d
}

// A GPU that encodes is not automatically a GPU that decodes every input, and
// with -hwaccel_output_format vaapi pinned the difference is a dead session
// rather than a slower one.
func TestPreflightVAAPIDecode_RecordsPerCodecVerdicts(t *testing.T) {
	d := decodeDetector(t, func(_ context.Context, codec string) error {
		if codec == "mpeg2video" {
			return errors.New("no mpeg2 decode entrypoint")
		}
		return nil
	})

	d.PreflightVAAPIDecode()

	assert.True(t, hardware.VAAPIDecodeVerified("h264"))
	assert.True(t, hardware.VAAPIDecodeVerified("hevc"))
	assert.False(t, hardware.VAAPIDecodeVerified("mpeg2video"))
}

func TestVAAPIDecodeVerified_NormalisesCodecSpellings(t *testing.T) {
	hardware.SetVAAPIDecodeCapabilities(map[string]bool{"h264": true, "hevc": true})
	t.Cleanup(func() { hardware.SetVAAPIDecodeCapabilities(nil) })

	for _, spelling := range []string{"h264", "H264", " avc ", "avc1"} {
		assert.True(t, hardware.VAAPIDecodeVerified(spelling), spelling)
	}
	for _, spelling := range []string{"hevc", "h265", "hvc1"} {
		assert.True(t, hardware.VAAPIDecodeVerified(spelling), spelling)
	}
}

// An unscanned source has no proof, and that is exactly when guessing costs the
// viewer the stream.
func TestVAAPIDecodeVerified_UnknownAndUnprobedReadFalse(t *testing.T) {
	hardware.SetVAAPIDecodeCapabilities(nil)
	assert.False(t, hardware.VAAPIDecodeVerified("h264"))

	hardware.SetVAAPIDecodeCapabilities(map[string]bool{"h264": true})
	t.Cleanup(func() { hardware.SetVAAPIDecodeCapabilities(nil) })
	assert.False(t, hardware.VAAPIDecodeVerified(""))
	assert.False(t, hardware.VAAPIDecodeVerified("vp9"))
}

func TestPreflightVAAPIDecode_NoDeviceIsNotProbed(t *testing.T) {
	d := decodeDetector(t, func(context.Context, string) error {
		t.Fatal("probed without a VAAPI device")
		return nil
	})
	d.VaapiDevice = ""

	d.PreflightVAAPIDecode()
	assert.False(t, hardware.VAAPIDecodeVerified("h264"))
}
