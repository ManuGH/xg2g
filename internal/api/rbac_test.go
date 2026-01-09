// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/stretchr/testify/assert"
)

func TestScopeMiddleware_DenyWriteByDefault(t *testing.T) {
	s := New(config.AppConfig{
		APIToken:       "secret",
		APITokenScopes: []string{string(v3.ScopeV3Read)},
		Streaming: config.StreamingConfig{
			DeliveryPolicy: "universal",
		},
	}, config.NewManager(""))

	handler := s.authMiddleware(s.scopeMiddleware(v3.ScopeV3Write)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest(http.MethodPost, "/api/v3/intents", nil)
	req.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestScopeMiddleware_WriteImpliesRead(t *testing.T) {
	s := New(config.AppConfig{
		APIToken:       "secret",
		APITokenScopes: []string{string(v3.ScopeV3Write)},
		Streaming: config.StreamingConfig{
			DeliveryPolicy: "universal",
		},
	}, config.NewManager(""))

	handler := s.authMiddleware(s.scopeMiddleware(v3.ScopeV3Read)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest(http.MethodGet, "/api/v3/sessions", nil)
	req.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestScopeMiddleware_TokenList(t *testing.T) {
	s := New(config.AppConfig{
		APITokens: []config.ScopedToken{
			{Token: "scoped", Scopes: []string{string(v3.ScopeV3Write)}},
		},
		Streaming: config.StreamingConfig{
			DeliveryPolicy: "universal",
		},
	}, config.NewManager(""))

	handler := s.authMiddleware(s.scopeMiddleware(v3.ScopeV3Write)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest(http.MethodPost, "/api/v3/intents", nil)
	req.Header.Set("Authorization", "Bearer scoped")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestScopeMiddleware_EmptyScopesUnauthorized(t *testing.T) {
	t.Run("api_token", func(t *testing.T) {
		s := New(config.AppConfig{
			APIToken:       "secret",
			APITokenScopes: nil,
			Streaming: config.StreamingConfig{
				DeliveryPolicy: "universal",
			},
		}, config.NewManager(""))

		handler := s.authMiddleware(s.scopeMiddleware(v3.ScopeV3Read)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})))

		req := httptest.NewRequest(http.MethodGet, "/api/v3/sessions", nil)
		req.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("token_list", func(t *testing.T) {
		s := New(config.AppConfig{
			APITokens: []config.ScopedToken{
				{Token: "scoped", Scopes: nil},
			},
			Streaming: config.StreamingConfig{
				DeliveryPolicy: "universal",
			},
		}, config.NewManager(""))

		handler := s.authMiddleware(s.scopeMiddleware(v3.ScopeV3Read)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})))

		req := httptest.NewRequest(http.MethodGet, "/api/v3/sessions", nil)
		req.Header.Set("Authorization", "Bearer scoped")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}
