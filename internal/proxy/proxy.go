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
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/rs/zerolog"
)

// Server represents a reverse proxy server for Enigma2 streams.
type Server struct {
	addr           string
	targetURL      *url.URL      // Fallback target URL (optional)
	proxy          *httputil.ReverseProxy
	httpServer     *http.Server
	logger         zerolog.Logger
	transcoder     *Transcoder                   // Optional audio transcoder
	streamDetector *openwebif.StreamDetector     // Smart stream detection
	receiverHost   string                        // Receiver host for fallback
}

// Config holds the configuration for the proxy server.
type Config struct {
	// ListenAddr is the address to listen on (e.g., ":18000")
	ListenAddr string

	// TargetURL is the URL to proxy requests to (e.g., "http://10.10.55.57:17999")
	// Optional: If not provided, uses StreamDetector with ReceiverHost
	TargetURL string

	// ReceiverHost is the receiver hostname/IP for Smart Detection fallback
	// Required if TargetURL is not provided
	ReceiverHost string

	// StreamDetector enables smart port detection (8001 vs 17999)
	// Optional: If provided, overrides TargetURL for optimal routing
	StreamDetector *openwebif.StreamDetector

	// Logger is the logger instance to use
	Logger zerolog.Logger
}

// New creates a new proxy server.
func New(cfg Config) (*Server, error) {
	if cfg.ListenAddr == "" {
		return nil, fmt.Errorf("listen address is required")
	}

	// Validate configuration: Need either TargetURL or ReceiverHost
	if cfg.TargetURL == "" && cfg.ReceiverHost == "" {
		return nil, fmt.Errorf("either TargetURL or ReceiverHost is required")
	}

	s := &Server{
		addr:           cfg.ListenAddr,
		logger:         cfg.Logger,
		streamDetector: cfg.StreamDetector,
		receiverHost:   cfg.ReceiverHost,
	}

	// Parse target URL if provided (used as fallback)
	if cfg.TargetURL != "" {
		target, err := url.Parse(cfg.TargetURL)
		if err != nil {
			return nil, fmt.Errorf("parse target URL %q: %w", cfg.TargetURL, err)
		}
		s.targetURL = target

		// Create reverse proxy for fallback (when Smart Detection is not available)
		s.proxy = httputil.NewSingleHostReverseProxy(target)
		s.proxy.ErrorLog = nil // We handle errors ourselves

		// Customize the director to preserve the original path
		originalDirector := s.proxy.Director
		s.proxy.Director = func(req *http.Request) {
			originalDirector(req)
			req.Host = target.Host
		}
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

	// Log Smart Detection status
	if s.streamDetector != nil {
		cfg.Logger.Info().
			Str("receiver", s.receiverHost).
			Msg("Smart stream detection enabled (automatic port selection)")
	} else if s.targetURL != nil {
		cfg.Logger.Info().
			Str("target", s.targetURL.String()).
			Msg("Using fixed target URL (Smart Detection disabled)")
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
		// Build target URL for this request using Smart Detection or fallback
		targetURL := s.resolveTargetURL(r.Context(), r.URL.Path, r.URL.RawQuery)

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

// resolveTargetURL resolves the target URL for a request using Smart Detection or fallback.
// It extracts the service reference from the path and uses StreamDetector to find the optimal backend.
func (s *Server) resolveTargetURL(ctx context.Context, path, rawQuery string) string {
	// Extract service reference from path (e.g., /1:0:19:132F:3EF:1:C00000:0:0:0:)
	serviceRef := strings.TrimPrefix(path, "/")

	// Try Smart Detection first (if available and enabled)
	if s.streamDetector != nil && serviceRef != "" && openwebif.IsEnabled() {
		streamInfo, err := s.streamDetector.DetectStreamURL(ctx, serviceRef, "")
		if err == nil && streamInfo != nil {
			targetURL := streamInfo.URL
			if rawQuery != "" {
				targetURL += "?" + rawQuery
			}

			s.logger.Debug().
				Str("service_ref", serviceRef).
				Int("port", streamInfo.Port).
				Str("target", targetURL).
				Msg("using smart detection for backend URL")

			return targetURL
		}

		// Log detection failure but continue with fallback
		s.logger.Debug().
			Err(err).
			Str("service_ref", serviceRef).
			Msg("smart detection failed, using fallback target")
	}

	// Fallback to configured target URL or receiver host
	if s.targetURL != nil {
		targetURL := s.targetURL.String() + path
		if rawQuery != "" {
			targetURL += "?" + rawQuery
		}
		return targetURL
	}

	// Last resort: Use receiver host with default port 8001
	targetURL := fmt.Sprintf("http://%s:8001%s", s.receiverHost, path)
	if rawQuery != "" {
		targetURL += "?" + rawQuery
	}

	s.logger.Debug().
		Str("target", targetURL).
		Msg("using receiver host fallback")

	return targetURL
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
	logEvent := s.logger.Info().Str("addr", s.addr)

	if s.targetURL != nil {
		logEvent.Str("target", s.targetURL.String())
	} else if s.receiverHost != "" {
		logEvent.Str("receiver", s.receiverHost).Str("mode", "smart_detection")
	}

	logEvent.Msg("starting stream proxy server")

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

// GetTargetURL returns the target URL from environment (optional).
// If not provided, proxy will use Smart Detection with receiver host.
func GetTargetURL() string {
	return os.Getenv("XG2G_PROXY_TARGET")
}

// GetReceiverHost returns the receiver host from XG2G_OWI_BASE.
// Extracts hostname/IP from base URL (e.g., "http://10.10.55.64" -> "10.10.55.64")
func GetReceiverHost() string {
	baseURL := os.Getenv("XG2G_OWI_BASE")
	if baseURL == "" {
		return ""
	}

	// Parse URL to extract host
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}

	return parsed.Hostname()
}
