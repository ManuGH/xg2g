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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const playbackSummaryTestCaps = `{
	"capabilitiesVersion":2,
	"container":["mpegts","ts"],
	"videoCodecs":["h264"],
	"audioCodecs":["aac"],
	"hlsEngines":["native"],
	"preferredHlsEngine":"native",
	"runtimeProbeUsed":true,
	"runtimeProbeVersion":1,
	"clientFamilyFallback":"safari_native"
}`

func postPlaybackSummary(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v3/live/playback-summary", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	s.PostLivePlaybackSummary(w, r)
	return w
}

func TestPostLivePlaybackSummary_UnresolvableRefsAreOmittedNotFatal(t *testing.T) {
	svc := new(MockRecordingsService)
	s := createTestServerDTO(svc)

	// No scan truth is available in this fixture, so every ref fails to
	// resolve — the batch must still succeed with an empty items map.
	body := `{
		"serviceRefs":["1:0:1:1234:5678:9ABC:0:0:0:0:","not-a-ref",""],
		"capabilities":` + playbackSummaryTestCaps + `
	}`

	w := postPlaybackSummary(t, s, body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var raw struct {
		Items map[string]json.RawMessage `json:"items"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	assert.NotNil(t, raw.Items)
	assert.Empty(t, raw.Items)
}

func TestPostLivePlaybackSummary_RejectsEmptyRefs(t *testing.T) {
	svc := new(MockRecordingsService)
	s := createTestServerDTO(svc)

	body := `{"serviceRefs":[],"capabilities":` + playbackSummaryTestCaps + `}`
	w := postPlaybackSummary(t, s, body)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPostLivePlaybackSummary_RejectsOversizedBatch(t *testing.T) {
	svc := new(MockRecordingsService)
	s := createTestServerDTO(svc)

	refs := make([]string, 101)
	for i := range refs {
		refs[i] = "1:0:1:1234:5678:9ABC:0:0:0:0:"
	}
	encoded, err := json.Marshal(refs)
	require.NoError(t, err)

	body := `{"serviceRefs":` + string(encoded) + `,"capabilities":` + playbackSummaryTestCaps + `}`
	w := postPlaybackSummary(t, s, body)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPostLivePlaybackSummary_RejectsInvalidCapabilities(t *testing.T) {
	svc := new(MockRecordingsService)
	s := createTestServerDTO(svc)

	body := `{"serviceRefs":["1:0:1:1234:5678:9ABC:0:0:0:0:"],"capabilities":{"capabilitiesVersion":0}}`
	w := postPlaybackSummary(t, s, body)
	require.Equal(t, http.StatusBadRequest, w.Code)
}
