//go:build !dev

// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package http

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
)

//go:embed all:dist
var uiFS embed.FS

func init() {
	// Go's built-in mime table has no entry for .webmanifest and slim
	// container images ship no /etc/mime.types, so http.FileServer would
	// fall back to content sniffing for the PWA manifest.
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

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

	return uiHandler(cfg, fileServer, uiAvailable)
}

// uiHandler is the testable core. Split out so the !uiAvailable branch is reachable in tests:
// with the UI bundle absent, a missing asset 404s via http.NotFound, which — unlike
// http.FileServer (it clears caching headers on its own 404) — leaves the `immutable`
// Cache-Control that setUIHeaders stamps for asset routes intact. Without the cacheControlGuard
// that 404 would be cached for a year. The bug's real-world reach is therefore API-only / not-
// yet-built deployments; the guard also stands as defense-in-depth for the FileServer path.
func uiHandler(cfg UIConfig, fileServer http.Handler, uiAvailable bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setUIHeaders(w, r.URL.Path, cfg.CSP, uiCacheModeProd)
		// setUIHeaders stamps `immutable` up front for asset routes; the guard strips the cache
		// headers when the final status is >= 400 so a missing asset's 404 is not cached.
		w = &cacheControlGuard{ResponseWriter: w}

		if uiAvailable {
			if isUIHTMLRoute(r.URL.Path) {
				req := r.Clone(r.Context())
				req.URL.Path = "/"
				req.URL.RawPath = req.URL.Path
				fileServer.ServeHTTP(w, req)
				return
			}

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
