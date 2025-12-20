// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package proxy

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"

	apimw "github.com/ManuGH/xg2g/internal/api/middleware"
	"github.com/ManuGH/xg2g/internal/auth"
	"github.com/ManuGH/xg2g/internal/config"
	coreopenwebif "github.com/ManuGH/xg2g/internal/core/openwebif"
	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/m3u"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/rs/zerolog"
)

// Server represents a reverse proxy server for Enigma2 streams.
type Server struct {
	addr         string
	targetURL    *url.URL // Fallback target URL (optional)
	proxy        *httputil.ReverseProxy
	httpServer   *http.Server
	logger       zerolog.Logger
	transcoder   *Transcoder // Optional audio transcoder
	receiverHost string      // Receiver host for fallback
	hlsManager   *HLSManager // HLS streaming manager for iOS
	tlsCert      string
	tlsKey       string
	dataDir      string // For reading playlist.m3u
	playlistPath string // Path to M3U playlist
	// channelMap stores StreamID -> StreamURL mappings.
	channelMap         map[string]string
	channelMu          sync.RWMutex
	listenPort         string
	localHosts         map[string]struct{}
	recordingRoots     map[string]string
	allowedAuthorities map[string]struct{} // Canonical host:port allowlist

	streamLimiter     *semaphore.Weighted
	streamLimit       int64
	transcodeFailOpen bool

	idleTimeout   time.Duration
	registry      *Registry // Replaces activeStreams sync.Map
	monitorCancel context.CancelFunc
	mu            sync.Mutex

	apiToken      string
	authAnonymous bool
}

// Config holds the configuration for the proxy server.
type Config struct {
	ListenAddr     string
	TargetURL      string
	ReceiverHost   string
	Logger         zerolog.Logger
	TLSCert        string
	TLSKey         string
	DataDir        string
	PlaylistPath   string
	RecordingRoots map[string]string
	Runtime        config.RuntimeSnapshot
	APIToken       string
	AuthAnonymous  bool
}

