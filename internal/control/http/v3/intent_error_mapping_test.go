// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/control/http/problem"
)

func TestIntentErrorMapping_Table(t *testing.T) {
	for _, kind := range intentErrorKinds {
		spec, ok := intentErrorMap[kind]
		require.True(t, ok, "missing intent error mapping for %v", kind)
		require.NotZero(t, spec.status)
		require.NotNil(t, spec.apiErr)
	}
}

func TestIntentErrorMapping_ResponseShape(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/v3/intents", nil)

	for _, kind := range intentErrorKinds {
		spec := intentErrorMap[kind]
		rr := httptest.NewRecorder()

		respondIntentFailure(rr, req, kind, "details")

		require.Equal(t, spec.status, rr.Code)
		require.Contains(t, rr.Header().Get("Content-Type"), "application/problem+json")

		var body map[string]any
		require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
		require.Equal(t, float64(spec.status), body["status"])
		require.Equal(t, spec.apiErr.Code, body["code"])
		require.Equal(t, "error/"+strings.ToLower(spec.apiErr.Code), body["type"])
		require.Equal(t, spec.apiErr.Message, body["title"])
		require.Equal(t, "details", body["details"])

		reqID, ok := body[problem.JSONKeyRequestID].(string)
		require.True(t, ok)
		require.NotEmpty(t, reqID)
	}
}
