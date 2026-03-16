// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/problemcode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func decodeProblemBody(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	return body
}

func TestProblemSpecForPlaybackInfoError_InvalidInputLive(t *testing.T) {
	spec := problemSpecForPlaybackInfoError("live", &v3recordings.PlaybackInfoError{
		Kind:    v3recordings.PlaybackInfoErrorInvalidInput,
		Message: "serviceRef must be a valid live Enigma2 reference",
	})

	require.Equal(t, http.StatusBadRequest, spec.status)
	require.Nil(t, spec.rawProblem)
	require.Nil(t, spec.retryAfter)
	assert.Equal(t, "live/invalid", spec.problem.problemType)
	assert.Equal(t, problemcode.CodeInvalidInput, spec.problem.code)
	assert.Equal(t, "serviceRef must be a valid live Enigma2 reference", spec.problem.detail)
}

func TestProblemSpecForPlaybackInfoError_PreparingWithMetadata(t *testing.T) {
	spec := problemSpecForPlaybackInfoError("compact", &v3recordings.PlaybackInfoError{
		Kind:              v3recordings.PlaybackInfoErrorPreparing,
		Message:           "Retry shortly.",
		RetryAfterSeconds: 17,
		ProbeState:        "in_flight",
	})

	require.Equal(t, http.StatusServiceUnavailable, spec.status)
	require.NotNil(t, spec.retryAfter)
	assert.Equal(t, 17, *spec.retryAfter)
	assert.Equal(t, "recordings/preparing", spec.problem.problemType)
	assert.Equal(t, problemcode.CodeRecordingPreparing, spec.problem.code)
	assert.Equal(t, "Retry shortly.", spec.problem.detail)
	assert.Equal(t, map[string]any{
		"retryAfterSeconds": 17,
		"probeState":        "in_flight",
	}, spec.extra)
}

func TestWritePlaybackInfoServiceError_PassthroughProblem(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/rec1/stream-info", nil)
	r = r.WithContext(log.ContextWithRequestID(r.Context(), "req-problem"))

	writePlaybackInfoServiceError(w, r, "rec1", "compact", &v3recordings.PlaybackInfoError{
		Kind: v3recordings.PlaybackInfoErrorProblem,
		Problem: &v3recordings.PlaybackInfoProblem{
			Status: 422,
			Type:   "recordings/decision-ambiguous",
			Title:  "Decision Ambiguous",
			Code:   "decision_ambiguous",
			Detail: "Media truth unavailable or unknown",
		},
	})

	resp := w.Result()
	require.Equal(t, 422, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/problem+json")
	body := decodeProblemBody(t, resp)
	assert.Equal(t, "/problems/recordings/decision-ambiguous", body["type"])
	assert.Equal(t, "decision_ambiguous", body["code"])
	assert.Equal(t, "Media truth unavailable or unknown", body["detail"])
	assert.Equal(t, "req-problem", body["requestId"])
}

func TestWritePlaybackInfoServiceError_PreparingWritesRetryAfterAndExtras(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/rec1/stream-info", nil)
	r = r.WithContext(log.ContextWithRequestID(r.Context(), "req-preparing"))

	writePlaybackInfoServiceError(w, r, "rec1", "compact", &v3recordings.PlaybackInfoError{
		Kind:              v3recordings.PlaybackInfoErrorPreparing,
		Message:           "Retry shortly.",
		RetryAfterSeconds: 11,
		ProbeState:        "in_flight",
	})

	assert.Equal(t, "11", w.Header().Get("Retry-After"))
	resp := w.Result()
	require.Equal(t, 503, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/problem+json")
	body := decodeProblemBody(t, resp)
	assert.True(t, strings.HasSuffix(body["type"].(string), "/recordings/preparing"))
	assert.Equal(t, float64(11), body["retryAfterSeconds"])
	assert.Equal(t, "in_flight", body["probeState"])
	assert.Equal(t, problemcode.CodeRecordingPreparing, body["code"])
}
