// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package playbackinfo

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/problemcode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRecordingPlaybackPostInput_Success(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v3/recordings/rec1/stream-info", strings.NewReader(`{
		"capabilitiesVersion":2,
		"container":["mp4","hls"],
		"videoCodecs":["h264"],
		"audioCodecs":["aac"],
		"supportsHls":true,
		"deviceContext":{
			"brand":"google",
			"product":"mdarcy",
			"device":"foster",
			"platform":"android-tv",
			"manufacturer":"NVIDIA",
			"model":"Shield",
			"osName":"Android",
			"osVersion":"14",
			"sdkInt":34
		},
		"networkContext":{
			"kind":"ethernet",
			"downlinkKbps":940000,
			"metered":false,
			"internetValidated":true
		}
	}`))

	caps, problem := ParseRecordingPlaybackPostInput(req)

	require.Nil(t, problem)
	require.NotNil(t, caps)
	assert.Equal(t, 2, caps.CapabilitiesVersion)
	assert.Equal(t, []string{"mp4", "hls"}, caps.Container)
	assert.Equal(t, []string{"h264"}, caps.VideoCodecs)
	assert.Equal(t, []string{"aac"}, caps.AudioCodecs)
	require.NotNil(t, caps.SupportsHls)
	assert.True(t, *caps.SupportsHls)
	require.NotNil(t, caps.DeviceContext)
	assert.Equal(t, "google", *caps.DeviceContext.Brand)
	assert.Equal(t, "mdarcy", *caps.DeviceContext.Product)
	assert.Equal(t, "foster", *caps.DeviceContext.Device)
	assert.Equal(t, "android-tv", *caps.DeviceContext.Platform)
	require.NotNil(t, caps.NetworkContext)
	assert.Equal(t, "ethernet", *caps.NetworkContext.Kind)
}

func TestParseRecordingPlaybackPostInput_InvalidCapabilitiesVersion(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v3/recordings/rec1/stream-info", strings.NewReader(`{
		"capabilitiesVersion":0,
		"container":["mp4"],
		"videoCodecs":["h264"],
		"audioCodecs":["aac"]
	}`))

	caps, problem := ParseRecordingPlaybackPostInput(req)

	assert.Nil(t, caps)
	require.NotNil(t, problem)
	assert.Equal(t, http.StatusBadRequest, problem.Status)
	assert.Equal(t, "recordings/invalid", problem.ProblemType)
	assert.Equal(t, problemcode.CodeInvalidCapabilities, problem.Code)
	assert.Equal(t, "capabilities_version must be >= 1", problem.Detail)
}

func TestParseLivePlaybackPostInput_NormalizesServiceRef(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(`{
		"serviceRef":" 1:0:1:1234:5678:9abc:0:0:0:0: ",
		"capabilities":{
			"capabilitiesVersion":2,
			"container":["mpegts"],
			"videoCodecs":["h264"],
			"audioCodecs":["aac"],
			"deviceContext":{
				"platform":"browser",
				"osName":"windows",
				"osVersion":"11"
			}
		}
	}`))

	input, problem := ParseLivePlaybackPostInput(req)

	require.Nil(t, problem)
	assert.Equal(t, "1:0:1:1234:5678:9ABC:0:0:0:0", input.ServiceRef)
	require.NotNil(t, input.Capabilities)
	assert.Equal(t, 2, input.Capabilities.CapabilitiesVersion)
	assert.Equal(t, []string{"mpegts"}, input.Capabilities.Container)
	require.NotNil(t, input.Capabilities.DeviceContext)
	assert.Equal(t, "browser", *input.Capabilities.DeviceContext.Platform)
}

func TestParseLivePlaybackPostInput_InvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(`{"serviceRef":"1:0:1:1234:5678:9ABC:0:0:0:0:","capabilities":`))

	input, problem := ParseLivePlaybackPostInput(req)

	assert.Equal(t, LivePlaybackInfoInput{}, input)
	require.NotNil(t, problem)
	assert.Equal(t, http.StatusBadRequest, problem.Status)
	assert.Equal(t, "live/invalid", problem.ProblemType)
	assert.Equal(t, problemcode.CodeInvalidInput, problem.Code)
	assert.Contains(t, problem.Detail, "Failed to parse request body:")
}

func TestParseLivePlaybackPostInput_MissingServiceRef(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(`{
		"serviceRef":"",
		"capabilities":{
			"capabilitiesVersion":2,
			"container":["mpegts"],
			"videoCodecs":["h264"],
			"audioCodecs":["aac"]
		}
	}`))

	input, problem := ParseLivePlaybackPostInput(req)

	assert.Equal(t, LivePlaybackInfoInput{}, input)
	require.NotNil(t, problem)
	assert.Equal(t, http.StatusBadRequest, problem.Status)
	assert.Equal(t, "live/invalid", problem.ProblemType)
	assert.Equal(t, problemcode.CodeInvalidInput, problem.Code)
	assert.Equal(t, "serviceRef is required", problem.Detail)
}

func TestParseLivePlaybackPostInput_RecognizesPreviewContextHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(`{
		"serviceRef":"1:0:1:1234:5678:9ABC:0:0:0:0",
		"capabilities":{
			"capabilitiesVersion":2,
			"container":["mpegts"],
			"videoCodecs":["h264"],
			"audioCodecs":["aac"]
		}
	}`))
	req.Header.Set("X-XG2G-Playback-Info-Context", "epg_badge")

	input, problem := ParseLivePlaybackPostInput(req)

	require.Nil(t, problem)
	assert.Equal(t, "1:0:1:1234:5678:9ABC:0:0:0:0", input.ServiceRef)
	require.NotNil(t, input.Capabilities)
}
