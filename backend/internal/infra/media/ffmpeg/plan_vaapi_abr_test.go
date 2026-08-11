package ffmpeg

import (
	"context"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestABR_VAAPI_3Tier_Plan(t *testing.T) {
	adapter := newABRTestAdapter(t)
	adapter.VaapiDevice = "/dev/dri/renderD128"
	adapter.detector.vaapiEncoders = map[string]bool{"h264_vaapi": true}

	spec := ports.StreamSpec{
		SessionID: "sess-vaapi-abr-3tier",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Source:    ports.StreamSource{Type: ports.SourceURL, ID: "stream1"},
		Profile: ports.ProfileSpec{
			Name:              "hq25",
			TranscodeVideo:    true,
			VideoCodec:        "h264",
			VideoSourceHeight: 1080,
			EnableABR:         true,
			HWAccel:           "vaapi",
		},
	}

	plan, err := adapter.buildArgsWithPlan(context.Background(), spec, "http://localhost:8080/stream")
	require.NoError(t, err)
	assert.Equal(t, "master.m3u8", plan.primaryPlaylist)

	cmdStr := strings.Join(plan.args, " ")
	assert.Contains(t, cmdStr, "-vaapi_device /dev/dri/renderD128")
	assert.Contains(t, cmdStr, "-filter_complex [0:v:0]format=nv12,hwupload[v_gpu]; [v_gpu]split=3[v1080][v720in][v480in]; [v720in]scale_vaapi=w=1280:h=720[v720]; [v480in]scale_vaapi=w=854:h=480[v480]; [0:a:0?]asplit=3[a1080][a720][a480]")
	assert.Contains(t, cmdStr, "-c:v:0 h264_vaapi -b:v:0 4500k -maxrate:v:0 5200k")
	assert.Contains(t, cmdStr, "-c:v:1 h264_vaapi -b:v:1 2000k -maxrate:v:1 2400k")
	assert.Contains(t, cmdStr, "-c:v:2 h264_vaapi -b:v:2 900k -maxrate:v:2 1100k")
	assert.Contains(t, cmdStr, "v:0,a:0,name:1080p v:1,a:1,name:720p v:2,a:2,name:480p")
}

func TestABR_VAAPI_2Tier_Deinterlace_Plan(t *testing.T) {
	adapter := newABRTestAdapter(t)
	adapter.VaapiDevice = "/dev/dri/renderD128"
	adapter.detector.vaapiEncoders = map[string]bool{"h264_vaapi": true}

	spec := ports.StreamSpec{
		SessionID: "sess-vaapi-abr-2tier-deint",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Source:    ports.StreamSource{Type: ports.SourceURL, ID: "stream1"},
		Profile: ports.ProfileSpec{
			Name:              "hq25",
			TranscodeVideo:    true,
			VideoCodec:        "h264",
			VideoSourceHeight: 720,
			Deinterlace:       true,
			EnableABR:         true,
			HWAccel:           "vaapi",
		},
	}

	plan, err := adapter.buildArgsWithPlan(context.Background(), spec, "http://localhost:8080/stream")
	require.NoError(t, err)
	assert.Equal(t, "master.m3u8", plan.primaryPlaylist)

	cmdStr := strings.Join(plan.args, " ")
	assert.Contains(t, cmdStr, "-vaapi_device /dev/dri/renderD128")
	assert.Contains(t, cmdStr, "deinterlace_vaapi")
	assert.Contains(t, cmdStr, "split=2[v720in][v480in]")
	assert.Contains(t, cmdStr, "scale_vaapi=w=854:h=480[v480]")
	assert.Contains(t, cmdStr, "-c:v:0 h264_vaapi")
	assert.Contains(t, cmdStr, "-c:v:1 h264_vaapi")
	assert.False(t, strings.Contains(cmdStr, "1080p"))
}

func TestABR_UnverifiedVAAPI_FallsBackToCPU(t *testing.T) {
	adapter := newABRTestAdapter(t)
	adapter.VaapiDevice = "" // No device available or preflight rejected

	spec := ports.StreamSpec{
		SessionID: "sess-vaapi-fallback-cpu",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Source:    ports.StreamSource{Type: ports.SourceURL, ID: "stream1"},
		Profile: ports.ProfileSpec{
			Name:              "hq25",
			TranscodeVideo:    true,
			VideoCodec:        "h264",
			VideoSourceHeight: 1080,
			EnableABR:         true,
			HWAccel:           "auto", // Auto hardware negotiation
		},
	}

	plan, err := adapter.buildArgsWithPlan(context.Background(), spec, "http://localhost:8080/stream")
	require.NoError(t, err)
	assert.Equal(t, "master.m3u8", plan.primaryPlaylist)

	cmdStr := strings.Join(plan.args, " ")
	// Must fallback to CPU libx264
	assert.Contains(t, cmdStr, "-c:v:0 libx264")
	assert.Contains(t, cmdStr, "-preset ultrafast")
	assert.False(t, strings.Contains(cmdStr, "-vaapi_device"))
}
