// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package http

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var uiFS embed.FS

// UIConfig configures the UI handler
type UIConfig struct {
	CSP string
}

// UIHandler serves the embedded Web UI (SPA) with correct caching + CSP.
// It is self-contained: embed + serving live together in control.
func UIHandler(cfg UIConfig) http.Handler {
	// Subdirectory "dist" matches the build output
	subFS, err := fs.Sub(uiFS, "dist")
	var fileServer http.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "UI not available", http.StatusInternalServerError)
	})
	if err == nil {
		fileServer = http.FileServer(http.FS(subFS))
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Explicitly attach CSP so the main UI HTML allows blob: media (Safari HLS)
		w.Header().Set("Content-Security-Policy", cfg.CSP)

		// Assets (js, css, images) should be cached (hashed)
		// Index.html should NOT be cached to ensure updates
		path := r.URL.Path
		if path == "/" || path == "/index.html" || path == "" || !strings.Contains(path, ".") {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
		} else {
			// Hashed assets can be cached forever
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}

		fileServer.ServeHTTP(w, r)
	})
}
