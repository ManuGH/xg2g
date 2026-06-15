// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package http

import (
	"fmt"
	"html"
	"net/http"
	"strings"
)

// UIConfig configures the UI handler.
type UIConfig struct {
	CSP         string
	DevProxyURL string
	DevDir      string
}

type uiCacheMode int

const (
	uiCacheModeProd uiCacheMode = iota
	uiCacheModeDev
)

func isUIHTMLRoute(path string) bool {
	return path == "/" || path == "/index.html" || path == "" || !strings.Contains(path, ".")
}

func setUIHeaders(w http.ResponseWriter, path, csp string, mode uiCacheMode) {
	if csp != "" {
		w.Header().Set("Content-Security-Policy", csp)
	}

	switch mode {
	case uiCacheModeDev:
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
	default:
		if isUIHTMLRoute(path) {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
			return
		}

		// Hashed assets can be cached aggressively in production.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
}

// cacheControlGuard strips caching headers on error responses so a missing hashed asset
// (404) is never served with the long-lived `immutable` Cache-Control that setUIHeaders
// stamps up front for asset routes — otherwise a client caches the failure for a year and
// never re-requests, even once the asset exists. It mutates the header map inside WriteHeader,
// BEFORE the status line and headers are committed (afterwards is too late). Only >= 400 is
// stripped: a 304 Not Modified is a successful revalidation of a hashed asset and MUST keep
// its cache headers, so "anything but 200" would be wrong here.
type cacheControlGuard struct {
	http.ResponseWriter
	wroteHeader bool
}

func (g *cacheControlGuard) WriteHeader(code int) {
	if !g.wroteHeader {
		g.wroteHeader = true
		if code >= 400 {
			h := g.Header()
			h.Del("Cache-Control")
			h.Del("Pragma")
			h.Del("Expires")
		}
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *cacheControlGuard) Write(b []byte) (int, error) {
	if !g.wroteHeader {
		// Implicit 200 (handler wrote a body without an explicit status): not an error, so
		// cache headers stay; just record that the header is now committed.
		g.WriteHeader(http.StatusOK)
	}
	return g.ResponseWriter.Write(b)
}

func writeUIHTMLResponse(w http.ResponseWriter, status int, title, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(
		w,
		"<!doctype html><html><head><meta charset=\"utf-8\"><title>xg2g</title></head><body><h1>%s</h1><p>%s</p></body></html>",
		html.EscapeString(title),
		html.EscapeString(message),
	)
}
