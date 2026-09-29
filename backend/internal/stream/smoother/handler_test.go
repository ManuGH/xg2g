// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package smoother

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
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

func TestSmootherHandler_Relay(t *testing.T) {
	relayCalled := false
	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		relayCalled = true
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		pkt := createDummyTSPacket(100, 0, false, 0)
		_, _ = w.Write(pkt)
	}))
	defer relayServer.Close()

	cfg := DefaultConfig()
	cfg.StartupReservoirMs = 10.0
	cfg.PacerIntervalMs = 5.0
	handler := NewHandler("", 8001, cfg)

	b64Target := strings.TrimRight(strings.ReplaceAll(strings.ReplaceAll(
		relayServer.URL+"/stream.ts", "+", "-"), "/", "_"), "=")
	// Base64url encode
	b64Encoded := strings.TrimRight(
		strings.NewReplacer("+", "-", "/", "_").Replace(
			javaBase64(relayServer.URL+"/stream.ts"),
		),
		"=",
	)
	_ = b64Target

	tests := []struct {
		name       string
		path       string
		query      string
		wantStatus int
		wantUpcall bool
	}{
		{
			name:       "valid relay with base64url path",
			path:       "/api/v3/stream/smooth/relay/" + b64Encoded,
			wantStatus: http.StatusOK,
			wantUpcall: true,
		},
		{
			name:       "valid relay with url query param",
			path:       "/api/v3/stream/smooth/",
			query:      "url=" + url.QueryEscape(relayServer.URL+"/stream.ts"),
			wantStatus: http.StatusOK,
			wantUpcall: true,
		},
		{
			name:       "valid relay with b64 query param",
			path:       "/api/v3/stream/smooth/",
			query:      "b64=" + b64Encoded,
			wantStatus: http.StatusOK,
			wantUpcall: true,
		},
		{
			name:       "reject relay with unsupported scheme",
			path:       "/api/v3/stream/smooth/",
			query:      "url=ftp://evil.com/stream.ts",
			wantStatus: http.StatusBadRequest,
			wantUpcall: false,
		},
		{
			name:       "reject corrupt base64",
			path:       "/api/v3/stream/smooth/relay/???notbase64???",
			wantStatus: http.StatusBadRequest,
			wantUpcall: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			relayCalled = false
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
			if tc.wantUpcall != relayCalled {
				t.Errorf("relayCalled = %v, want %v", relayCalled, tc.wantUpcall)
			}
		})
	}
}

func javaBase64(s string) string {
	var b strings.Builder
	enc := []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/")
	data := []byte(s)
	for i := 0; i < len(data); i += 3 {
		v := uint32(data[i]) << 16
		if i+1 < len(data) {
			v |= uint32(data[i+1]) << 8
		}
		if i+2 < len(data) {
			v |= uint32(data[i+2])
		}
		b.WriteByte(enc[(v>>18)&0x3F])
		b.WriteByte(enc[(v>>12)&0x3F])
		if i+1 < len(data) {
			b.WriteByte(enc[(v>>6)&0x3F])
		} else {
			b.WriteByte('=')
		}
		if i+2 < len(data) {
			b.WriteByte(enc[v&0x3F])
		} else {
			b.WriteByte('=')
		}
	}
	return b.String()
}

func TestSmootherHandler_TranscodeRoute(t *testing.T) {
	cfg := DefaultConfig()
	cfg.StartupReservoirMs = 10.0
	handler := NewHandler("http://127.0.0.1:8001", 8001, cfg)

	// Valid target base64
	targetURL := "http://127.0.0.1:8080/stream.ts"
	b64Target := base64.RawURLEncoding.EncodeToString([]byte(targetURL))

	// Invalid target: unsupported scheme
	badSchemeURL := "ftp://127.0.0.1/stream.ts"
	b64BadScheme := base64.RawURLEncoding.EncodeToString([]byte(badSchemeURL))

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{
			name:       "reject unsupported scheme in transcode",
			path:       "/api/v3/stream/smooth/transcode/" + b64BadScheme,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "reject corrupt base64 in transcode",
			path:       "/api/v3/stream/smooth/transcode/???corrupt???",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "valid transcode target initiates pipeline",
			path:       "/api/v3/stream/smooth/transcode/" + b64Target,
			wantStatus: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil).WithContext(ctx)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}
