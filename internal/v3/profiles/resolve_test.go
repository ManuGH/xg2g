package profiles

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/v3/scan"
	"github.com/stretchr/testify/assert"
)

func TestResolve_AutoSafari(t *testing.T) {
	// Real UA from curl log
	safariUA := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15"

	// Existing behavior (no cap, no GPU, auto hwaccel)
	spec := Resolve("", safariUA, 0, nil, false, HWAccelAuto)
	assert.Equal(t, "safari", spec.Name)

	spec2 := Resolve("auto", safariUA, 0, nil, false, HWAccelAuto)
	assert.Equal(t, "safari", spec2.Name)
}

func TestResolve_SmartScan(t *testing.T) {
	safariUA := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15"

	// 1. Progressive -> Copy (Direct Remux)
	// Even if GPU is available, progressive should use Copy for efficiency/quality
	progCap := &scan.Capability{Interlaced: false}
	specProg := Resolve("auto", safariUA, 0, progCap, true, HWAccelAuto)
	assert.Equal(t, false, specProg.TranscodeVideo, "Progressive should assume safe for copy")
	assert.Equal(t, "fmp4", specProg.Container)
	assert.Equal(t, 192, specProg.AudioBitrateK, "Audio should be normalized for Safari")

	// 2. Interlaced + GPU -> Transcode VAAPI
	interCap := &scan.Capability{Interlaced: true}
	specGPU := Resolve("auto", safariUA, 0, interCap, true, HWAccelAuto)
	assert.Equal(t, true, specGPU.TranscodeVideo, "Interlaced should force transcode")
	assert.Equal(t, true, specGPU.Deinterlace)
	assert.Equal(t, "vaapi", specGPU.HWAccel)
	assert.Equal(t, "h264", specGPU.VideoCodec)
	assert.Equal(t, 16, specGPU.VideoCRF)

	// 3. Interlaced + No GPU -> Transcode CPU
	specCPU := Resolve("auto", safariUA, 0, interCap, false, HWAccelAuto)
	assert.Equal(t, true, specCPU.TranscodeVideo)
	assert.Equal(t, true, specCPU.Deinterlace)
	assert.Equal(t, "", specCPU.HWAccel)
	assert.Equal(t, "libx264", specCPU.VideoCodec)
	assert.Equal(t, "veryfast", specCPU.Preset)
	assert.Equal(t, 18, specCPU.VideoCRF)
}

func TestResolve_UnknownCap(t *testing.T) {
	safariUA := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15"

	// Unknown capability -> Conservative Interlaced assumption (Safety first)
	specUnknown := Resolve("auto", safariUA, 0, nil, true, HWAccelAuto)
	assert.Equal(t, true, specUnknown.TranscodeVideo)
	assert.Equal(t, true, specUnknown.Deinterlace)
}
