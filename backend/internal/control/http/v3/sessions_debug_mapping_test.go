// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	v3sessions "github.com/ManuGH/xg2g/internal/control/http/v3/sessions"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteSessionsDebugServiceError_Internal(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v3/sessions", nil)
	r = r.WithContext(log.ContextWithRequestID(r.Context(), "req-sessions-debug"))

	writeSessionsDebugServiceError(w, r, &v3sessions.ListSessionsDebugError{
		Kind:    v3sessions.ListSessionsDebugErrorInternal,
		Message: "boom",
	})

	resp := w.Result()
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	body := decodeProblemBody(t, resp)
	spec := problemSpecForAPIError(ErrInternalServer, "")
	assert.Equal(t, "/problems/"+spec.problemType, body["type"])
	assert.Equal(t, spec.code, body["code"])
	assert.Equal(t, "boom", body["details"])
	assert.Equal(t, "req-sessions-debug", body["requestId"])
}

func TestWriteSessionsDebugResponse_WritesPaginationShape(t *testing.T) {
	w := httptest.NewRecorder()

	writeSessionsDebugResponse(w, v3sessions.ListSessionsDebugResult{
		Sessions: []*model.SessionRecord{
			{SessionID: "s1"},
			{SessionID: "s2"},
		},
		Pagination: v3sessions.ListSessionsDebugPagination{
			Offset: 5,
			Limit:  10,
			Total:  42,
			Count:  2,
		},
	})

	resp := w.Result()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	sessions, ok := body["sessions"].([]any)
	require.True(t, ok)
	assert.Len(t, sessions, 2)
	pagination, ok := body["pagination"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(5), pagination["offset"])
	assert.Equal(t, float64(10), pagination["limit"])
	assert.Equal(t, float64(42), pagination["total"])
	assert.Equal(t, float64(2), pagination["count"])
}
