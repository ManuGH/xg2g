// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLegacyAPIMiddleware(t *testing.T) {
	s := &Server{cfg: config.AppConfig{APILegacyEnabled: true}}
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	handler := s.legacyAPIMiddleware(dummyHandler)

	t.Run("canonical v3 request is unmeasured and unaltered", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings", nil)
		req.Header.Set("User-Agent", "xg2g-client/3.0")
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "ok", rr.Body.String())
	})

	t.Run("legacy v2 request increments metric and passes through without behavior change", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/system/health", nil)
		req.Header.Set("User-Agent", "xg2g-webui-legacy/2.1 (Linux x86_64)")
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "ok", rr.Body.String())

		// Check prometheus metric incremented for path /api/v2/system and client xg2g-webui-legacy/2.1
		val := testutil.ToFloat64(legacyAPIRequestsTotal.WithLabelValues("/api/v2/system", "xg2g-webui-legacy/2.1"))
		assert.GreaterOrEqual(t, val, float64(1))
	})

	t.Run("legacy v1 request without User-Agent increments metric with unknown client", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/query", nil)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		val := testutil.ToFloat64(legacyAPIRequestsTotal.WithLabelValues("/api/v1/query", "unknown"))
		assert.GreaterOrEqual(t, val, float64(1))
	})

	t.Run("paths like /api/v3-beta or /api/v34 are treated and measured as legacy endpoints", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v3-beta/test", nil)
		req.Header.Set("User-Agent", "xg2g-beta/1.0")
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		val := testutil.ToFloat64(legacyAPIRequestsTotal.WithLabelValues("/api/v3-beta/test", "xg2g-beta/1.0"))
		assert.GreaterOrEqual(t, val, float64(1))
	})

	t.Run("non-api public request passes through without measurement", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
	})
}

func TestGetClientLabel(t *testing.T) {
	tests := []struct {
		name     string
		ua       string
		expected string
	}{
		{
			name:     "empty User-Agent",
			ua:       "",
			expected: "unknown",
		},
		{
			name:     "whitespace only",
			ua:       "   \t\n   ",
			expected: "unknown",
		},
		{
			name:     "simple client token",
			ua:       "curl/7.68.0",
			expected: "curl/7.68.0",
		},
		{
			name:     "complex User-Agent takes first token",
			ua:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
			expected: "Mozilla/5.0",
		},
		{
			name:     "extremely long token gets truncated to 64 chars",
			ua:       strings.Repeat("a", 100),
			expected: strings.Repeat("a", 64),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
			if tt.ua != "" {
				req.Header.Set("User-Agent", tt.ua)
			}
			require.Equal(t, tt.expected, getClientLabel(req))
		})
	}
}

func TestNormalizeLegacyPath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/api/v1/channel", "/api/v1/channel"},
		{"/api/v1/channel/12345/stream", "/api/v1/channel"},
		{"/api/v2/system/health/details", "/api/v2/system"},
		{"/api/short", "/api/short"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeLegacyPath(tt.input))
		})
	}
}

func TestLegacyAPIMiddleware_Gate(t *testing.T) {
	s := &Server{cfg: config.AppConfig{APILegacyEnabled: false}}
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	handler := s.legacyAPIMiddleware(dummyHandler)

	t.Run("canonical v3 request passes through even when APILegacyEnabled is false", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings", nil)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "ok", rr.Body.String())
	})

	t.Run("path traversal trick /api/v3/../v2/status is intercepted and blocked", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v3/../v2/status", nil)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusGone, rr.Code)
	})

	t.Run("legacy v2 request returns 410 Gone with problem+json when APILegacyEnabled is false", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/system/health", nil)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusGone, rr.Code)
		assert.Equal(t, "application/problem+json", rr.Header().Get("Content-Type"))

		var resp map[string]interface{}
		err := json.Unmarshal(rr.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "Legacy API Retired", resp["title"])
		assert.Equal(t, float64(http.StatusGone), resp["status"])
	})
}
