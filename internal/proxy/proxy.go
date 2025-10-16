// Package proxy provides a reverse proxy for Enigma2 streams with HEAD request support.
package proxy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"

	"github.com/rs/zerolog"
)

// Server represents a reverse proxy server for Enigma2 streams.
type Server struct {
	addr       string
	targetURL  *url.URL
	proxy      *httputil.ReverseProxy
	httpServer *http.Server
	logger     zerolog.Logger
}

// Config holds the configuration for the proxy server.
type Config struct {
	// ListenAddr is the address to listen on (e.g., ":18000")
	ListenAddr string

	// TargetURL is the URL to proxy requests to (e.g., "http://10.10.55.57:17999")
	TargetURL string

	// Logger is the logger instance to use
	Logger zerolog.Logger
}

// New creates a new proxy server.
func New(cfg Config) (*Server, error) {
	if cfg.ListenAddr == "" {
		return nil, fmt.Errorf("listen address is required")
	}

	if cfg.TargetURL == "" {
		return nil, fmt.Errorf("target URL is required")
	}

	target, err := url.Parse(cfg.TargetURL)
	if err != nil {
		return nil, fmt.Errorf("parse target URL %q: %w", cfg.TargetURL, err)
	}

	s := &Server{
		addr:      cfg.ListenAddr,
		targetURL: target,
		logger:    cfg.Logger,
	}

	// Create reverse proxy with custom director
	s.proxy = httputil.NewSingleHostReverseProxy(target)
	s.proxy.ErrorLog = nil // We handle errors ourselves

	// Customize the director to preserve the original path
	originalDirector := s.proxy.Director
	s.proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
	}

	// Create HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRequest)

	s.httpServer = &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadTimeout:       10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      0, // No timeout for streaming
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	return s, nil
}

// handleRequest handles incoming HTTP requests.
// HEAD requests are answered directly without proxying to avoid EOF errors from Enigma2.
// GET/POST requests are proxied to the target URL.
func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	// Log the request
	s.logger.Debug().
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Str("remote_addr", r.RemoteAddr).
		Msg("proxy request")

	// Handle HEAD requests without proxying
	if r.Method == http.MethodHead {
		s.handleHeadRequest(w, r)
		return
	}

	// Proxy GET/POST requests to target
	s.proxy.ServeHTTP(w, r)
}

// handleHeadRequest handles HEAD requests by returning a 200 OK response
// with appropriate headers without proxying to the target.
func (s *Server) handleHeadRequest(w http.ResponseWriter, r *http.Request) {
	// Set headers for MPEG-TS stream
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Accept-Ranges", "none")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Connection", "close")

	w.WriteHeader(http.StatusOK)

	s.logger.Debug().
		Str("path", r.URL.Path).
		Msg("answered HEAD request")
}

// Start starts the proxy server.
func (s *Server) Start() error {
	s.logger.Info().
		Str("addr", s.addr).
		Str("target", s.targetURL.String()).
		Msg("starting stream proxy server")

	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("proxy server failed: %w", err)
	}

	return nil
}

// Shutdown gracefully shuts down the proxy server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("shutting down stream proxy server")
	return s.httpServer.Shutdown(ctx)
}

// IsEnabled checks if the proxy is enabled via environment variable.
func IsEnabled() bool {
	return os.Getenv("XG2G_ENABLE_STREAM_PROXY") == "true"
}

// GetListenAddr returns the listen address from environment or default.
func GetListenAddr() string {
	if addr := os.Getenv("XG2G_PROXY_PORT"); addr != "" {
		return ":" + addr
	}
	return ":18000" // Default proxy port
}

// GetTargetURL returns the target URL from environment.
func GetTargetURL() string {
	return os.Getenv("XG2G_PROXY_TARGET")
}
