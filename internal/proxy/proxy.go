// Package proxy provides a reverse proxy for Enigma2 streams with HEAD request support.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
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
	transcoder *Transcoder // Optional audio transcoder
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

	// Initialize optional transcoder
	if IsTranscodingEnabled() {
		transcoderCfg := GetTranscoderConfig()
		s.transcoder = NewTranscoder(transcoderCfg, cfg.Logger)

		if transcoderCfg.GPUEnabled {
			cfg.Logger.Info().
				Str("transcoder_url", transcoderCfg.TranscoderURL).
				Msg("GPU transcoding enabled (full video+audio)")
		} else {
			cfg.Logger.Info().
				Str("codec", transcoderCfg.Codec).
				Str("bitrate", transcoderCfg.Bitrate).
				Int("channels", transcoderCfg.Channels).
				Msg("audio transcoding enabled (audio-only)")
		}
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
// GET requests may be transcoded if audio transcoding is enabled.
// POST requests are proxied directly to the target URL.
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

	// Handle GET requests with optional transcoding
	if r.Method == http.MethodGet && s.transcoder != nil {
		// Build target URL for this request
		targetURL := s.targetURL.String() + r.URL.Path
		if r.URL.RawQuery != "" {
			targetURL += "?" + r.URL.RawQuery
		}

		// Priority 1: GPU transcoding (if enabled)
		if s.transcoder.IsGPUEnabled() {
			s.logger.Debug().
				Str("path", r.URL.Path).
				Str("target", targetURL).
				Msg("routing stream through GPU transcoder")

			if err := s.transcoder.ProxyToGPUTranscoder(r.Context(), w, r, targetURL); err != nil {
				// Only log error if it's not a context cancellation (client disconnect)
				if !errors.Is(err, context.Canceled) {
					s.logger.Error().
						Err(err).
						Str("path", r.URL.Path).
						Msg("GPU transcoding failed, falling back to direct proxy")
				}
				// Fallback to direct proxy on error
				s.proxy.ServeHTTP(w, r)
			}
			return
		}

		// Priority 2: Audio-only transcoding
		// Use Rust remuxer if enabled, otherwise fall back to FFmpeg
		var transcodeFn func(context.Context, http.ResponseWriter, *http.Request, string) error
		if s.transcoder.Config.UseRustRemuxer {
			transcodeFn = s.transcoder.TranscodeStreamRust
			s.logger.Debug().Str("method", "rust").Msg("using native rust remuxer")
		} else {
			transcodeFn = s.transcoder.TranscodeStream
			s.logger.Debug().Str("method", "ffmpeg").Msg("using ffmpeg transcoding")
		}

		if err := transcodeFn(r.Context(), w, r, targetURL); err != nil {
			// Only log error if it's not a context cancellation (client disconnect)
			if r.Context().Err() == nil {
				s.logger.Error().
					Err(err).
					Str("path", r.URL.Path).
					Msg("audio transcoding failed")
			} else {
				s.logger.Debug().
					Str("path", r.URL.Path).
					Msg("audio transcoding stopped (client disconnected)")
			}
		}
		return
	}

	// Proxy GET/POST requests to target (no transcoding)
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
	enabled, _ := strconv.ParseBool(os.Getenv("XG2G_ENABLE_STREAM_PROXY"))
	return enabled
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
