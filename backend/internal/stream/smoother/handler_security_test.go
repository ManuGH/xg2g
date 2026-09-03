// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package smoother_test

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

	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
	"github.com/ManuGH/xg2g/internal/stream/ingest/session"
	"github.com/ManuGH/xg2g/internal/stream/smoother"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockPipeline struct {
	ring *ring.MasterRing
}

func (m *mockPipeline) PrimedAttachWithTimeout(ctx context.Context, timeout time.Duration) (ring.PrimedAttachPoint, *ring.SubscriberReader, error) {
	if m.ring == nil {
		m.ring = ring.NewMasterRing(1024 * 1024)
	}
	sub := m.ring.NewSubscriberReader(0)
	return ring.PrimedAttachPoint{}, sub, nil
}

type mockPipelineHolder struct {
	io.ReadCloser
	pipe smoother.AttachablePipeline
}

func (h *mockPipelineHolder) Pipeline() any {
	return h.pipe
}

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
	handler := smoother.NewHandlerWithManager(mgr, "127.0.0.1", 8001, smoother.DefaultConfig())

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
	handler := smoother.NewHandler("127.0.0.1", 8001, smoother.DefaultConfig())

	req := httptest.NewRequest(http.MethodGet, "/api/v3/stream/smooth/1:0:1:1:1:1:0:0:0:0:", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestSmootherHandler_Security_CoalescingMatrix(t *testing.T) {
	pipe := &mockPipeline{ring: ring.NewMasterRing(1024 * 1024)}
	conn := &mockConnector{
		dialFn: func(ctx context.Context, key session.SessionKey) (io.ReadCloser, error) {
			pr, pw := io.Pipe()
			go func() {
				<-ctx.Done()
				_ = pw.Close()
			}()
			return &mockPipelineHolder{ReadCloser: pr, pipe: pipe}, nil
		},
	}

	mgr := session.NewManager(session.DefaultManagerConfig(), conn)
	smootherHandler := smoother.NewHandlerWithManager(mgr, "127.0.0.1", 8001, smoother.DefaultConfig())

	key := session.NewSessionKey("127.0.0.1", 8001, "1:0:1:283D:3FB:1:C00000:0:0:0:")
	key.TargetProgram = 0x283D

	// 1. First client acquires via session.Manager directly (simulating Live subscriber)
	sess1, err := mgr.Acquire(context.Background(), key)
	require.NoError(t, err)
	defer sess1.Release()

	assert.Equal(t, int32(1), conn.dials.Load(), "initial live acquire must perform exactly 1 dial")

	// 2. Second client arrives via smootherHandler for the SAME service ref
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	req2 := httptest.NewRequest(http.MethodGet, "/api/v3/stream/smooth/1:0:1:283D:3FB:1:C00000:0:0:0:", nil).WithContext(ctx2)

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel2() // cancel to finish HTTP request
	}()

	w2 := httptest.NewRecorder()
	smootherHandler.ServeHTTP(w2, req2)

	// Dial count must still be 1 (coalesced!)
	assert.Equal(t, int32(1), conn.dials.Load(), "concurrent smoother client must coalesce into existing upstream session")
}

func TestSmootherHandler_Security_AdmissionDeniedFailsClosed(t *testing.T) {
	conn := &mockConnector{
		dialFn: func(ctx context.Context, key session.SessionKey) (io.ReadCloser, error) {
			return nil, errors.New("topology full: no tuner lease available")
		},
	}
	mgr := session.NewManager(session.DefaultManagerConfig(), conn)
	handler := smoother.NewHandlerWithManager(mgr, "127.0.0.1", 8001, smoother.DefaultConfig())

	req := httptest.NewRequest(http.MethodGet, "/api/v3/stream/smooth/1:0:1:1:1:1:0:0:0:0:", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadGateway, w.Code, "admission rejection must fail-closed with 502")
	assert.Equal(t, int32(1), conn.dials.Load(), "only 1 dial attempt made")
}
