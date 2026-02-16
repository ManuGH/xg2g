// Package proxy provides a reverse proxy for Enigma2 streams with HEAD request support.
package proxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/rs/zerolog"
)

// Server represents a reverse proxy server for Enigma2 streams.
type Server struct {
	addr           string
	targetURL      *url.URL // Fallback target URL (optional)
	proxy          *httputil.ReverseProxy
	httpServer     *http.Server
	logger         zerolog.Logger
	transcoder     *Transcoder               // Optional audio transcoder
	streamDetector *openwebif.StreamDetector // Smart stream detection
	receiverHost   string                    // Receiver host for fallback
	hlsManager     *HLSManager               // HLS streaming manager for iOS
	tlsCert        string
	tlsKey         string
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

	// TLS Configuration
	TLSCert string
	TLSKey  string
}

type trackingResponseWriter struct {
	http.ResponseWriter
	wroteHeader  bool
	statusCode   int
	bytesWritten int64
}

func newTrackingResponseWriter(w http.ResponseWriter) *trackingResponseWriter {
	return &trackingResponseWriter{ResponseWriter: w}
}

func (tw *trackingResponseWriter) WriteHeader(statusCode int) {
	if tw.wroteHeader {
		return
	}
	tw.wroteHeader = true
	tw.statusCode = statusCode
	tw.ResponseWriter.WriteHeader(statusCode)
}

func (tw *trackingResponseWriter) Write(p []byte) (int, error) {
	if !tw.wroteHeader {
		tw.WriteHeader(http.StatusOK)
	}
	n, err := tw.ResponseWriter.Write(p)
	tw.bytesWritten += int64(n)
	return n, err
}

func (tw *trackingResponseWriter) ReadFrom(r io.Reader) (int64, error) {
	if !tw.wroteHeader {
		tw.WriteHeader(http.StatusOK)
	}
	if rf, ok := tw.ResponseWriter.(io.ReaderFrom); ok {
		n, err := rf.ReadFrom(r)
		tw.bytesWritten += n
		return n, err
	}
	n, err := io.Copy(tw.ResponseWriter, r)
	tw.bytesWritten += n
	return n, err
}

func (tw *trackingResponseWriter) Flush() {
	if f, ok := tw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (tw *trackingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := tw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("hijacker not supported")
	}
	return h.Hijack()
}

func (tw *trackingResponseWriter) Push(target string, opts *http.PushOptions) error {
	if p, ok := tw.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, opts)
	}
	return http.ErrNotSupported
}

