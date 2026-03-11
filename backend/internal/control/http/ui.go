// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package http

import (
	"fmt"
	"net/http"
	"strings"
)

// UIConfig configures the UI handler.
type UIConfig struct {
	CSP string
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

func writeUIHTMLResponse(w http.ResponseWriter, status int, title, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(
		w,
		"<!doctype html><html><head><meta charset=\"utf-8\"><title>xg2g</title></head><body><h1>%s</h1><p>%s</p></body></html>",
		title,
		message,
	)
}

func withAdditionalConnectSrc(csp string, extras ...string) string {
	if strings.TrimSpace(csp) == "" {
		return csp
	}

	directives := strings.Split(csp, ";")
	for i, directive := range directives {
		trimmed := strings.TrimSpace(directive)
		if !strings.HasPrefix(trimmed, "connect-src") {
			continue
		}
		for _, extra := range extras {
			if strings.Contains(trimmed, extra) {
				continue
			}
			trimmed += " " + extra
		}
		directives[i] = trimmed
		return strings.Join(directives, "; ")
	}

	return strings.TrimSpace(csp) + "; connect-src " + strings.Join(extras, " ")
}