// New creates a new proxy server.
func New(cfg Config) (*Server, error) {
	if cfg.ListenAddr == "" {
		return nil, fmt.Errorf("listen address is required")
	}

	if cfg.TargetURL == "" && cfg.ReceiverHost == "" {
		return nil, fmt.Errorf("either TargetURL or ReceiverHost is required")
	}

	// Security Gate 1: Fail-Closed Auth
	// If anonymous auth is disabled, we MUST have a valid, non-empty token configured.
	if !cfg.AuthAnonymous && strings.TrimSpace(cfg.APIToken) == "" {
		return nil, fmt.Errorf("proxy authentication token is unset or empty, and anonymous access is disabled (refusing to start open proxy)")
	}

	// Ensure RecordingRoots has a default if empty to prevent surprising denials
	recordingRoots := cfg.RecordingRoots
	if len(recordingRoots) == 0 {
		recordingRoots = map[string]string{"hdd": "/media/hdd/movie"}
	}

	s := &Server{
		addr:           cfg.ListenAddr,
		logger:         cfg.Logger,
		receiverHost:   cfg.ReceiverHost,
		tlsCert:        cfg.TLSCert,
		tlsKey:         cfg.TLSKey,
		hlsManager:     nil, // init later
		dataDir:        cfg.DataDir,
		playlistPath:   cfg.PlaylistPath,
		recordingRoots: recordingRoots,
		channelMap:     make(map[string]string),
		registry:       NewRegistry(),
		localHosts:     make(map[string]struct{}),
		apiToken:       cfg.APIToken,
		authAnonymous:  cfg.AuthAnonymous,
	}

	listenHost, listenPort := splitListenAddr(cfg.ListenAddr)
	s.listenPort = listenPort
	s.localHosts = collectLocalHosts(listenHost)

	if err := s.loadM3U(); err != nil {
		cfg.Logger.Warn().Err(err).Msg("failed to load initial playlist (will retry on lookup)")
	}

	rt := cfg.Runtime

	if n := rt.StreamProxy.MaxConcurrentStreams; n > 0 {
		s.streamLimit = n
		s.streamLimiter = semaphore.NewWeighted(n)
	}

	s.transcodeFailOpen = rt.StreamProxy.TranscodeFailOpen

	if rt.StreamProxy.IdleTimeout > 0 {
		s.idleTimeout = rt.StreamProxy.IdleTimeout
	}

	// Security Gate 2: SSRF / Open Proxy Hardening
	// Pre-calculate allowed authorities (host:port) to prevent pivoting.
	s.allowedAuthorities = make(map[string]struct{})

	if cfg.TargetURL != "" {
		target, err := url.Parse(cfg.TargetURL)
		if err != nil {
			return nil, fmt.Errorf("parse target URL %q: %w", cfg.TargetURL, err)
		}
		// Enforce explicit port
		if target.Port() == "" {
			return nil, fmt.Errorf("configured TargetURL %q must include an explicit port (e.g. :80 or :443)", cfg.TargetURL)
		}

		auth := canonicalizeAuthority(target.Host)
		s.allowedAuthorities[auth] = struct{}{}

		s.targetURL = target

		s.proxy = httputil.NewSingleHostReverseProxy(target)
		s.proxy.ErrorLog = nil

		originalDirector := s.proxy.Director
		s.proxy.Director = func(req *http.Request) {
			originalDirector(req)
			req.Host = target.Host
		}
	} else if cfg.ReceiverHost != "" {
		// ReceiverHost legacy mode: implies port 8001
		// Validate that ReceiverHost is host-only (no port); port is implied by legacy behavior.
		if _, _, err := net.SplitHostPort(cfg.ReceiverHost); err == nil {
			return nil, fmt.Errorf("ReceiverHost %q must not include a port; configure TargetURL instead", cfg.ReceiverHost)
		}

		// Determine the authority we actually fallback to: ReceiverHost:8001
		receiverAuth := net.JoinHostPort(cfg.ReceiverHost, "8001")
		// Canonicalize
		s.allowedAuthorities[canonicalizeAuthority(receiverAuth)] = struct{}{}

		s.proxy = &httputil.ReverseProxy{
			Director: func(req *http.Request) {
				targetURL, _ := s.resolveTargetURL(req.Context(), req.URL.Path, req.URL.RawQuery)
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

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	s.proxy.Transport = transport

	if rt.Transcoder.Enabled {
		transcoderCfg := buildTranscoderConfigFromRuntime(rt.Transcoder)
		if transcoderCfg.Enabled || transcoderCfg.H264RepairEnabled || transcoderCfg.GPUEnabled || transcoderCfg.VideoTranscode {
			s.transcoder = NewTranscoder(transcoderCfg, cfg.Logger.With().Str("component", "transcoder").Logger())
		}
	}

	hlsCfg := HLSManagerConfig{
		OutputDir:      rt.HLS.OutputDir,
		Generic:        rt.HLS.Generic,
		Safari:         rt.HLS.Safari,
		LLHLS:          rt.HLS.LLHLS,
		FFmpegLogLevel: rt.FFmpegLogLevel,
	}
	hlsManager, err := NewHLSManager(cfg.Logger.With().Str("component", "hls").Logger(), hlsCfg)
	if err != nil {
		cfg.Logger.Warn().Err(err).Msg("failed to initialize HLS manager, HLS streaming disabled")
	} else {
		s.hlsManager = hlsManager
		cfg.Logger.Info().Msg("HLS streaming enabled")
	}

	r := apimw.NewRouter(apimw.StackConfig{})
	r.HandleFunc("/", s.handleRequest)
	r.HandleFunc("/*", s.handleRequest)
	r.NotFound(s.handleRequest)
	r.MethodNotAllowed(s.handleRequest)

	s.httpServer = &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           r,
		ReadTimeout:       40 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	return s, nil
}

// GetSessions returns the list of active stream sessions.
func (s *Server) GetSessions() []*StreamSession {
	if s.registry == nil {
		return nil
	}
	return s.registry.List()
}

func (s *Server) validateUpstream(upstream string) error {
	// ALLOW: http://, https://
	// DENY: file://, user@host
	// HOST:PORT MUST MATCH configured allowedAuthorities EXACTLY

	u, err := url.Parse(upstream)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("disallowed scheme: %s", u.Scheme)
	}

	if u.User != nil {
		return fmt.Errorf("userinfo not allowed in upstream")
	}

	// Must have a port to match our strict config requirements
	if u.Port() == "" {
		return fmt.Errorf("upstream must have explicit port")
	}

	// Canonicalize authority (host:port)
	auth := canonicalizeAuthority(u.Host)

	// Strict Match
	if _, ok := s.allowedAuthorities[auth]; !ok {
		// No fuzzy matching, no localhost pivoting.
		// P3 Invariant: ONLY configured upstream.
		return fmt.Errorf("upstream authority %q not allowed (must match configured target exactly)", auth)
	}

	return nil
}

func canonicalizeAuthority(hostport string) string {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return strings.ToLower(hostport)
	}

	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			host = v4.String()
		} else {
			host = ip.String()
		}
	} else {
		host = strings.ToLower(host)
	}

	return net.JoinHostPort(host, port)
}

