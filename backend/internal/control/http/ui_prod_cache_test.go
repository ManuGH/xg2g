//go:build !dev

package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestUIHandler_NoImmutableCacheOn404 is L1's RED control. It exercises the actual vulnerable
// path: the !uiAvailable branch (UI bundle not built), where a missing asset 404s via
// http.NotFound. setUIHeaders stamps `public, max-age=31536000, immutable` for asset routes
// before serving, and unlike http.FileServer (which clears caching headers on its own 404),
// http.NotFound leaves them — so without the cacheControlGuard the client caches the 404 for a
// year. RED without the guard: the 404 carries the immutable Cache-Control.
func TestUIHandler_NoImmutableCacheOn404(t *testing.T) {
	h := uiHandler(UIConfig{CSP: "default-src 'self'"}, nil, false)

	req := httptest.NewRequest(http.MethodGet, "/assets/does-not-exist-abc123.js", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a missing asset, got %d", rec.Code)
	}
	cc := rec.Header().Get("Cache-Control")
	if strings.Contains(cc, "immutable") || strings.Contains(cc, "max-age=31536000") {
		t.Errorf("404 for a missing asset carries a long-lived cache header %q — the client would cache the failure", cc)
	}
}

// TestUIHandler_CacheHeadersPreservedOnSuccess guards the other side: the guard must only strip
// on >= 400. A 200 (here the HTML fallback) must keep its Cache-Control — otherwise the L1 fix
// would also kill the (no-cache) caching directives the success path intends. This is the
// counterpart that keeps "don't strip on 304/2xx" honest.
func TestUIHandler_CacheHeadersPreservedOnSuccess(t *testing.T) {
	h := uiHandler(UIConfig{CSP: "default-src 'self'"}, nil, false)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for the HTML fallback, got %d", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc == "" {
		t.Error("200 response lost its Cache-Control header — the guard must only strip on >= 400")
	}
}

// TestCacheControlGuard_StripBoundary pins the exact >= 400 boundary the guard depends on:
// 200 and 304 (successful revalidation of a hashed asset) keep cache headers; 404/500 strip.
func TestCacheControlGuard_StripBoundary(t *testing.T) {
	cases := []struct {
		code      int
		wantStrip bool
	}{
		{http.StatusOK, false},
		{http.StatusNotModified, false}, // 304: revalidation success must keep cache headers
		{http.StatusNotFound, true},
		{http.StatusInternalServerError, true},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		g := &cacheControlGuard{ResponseWriter: rec}
		g.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		g.WriteHeader(tc.code)
		got := rec.Header().Get("Cache-Control")
		if tc.wantStrip && got != "" {
			t.Errorf("code %d: expected cache headers stripped, got %q", tc.code, got)
		}
		if !tc.wantStrip && got == "" {
			t.Errorf("code %d: expected cache headers preserved, got empty", tc.code)
		}
	}
}
