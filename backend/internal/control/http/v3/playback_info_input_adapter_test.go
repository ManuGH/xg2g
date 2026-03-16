// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

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
		"supportsHls":true
	}`))

	caps, problem := parseRecordingPlaybackPostInput(req)

	require.Nil(t, problem)
	require.NotNil(t, caps)
	assert.Equal(t, 2, caps.CapabilitiesVersion)
	assert.Equal(t, []string{"mp4", "hls"}, caps.Container)
	assert.Equal(t, []string{"h264"}, caps.VideoCodecs)
	assert.Equal(t, []string{"aac"}, caps.AudioCodecs)
	require.NotNil(t, caps.SupportsHls)
	assert.True(t, *caps.SupportsHls)
}

func TestParseRecordingPlaybackPostInput_InvalidCapabilitiesVersion(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v3/recordings/rec1/stream-info", strings.NewReader(`{
		"capabilitiesVersion":0,
		"container":["mp4"],
		"videoCodecs":["h264"],
		"audioCodecs":["aac"]
	}`))

	caps, problem := parseRecordingPlaybackPostInput(req)

	assert.Nil(t, caps)
	require.NotNil(t, problem)
	assert.Equal(t, http.StatusBadRequest, problem.status)
	assert.Equal(t, "recordings/invalid", problem.problemType)
	assert.Equal(t, problemcode.CodeInvalidCapabilities, problem.code)
	assert.Equal(t, "capabilities_version must be >= 1", problem.detail)
}

func TestParseLivePlaybackPostInput_NormalizesServiceRef(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(`{
		"serviceRef":" 1:0:1:1234:5678:9abc:0:0:0:0: ",
		"capabilities":{
			"capabilitiesVersion":2,
			"container":["mpegts"],
			"videoCodecs":["h264"],
			"audioCodecs":["aac"]
		}
	}`))

	input, problem := parseLivePlaybackPostInput(req)

	require.Nil(t, problem)
	assert.Equal(t, "1:0:1:1234:5678:9ABC:0:0:0:0", input.serviceRef)
	require.NotNil(t, input.capabilities)
	assert.Equal(t, 2, input.capabilities.CapabilitiesVersion)
	assert.Equal(t, []string{"mpegts"}, input.capabilities.Container)
}

func TestParseLivePlaybackPostInput_InvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(`{"serviceRef":"1:0:1:1234:5678:9ABC:0:0:0:0:","capabilities":`))

	input, problem := parseLivePlaybackPostInput(req)

	assert.Equal(t, livePlaybackInfoInput{}, input)
	require.NotNil(t, problem)
	assert.Equal(t, http.StatusBadRequest, problem.status)
	assert.Equal(t, "live/invalid", problem.problemType)
	assert.Equal(t, problemcode.CodeInvalidInput, problem.code)
	assert.Contains(t, problem.detail, "Failed to parse request body:")
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

	input, problem := parseLivePlaybackPostInput(req)

	assert.Equal(t, livePlaybackInfoInput{}, input)
	require.NotNil(t, problem)
	assert.Equal(t, http.StatusBadRequest, problem.status)
	assert.Equal(t, "live/invalid", problem.problemType)
	assert.Equal(t, problemcode.CodeInvalidInput, problem.code)
	assert.Equal(t, "serviceRef is required", problem.detail)
}
