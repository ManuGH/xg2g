// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serveWithCompression(t *testing.T, contentType, acceptEncoding string) *httptest.ResponseRecorder {
	t.Helper()
	body := strings.Repeat("xg2g compressible payload ", 64) // big enough to be worth gzipping
	handler := Compression()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.com/x", nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestCompression_GzipsJSON(t *testing.T) {
	rec := serveWithCompression(t, "application/json", "gzip")
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
}

func TestCompression_GzipsHLSPlaylist(t *testing.T) {
	rec := serveWithCompression(t, "application/vnd.apple.mpegurl", "gzip")
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip for HLS playlist", got)
	}
}

// TestCompression_SkipsMediaSegment is the negative control: a transport-stream
// segment must pass through uncompressed even when the client offers gzip, so
// Range requests keep working and we don't waste CPU on already-compressed media.
func TestCompression_SkipsMediaSegment(t *testing.T) {
	rec := serveWithCompression(t, "video/mp2t", "gzip")
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want media segment left uncompressed", got)
	}
}

// TestCompression_NoAcceptEncoding confirms we never compress when the client
// doesn't ask for it.
func TestCompression_NoAcceptEncoding(t *testing.T) {
	rec := serveWithCompression(t, "application/json", "")
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want none when Accept-Encoding absent", got)
	}
}
