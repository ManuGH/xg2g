// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func TestPromhttpExposure(t *testing.T) {
	srv := httptest.NewServer(promhttp.Handler())
	defer srv.Close()

	if _, err := srv.Client().Get(srv.URL); err != nil {
		t.Fatal(err)
	}
}

func TestRecordPlaylistFileValidity(t *testing.T) {
	tests := []struct {
		name     string
		fileType string
		valid    bool
	}{
		{
			name:     "valid m3u file",
			fileType: "m3u",
			valid:    true,
		},
		{
			name:     "invalid m3u file",
			fileType: "m3u",
			valid:    false,
		},
		{
			name:     "valid xmltv file",
			fileType: "xmltv",
			valid:    true,
		},
		{
			name:     "invalid xmltv file",
			fileType: "xmltv",
			valid:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This should not panic
			metrics.RecordPlaylistFileValidity(tt.fileType, tt.valid)

			// Verify the metric was recorded by scraping the metrics endpoint
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			promhttp.Handler().ServeHTTP(recorder, req)

			body := recorder.Body.String()

			// Check that the metric exists in the output
			if !strings.Contains(body, "xg2g_playlist_file_valid") {
				t.Error("expected xg2g_playlist_file_valid metric to be present")
			}

			// Check that the file type label is present
			expectedLabel := `type="` + tt.fileType + `"`
			if !strings.Contains(body, expectedLabel) {
				t.Errorf("expected label %q to be present in metrics output", expectedLabel)
			}
		})
	}
}

func TestRecordPlaylistFileValidity_MultipleTypes(t *testing.T) {
	// Record validity for both file types
	metrics.RecordPlaylistFileValidity("m3u", true)
	metrics.RecordPlaylistFileValidity("xmltv", false)

	// Scrape metrics
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	promhttp.Handler().ServeHTTP(recorder, req)

	body := recorder.Body.String()

	// Verify both types are present
	if !strings.Contains(body, `type="m3u"`) {
		t.Error("expected m3u type label in metrics")
	}
	if !strings.Contains(body, `type="xmltv"`) {
		t.Error("expected xmltv type label in metrics")
	}

	// Verify the metric name is present
	if !strings.Contains(body, "xg2g_playlist_file_valid") {
		t.Error("expected xg2g_playlist_file_valid metric")
	}
}
