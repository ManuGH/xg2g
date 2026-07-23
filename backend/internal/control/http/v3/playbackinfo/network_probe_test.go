// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package playbackinfo

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlaybackNetworkProbeSkipsTrustedPrivateLAN(t *testing.T) {
	svc := NewService(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v3/system/healthz?playbackProbe=1", nil)
	w := httptest.NewRecorder()

	svc.ServePlaybackNetworkProbe(w, req, "192.168.10.25")

	require.Equal(t, http.StatusNoContent, w.Code)
	require.Equal(t, playbackProbeLAN, w.Header().Get(playbackProbeHeader))
	require.Empty(t, w.Body.Bytes())
}

func TestPlaybackNetworkProbeMeasuresPublicClient(t *testing.T) {
	svc := NewService(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v3/system/healthz?playbackProbe=1", nil)
	w := httptest.NewRecorder()

	svc.ServePlaybackNetworkProbe(w, req, "203.0.113.25")

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, playbackProbeMeasured, w.Header().Get(playbackProbeHeader))
	require.Equal(t, playbackProbeBytes, w.Body.Len())
	require.Equal(t, "identity", w.Header().Get("Content-Encoding"))
}

func TestPlaybackNetworkProbeDoesNotTrustSpoofedForwardedClient(t *testing.T) {
	svc := NewService(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v3/system/healthz?playbackProbe=1", nil)
	w := httptest.NewRecorder()

	svc.ServePlaybackNetworkProbe(w, req, "203.0.113.25")

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, playbackProbeMeasured, w.Header().Get(playbackProbeHeader))
}
