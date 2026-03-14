//go:build dev

// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package http

import (
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
)

const defaultUIDevProxyURL = "http://127.0.0.1:5173"

// UIHandler serves the Web UI in development mode.
// It prefers a live Vite dev server for HMR and can optionally serve a local dist directory.
func UIHandler(cfg UIConfig) http.Handler {
	devCSP := withAdditionalConnectSrc(cfg.CSP, "ws:", "wss:")
	devCSP = withAdditionalScriptSrc(devCSP, "'unsafe-inline'")

	if dir := strings.TrimSpace(os.Getenv("XG2G_UI_DEV_DIR")); dir != "" {
		return newUIDevStaticHandler(dir, devCSP)
	}

	proxyURL := strings.TrimSpace(os.Getenv("XG2G_UI_DEV_PROXY_URL"))
	if proxyURL == "" {
		proxyURL = defaultUIDevProxyURL
	}

	target, err := url.Parse(proxyURL)
	if err != nil || target.Scheme == "" || target.Host == "" {
		return newUIDevUnavailableHandler(devCSP, proxyURL)
	}

	return newUIDevProxyHandler(target, devCSP, proxyURL)
}

func newUIDevProxyHandler(target *url.URL, csp, proxyURL string) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(target)
	baseDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		baseDirector(req)
		req.Host = target.Host
		req.URL.Path = joinUIPath(target.Path, "/ui"+ensureLeadingSlash(req.URL.Path))
		req.URL.RawPath = req.URL.Path
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, _ error) {
		setUIHeaders(w, r.URL.Path, csp, uiCacheModeDev)
		if isUIHTMLRoute(r.URL.Path) {
			writeUIHTMLResponse(w, http.StatusBadGateway, "xg2g UI dev server unavailable", "Start `make webui-dev` or point XG2G_UI_DEV_PROXY_URL at a running Vite server. Current target: "+proxyURL)
			return
		}
		http.Error(w, "xg2g UI dev server unavailable", http.StatusBadGateway)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setUIHeaders(w, r.URL.Path, csp, uiCacheModeDev)
		proxy.ServeHTTP(w, r)
	})
}

func newUIDevStaticHandler(root, csp string) http.Handler {
	fsys := os.DirFS(root)
	fileServer := http.FileServer(http.FS(fsys))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setUIHeaders(w, r.URL.Path, csp, uiCacheModeDev)

		if isUIHTMLRoute(r.URL.Path) {
			if _, err := fs.Stat(fsys, "index.html"); err == nil {
				req := r.Clone(r.Context())
				req.URL.Path = "/"
				fileServer.ServeHTTP(w, req)
				return
			}

			writeUIHTMLResponse(w, http.StatusOK, "xg2g UI not built", "No index.html found in XG2G_UI_DEV_DIR.")
			return
		}

		fileServer.ServeHTTP(w, r)
	})
}

func newUIDevUnavailableHandler(csp, proxyURL string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setUIHeaders(w, r.URL.Path, csp, uiCacheModeDev)
		if isUIHTMLRoute(r.URL.Path) {
			writeUIHTMLResponse(w, http.StatusBadGateway, "xg2g UI dev server unavailable", "Set XG2G_UI_DEV_PROXY_URL to a valid Vite server URL or XG2G_UI_DEV_DIR to a local dist directory. Current target: "+proxyURL)
			return
		}
		http.Error(w, "xg2g UI dev server unavailable", http.StatusBadGateway)
	})
}

func ensureLeadingSlash(path string) string {
	if path == "" || strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}

func joinUIPath(basePath, requestPath string) string {
	if basePath == "" || basePath == "/" {
		return requestPath
	}
	if strings.HasSuffix(basePath, "/") && strings.HasPrefix(requestPath, "/") {
		return basePath[:len(basePath)-1] + requestPath
	}
	if strings.HasSuffix(basePath, "/") || strings.HasPrefix(requestPath, "/") {
		return basePath + requestPath
	}
	return basePath + "/" + requestPath
}
