// SPDX-License-Identifier: MIT
package jobs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestRefresh_IntegrationSuccess(t *testing.T) {
	// Spin up a stub OpenWebIF server
	mux := http.NewServeMux()

	// Bouquets endpoint returns a single bouquet named "Favourites"
	mux.HandleFunc("/api/bouquets", func(w http.ResponseWriter, r *http.Request) {
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

	cfg := Config{
		DataDir:    tmp,
		OWIBase:    u.String(),
		Bouquet:    "Favourites",
		XMLTVPath:  "xmltv.xml",
		StreamPort: 8001,
	}

	st, err := Refresh(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	if st == nil || st.Channels != 2 {
		t.Fatalf("unexpected status: %#v", st)
	}

	// Verify files were written
	if _, err := os.Stat(filepath.Join(tmp, "playlist.m3u")); err != nil {
		t.Fatalf("playlist.m3u not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "xmltv.xml")); err != nil {
		t.Fatalf("xmltv.xml not written: %v", err)
	}
}
