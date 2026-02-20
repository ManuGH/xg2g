// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_SessionAndPlayback verifies the full auth flow:
// 1. Unauthenticated access to recordings -> 401
// 2. Login via /api/v3/auth/session -> 200 + Cookie
// 3. Cookie-based access to recordings -> 200 (or 400 if invalid serviceRef)
func TestIntegration_SessionAndPlayback(t *testing.T) {
	// Setup Server
	cfg := config.AppConfig{
		APIToken:       "integration-secret",
		APITokenScopes: []string{string(v3.ScopeV3Read)},
		DataDir:        t.TempDir(),
		ForceHTTPS:     true, // Enable ForceHTTPS to verify Secure cookie
	}
	s := mustNewServer(t, cfg, config.NewManager(""))

	// Use the router to ensure middleware integration
	handler := s.Handler()

	// 1. Attempt unauthenticated access
	req1 := httptest.NewRequest("GET", "/api/v3/recordings/some-id/playlist.m3u8", nil)
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	assert.Equal(t, http.StatusUnauthorized, w1.Code, "Expected 401 without auth")

	// 1b. Bearer-only access should be rejected for media
	req1b := httptest.NewRequest("GET", "/api/v3/recordings/some-id/playlist.m3u8", nil)
	req1b.Header.Set("Authorization", "Bearer integration-secret")
	w1b := httptest.NewRecorder()
	handler.ServeHTTP(w1b, req1b)
	assert.Equal(t, http.StatusUnauthorized, w1b.Code, "Expected 401 with bearer-only media access")

	// 2. Login to get session cookie
	req2 := httptest.NewRequest("POST", "/api/v3/auth/session", nil)
	req2.Host = "example.com"
	req2.Header.Set("Origin", "http://example.com")
	req2.Header.Set("Authorization", "Bearer integration-secret")
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code, "Login should succeed")

	// Extract cookie
	var sessionCookie *http.Cookie
	for _, c := range w2.Result().Cookies() {
		if c.Name == "xg2g_session" {
			sessionCookie = c
			break
		}
	}
	require.NotNil(t, sessionCookie, "Session cookie missing")
	assert.Equal(t, "integration-secret", sessionCookie.Value)
	assert.True(t, sessionCookie.HttpOnly, "Cookie must be HttpOnly")
	assert.True(t, sessionCookie.Secure, "Cookie must be Secure (ForceHTTPS=true)")
	assert.Equal(t, http.SameSiteStrictMode, sessionCookie.SameSite, "Cookie must be SameSite=Strict")
	assert.Equal(t, "/api/v3/", sessionCookie.Path, "Cookie path must be scoped to v3 API")

	// 3. Use cookie for access
	// We use a valid Base64URL encoded ID ("dGVzdA==" -> "test") that decodes to an
	// invalid recording reference. We expect 400, which proves we passed auth and
	// reached the handler logic (401 would indicate auth failure).

	req4 := httptest.NewRequest("GET", "/api/v3/recordings/dGVzdA==/playlist.m3u8", nil)
	req4.AddCookie(sessionCookie)
	w4 := httptest.NewRecorder()
	handler.ServeHTTP(w4, req4)

	// Assert deterministic failure mode (400)
	// This confirms we passed Auth (401) and reached the handler logic.
	assert.Equal(t, http.StatusBadRequest, w4.Code, "Expected 400 (Auth passed, invalid recording ID)")
}
