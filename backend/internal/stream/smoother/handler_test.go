// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package smoother

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/iptv/edge"
	"github.com/ManuGH/xg2g/internal/iptv/sourceref"
	"github.com/ManuGH/xg2g/internal/problemcode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSmootherHandler_ServiceRefValidation(t *testing.T) {
	// Mock upstream receiver that returns a short TS stream
	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalled = true
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		// Write dummy TS packet
		pkt := createDummyTSPacket(100, 0, false, 0)
		_, _ = w.Write(pkt)
	}))
	defer upstream.Close()

	u, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("failed to parse upstream URL: %v", err)
	}

	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("failed to parse upstream port: %v", err)
	}

	cfg := DefaultConfig()
	cfg.StartupReservoirMs = 10.0
	cfg.PacerIntervalMs = 5.0

	handler := NewHandler(upstream.URL, port, cfg)

	tests := []struct {
		name       string
		path       string
		query      string
		wantStatus int
		wantUpcall bool
	}{
		{
			name:       "valid DVB service reference in path",
			path:       "/api/v3/stream/smooth/1:0:19:132F:3EF:1:C00000:0:0:0:",
			wantStatus: http.StatusOK,
			wantUpcall: true,
		},
		{
			name:       "valid service reference in query",
			path:       "/api/v3/stream/smooth/",
			query:      "sref=1:0:19:132F:3EF:1:C00000:0:0:0:",
			wantStatus: http.StatusOK,
			wantUpcall: true,
		},
		{
			name:       "reject path traversal with dot-dot-slash",
			path:       "/api/v3/stream/smooth/../../web/powerstate",
			wantStatus: http.StatusBadRequest,
			wantUpcall: false,
		},
		{
			name:       "reject url-encoded path traversal",
			path:       "/api/v3/stream/smooth/%2e%2e%2fweb%2fpowerstate",
			wantStatus: http.StatusBadRequest,
			wantUpcall: false,
		},
		{
			name:       "reject query parameter injection in path",
			path:       "/api/v3/stream/smooth/1:0:19:132F:3EF:1:C00000:0:0:0:%3faction=reboot",
			wantStatus: http.StatusBadRequest,
			wantUpcall: false,
		},
		{
			name:       "reject query parameter injection in query param",
			path:       "/api/v3/stream/smooth/",
			query:      "sref=1:0:19:132F:3EF:1:C00000:0:0:0:?action=reboot",
			wantStatus: http.StatusBadRequest,
			wantUpcall: false,
		},
		{
			name:       "reject embedded at-symbol",
			path:       "/api/v3/stream/smooth/user:pass@evil.com",
			wantStatus: http.StatusBadRequest,
			wantUpcall: false,
		},
		{
			name:       "reject missing service ref",
			path:       "/api/v3/stream/smooth/",
			wantStatus: http.StatusBadRequest,
			wantUpcall: false,
		},
		{
			name:       "reject spaces and control characters",
			path:       "/api/v3/stream/smooth/1:0:19:132F%20bad",
			wantStatus: http.StatusBadRequest,
			wantUpcall: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			upstreamCalled = false
			reqURL := tc.path
			if tc.query != "" {
				reqURL += "?" + tc.query
			}

			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			req := httptest.NewRequest(http.MethodGet, reqURL, nil).WithContext(ctx)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d. Body: %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantUpcall != upstreamCalled {
				t.Errorf("upstreamCalled = %v, want %v", upstreamCalled, tc.wantUpcall)
			}
			if tc.wantStatus == http.StatusBadRequest && !strings.Contains(rec.Body.String(), "serviceRef") {
				t.Errorf("expected error message to mention serviceRef, got: %s", rec.Body.String())
			}
		})
	}
}

func TestSmootherHandler_IPTVInboundResolution(t *testing.T) {
	var requestedPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		pkt := createDummyTSPacket(100, 0, false, 0)
		_, _ = w.Write(pkt)
	}))
	defer upstream.Close()

	u, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(u.Port())
	require.NoError(t, err)

	cfg := DefaultConfig()
	cfg.StartupReservoirMs = 10.0
	cfg.PacerIntervalMs = 5.0

	secret := "secret-test-key-of-at-least-32-bytes!!"
	parser, err := sourceref.NewParser([]byte(secret))
	require.NoError(t, err)
	reg := sourceref.NewRegistry()
	rawCanary := "4097:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid/live/stream.ts:Canary"
	src, err := parser.Parse(rawCanary)
	require.NoError(t, err)
	require.NoError(t, reg.Replace([]sourceref.Source{src}))
	res := edge.NewResolver(reg, parser, nil)

	opaqueID := string(src.ID())

	handler := NewHandler(upstream.URL, port, cfg)
	handler.SetIPTVResolver(res)

	// 1. Opaque known: resolves and reaches upstream
	{
		requestedPath = ""
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		req := httptest.NewRequest(http.MethodGet, "/api/v3/stream/smooth/"+opaqueID, nil).WithContext(ctx)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, requestedPath, "canary.invalid")
	}

	// 2. Opaque unknown: returns 404 ProblemDetails, zero input echo
	{
		unknownID := "iptv_abcdefghijklmnopqrstuvwxyz"
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		req := httptest.NewRequest(http.MethodGet, "/api/v3/stream/smooth/"+unknownID, nil).WithContext(ctx)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.NotContains(t, rec.Body.String(), unknownID)
		assert.Contains(t, rec.Body.String(), problemcode.CodeNotFound)
	}

	// 3. Malformed iptv_ ID: returns 400 ProblemDetails, zero input echo
	{
		malformedID := "iptv_invalid_base32!!!"
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		req := httptest.NewRequest(http.MethodGet, "/api/v3/stream/smooth/"+malformedID, nil).WithContext(ctx)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.NotContains(t, rec.Body.String(), malformedID)
		assert.Contains(t, rec.Body.String(), problemcode.CodeInvalidInput)
	}

	// 4. Raw IPTV ref: passes through
	{
		requestedPath = ""
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		req := httptest.NewRequest(http.MethodGet, "/api/v3/stream/smooth/"+rawCanary, nil).WithContext(ctx)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, requestedPath, "canary.invalid")
	}

	// 5. Nil resolver: opaque returns 404, raw IPTV ref succeeds
	{
		handlerNil := NewHandler(upstream.URL, port, cfg)
		handlerNil.SetIPTVResolver(nil)

		// Opaque -> 404
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		req := httptest.NewRequest(http.MethodGet, "/api/v3/stream/smooth/"+opaqueID, nil).WithContext(ctx)
		rec := httptest.NewRecorder()
		handlerNil.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)

		// Raw -> 200
		ctx2, cancel2 := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel2()
		req2 := httptest.NewRequest(http.MethodGet, "/api/v3/stream/smooth/"+rawCanary, nil).WithContext(ctx2)
		rec2 := httptest.NewRecorder()
		handlerNil.ServeHTTP(rec2, req2)
		assert.Equal(t, http.StatusOK, rec2.Code)
	}
}
