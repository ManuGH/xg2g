package ffmpeg

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rateControlDetector(t *testing.T, probe func(ctx context.Context, encoder, mode string) error) *Detector {
	t.Helper()
	d := newDetector("ffmpeg", zerolog.Nop(), "/dev/dri/renderD128", t.TempDir(), AdapterConfig{})
	d.vaapiDeviceChecked = true
	d.vaapiEncoders = map[string]bool{"av1_vaapi": true, "hevc_vaapi": true}
	d.vaapiEncoderCaps = map[string]hardware.VAAPIEncoderCapability{
		"av1_vaapi":  {Verified: true, AutoEligible: true, ProbeElapsed: 65 * time.Millisecond},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 59 * time.Millisecond},
	}
	d.rateControlProbeFn = probe
	hardware.SetVAAPIRateControlCapabilities(nil)
	t.Cleanup(func() { hardware.SetVAAPIRateControlCapabilities(nil) })
	return d
}

// The whole point of probing: what a driver accepts is measured, never inferred
// from the vendor. This is the Intel iHD shape - AV1 rejects QVBR, HEVC takes it.
func TestPreflightVAAPIRateControlModes_RecordsPerEncoderVerdicts(t *testing.T) {
	var asked [][2]string
	d := rateControlDetector(t, func(_ context.Context, encoder, mode string) error {
		asked = append(asked, [2]string{encoder, mode})
		if encoder == "av1_vaapi" {
			return errors.New("Driver does not support QVBR RC mode")
		}
		return nil
	})

	d.PreflightVAAPIRateControlModes()

	assert.False(t, hardware.VAAPIRateControlVerified("av1_vaapi", hardware.RateControlQVBR))
	assert.True(t, hardware.VAAPIRateControlVerified("hevc_vaapi", hardware.RateControlQVBR))
	assert.Equal(t, []string{hardware.RateControlQVBR}, hardware.VAAPIRateControlModes("hevc_vaapi"))
	assert.Empty(t, hardware.VAAPIRateControlModes("av1_vaapi"))
	require.Len(t, asked, 3, "only verified encoders and candidate modes are probed")
}

func TestPreflightVAAPIRateControlModes_SkipsUnverifiedEncoders(t *testing.T) {
	d := rateControlDetector(t, func(_ context.Context, encoder, _ string) error {
		if encoder == "h264_vaapi" {
			t.Fatal("probed an encoder the preflight never verified")
		}
		return nil
	})

	d.PreflightVAAPIRateControlModes()
	assert.False(t, hardware.VAAPIRateControlVerified("h264_vaapi", hardware.RateControlQVBR))
}

func TestPreflightVAAPIRateControlModes_RunsOnce(t *testing.T) {
	calls := 0
	d := rateControlDetector(t, func(context.Context, string, string) error {
		calls++
		return nil
	})

	d.PreflightVAAPIRateControlModes()
	d.PreflightVAAPIRateControlModes()
	assert.Equal(t, 3, calls, "second call must reuse the cached verdicts")
}

func TestPreflightVAAPIRateControlModes_NoDeviceIsNotProbed(t *testing.T) {
	d := rateControlDetector(t, func(context.Context, string, string) error {
		t.Fatal("probed without a VAAPI device")
		return nil
	})
	d.VaapiDevice = ""

	d.PreflightVAAPIRateControlModes()
	assert.False(t, hardware.VAAPIRateControlVerified("av1_vaapi", hardware.RateControlQVBR))
}

// Unprobed must never read as supported: the failure mode of a wrong "yes" is a
// stream that dies at encoder-open.
func TestVAAPIRateControlVerifiedIsFalseBeforeAnyProbe(t *testing.T) {
	hardware.SetVAAPIRateControlCapabilities(nil)
	assert.False(t, hardware.VAAPIRateControlVerified("av1_vaapi", hardware.RateControlQVBR))
	assert.False(t, hardware.VAAPIRateControlVerified("", ""))
}

// B-frames are a per-encoder fact like every other option: measured on an Intel
// AV1 encoder they cut 9.3% of the bits at identical quality, but whether a
// given driver honours -bf has to be proven, not assumed.
func TestPreflightVAAPIEncoderOptions_RecordsBFrameVerdicts(t *testing.T) {
	d := rateControlDetector(t, func(context.Context, string, string) error { return nil })
	hardware.SetVAAPIEncoderOptions(nil)
	t.Cleanup(func() { hardware.SetVAAPIEncoderOptions(nil) })
	d.encoderOptionProbeFn = func(_ context.Context, encoder, option, value string) error {
		if option != "-bf" || value == "" {
			t.Fatalf("unexpected option probe %q=%q", option, value)
		}
		if encoder == "av1_vaapi" {
			return errors.New("driver rejects frame reordering")
		}
		return nil
	}

	d.PreflightVAAPIRateControlModes()

	assert.False(t, hardware.VAAPIEncoderOptionVerified("av1_vaapi", hardware.OptionBFrames))
	assert.True(t, hardware.VAAPIEncoderOptionVerified("hevc_vaapi", hardware.OptionBFrames))
}

func TestAppendVaapiBFrameArgs_OnlyWhenProven(t *testing.T) {
	hardware.SetVAAPIEncoderOptions(nil)
	t.Cleanup(func() { hardware.SetVAAPIEncoderOptions(nil) })
	assert.NotContains(t, appendVaapiBFrameArgs(nil, "av1"), "-bf", "unprobed must not request reordering")

	hardware.SetVAAPIEncoderOptions(map[string]map[string]bool{
		"av1_vaapi": {hardware.OptionBFrames: true},
	})
	assert.Contains(t, appendVaapiBFrameArgs(nil, "av1"), "-bf")

	hardware.SetVAAPIEncoderOptions(map[string]map[string]bool{
		"av1_vaapi": {hardware.OptionBFrames: false},
	})
	assert.NotContains(t, appendVaapiBFrameArgs(nil, "av1"), "-bf", "a rejected option must not be emitted")
}
