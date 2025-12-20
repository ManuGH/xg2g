// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestShouldAutoRouteHLS_QueryOverrides(t *testing.T) {
	lookup := func(string) bool { return true }

	req := httptest.NewRequest(http.MethodGet, "/1:0:1?mode=ts", nil)
	req.Header.Set("Accept", "application/vnd.apple.mpegurl")
	if shouldAutoRouteHLS(req.URL.Path, req, lookup) {
		t.Fatalf("expected mode=ts to disable auto HLS")
	}

	req = httptest.NewRequest(http.MethodGet, "/1:0:1?hls=1", nil)
	req.Header.Set("Accept", "video/mp2t")
	if !shouldAutoRouteHLS(req.URL.Path, req, lookup) {
		t.Fatalf("expected hls=1 to enable auto HLS")
	}
}

func TestShouldAutoRouteHLS_AcceptHeaders(t *testing.T) {
	lookup := func(string) bool { return true }

	req := httptest.NewRequest(http.MethodGet, "/1:0:1", nil)
	req.Header.Set("Accept", "application/vnd.apple.mpegurl")
	if !shouldAutoRouteHLS(req.URL.Path, req, lookup) {
		t.Fatalf("expected HLS accept header to enable auto HLS")
	}

	req = httptest.NewRequest(http.MethodGet, "/1:0:1", nil)
	req.Header.Set("Accept", "video/mp2t")
	if shouldAutoRouteHLS(req.URL.Path, req, lookup) {
		t.Fatalf("expected TS accept header to disable auto HLS")
	}
}

func TestShouldAutoRouteHLS_FetchMetadata(t *testing.T) {
	lookup := func(string) bool { return true }

	req := httptest.NewRequest(http.MethodGet, "/1:0:1", nil)
	req.Header.Set("Sec-Fetch-Dest", "video")
	if !shouldAutoRouteHLS(req.URL.Path, req, lookup) {
		t.Fatalf("expected Sec-Fetch-Dest=video to enable auto HLS")
	}

	req = httptest.NewRequest(http.MethodGet, "/1:0:1", nil)
	req.Header.Set("Sec-CH-UA", "\"Chromium\";v=\"120\"")
	if !shouldAutoRouteHLS(req.URL.Path, req, lookup) {
		t.Fatalf("expected Sec-CH-UA to enable auto HLS")
	}
}

func TestShouldAutoRouteHLS_DefaultsToHLS(t *testing.T) {
	lookup := func(string) bool { return true }

	req := httptest.NewRequest(http.MethodGet, "/1:0:1", nil)
	if !shouldAutoRouteHLS(req.URL.Path, req, lookup) {
		t.Fatalf("expected default to be HLS when client is ambiguous")
	}
}

func TestShouldAutoRouteHLS_NonStreamPaths(t *testing.T) {
	tests := []string{
		"/metrics",
		"/healthz",
		"/readyz",
		"/files/playlist.m3u",
		"/discover.json",
		"/lineup.json",
		"/device.xml",
		"/stream/unknown",
		"/api/v2/status",
	}

	lookup := func(string) bool { return false }
	for _, path := range tests {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept", "application/vnd.apple.mpegurl")
		req.Header.Set("Sec-Fetch-Dest", "video")
		if shouldAutoRouteHLS(path, req, lookup) {
			t.Fatalf("expected %s to avoid auto HLS routing", path)
		}
	}
}

func TestShouldAutoRouteHLS_KnownSlug(t *testing.T) {
	lookup := func(id string) bool { return id == "known" }

	req := httptest.NewRequest(http.MethodGet, "/known", nil)
	if !shouldAutoRouteHLS(req.URL.Path, req, lookup) {
		t.Fatalf("expected known slug to be considered stream-like")
	}

	req = httptest.NewRequest(http.MethodGet, "/unknown", nil)
	if shouldAutoRouteHLS(req.URL.Path, req, lookup) {
		t.Fatalf("expected unknown slug to avoid auto HLS routing")
	}
}
