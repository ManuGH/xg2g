// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.
package jobs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
)

func TestRefresh_IntegrationSuccess(t *testing.T) {
	// Spin up a stub OpenWebIF server
	mux := http.NewServeMux()

	// Bouquets endpoint returns a single bouquet named "Favourites"
	mux.HandleFunc("/api/bouquets", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"bouquets": [][]string{{"1:7:TEST:REF:", "Favourites"}},
		})
	})

	// getservices must check bouquet ref (uses sRef parameter)
	mux.HandleFunc("/api/getservices", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		sref := r.URL.Query().Get("sRef")
		if sref == "" {
			t.Fatalf("missing sRef in request")
		}
		// Respond with two services
		_ = json.NewEncoder(w).Encode(map[string]any{
			"services": []map[string]string{
				{"servicename": "Chan A", "servicereference": "1:0:19:AAA:1:1:C00000:0:0:0:"},
				{"servicename": "Chan B", "servicereference": "1:0:19:BBB:1:1:C00000:0:0:0:"},
			},
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Configure refresh to use the stub server
	tmp := t.TempDir()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.AppConfig{
		DataDir: tmp,
		Enigma2: config.Enigma2Settings{
			BaseURL:    u.String(),
			StreamPort: 8001,
		},
		Bouquet:   "Favourites",
		XMLTVPath: "xmltv.xml",
	}

	snap := config.BuildSnapshot(cfg, config.ReadOSRuntimeEnvOrDefault())
	status, err := Refresh(context.Background(), snap)
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	if status == nil || status.Channels != 2 {
		t.Fatalf("unexpected status: %#v", status)
	}

	// Verify files were written
	if _, err := os.Stat(filepath.Join(tmp, snap.Runtime.PlaylistFilename)); err != nil {
		t.Fatalf("playlist not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "xmltv.xml")); err != nil {
		t.Fatalf("xmltv.xml not written: %v", err)
	}
}

func TestRefresh_DoesNotPrewarmPiconsWithoutRuntimePool(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/bouquets", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"bouquets": [][]string{{"1:7:TEST:REF:", "Favourites"}},
		})
	})
	mux.HandleFunc("/api/getservices", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"services": []map[string]string{
				{"servicename": "Chan A", "servicereference": "1:0:19:AAA:1:1:C00000:0:0:0:"},
			},
		})
	})
	mux.HandleFunc("/picon/", func(http.ResponseWriter, *http.Request) {
		t.Fatalf("unexpected picon fetch without configured runtime pool")
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	tmp := t.TempDir()
	cfg := config.AppConfig{
		DataDir:   tmp,
		PiconBase: srv.URL,
		Enigma2: config.Enigma2Settings{
			BaseURL:    srv.URL,
			StreamPort: 8001,
		},
		Bouquet: "Favourites",
	}

	snap := config.BuildSnapshot(cfg, config.ReadOSRuntimeEnvOrDefault())
	status, err := Refresh(context.Background(), snap)
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	if status == nil || status.Channels != 1 {
		t.Fatalf("unexpected status: %#v", status)
	}

	if _, err := os.Stat(filepath.Join(tmp, "picons")); !os.IsNotExist(err) {
		t.Fatalf("picon cache dir should not exist without runtime pool, err=%v", err)
	}
}

func TestRefresh_WithRuntimePoolPrewarmsPicons(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/bouquets", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"bouquets": [][]string{{"1:7:TEST:REF:", "Favourites"}},
		})
	})
	serviceRef := "1:0:19:AAA:1:1:C00000:0:0:0:"
	mux.HandleFunc("/api/getservices", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"services": []map[string]string{
				{"servicename": "Chan A", "servicereference": serviceRef},
			},
		})
	})
	mux.HandleFunc("/picon/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, ".png") {
			t.Fatalf("unexpected picon path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("fake-png"))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	tmp := t.TempDir()
	cfg := config.AppConfig{
		DataDir:   tmp,
		PiconBase: srv.URL,
		Enigma2: config.Enigma2Settings{
			BaseURL:    srv.URL,
			StreamPort: 8001,
		},
		Bouquet: "Favourites",
	}

	pool, err := NewPiconPoolForConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewPiconPoolForConfig returned error: %v", err)
	}
	defer pool.Stop()

	snap := config.BuildSnapshot(cfg, config.ReadOSRuntimeEnvOrDefault())
	status, err := RefreshWithOptions(context.Background(), snap, WithPiconPool(pool))
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	if status == nil || status.Channels != 1 {
		t.Fatalf("unexpected status: %#v", status)
	}

	storeRef := strings.TrimRight(strings.ReplaceAll(serviceRef, ":", "_"), "_")
	wantFile := filepath.Join(tmp, "picons", storeRef+".png")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(wantFile); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("expected prewarmed picon file %q", wantFile)
}

func TestRefresh_WithRuntimePoolPrewarmsPiconsWithoutExplicitPiconBase(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/bouquets", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"bouquets": [][]string{{"1:7:TEST:REF:", "Favourites"}},
		})
	})
	serviceRef := "1:0:19:132F:3EF:1:C00000:0:0:0:"
	mux.HandleFunc("/api/getservices", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"services": []map[string]string{
				{"servicename": "ORF1 HD", "servicereference": serviceRef},
			},
		})
	})
	mux.HandleFunc("/picon/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, ".png") {
			t.Fatalf("unexpected picon path %q", r.URL.Path)
		}
		// Simulate the real-world receiver behavior: HD refs 404, normalized SD-style refs exist.
		if strings.Contains(r.URL.Path, "1_0_19_") {
			http.NotFound(w, r)
			return
		}
		if !strings.Contains(r.URL.Path, "1_0_1_") {
			t.Fatalf("expected normalized picon lookup, got %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("fake-png"))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	tmp := t.TempDir()
	cfg := config.AppConfig{
		DataDir: tmp,
		Enigma2: config.Enigma2Settings{
			BaseURL:    srv.URL,
			StreamPort: 8001,
		},
		Bouquet: "Favourites",
	}

	pool, err := NewPiconPoolForConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewPiconPoolForConfig returned error: %v", err)
	}
	defer pool.Stop()

	snap := config.BuildSnapshot(cfg, config.ReadOSRuntimeEnvOrDefault())
	status, err := RefreshWithOptions(context.Background(), snap, WithPiconPool(pool))
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	if status == nil || status.Channels != 1 {
		t.Fatalf("unexpected status: %#v", status)
	}

	storeRef := strings.TrimRight(strings.ReplaceAll(serviceRef, ":", "_"), "_")
	wantFile := filepath.Join(tmp, "picons", storeRef+".png")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(wantFile); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("expected normalized prewarmed picon file %q", wantFile)
}
