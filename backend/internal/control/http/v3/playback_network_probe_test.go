// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/stretchr/testify/require"
)

func TestPlaybackNetworkProbeSkipsTrustedPrivateLAN(t *testing.T) {
	s := NewServer(config.AppConfig{}, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v3/system/healthz?playbackProbe=1", nil)
	req.RemoteAddr = "192.168.10.25:12345"
	w := httptest.NewRecorder()

	s.GetSystemHealthz(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
	require.Equal(t, playbackProbeLAN, w.Header().Get(playbackProbeHeader))
	require.Empty(t, w.Body.Bytes())
}

func TestPlaybackNetworkProbeMeasuresPublicClient(t *testing.T) {
	s := NewServer(config.AppConfig{}, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v3/system/healthz?playbackProbe=1", nil)
	req.RemoteAddr = "203.0.113.25:12345"
	w := httptest.NewRecorder()

	s.GetSystemHealthz(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, playbackProbeMeasured, w.Header().Get(playbackProbeHeader))
	require.Equal(t, playbackProbeBytes, w.Body.Len())
	require.Equal(t, "identity", w.Header().Get("Content-Encoding"))
}

func TestPlaybackNetworkProbeDoesNotTrustSpoofedForwardedClient(t *testing.T) {
	s := NewServer(config.AppConfig{}, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v3/system/healthz?playbackProbe=1", nil)
	req.RemoteAddr = "203.0.113.25:12345"
	req.Header.Set("X-Forwarded-For", "192.168.10.25")
	w := httptest.NewRecorder()

	s.GetSystemHealthz(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, playbackProbeMeasured, w.Header().Get(playbackProbeHeader))
}
