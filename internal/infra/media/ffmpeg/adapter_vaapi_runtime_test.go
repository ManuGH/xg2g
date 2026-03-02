package ffmpeg

import (
	"io"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestIsVAAPIRuntimeErrorLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{
			name: "vaapi failure",
			line: "[h264_vaapi @ 0x123] Failed to end picture encode issue: 23 (internal encoding error).",
			want: true,
		},
		{
			name: "hwaccel error",
			line: "[AVHWDeviceContext @ 0xabc] hwaccel initialisation returned error.",
			want: true,
		},
		{
			name: "non-gpu error",
			line: "[hls @ 0xdef] error writing segment",
			want: false,
		},
		{
			name: "progress line",
			line: "out_time_ms=1234000",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isVAAPIRuntimeErrorLine(tt.line))
		})
	}
}

func TestRecordVAAPIRuntimeFailure_DemotesAdapterAfterThreshold(t *testing.T) {
	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderPreflight(map[string]bool{"h264_vaapi": true})
	t.Cleanup(func() {
		hardware.SetVAAPIPreflightResult(false)
		hardware.SetVAAPIEncoderPreflight(nil)
	})

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
		6,
		0,
		0,
		"/dev/dri/renderD128",
	)
	adapter.vaapiEncoders = map[string]bool{"h264_vaapi": true}

	adapter.recordVAAPIRuntimeFailure("sid-1", "[h264_vaapi] failed")
	adapter.recordVAAPIRuntimeFailure("sid-1", "[h264_vaapi] failed")
	assert.True(t, adapter.VaapiEncoderVerified("h264_vaapi"), "gpu must stay available before threshold")

	adapter.recordVAAPIRuntimeFailure("sid-1", "[h264_vaapi] failed")
	assert.False(t, adapter.VaapiEncoderVerified("h264_vaapi"), "adapter must clear encoder availability after demotion")
	assert.False(t, hardware.IsVAAPIReady(), "global hardware readiness must be demoted")
	assert.False(t, hardware.IsVAAPIEncoderReady("h264_vaapi"), "global encoder readiness must be demoted")
}
