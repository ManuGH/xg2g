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
	"github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
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

func TestMapSessionsDebugResponse_MasksContextDataAndReplay(t *testing.T) {
	rawIPTVRef := "4097:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid/SECRET-CANARY-1/live/token-xyz-987/channel_prime.ts:Canary Channel 1"
	maskedID := "iptv_masked_canary_id"

	replay := runtimepolicy.RuntimePolicyReplay{
		Metadata: runtimepolicy.ReplayMetadata{
			SessionID:  "sess-1",
			ServiceRef: rawIPTVRef,
		},
	}
	replayJSON, err := json.Marshal(replay)
	require.NoError(t, err)

	origRecord := &model.SessionRecord{
		SessionID:  "sess-1",
		ServiceRef: rawIPTVRef,
		ContextData: map[string]string{
			model.CtxKeySource:              rawIPTVRef,
			"custom_iptv":                   "5001:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid/stream:Custom",
			model.CtxKeyRuntimePolicyReplay: string(replayJSON),
			"client_ip":                     "192.168.1.50",
			"dvb_source":                    "1:0:19:283D:3FB:1:C00000:0:0:0:",
		},
	}

	mockMask := func(ref string) string {
		if isIPTVRef(ref) {
			return maskedID
		}
		return ref
	}

	result := v3sessions.ListSessionsDebugResult{
		Sessions: []*model.SessionRecord{origRecord},
		Pagination: v3sessions.ListSessionsDebugPagination{
			Count: 1,
			Total: 1,
		},
	}

	mapped := mapSessionsDebugResponse(result, mockMask)
	mappedSessions, ok := mapped["sessions"].([]*model.SessionRecord)
	require.True(t, ok)
	require.Len(t, mappedSessions, 1)

	sOut := mappedSessions[0]
	assert.Equal(t, maskedID, sOut.ServiceRef, "ServiceRef must be masked")

	require.NotNil(t, sOut.ContextData)
	assert.Equal(t, maskedID, sOut.ContextData[model.CtxKeySource], "ContextData[source] must be masked")
	assert.Equal(t, maskedID, sOut.ContextData["custom_iptv"], "Custom IPTV ref in ContextData must be masked")
	assert.Equal(t, "192.168.1.50", sOut.ContextData["client_ip"], "Non-IPTV context data must be preserved")
	assert.Equal(t, "1:0:19:283D:3FB:1:C00000:0:0:0:", sOut.ContextData["dvb_source"], "DVB references must remain untouched")

	// Verify replay JSON in ContextData
	var outReplay runtimepolicy.RuntimePolicyReplay
	require.NoError(t, json.Unmarshal([]byte(sOut.ContextData[model.CtxKeyRuntimePolicyReplay]), &outReplay))
	assert.Equal(t, maskedID, outReplay.Metadata.ServiceRef, "Replay metadata ServiceRef must be masked")

	// Verify original session record was not mutated (defensive copy)
	assert.Equal(t, rawIPTVRef, origRecord.ServiceRef, "Original SessionRecord must not be mutated")
	assert.Equal(t, rawIPTVRef, origRecord.ContextData[model.CtxKeySource], "Original ContextData must not be mutated")
}