func (tw *trackingResponseWriter) responseCommitted() bool {
	return tw.wroteHeader || tw.bytesWritten > 0
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
		tlsCert:        cfg.TLSCert,
		tlsKey:         cfg.TLSKey,
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
	} else if cfg.ReceiverHost != "" {
		// Create a dynamic reverse proxy for Smart Detection mode
		// The Director function will resolve the target URL on each request
		s.proxy = &httputil.ReverseProxy{
			Director: func(req *http.Request) {
				targetURL := s.resolveTargetURL(req.Context(), req.URL.Path, req.URL.RawQuery)
				target, _ := url.Parse(targetURL)
				if target != nil {
					req.URL.Scheme = target.Scheme
					req.URL.Host = target.Host
					req.URL.Path = target.Path
					req.Host = target.Host
				}
			},
			ErrorLog: nil,
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

	// Initialize HLS manager for iOS streaming
	hlsManager, err := NewHLSManager(cfg.Logger.With().Str("component", "hls").Logger(), "")
	if err != nil {
		cfg.Logger.Warn().Err(err).Msg("failed to initialize HLS manager, HLS streaming disabled")
	} else {
		s.hlsManager = hlsManager
		cfg.Logger.Info().Msg("HLS streaming enabled for iOS devices")
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
// GET requests may be processed through stream repair/transcoding pipeline (priority order):
//  0. H.264 Stream Repair (XG2G_H264_STREAM_REPAIR=true) - Fixes broken H.264 for Plex
//  1. GPU Transcoding (XG2G_GPU_TRANSCODE=true) - Full video+audio transcode
//  2. Audio Transcoding (XG2G_ENABLE_AUDIO_TRANSCODING=true) - Audio-only transcode
//  3. Direct Proxy (default) - No processing
//
// HLS requests (/hls/, .m3u8, segment_*.ts) are routed to HLS manager.
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

	// Auto-detect iOS clients and serve HLS instead of MPEG-TS
	// NOTE: This only works for DIRECT iOS clients (Safari, VLC, IPTV apps)
	// It does NOT work for Plex iOS clients, because Plex Server acts as proxy
	// and its User-Agent is "PlexMediaServer/...", not iOS-specific.
	// IMPORTANT: Exclude HLS component files (.ts, .m3u8) to prevent recursive conversion
	if s.hlsManager != nil && r.Method == http.MethodGet &&
		!strings.HasPrefix(r.URL.Path, "/hls/") &&
		!strings.HasSuffix(r.URL.Path, ".ts") &&
		!strings.HasSuffix(r.URL.Path, ".m3u8") {
		userAgent := r.Header.Get("User-Agent")
		isIOSClient := (strings.Contains(userAgent, "iPhone") ||
			strings.Contains(userAgent, "iPad") ||
			strings.Contains(userAgent, "iOS") ||
			strings.Contains(userAgent, "AppleCoreMedia") ||
			strings.Contains(userAgent, "CFNetwork")) &&
			!strings.Contains(userAgent, "Plex") // Exclude Plex (it handles MPEG-TS)

		// Auto-upgrade iOS clients to HLS for better compatibility
		if isIOSClient {
			hlsPath := "/hls" + r.URL.Path
			s.logger.Info().
				Str("user_agent", userAgent).
				Str("original_path", r.URL.Path).
				Str("hls_path", hlsPath).
				Str("client_ip", r.RemoteAddr).
				Msg("auto-redirecting iOS client to HLS")

			// Internal redirect to HLS handler
			r.URL.Path = hlsPath
			s.handleHLSRequest(w, r)
			return
		}
	}

	// Handle explicit HLS requests (iOS streaming)
	if s.hlsManager != nil && r.Method == http.MethodGet {
		path := r.URL.Path

		// Handle segment requests without /hls/ prefix (Safari requests segments with relative paths)
		if !strings.HasPrefix(path, "/hls/") && strings.Contains(path, "segment_") && strings.HasSuffix(path, ".ts") {
			segmentName := filepath.Base(path)
			if err := s.hlsManager.ServeSegmentFromAnyStream(w, segmentName); err != nil {
				s.logger.Error().
					Err(err).
					Str("segment", segmentName).
					Msg("failed to serve HLS segment")
				http.Error(w, "Segment not found", http.StatusNotFound)
			}
			return
		}

		// Handle HLS requests with /hls/ prefix or playlist requests
		// - /hls/<service_ref> - HLS playlist request
		// - /*.m3u8 - Playlist file
		if strings.HasPrefix(path, "/hls/") || strings.HasSuffix(path, ".m3u8") {
			s.handleHLSRequest(w, r)
			return
		}
	}

	// Handle GET requests with optional transcoding
	if r.Method == http.MethodGet && s.transcoder != nil {
		// Build target URL for this request using Smart Detection or fallback
		targetURL := s.resolveTargetURL(r.Context(), r.URL.Path, r.URL.RawQuery)

		// Priority 0: H.264 stream repair (if enabled) - Fixes broken H.264 streams for Plex
		// This adds PPS/SPS headers using FFmpeg's h264_mp4toannexb bitstream filter
		if s.transcoder.Config.H264RepairEnabled {
			s.logger.Info().
				Str("path", r.URL.Path).
				Str("target", targetURL).
				Msg("routing stream through H.264 PPS/SPS repair (Plex compatibility fix)")

			tw := newTrackingResponseWriter(w)
			if err := s.transcoder.RepairH264Stream(r.Context(), tw, r, targetURL); err != nil {
				// Client disconnected; nothing more to do.
				if errors.Is(err, context.Canceled) || r.Context().Err() != nil {
					return
				}
				// If anything was already written, a fallback would corrupt the stream.
				if tw.responseCommitted() {
					s.logger.Error().
						Err(err).
						Str("path", r.URL.Path).
						Msg("H.264 stream repair failed after response started; skipping fallback")
					return
				}
				s.logger.Error().
					Err(err).
					Str("path", r.URL.Path).
					Msg("H.264 stream repair failed, falling back to direct proxy")
				s.proxy.ServeHTTP(w, r)
			}
			return
		}

		// Priority 1: GPU transcoding (if enabled)
		if s.transcoder.IsGPUEnabled() {
			s.logger.Debug().
				Str("path", r.URL.Path).
				Str("target", targetURL).
				Msg("routing stream through GPU transcoder")

			tw := newTrackingResponseWriter(w)
			if err := s.transcoder.ProxyToGPUTranscoder(r.Context(), tw, r, targetURL); err != nil {
				if errors.Is(err, context.Canceled) || r.Context().Err() != nil {
					return
				}
				if tw.responseCommitted() {
					s.logger.Error().
						Err(err).
						Str("path", r.URL.Path).
						Msg("GPU transcoding failed after response started; skipping fallback")
					return
				}
				s.logger.Error().
					Err(err).
					Str("path", r.URL.Path).
					Msg("GPU transcoding failed, falling back to direct proxy")
				s.proxy.ServeHTTP(w, r)
			}
			return
		}

		// Priority 2: Audio-only transcoding
		// Try Rust remuxer first (if enabled), with automatic fallback to FFmpeg
		tw := newTrackingResponseWriter(w)
		var err error
		if s.transcoder.Config.UseRustRemuxer {
			s.logger.Debug().Str("method", "rust").Msg("attempting native rust remuxer")
			err = s.transcoder.TranscodeStreamRust(r.Context(), tw, r, targetURL)

			// If Rust remuxer fails and it's not a client disconnect, fall back to FFmpeg
			if err != nil && r.Context().Err() == nil {
				if tw.responseCommitted() {
					s.logger.Error().
						Err(err).
						Str("path", r.URL.Path).
						Msg("rust remuxer failed after response started; skipping ffmpeg fallback")
					return
				}
				s.logger.Warn().
					Err(err).
					Str("path", r.URL.Path).
					Msg("rust remuxer failed, falling back to FFmpeg subprocess")

				// Fallback: Try FFmpeg subprocess transcoding
				s.logger.Debug().Str("method", "ffmpeg").Msg("using ffmpeg transcoding")
				err = s.transcoder.TranscodeStream(r.Context(), tw, r, targetURL)
			}
		} else {
			// Rust remuxer disabled, use FFmpeg directly
			s.logger.Debug().Str("method", "ffmpeg").Msg("using ffmpeg transcoding")
			err = s.transcoder.TranscodeStream(r.Context(), tw, r, targetURL)
		}

		// If transcoding succeeded or client disconnected, we're done
		if err == nil || r.Context().Err() != nil {
			if err != nil {
				s.logger.Debug().
					Str("path", r.URL.Path).
					Msg("audio transcoding stopped (client disconnected)")
			}
			return
		}

		// If transcoding failed, log and fall back to direct proxy
		if tw.responseCommitted() {
			s.logger.Error().
				Err(err).
				Str("path", r.URL.Path).
				Msg("all transcoding methods failed after response started; skipping fallback")
			return
		}
		s.logger.Warn().
			Err(err).
			Str("path", r.URL.Path).
			Msg("all transcoding methods failed, falling back to direct proxy")
		// Fall through to s.proxy.ServeHTTP below
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
	targetURL := fmt.Sprintf("http://%s%s", net.JoinHostPort(s.receiverHost, "8001"), path)
	if rawQuery != "" {
		targetURL += "?" + rawQuery
	}

	s.logger.Debug().
		Str("target", targetURL).
		Msg("using receiver host fallback")

	return targetURL
}

// handleHLSRequest handles HLS streaming requests for iOS devices.
func (s *Server) handleHLSRequest(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Extract service reference from path
	var serviceRef string
	if strings.HasPrefix(path, "/hls/") {
		// /hls/<service_ref> format
		serviceRef = strings.TrimPrefix(path, "/hls/")
		// Remove any file extensions
		serviceRef = strings.TrimSuffix(serviceRef, ".m3u8")
		serviceRef = strings.TrimSuffix(serviceRef, "/")
	} else {
		// Try to extract from path (e.g., /1:0:19:132F:3EF:1:C00000:0:0:0:/playlist.m3u8)
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) > 0 {
			serviceRef = parts[0]
		}
	}

	if serviceRef == "" {
		http.Error(w, "service reference required", http.StatusBadRequest)
		return
	}

	// Build target URL for this service reference
	targetURL := s.resolveTargetURL(r.Context(), "/"+serviceRef, r.URL.RawQuery)

	s.logger.Debug().
		Str("service_ref", serviceRef).
		Str("target", targetURL).
		Str("path", path).
		Msg("serving HLS stream")

	// Serve HLS content
	if err := s.hlsManager.ServeHLS(w, r, serviceRef, targetURL); err != nil {
		s.logger.Error().
			Err(err).
			Str("service_ref", serviceRef).
			Msg("HLS streaming failed")
		http.Error(w, "HLS streaming failed", http.StatusInternalServerError)
	}
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

	if s.tlsCert != "" && s.tlsKey != "" {
		logEvent.Msg("starting stream proxy server (HTTPS)")
		if err := s.httpServer.ListenAndServeTLS(s.tlsCert, s.tlsKey); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("proxy server (HTTPS) failed: %w", err)
		}
	} else {
		logEvent.Msg("starting stream proxy server (HTTP)")
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("proxy server (HTTP) failed: %w", err)
		}
	}

	return nil
}

// Shutdown gracefully shuts down the proxy server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("shutting down stream proxy server")

	// Shutdown HLS manager if initialized
	if s.hlsManager != nil {
		s.hlsManager.Shutdown()
	}

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
