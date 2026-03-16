// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"
	"net/http/httptest"
	"testing"

	v3sessions "github.com/ManuGH/xg2g/internal/control/http/v3/sessions"
	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteSessionStateServiceError_InvalidInput(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v3/sessions/bad", nil)
	r = r.WithContext(log.ContextWithRequestID(r.Context(), "req-invalid-session"))

	writeSessionStateServiceError(w, r, "", &v3sessions.GetSessionError{
		Kind:    v3sessions.GetSessionErrorInvalidInput,
		Message: "invalid session id",
	})

	resp := w.Result()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	body := decodeProblemBody(t, resp)
	spec := problemSpecForAPIError(ErrInvalidInput, "")
	assert.Equal(t, "/problems/"+spec.problemType, body["type"])
	assert.Equal(t, spec.code, body["code"])
	assert.Equal(t, "req-invalid-session", body["requestId"])
	assert.Equal(t, "invalid session id", body["details"])
}

func TestWriteSessionStateServiceError_TerminalWritesGoneProblem(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v3/sessions/550e8400-e29b-41d4-a716-446655440001", nil)
	r = r.WithContext(log.ContextWithRequestID(r.Context(), "req-terminal-session"))

	err := &v3sessions.GetSessionError{
		Kind: v3sessions.GetSessionErrorTerminal,
		Terminal: &v3sessions.GetSessionTerminal{
			Session: &model.SessionRecord{
				SessionID:  "550e8400-e29b-41d4-a716-446655440001",
				State:      model.SessionFailed,
				ServiceRef: "1:0:1:445D:453:1:C00000:0:0:0:",
				Profile:    model.ProfileSpec{Name: "compatible"},
			},
			Outcome: lifecycle.PublicOutcome{
				State:      model.SessionFailed,
				Reason:     model.RProcessEnded,
				DetailCode: model.DTranscodeStalled,
			},
		},
	}

	writeSessionStateServiceError(w, r, "", err)

	resp := w.Result()
	require.Equal(t, http.StatusGone, resp.StatusCode)
	body := decodeProblemBody(t, resp)
	spec := mapTerminalProblem(lifecycle.PublicOutcome{
		State:      model.SessionFailed,
		Reason:     model.RProcessEnded,
		DetailCode: model.DTranscodeStalled,
	})
	assert.Equal(t, "/problems/"+spec.problemType, body["type"])
	assert.Equal(t, spec.code, body["code"])
	assert.Equal(t, spec.detail, body["detail"])
	assert.Equal(t, "req-terminal-session", body["requestId"])
	assert.Equal(t, "550e8400-e29b-41d4-a716-446655440001", body["session"])
	assert.Equal(t, string(SessionResponseStateFAILED), body["state"])
	assert.Equal(t, string(SessionResponseReason(model.RProcessEnded)), body["reason"])
	assert.Equal(t, "transcode stalled - no progress detected", body["reason_detail"])
}

func TestWriteSessionStateServiceError_TerminalWithoutSessionFallsBackToNotFound(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v3/sessions/missing", nil)

	writeSessionStateServiceError(w, r, "", &v3sessions.GetSessionError{
		Kind:     v3sessions.GetSessionErrorTerminal,
		Terminal: &v3sessions.GetSessionTerminal{},
	})

	resp := w.Result()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	body := decodeProblemBody(t, resp)
	spec := problemSpecForAPIError(ErrSessionNotFound, "")
	assert.Equal(t, "/problems/"+spec.problemType, body["type"])
	assert.Equal(t, spec.code, body["code"])
}
