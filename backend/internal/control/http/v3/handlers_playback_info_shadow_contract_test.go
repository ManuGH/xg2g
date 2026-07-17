// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func createServerWithShadow(svc *MockRecordingsService, shadowEnabled bool) *v3.Server {
	cfg := config.AppConfig{
		PlannerShadow: config.PlannerShadowConfig{
			Enabled:       shadowEnabled,
			QueueCapacity: 64,
		},
	}
	s := v3.NewServer(cfg, nil, nil)
	s.SetRecordingsService(svc)
	s.SetDependencies(v3.Dependencies{
		ResumeStore:       new(MockResumeStore),
		RecordingsService: svc,
	})
	return s
}

func TestContract_PlaybackInfo_ShadowOnVsOff_ExactHTTPContractEquality(t *testing.T) {
	svc := new(MockRecordingsService)
	svc.On("GetMediaTruth", mock.Anything, validRecordingID).Return(playback.MediaTruth{
		Status:     playback.MediaStatusReady,
		Container:  "mp4",
		VideoCodec: "h264",
		AudioCodec: "aac",
		Width:      1920,
		Height:     1080,
		FPS:        25,
	}, nil)

	srvOff := createServerWithShadow(svc, false)
	srvOn := createServerWithShadow(svc, true)

	// 1. Test GET /api/v3/recordings/{id}/stream-info
	reqGet := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+validRecordingID+"/stream-info", nil)
	wOffGet := httptest.NewRecorder()
	wOnGet := httptest.NewRecorder()

	srvOff.GetRecordingPlaybackInfo(wOffGet, reqGet, validRecordingID)
	srvOn.GetRecordingPlaybackInfo(wOnGet, reqGet, validRecordingID)

	require.Equal(t, wOffGet.Code, wOnGet.Code)
	assert.Equal(t, wOffGet.Header().Get("Content-Type"), wOnGet.Header().Get("Content-Type"))
	assert.Equal(t, wOffGet.Body.String(), wOnGet.Body.String(), "HTTP JSON response body and decision token must be byte-for-byte identical")

	// 2. Test POST /api/v3/live/stream-info
	liveBody := `{"serviceRef":"1:0:1:2B66:3F3:1:C00000:0:0:0:"}`
	reqPostOff := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(liveBody))
	reqPostOn := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(liveBody))
	wOffPost := httptest.NewRecorder()
	wOnPost := httptest.NewRecorder()

	srvOff.PostLivePlaybackInfo(wOffPost, reqPostOff)
	srvOn.PostLivePlaybackInfo(wOnPost, reqPostOn)

	require.Equal(t, wOffPost.Code, wOnPost.Code)
	assert.Equal(t, wOffPost.Header().Get("Content-Type"), wOnPost.Header().Get("Content-Type"))
	assert.Equal(t, wOffPost.Body.String(), wOnPost.Body.String(), "Live HTTP JSON response body and decision token must be byte-for-byte identical")
}
