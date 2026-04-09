// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	controlhttp "github.com/ManuGH/xg2g/internal/control/http"
	v3sessions "github.com/ManuGH/xg2g/internal/control/http/v3/sessions"
	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteSessionStateResponse_WritesJSONAndTraceHeader(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v3/sessions/550e8400-e29b-41d4-a716-446655440001", nil)
	r = r.WithContext(log.ContextWithRequestID(r.Context(), "req-session-response"))

	writeSessionStateResponse(w, r, "", v3sessions.GetSessionResult{
		Session: &model.SessionRecord{
			SessionID:          "550e8400-e29b-41d4-a716-446655440001",
			ServiceRef:         "1:0:1:445D:453:1:C00000:0:0:0:",
			Profile:            model.ProfileSpec{Name: "compatible"},
			State:              model.SessionReady,
			CorrelationID:      "corr-123",
			UpdatedAtUnix:      1700000000,
			HeartbeatInterval:  30,
			LeaseExpiresAtUnix: 1700000030,
			ContextData:        map[string]string{model.CtxKeyClientPath: "hlsjs"},
			PlaybackTrace:      &model.PlaybackTrace{RequestProfile: "compatible"},
			LastAccessUnix:     1700000000,
		},
		Outcome: lifecycle.PublicOutcome{
			State:      model.SessionReady,
			Reason:     model.RNone,
			DetailCode: model.DNone,
		},
		PlaybackInfo: v3sessions.SessionPlaybackInfo{
			Mode:                 model.ModeRecording,
			DurationSeconds:      float64Ptr(3600),
			SeekableStartSeconds: float64Ptr(0),
			SeekableEndSeconds:   float64Ptr(3600),
		},
	})

	resp := w.Result()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "req-session-response", resp.Header.Get(controlhttp.HeaderRequestID))
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

	var body SessionResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "req-session-response", body.RequestId)
	require.NotNil(t, body.Trace)
	require.NotNil(t, body.Trace.SessionId)
	assert.Equal(t, "550e8400-e29b-41d4-a716-446655440001", *body.Trace.SessionId)
	require.NotNil(t, body.Mode)
	assert.Equal(t, RECORDING, *body.Mode)
	require.NotNil(t, body.PlaybackUrl)
	assert.Equal(t, V3BaseURL+"/sessions/550e8400-e29b-41d4-a716-446655440001/hls/index.m3u8", *body.PlaybackUrl)
	require.NotNil(t, body.DurationSeconds)
	assert.Equal(t, float32(3600), *body.DurationSeconds)
	assert.Equal(t, int32(30), body.HeartbeatIntervalSeconds)
	assert.Equal(t, "2023-11-14T22:13:50Z", body.LeaseExpiresAt.Format(time.RFC3339))
}

func TestMapSessionStateResponse_IncludesProfileReasonForSafariTranscode(t *testing.T) {
	resp := mapSessionStateResponse("req-profile-reason", "", v3sessions.GetSessionResult{
		Session: &model.SessionRecord{
			SessionID:          "550e8400-e29b-41d4-a716-446655440002",
			ServiceRef:         "1:0:1:445D:453:1:C00000:0:0:0:",
			Profile:            model.ProfileSpec{Name: profiles.ProfileSafari, TranscodeVideo: true},
			State:              model.SessionPriming,
			CorrelationID:      "corr-safari",
			HeartbeatInterval:  30,
			LeaseExpiresAtUnix: 1700000030,
		},
		Outcome: lifecycle.PublicOutcome{
			State:      model.SessionPriming,
			Reason:     model.RNone,
			DetailCode: model.DNone,
		},
		PlaybackInfo: v3sessions.SessionPlaybackInfo{Mode: model.ModeLive},
	})

	require.NotNil(t, resp.ProfileReason)
	assert.Equal(t, sessionProfileReasonSafariCompatTranscode, *resp.ProfileReason)
}

func float64Ptr(v float64) *float64 { return &v }