func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	logger := xglog.WithContext(r.Context(), s.logger)

	// 1. Authentication Check
	// Policy: If auth is required (!AuthAnonymous), we enforce it strictly.
	// Since New() enforces that s.apiToken is set if !AuthAnonymous, checking !AuthAnonymous implies checking expectedToken presence.
	if !s.authAnonymous {
		if !auth.AuthorizeRequest(r, s.apiToken, true) {
			logger.Warn().
				Str("event", "auth.fail").
				Str("path", r.URL.Path).
				Str("reason", "unauthorized").
				Msg("proxy request unauthorized")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}
	// Security: Fail fast on disallowed upstream URL
	if r.URL.RawQuery != "" {
		if values, err := url.ParseQuery(r.URL.RawQuery); err == nil {
			if upstream := values.Get("upstream"); upstream != "" {
				if err := s.validateUpstream(upstream); err != nil {
					logger.Warn().Str("upstream", upstream).Err(err).Msg("rejecting proxy request with disallowed upstream URL")
					http.Error(w, "Forbidden: disallowed upstream URL", http.StatusForbidden)
					return
				}
			}
		}
	}

	acquired := s.acquireStreamSlotIfNeeded(w, r)
	if acquired {
		defer s.releaseStreamSlot()
	}

	// Session Tracking Upgrade
	var session *StreamSession

	if isStreamSessionStart(r) {
		// Identify Stream
		path := r.URL.Path
		serviceRef := extractServiceRef(path)
		channelName := serviceRef // Default to ref

		// Try to look up name if possible?
		// For now simple ref is enough.

		// Note: isStream check logic was:
		// isStream := strings.HasPrefix(r.URL.Path, "/stream/") || strings.HasPrefix(r.URL.Path, "/api/stream.m3u")
		// But isStreamSessionStart() likely does similar.
		// Assuming isStreamSessionStart covers it.

		// If it's a playlist or direct stream (simple check to avoid segments)
		if strings.HasSuffix(r.URL.Path, ".m3u8") || !strings.Contains(r.URL.Path, ".") {
			// Create cancelable context for Terminate
			ctx, cancel := context.WithCancel(r.Context())
			r = r.WithContext(ctx)

			// Register
			session = s.registry.Register(r, channelName, serviceRef, nil)
			session.cancel = cancel

			defer s.registry.Unregister(session.ID)
		}
	}

	logger.Debug().
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Str("remote_addr", r.RemoteAddr).
		Msg("proxy request")

	// Update activity on write
	if session != nil {
		w = newSessionWriter(w, session, logger)
	}

	if s.tryHandleHEAD(w, r) {
		return
	}

	if s.tryHandleHLS(w, r) {
		return
	}

	if s.tryHandleTranscode(w, r) {
		return
	}

	// Strict check: if we can't resolve the target, fail before proxying.
	// This prevents the "Director" from processing an empty/invalid URL.
	//
	// Note: In tests, Server can be manually constructed with a fixed reverse proxy and without
	// TargetURL/ReceiverHost. In that case, skip resolution and let the injected proxy handle it.
	if s.targetURL != nil || s.receiverHost != "" || s.playlistPath != "" {
		if _, ok := s.resolveTargetURL(r.Context(), r.URL.Path, r.URL.RawQuery); !ok {
			http.Error(w, "stream not found", http.StatusNotFound)
			return
		}
	}

	metrics.IncActiveStreams("direct")
	defer metrics.DecActiveStreams("direct")
	s.proxy.ServeHTTP(w, r)
}

