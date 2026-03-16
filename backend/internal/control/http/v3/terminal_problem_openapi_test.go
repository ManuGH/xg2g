// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/stretchr/testify/require"
)

func TestV3Contract_SessionTerminalProblem_OpenAPI(t *testing.T) {
	s, st := newV3TestServer(t, t.TempDir())
	sessionID := "550e8400-e29b-41d4-a716-446655440000"

	require.NoError(t, st.PutSession(context.Background(), &model.SessionRecord{
		SessionID:        sessionID,
		ServiceRef:       "1:0:1:445D:453:1:C00000:0:0:0:",
		Profile:          model.ProfileSpec{Name: "high"},
		State:            model.SessionFailed,
		Reason:           model.RProcessEnded,
		ReasonDetailCode: model.DTranscodeStalled,
		CorrelationID:    "corr-openapi-stall-001",
	}))

	req := httptest.NewRequest(http.MethodGet, V3BaseURL+"/sessions/"+sessionID, nil)
	req.Header.Set("Authorization", "Bearer test-token")

	rr := httptest.NewRecorder()
	NewRouter(s, RouterOptions{BaseURL: V3BaseURL}).ServeHTTP(rr, req)

	require.Equal(t, http.StatusGone, rr.Code)
	validateOpenAPIResponse(t, loadOpenAPIDoc(t), req, rr, nil)
}
