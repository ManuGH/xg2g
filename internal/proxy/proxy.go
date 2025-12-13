// Package proxy provides a reverse proxy for Enigma2 streams with HEAD request support.
package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/m3u"
	"github.com/ManuGH/xg2g/internal/metrics"
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
	dataDir        string // For reading playlist.m3u
	playlistPath   string // Path to M3U playlist
	// channelMap stores StreamID -> StreamURL mappings.
	// Concurrency: Protected by channelMu (RWMutex).
	// - Reads (Lookup) use RLock/RUnlock
	// - Writes (Load) use Lock/Unlock
	// Reload Strategy:
	// - Initial load at startup.
	// - On specific lookup miss (slug not found), we attempt one reload.
	// - Future plan: Use file watcher or periodic refresh for high-availability updates.
	channelMap map[string]string
	channelMu  sync.RWMutex
	listenPort string
	localHosts map[string]struct{}
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

	// Playlist Configuration
	DataDir      string
	PlaylistPath string
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
		dataDir:        cfg.DataDir,
		playlistPath:   cfg.PlaylistPath,
		channelMap:     make(map[string]string),
	}

	listenHost, listenPort := splitListenAddr(cfg.ListenAddr)
	s.listenPort = listenPort
	s.localHosts = collectLocalHosts(listenHost)

	// Load M3U playlist if available
	if err := s.loadM3U(); err != nil {
		cfg.Logger.Warn().Err(err).Msg("failed to load initial playlist (will retry on lookup)")
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
		ReadTimeout:       40 * time.Second, // Increased to allow FFmpeg probing (>30s)
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      0, // No timeout for streaming
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	return s, nil
}

// handleRequest handles incoming HTTP requests using a priority chain of handlers.
// Chain: HEAD -> HLS -> Transcode -> Direct
func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	// Log the request
	s.logger.Debug().
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Str("remote_addr", r.RemoteAddr).
		Msg("proxy request")

	// 1. HEAD Requests (Enigma2 compatibility)
	if s.tryHandleHEAD(w, r) {
		return
	}

	// 2. HLS Requests (iOS/Plex auto-detection & files)
	if s.tryHandleHLS(w, r) {
		return
	}

	// 3. Transcoding (Stream Repair / GPU / Audio)
	if s.tryHandleTranscode(w, r) {
		return
	}

	// 4. Direct Proxy (Fallback)
	metrics.IncActiveStreams("direct")
	defer metrics.DecActiveStreams("direct")
	s.proxy.ServeHTTP(w, r)
}