func extractServiceRef(path string) string {
	// Simple extraction logic, mirrors handleHLSRequest or resolveTargetURL partially
	trimmed := strings.Trim(path, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return "unknown"
}

// sessionWriter wraps ResponseWriter to update StreamSession.
type sessionWriter struct {
	http.ResponseWriter
	session  *StreamSession
	logger   zerolog.Logger
	wroteHdr bool
	mu       sync.Mutex
}

func newSessionWriter(w http.ResponseWriter, session *StreamSession, logger zerolog.Logger) *sessionWriter {
	return &sessionWriter{
		ResponseWriter: w,
		session:        session,
		logger:         logger,
	}
}

func (w *sessionWriter) WriteHeader(status int) {
	w.mu.Lock()
	w.wroteHdr = true
	w.mu.Unlock()
	w.ResponseWriter.WriteHeader(status)
}

func (w *sessionWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	if n > 0 {
		w.session.UpdateActivity(n)
	}
	return n, err
}

func (w *sessionWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *sessionWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("hijack not supported")
}

func (s *Server) resolveTargetURL(ctx context.Context, path, rawQuery string) (string, bool) {
	logger := xglog.WithContext(ctx, s.logger)

	serviceRef := strings.TrimPrefix(path, "/")
	isRef := strings.Contains(serviceRef, ":")

	if !isRef && serviceRef != "" {
		if streamURL, ok := s.lookupStreamURL(serviceRef); ok {
			if s.isSelfURL(streamURL) {
				if parsed, err := url.Parse(streamURL); err == nil {
					path = parsed.Path
					serviceRef = strings.TrimPrefix(path, "/")
					logger.Debug().Str("slug", serviceRef).Str("resolved_path", path).Msg("resolved slug to self-referencing proxy URL, extracting path")
				}
			} else {
				streamURL = appendRawQuery(streamURL, rawQuery)
				logger.Debug().Str("slug", serviceRef).Str("target", streamURL).Msg("resolved slug to upstream URL via M3U")
				return streamURL, true
			}
		} else {
			if err := s.loadM3U(); err == nil {
				if streamURL, ok := s.lookupStreamURL(serviceRef); ok {
					streamURL = appendRawQuery(streamURL, rawQuery)
					logger.Info().Str("slug", serviceRef).Msg("resolved slug after M3U reload")
					return streamURL, true
				}
			}
		}
	}

	// Dynamic Upstream Override (e.g. for Recordings)
	if rawQuery != "" {
		values, _ := url.ParseQuery(rawQuery)
		if upstream := values.Get("upstream"); upstream != "" {
			if err := s.validateUpstream(upstream); err == nil {
				logger.Debug().Str("upstream", upstream).Msg("using explicit upstream URL")
				return upstream, true
			}
			logger.Warn().Str("upstream", upstream).Msg("ignoring disallowed upstream URL")
		}
	}

	if s.targetURL != nil {
		targetURL := s.targetURL.String() + path
		return appendRawQuery(targetURL, rawQuery), true
	}

	// Universal Fallback (WebAPI Resolution)
	// If the stream is not in our loaded M3U, it might still be a valid Service Reference.
	// Instead of guessing Port 8001 (which breaks encrypted channels), we ask the WebAPI.
	if serviceRef != "" && !strings.Contains(serviceRef, "/") {
		// Construct the WebAPI URL for this reference
		// Note: We don't have the full "Zap" logic here, just the URL construction.
		// BUT: resolveTargetURL is synchronous and expected to return a URL string.
		// We can't block for 5s here easily if this is called in Director.

		// BETTER APPROACH:
		// We construct a "Virtual" WebAPI URL for this service ref.
		// We trust the ZapAndResolveStream logic (called later by HLS handler) to handle the actual resolution.
		// However, for DIRECT proxying (Director), we need a simplified approach.

		// Let's use the helper to get the WebAPI URL for this ref.
		// Then we can try to resolve it ONCE.

		// Wait, if we return a WebAPI URL here, the caller might use it as the upstream?
		// No, the caller expects a STREAM URL (http://ip:port/ref).

		// We must perform the resolution here if we want to be safe.
		// This might add latency to the first request, but it's better than broken streams.

		webAPIURL := coreopenwebif.ConvertToWebAPI(
			fmt.Sprintf("http://%s:80", s.receiverHost), // Base URL
			serviceRef,
		)

		logger.Info().Str("service_ref", serviceRef).Msg("attempting dynamic resolution via WebAPI (not in playlist)")

		// We use a short timeout resolution here just to get the port.
		// Actual Zapping/Delay happens in the HLS handler usually, but for direct proxy
		// we might need to do it here.
		info, err := resolveWebAPIStreamInfo(webAPIURL) // This is from hls_helper.go (package proxy)
		if err == nil {
			logger.Info().Str("service_ref", serviceRef).Str("resolved_url", info.URL).Msg("dynamically resolved stream URL")
			return appendRawQuery(info.URL, rawQuery), true
		}

		logger.Warn().Err(err).Str("service_ref", serviceRef).Msg("dynamic WebAPI resolution failed")
	}

	// NO BLIND FALLBACK to 8001.
	logger.Warn().Str("path", path).Msg("could not resolve stream URL (check playlist or upstream param)")
	return "", false
}

func (s *Server) acquireStreamSlotIfNeeded(w http.ResponseWriter, r *http.Request) bool {
	if s.streamLimiter == nil {
		return false
	}
	if !isStreamSessionStart(r) {
		return false
	}
	if !s.streamLimiter.TryAcquire(1) {
		http.Error(w, "too many concurrent streams", http.StatusTooManyRequests)
		return false
	}
	return true
}

func (s *Server) releaseStreamSlot() {
	if s.streamLimiter != nil {
		s.streamLimiter.Release(1)
	}
}

func isStreamSessionStart(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	path := r.URL.Path
	if strings.HasPrefix(path, "/api/") ||
		strings.HasPrefix(path, "/healthz") ||
		strings.HasPrefix(path, "/readyz") ||
		strings.HasPrefix(path, "/metrics") ||
		strings.HasPrefix(path, "/discover") ||
		strings.HasPrefix(path, "/lineup") ||
		strings.HasPrefix(path, "/device") ||
		strings.HasPrefix(path, "/files/") {
		return false
	}

	if strings.HasPrefix(path, "/hls/") {
		if strings.HasSuffix(path, ".m3u8") {
			return true
		}
		if strings.Contains(path, "segment_") || strings.HasSuffix(path, ".ts") {
			return false
		}
		return true
	}

	return true
}

func (s *Server) handleHLSRequest(w http.ResponseWriter, r *http.Request) {
	logger := xglog.WithContext(r.Context(), s.logger)

	path := r.URL.Path
	var serviceRef string
	var remainder string
	if strings.HasPrefix(path, "/hls/") {
		trimmed := strings.TrimPrefix(path, "/hls/")
		parts := strings.Split(trimmed, "/")
		if len(parts) > 0 {
			serviceRef = parts[0]
		}
		if len(parts) > 1 {
			remainder = parts[1]
		}
		serviceRef = strings.TrimSuffix(serviceRef, ".m3u8")
	} else {
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) > 0 {
			serviceRef = parts[0]
		}
		if len(parts) > 1 {
			remainder = parts[1]
		}
	}

	if serviceRef == "" {
		http.Error(w, "service reference required", http.StatusBadRequest)
		return
	}

	targetURL, ok := s.resolveTargetURL(r.Context(), "/"+serviceRef, r.URL.RawQuery)
	if !ok {
		http.Error(w, "service reference not found in playlist", http.StatusNotFound)
		return
	}

	logger.Debug().
		Str("service_ref", serviceRef).
		Str("target", targetURL).
		Str("path", path).
		Msg("serving HLS stream")

	if remainder == "preflight" || strings.HasSuffix(path, "/preflight") {
		if err := s.hlsManager.PreflightHLS(r.Context(), r, serviceRef, targetURL); err != nil {
			logger.Error().Err(err).Str("service_ref", serviceRef).Msg("HLS preflight failed")
			http.Error(w, "HLS preflight failed", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := s.hlsManager.ServeHLS(w, r, serviceRef, targetURL); err != nil {
		logger.Error().
			Err(err).
			Str("service_ref", serviceRef).
			Msg("HLS streaming failed")
		http.Error(w, "HLS streaming failed", http.StatusInternalServerError)
	}
}

func (s *Server) Start() error {
	logEvent := s.logger.Info().Str("addr", s.addr)

	monitorCtx, monitorCancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.monitorCancel = monitorCancel
	s.mu.Unlock()
	s.startIdleMonitor(monitorCtx)

	if s.targetURL != nil {
		logEvent.Str("target", s.targetURL.String())
	} else if s.receiverHost != "" {
		logEvent.Str("receiver", s.receiverHost).Str("mode", "receiver_fallback")
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

func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("shutting down stream proxy server")

	if s.hlsManager != nil {
		s.hlsManager.Shutdown()
	}

	s.mu.Lock()
	if s.monitorCancel != nil {
		s.monitorCancel()
		s.monitorCancel = nil
	}
	s.mu.Unlock()

	if s.proxy != nil && s.proxy.Transport != nil {
		if t, ok := s.proxy.Transport.(*http.Transport); ok {
			t.CloseIdleConnections()
		}
	}

	return s.httpServer.Shutdown(ctx)
}

func (s *Server) loadM3U() error {
	if s.playlistPath == "" {
		return nil
	}

	data, err := os.ReadFile(s.playlistPath)
	if err != nil {
		return fmt.Errorf("read playlist: %w", err)
	}

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

func (s *Server) lookupStreamURL(id string) (string, bool) {
	s.channelMu.RLock()
	url, ok := s.channelMap[id]
	s.channelMu.RUnlock()
	return url, ok
}

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

func (s *Server) startIdleMonitor(ctx context.Context) {
	if s.idleTimeout <= 0 {
		return
	}

	s.logger.Info().Dur("timeout", s.idleTimeout).Msg("starting centralized stream idle monitor")

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				now := time.Now()
				timeout := s.idleTimeout

				// Validate sessions from Registry, not sync.Map
				sessions := s.registry.List()
				for _, sess := range sessions {
					last := sess.LastActivity()
					if now.Sub(last) > timeout {
						s.logger.Warn().
							Str("session_id", sess.ID).
							Dur("last_activity", now.Sub(last)).
							Msg("stream idle timeout reached, cancelling")

						sess.cancel()
					}
				}
			}
		}
	}()
}
