// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/control/auth"
	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func intPtr(i int) *int { return &i }

func TestBuildPlaybackInfoServiceRequest_LiveRequest(t *testing.T) {
	hlsEngines := []string{"native", "hlsjs"}
	videoCodecSignals := []PlaybackVideoCodecSignal{
		{
			Codec:          "av1",
			Supported:      true,
			Smooth:         boolPtr(true),
			PowerEfficient: boolPtr(true),
		},
		{
			Codec:     "h264",
			Supported: true,
		},
	}
	caps := &PlaybackCapabilities{
		AllowTranscode:       boolPtr(false),
		AudioCodecs:          []string{"aac"},
		CapabilitiesVersion:  3,
		ClientFamilyFallback: strPtr("safari"),
		Container:            []string{"mpegts", "hls"},
		DeviceType:           strPtr("tv"),
		HlsEngines:           &hlsEngines,
		MaxVideo: &struct {
			Fps    *int `json:"fps,omitempty"`
			Height *int `json:"height,omitempty"`
			Width  *int `json:"width,omitempty"`
		}{
			Fps:    intPtr(60),
			Height: intPtr(1080),
			Width:  intPtr(1920),
		},
		PreferredHlsEngine:  strPtr("native"),
		RuntimeProbeUsed:    boolPtr(true),
		RuntimeProbeVersion: intPtr(2),
		SupportsHls:         boolPtr(true),
		SupportsRange:       boolPtr(true),
		VideoCodecSignals:   &videoCodecSignals,
		VideoCodecs:         []string{"h264"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v3/streams/live?profile=safari", nil)
	req.Header.Add("X-Test", "first")
	req.Header.Add("X-Test", "second")
	req.Header.Set("User-Agent", "Mozilla/5.0 Safari/605.1.15")
	req = req.WithContext(log.ContextWithRequestID(req.Context(), "req-123"))
	req = req.WithContext(auth.WithPrincipal(req.Context(), auth.NewPrincipal("token", "alice", nil)))

	got := buildPlaybackInfoServiceRequest(req, "1:0:1:2:3:4:5:6:7:8:9", caps, "v3.1", "live")

	require.NotNil(t, got.Capabilities)
	assert.Equal(t, "1:0:1:2:3:4:5:6:7:8:9", got.SubjectID)
	assert.Equal(t, v3recordings.PlaybackSubjectLive, got.SubjectKind)
	assert.Equal(t, "v3.1", got.APIVersion)
	assert.Equal(t, "live", got.SchemaType)
	assert.Equal(t, "safari", got.RequestedProfile)
	assert.Equal(t, "alice", got.PrincipalID)
	assert.Equal(t, "req-123", got.RequestID)
	assert.Equal(t, string(ClientProfileSafari), got.ClientProfile)
	assert.Equal(t, map[string]string{
		"User-Agent": "Mozilla/5.0 Safari/605.1.15",
		"X-Test":     "first",
	}, got.Headers)
	assert.Equal(t, 3, got.Capabilities.CapabilitiesVersion)
	assert.Equal(t, []string{"mpegts", "hls"}, got.Capabilities.Containers)
	assert.Equal(t, []string{"h264"}, got.Capabilities.VideoCodecs)
	assert.Equal(t, []string{"av1", "h264"}, []string{
		got.Capabilities.VideoCodecSignals[0].Codec,
		got.Capabilities.VideoCodecSignals[1].Codec,
	})
	require.NotNil(t, got.Capabilities.VideoCodecSignals[0].PowerEfficient)
	assert.True(t, *got.Capabilities.VideoCodecSignals[0].PowerEfficient)
	assert.Equal(t, []string{"aac"}, got.Capabilities.AudioCodecs)
	assert.True(t, got.Capabilities.SupportsHLS)
	assert.True(t, got.Capabilities.SupportsHLSExplicit)
	assert.Equal(t, []string{"native", "hlsjs"}, got.Capabilities.HLSEngines)
	assert.Equal(t, "native", got.Capabilities.PreferredHLSEngine)
	assert.True(t, got.Capabilities.RuntimeProbeUsed)
	assert.Equal(t, 2, got.Capabilities.RuntimeProbeVersion)
	assert.Equal(t, "safari", got.Capabilities.ClientFamilyFallback)
	assert.Equal(t, "tv", got.Capabilities.DeviceType)
	require.NotNil(t, got.Capabilities.SupportsRange)
	assert.True(t, *got.Capabilities.SupportsRange)
	require.NotNil(t, got.Capabilities.AllowTranscode)
	assert.False(t, *got.Capabilities.AllowTranscode)
	require.NotNil(t, got.Capabilities.MaxVideo)
	assert.Equal(t, 1920, got.Capabilities.MaxVideo.Width)
	assert.Equal(t, 1080, got.Capabilities.MaxVideo.Height)
	assert.Equal(t, 60, got.Capabilities.MaxVideo.Fps)
}

func TestBuildPlaybackInfoServiceRequest_RecordingDefaults(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/rec1/stream-info", nil)

	got := buildPlaybackInfoServiceRequest(req, "rec1", nil, "v3", "compact")

	assert.Equal(t, "rec1", got.SubjectID)
	assert.Equal(t, v3recordings.PlaybackSubjectRecording, got.SubjectKind)
	assert.Equal(t, "v3", got.APIVersion)
	assert.Equal(t, "compact", got.SchemaType)
	assert.Equal(t, "", got.RequestedProfile)
	assert.Equal(t, "", got.PrincipalID)
	assert.Equal(t, "", got.RequestID)
	assert.Equal(t, string(ClientProfileGeneric), got.ClientProfile)
	assert.NotNil(t, got.Headers)
	assert.Nil(t, got.Capabilities)
}

func TestBuildPlaybackInfoServiceRequest_RecordingPreservesExplicitAndroidProfile(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/rec1/stream-info?profile=android_native", nil)

	got := buildPlaybackInfoServiceRequest(req, "rec1", nil, "v3", "compact")

	assert.Equal(t, "android_native", got.RequestedProfile)
	assert.Equal(t, "android_native", got.ClientProfile)
}
