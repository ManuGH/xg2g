// SPDX-License-Identifier: MIT
package load

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/jobs"
)

// mockOWI simulates an Enigma2 receiver with many services
type mockOWI struct {
	numServices int
}

func (m *mockOWI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/bouquets":
		fmt.Fprintln(w, `{"bouquets": [["Favourites", "Favourites"]]}`)
	case r.URL.Path == "/api/getservices":
		fmt.Fprint(w, `{"services": [`)
		for i := 0; i < m.numServices; i++ {
			if i > 0 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintf(w, `{"servicename": "Channel %d", "servicereference": "1:0:1:%d:0:0:0:0:0:0:"}`, i+1, i+1)
		}
		fmt.Fprint(w, `]}`)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestLoad_1500Services(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load test in short mode")
	}

	// Setup mock receiver
	mock := &mockOWI{numServices: 1500}
	server := httptest.NewServer(mock)
	defer server.Close()

	// Setup config
	tmpDir := t.TempDir()
	cfg := config.AppConfig{
		DataDir:    tmpDir,
		OWIBase:    server.URL,
		Bouquet:    "Favourites",
		StreamPort: 8001,
		OWITimeout: 5 * time.Second,
	}

	// Measure memory before
	var m1, m2 runtime.MemStats
	runtime.ReadMemStats(&m1)

	start := time.Now()

	// Run refresh job
	status, err := jobs.Refresh(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	duration := time.Since(start)
	runtime.ReadMemStats(&m2)

	// Assertions
	if status.Channels != 1500 {
		t.Errorf("Expected 1500 channels, got %d", status.Channels)
	}

	// Performance thresholds
	if duration > 2*time.Second {
		t.Errorf("Refresh took too long: %v (threshold: 2s)", duration)
	}

	alloc := m2.TotalAlloc - m1.TotalAlloc
	t.Logf("Memory allocated: %d MB", alloc/1024/1024)
	t.Logf("Duration: %v", duration)

	// Verify output file integrity
	m3uPath := filepath.Join(tmpDir, "playlist.m3u")
	content, err := os.ReadFile(m3uPath)
	if err != nil {
		t.Fatalf("Failed to read playlist: %v", err)
	}

	lines := strings.Split(string(content), "\n")
	seenIDs := make(map[string]bool)
	count := 0

	for _, line := range lines {
		if strings.HasPrefix(line, "#EXTINF:") {
			count++

			// Check for empty name
			if strings.TrimSpace(line[strings.LastIndex(line, ",")+1:]) == "" {
				t.Errorf("Found empty channel name at line: %s", line)
			}

			// Extract tvg-id
			idStart := strings.Index(line, `tvg-id="`)
			if idStart != -1 {
				idStart += 8
				idEnd := strings.Index(line[idStart:], `"`)
				if idEnd != -1 {
					id := line[idStart : idStart+idEnd]
					if seenIDs[id] {
						t.Errorf("Duplicate tvg-id found: %s", id)
					}
					seenIDs[id] = true
				}
			}
		}
	}

	if count != 1500 {
		t.Errorf("Playlist contains %d channels, expected 1500", count)
	}
}
