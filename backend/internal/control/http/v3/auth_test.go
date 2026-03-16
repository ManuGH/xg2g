// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/problemcode"

	"github.com/stretchr/testify/assert"
)

// Note: Token extraction logic is tested in security_utils_test.go

func TestAuthMiddleware_NoTokenConfigured(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	s := &Server{
		cfg: config.AppConfig{
			APIToken: "",
		},
	}
	handler := s.authMiddleware(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.False(t, nextCalled)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_MissingAuthHeader(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not be called")
	})

	s := &Server{
		cfg: config.AppConfig{
			APIToken:       "secret-token",
			APITokenScopes: []string{string(ScopeV3Read)},
		},
	}
	handler := s.authMiddleware(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_ValidBearerToken(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	s := &Server{
		cfg: config.AppConfig{
			APIToken:       "secret-token",
			APITokenScopes: []string{string(ScopeV3Read)},
		},
	}
	handler := s.authMiddleware(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.True(t, nextCalled)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthMiddleware_ValidCookie(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	s := &Server{
		cfg: config.AppConfig{
			APIToken:       "secret-token",
			APITokenScopes: []string{string(ScopeV3Read)},
		},
	}
	handler := s.authMiddleware(next)
	sessionID := mustCreateAuthSession(t, s, "secret-token")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "xg2g_session", Value: sessionID})
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.True(t, nextCalled)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthMiddleware_MediaRequiresSessionCookie(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	s := &Server{
		cfg: config.AppConfig{
			APIToken:       "secret-token",
			APITokenScopes: []string{string(ScopeV3Read)},
		},
	}
	handler := s.authMiddleware(next)
	sessionID := mustCreateAuthSession(t, s, "secret-token")

	paths := []string{
		"/api/v3/recordings/abc/stream.mp4",
		"/api/v3/recordings/abc/playlist.m3u8",
		"/api/v3/sessions/00000000-0000-0000-0000-000000000000/hls/index.m3u8",
	}

	for _, path := range paths {
		t.Run(path+"/bearer-only", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Authorization", "Bearer secret-token")
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})

		t.Run(path+"/cookie", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.AddCookie(&http.Cookie{Name: "xg2g_session", Value: sessionID})
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
		})
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not be called")
	})

	s := &Server{
		cfg: config.AppConfig{
			APIToken:       "secret-token",
			APITokenScopes: []string{string(ScopeV3Read)},
		},
	}
	handler := s.authMiddleware(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_EmptyScopesPrimaryToken(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not be called")
	})

	s := &Server{
		cfg: config.AppConfig{
			APIToken: "secret-token",
		},
	}
	handler := s.authMiddleware(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_EmptyScopesAdditionalToken(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not be called")
	})

	s := &Server{
		cfg: config.AppConfig{
			APITokens: []config.ScopedToken{
				{Token: "scoped"},
			},
		},
	}
	handler := s.authMiddleware(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer scoped")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// Test legacy X-API-Token header support when explicitly enabled.
func TestAuthMiddleware_ValidXAPITokenWhenLegacyEnabled(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	s := &Server{
		cfg: config.AppConfig{
			APIToken:       "secret-token",
			APITokenScopes: []string{string(ScopeV3Read)},
			// Direct server config in this unit test bypasses registry defaults.
			APIDisableLegacyTokenSources: false,
		},
	}
	handler := s.authMiddleware(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Token", "secret-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.True(t, nextCalled)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthMiddleware_LegacyTokenSourcesDisabled(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	s := &Server{
		cfg: config.AppConfig{
			APIToken:                     "secret-token",
			APITokenScopes:               []string{string(ScopeV3Read)},
			APIDisableLegacyTokenSources: true,
		},
	}
	handler := s.authMiddleware(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Token", "secret-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	bearerReq := httptest.NewRequest(http.MethodGet, "/", nil)
	bearerReq.Header.Set("Authorization", "Bearer secret-token")
	bearerResp := httptest.NewRecorder()
	handler.ServeHTTP(bearerResp, bearerReq)

	assert.True(t, nextCalled)
	assert.Equal(t, http.StatusOK, bearerResp.Code)
}

func TestCreateSession(t *testing.T) {
	s := &Server{
		cfg: config.AppConfig{
			APIToken:       "secret",
			APITokenScopes: []string{string(ScopeV3Read)},
			ForceHTTPS:     true,
		},
	}

	// Auth success
	req := httptest.NewRequest("POST", "/api/v3/auth/session", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.TLS = &tls.ConnectionState{}
	w := httptest.NewRecorder()

	s.CreateSession(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Check cookie
	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "xg2g_session" {
			found = true
			assert.NotEqual(t, "secret", c.Value)
			assert.NotEmpty(t, c.Value)
			assert.True(t, c.HttpOnly)
			assert.True(t, c.Secure) // ForceHTTPS=true
			assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
			assert.Equal(t, "/api/v3/", c.Path)
			assert.Equal(t, 24*time.Hour, time.Duration(c.MaxAge)*time.Second)
			resolved, ok := s.resolveSessionToken(c.Value)
			assert.True(t, ok)
			assert.Equal(t, "secret", resolved)
		}
	}
	assert.True(t, found, "xg2g_session cookie not found")
}

func TestCreateSession_TransportSecurity(t *testing.T) {
	testCases := []struct {
		name         string
		cfg          config.AppConfig
		remoteAddr   string
		xfp          string
		tlsRequest   bool
		wantStatus   int
		wantCode     string
		expectCookie bool
		expectSecure bool
	}{
		{
			name: "plain HTTP non-loopback rejected",
			cfg: config.AppConfig{
				APIToken:       "secret",
				APITokenScopes: []string{string(ScopeV3Read)},
			},
			wantStatus:   http.StatusBadRequest,
			wantCode:     problemcode.CodeHTTPSRequired,
			expectCookie: false,
		},
		{
			name: "direct TLS request",
			cfg: config.AppConfig{
				APIToken:       "secret",
				APITokenScopes: []string{string(ScopeV3Read)},
			},
			tlsRequest:   true,
			wantStatus:   http.StatusOK,
			expectCookie: true,
			expectSecure: true,
		},
		{
			name: "trusted proxy HTTPS",
			cfg: config.AppConfig{
				APIToken:       "secret",
				APITokenScopes: []string{string(ScopeV3Read)},
				TrustedProxies: "127.0.0.1,::1",
			},
			remoteAddr:   "127.0.0.1:1234",
			xfp:          "https",
			wantStatus:   http.StatusOK,
			expectCookie: true,
			expectSecure: true,
		},
		{
			name: "loopback plain HTTP allowed",
			cfg: config.AppConfig{
				APIToken:       "secret",
				APITokenScopes: []string{string(ScopeV3Read)},
			},
			remoteAddr:   "127.0.0.1:1234",
			wantStatus:   http.StatusOK,
			expectCookie: true,
			expectSecure: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{cfg: tc.cfg}

			req := httptest.NewRequest(http.MethodPost, "/api/v3/auth/session", nil)
			req.Header.Set("Authorization", "Bearer secret")
			if tc.remoteAddr != "" {
				req.RemoteAddr = tc.remoteAddr
			}
			if tc.xfp != "" {
				req.Header.Set("X-Forwarded-Proto", tc.xfp)
			}
			if tc.tlsRequest {
				req.TLS = &tls.ConnectionState{}
			}
			w := httptest.NewRecorder()

			s.CreateSession(w, req)
			assert.Equal(t, tc.wantStatus, w.Code)

			var sessionCookie *http.Cookie
			for _, c := range w.Result().Cookies() {
				if c.Name == "xg2g_session" {
					sessionCookie = c
					break
				}
			}

			if !tc.expectCookie {
				assert.Nil(t, sessionCookie)
				var body map[string]any
				assert.NoError(t, json.NewDecoder(w.Body).Decode(&body))
				assert.Equal(t, tc.wantCode, body["code"])
				return
			}
			if sessionCookie == nil {
				t.Fatalf("xg2g_session cookie not found")
			}
			assert.Equal(t, tc.expectSecure, sessionCookie.Secure)
		})
	}
}

func TestCreateSession_RejectsCookieOnly(t *testing.T) {
	s := &Server{
		cfg: config.AppConfig{
			APIToken:       "secret",
			APITokenScopes: []string{string(ScopeV3Read)},
			ForceHTTPS:     true,
		},
	}

	req := httptest.NewRequest("POST", "/api/v3/auth/session", nil)
	req.AddCookie(&http.Cookie{Name: "xg2g_session", Value: "secret"})
	w := httptest.NewRecorder()

	s.CreateSession(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_InvalidSessionID(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not be called")
	})

	s := &Server{
		cfg: config.AppConfig{
			APIToken:       "secret-token",
			APITokenScopes: []string{string(ScopeV3Read)},
		},
	}
	handler := s.authMiddleware(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "xg2g_session", Value: "invalid-session-id"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthSession_DeleteInvalidatesCookie(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	s := &Server{
		cfg: config.AppConfig{
			APIToken:       "secret-token",
			APITokenScopes: []string{string(ScopeV3Read)},
		},
	}
	handler := s.authMiddleware(next)

	sessionID := mustCreateAuthSession(t, s, "secret-token")
	s.deleteAuthSession(sessionID)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "xg2g_session", Value: sessionID})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func mustCreateAuthSession(t *testing.T, s *Server, token string) string {
	t.Helper()
	sessionID, err := s.authSessionStoreOrDefault().CreateSession(token, time.Hour)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	return sessionID
}
