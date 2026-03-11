//go:build !dev

// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package http

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var uiFS embed.FS

// UIHandler serves the embedded Web UI (SPA) with correct caching + CSP.
// It is self-contained: embed + serving live together in control.
func UIHandler(cfg UIConfig) http.Handler {
	subFS, err := fs.Sub(uiFS, "dist")
	uiAvailable := false
	var fileServer http.Handler
	if err == nil {
		if _, statErr := fs.Stat(subFS, "index.html"); statErr == nil {
			uiAvailable = true
			fileServer = http.FileServer(http.FS(subFS))
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setUIHeaders(w, r.URL.Path, cfg.CSP, uiCacheModeProd)

		if uiAvailable {
			fileServer.ServeHTTP(w, r)
			return
		}

		// Fallback: allow running API-only binaries/tests without building the WebUI bundle.
		// Keep it a 200 so callers (and tests) can still validate caching/CSP behavior.
		if isUIHTMLRoute(r.URL.Path) {
			writeUIHTMLResponse(w, http.StatusOK, "xg2g UI not built", "Build the WebUI bundle to enable /ui/.")
			return
		}

		http.NotFound(w, r)
	})
}
