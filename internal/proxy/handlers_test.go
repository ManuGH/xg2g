// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRoutingDecision_Precedence(t *testing.T) {
	lookup := func(string) bool { return true }

	type testCase struct {
		name         string
		path         string
		headers      map[string]string
		query        string
		wantDecision string
		wantReason   string
	}

	tests := []testCase{
		{
			name:         "query ts overrides accept hls",
			path:         "/1:0:1",
			query:        "mode=ts",
			headers:      map[string]string{"Accept": "application/vnd.apple.mpegurl"},
			wantDecision: routeDecisionTS,
			wantReason:   routeReasonQuery,
		},
		{
			name:         "query hls overrides accept ts",
			path:         "/1:0:1",
			query:        "hls=1",
			headers:      map[string]string{"Accept": "video/mp2t"},
			wantDecision: routeDecisionHLS,
			wantReason:   routeReasonQuery,
		},
		{
			name:         "accept hls",
			path:         "/1:0:1",
			headers:      map[string]string{"Accept": "application/vnd.apple.mpegurl"},
			wantDecision: routeDecisionHLS,
			wantReason:   routeReasonAccept,
		},
		{
			name:         "accept ts wins over fetch",
			path:         "/1:0:1",
			headers:      map[string]string{"Accept": "video/mp2t", "Sec-Fetch-Dest": "video"},
			wantDecision: routeDecisionTS,
			wantReason:   routeReasonAccept,
		},
		{
			name:         "fetch metadata",
			path:         "/1:0:1",
			headers:      map[string]string{"Sec-Fetch-Dest": "video"},
			wantDecision: routeDecisionHLS,
			wantReason:   routeReasonFetch,
		},
		{
			name:         "default for ref",
			path:         "/1:0:1",
			wantDecision: routeDecisionHLS,
			wantReason:   routeReasonDefault,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := tt.path
			if tt.query != "" {
				url = url + "?" + tt.query
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			decision := decideStreamRouting(req.URL.Path, req, lookup)
			if decision.decision != tt.wantDecision {
				t.Fatalf("decision mismatch: got %q want %q", decision.decision, tt.wantDecision)
			}
			if decision.reason != tt.wantReason {
				t.Fatalf("reason mismatch: got %q want %q", decision.reason, tt.wantReason)
			}
		})
	}
}

func TestRoutingDecision_DefaultForSlug(t *testing.T) {
	lookup := func(id string) bool { return id == "known" }
	req := httptest.NewRequest(http.MethodGet, "/known", nil)
	decision := decideStreamRouting(req.URL.Path, req, lookup)
	if decision.decision != routeDecisionHLS {
		t.Fatalf("decision mismatch: got %q want %q", decision.decision, routeDecisionHLS)
	}
	if decision.reason != routeReasonDefault {
		t.Fatalf("reason mismatch: got %q want %q", decision.reason, routeReasonDefault)
	}
}

func TestRoutingDecision_CompatRegression(t *testing.T) {
	tests := []string{
		"/metrics",
		"/healthz",
		"/readyz",
		"/files/playlist.m3u",
		"/discover.json",
		"/lineup.json",
		"/device.xml",
		"/api/v2/status",
	}

	lookup := func(string) bool { return false }
	for _, path := range tests {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept", "application/vnd.apple.mpegurl")
		req.Header.Set("Sec-Fetch-Dest", "video")
		decision := decideStreamRouting(path, req, lookup)
		if decision.decision != routeDecisionProxy || decision.reason != routeReasonGateReject {
			t.Fatalf("expected %s to avoid auto HLS routing", path)
		}
	}
}

func TestRoutingDecision_StreamPathGateReject(t *testing.T) {
	lookup := func(string) bool { return false }
	req := httptest.NewRequest(http.MethodGet, "/stream/unknown", nil)
	req.Header.Set("Accept", "application/vnd.apple.mpegurl")
	req.Header.Set("Sec-Fetch-Dest", "video")
	decision := decideStreamRouting(req.URL.Path, req, lookup)
	if decision.decision != routeDecisionProxy || decision.reason != routeReasonGateReject {
		t.Fatalf("expected stream-like path to avoid auto HLS routing when gate rejects")
	}
}

func TestRoutingDecision_UnknownSlug(t *testing.T) {
	lookup := func(id string) bool { return id == "known" }
	req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
	req.Header.Set("Accept", "application/vnd.apple.mpegurl")
	req.Header.Set("Sec-Fetch-Dest", "video")
	decision := decideStreamRouting(req.URL.Path, req, lookup)
	if decision.decision != routeDecisionProxy || decision.reason != routeReasonGateReject {
		t.Fatalf("expected unknown slug to avoid auto HLS routing")
	}
}
