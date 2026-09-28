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