// resolveTargetURL resolves the target URL for a request using Smart Detection or fallback.
// It extracts the service reference from the path and uses StreamDetector to find the optimal backend.
func (s *Server) resolveTargetURL(ctx context.Context, path, rawQuery string) string {
	// Extract service reference from path (e.g., /1:0:19:132F:3EF:1:C00000:0:0:0:)
	serviceRef := strings.TrimPrefix(path, "/")

	// Check if this looks like a service reference (contains colons)
	isRef := strings.Contains(serviceRef, ":")

	// If it's a slug (frontend ID) and not a Ref, try to resolve it via M3U map
	if !isRef && serviceRef != "" {
		if streamURL, ok := s.lookupStreamURL(serviceRef); ok {
			// Found in map! Use the URL from M3U.
			// This might be http://RECEIVER:8001/... or http://PROXY:18000/...
			// If it points to us (proxy), we need to extract the Ref from it to avoid loops
			// or simply use it if the proxy client handles it (but loop is bad).
			// Let's check if it's a proxy loop.
			if s.isSelfURL(streamURL) {
				// It's pointing to us. Extract Ref from path.
				// Assume format http://host:port/REF...
				if parsed, err := url.Parse(streamURL); err == nil {
					// Use the path from the URL as the new path (likely /REF)
					path = parsed.Path
					serviceRef = strings.TrimPrefix(path, "/")
					isRef = strings.Contains(serviceRef, ":")
					s.logger.Debug().Str("slug", serviceRef).Str("resolved_path", path).Msg("resolved slug to self-referencing proxy URL, extracting path")
				}
			} else {
				// It's an external URL (Direct to Receiver). Use it directly!
				streamURL = appendRawQuery(streamURL, rawQuery)
				s.logger.Debug().Str("slug", serviceRef).Str("target", streamURL).Msg("resolved slug to upstream URL via M3U")
				return streamURL
			}
		} else {
			// Not found in map. Try reloading M3U once?
			if err := s.loadM3U(); err == nil {
				if streamURL, ok := s.lookupStreamURL(serviceRef); ok {
					streamURL = appendRawQuery(streamURL, rawQuery)
					s.logger.Info().Str("slug", serviceRef).Msg("resolved slug after M3U reload")
					return streamURL
				}
			}
		}
	}

	// Try Smart Detection first (if available and enabled)
	if s.streamDetector != nil && serviceRef != "" && openwebif.IsEnabled() {
		streamInfo, err := s.streamDetector.DetectStreamURL(ctx, serviceRef, "", true)
		if err == nil && streamInfo != nil {
			targetURL := appendRawQuery(streamInfo.URL, rawQuery)

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
		return appendRawQuery(targetURL, rawQuery)
	}

	// Last resort: Use receiver host with default port 8001
	targetURL := fmt.Sprintf("http://%s%s", net.JoinHostPort(s.receiverHost, "8001"), path)
	targetURL = appendRawQuery(targetURL, rawQuery)

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
		// /hls/<service_ref> format, potentially followed by /playlist.m3u8 or /segment.ts
		trimmed := strings.TrimPrefix(path, "/hls/")
		parts := strings.Split(trimmed, "/")
		if len(parts) > 0 {
			serviceRef = parts[0]
		}
		// Remove .m3u8 if present (for flat format /hls/REF.m3u8)
		serviceRef = strings.TrimSuffix(serviceRef, ".m3u8")
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

// loadM3U loads the M3U playlist and builds the ID->URL map.
func (s *Server) loadM3U() error {
	if s.playlistPath == "" {
		return nil
	}

	// Read M3U file
	data, err := os.ReadFile(s.playlistPath)
	if err != nil {
		return fmt.Errorf("read playlist: %w", err)
	}

	// Parse
	channels := m3u.Parse(string(data))
	newMap := make(map[string]string)

	for _, ch := range channels {
		id := ch.TvgID
		if id != "" && ch.URL != "" {
			newMap[id] = ch.URL
		}
	}
	s.logger.Info().Int("count", len(newMap)).Str("path", s.playlistPath).Msg("loaded channels from playlist")
	s.channelMu.Lock()
	s.channelMap = newMap
	s.channelMu.Unlock()

	return nil
}

// lookupStreamURL looks up a stream URL by ID.
func (s *Server) lookupStreamURL(id string) (string, bool) {
	s.channelMu.RLock()
	url, ok := s.channelMap[id]
	s.channelMu.RUnlock()
	return url, ok
}

// appendRawQuery merges the provided raw query string into the base URL.
func appendRawQuery(base, rawQuery string) string {
	if rawQuery == "" {
		return base
	}

	u, err := url.Parse(base)
	if err != nil {
		if strings.Contains(base, "?") {
			return base + "&" + rawQuery
		}
		return base + "?" + rawQuery
	}

	extra, err := url.ParseQuery(rawQuery)
	if err != nil {
		if u.RawQuery == "" {
			u.RawQuery = rawQuery
		} else {
			u.RawQuery = u.RawQuery + "&" + rawQuery
		}
		return u.String()
	}

	values := u.Query()
	for key, vs := range extra {
		for _, v := range vs {
			values.Add(key, v)
		}
	}
	u.RawQuery = values.Encode()
	return u.String()
}

func splitListenAddr(addr string) (string, string) {
	if addr == "" {
		return "", ""
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", strings.TrimPrefix(addr, ":")
	}
	if port == "" {
		port = strings.TrimPrefix(addr, ":")
	}
	return host, port
}

func collectLocalHosts(explicitHost string) map[string]struct{} {
	hosts := map[string]struct{}{
		"localhost": {},
		"127.0.0.1": {},
		"::1":       {},
	}

	addHost := func(host string) {
		if host == "" {
			return
		}
		hosts[strings.ToLower(host)] = struct{}{}
	}

	if explicitHost != "" && explicitHost != "0.0.0.0" && explicitHost != "::" {
		addHost(explicitHost)
	}

	if hn, err := os.Hostname(); err == nil {
		addHost(hn)
	}

	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				switch v := addr.(type) {
				case *net.IPNet:
					addHost(v.IP.String())
				case *net.IPAddr:
					addHost(v.IP.String())
				default:
					addHost(addr.String())
				}
			}
		}
	}

	return hosts
}

func (s *Server) isSelfURL(streamURL string) bool {
	if s.listenPort == "" {
		return false
	}

	parsed, err := url.Parse(streamURL)
	if err != nil {
		return false
	}

	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if port == "" {
		switch parsed.Scheme {
		case "https":
			port = "443"
		default:
			port = "80"
		}
	}

	if port != s.listenPort {
		return false
	}

	if host == "" {
		return true
	}

	_, ok := s.localHosts[host]
	return ok
}
