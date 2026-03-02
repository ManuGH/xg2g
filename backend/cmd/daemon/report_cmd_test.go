package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReportRedaction(t *testing.T) {
	// 1. Setup Mock Server to serve status (mimicking API)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header is NOT in the status body (sanity check)
		token := r.Header.Get("Authorization")
		if token != "Bearer supersecret-token-123" {
			t.Errorf("Expected bearer token, got: %s", token)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"healthy","runtime":{"go":"1.25.3"}}`))
	}))
	defer ts.Close()

	// Extract port from URL
	port := 0
	// format: http://127.0.0.1:xxxxx
	parts := strings.Split(ts.URL, ":")
	if len(parts) == 3 {
		// port is parts[2]
		// quick parse manually or sscanf
		var p int
		fmt.Sscanf(parts[2], "%d", &p)
		port = p
	}

	// 2. Generate Report
	// We use a known secret token
	secretToken := "supersecret-token-123"
	report := buildReportData(port, secretToken)

	// 3. Verify Redaction (Secrets NOT present)
	// Marshal to string to search easily
	bytes, _ := json.Marshal(report)
	out := string(bytes)

	if strings.Contains(out, secretToken) {
		t.Fatalf("Report contained secret token! Leak detected.")
	}

	// 4. Verify Whitelist Fields (Fingerprint present)
	if !strings.Contains(out, "fingerprint") {
		t.Errorf("Report missing fingerprint")
	}
	if !strings.Contains(out, "go_version") {
		t.Errorf("Report missing go_version")
	}

	// 5. Bounds Check
	if len(bytes) > 256*1024 { // 256KB limit
		t.Errorf("Report too large: %d bytes", len(bytes))
	}
}
