package ffmpeg

import (
	"context"
	"errors"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
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

// "default" resolves to the highest-numbered mode the driver offers, and the
// numbering runs bob(1) < weave(2) < motion_adaptive(3) < motion_compensated(4).
// A driver with only bob and weave therefore gets weave, which interleaves both
// fields instead of deinterlacing and leaves combing in every 50p frame.
func TestBestVAAPIDeinterlaceMode_NeverWeave(t *testing.T) {
	hardware.SetVAAPIDeinterlaceModes(map[string]bool{
		"motion_compensated": false,
		"motion_adaptive":    false,
		"bob":                true,
	})
	t.Cleanup(func() { hardware.SetVAAPIDeinterlaceModes(nil) })

	assert.Equal(t, "bob", hardware.BestVAAPIDeinterlaceMode(vaapiDeinterlaceModePreference))
	assert.NotContains(t, vaapiDeinterlaceModePreference, "weave")
}

func TestBestVAAPIDeinterlaceMode_PrefersTheMostCapable(t *testing.T) {
	hardware.SetVAAPIDeinterlaceModes(map[string]bool{
		"motion_compensated": true,
		"motion_adaptive":    true,
		"bob":                true,
	})
	t.Cleanup(func() { hardware.SetVAAPIDeinterlaceModes(nil) })

	assert.Equal(t, "motion_compensated", hardware.BestVAAPIDeinterlaceMode(vaapiDeinterlaceModePreference))
}

// Unprobed keeps FFmpeg's own default, which is correct on a capable driver.
func TestBestVAAPIDeinterlaceMode_UnprobedKeepsTheFFmpegDefault(t *testing.T) {
	hardware.SetVAAPIDeinterlaceModes(nil)
	assert.Empty(t, hardware.BestVAAPIDeinterlaceMode(vaapiDeinterlaceModePreference))
}

func TestVaapiDeinterlaceFilter_CarriesModeAndFieldRate(t *testing.T) {
	hardware.SetVAAPIDeinterlaceModes(map[string]bool{"motion_adaptive": true})
	t.Cleanup(func() { hardware.SetVAAPIDeinterlaceModes(nil) })

	spec := ports.StreamSpec{Profile: ports.ProfileSpec{PolicyModeHint: ports.RuntimeModeHQ50}}
	assert.Equal(t, "deinterlace_vaapi=mode=motion_adaptive:rate=field", vaapiDeinterlaceFilter(spec))

	hq25 := ports.StreamSpec{Profile: ports.ProfileSpec{PolicyModeHint: ports.RuntimeModeHQ25}}
	assert.Equal(t, "deinterlace_vaapi=mode=motion_adaptive", vaapiDeinterlaceFilter(hq25))
}
