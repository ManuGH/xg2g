// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package pipeline

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLiveHandler_Security_InvalidServiceRefs(t *testing.T) {
	cfg := DefaultConnectorConfig("127.0.0.1", 8001)
	mgr := session.NewManager(session.DefaultManagerConfig(), NewLivePipelineConnector(cfg))
	handler := NewHandlerWithReceiver(mgr, "127.0.0.1", 8001)

	invalidRefs := []struct {
		name string
		ref  string
	}{
		{"empty", ""},
		{"slash", "/"},
		{"double_slash", "//"},
		{"dot_dot", ".."},
		{"path_traversal_prefix", "../1:0:1:1:1:1:0:0:0:0:"},
		{"path_traversal_encoded", "%2e%2e%2f1:0:1:1:1:1:0:0:0:0:"},
		{"slash_encoded", "1:0:1:1:1:1:0:0:0:0:%2Fsecret"},
		{"query_encoded", "1:0:1:1:1:1:0:0:0:0:%3Fadmin=1"},
		{"fragment_encoded", "1:0:1:1:1:1:0:0:0:0:%23segment"},
		{"backslash", "1:0:1:1:1:1:0:0:0:0:\\windows"},
		{"null_byte_encoded", "1:0:1:1:1:1:0:0:0:0:%00"},
		{"crlf_encoded", "1:0:1:1:1:1:0:0:0:0:%0D%0Aevil"},
		{"no_colons", "malicious_string_without_colons"},
		{"spaces_only", "%20%20%20"},
	}

	for _, tc := range invalidRefs {
		t.Run(tc.name, func(t *testing.T) {
			// Test via path
			req := httptest.NewRequest(http.MethodGet, "/api/v3/stream/live/"+tc.ref, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code, "expected 400 for ref %q", tc.ref)

			// Test via query param
			reqQuery := httptest.NewRequest(http.MethodGet, "/api/v3/stream/live/?sref="+url.QueryEscape(tc.ref), nil)
			wQuery := httptest.NewRecorder()
			handler.ServeHTTP(wQuery, reqQuery)
			assert.Equal(t, http.StatusBadRequest, wQuery.Code, "expected 400 for query ref %q", tc.ref)
		})
	}
}

func TestLiveHandler_Security_ValidServiceRefAccepted(t *testing.T) {
	cfg := DefaultConnectorConfig("127.0.0.1", 8001)
	mgr := session.NewManager(session.DefaultManagerConfig(), NewLivePipelineConnector(cfg))
	handler := NewHandlerWithReceiver(mgr, "127.0.0.1", 8001)

	validRefs := []string{
		"1:0:19:283D:3FB:1:C00000:0:0:0:",
		"1:0:1:1:1:1:0:0:0:0:",
		"1:0:19:132F:3EF:1:C00000:0:0:0",
	}

	for _, ref := range validRefs {
		t.Run(ref, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(50*time.Millisecond, cancel)
			req := httptest.NewRequest(http.MethodGet, "/api/v3/stream/live/"+ref, nil).WithContext(ctx)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			// It should pass validation (and fail downstream or return 200 before cancel, but never 400 Bad Request)
			assert.NotEqual(t, http.StatusBadRequest, w.Code, "valid ref %q must not be rejected with 400", ref)
		})
	}
}

func TestLiveConnector_Security_DisablesRedirects(t *testing.T) {
	var targetHit atomic.Bool
	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHit.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer targetSrv.Close()

	redirectSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, targetSrv.URL, http.StatusMovedPermanently)
	}))
	defer redirectSrv.Close()

	cfg := DefaultConnectorConfig(redirectSrv.URL, 8001)
	connector := NewLivePipelineConnector(cfg)

	key := session.NewSessionKey("127.0.0.1", 8001, "1:0:1:1:1:1:0:0:0:0:")
	_, _, err := connector.dialHTTP(context.Background(), key)
	require.Error(t, err, "301 redirect must be rejected as an error by NewSessionPipeline or dialHTTP")
	assert.False(t, targetHit.Load(), "redirect target must never be visited")
}
