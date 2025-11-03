// SPDX-License-Identifier: MIT

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseBearer(t *testing.T) {
	tests := []struct {
		name      string
		auth      string
		wantToken string
	}{
		{
			name:      "valid bearer token",
			auth:      "Bearer abc123",
			wantToken: "abc123",
		},
		{
			name:      "bearer with spaces",
			auth:      "Bearer   xyz789  ",
			wantToken: "xyz789",
		},
		{
			name:      "missing bearer prefix",
			auth:      "abc123",
			wantToken: "",
		},
		{
			name:      "empty string",
			auth:      "",
			wantToken: "",
		},
		{
			name:      "wrong scheme",
			auth:      "Basic abc123",
			wantToken: "",
		},
		{
			name:      "bearer lowercase",
			auth:      "bearer token123",
			wantToken: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseBearer(tt.auth)
			assert.Equal(t, tt.wantToken, got)
		})
	}
}

func TestAuthenticate_NoTokenConfigured(t *testing.T) {
	// When no token is configured, authentication is disabled
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	handler := authenticate(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// Simulate no token in context
	req = req.WithContext(context.WithValue(req.Context(), serverConfigKey{}, ""))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.True(t, nextCalled, "next handler should be called when auth is disabled")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthenticate_MissingAuthHeader(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not be called")
	})

	handler := authenticate(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// Simulate token configured in context
	req = req.WithContext(context.WithValue(req.Context(), serverConfigKey{}, "secret-token"))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthenticate_ValidBearerToken(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		// Verify principal was set in context
		principal := principalFrom(r.Context())
		assert.Equal(t, "authenticated", principal)
		w.WriteHeader(http.StatusOK)
	})

	handler := authenticate(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	req = req.WithContext(context.WithValue(req.Context(), serverConfigKey{}, "secret-token"))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.True(t, nextCalled)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthenticate_ValidXAPIToken(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	handler := authenticate(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Token", "secret-token")
	req = req.WithContext(context.WithValue(req.Context(), serverConfigKey{}, "secret-token"))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.True(t, nextCalled)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthenticate_InvalidToken(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not be called")
	})

	handler := authenticate(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	req = req.WithContext(context.WithValue(req.Context(), serverConfigKey{}, "secret-token"))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestPrincipalFrom(t *testing.T) {
	tests := []struct {
		name          string
		ctx           context.Context
		wantPrincipal string
	}{
		{
			name:          "with principal",
			ctx:           context.WithValue(context.Background(), ctxPrincipalKey{}, "alice"),
			wantPrincipal: "alice",
		},
		{
			name:          "without principal",
			ctx:           context.Background(),
			wantPrincipal: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := principalFrom(tt.ctx)
			assert.Equal(t, tt.wantPrincipal, got)
		})
	}
}
