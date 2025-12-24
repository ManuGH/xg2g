// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestScopeMiddleware_DenyWriteByDefault(t *testing.T) {
	s := &Server{
		cfg: config.AppConfig{
			APIToken: "secret",
		},
	}

	handler := s.authMiddleware(s.scopeMiddleware(ScopeV3Write)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest(http.MethodPost, "/api/v3/intents", nil)
	req.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestScopeMiddleware_WriteImpliesRead(t *testing.T) {
	s := &Server{
		cfg: config.AppConfig{
			APIToken:       "secret",
			APITokenScopes: []string{string(ScopeV3Write)},
		},
	}

	handler := s.authMiddleware(s.scopeMiddleware(ScopeV3Read)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest(http.MethodGet, "/api/v3/sessions", nil)
	req.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestScopeMiddleware_AnonymousReadOnly(t *testing.T) {
	s := &Server{
		cfg: config.AppConfig{
			AuthAnonymous: true,
		},
	}

	readHandler := s.authMiddleware(s.scopeMiddleware(ScopeV3Read)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	readReq := httptest.NewRequest(http.MethodGet, "/api/v3/sessions", nil)
	readResp := httptest.NewRecorder()
	readHandler.ServeHTTP(readResp, readReq)
	assert.Equal(t, http.StatusOK, readResp.Code)

	writeHandler := s.authMiddleware(s.scopeMiddleware(ScopeV3Write)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	writeReq := httptest.NewRequest(http.MethodPost, "/api/v3/intents", nil)
	writeResp := httptest.NewRecorder()
	writeHandler.ServeHTTP(writeResp, writeReq)
	assert.Equal(t, http.StatusForbidden, writeResp.Code)
}

func TestScopeMiddleware_TokenList(t *testing.T) {
	s := &Server{
		cfg: config.AppConfig{
			APITokens: []config.ScopedToken{
				{Token: "scoped", Scopes: []string{string(ScopeV3Write)}},
			},
		},
	}

	handler := s.authMiddleware(s.scopeMiddleware(ScopeV3Write)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest(http.MethodPost, "/api/v3/intents", nil)
	req.Header.Set("Authorization", "Bearer scoped")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
