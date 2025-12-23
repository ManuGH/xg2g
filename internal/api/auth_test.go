// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package api

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
	// When no token is configured, authentication is disabled (unless Anonymous is false? Logic says if token=="" check AuthAnon)
	// Code: if token=="" { if authAnon { allow } else { fail_closed } }

	// Case 1: AuthAnonymous = true
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	s := &Server{
		cfg: config.AppConfig{
			APIToken:      "",
			AuthAnonymous: true,
		},
	}
	handler := s.authMiddleware(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.True(t, nextCalled, "next handler should be called when auth is anonymous")
	assert.Equal(t, http.StatusOK, w.Code)

	// Case 2: AuthAnonymous = false (Fail Closed)
	nextCalled = false
	s.cfg.AuthAnonymous = false
	handler = s.authMiddleware(next)
	w = httptest.NewRecorder()
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
			APIToken: "secret-token",
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
			APIToken: "secret-token",
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
			APIToken: "secret-token",
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

func TestAuthMiddleware_InvalidToken(t *testing.T) {
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
	req.Header.Set("Authorization", "Bearer wrong-token")
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
			APIToken: "secret-token",
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

func TestHandleSessionLogin(t *testing.T) {
	s := &Server{
		cfg: config.AppConfig{
			APIToken:   "secret",
			ForceHTTPS: true,
		},
	}

	// Auth success
	req := httptest.NewRequest("POST", "/api/v2/auth/session", nil)
	req.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()

	s.HandleSessionLogin(w, req)

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
