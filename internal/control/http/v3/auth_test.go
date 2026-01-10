// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"

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

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "xg2g_session", Value: "secret-token"})
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
			req.AddCookie(&http.Cookie{Name: "xg2g_session", Value: "secret-token"})
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

// Test legacy X-API-Token header support
func TestAuthMiddleware_ValidXAPIToken(t *testing.T) {
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
	req.Header.Set("X-API-Token", "secret-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.True(t, nextCalled)
	assert.Equal(t, http.StatusOK, w.Code)
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
	w := httptest.NewRecorder()

	s.CreateSession(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Check cookie
	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "xg2g_session" {
			found = true
			assert.Equal(t, "secret", c.Value)
			assert.True(t, c.HttpOnly)
			assert.True(t, c.Secure) // ForceHTTPS=true
			assert.Equal(t, 24*time.Hour, time.Duration(c.MaxAge)*time.Second)
		}
	}
	assert.True(t, found, "xg2g_session cookie not found")
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
