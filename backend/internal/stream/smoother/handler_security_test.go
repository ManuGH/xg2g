// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package smoother

import (
	"context"
	"errors"
	"io"
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

type mockConnector struct {
	dials  atomic.Int32
	dialFn func(ctx context.Context, key session.SessionKey) (io.ReadCloser, error)
}

func (m *mockConnector) Connect(ctx context.Context, key session.SessionKey) (io.ReadCloser, error) {
	m.dials.Add(1)
	if m.dialFn != nil {
		return m.dialFn(ctx, key)
	}
	return nil, errors.New("mock upstream not configured")
}

func TestSmootherHandler_Security_InvalidServiceRefs(t *testing.T) {
	conn := &mockConnector{}
	mgr := session.NewManager(session.DefaultManagerConfig(), conn)
	handler := NewHandlerWithManager(mgr, "127.0.0.1", 8001, DefaultConfig())

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
			req := httptest.NewRequest(http.MethodGet, "/api/v3/stream/smooth/"+tc.ref, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code, "expected 400 for ref %q", tc.ref)

			// Test via query param
			reqQuery := httptest.NewRequest(http.MethodGet, "/api/v3/stream/smooth/?sref="+url.QueryEscape(tc.ref), nil)
			wQuery := httptest.NewRecorder()
			handler.ServeHTTP(wQuery, reqQuery)
			assert.Equal(t, http.StatusBadRequest, wQuery.Code, "expected 400 for query ref %q", tc.ref)
		})
	}
	assert.Equal(t, int32(0), conn.dials.Load(), "invalid refs must never dial upstream")
}

func TestSmootherHandler_Security_UnmanagedFailClosed(t *testing.T) {
	// Handler without session manager must fail closed
	handler := NewHandler("127.0.0.1", 8001, DefaultConfig())

	req := httptest.NewRequest(http.MethodGet, "/api/v3/stream/smooth/1:0:1:1:1:1:0:0:0:0:", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestSmootherHandler_Security_DisablesRedirects(t *testing.T) {
	handler := NewHandlerWithManager(nil, "127.0.0.1", 8001, DefaultConfig())
	require.NotNil(t, handler.client)
	require.NotNil(t, handler.client.CheckRedirect)

	err := handler.client.CheckRedirect(nil, nil)
	assert.Equal(t, http.ErrUseLastResponse, err, "smoother client must return http.ErrUseLastResponse on redirect")
}

func TestSmootherHandler_Security_CoalescingAndAdmissionLimits(t *testing.T) {
	conn := &mockConnector{}
	mgr := session.NewManager(session.DefaultManagerConfig(), conn)
	handler := NewHandlerWithManager(mgr, "127.0.0.1", 8001, DefaultConfig())

	// When upstream fails/admission denied:
	req := httptest.NewRequest(http.MethodGet, "/api/v3/stream/smooth/1:0:1:1:1:1:0:0:0:0:", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.Equal(t, int32(1), conn.dials.Load(), "exactly one dial attempt should be made")

	// Concurrent requests for the same service ref coalesce in session.Manager:
	conn.dialFn = func(ctx context.Context, key session.SessionKey) (io.ReadCloser, error) {
		pr, _ := io.Pipe()
		return pr, nil
	}

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	req1 := httptest.NewRequest(http.MethodGet, "/api/v3/stream/smooth/1:0:1:2:2:2:0:0:0:0:", nil).WithContext(ctx1)

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	req2 := httptest.NewRequest(http.MethodGet, "/api/v3/stream/smooth/1:0:1:2:2:2:0:0:0:0:", nil).WithContext(ctx2)

	dialsBefore := conn.dials.Load()

	// Acquire simultaneously
	go func() {
		w1 := httptest.NewRecorder()
		handler.ServeHTTP(w1, req1)
	}()

	time.Sleep(20 * time.Millisecond)

	w2 := httptest.NewRecorder()
	// Cancel after brief wait so it returns
	time.AfterFunc(50*time.Millisecond, cancel2)
	handler.ServeHTTP(w2, req2)

	dialsAfter := conn.dials.Load()
	assert.Equal(t, dialsBefore+1, dialsAfter, "second concurrent request for same service must coalesce and not trigger second dial")
}
